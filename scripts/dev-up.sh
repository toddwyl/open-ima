#!/usr/bin/env bash
# 仅启动本地依赖（Meilisearch + parser sidecar），供手动分步开发使用。
# 一般不需要本脚本：./scripts/start.sh 会一键拉起全栈并复用已运行的依赖。
set -euo pipefail
cd "$(dirname "$0")/.."

echo "==> 1/3 Meilisearch"
if [[ -x .local/bin/meilisearch ]]; then
  (.local/bin/meilisearch --http-addr 127.0.0.1:7700 --no-analytics --db-path ./data/meili &)
elif command -v meilisearch >/dev/null; then
  (meilisearch --http-addr 127.0.0.1:7700 --no-analytics --db-path ./data/meili &)
else
  echo "meilisearch binary not found; run ./scripts/install.sh first" >&2
  exit 1
fi

echo "==> 2/3 parser sidecar"
if [ ! -d parser/.venv ]; then
  (cd parser && python3 -m venv .venv && .venv/bin/pip install -r requirements.txt)
fi
(cd parser && .venv/bin/uvicorn app.main:app --port 8100 &)

echo "==> 3/3 app"
echo "Ensure Ollama has bge-m3, set IMA_LLM_API_KEY, then run: go run ./cmd/open-ima"
