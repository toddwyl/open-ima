# V1 Business E2E Coverage Correction

**Goal:** Correct the premature completion claim by deriving end-to-end cases from the V1 design and proving each user-visible workflow against directly started app/parser/Meilisearch processes.

**Status:** Completed on 2026-09-27.

## Required matrix

- Health and embedded SPA fallback.
- Knowledge-base create, validation, duplicate-name conflict, list/count, and delete.
- File upload validation, hash deduplication, Markdown/PDF success, parser failure, retry, listing, and deletion.
- URL validation, ingestion, content deduplication, parsing, indexing, and searchability.
- Hybrid and text search, invalid search requests, highlighting, and deletion cleanup.
- New chat and continued chat, SSE tokens/citations/done, conversation list, persisted message history, and cross-KB conversation rejection.
- Internal file endpoint authorization.
- Final harness plus the complete business E2E suite against real Meilisearch v1.10.3.

## Completion rule

Every row above must have an explicit executable assertion and pass in one clean run. A single ingestion/search/chat happy path is insufficient. Record failures and fixes here, run `HARNESS_BASELINE_CHECK=1 ./scripts/harness.sh`, then archive this plan.

## Evidence

- `scripts/business_e2e.py` asserts every matrix row through HTTP, including failure and cleanup paths.
- `scripts/smoke.sh` starts local processes directly and uses project-local Meilisearch v1.10.3 plus Ollama `bge-m3`.
- Meilisearch owns embedding generation through `source: ollama`; indexed documents contain no `_vectors`, and hybrid queries contain no application-generated `vector`.
- A real probe and the complete suite confirmed that Meilisearch v1.10.3 requires Ollama's `/api/embeddings` compatibility endpoint.
- The expanded suite found and fixed orphan document creation, wrong 5xx/2xx status codes for missing resources, and `null` responses from empty list endpoints.
- Final complete business E2E result: `BUSINESS E2E OK`.
