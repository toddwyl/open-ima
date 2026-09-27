#!/usr/bin/env bash
# Start local dependencies without compose. P5 adds the production-like stack.
set -euo pipefail
cd "$(dirname "$0")/.."

echo "==> 1/3 Meilisearch"
if command -v meilisearch >/dev/null; then
  (meilisearch --http-addr 127.0.0.1:7700 --no-analytics --db-path ./data/meili &)
elif command -v docker >/dev/null; then
  (docker run --rm -p 7700:7700 -v "$PWD/data/meili:/meili_data" getmeili/meilisearch:v1.10 &)
else
  echo "Meilisearch or Docker is required." >&2
  exit 1
fi

echo "==> 2/3 parser sidecar"
if [ ! -d parser/.venv ]; then
  (cd parser && python3 -m venv .venv && .venv/bin/pip install -r requirements.txt)
fi
(cd parser && .venv/bin/uvicorn app.main:app --port 8100 &)

echo "==> 3/3 app"
echo "Ensure Ollama has bge-m3, set IMA_LLM_API_KEY, then run: go run ./cmd/open-ima"
