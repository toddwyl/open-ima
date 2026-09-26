# P5: Deployment, Harness, and Smoke Plan

**Goal:** Deliver reproducible app/parser/Meilisearch deployment, a complete repository harness, reindex tooling, and an end-to-end smoke test covering ingestion, retrieval, chat, citations, and the embedded SPA.

**Current status:** Tasks 1-3 are implemented. The full harness and process-mode end-to-end smoke pass on 2026-09-27. Docker is not installed on the current host, so compose runtime verification and final archive remain required.

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
- Support `SMOKE_MODE=process` for hosts without Docker; both modes exercise the same HTTP assertions.

## Task 4: Completion audit and archive

- Run the completed harness from a clean state.
- Run process smoke and compose smoke.
- Audit every roadmap completion criterion, update README and AGENTS technology stack, then move all completed active plans to `docs/plans/completed/`.

## Verification record

- `./scripts/harness.sh`: passed; Go tests/vet/static build, 28 parser tests, 4 frontend tests, TypeScript check and Vite build all passed.
- `SMOKE_MODE=process ./scripts/smoke.sh`: passed twice; verified upload to `ready`, hybrid search, SSE chat, citations, and embedded SPA over real app/parser processes with deterministic Meili/model HTTP doubles.
- Compose YAML structure is covered by `deployment_test.go`.
- `docker compose --profile smoke up -d --build`: blocked because no Docker CLI/runtime is installed. This is still required to verify image builds, container health checks, real Meilisearch behavior, and the parser image-size target.
