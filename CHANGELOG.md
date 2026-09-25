# Changelog (axon fork)

axon tracks upstream ntfy; the entries below cover fork-specific changes only.
Format loosely follows [Keep a Changelog](https://keepachangelog.com); fork versions
are planned as `v2.<upstream-minor>.<upstream-patch>-axon.N` (see docs/ai-plan/phase-7).

## [Unreleased]

### Added — AI layer (all opt-in, off by default via `ai-enabled: false`)
- AI provider abstraction (`ai/` package): OpenAI-compatible (also covers Ollama,
  OpenRouter, vLLM), Anthropic, and a scripted mock provider, with an LRU response
  cache, per-visitor + global daily token budgets, and Prometheus metrics.
- Natural-language subscription planning: `POST /v1/ai/plan` (+ `/v1/ai/tune`), web
  "Describe it with AI" assistant with plan review and multi-turn refinement.
- Inline message enrichment on opted-in topics (`ai-enrich-topics`): AI summary as the
  message title, importance classification (never overrides explicit publisher
  priority), optional translation (`ai-translate-topics` + `ai-translate-lang`), with a
  hard inline timeout and full pass-through on breach.
- Topic digests and cross-topic briefings: `POST /v1/ai/digest` and `POST /v1/ai/briefing`,
  with web dialogs ("AI summarize this topic…", "AI briefing").
- Chat over notification history: `POST /v1/ai/chat` (cited multi-turn answers, per-topic
  or cross-topic) and `POST /v1/ai/chat/stream` (SSE with delta/citations/done events and
  server-validated `[n]` citations); web dialog with progressive rendering.
- MCP server for AI agents (`ntfy mcp`): publish, read_messages, subscribe_wait,
  ask_history, digest_topic, briefing, list_subscriptions, plan_subscription.
- Scoped agent tokens: `ntfy token add --scope=...`; the `ai` scope gates the
  LLM-costing endpoints (fork database migration 9→10, SQLite + PostgreSQL).
- Admin/usage endpoints `GET /v1/ai/status` and `GET /v1/ai/usage`; `enable_ai` web
  config flag; AI section in server.yml and docs/config.md.

### Changed
- Web app: per-subscription query filters are now sent to the server as `?q=`
  (connection and poll requests); changing a filter reconnects the stream.
- `subscribeTopic` extracted to `web/src/app/subscribe.js` (shared by the manual
  dialog, the AI assistant, and reservations).

### Upstream compatibility
- Wire protocol, binary name, and config paths are unchanged: axon is a drop-in
  replacement for upstream ntfy servers and clients. Mobile apps work unmodified.
- Fork database migration `9→10` (token scopes) — see docs/ai-plan/upstream-sync.md
  for the renumber-on-conflict policy.
