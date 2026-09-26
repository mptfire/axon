# axon — AI-native notifications (a fork of ntfy)

**Branch:** `main` · **Upstream:** [binwiederhier/ntfy](https://github.com/binwiederhier/ntfy) (Apache-2.0) · **Name:** `axon` — *notifications with a brain.*

axon takes ntfy — a simple, battle-tested HTTP pub-sub notification service (Go server, React web app, mobile apps in separate repos) — and makes AI a first-class citizen in all three directions of the notification flow:

1. **AI sets up your subscriptions** (Phase 2, the headline): you describe what you care about in plain language; the assistant plans the topic, filters, priorities and publisher wiring, you review, one click applies it.
2. **AI processes your message stream** (Phases 3–5): enrichment and correlation at the delivery choke point, scheduled digests/briefings, and chat-with-your-notifications.
3. **AI agents use axon as transport** (Phase 6): a built-in MCP server so any agent can publish, wait on, and manage notifications.

Every phase works on top of the existing ntfy architecture — no protocol breaks, no rip-and-replace.

---

## Principles (non-negotiable)

1. **Works without AI.** `ai-enabled: false` (the default) is behavior-identical to upstream. Every AI feature degrades gracefully when the provider is down, over budget, or too slow.
2. **AI proposes, you dispose.** The assistant never silently subscribes, unsubscribes, drops, or rewrites messages without a reviewed confirmation step. Auto-modes (Phase 3) are opt-in per subscription.
3. **Privacy is a config choice.** Local-first: the recommended provider is Ollama on the same host, so message content never leaves the machine. If a cloud provider is configured, only the explicitly enabled features send content, and the server logs what was sent (topic, size, when) to an auditable local log. No training, no retention on provider side beyond the API call.
4. **Cost and abuse control.** Per-user/per-visitor token budgets, response caching by content hash, strict rate limits on all `/v1/ai/*` endpoints (reusing the `visitor` machinery), and usage visibility.
5. **Message content is untrusted input.** Every prompt that includes notification bodies defends against prompt injection: strict JSON output schemas, no tool-calling over message content, capability-limited application of results. This is a security requirement for every phase, reviewed at each acceptance gate.
6. **Wire-protocol compatibility.** The Android/iOS apps live in separate repos ([ntfy-android](https://github.com/binwiederhier/ntfy-android), [ntfy-ios](https://github.com/binwiederhier/ntfy-ios)) and must keep working unmodified. All new endpoints are additive (`/v1/ai/*`, MCP); no changes to `/`, `/v1/*` payload shapes (see the warning at `server/server.go:83`).
7. **Keep the fork mergeable.** New code lives in new files/packages (`ai/`, `server/server_ai.go`, `web/src/components/Ai*`); edits inside upstream files stay minimal and marked with `// axon:` comments so quarterly upstream merges stay cheap.

---

## Target architecture

```
                                   ┌─────────────────────────────────────────────┐
                                   │  axon server (Go)                           │
 publishers ──PUT/POST /topic──►  │                                             │
                                   │  handleInternal ──► /v1/ai/* ──┐            │
 AI agents ────MCP (stdio/SSE)──► │        │                       ▼            │
                                   │        │             ai/ package            │
                                   │        │         (Provider interface)       │
                                   │        │          │      │      │           │
                                   │        ▼        OpenAI  Anthropic Ollama    │
                                   │   handlePublishInternal                   │
                                   │        │                                   │
                                   │        ▼                                   │
                                   │   dispatch()  ◄── AI enrichment seam       │
                                   │     │  (server/server.go:824,             │
                                   │     │   the single choke point)            │
                                   │     ├──► topic fan-out ──► subscribers     │
                                   │     ├──► Firebase / email / webpush        │
                                   │     ├──► message cache (SQLite/Postgres)   │
                                   │     └──► digest scheduler ──► _digest_...  │
                                   └─────────────────────────────────────────────┘
                                   ▲
 web app (React) ──────────────────┘  WebSocket per subscription + AI assistant
                                      panel (plan/tune/chat) over /v1/ai/*
 mobile apps (unchanged) ────────────┘  same protocol as upstream ntfy
```

New pieces introduced by this plan (nothing upstream is replaced):

| Piece | Where | Introduced |
|---|---|---|
| AI provider abstraction | new `ai/` Go package | Phase 1 |
| AI endpoints (`/v1/ai/*`) | new `server/server_ai.go`, routed from `handleInternal` | Phase 1+ |
| Subscription planner (NL → plan JSON) | `ai/planner.go` | Phase 2 |
| Web AI assistant (subscribe wizard, tune, chat) | `web/src/components/Ai*` | Phases 2, 3, 5 |
| Delivery-time enrichment | hook in/around `dispatch()` | Phase 3 |
| Digest scheduler | reuses manager loop + delayed sender patterns | Phase 4 |
| Chat over history | SSE streaming over `messageCache` retrieval | Phase 5 |
| MCP server | new `ntfy mcp` subcommand (`cmd/mcp.go`) | Phase 6 |

---

## Phase index

| # | Phase | Delivers | Depends on | Size\* | Status |
|---|---|---|---|---|---|
| 0 | [Foundation & fork plumbing](phase-0-foundation.md) | Identity, CI, docs home, upstream-sync policy | — | S | ✅ Done (name: `axon`) |
| 1 | [AI provider layer](phase-1-ai-provider-layer.md) | `ai/` package, provider config, budgets, `/v1/ai/status` | 0 | M | ✅ Done |
| 2 | [AI subscription setup](phase-2-ai-subscription-setup.md) | **Headline:** natural-language subscription wizard in web app + `/v1/ai/plan` | 1 | M | ✅ Done (wizard, tune UI, refinement, per-subscription `?q=` filters) |
| 3 | [Smart delivery](phase-3-smart-delivery.md) | Summarize / classify / correlate / translate on the message path | 1 | L | ✅ Complete: summarize/classify/translate + embedding-based digest clustering (the original "correlate" scope, delivered via ai.Embedder) |
| 4 | [Digests & briefings](phase-4-digests-briefings.md) | Scheduled AI digests per user | 3 | M | ✅ Complete: on-demand digest + cross-topic briefing + **scheduled daily briefings** (prefs.digest, IANA timezone with local-hour delivery, private dg_ topics with read ACL) |
| 5 | [Chat with your notifications](phase-5-chat-with-notifications.md) | Conversational Q&A / search over history (streaming) | 1 | M | ✅ Complete: cited multi-turn chat per-topic + cross-topic, SSE streaming, hybrid semantic retrieval (`ai-embeddings-model`) | Conversational Q&A / search over history (streaming) | 1 | M | ✅ Cited multi-turn Q&A per topic and cross-topic, SSE-streamed answers with inline citations, web dialog, MCP `ask_history` (embeddings optional future) |
| 6 | [Agents & MCP](phase-6-agents-mcp.md) | `ntfy mcp`, scoped agent tokens, agent docs | 1, 2 | M | ✅ Done: `ntfy mcp` + `--scope` agent tokens (fork migration 9→10) + [docs/agents.md](../agents.md) |
| 7 | [Mobile, hardening & release](phase-7-mobile-privacy-release.md) | Mobile AI settings (separate repos), security review, 1.0 | 2–4 | L | ☐ Open: mobile apps (separate repos) + release engineering; CHANGELOG and docs are in place |

\* Size for 1–2 familiar engineers: S ≈ 1 week, M ≈ 2–4 weeks, L ≈ 4–8 weeks.

Dependency graph:

```
0 ──► 1 ──► 2 ──► 3 ──► 4
       │      │
       │      └────────────► 7  (also needs 3, 4)
       ├──► 5
       └──► 6  (needs 2's planner)
```

Minimum lovable product = **Phases 0 + 1 + 2**: a self-hostable ntfy whose web app sets up subscriptions from a plain-language description.

---

## Cross-cutting concerns (apply to every phase)

### Prompt-injection defense
Notification bodies are attacker-controllable input. Rules for any prompt that embeds them: (a) strict JSON-schema outputs validated server-side, reject-and-retry on violation; (b) never let model output directly invoke actions — the server maps validated fields to actions, and destructive ones still require user confirmation; (c) cap embedded content length; (d) treat model output citing "instructions" inside messages as data, never commands. Add adversarial test cases (golden files) per feature.

### Cost & rate control
- Per-visitor and global daily token budgets enforced in `ai/` (Phase 1), with HTTP 429 + `Retry-After` mirroring upstream's rate-limit response shape.
- Response cache keyed by `(feature, model, hash(content))` — bursty alert storms hit one LLM call.
- Usage counters exposed via `/v1/ai/usage` (self) and Prometheus metrics (`metrics/` package pattern).

### Testing strategy
- `ai/mock.go` in-process provider (scripted responses + injected latency/errors) so the full `make check` suite (Go tests, vitest, eslint) runs with zero network.
- Golden prompt files under `ai/testdata/prompts/` — prompt text is code, reviewed in diffs.
- Latency-budget tests: enrichment must fall back to the original message before `ai-inline-timeout`.

### Config & docs pattern
Every new option follows upstream's triplet: struct field in `server/config.go`, yml/env/flag wiring in `cmd/config_loader.go` + `server/server.yml`, and a row in `docs/config.md`. Web feature flags flow through `configResponse` (`server/server_web.go:76`) so the web app can hide AI UI when the server has no provider configured.

### Upstream sync
Merge `upstream/main` into `main` quarterly. Conflict risk is minimized by Principle 7 (isolated files). Before each merge, re-verify the integration seams listed in each phase doc, since upstream may move lines cited here (e.g. `dispatch()` at `server/server.go:824`).

### Licensing
Upstream is Apache-2.0 (with a GPLv2 option for the Android app, which lives elsewhere). The fork stays Apache-2.0, keeps upstream copyright notices, and adds a `NOTICE` entry crediting ntfy/Philipp Heckel. Rebranding choices (name, logo) happen in Phase 0 and must not imply endorsement.

---

## Building today

Identical to upstream ntfy:

```bash
make build         # web app + docs + CLI (web is embedded into the Go binary via //go:embed site)
make cli-linux-amd64
make check         # Go tests + web tests + vet/staticcheck/eslint/prettier
make docker-dev    # local Docker image
```

Docs (including this plan): `mkdocs serve` — this plan is under the **"AI fork plan"** nav section.
