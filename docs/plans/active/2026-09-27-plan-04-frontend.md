# P4: React Knowledge Workspace Plan

**Goal:** Deliver the usable V1 SPA for knowledge-base management, document ingestion, RAG chat, and search, built into and served by the Go binary.

**Direction:** A quiet editorial knowledge workspace: paper white, ink black, teal actions, coral failures, compact navigation, and strong typographic hierarchy. The first screen is the actual workspace, not a landing page.

**Current status:** All four tasks implemented and verified on 2026-09-27.

- Main implementation: `639e950` embedded React knowledge workspace.
- Build hygiene: `5a1cce2` emission-free TypeScript checks.
- Browser QA: mobile-width documents, hybrid search, SSE chat, and citations verified against deterministic local mocks.

## Task 1: Frontend foundation and API client

Create a Vite + React + TypeScript app under `web/` with Vitest, Testing Library, and Lucide icons. Add typed API functions for every V1 route and an SSE parser for chat events.

Verification: API/SSE unit tests, TypeScript check, production build.

## Task 2: Knowledge workspace UI

Build knowledge-base navigation and creation/deletion, document upload and URL ingestion, 3-second status polling, retry/delete controls, conversations and streaming chat with citations, and text/hybrid search with highlighted snippets. Include loading, empty, failure, and narrow-screen states.

Verification: component tests for primary workflows and error states.

## Task 3: Go embed and route fallback

Embed `web/dist` into the Go binary and serve SPA routes without shadowing `/api` or `/internal`. Add handler tests for assets and fallback.

Verification: `npm test`, `npm run typecheck`, `npm run build`, `go test ./...`, `CGO_ENABLED=0 go build ./...`.

## Task 4: Browser QA

Run the app against deterministic local API mocks, inspect desktop and mobile screenshots, verify no overlap/overflow, and exercise create/upload/chat/search navigation.

Completion remains active until P5 deployment smoke verifies the built SPA with the deployed backend.
