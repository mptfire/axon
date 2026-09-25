# Phase 4 — Digests & briefings

**Depends on:** Phase 3 (correlation input; per-subscription AI settings UI) · **Size:** M (2–3 weeks) · **Status:** ✅ Done (`POST /v1/ai/digest`, `POST /v1/ai/briefing`, scheduled daily briefings via `prefs.digest`: minute scheduler, private `dg_*` delivery topics with read ACL + auto-subscription, quiet-day skip, last-daily dedup, Preferences UI. Timezone-aware delivery shipped (IANA zones, embedded tzdata). Remaining idea: per-topic briefing inclusion toggles

**Goal:** scheduled, AI-written digests per user — "morning briefing" and on-demand rollups — so noisy topics can be muted entirely without fear of missing what matters.

## Mechanics (all patterns already exist upstream)

- **Scheduler:** reuse the periodic manager loop (`runManager`/`execManager`, `server/server.go:1919` + `server/server_manager.go`) and the delayed-sender pattern (`runDelayedSender`, `server/server.go:1989`) — add an `aiDigestRunner` on the same cadence that computes per-user due digests.
- **Corpus:** for each user, `messageCache.MessagesCapped` over their subscribed topics (subscriptions come from `user.prefs`) since the last digest, pre-filtered server-side (time, topic, min-priority) and pre-clustered with Phase 3's `correlate` incident keys so the model sees incidents, not 200 raw alerts.
- **Delivery:** publish the digest as an ordinary message to a per-user reserved topic (`_u_<userid>_digest`, disallowed for others via the existing reservation/ACL system), which fans out through the normal paths — web, mobile push, email — for free. Digest settings sync across devices via the existing `publishSyncEventAsync` (`server/server_account.go:943`).
- On-demand: `POST /v1/ai/digest` ("summarize topic X since Monday") — same pipeline, immediate, streamed to the web app.

## User model
- Per-user digest prefs in `user.prefs`: `{enabled, schedule: "daily 08:00" | "weekly Mon 09:00", timezone, includeTopics: [...], maxTokens}`.
- Web UI in `Preferences.jsx` + a digest card renderer in the notification list (grouped, collapsible, links jump to the underlying messages).
- A digest is itself a message: it respects mute/priority rules, is searchable, and counts once against budgets.

## Prompt & output
- Input: clustered incidents (topic, count, first/last message summary, max priority, sample titles) — never full bodies unless short; strict output schema: `{headline, sections: [{title, points[], severity, topicRefs[]}]}` rendered server-side to markdown; topicRefs link back to the real messages in the web app.
- Localized to the user's locale.
- Token discipline: cap corpus size; if over, digest-of-digests (cluster summaries first).

## Config
`ai-digest-enabled` (operator), default user schedule off, `ai-digest-max-tokens` per digest, quiet-hours awareness (deliver at schedule time in user TZ; respect notification muted hours for the push itself).

## Tests
- Scheduler unit tests with a fake clock (already the pattern in manager tests).
- Golden digests: fixed corpus → fixture prompt → mock provider → rendered markdown snapshot.
- Corpus > cap ⇒ hierarchical summarization path.
- Budget: a user with 20 topics × 500 msgs/day stays within `ai-digest-max-tokens`.

## Acceptance criteria
- A user muting 5 noisy topics still gets a trustworthy 8:00 briefing with incidents, counts, and links; total added LLM cost bounded and visible on `/v1/ai/usage`.
- Digest delivery works unmodified on the Android app (it's just a topic message).
- Turning digests off stops all scheduler work for that user (no idle LLM calls).

## Risks
- Timezone/DST bugs in scheduling — use the delayed-sender's existing due-time machinery; fake-clock tests across a DST boundary.
- Hallucinated details in briefings — schema pins every point to `topicRefs`/message ids; anything unverifiable is dropped at render time (no free-text severity claims).
