# P3: RAG Search and Chat Implementation Plan

**Goal:** Deliver query rewriting, hybrid retrieval, SSE chat with citations, and conversation history on top of the completed P2 ingestion pipeline.

**Current status:** Ready to implement Task 1.

## Contracts

- `GET /api/kbs/{id}/search?q=&mode=hybrid|text` returns ranked results with highlighted snippets.
- `POST /api/kbs/{id}/chat` accepts `{conversation_id?, query}` and streams `token`, `citations`, `done`, or `error` SSE events.
- `GET /api/kbs/{id}/conversations` and `GET /api/conversations/{id}/messages` expose persisted history.
- Query rewriting uses the latest two rounds and silently falls back to the original query.
- Chat generation uses the latest five rounds and numbered source context; citations are emitted after tokens.
- Retrieval embeds original and rewritten queries, runs Meilisearch hybrid search with `semanticRatio: 0.5`, merges by reciprocal-rank fusion, deduplicates by chunk id, and keeps eight results.

## Task 1: Meilisearch search API

Extend `internal/meili` with `SearchRequest`, `SearchHit`, and `Search`. Cover text and hybrid request JSON, filters, highlighted content, scores, auth, and HTTP failures with `httptest`.

Verification: `go test ./internal/meili/`; `./scripts/harness.sh`.

## Task 2: Chat completion client

Add `internal/llm/chat.go` with non-streaming completion for query rewrite and streaming completion for answer generation. Parse OpenAI-compatible SSE frames, support `[DONE]`, propagate HTTP/JSON/scanner errors, and include authorization.

Verification: `go test ./internal/llm/`; `./scripts/harness.sh`.

## Task 3: RAG service, history, and HTTP handlers

Add `internal/rag` to manage conversations/messages, query rewriting, dual-query retrieval, RRF, prompt construction, search results, and SSE events. Persist user and completed assistant messages including citation JSON. Validate ownership by KB and reject empty queries.

Verification: `go test ./internal/rag/`; `go test ./internal/...`; `./scripts/harness.sh`.

## Task 4: Server wiring and end-to-end RAG test

Wire RAG into `server.New` and expose search/chat/history routes. Extend the external mock integration test through ingestion, hybrid search, SSE answer/citations, and persisted history.

Verification: `go test ./...`; `CGO_ENABLED=0 go build ./...`; `go vet ./...`; parser tests; `./scripts/harness.sh`.

## Completion

After all tasks pass, record commits here. Keep this plan active until P5 smoke proves the deployed API path, then move it to `completed/`.
