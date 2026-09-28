# Security audit — axon deployment

**Scope:** axon server (fork of ntfy v2.28.0-23-g1d2b9c36) with the full AI layer compiled in,
audited against a live local instance identical to the planned deployment (deny-all auth, AI
enabled with a scripted provider, all `/v1/ai/*` endpoints active). Binary: fully static Go
build, runs as the unprivileged `axon` user under a hardened systemd unit.

**Date:** 2026-09-25 · **Auditor:** automated + manual review · **Result: PASS with recommendations**

---

## Verified properties (attack-surface tests, all against the live instance)

### Authentication & access control
| Test | Result |
|---|---|
| Anonymous access to user-gated endpoints (`/v1/account` write ops, `/v1/ai/tune`, `/v1/ai/digest`, `/v1/ai/chat`, `/v1/ai/chat/stream`, `/v1/ai/briefing`) | ✅ 401 |
| Anonymous publish under `auth-default-access: deny-all` | ✅ 403 |
| Anonymous access to admin endpoints (`/v1/users`, `/v1/version`) | ✅ 401 |
| Invalid bearer token | ✅ 401 |
| Digest delivery topic: other users cannot read (deny-all + per-user read ACL) | ✅ 403 |
| Scoped tokens: token with `publish` scope calling `POST /v1/ai/plan` | ✅ 403 |
| Scoped token with `ai` scope / unscoped token / password auth | ✅ 200 |
| MCP server network exposure | ✅ none — stdio only, no HTTP route |

`GET /v1/account` and `GET /v1/ai/usage` return 200 to anonymous callers **by upstream design**;
verified that the bodies contain only the anonymous visitor's own limits/counters — no user data.

### AI-specific controls (the fork's novel surface)
| Test | Result |
|---|---|
| Per-visitor daily request quota (10/day): 10 calls allowed, 11th+ → 429 | ✅ |
| Scope-rejected calls do **not** consume quota (403 before accounting) | ✅ |
| Daily token budget enforcement (42912 with generic body — no internals leaked) | ✅ |
| Token accounting: only successful provider calls bill tokens | ✅ (verified via `/v1/ai/usage`) |
| Streaming citations: invented `[n]` markers stripped; only context message IDs resolvable | ✅ |
| Planner output schema enforcement: non-JSON model output → 500 to caller, nothing persisted/applied | ✅ |
| Plans are never applied server-side (no write endpoint consumes a plan) | ✅ |
| Publisher priority never overridden by AI; summaries/translations length-capped and control-char stripped | ✅ (unit + live) |
| Enrichment/chat/digest prompts treat message bodies as untrusted data (schema-constrained output; adversarial corpora in unit tests) | ✅ |
| AI config (`ai-base-url`, API keys) is operator-only; users cannot influence provider endpoints | ✅ |
| Secrets (API keys, token values, passwords) never appear in logs | ✅ |

### Deployment hardening (applied during this audit)
- `/var/lib/axon` → `0750 axon:axon`; `UMask=0077` in the unit so DBs are created `0600`
  (was `0644` — **fixed**, see F-2).
- systemd unit hardened: `ProtectSystem=strict`, `PrivateDevices`, `ProtectKernel*`,
  `ProtectClock`, `ProtectHostname`, `RestrictSUIDSGID`, `RestrictNamespaces`,
  `LockPersonality`, `MemoryDenyWriteExecute`, `RestrictRealtime`, `CapabilityBoundingSet=`,
  `RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX`, `SystemCallFilter=@system-service`,
  `NoNewPrivileges`, `PrivateTmp`. Service verified healthy after restart.
- Config file `/etc/axon/server.yml` is `0640 root:axon` (API keys not world-readable).

## Findings

