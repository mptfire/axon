# Changelog (axon fork)

axon tracks upstream ntfy; the entries below cover fork-specific changes only.
Format loosely follows [Keep a Changelog](https://keepachangelog.com); fork versions
are planned as `v2.<upstream-minor>.<upstream-patch>-axon.N` (see docs/ai-plan/phase-7).

## v2.28.0-axon.2 (2026-09-26)

### Fixed
- **Postgres**: provisioned-tokens query was missing the `scopes` column added by the
  scoped-tokens migration — provisioned-token reads failed on the Postgres backend
  (SQLite was unaffected; caught by CI on the Postgres test backend).

### Changed
- `.go-version` bumped to 1.27.1 (required by goreleaser v2.18.2 in CI);
  `template/gotext` regeneration marker updated to match.
- golint/revive + staticcheck clean across all packages (mock provider method docs,
  MCP error-string style, identical struct conversions).
- Web app formatting brought in line with prettier; eslint errors in AI components fixed.

### Added
- Release automation: `v*-axon*` tags now build static amd64 + arm64 binaries in CI and
  attach them to a GitHub Release automatically.

## v2.28.0-axon.1 (2026-09-25)

First public release: AI provider layer (Poolside/Ollama/OpenAI-compatible/Anthropic/mock),
subscription wizard + tuning, inline enrichment + translation, digests + cross-topic
briefings + scheduled briefings, streaming cited chat, MCP server, scoped agent tokens,
timezone-aware scheduling, security audit (docs/security-audit.md).

### Added — AI layer (all opt-in, off by default via `ai-enabled: false`)
- AI provider abstraction (`ai/` package): OpenAI-compatible (also covers Ollama,
  OpenRouter, vLLM), Anthropic, **Poolside** (`poolside`, inference.poolside.ai,
  Laguna models), and a scripted mock provider, with an LRU response cache,
  per-visitor + global daily token budgets, and Prometheus metrics.
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
  server-validated `[n]` citations); web dialog with progressive rendering. Optional
  semantic retrieval via `ai-embeddings-model` — hybrid keyword + vector ranking with an
  in-memory embedding cache.
- Scheduled daily briefings: opt in via account settings (delivery hour + timezone +
  window); the server summarizes recent activity across all your topics once a day and
  delivers it to a private per-user topic (`dg_*`, read-only ACL, auto-subscribed).
  IANA timezone database is embedded (works in minimal containers).
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
