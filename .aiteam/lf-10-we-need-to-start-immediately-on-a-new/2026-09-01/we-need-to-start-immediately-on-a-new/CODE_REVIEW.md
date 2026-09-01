# Code Review
# Feature: we-need-to-start-immediately-on-a-new (issue #10)
# Reviewed by: go-code-reviewer
# Date: 2026-09-01
# Status: ADVISORY_ONLY

---

## Scope of this review

Combined diff of US-1, US-2 and US-3, reviewed together as one change since
none has previously passed this gate. Baseline: `git diff 1c78f70~1` plus the
currently uncommitted working-tree changes on top of it (US-3's landing in
this run). Files covered: `internal/daemon/tick.go`, `internal/daemon/doc.go`,
`internal/daemon/fixture_test.go`, `internal/daemon/issuebody_log_test.go`,
`internal/daemon/tick_test.go`, `internal/harness/prompt.go`,
`internal/store/migrations/0003_session_body_pending.sql`,
`internal/store/sessions.go`, `internal/store/store_test.go`,
`internal/turn/execute.go`, `internal/turn/prepare.go`,
`.aiteam/AC_AMENDMENTS.md`.

Verified mechanically before the manual read: `go build ./...`,
`go vet ./internal/{daemon,store,turn,harness}/...`, `gofmt -l` (all clean),
`go test -count=1 ./internal/{daemon,store,turn,harness}/...` (all pass),
`go test -race -count=1 ./internal/daemon/...` (clean, no races detected).

---

## BLOCKING findings

None.

---

## ADVISORY findings

### CR-A-001: Refusal reason compared by re-declaring the same string literal
**File:** `internal/daemon/tick.go:1264` and `internal/daemon/tick.go:1292`
**Story:** US-2 / T-002 (retry-marking logic in `Daemon.runIssueBody`)
**Category:** Idiom
**Finding:** The switch sets `refusal = "it is not assigned to the owner"`
at the "not assigned" case, and eleven lines later the retry computation
re-declares the identical literal to test against it:
`retry := refusal == "it is not assigned to the owner" && ...`. The two
literals are not tied together by the compiler; if either string is
copy-edited later (e.g. as part of a future log-message tweak) without the
other, `retry` silently stops firing for the case it exists to cover, with no
compile error. In this specific instance the risk is mitigated by
`TestALateAssignmentFiresTheBodyOnALaterTick`, which pins the end-to-end
behaviour and would catch the drift, but the pattern is still stringly-typed
control flow standing in for what a `bool` set once in the same case
(`notAssignedRefusal := true` alongside `refusal = "..."`) or a small typed
sentinel would express without the duplication.
**Suggestion:** Set a local bool (e.g. `notAssignedRefusal`) inside the
"not assigned" case at the same time `refusal` and `atInfo` are set, and test
that bool in the `retry` computation instead of re-comparing the string.

---

## Cross-story summary

The three stories integrate cleanly. Findings against the specific checks
requested:

1. **US-3's has-comments conjunction vs. US-2's retry marking**: `retry` is
   gated on `refusal == "it is not assigned to the owner"` and nothing else,
   so the has-comments branch — whether it fires (stranger-authored, blocked)
   or is exempted (owner-authored, runs immediately) — never sets `retry`.
   `TestNoRetryRefusalsNeverMarkBodyPending` pins exactly this for both the
   assigned and unassigned cases. Correct.
2. **SD-C's two identity comparisons**: the assignee check
   (`t.issue.AssignedTo(owner)`, in `internal/github/issues.go`, untouched by
   this diff) and the new author check
   (`len(comments) != 0 && (owner == 0 || t.issue.Author.ID != owner)` in
   `internal/daemon/tick.go:1282`) each carry their own independent zero-id
   guard and are not folded into a shared helper — confirmed by reading both
   sites; `AC_AMENDMENTS.md`'s AC-2.16d entry states this explicitly as a
   deliberate choice ("kept as two separate comparisons rather than folded
   into one helper... a shared helper would make the two look
   interchangeable, which they are not").
3. **AC_AMENDMENTS.md**: two entries present, in the ledger's existing
   format (First stated in / Amended / Status / It used to say / It now says
   / Why the change is safe). The AC-2.16d entry explicitly states, in bold,
   "This is an exemption from AC-2.16d, not a second way to satisfy
   AC-2.16a — and the two must not be read as contradicting each other,"
   which is exactly the distinction the plan required be on record.
4. **Doc comments (SD-E)**: all four falsified comments were rewritten, not
   merely appended to: `tick.go`'s "Watermark first, then run" section is
   replaced by "Reachable a second time, on purpose"; `execute.go`'s
   `Trigger.CommentID` comment drops the "fires a body exactly once" claim
   for the runs-table-uniqueness framing; `prepare.go`'s
   `bodyIsInstruction` comment gains the note that it says nothing about
   whether this is the first run of the conversation; `harness/prompt.go`'s
   comment drops the "not reachable today" claim and explains the new
   reachable path. `internal/daemon/doc.go` covers all three stories'
   concerns (the issue-body trigger's existence, US-2's durability, US-3's
   author-dependence) in one continuous section with no internal
   contradiction — it even states the "gap left on purpose" the same way the
   AC-2.16d entry does.
5. **Ring-fenced areas**: `git diff` against `internal/report`,
   `internal/github`, `internal/config`, and
   `internal/store/migrations/0002_session_backlog.sql` is empty both in the
   already-committed range and in the uncommitted working-tree changes.
   Confirmed untouched.
6. **Pinned regression tests**: `TestANonOwnerAssigneeDoesNotFireTheBody`,
   `TestABodyThatCommandsNothingStartsNothingAndPostsNothing`, and
   `TestIssueBodyToolsRefusalPostsNothing` all still exist and still assert
   what the plan requires of them (own assignment stays sole gate; a
   commandless body starts and posts nothing; the issue-body refusal path
   posts nothing to the forge).
7. **Standard checks**: idiomatic Go throughout (gofmt/vet clean); errors
   wrapped with `%w` at every new call site in `internal/store/sessions.go`
   and `internal/daemon/tick.go`; no new goroutines introduced — all new
   logic (`bodyPending`, `setBodyPending`, `clearBodyPending`,
   `retryIssueBody`) runs on the existing single Tick goroutine or inside the
   existing per-task worker goroutine, never spawning a new one; the new
   store methods follow the established `requireAffected`/`ErrNotFound`
   pattern and defer `rows.Close()` correctly; the daemon writes nothing to
   the forge on any issue-body refusal path (SD-D held); no SQL is built by
   string concatenation — the new `UPDATE`/`SELECT` in `sessions.go` use `?`
   placeholders throughout; the new partial index
   (`idx_sessions_body_pending`) keeps the steady-state cost of the new
   `Daemon.bodyPending` union at zero rows, matching the backlog union's
   existing shape. `go test -race` on `internal/daemon` found nothing.

No boundary looked risky beyond the one advisory above, which is a
maintainability nit rather than a functional risk given the existing test
coverage.

---

## Verdict

ADVISORY_ONLY — 1 suggestion noted (CR-A-001). Proceeding to environment setup.
