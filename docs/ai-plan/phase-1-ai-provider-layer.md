# Phase 1 — AI provider layer (`ai/` package)

**Depends on:** Phase 0 · **Size:** M (2–3 weeks) · **Status:** ☐ Not started

**Goal:** a server-side, provider-agnostic LLM abstraction with config, budgets, accounting, and health visibility — and **no user-facing features yet**. Everything later (Phases 2–6) calls only into this package.

## Design

New package `ai/` (no imports from `server/`; `server` depends on `ai`, never the reverse):

```go
// ai/provider.go
type Provider interface {
    Name() string
    Ping(ctx context.Context) error
    Complete(ctx context.Context, req Request) (Response, error) // non-streaming
    Stream(ctx context.Context, req Request) (<-chan StreamChunk, error)
}

type Request struct {
    Model       string        // resolved per-feature override or default
    System      string
    Messages    []Message
    MaxTokens   int
    Temperature float32
    JSONSchema  *Schema       // strict structured output when supported
    Feature     string        // "plan", "enrich", "digest", "chat", ... for accounting
    UserID      string        // budget attribution
}

type Response struct {
    Text        string
    InputTokens, OutputTokens int
    FinishReason string
}
```

Implementations:
- `openai.go` — OpenAI-compatible Chat Completions/Responses API; also covers OpenRouter, Groq, vLLM, LM Studio via `ai-base-url`.
- `anthropic.go` — Messages API.
- `ollama.go` — local-first default recommendation (`http://localhost:11434`), no API key required.
- `mock.go` — in-process scripted provider for tests (latency/error injection).
- Registry selected at server start; **nil provider when unconfigured** — every call site checks `s.ai != nil`, mirroring how `stripe`/`twilio`/`firebaseClient` are nil-if-unconfigured in the `Server` struct (`server/server.go:40`).

## Config (follows upstream triplet: config.go / server.yml / docs/config.md)

| Option | Default | Notes |
|---|---|---|
| `ai-enabled` | `false` | Master switch; false ⇒ upstream-identical behavior |
| `ai-provider` | — | `openai` \| `anthropic` \| `ollama` \| `openai-compatible` |
| `ai-base-url` | provider default | e.g. `http://localhost:11434/v1` |
| `ai-api-key` / `ai-api-key-file` | — | file variant like upstream's TLS key options |
| `ai-model` | provider default | default model for all features |
| `ai-model-<feature>` | — | per-feature overrides: `ai-model-plan`, `ai-model-enrich`, `ai-model-digest`, `ai-model-chat` |
| `ai-request-timeout` | `10s` | hard cap per call |
| `ai-inline-timeout` | `2s` | budget for blocking (inline) use in the message path — see Phase 3 |
| `ai-visitor-daily-token-budget` | `20000` | per user/IP; 429 with `Retry-After` on breach |
| `ai-global-daily-token-budget` | `2000000` | operator protection |
| `ai-cache-size` | `100MB` | response cache by content hash |

## Server wiring
- `server.New()` accepts the provider; stored as `s.ai` (nil-safe).
- New file `server/server_ai.go` with the first endpoint: `GET /v1/ai/status` (admin token required): provider, model, ping result, today's token usage vs budgets, cache hit rate.
- Prometheus counters in `metrics/`: `ntfy_ai_requests_total{feature,result}`, `ntfy_ai_tokens_total{feature,direction}`, `ntfy_ai_request_duration_seconds`.
- Structured log lines for every external call: `(feature, model, tokens, duration, cacheHit)` — this is also the privacy audit trail (Principle 3).
- Response cache: in-memory LRU keyed by `(feature, model, sha256(prompt-content))`; bounded by `ai-cache-size`.

## Tests
- Unit: mock provider — schema validation + reject/retry, timeout, budget exhaustion returns the upstream-style rate-limit error JSON, cache hits skip the provider.
- Config: yml/env/flag triplets resolve identically (copy patterns from existing config tests).
- Integration (opt-in, `NTFY_AI_E2E=1`): round-trip against a local Ollama if present; skipped in CI.

## Acceptance criteria
- `ntfy serve` with no AI options: byte-identical behavior, `/v1/ai/status` returns 404/feature-disabled.
- With `ai-provider: ollama`: status endpoint reports healthy; token counters increment; budgets enforce.
- `make check` green with mock provider only (no network).
- Decision record: which model per feature on Ollama vs cloud (cost/quality table in this doc once measured).

## Risks
- Provider API churn (esp. OpenAI) — contain it inside provider files; pin behavior with contract tests against recorded fixtures.
- Budget accounting granularity — decide now that streamed output tokens count at close, attribute to the initiating visitor.