| ID | Severity | Finding | Disposition |
|----|----------|---------|-------------|
| F-1 | **Medium** | `Access-Control-Allow-Origin: *` (upstream default) and no CSP/X-Frame-Options/X-Content-Type-Options headers | Mitigated: API auth uses headers (not cookies), so CSRF/origin risk is low. **Recommended:** set `access-control-allow-origin: "https://ntfy.example.com"` and add CSP/X-Frame-Options/X-Content-Type-Options at the Caddy layer (config snippet below) |
| F-2 | **Medium** | Auth DB (`user.db`) created world-readable (`0644`) — bcrypt hashes + topics readable by other local users on a shared host | **Fixed:** `UMask=0077` in the unit + `chmod 750 /var/lib/axon` |
| F-3 | **Low** | `POST /v1/ai/plan` is available anonymously (by design) — 10/day/visitor quota + token budget + response cache bound the abuse ceiling, but a determined attacker can still burn the global daily budget from many IPs | Accepted for a personal instance. If needed: require auth for plan (route change, one line) or front with Caddy rate limiting |
| F-4 | **Low** | `enable-signup: true` on a public instance lets anyone create accounts and consume AI budget from their IP | Personal-instance tradeoff. Recommend closing signup once your own account exists (`enable-signup: false`) |
| F-5 | **Info** | Prompt injection via notification bodies is possible **by construction** (attacker-controlled content reaches the LLM) | Defense-in-depth enforced server-side and tested: schema-constrained outputs, length caps, control-char stripping, citation allow-lists, no tool execution from model output, destructive actions require human review. Residual risk: a successfully-injected model could produce a *wrong summary/answer* — it cannot execute actions or exfiltrate beyond the prompted window |
| F-6 | **Info** | Deployment origin: ensure DNS for your hostname matches the actual origin, and that a static IP / DDNS is used if running on a dynamic connection | Documented; operator-specific infrastructure details intentionally omitted from this public report |

## Addendum — post-audit additions (re-verified)

Features shipped after the initial audit, with their security posture:

- **MCP over HTTP** (`POST /mcp`, gated by `enable-mcp`): JSON-RPC transport for AI agents.
  - Rate limited by the per-visitor `limitRequests` middleware (audit F-B: this
    wrapping was added later — the original claim that it always was rate-limited was wrong)
  - Batches are capped at 10 JSON-RPC messages per request; the caller's resolved
    IP is forwarded on inner self-calls so traffic and ban strikes attribute to the
    real caller, not the server/proxy bucket
  - Per-request auth: tools run with the **caller's** Authorization header; the server's
    configured token is never exposed to HTTP callers (stdio mode uses it locally)
  - Without credentials on a deny-all server, every tool call fails at the ACL layer
  - Topic `mcp` is reserved (disallowed list) to avoid endpoint/topic collision
  - Batch JSON-RPC supported; notification-only requests return 202 with no body
- **Timezone-aware briefings**: IANA timezone strings validated server-side
  (`time.LoadLocation`), stored per-user, scheduler evaluates local hour/day
- **Embedding-backed retrieval** (`ai-embeddings-model`): embeddings cached in memory
  (5,000-entry LRU, keyed by normalized text); provider failures degrade to keyword-only
- **Correlation** (`buildClusterHints`): embedding similarity only shapes the digest
  prompt; failure returns empty hints (plain summarization)

## VPS deployment checklist

1. [ ] Run the bundle installer (`./install.sh`) — creates `axon` system user, hardened unit included
2. [ ] Create admin: `axon user add --config=/etc/axon/server.yml --role=admin <name>`
3. [ ] Caddy site block:
   ```caddy
   ntfy.example.com {
       reverse_proxy 127.0.0.1:2587
       header {
           X-Content-Type-Options nosniff
           X-Frame-Options DENY
           Referrer-Policy no-referrer
           Strict-Transport-Security "max-age=31536000; includeSubDomains"
       }
   }
   ```
   (Caddy issues/renews the Let's Encrypt certificate automatically)
4. [ ] After your own account exists: set `enable-signup: false` in `/etc/axon/server.yml`, restart
5. [ ] Enable AI deliberately: Ollama on the VPS (`ollama pull llama3.1`) or a hosted endpoint; then `systemctl restart axon`
6. [ ] Optional: `axon webpush keys` + web-push config for phone delivery without the ntfy apps
7. [ ] Backups: `/var/lib/axon/` (cache, auth DB, attachments) — the auth DB holds password hashes
8. [ ] Keep SSH key-only auth on the VPS; consider fail2ban (axon ships a ban-feed:
   `ban-file` + fail2ban integration for abusive visitors, upstream feature)

## Threat-model notes (AI layer)

- **What can leave the server:** only the explicitly enabled feature's prompt content (summaries,
  chat windows, digest corpora), to the configured provider. With Ollama (local), nothing leaves
  the machine. Every outbound call is logged locally with feature/model/token counts.
- **What a prompt injection can do:** at worst, produce a misleading summary/title/answer that a
  human reads. It cannot: publish as someone else, read topics outside the requesting user's
  **ACL-authorized** subscriptions (audited at read time, not at subscribe time — see
  Addendum 3 / F-A; the subscription list itself is client-writable and was never a
  boundary), call tools, persist instructions, or exceed the validated output schemas.
- **Rate/budget ceilings:** 10 AI requests/day/visitor, 20k tokens/day/visitor, 2M tokens/day
  server-wide (defaults), plus upstream's per-visitor request limits on every endpoint.

