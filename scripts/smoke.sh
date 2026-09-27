#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"
MODE="${SMOKE_MODE:-process}"
[[ "${MODE}" == "process" ]] || { echo "business smoke supports local process mode only" >&2; exit 2; }
SMOKE_TMP="$(mktemp -d "${TMPDIR:-/tmp}/open-ima-smoke.XXXXXX")"
PIDS=()

# 随机空闲端口:与开发栈(8080/8100/7700...)彻底解耦,避免连到残留实例上。
# 选取后存在极小的竞态窗口(端口被他人抢占),smoke 进程绑定失败会立即报错。
free_port() {
	python3 -c 'import socket; s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()'
}
APP_PORT="$(free_port)"
PARSER_PORT="$(free_port)"
MEILI_PORT="$(free_port)"
MODEL_PORT="$(free_port)"
FIXTURE_PORT="$(free_port)"
BASE_URL="http://127.0.0.1:${APP_PORT}"

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
python3 -m http.server "${FIXTURE_PORT}" --bind 127.0.0.1 --directory "${SMOKE_TMP}" >"${SMOKE_TMP}/fixture.log" 2>&1 & PIDS+=("$!")
wait_for "http://127.0.0.1:${FIXTURE_PORT}/page.html"

command -v ollama >/dev/null 2>&1 || { echo "ollama is required for business smoke" >&2; exit 2; }
curl --fail --silent http://127.0.0.1:11434/api/tags | grep -q 'bge-m3' || {
  echo "running Ollama with bge-m3 is required (run: ollama pull bge-m3)" >&2
  exit 2
}
# go run 不会向子进程转发信号,cleanup 杀 go run 包装进程会遗留编译产物
# 子进程占用端口;先编译为临时二进制再直接执行,kill 才生效。
BIN_DIR="${SMOKE_TMP}/bin"
mkdir -p "${BIN_DIR}"
go build -o "${BIN_DIR}/" ./cmd/dev/mock-meili ./cmd/dev/mock-model ./cmd/open-ima

if [[ -z "${SMOKE_MEILI_BIN:-}" && -x "${ROOT_DIR}/.local/bin/meilisearch" ]]; then
  SMOKE_MEILI_BIN="${ROOT_DIR}/.local/bin/meilisearch"
fi
if [[ -n "${SMOKE_MEILI_BIN:-}" ]]; then
  [[ -x "${SMOKE_MEILI_BIN}" ]] || { echo "SMOKE_MEILI_BIN must be an executable Meilisearch binary" >&2; exit 2; }
  "${SMOKE_MEILI_BIN}" --http-addr "127.0.0.1:${MEILI_PORT}" --db-path "${SMOKE_TMP}/meili" --no-analytics >"${SMOKE_TMP}/meili.log" 2>&1 & PIDS+=("$!")
else
  "${BIN_DIR}/mock-meili" -addr "127.0.0.1:${MEILI_PORT}" >"${SMOKE_TMP}/meili.log" 2>&1 & PIDS+=("$!")
fi
"${BIN_DIR}/mock-model" -addr "127.0.0.1:${MODEL_PORT}" >"${SMOKE_TMP}/model.log" 2>&1 & PIDS+=("$!")
# exec 让子 shell 进程直接替换为 uvicorn,$! 即为 python 进程,cleanup 可杀
(cd parser && exec .venv/bin/python -m uvicorn app.main:app --host 127.0.0.1 --port "${PARSER_PORT}") >"${SMOKE_TMP}/parser.log" 2>&1 & PIDS+=("$!")
wait_for "http://127.0.0.1:${MEILI_PORT}/health"
wait_for "http://127.0.0.1:${MODEL_PORT}/health"
wait_for "http://127.0.0.1:${PARSER_PORT}/health"
IMA_DATA_DIR="${SMOKE_TMP}/data" \
  IMA_HTTP_ADDR="127.0.0.1:${APP_PORT}" \
  IMA_PUBLIC_BASE_URL="http://127.0.0.1:${APP_PORT}" \
  IMA_PARSER_URL="http://127.0.0.1:${PARSER_PORT}" \
  IMA_MEILI_URL="http://127.0.0.1:${MEILI_PORT}" \
  IMA_MEILI_INDEX=chunks \
	IMA_MEILI_EMBEDDER_URL="http://127.0.0.1:11434/api/embeddings" \
	IMA_MEILI_EMBEDDER_MODEL=bge-m3 \
  IMA_LLM_BASE_URL="http://127.0.0.1:${MODEL_PORT}/v1" \
  IMA_LLM_MODEL=mock \
	IMA_MEILI_EMBEDDER_DIMENSIONS=1024 \
"${BIN_DIR}/open-ima" >"${SMOKE_TMP}/app.log" 2>&1 & PIDS+=("$!")

wait_for "${BASE_URL}/health"
FIXTURE_URL="http://127.0.0.1:${FIXTURE_PORT}"
python3 scripts/business_e2e.py \
  --base-url "${BASE_URL}" \
	--meili-url "http://127.0.0.1:${MEILI_PORT}" \
  --fixture-dir "${SMOKE_TMP}" \
  --fixture-url "${FIXTURE_URL}"
