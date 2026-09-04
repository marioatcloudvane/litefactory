# Notes — Implement mode, litefactory-needs-to-accept-attachments

## Resumed run (2026-09-04)

Prior run was cut off by a rate-limit/budget timeout. Verified via git log/build/test
before resuming: US-1, US-2, US-5 already landed and committed (`85b3dc9`,
`b7aeff6`); `go build/vet/test ./...` green. US-3, US-4, US-6, US-7, US-8 had no
code yet (`internal/attach` did not exist). Resuming with the 5 remaining stories run
**sequentially** (not the default parallel Phase 1 pattern) in the plan's Workflow
order — US-6, US-3, US-7, US-4, US-8 — because all funnel through the new shared
`internal/attach` package and US-7/US-8 both touch the manifest shape, which is a
genuine file-overlap risk under parallel agents.

## US-6 — DONE, with one flagged task-sequencing deviation

Engineer built `internal/attach/{doc.go,reason.go,errors.go,rejection.go}`: the typed
`RejectionReason` vocabulary (T-6.1), the `Rejection` struct doubling as manifest-record
shape and reply-line composer (T-6.3/T-6.4), and `attachmentNameSpan` (a salvaged-prefix
filename sanitizer mirroring `internal/report`'s `commandSpan`).

**Deviation on T-6.2:** the engineer did not add a `report.ReplyKind` case or a
`internal/turn/report.go` `replyKind()` case. Reasoning given: `report.Reply`/
`RenderReply`'s doc comments and shared sentence constants are structurally reserved
for "no run happened" ("Nothing was run"), which directly contradicts AC-6.1's
requirement that the run proceeds and other attachments still ingest when one is
rejected — wiring a rejection through `report.Reply` would mislabel a turn that did
run as one that didn't. T-6.2/T-6.3's intent (a place to compose rejection text) was
instead satisfied inside `internal/attach` itself (`Rejection.Line()`/`Lines()`), as
freestanding functions with no live caller yet.

Engineer's recommendation: the actual wiring point for posting rejection text to the
issue should be `report.Run`/`RenderRun` (already carries structured non-textual run
detail via `Files []git.FileChange`), and that integration naturally belongs to US-7
(which already owns building `attachments.md` and needs the full picture to decide
correctly). **Action:** the US-7 engineer invocation below is instructed to pick up
this integration point and to reply-post via `report.Run`/`RenderRun`, not via a new
`report.ReplyKind` case. This is a task-level implementation-approach deviation, not
a plan-level architectural one (the vocabulary/shape and redaction/no-raw-data
guarantees from US-6's ACs are all still met) — flagging for code review's attention
rather than treating as a plan violation requiring `.aiteam/AC_AMENDMENTS.md`. Will
confirm code reviewer concurs with that read during Phase 1.5.

T-6.5 (regression test) intentionally left to the unit test engineer in Phase 3, per
this team's convention that implementation engineers do not write tests.

## Code Review Advisories

(populated after Phase 1.5)
