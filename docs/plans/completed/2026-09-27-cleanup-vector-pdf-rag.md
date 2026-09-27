# Cleanup Legacy Vector Code And Verify PDF RAG

**Status:** Completed on 2026-09-27.

## Goal

- Remove the obsolete Go-side embedding client and configuration now that Meilisearch owns Ollama embedding generation.
- Prove the complete PDF workflow: upload, parse, index, retrieve PDF content, include it in the LLM prompt, stream a document-grounded answer, and cite the PDF.

## Completion

- No runtime Go code calls an embedding API or constructs vectors.
- Current configuration and local scripts describe only the Meilisearch-managed Ollama embedder.
- Business E2E asserts a unique fact from the uploaded PDF appears in the streamed answer and the PDF document appears in citations.
- `./scripts/harness.sh` and the real local-process business E2E pass.

## Evidence

- Deleted `internal/llm/embedding.go` and its tests, stale mock endpoint, test servers, and `IMA_EMBEDDING_*` configuration.
- Renamed chunk `EmbeddingContent` to `RetrievalContent` and restored section context in documents sent to Meilisearch.
- Real local E2E uploaded a PDF containing the unique fact `ORCHID-7429`; the streamed answer contained that fact and citations included the PDF document ID.
- `./scripts/harness.sh` passed after the cleanup.
