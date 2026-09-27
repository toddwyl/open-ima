# Local Settings Center

**Status:** Completed on 2026-09-27.

## Goal

Add a Web settings center for local LLM and Ollama/Meilisearch embedder configuration, persisted in SQLite and applied without exposing API keys.

## Acceptance

- Settings remain available after restart and override environment defaults.
- API keys are write-only through the API.
- OpenAI/Anthropic protocol and provider fields apply to subsequent RAG calls immediately.
- Embedder settings are validated through Meilisearch before persistence.
- Desktop/mobile UI, API tests, frontend tests, browser verification, and harness pass.

## Evidence

- SQLite persistence and API-key masking tests pass in `internal/settings`.
- Frontend component tests cover opening the configuration center and rendering saved values.
- Browser verification covered mobile layout, protocol switching, saving, and persistence after reload.
- `./scripts/harness.sh` and the real Meilisearch + Ollama business E2E both pass.
