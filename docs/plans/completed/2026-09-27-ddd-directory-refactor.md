# DDD directory refactor

## Status

Completed on branch `refactor/ddd-architecture`. Stages 1–6 are committed,
the harness passes at every stage boundary, and the process-based business
E2E (`./scripts/smoke.sh`) passes 25/25 business cases against real
Meilisearch and Ollama bge-m3. Merged to `main`; worktree cleaned up.

## Goal

Restructure the Go backend into horizontal DDD layers: multiple domains under
`internal/domain`, cross-domain workflows under `internal/application`, technical
implementations under `internal/infrastructure`, HTTP adapters under
`internal/interfaces`, and dependency composition under `internal/app`.

## Constraints

- Preserve HTTP APIs, SSE events, SQLite schema, environment variables, and runtime behavior.
- Keep local business verification process-based; Docker is a deployment artifact only.
- Do not modify the user-owned untracked `.claude/` directory in the main checkout.

## Progress

- Stage 1 (`ef25912`): consolidated Compose/Dockerfiles/ignore files under
  `deploy/docker/`; renamed the app command to `cmd/open-ima`; moved smoke
  mocks under `cmd/dev/`; updated scripts and README commands.
- Stage 2 (`6ca5e7f`): moved technical packages under `internal/infrastructure`
  (config, storage, queue, parser, meili, llm, fetch, sqlite as `db`→`sqlite`).
- Stage 3 (`b6a1ef4`): added `internal/domain` with the four domains
  `knowledgebase`, `document`, `conversation`, `settings` (entity, repository
  contract, domain service, errors each) plus stdlib-only `idgen`; sqlite
  repository implementations under `internal/infrastructure/sqlite`.
- Stage 4 (`6c2431a`): added `internal/application` — `port` (queue, storage,
  parser, search, chat) and use cases `ingest`, `knowledgebase`, `chat`,
  `settings`; infrastructure packages satisfy ports via type aliases.
- Stage 5 (`eafdd65`): moved handlers to `internal/interfaces/http` (package
  `httpapi`) and the composition root to `internal/app`; deleted legacy
  `kb`/`media`/`rag`/`settings`/`upload`/`server`/`httpx`; updated
  `cmd/open-ima` and `cmd/reindex`; handler and E2E tests moved with routes.
- Stage 6: added `internal/app/architecture_test.go` fixing the layering
  rules (non-test imports only); updated AGENTS.md repo map and README paths.

## Important decisions

- Horizontal DDD layers, not feature-first vertical slices.
- Domain packages depend on the standard library only; UUID generation uses a
  stdlib `idgen` instead of `github.com/google/uuid` inside the domain.
- Infrastructure implements `application/port` interfaces directly with type
  aliases (e.g. `type Message = port.ChatMessage`) to avoid adapter boilerplate.
- The settings→chat dependency cycle is broken by the `ChatReconfigurer`
  interface declared in `application/settings` and implemented by
  `application/chat`.
- Startup settings overlay lives in the composition root: `app.New` loads the
  kv map via the settings repository, overlays it onto the loaded config using
  domain key constants, and passes the effective values to the settings use
  case, which owns the in-memory current values afterwards.
- Preserve all HTTP contracts, SSE events, SQLite schema, environment
  variables, model protocols, and Meilisearch behavior (verified by ported
  tests at every stage).
- Local business E2E must use process scripts, real local Meilisearch, and
  Ollama embedding rather than Docker.

## Completion

- `./scripts/smoke.sh` passed 25/25 business cases with real Meilisearch and Ollama bge-m3.
- Browser verification not required: no frontend code changed; the business E2E covers every API the SPA consumes.
- Branch merged into `main`; worktree removed; plan archived.
