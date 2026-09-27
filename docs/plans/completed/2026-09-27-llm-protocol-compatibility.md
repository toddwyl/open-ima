# LLM Protocol Compatibility

**Status:** Completed on 2026-09-27.

## Goal

Support both OpenAI Chat Completions and Anthropic Messages protocols through one configured chat client, including synchronous query rewriting and streaming RAG answers.

## Acceptance

- `IMA_LLM_PROTOCOL=openai|anthropic` selects request shape, endpoint, authentication headers, and response parser.
- Unit tests cover complete and streaming responses for both protocols.
- An opt-in integration test verifies both Kimi-compatible endpoints with one local API key.
- Full repository harness passes.

## Evidence

- Unit tests cover OpenAI and Anthropic complete/stream request and response formats.
- `TestKimiCompatibleProtocols` passed against both real Kimi endpoints with the local API key.
- `./scripts/harness.sh` passed.
