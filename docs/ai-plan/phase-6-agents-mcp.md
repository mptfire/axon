# Phase 6 — Agents & MCP (nfty as the AI-native transport)

**Depends on:** Phase 1 (provider layer), Phase 2 (planner reuse) · **Size:** M (3 weeks) · **Status:** ☐ Not started

**Goal:** flip the direction — instead of humans using AI to manage notifications, let AI agents (Claude, GPTs, IDE agents, scripts) use nfty as their notification bus. nfty becomes the push layer agents already know how to speak: MCP.

## Deliverable 1: `ntfy mcp` — a Model Context Protocol server

New subcommand `ntfy mcp` (`cmd/mcp.go`), stdio transport for local agents (plus SSE streamable HTTP for remote, same tool set):

| Tool | Maps to | Notes |
|---|---|---|
| `publish` | existing `PUT /<topic>` | topic, title, body, priority, tags, actions; supports scheduled delivery |
| `subscribe_wait` | `GET /<topic>/json?since=…&poll=1` + long-poll with timeout | blocking wait for the next message on a topic — the primitive agents lack |
| `list_subscriptions` | account subscriptions from `user.prefs` | |
| `plan_subscription` | Phase 2 planner | NL → plan → **confirm flag required** to apply (Principle 2 holds for agents too) |
| `search_history` | Phase 5 retrieval | scoped to the token's user |

Auth: ntfy user tokens (`ntfy token create`) passed via env/config — Bearer on the HTTP API; the MCP layer is a thin client of the same public API (no server internals), so it inherits rate limits, ACLs and budgets for free. The server-side addition is only: a scoped-token capability (`read`, `publish`, `ai`) — extend `user.Token` with a scopes column (additive schema migration, following upstream's migration patterns in `user/manager_sqlite.go`).

## Deliverable 2: agent-friendly publishing conveniences (server, small)
- `x-actions` already exist upstream; document agent recipes (approve/reject buttons → callback topics).
- First-class recipe docs: "agent → ntfy → human phone" pattern: agent publishes priority-5 with actions; human reply publishes back to a per-conversation topic the agent `subscribe_wait`s on. This closes the loop for human-in-the-loop agent workflows — nfty's unique angle vs. email/webhook.

## Deliverable 3: docs
- `docs/agents.md` (mkdocs nav): quickstarts for Claude Desktop/Code, generic MCP clients, plain curl agents; scoping and safety guidance.

## Guardrails
- Scoped tokens: default least-privilege; `ai` scope required for planner/history tools.
- Rate limits: agents hit the same `visitor` limits as everyone (per-token attribution, not per-IP — extend visitor keying to token id).
- `subscribe_wait` bounded (max wait, max message size) to avoid pinning connections.

## Tests
- MCP contract tests against a scripted in-process server (tool schema, auth rejection, scope enforcement).
- End-to-end happy path in CI with a mock agent: publish → human action reply → agent receives on callback topic.
- Load: 100 concurrent `subscribe_wait` connections on a busy topic (reuse existing test server harness in `test/server.go`).

## Acceptance criteria
- A Claude Desktop user adds nfty via MCP config and, without reading API docs, sets up a monitored subscription and receives a pushed notification on their phone.
- A headless agent script completes the approve/reject loop (publish with actions → human taps → agent consumes reply).
- Tokens without `ai` scope cannot reach planner/history tools (negative tests).

## Risks
- MCP spec evolution — pin a protocol version; contract tests make breakage loud.
- Agents spamming topics — same rate-limit story as humans; monitor via Prometheus and tune defaults.
