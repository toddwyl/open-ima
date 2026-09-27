#!/usr/bin/env bash
# 一键启动 open-ima 本地全栈:Meilisearch + Ollama(bge-m3)+ parser sidecar + app(内嵌 SPA)。
# 配置来自 .env(IMA_* 前缀)或环境变量;Ollama 未运行时会自动拉起并确保 embedding 模型就绪。
# Ctrl+C 会一并停止本脚本启动的所有进程;已在运行的依赖会被复用而不重复启动。
set -euo pipefail
cd "$(dirname "$0")/.."

MEILI_ADDR="127.0.0.1:7700"
PARSER_ADDR="127.0.0.1:8100"
PIDS=()
APP_BIN=""

cleanup() {
  trap - EXIT INT TERM
  echo
  echo "==> stopping"
  for pid in "${PIDS[@]:-}"; do kill "${pid}" 2>/dev/null || true; done
  for pid in "${PIDS[@]:-}"; do wait "${pid}" 2>/dev/null || true; done
  [[ -z "${APP_BIN}" ]] || rm -f "${APP_BIN}"
}
trap cleanup EXIT INT TERM

wait_for() {
  local name="$1" url="$2"
  for _ in $(seq 1 80); do
    if curl --fail --silent "${url}" >/dev/null 2>&1; then
      echo "==> ${name} ready"
      return 0
    fi
    sleep 0.25
  done
  echo "timed out waiting for ${name} (${url})" >&2
  return 1
}

if [[ -f .env ]]; then
  set -a
  # shellcheck disable=SC1091
  source .env
  set +a
fi
HTTP_ADDR="${IMA_HTTP_ADDR:-:8080}"
APP_PORT="${HTTP_ADDR##*:}"

if [[ ! -f web/dist/index.html ]]; then
  echo "web/dist is missing; run: npm --prefix web ci && npm --prefix web run build" >&2
  exit 1
fi
mkdir -p data

echo "==> 1/4 Meilisearch"
if curl --fail --silent "http://${MEILI_ADDR}/health" >/dev/null 2>&1; then
  echo "    reusing instance already on ${MEILI_ADDR}"
else
  MEILI_BIN="${IMA_MEILI_BIN:-}"
  if [[ -z "${MEILI_BIN}" && -x .local/bin/meilisearch ]]; then
    MEILI_BIN=".local/bin/meilisearch"
  elif [[ -z "${MEILI_BIN}" ]] && command -v meilisearch >/dev/null 2>&1; then
    MEILI_BIN="$(command -v meilisearch)"
  fi
  [[ -n "${MEILI_BIN}" ]] || {
    echo "meilisearch binary not found; install it or place it at .local/bin/meilisearch" >&2
    exit 1
  }
  mkdir -p data/meili
  "${MEILI_BIN}" --http-addr "${MEILI_ADDR}" --db-path ./data/meili --no-analytics >data/meili.log 2>&1 &
  PIDS+=("$!")
  wait_for "Meilisearch" "http://${MEILI_ADDR}/health"
fi

echo "==> 2/4 parser sidecar"
if curl --fail --silent "http://${PARSER_ADDR}/health" >/dev/null 2>&1; then
  echo "    reusing instance already on ${PARSER_ADDR}"
else
  [[ -x parser/.venv/bin/python ]] || {
    echo "parser/.venv is missing; run: python3 -m venv parser/.venv && parser/.venv/bin/pip install -r parser/requirements.txt" >&2
    exit 1
  }
  # exec 让子 shell 进程直接替换为 uvicorn,$! 即为 python 进程,cleanup 可杀
  (cd parser && exec .venv/bin/python -m uvicorn app.main:app --host 127.0.0.1 --port 8100) >data/parser.log 2>&1 &
  PIDS+=("$!")
  wait_for "parser" "http://${PARSER_ADDR}/health"
fi

echo "==> 3/4 Ollama"
# 与 config 的默认值/env 对齐:embedder URL 去掉 /api/... 路径即 Ollama 服务根。
OLLAMA_URL="${IMA_MEILI_EMBEDDER_URL:-http://127.0.0.1:11434/api/embeddings}"
OLLAMA_BASE="${OLLAMA_URL%%/api/*}"
EMBED_MODEL="${IMA_MEILI_EMBEDDER_MODEL:-bge-m3}"
if curl --fail --silent "${OLLAMA_BASE}/api/tags" >/dev/null 2>&1; then
  echo "    reusing instance already on ${OLLAMA_BASE}"
else
  case "${OLLAMA_BASE}" in
    http://127.0.0.1:* | http://localhost:* | http://\[::1\]:*) ;;
    *)
      echo "Ollama not reachable at ${OLLAMA_BASE} (non-local embedder URL); start it yourself" >&2
      exit 1
      ;;
  esac
  command -v ollama >/dev/null 2>&1 || {
    echo "ollama CLI not found; install it from https://ollama.com" >&2
    exit 1
  }
  OLLAMA_HOST="${OLLAMA_BASE#http://}" ollama serve >data/ollama.log 2>&1 &
  PIDS+=("$!")
  wait_for "Ollama" "${OLLAMA_BASE}/api/tags"
fi
if ! curl --fail --silent "${OLLAMA_BASE}/api/tags" | grep -q "${EMBED_MODEL}"; then
  echo "    pulling embedding model ${EMBED_MODEL} ..."
  OLLAMA_HOST="${OLLAMA_BASE#http://}" ollama pull "${EMBED_MODEL}"
fi

echo "==> 4/4 app"
mkdir -p data
# 直接运行编译产物而非 go run:go run 不会把信号转发给子进程,会导致
# Ctrl+C 后服务残留。
APP_BIN="$(mktemp -t open-ima)"
go build -o "${APP_BIN}" ./cmd/open-ima
echo "    listening on http://127.0.0.1:${APP_PORT} (Ctrl+C stops everything)"
"${APP_BIN}" &
PIDS+=("$!")
wait_for "app" "http://127.0.0.1:${APP_PORT}/health"
wait "${PIDS[@]: -1}"
