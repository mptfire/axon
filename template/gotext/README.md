# `template/gotext/` -- vendored `text/template` with an execution deadline

This directory is a **verbatim copy of Go's standard-library `text/template` package**, plus one
small patch that adds a wall-clock execution deadline. It exists for exactly one reason: to stop
**user-supplied** message templates (`Template: yes`, see the [templating docs](https://ntfy.sh/docs/publish/#message-templating))
from burning CPU.

- **Source:** Go stdlib `text/template` (+ `internal/fmtsort`), `$(go env GOROOT)/src`
- **Version:** pinned in the repo-root [`.go-version`](../../.go-version); recorded in `GENERATED_FROM`
- **Local modifications:** `patches/` (see [The patch](#the-patch))
- **Update mechanism:** Manual -- `make update-template`, then commit (never autorolled; see [Updating](#updating-when-bumping-the-go-toolchain))

## Why this exists

ntfy lets users send a Go template that is rendered against a JSON body. Go's `text/template`
**cannot be interrupted mid-execution** -- there is no context, no deadline, no cancellation
([golang/go#31107](https://github.com/golang/go/issues/31107) was declined). So a crafted template
with a tight or nested `{{range}}` (e.g. ranging over a large JSON array with a big loop body that
writes no output) can run for tens of seconds on a single request. That is a CPU denial of service
(GHSA-rhwf-xgc9-m9fp).

There is no way to add an interrupt from the outside -- the executor's per-node `walk` loop is
unexported. The only robust fix is to patch the executor itself. Rather than reach for fragile
heuristics (guessing iteration counts, wrapping every function, etc.), we vendor the package and add
a **single check inside `walk`**: every ~256 nodes it checks a wall-clock deadline and aborts (via
the normal `ExecError` path) if it has passed. This bounds CPU for *any* template shape -- cheap
loops and expensive functions alike -- by construction.

The one user-facing execution site (`server/server_template.go` `renderTemplate`) sets the deadline
with `SetExecutionDeadline` and maps the resulting error to a `400`. Trusted templates (operator
config: Twilio, `cmd/serve.go`) keep using the standard library -- they are not user-supplied.

## What's here

| File | Origin |
|------|--------|
| `exec.go`, `funcs.go`, `template.go`, `option.go` | verbatim from `$(go env GOROOT)/src/text/template/` |
| `fmtsort/sort.go` | verbatim from `$(go env GOROOT)/src/internal/fmtsort/` -- `exec.go` needs it, and `internal/...` packages can't be imported from outside GOROOT, so it comes along |
| `patches/0001-exec-deadline.patch` | our only change (see below) |
| `GENERATED_FROM` | the exact Go version `make update-template` last regenerated this copy from; provenance, written by that target |

The Go toolchain version this copy is pinned to lives in the repo-root [`.go-version`](../../.go-version)
file (the single source of truth, also consumed by CI and the `make` targets below). `GENERATED_FROM`
must equal it -- `make check` fails otherwise (see below).

We do **not** vendor `text/template/parse` -- it's a normal importable stdlib package and stays a
plain import.

## The patch

`patches/` is a quilt-style ordered series (apply `0001-*`, then `0002-*`, ...). Today there is just
`0001-exec-deadline.patch`, which is small and purely additive:

- renames the package to `gotext` and rewrites the `internal/fmtsort` import to `heckel.io/ntfy/v2/template/gotext/fmtsort`
- adds `deadline`/`steps` fields to the executor `state` and a `deadline` field + a
  `SetExecutionDeadline(time.Time)` method on `Template`
- adds the amortized deadline check at the top of `state.walk`
- adds the exported sentinel `ErrExecutionInterrupted` (detect with `errors.Is`)

Keeping the patch tiny and additive is deliberate: it makes re-basing onto a new Go release cheap.

## Updating (when bumping the Go toolchain)

The copy is **pinned to the Go version in the root `.go-version`**, so it's not frozen -- re-syncing
on a Go bump pulls in all upstream fixes for free. `.go-version` is authoritative and hand-edited; to
bump the toolchain: edit `.go-version`, install that toolchain
(`go install golang.org/dl/<version>@latest && <version> download`), then re-sync:

```
make update-template   # copies the files from your GOROOT and re-applies patches/*.patch
```

`make update-template` **errors** unless your local Go matches `.go-version` -- it validates against
the pin, it never writes it. If the patch hunks no longer apply against the new release, refresh the
patch as part of the bump.

`make template-check` (wired into `make check`) has two layers:

1. **Marker check (ungated, runs on any toolchain):** fails if `GENERATED_FROM` != `.go-version`, i.e.
   someone bumped the pin but forgot `make update-template` (or vice versa). This catches the common
   mistake locally, on any developer's Go.
2. **Content check (gated to the pinned Go):** re-derives the copy from `GOROOT + patches` and diffs it
   against what's committed, catching hand-edits and patch problems. It no-ops on a non-pinned
   toolchain so it never fails spuriously.

CI installs exactly `.go-version` (`go-version-file`), so both layers run there. `make release`
additionally refuses to run off the pinned Go, so the content check is never skipped for a release.

## License

These files are copyright The Go Authors, under the BSD-3-Clause license (headers preserved in each
file). That is compatible with ntfy's Apache-2.0 / GPLv2 licensing.
