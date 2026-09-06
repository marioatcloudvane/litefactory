# Drift Report — Router trusts the daemon-resolved mode header
# Branch: lf/69-we-need-to-make-more-defaults-apply-on | Date: 2026-09-04

## Delivered as planned
- US-1 / T-1: precondition branch inserted at the top of `CLAUDE.md`'s `## Classify` section — header present and recognized (`research`/`plan`/`implement`/`spike`, case-insensitive) trusts it and skips steps 1-3; header absent/empty/`infer` runs steps 1-3 unchanged; header present but unrecognized falls back to steps 1-3 and reports the divergence. `## Gate` explicitly stated to run unconditionally. No hedging language ("looks wrong," compare-to-request) present.
- US-1 / T-3a: verified `## Classify` steps 1-3, the "sounds like" table, and `## Gate` are byte-for-byte unchanged; `AGENTS.md` has zero changed lines.
- US-2 / T-2: comments added at both mode-header emission sites in `internal/harness/prompt.go` (`writeHeader` at ~line 480, `writeResumeHeader` at ~line 630), naming `CLAUDE.md`'s `## Classify` section as a consumer of the exact `"- Mode: %s\n"` literal, explaining the mechanism and drift consequence, and naming the existing test pins without overstating the guarantee.
- US-2 / T-3b: confirmed comment-only diff (no executable line added/removed/reordered), `gofmt -l .` silent, `go build ./...` and full `go test ./...` green (974 passed, 0 failed), matching pre-edit state.
- All AC-1 through AC-11 verified by the code reviewer and/or the unit test verification pass.

## Added scope
None. Both stories were implemented exactly to their task lists; no extra files, endpoints, or logic were introduced.

## Dropped or deferred
None. Both stories' full task lists (T-1, T-2, T-3a, T-3b) were completed.

## Architectural deviations
None. `AGENTS.md` was correctly left untouched per the plan's Shared Decision that it defers unconditionally to `CLAUDE.md`. No ring-fenced file (`internal/daemon/mode.go`, `internal/daemon/agent.go`, `internal/daemon/tools.go`, `tick.go`'s mode-label write-back, any config key, `## Gate`'s own text) was touched. The stale line-number caveat from the post-merge re-verification was handled correctly — both implementation engineers located their targets by content (heading text, function names, the `"- Mode: %s\n"` literal) rather than trusting the cited line numbers, and the actual current locations (line 480 and line 630 in `prompt.go`) were confirmed and used.

## Verdict
CLEAN — delivered matches plan within acceptable micro-decisions (comment placement style, exact wording of the unrecognized-header report clause — noted as advisory CR-A-002, non-blocking).
