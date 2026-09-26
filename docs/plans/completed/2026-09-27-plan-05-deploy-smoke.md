# P5: Deployment, Harness, and Smoke Plan

**Goal:** Deliver reproducible app/parser/Meilisearch deployment, a complete repository harness, reindex tooling, and an end-to-end smoke test covering ingestion, retrieval, chat, citations, and the embedded SPA.

**Current status:** Complete on 2026-09-27. Per the final local-testing contract, the authoritative E2E runs through directly started processes rather than Docker. Deployment manifests remain available for optional container deployment.

**Implementation commit:** `455799b`.

## Task 1: Production images and compose

- Add an app multi-stage Dockerfile: Node builds `web/dist`, Go builds static server and mock-model binaries, minimal runtime serves port 8080.
- Add parser and Meilisearch health checks and persistent data volumes in `docker-compose.yml`.
- Keep the default stack at three services. Add a profile-gated OpenAI-compatible model mock for deterministic smoke tests only.
- Add `.dockerignore` and example configuration/environment files without secrets.

## Task 2: Complete harness and reindex tooling

- Replace every harness TODO with formatting, vet, Go tests/build, parser tests, frontend audit/test/typecheck/build, and shell syntax checks.
- Add `cmd/reindex` and `scripts/reindex.sh` to enqueue full idempotent document reprocessing after embedding/index changes.

## Task 3: End-to-end smoke

- Add `cmd/mock-model` for deterministic embeddings, rewrite, and streaming chat.
- Add `scripts/smoke.sh` that starts the stack, waits for health, creates a KB, uploads Markdown, waits for `ready`, searches, chats, verifies citations/history, and verifies embedded SPA HTML.
- Use process mode by default; support optional `SMOKE_MEILI_BIN` for a real local Meilisearch process and explicit `SMOKE_MODE=compose` for container deployment.

## Task 4: Completion audit and archive

- Run the completed harness from a clean state.
- Run process smoke with the deterministic Meilisearch double and with a real Meilisearch binary.
- Audit every roadmap completion criterion, update README and AGENTS technology stack, then move all completed active plans to `docs/plans/completed/`.

## Verification record

- `./scripts/harness.sh`: passed; Go tests/vet/static build, 28 parser tests, 4 frontend tests, TypeScript check and Vite build all passed.
- `./scripts/smoke.sh`: passed; verified Markdown and generated PDF upload to `ready`, hybrid search, SSE chat, citations, conversation/message history, and embedded SPA over real app/parser processes.
- `./scripts/smoke.sh`: also passed with the official Meilisearch v1.10.3 Apple Silicon binary persisted at the gitignored project path `.local/bin/meilisearch`. This run exposed the required vector-store feature initialization; `EnsureIndex` now enables it idempotently before configuring the user-provided embedder.
- Compose YAML structure is covered by `deployment_test.go`.
- Docker runtime verification was intentionally not used for local testing, per the final repository contract in `AGENTS.md`.