## Addendum 2 — external review response (audit triage)

An independent code review of the fork confirmed the audit above and raised two new
low-severity findings, both fixed:

- **Docker image ran as root** → the image now creates an unprivileged `ntfy` user
  and drops privileges via `USER`. The binary keeps `cap_net_bind_service` so the
  default `:80` listen address still works (verified: container serves as uid 100).
  Operators bind-mounting cache/config directories from the host must `chown` them to
  the image's `ntfy` uid/gid (or run with `--user`); rootless runtimes that ignore
  file capabilities should publish to an unprivileged container port instead.
- **Checkout-success callback unthrottled** (upstream `FIXME`) → the Stripe
  checkout-success handler now applies a dedicated per-IP sliding-window rate limit
  (10 lookups/hour) inside the handler, before any Stripe API call. Deliberately *not*
  wired into the shared per-visitor request limiter: that limiter's exact accounting is
  pinned by upstream tests, and enumeration of this endpoint is bounded equally well by
  an independent budget.

The same review verified (no issues found): bcrypt comparison timing, attachment ID
regex anchoring, the `html/template` fix (GHSA-rhwf-xgc9-m9fp), the webpush SSRF
guard's anchored regex (GHSA-w9hq-5jg7-q4j7), email header-injection closure, Stripe
webhook signature verification, MCP tools running with caller credentials only,
fully parameterized SQL, no path traversal, no hardcoded secrets, no frontend XSS
sinks, and no IDOR paths. Its `CORS *` and prompt-injection remarks match findings
F-1 / F-5 above.

`govulncheck ./...` (with symbol analysis): **0 callable vulnerabilities**.
`golang.org/x/crypto` was upgraded v0.55.0 → v0.56.0 for GO-2026-6355 and
GO-2026-6354 (module-level findings). The one remaining module-level advisory,
GO-2026-5932, flags `x/crypto/openpgp` as unmaintained-by-design; no fixed version
exists and this codebase never imports `openpgp`.


## Addendum 3 — second external review (AI-layer and MCP focus)

A follow-up code review found one High and one Medium issue, both fixed:

- **F-A (High) — AI endpoints enforced the subscription list, not the ACL.** The
  synced subscription list is client-writable (`POST /v1/account/subscription`
  accepts any topic), and chat/digest/briefing read the message cache directly
  after only checking membership in that list. On a deny-all server, any
  authenticated user could read any topic's cached messages by subscribing to it
  and asking the chat endpoint to "cite every message". **Fixed:** all four AI
  handlers (chat, chat-stream, digest, briefing) and the scheduled-briefing
  collector now call `userManager.Authorize(u, topic, PermissionRead)` at read
  time — 403 for explicit-topic requests, silent skip in cross-topic loops and
  the scheduler. Admins and topic owners are unaffected. Regression test:
  `TestServer_AI_ACLs_EnforcedAtReadTime` (the reviewer's exact repro: user B
  subscribes to A's topic → chat 403, digest 403, briefing excludes).
- **F-B (Medium) — `/mcp` was unmetered and misattributed.** The outer request
  had no rate limiter, and inner self-calls carried no caller identity, so all
  MCP users shared the server's own rate-limit bucket and could accrue ban-feed
  strikes against the proxy IP. **Fixed:** the route is wrapped in
  `limitRequests`; batches are capped at 10 requests; the resolved visitor IP
  is forwarded (server-set, not client-spoofable) on inner calls for
  attribution. Residual: if `BaseURL` resolves through a proxy, final
  attribution depends on the proxy appending the hop correctly (ours does).
- **Low — MCP `read_messages` `since` was interpolated unescaped** into the
  inner request path. Now validated (`all` | duration | unix ts | message ID)
  and `url.QueryEscape`d.
- **Info — `handleAIBriefing` read `len(u.Prefs.Subscriptions)` before its nil
  guard.** Unreachable in practice (Prefs is always initialized); reordered.
- **Low (accepted) — `/metrics` on the main listener has no auth** when
  `enable-metrics` is set without `metrics-listen-http` (upstream behavior).
  This instance does not enable metrics; if you do, bind a separate
  metrics listener or block `/metrics` at the proxy.
- **Info (accepted, upstream) — Twilio and default S3 clients use
  `http.DefaultClient`** (no timeout). Not exercised in this deployment.

The same review confirmed: admin routes all pass `ensureAdmin` (nil-safe),
`handleUsersUpdate`/`handleUsersDelete` refuse privileged mutations, no
`InsecureSkipVerify` anywhere, no secrets in S3/AI provider logging, and the
web frontend renders markdown through react-remark without raw HTML.
