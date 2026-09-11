# Code Review — US-001

Feature: modes-should-stay  
Reviewed by: go-code-reviewer  
Date: 2026-09-09  
Status: CLEAN

## Scope

Frozen story US-001, issue #188, numeric identity 5399419269, revision `3959846ddf1ba22335787fb9d1b6053d73e6724c2230cdc3cc93c86d70be955e`, no dependencies. Reviewed worktree production changes in `internal/daemon/mode.go`, `internal/daemon/tick.go`, `internal/daemon/daemon.go`, and `internal/store/modes.go` against the supplied story and shared plan. Test files are owned by the test engineer and were not reviewed here.

## Blocking findings

None.

## Advisory findings

None for US-001.

## Acceptance and architecture review

- AC-001-1: The ordinary run path reports before marking/deleting its trigger journal, and no longer removes the canonical label. This path does not discriminate between supported modes or run success/failure. Subsequent retained-label routing still bypasses classification.
- AC-001-2: Command/classifier admission remains intact; completion has no forge label effect. Owner/self-comment filtering and comment ordering remain intact; classifier-enabled dispatch continues refreshing issue labels between actionable comments.
- AC-001-3: Startup abandoned-run reporting retires journals only after successful reporting. The startup sweep also retires completed journals for issues no longer discovered. Failed delivery does not satisfy the new reported-run predicate. Issue-body trigger zero requires actual reporting rather than its pre-execution bootstrap watermark.
- AC-001-4: Explicit cleanup-pending and positive-trigger watermark-inferred debt are retired without forge access. Startup and drain retirement are independent of classifier availability and cannot restore owner-removed labels.

SQL stays in store and all new accessors take the caller context. No transaction spans forge I/O. Error returns carry context at store/startup/drain boundaries. The startup sweep occurs before ticking; the scoped changes introduce no goroutines, shared mutable state, secret handling or filesystem I/O. `git diff --check` passed.

## Cross-story boundary

US-002 must address admission label confirmation and retry pinning. In the current admission code an already-observed canonical label does not set the new journal's `LabelApplied` bit; a report retry can therefore reapply that label after a later owner edit. This is existing admission behavior covered by US-002 AC-002-2/6 and does not arise from the new journal-retirement paths. This review does not certify those subsequent-story criteria.

## Verdict

CLEAN for the scoped US-001 production changes. Runtime acceptance coverage and passing scoped tests remain the test engineer/orchestrator gate before publishing verification.
