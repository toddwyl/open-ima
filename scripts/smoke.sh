#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"
MODE="${SMOKE_MODE:-process}"
[[ "${MODE}" == "process" ]] || { echo "business smoke supports local process mode only" >&2; exit 2; }
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

[[ -x parser/.venv/bin/python ]] || { echo "parser/.venv is required for business smoke" >&2; exit 2; }
printf '# Smoke Test\n\nOpen IMA smoke document content.' >"${SMOKE_TMP}/smoke.md"
printf '<!doctype html><html><head><title>URL Business Fixture</title></head><body><main><h1>URL Fixture</h1><p>Open IMA URL business fixture unique content.</p></main></body></html>' >"${SMOKE_TMP}/page.html"
parser/.venv/bin/python - "${SMOKE_TMP}/smoke.pdf" "${SMOKE_TMP}/blank.pdf" <<'PY'
import sys
from fpdf import FPDF

pdf = FPDF()
pdf.add_page()
pdf.set_font("Helvetica", size=12)
pdf.multi_cell(0, 10, "Project Atlas launch code is ORCHID-7429. This fact exists only in the uploaded PDF.")
pdf.output(sys.argv[1])

blank = FPDF()
blank.add_page()
blank.output(sys.argv[2])
PY
python3 -m http.server 8300 --bind 127.0.0.1 --directory "${SMOKE_TMP}" >"${SMOKE_TMP}/fixture.log" 2>&1 & PIDS+=("$!")
wait_for "http://127.0.0.1:8300/page.html"

command -v ollama >/dev/null 2>&1 || { echo "ollama is required for business smoke" >&2; exit 2; }
curl --fail --silent http://127.0.0.1:11434/api/tags | grep -q 'bge-m3' || {
  echo "running Ollama with bge-m3 is required (run: ollama pull bge-m3)" >&2
  exit 2
}
if [[ -z "${SMOKE_MEILI_BIN:-}" && -x "${ROOT_DIR}/.local/bin/meilisearch" ]]; then
  SMOKE_MEILI_BIN="${ROOT_DIR}/.local/bin/meilisearch"
fi
if [[ -n "${SMOKE_MEILI_BIN:-}" ]]; then
  [[ -x "${SMOKE_MEILI_BIN}" ]] || { echo "SMOKE_MEILI_BIN must be an executable Meilisearch binary" >&2; exit 2; }
  "${SMOKE_MEILI_BIN}" --http-addr 127.0.0.1:7700 --db-path "${SMOKE_TMP}/meili" --no-analytics >"${SMOKE_TMP}/meili.log" 2>&1 & PIDS+=("$!")
else
  go run ./cmd/dev/mock-meili >"${SMOKE_TMP}/meili.log" 2>&1 & PIDS+=("$!")
fi
go run ./cmd/dev/mock-model >"${SMOKE_TMP}/model.log" 2>&1 & PIDS+=("$!")
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
	IMA_MEILI_EMBEDDER_URL="http://127.0.0.1:11434/api/embeddings" \
	IMA_MEILI_EMBEDDER_MODEL=bge-m3 \
  IMA_LLM_BASE_URL="http://127.0.0.1:8200/v1" \
  IMA_LLM_MODEL=mock \
	IMA_MEILI_EMBEDDER_DIMENSIONS=1024 \
go run ./cmd/open-ima >"${SMOKE_TMP}/app.log" 2>&1 & PIDS+=("$!")

wait_for "${BASE_URL}/health"
FIXTURE_URL="http://127.0.0.1:8300"
python3 scripts/business_e2e.py \
  --base-url "${BASE_URL}" \
	--meili-url "http://127.0.0.1:7700" \
  --fixture-dir "${SMOKE_TMP}" \
  --fixture-url "${FIXTURE_URL}"
