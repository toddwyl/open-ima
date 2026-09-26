#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"
MODE="${SMOKE_MODE:-compose}"
BASE_URL="${SMOKE_BASE_URL:-http://127.0.0.1:8080}"
SMOKE_TMP="$(mktemp -d "${TMPDIR:-/tmp}/open-ima-smoke.XXXXXX")"
PIDS=()

cleanup() {
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
  go run ./cmd/mock-meili >"${SMOKE_TMP}/meili.log" 2>&1 & PIDS+=("$!")
  go run ./cmd/mock-model >"${SMOKE_TMP}/model.log" 2>&1 & PIDS+=("$!")
  (cd parser && .venv/bin/python -m uvicorn app.main:app --host 127.0.0.1 --port 8100) >"${SMOKE_TMP}/parser.log" 2>&1 & PIDS+=("$!")
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

KB_JSON="$(curl --fail --silent --show-error -X POST "${BASE_URL}/api/kbs" -H 'Content-Type: application/json' -d '{"name":"Smoke Test"}')"
KB_ID="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])' <<<"${KB_JSON}")"
UPLOAD_JSON="$(curl --fail --silent --show-error -X POST "${BASE_URL}/api/kbs/${KB_ID}/documents" -F "file=@${SMOKE_TMP}/smoke.md")"
DOC_ID="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["document_id"])' <<<"${UPLOAD_JSON}")"

STATUS=""
for _ in $(seq 1 80); do
  DOCS_JSON="$(curl --fail --silent "${BASE_URL}/api/kbs/${KB_ID}/documents")"
  STATUS="$(python3 -c 'import json,sys; d=json.load(sys.stdin); print(d[0]["status"] if d else "")' <<<"${DOCS_JSON}")"
  [[ "${STATUS}" == "ready" ]] && break
  [[ "${STATUS}" == "failed" ]] && { echo "document failed: ${DOCS_JSON}" >&2; exit 1; }
  sleep 0.25
done
[[ "${STATUS}" == "ready" ]] || { echo "document did not become ready: ${STATUS}" >&2; exit 1; }

SEARCH_JSON="$(curl --fail --silent --show-error "${BASE_URL}/api/kbs/${KB_ID}/search?q=smoke&mode=hybrid")"
python3 -c 'import json,sys; d=json.load(sys.stdin); assert d and d[0]["document_id"]==sys.argv[1]' "${DOC_ID}" <<<"${SEARCH_JSON}"

CHAT_BODY="$(curl --fail --silent --show-error -X POST "${BASE_URL}/api/kbs/${KB_ID}/chat" -H 'Content-Type: application/json' -d '{"query":"What does the smoke document say?"}')"
grep -q 'event: citations' <<<"${CHAT_BODY}"
grep -q "${DOC_ID}" <<<"${CHAT_BODY}"
grep -q 'event: done' <<<"${CHAT_BODY}"

HTML="$(curl --fail --silent --show-error "${BASE_URL}/")"
grep -q '<div id="root"></div>' <<<"${HTML}"
echo "smoke OK: document=${DOC_ID} status=${STATUS} search+chat+citations+spa"
