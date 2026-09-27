# DDD directory refactor

## Status

Paused for handoff on branch `refactor/ddd-architecture`. The first-stage
changes are intentionally uncommitted because the required harness run was
interrupted during `npm ci` and therefore did not pass.

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

- Created the isolated worktree and topic branch.
- Confirmed `dumps/` is an untracked, empty, unreferenced local directory.
- Moved the design document into `docs/design/` and repaired current links.
- Consolidated Compose, Dockerfiles, Docker ignore files, and the deployment
  contract test under `deploy/docker/`.
- Renamed the app command to `cmd/open-ima` and moved smoke mocks under
  `cmd/dev/`; updated current scripts and README commands.
- Started `./scripts/harness.sh`; `git diff --check`, `gofmt`, and `go vet`
  completed before the user interrupted the run during frontend installation.

## Important decisions

- Use horizontal DDD layers. `internal/domain` contains the four domains
  `knowledgebase`, `document`, `conversation`, and `settings`.
- Every domain owns `entity.go`, `repository.go`, `service.go`, and domain
  errors; repository files define interfaces only.
- Cross-domain workflows live under `internal/application`; concrete technical
  implementations live under `internal/infrastructure`; HTTP delivery lives
  under `internal/interfaces/http`; `internal/app` is the composition root.
- Preserve all HTTP contracts, SSE events, SQLite schema, environment
  variables, model protocols, and Meilisearch behavior.
- Local business E2E must use process scripts, real local Meilisearch, and
  Ollama embedding rather than Docker.

## Next steps

1. Review the uncommitted first-stage diff and rerun `./scripts/harness.sh`.
2. If it passes, commit the first stage before starting package refactoring.
3. Introduce domain entities, repository contracts, and services.
4. Split application workflows and infrastructure adapters.
5. Move HTTP delivery and composition into interfaces/app.
6. Add architecture tests, run the full harness and local business E2E, then archive this plan.
