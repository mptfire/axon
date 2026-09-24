# Upstream sync policy

The fork trunk is `ai/main`. Upstream is the `upstream` remote pointing at
[binwiederhier/ntfy](https://github.com/binwiederhier/ntfy).

## Cadence

Merge `upstream/main` into `ai/main` **quarterly**, or immediately for security
releases affecting `server/`, `user/`, or `message/`.

```bash
git fetch upstream
git merge upstream/main        # resolve conflicts, keep // axon: markers intact
make check && make build       # full gate before pushing
```

## Pre-merge checklist

Upstream may move the lines our AI layer hooks into. Re-verify each seam after
every merge (grep first, line numbers drift):

- [ ] `dispatch()` — "single choke point" comment, `server/server.go` (was `:824`)
- [ ] `handleInternal` router + path regexes, `server/server.go` (was `:563`, `:84`)
- [ ] `configResponse` / `apiConfigResponse` (web feature flags), `server/server_web.go` (was `:76`)
- [ ] Subscription account endpoints (`/v1/account/subscription`), `server/server_account.go` (was `:428`)
- [ ] `topic.Publish` fan-out semantics (shared `*model.Message` pointer), `server/topic.go` (was `:107`)
- [ ] `user.prefs` subscription JSON handling, `user/manager_sqlite.go`
- [ ] Manager/delayed-sender loops (`runManager`, `runDelayedSender`), `server/server.go`
- [ ] `topicPathRegex` still in sync with the mobile apps (see warning at top of the regex list)

## Conflict policy

- New-code-first: AI features live in `ai/`, `server/server_ai*.go`, `server/server_mcp*.go`,
  `web/src/components/Ai*` — these should never conflict.
- In-file edits inside upstream code are marked `// axon:` and kept minimal.
- Never reorder or reformat upstream code opportunistically; keep diffs surgical.
- The Go module path stays `heckel.io/ntfy/v2` (see plan Phase 0).

## After every merge

- Run the full gate: `make check`, `make build`, docker build.
- Re-run the mobile compatibility sanity test (unmodified upstream Android app
  against a dev server) before publishing images.
