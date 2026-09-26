# Phase 7 — Mobile apps, hardening & 1.0 release

**Depends on:** Phases 2–4 shipped (settings/data model stable) · **Size:** L (4–8 weeks, partly parallelizable) · **Status:** 🔧 Release slice done: tag-triggered release automation (static amd64+arm64 binaries auto-attached to GitHub Releases), first releases shipped (v2.28.0-axon.1/.2). Open: mobile apps, goreleaser expansion

**Goal:** bring the AI experience to the mobile apps (separate repos), pass a security/privacy review, and ship a coherent 1.0.

## 7.1 Mobile (forks of ntfy-android / ntfy-ios — separate repositories)

Constraint: **wire protocol unchanged** (see `server/server.go:83` — the path/protocol constants are shared contract with the apps; all fork features are additive fields/endpoints the apps already ignore or will newly read).

- [ ] Fork both apps; AI settings screen: per-subscription toggles for Phase 3 features + digest schedule — **no on-device model calls**; settings sync via account `prefs` (already synced cross-device through `publishSyncEventAsync`, `server/server_account.go:943`), so the server remains the only AI execution point. This keeps mobile cheap, private, and battery-friendly.
- [ ] Digest rendering: digests are ordinary topic messages (Phase 4) — mobile gets them for free; add the collapsible digest card styling.
- [ ] Revisions (Phase 3 summaries): additive `revision` field — apps show latest revision for known ids; graceful ignore on old app versions.
- [ ] Rebrand pass (name/icons/store listings) per Phase 0 identity; GPLv2 obligations respected for the Android app (source published).

## 7.2 Hardening (server & web)

- [ ] Security review with threat model covering: prompt injection end-to-end (Phases 2/3/5 corpora), token/key handling (`ai-api-key-file`, no secrets in logs), scoped-token bypass, budget bypass via anonymous planner, MCP abuse, SSRF via `ai-base-url` misconfig, timing attacks on rate-limit responses.
- [ ] Privacy review vs Principles: default-off everywhere; audit log completeness (every external call logged); data-flow diagram published in `docs/privacy-ai.md`; "what leaves my server" section per feature.
- [ ] Load/soak: publish storm (10k msg/min) with enrichment on — verify pass-through under provider outage, no unbounded queues, memory stable; publish the benchmark numbers.
- [ ] Fuzz the schema validators (model output, plan JSON) with go-fuzz-style corpus.

## 7.3 Release engineering

- [ ] Versioning: fork version scheme `v2.x.y-axon.N` tracking upstream's base (honest compatibility signal).
- [ ] Release artifacts: binaries (goreleaser configs exist), Docker images (multi-arch, matching upstream Dockerfile set), docs site deploy.
- [ ] `docs/ai-plan/*` statuses finalized; user-facing docs: `docs/config.md` AI options table complete, `docs/agents.md`, quickstart "AI in 5 minutes with Ollama".
- [ ] Comms: comparison page vs upstream ntfy (fork is a superset; credit prominently), migration notes (upstream server ⇒ axon is a drop-in binary swap with AI off).

## Acceptance criteria
- Unmodified upstream Android/iOS apps work against the axon server (compat suite green); forked apps pass store review with AI features on.
- Security review findings all closed or accepted-with-mitigation in writing.
- 1.0 images published; a fresh user can go `docker compose up` + Ollama + phone app + AI subscription wizard in one documented sitting.

## Risks
- Store review friction for forked apps (duplication policies) — mitigated by clear differentiation and upstream-relationship documentation.
- Mobile repos are Swift/Kotlin codebases new to the fork team — scope mobile strictly to settings + rendering; no core app rewrites.
