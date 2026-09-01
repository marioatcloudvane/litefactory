# Drift Report — we-need-to-start-immediately-on-a-new (issue #10)
# Branch: lf-10-we-need-to-start-immediately-on-a-new | Date: 2026-09-01

Covers the whole Implement effort across both runs (0005 interrupted, this run
completing it) against `plan_brief.md` and the three sub-issues.

## Delivered as planned

- **US-1** — the three loud gate refusals (empty body, has-comments, KindNone,
  command-spawns-nothing) promoted from `DebugContext` to `InfoContext`,
  `not-assigned` branch deliberately left at Debug, single log call site with
  one message string and one attribute set, level asserted in tests (not just
  text). `internal/daemon/doc.go` updated to name the issue-body trigger.
- **US-2** — dedicated `sessions.body_pending` column via
  `0003_session_body_pending.sql` with a partial index; `runIssueBody` returns
  `(retry bool, err error)`; retry marked only on the narrow four-fact
  intersection (not-assigned + non-empty body + zero comments + spawning
  command); `bootstrap` returns `(bool, error)`; drain arm of `Daemon.work`
  gains a body-gate call guarded on `session.BodyPending`; body-pending
  sessions unioned into `Daemon.discover` with an admission policy against
  starvation; flag clears on run/permanent-refusal/close. All four falsified
  doc comments (`tick.go`, `execute.go`, `prepare.go`, `prompt.go`) rewritten.
  `AC_AMENDMENTS.md` entry for AC-2.16b added. `0002_session_backlog.sql`
  untouched.
- **US-3** — has-comments branch made conditional on authorship
  (`len(comments) != 0 && (owner == 0 || t.issue.Author.ID != owner)`), with
  the zero-id guard against a deleted author. `TestAnIssueWithCommentsDoesNotFireItsBody`
  split into stranger/owner/deleted-author subtests. `AC_AMENDMENTS.md` entry
  for AC-2.16d added, explicitly recorded as an exemption from AC-2.16d rather
  than a change to AC-2.16a. `internal/daemon/doc.go` names the
  author-dependence. Gate doc block rewritten.
- **Cross-story constraints held**: identity compared on numeric id only, with
  independent zero-id guards on both the assignee check and the author check
  (not folded into one helper); no forge write or new endpoint on any refusal
  path; `internal/report`, `internal/github`, `internal/config` untouched;
  three pinned regression tests pass unchanged.

## Added scope

- This run's own verification/repair pass on US-1/US-2 found no defects, so no
  code was added there beyond what the interrupted run had already committed.
- The unit-test engineer (Phase 3) found four ACs not actually exercised by an
  existing test and added targeted coverage for each, with no source changes:
  `TestOwnerOpenedIssueFiresTheBodyOnALaterAssignment` (AC-2.2, Workflow 2 —
  owner-authored issue, unassigned at first sight, assigned later),
  `TestALateAssignmentFindingCommentsAlreadyPresentRefusesPermanently`
  (AC-2.9 — flag clears on a retry that lands on a permanent refusal),
  `TestBodyPendingSessionsCannotStarveCommentDrivenWork` (AC-2.11, new
  `internal/daemon/fairness_test.go`), and
  `TestAuthorshipExemptionComparesNumericIDNeverLogin` (AC-3.4). All four are
  test-only additions closing coverage gaps against the plan's own AC list,
  not new behaviour.
- One further doc-comment fix in `internal/store/sessions.go`, made necessary
  by SD-E as a consequence of the US-3 change, not called out by name in
  US-3's own Architecture Notes but falling under the same "rewrite what you
  falsify" rule.

## Dropped or deferred

- None. All tasks T-1.1–T-1.6, T-2.1–T-2.12, T-3.1–T-3.5 are accounted for in
  the delivered code, and every AC in all three sub-issues was checked against
  the committed code and passing tests.

## Architectural deviations

- None found. The mechanism actually shipped (`sessions.body_pending`,
  return-value retry signal, drain-arm gate call, discover-time union) matches
  SD-A through SD-H verbatim, and US-3's exemption matches its Architecture
  Notes exactly (conjunction plus zero-id guard, not folded into the assignee
  check).

## Verdict

CLEAN — delivered matches plan. The only additions beyond the committed code
were test-coverage gap-fills for ACs the plan already specified, not new
scope; there is no undelivered task and no deviation from the plan's
architecture.
