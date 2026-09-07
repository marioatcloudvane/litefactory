# Code Review
# Feature: how-to-merge-pull-manage-conflicts
# Reviewed by: go-code-reviewer
# Date: 2026-09-07
# Status: CLEAN

---

## Scope

This review covers only US-1 ("Measure behind/ahead before every run"):

- `internal/git/merge.go`: `Divergence` struct, `(*Worktree).Divergence(ctx, base)`, `parseDivergenceCounts`.
- `internal/git/exec.go`: `divergenceTimeout` const.
- `internal/turn/prepare.go` (~lines 456-469): `syncBase`'s call to `worktree.Divergence(ctx, baseRef)`.

`MergePreview`, `MergeBase`, `recoverFromFailedMerge`, `MergeOptions`, `MergeResult`
and all other US-2 content in the same `merge.go` file were read only far enough
to confirm they do not interact with the `Divergence` code path, and are
otherwise explicitly out of scope per the review brief — no findings recorded
against them.

---

## BLOCKING findings

None.

---

## ADVISORY findings

None.

---

## Verification performed

**AC-1.1 (ahead/behind ordering).** `rangeSpec := base + "..." + w.branch` puts
`base` on the left and `w.branch` on the right of the symmetric-difference
range. For `git rev-list --left-right --count A...B`, the left count is
commits reachable only from `A`, the right count only from `B`. With
`A=base, B=branch`: left = commits only in base = what branch is missing =
`Behind`; right = commits only in branch = `Ahead`. `parseDivergenceCounts`
returns `(behind=fields[0], ahead=fields[1])` and the call site constructs
`Divergence{Ahead: ahead, Behind: behind, ...}` — the assignment is not
swapped. This matches the architect's explicit watch-out and the engineer's
independently-reported empirical verification. No test exists yet for this
(expected — a test engineer runs after this review), but the static trace is
unambiguous.

**S-1 (ref-name screening).** `checkName("base", base)` and
`checkName("branch", w.branch)` both run before `rangeSpec` is assembled, and
`rangeSpec` is built from those two already-screened values entirely inside
`internal/git` — no pre-joined range can reach this method from a caller.
`--end-of-options` is placed immediately before the positional `rangeSpec`
argument, exactly as `revParse` (`internal/git/clone.go:381`) places it before
its `rev` argument. This is a second, independent lock behind `checkName`, per
plan_brief.md's S-1 requirement.

**Placement (DEV-2).** `Divergence` is a method on `*git.Worktree`, not
`*git.Clone`, matching the architect's explicit constraint. It also calls
`w.check()` first, consistent with the package's `ErrUninitialized` guard
pattern used elsewhere on `*Worktree`.

**Error classification.** A missing/unresolvable `base` produces exit code
128 from `git rev-list`, classified via `classify(err, ErrRevisionNotFound,
128)` — the same sentinel and exit code `revParse` uses for the analogous
failure (`internal/git/clone.go:385`). No panic, no raw exit code surfaced.
`parseDivergenceCounts` failures (malformed output) are wrapped with
`ErrInvalidArgument` and context via `%w`, not swallowed or replaced with a
generic message.

**Timeout.** `divergenceTimeout = 30 * time.Second` in `internal/git/exec.go`
is a distinct named constant (not reused from an unrelated command), documented
with a rationale consistent with the other timeouts in that block, and wired
into the `w.runner.run(ctx, divergenceTimeout, ...)` call.

**Concurrency.** `Divergence` does no goroutine spawning, no shared mutable
state, and threads the caller-supplied `ctx` straight through to `runner.run`
(which derives its own `context.WithTimeout(ctx, timeout)` and defers
`cancel()`) — no `context.Background()` used mid-chain. Nothing to flag.

**Resource management.** No resources acquired directly by `Divergence` itself;
`runner.run` owns the process lifecycle (`cmd.WaitDelay`, buffered
stdout/stderr, deferred `cancel()`), unchanged by this method and already
covered by the package's existing exec chokepoint.

**Call-site integration (`internal/turn/prepare.go`).** `syncBase` builds
`baseRef := opts.remote() + "/" + repo.BaseBranch` and calls
`worktree.Divergence(ctx, baseRef)` as the first step inside `syncBase`, which
itself runs (via `prepareClone`) only after `fetch(ctx, opts, clone, repo,
logger)` has already completed (`prepare.go:397` runs before `ensureWorktree`
at 406 and `syncBase` at 411) — matching the "origin/<Repo.BaseBranch>, after
fetch" integration point exactly. Both `fetch` and `syncBase` run inside the
single `Options.CloneGate` acquisition taken at the top of `prepareClone`, so
no sibling fetch can land between the fetch this comparison relies on and the
`Divergence` call itself. A `Divergence` failure is wrapped with `failuref`
and reported through the same path every other `prepareClone` step uses — no
special-cased error handling that would diverge from the rest of the function.

**Build hygiene.** `go build ./...`, `go vet ./internal/git/... ./internal/turn/...`,
and `gofmt -l` against the three reviewed files all pass clean, confirming the
engineer's reported verification.

---

## Cross-story summary

US-1's `Divergence` method integrates cleanly with the existing `internal/git`
conventions (`Fetch`'s screening pattern, `revParse`'s `--end-of-options`
usage, `classify`'s sentinel/exit-code contract) and with the `syncBase` call
site being prepared for US-2. The `base...branch` ordering — the one place the
story flagged as easy to get backwards — is provably correct by static trace
of the range construction and the field assignment. No boundary risk observed
between this story's surface and the (out-of-scope) `MergePreview`/`MergeBase`
code that will consume `Divergence`'s result next.

---

## Verdict

CLEAN — No findings. Proceeding to environment setup.
