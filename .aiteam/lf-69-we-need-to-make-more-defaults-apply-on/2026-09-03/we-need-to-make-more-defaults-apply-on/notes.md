# Implementation Notes — issue #69 (US-1, US-2)

## Stale line numbers (pre-implementation caveat)
`RESEARCH_BRIEF.md`'s post-merge re-verification section flagged that the line
numbers cited in `US-2.md` (`prompt.go:423`, `:566`) were stale after `main` was
merged into this branch (attachment/document-handling code shifted things down
~50-70 lines). The US-2 implementation engineer was instructed to locate the two
target sites (`writeHeader`, `writeResumeHeader`) by function name and by the
literal `"- Mode: %s\n"` rather than by any cited line number. It confirmed the
actual current locations are line 480 (`writeHeader` emission) and line 630
(`writeResumeHeader` emission), and placed comments accordingly.

## Code Review Advisories (ADVISORY_ONLY verdict, non-blocking)
- **CR-A-001**: Both new comments in `prompt.go` cite `(AC-10)` against sentences
  that actually describe AC-9/AC-11 content (the "no test catches CLAUDE.md-side
  drift" property); AC-10 is the build-cleanliness criterion, verified externally
  rather than by comment prose. Citation mismatch only, not a substantive gap.
  Not fixed — cosmetic, does not affect correctness or the guarantee described.
- **CR-A-002**: `CLAUDE.md`'s unrecognized-header branch says to "note... that an
  unrecognized mode header was seen and ignored" rather than explicitly requiring
  the actual value be reported, which is a slightly softer reading than AC-3's
  "report the unrecognized value" phrasing. Doesn't affect the fallback-safety
  guarantee (steps 1-3 still run in this case regardless). Not fixed — left as
  written since the AC's substantive safety property (fallback runs, no invented
  mode) is met.

Full detail in `CODE_REVIEW.md` in this session directory.

## Environment / testing
Both stories are documentation/comment-only with no Go behavior change and no
real dependencies (no database, no external HTTP call, no message queue) — Phase
2 (environment setup) was skipped per the orchestrator's own rule, and no
integration test suite was invoked.
