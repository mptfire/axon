# Phase 0 — Foundation & fork plumbing

**Depends on:** nothing · **Size:** S (~1 week) · **Status:** ☐ Not started

**Goal:** make the fork a maintainable home: identity decided, CI green, plan integrated into the docs site, upstream-sync policy written down. **Zero behavior change** — at the end of this phase, `git diff upstream/main` contains only docs/CI/branding.

## Work items

### 0.1 Identity & branding decisions
- [x] **Product name: `axon`** (lowercase in prose/CLI contexts, "Axon" at sentence start). An axon is the
      nerve fiber that carries signals — a notification bus with a brain. Binary name, config paths
      (`/etc/ntfy/server.yml`), and the wire protocol **stay `ntfy`** for drop-in compatibility with
      upstream servers and clients; `axon` is the fork/product name (docs, README, NOTICE, docs site).
      Revisit only if we split distribution-wide (own Docker org, store listings).
- [x] Module path strategy: **keep `heckel.io/ntfy/v2`** — renaming the module touches every import and
      makes upstream merges painful; revisit only if we publish our own Go API. In-code fork edits are
      marked `// axon:` instead.
- [x] Update `README.md` (fork banner, upstream credit), `mkdocs.yml` (`site_name`), and add `NOTICE`
      (Apache-2.0 attribution) alongside `LICENSE`.
- [ ] Docker labels + image naming when release engineering lands (Phase 7).

### 0.2 Branch & sync policy
- [ ] `main` is the fork trunk; feature branches `ai/feat/<phase>-<thing>` merge via PR.
- [ ] Write `docs/ai-plan/upstream-sync.md`: quarterly merge of `upstream/main`, pre-merge checklist (re-verify seams: `dispatch()` at `server/server.go:824`, `handleInternal` router at `:563`, `configResponse` at `server/server_web.go:76`, subscription prefs handling at `server/server_account.go:428`).

### 0.3 CI
- [ ] Port upstream GitHub Actions (`test` workflow) to run on the fork: `make check` (Go tests, vitest, eslint, prettier, staticcheck) plus `make build` artifact sanity.
- [ ] Add a job that builds the Docker image (`make docker-dev` equivalent) to catch embed/site issues early.

### 0.4 Docs home
- [ ] This `docs/ai-plan/` tree added to `mkdocs.yml` nav (done together with this document) as "AI fork plan".
- [ ] `docs/ai-plan/index.md` is the living index — phase statuses updated as work lands.

### 0.5 Placeholders for later phases
- [ ] Add inert config fields (`ai-enabled: false` default) to `server/config.go` + `server/server.yml` + `docs/config.md` so later phases only fill in behavior, not plumbing. (Optional; can also land with Phase 1.)

## Acceptance criteria
- `make check` and `make build` pass on `main`.
- Diff vs `upstream/main` is docs/CI/branding only.
- Docs site renders the AI fork plan section.
- A dry-run of the upstream-merge checklist on a trivial upstream commit succeeds.

## Risks & notes
- Upstream moves fast (v2.x releases); do the first sync drill early so the process is proven before real divergence lands in Phase 3 (the only phase that edits near hot upstream code paths).
