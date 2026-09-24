# Phase 2 — AI subscription setup (the headline feature)

**Depends on:** Phase 1 · **Size:** M (3–4 weeks) · **Status:** ☐ Not started

**Goal:** describe what you care about in plain language; the assistant turns it into a concrete, reviewable subscription plan (topics, filters, priorities, publisher wiring) that you apply with one click. This is the "app connecting to AI to set up subscriptions" core of the fork.

**Examples the phase must nail:**
- *"Notify me when my GitHub Actions fail"* → plan: subscribe `myuser-ci` on this server + filter `priority >= 4` (or title match `failed`), plus copy-paste workflow step that publishes to the topic on failure.
- *"Page me for prod alerts at night, ignore the noise"* → plan: subscribe `prod-alerts`, min-priority 4, mute schedule 09:00–18:00 (quiet hours inverted), justification for each knob.
- *"I pasted this URL: https://example.com/feeds — can I get alerts?"* → explain ntfy is push, not pull; suggest a bridge script or an existing integration topic; optionally subscribe to a known public topic.

## Server: `POST /v1/ai/plan` (in `server/server_ai.go`)

Request:
```json
{ "prompt": "notify me when github actions fail on my repo",
  "context": { "existingTopics": ["ci", "prod"], "locale": "en" } }
```

Response — a strictly schema-validated plan (validated server-side; regenerate once on schema violation, then fail):
```json
{ "subscriptions": [{
    "baseUrl": "https://my-axon.example.com",
    "topic": "myuser-ci",
    "displayName": "CI failures",
    "filters": { "search": "failed|error", "minPriority": 3 },
    "mutedUntil": null, "schedule": null,
    "justification": "You asked to be notified on failures; search filter keeps green builds silent."
  }],
  "publisherInstructions": "Add this step to your workflow:\ncurl -d \"build failed: $JOB ($STATUS)\" -H \"Priority: 4\" https://my-axon.example.com/myuser-ci",
  "followUpQuestions": ["Do you also want PR-review reminders?"],
  "disclaimer": "AI-generated — review before applying." }
```

Semantics:
- **Plan only.** Applying the plan reuses the *existing* endpoints untouched: web app calls `POST /v1/account/subscription` (`server/server_account.go:428`, stored in `user.prefs` — no schema migration) for logged-in users, and writes the local Dexie row for anonymous users, exactly like `subscribeTopic()` in `web/src/components/SubscribeDialog.jsx:36` does today.
- Anonymous visitors: planner allowed (rate-limited harder), but plans are applied client-side only.
- Telemetry (local only): log prompt-hash, plan validity, which fields the user edited before applying — the improvement signal for prompts.
- Budget: planner calls count against Phase 1's visitor budget; hard cap ~10 plans/day/visitor.

Planner prompt design (`ai/planner.go` + golden files in `ai/testdata/prompts/plan/`):
- System prompt embeds a **capability map** of ntfy concepts: topics (no creation needed, just use), auth/reserved topics, priorities 1–5, tags, message filters (`?poll=1`, `since`, search filters server-side via `queryFilter` in `server/types.go`), federation (`upstream-base-url`), what ntfy **cannot** do (no RSS polling of arbitrary URLs) so it stops hallucinating features.
- Temperature 0, strict JSON schema, few-shot pairs for the canonical use cases (CI, server monitoring via existing tools, home automation, ntfy's own docs examples in `docs/examples/`).
- Injection defense: user prompt is the only instruction channel; publisher/content examples inside the prompt are quoted as data. (Cross-cutting rules apply.)

Second endpoint: `POST /v1/ai/tune` — same machinery, but input is an *existing* subscription + a goal ("only page me at night", "less noisy") → returns a patch of `{filters, mutedUntil, schedule, displayName}` applied via the existing `PATCH /v1/account/subscription` (`server/server_account.go:451`).

## Web app

- `web/src/components/AiSubscribeDialog.jsx` — a "Describe it" tab inside the existing `SubscribeDialog.jsx` flow:
  1. textarea + suggested starters; optional 1–2 round refinement chat (stateless; last message + accumulated plan);
  2. **plan review card**: each subscription as editable chips (topic, filters, priority), publisher instructions with copy button, per-item justification;
  3. Apply → existing subscribe path (below the dialog, nothing new); confirmation toast with undo (delete subscription).
- `web/src/components/SubscriptionPopup.jsx`: "AI tune this subscription…" menu item → tune dialog (same review-card pattern).
- Gating: `enable_ai` flag from `configResponse` (`server/server_web.go:76`, surfaced to the app via `/config.js`) — hidden entirely when the server has no provider.
- i18n: all strings through i18next from day one (`web/public/static/langs/`, start with `en`, stub others).
- Anonymous users: assistant visible but plans apply locally (Dexie) — mirroring how anonymous subscribe works today.

## Tests
- Planner: golden-file tests with mock provider — schema validation, refusal path ("ntfy can't poll RSS"), no auto-subscribe (server has no write endpoint for plans — assert only `plan` exists).
- Web: vitest for plan-review card rendering, apply flow calling existing managers, feature-flag gating.
- Adversarial: prompts attempting to steer the planner into exfiltrating context or inventing topics on other base URLs — plan schema pins `baseUrl` to the requesting origin unless the user typed another one.

## Acceptance criteria
- A new user goes from "I have a GitHub repo" to a working, filtered CI-failure subscription in under a minute, AI-assisted end to end.
- Zero writes happen server-side from the planner; only existing subscription endpoints mutate state.
- Planner refusal quality: 90%+ of "impossible" asks (RSS polling, email ingestion without setup) produce an honest explanation with the closest real alternative.
- Cost: p95 plan ≤ ~2k output tokens; cache hit on repeat prompts.

## Risks
- Hallucinated capabilities → mitigation is the capability map + golden refusal tests.
- Editor friction: if the plan card is clumsy, users fall back to manual add — usability-test the review card with 3–5 real configs (CI, Grafana, Home Assistant, uptime tools) before shipping.
