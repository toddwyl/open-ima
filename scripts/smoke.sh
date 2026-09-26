#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"
MODE="${SMOKE_MODE:-process}"
BASE_URL="${SMOKE_BASE_URL:-http://127.0.0.1:8080}"
SMOKE_TMP="$(mktemp -d "${TMPDIR:-/tmp}/open-ima-smoke.XXXXXX")"
PIDS=()

cleanup() {
  local status=$?
  if [[ "${status}" -ne 0 ]]; then
    for log in "${SMOKE_TMP}"/*.log; do
      [[ -f "${log}" ]] || continue
      echo "--- ${log} ---" >&2
      tail -n 80 "${log}" >&2 || true
    done
  fi
  for pid in "${PIDS[@]:-}"; do
    kill "${pid}" 2>/dev/null || true
  done
  for pid in "${PIDS[@]:-}"; do
    wait "${pid}" 2>/dev/null || true
  done
  if [[ "${MODE}" == "compose" ]] && command -v docker >/dev/null 2>&1; then
    docker compose --profile smoke down --volumes >/dev/null 2>&1 || true
  fi
  rm -r "${SMOKE_TMP}" 2>/dev/null || true
}
trap cleanup EXIT

wait_for() {
  local url="$1"
  for _ in $(seq 1 80); do
    if curl --fail --silent "${url}" >/dev/null; then return 0; fi
    sleep 0.25
  done
  echo "timed out waiting for ${url}" >&2
  return 1
}

if [[ "${MODE}" == "compose" ]]; then
  command -v docker >/dev/null 2>&1 || { echo "docker is required for compose smoke" >&2; exit 2; }
  export IMA_LLM_BASE_URL="http://model-mock:8200/v1"
  export IMA_EMBEDDING_BASE_URL="http://model-mock:8200/v1"
  export IMA_EMBEDDING_DIMENSIONS=3
  docker compose --profile smoke up -d --build
else
  [[ -x parser/.venv/bin/python ]] || { echo "parser/.venv is required for process smoke" >&2; exit 2; }
  if [[ -z "${SMOKE_MEILI_BIN:-}" && -x "${ROOT_DIR}/.local/bin/meilisearch" ]]; then
    SMOKE_MEILI_BIN="${ROOT_DIR}/.local/bin/meilisearch"
  fi
  if [[ -n "${SMOKE_MEILI_BIN:-}" ]]; then
    [[ -x "${SMOKE_MEILI_BIN}" ]] || { echo "SMOKE_MEILI_BIN must be an executable Meilisearch binary" >&2; exit 2; }
    "${SMOKE_MEILI_BIN}" --http-addr 127.0.0.1:7700 --db-path "${SMOKE_TMP}/meili" --no-analytics >"${SMOKE_TMP}/meili.log" 2>&1 & PIDS+=("$!")
  else
    go run ./cmd/mock-meili >"${SMOKE_TMP}/meili.log" 2>&1 & PIDS+=("$!")
  fi
  go run ./cmd/mock-model >"${SMOKE_TMP}/model.log" 2>&1 & PIDS+=("$!")
  (cd parser && .venv/bin/python -m uvicorn app.main:app --host 127.0.0.1 --port 8100) >"${SMOKE_TMP}/parser.log" 2>&1 & PIDS+=("$!")
  wait_for "http://127.0.0.1:7700/health"
  wait_for "http://127.0.0.1:8200/health"
  wait_for "http://127.0.0.1:8100/health"
  IMA_DATA_DIR="${SMOKE_TMP}/data" \
  IMA_HTTP_ADDR=":8080" \
  IMA_PUBLIC_BASE_URL="http://127.0.0.1:8080" \
  IMA_PARSER_URL="http://127.0.0.1:8100" \
  IMA_MEILI_URL="http://127.0.0.1:7700" \
  IMA_MEILI_INDEX=chunks \
  IMA_LLM_BASE_URL="http://127.0.0.1:8200/v1" \
  IMA_LLM_MODEL=mock \
  IMA_EMBEDDING_BASE_URL="http://127.0.0.1:8200/v1" \
  IMA_EMBEDDING_MODEL=mock \
  IMA_EMBEDDING_DIMENSIONS=3 \
  go run ./cmd/server >"${SMOKE_TMP}/app.log" 2>&1 & PIDS+=("$!")
fi

wait_for "${BASE_URL}/health"
printf '# Smoke Test\n\nOpen IMA smoke document content.' >"${SMOKE_TMP}/smoke.md"
if [[ "${MODE}" == "process" ]]; then
  parser/.venv/bin/python - "${SMOKE_TMP}/smoke.pdf" <<'PY'
import sys
from fpdf import FPDF

pdf = FPDF()
pdf.add_page()
pdf.set_font("Helvetica", size=12)
pdf.multi_cell(0, 10, "Open IMA PDF smoke document content.")
pdf.output(sys.argv[1])
PY
fi

KB_JSON="$(curl --fail --silent --show-error -X POST "${BASE_URL}/api/kbs" -H 'Content-Type: application/json' -d '{"name":"Smoke Test"}')"
KB_ID="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])' <<<"${KB_JSON}")"
UPLOAD_JSON="$(curl --fail --silent --show-error -X POST "${BASE_URL}/api/kbs/${KB_ID}/documents" -F "file=@${SMOKE_TMP}/smoke.md")"
DOC_ID="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["document_id"])' <<<"${UPLOAD_JSON}")"
PDF_DOC_ID=""
if [[ "${MODE}" == "process" ]]; then
  PDF_UPLOAD_JSON="$(curl --fail --silent --show-error -X POST "${BASE_URL}/api/kbs/${KB_ID}/documents" -F "file=@${SMOKE_TMP}/smoke.pdf")"
  PDF_DOC_ID="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["document_id"])' <<<"${PDF_UPLOAD_JSON}")"
fi

wait_document_ready() {
  local document_id="$1"
  local status=""
  local documents_json=""
  for _ in $(seq 1 80); do
    documents_json="$(curl --fail --silent "${BASE_URL}/api/kbs/${KB_ID}/documents")"
    status="$(python3 -c 'import json,sys; d=json.load(sys.stdin); i=sys.argv[1]; print(next((x["status"] for x in d if x["id"]==i), ""))' "${document_id}" <<<"${documents_json}")"
    [[ "${status}" == "ready" ]] && return 0
    [[ "${status}" == "failed" ]] && { echo "document failed: ${documents_json}" >&2; return 1; }
    sleep 0.25
  done
  echo "document did not become ready: ${document_id} status=${status}" >&2
  return 1
}

wait_document_ready "${DOC_ID}"
if [[ -n "${PDF_DOC_ID}" ]]; then
  wait_document_ready "${PDF_DOC_ID}"
fi

SEARCH_JSON="$(curl --fail --silent --show-error "${BASE_URL}/api/kbs/${KB_ID}/search?q=smoke&mode=hybrid")"
python3 -c 'import json,sys; d=json.load(sys.stdin); ids={x["document_id"] for x in d}; assert all(i in ids for i in sys.argv[1:])' "${DOC_ID}" ${PDF_DOC_ID:+"${PDF_DOC_ID}"} <<<"${SEARCH_JSON}"

CHAT_BODY="$(curl --fail --silent --show-error -X POST "${BASE_URL}/api/kbs/${KB_ID}/chat" -H 'Content-Type: application/json' -d '{"query":"What does the smoke document say?"}')"
grep -q 'event: citations' <<<"${CHAT_BODY}"
grep -q "${DOC_ID}" <<<"${CHAT_BODY}"
grep -q 'event: done' <<<"${CHAT_BODY}"

CONVERSATIONS_JSON="$(curl --fail --silent --show-error "${BASE_URL}/api/kbs/${KB_ID}/conversations")"
CONVERSATION_ID="$(python3 -c 'import json,sys; d=json.load(sys.stdin); assert len(d)==1; print(d[0]["id"])' <<<"${CONVERSATIONS_JSON}")"
MESSAGES_JSON="$(curl --fail --silent --show-error "${BASE_URL}/api/conversations/${CONVERSATION_ID}/messages")"
python3 -c 'import json,sys; d=json.load(sys.stdin); assert [m["role"] for m in d]==["user","assistant"]; assert d[1]["citations"]' <<<"${MESSAGES_JSON}"

HTML="$(curl --fail --silent --show-error "${BASE_URL}/")"
grep -q '<div id="root"></div>' <<<"${HTML}"
echo "smoke OK: markdown=${DOC_ID} pdf=${PDF_DOC_ID:-not-run} status=ready search+chat+citations+history+spa"
