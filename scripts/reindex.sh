#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

DB_PATH="${1:-./data/open-ima.db}"
go run ./cmd/reindex -db "${DB_PATH}"
