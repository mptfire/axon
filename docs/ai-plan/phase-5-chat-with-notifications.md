# Phase 5 — Chat with your notifications

**Depends on:** Phase 1 · **Size:** M (3 weeks) · **Status:** ✅ Done (SSE streaming: `POST /v1/ai/chat/stream` with delta/citations/done events, `[n]` inline markers resolved and validated server-side, graceful degradation for non-streaming providers, web progressive rendering; embeddings shipped: hybrid retrieval via `ai-embeddings-model`)

**Goal:** conversational search and Q&A over your notification history — "what did the backups topic say last night?", "summarize this week's alerts", "when was the last time prod-alerts was quiet for 24h?" — streamed, with answers that cite the underlying messages.

## Server: `POST /v1/ai/chat` (streaming)

- Auth: user token only (Bearer; the existing `maybeAuthenticate` path) — no anonymous chat (budget abuse).
- Retrieval-first design (v1 is deliberately classical, no embeddings):
  1. The server translates the question into scoped queries over `messageCache` (topic ∈ user's subscriptions, time range, priority, search terms) — reusing `messageCache.Messages(Capped)` filtering.
  2. Top-K messages (by recency + term match) become quoted context blocks; only then does the model answer.
  3. A cheap first-turn classifier can ask the user to disambiguate ("which topic: `backups` or `db-backups`?").
- Response: SSE stream (the SSE `messageEncoder` used by `handleSubscribeSSE` is the template) of `{delta}` chunks terminated by a `citations` event: message ids + topics, which the web app renders as click-to-scroll links.
- Conversation state: client-supplied message list (stateless server; last N turns), persisted client-side in Dexie per user; nothing stored server-side beyond the standard AI audit log line.
- Embeddings are a stretch goal behind the same endpoint (hybrid retrieval) — only if v1 recall disappoints; would need a pgvector/SQLite-vec decision and its own privacy review.

## Web app

- Chat drawer (right panel) available on AllSubscriptions and per-topic views; context chips auto-attach (current topic, visible time range).
- Answers render markdown; citations inline as message links; "answer again with stricter time range" affordance.
- Cost/transparency footer: tokens used this conversation (from a `usage` SSE event).

## Privacy & injection (cross-cutting rules, restated for this phase)
- Message bodies in context are **data**: system prompt instructs answer-only; no tools; schema-validated citations.
- Everything stays local with Ollama; with a cloud provider, the audit log shows exactly which topics' content left the machine (Principle 3).
- No model fine-tuning/training flags on any provider request.

## Tests
- Retrieval unit tests: query planning over fixtures (time ranges, topic scoping, term ranking).
- Streaming: SSE chunk ordering, citations event last, disconnect mid-stream cleans up (errgroup cancel — same patterns as `handleSubscribeWS`).
- Injection corpus: questions + bodies crafted to steer the model into dropping citations or inventing messages — citations must resolve to real ids or be stripped.
- Budget: long conversations count per-turn; hard conversation cap with a friendly notice.

## Acceptance criteria
- Over a 30-day real corpus (seeded from `docs/examples/` scenarios), "summarize last week's prod alerts" returns a correct, fully-cited answer in <10s p95 streaming-first-token <1.5s.
- Works fully offline against local Ollama.
- No server-side conversation persistence (verify: cache/DB diffs empty after a session).

## Risks
- Recall quality without embeddings — mitigated by good query planning + disambiguation turn; revisit after dogfooding.
- Token burn from long histories — client-side turn trimming + server-side hard cap.
