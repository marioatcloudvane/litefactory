Story-003 proposes 12 criteria and nine linked tasks. Completion means a flagship agent applies only the reviewed repair, preserves restorable backups, stops on drift or uncertainty, verifies actual changes and delivers the outcome after one recovery restart. Every criterion has a future proof method; none is reported met. No owner question blocks this proposal. Intent, architecture, assumptions and dependencies remain unchanged.

The complete current Plan consists of story-001 (#432, CLI diagnosis), story-002 (#433, priority GitHub conversations) and story-003 (#434, approved repair). Story-001 has no dependency; story-002 depends on story-001; story-003 depends on both. No scope is retired or omitted. Supplied state records acceptance owner evidence for the current story-001 and story-002 drafts. This submission supplies the remaining story-003 acceptance content for review; it does not grant acceptance or implementation readiness.

Shared decisions remain: one issue, shared engine and memory, owner-prefixed GitHub commands, read-only analysis, current flagship with no downgrade, explicit approval of a presented immutable repair, engine-enforced agent writes, retained backups and one restart after verified repair. Successful-copy deletion belongs to #413 and does not block doctor. Retained assumptions include no automatic rollback, held partial repairs, one startup as recovery when already down, and supported isolated providers. Authentic field-guide/provider evidence belongs to story-001; authentic apply isolation, process exclusion and restart observations remain future obligations here.

Story-specific content and review belong on #434. The parent retains the three-story link-only index, shared decisions and this assembled review, bound by the factory to the exact current drafts, dependencies and shared decisions. Criteria and assembled-plan approval remain in this third stage. Publication, owner review and factory save/readiness remain required. After that process completes, use /implement <instruction>.

AC-001: Both entry points bind owner approval to the exact current, fully presented proposal. Delayed, stale, ambiguous or duplicate input cannot authorize another repair.

Future proof: Test authority, presentation order/gaps, proposal drift and replay; inspect bindings and operation counts.

AC-002: The flagship agent edits only through scoped tools enforcing approved targets, changes, preconditions, order and generation; native shell/SQL and ambient access are unavailable.

Future proof: Probe allowed/forbidden calls; inspect effects. Require versioned authentic apply-isolation observations before adapter enablement.

AC-003: Holds precede target process/writer drain and every startup recovery path. Down-daemon apply excludes concurrent startup; uncertain exclusion prevents edits.

Future proof: Race each writer, parent update and startup path; inspect fences, process identity, singleton exclusion and sibling preservation.

AC-004: All declared row/file backups complete before writes and remain restorable after success, failure or interruption. Doctor deletes none; successful-copy cleanup belongs to #413.

Future proof: Inject space/copy/sync failures; prove no writes. Restore bytes, modes, existence and related rows from private durable manifests.

AC-005: Approved row repairs cover runs/locks, report acknowledgments, poll flags, command positions and graph recovery while preserving history and sibling state. Unsafe or forged transitions are refused.

Future proof: Test every operation, invariants, foreign targets, unapproved command skipping and shared-record drift; verify atomic row/journal commits.

AC-006: Only approved regular files under recorded roots change. Unsafe aliases, root drift and factory/authority files are refused; unrelated files and git metadata remain intact.

Future proof: Probe traversal, symlinks, hard links and replacement races; compare bytes/modes/index and interrupt atomic replacement.

AC-007: Engine rereads verify steps and final state. Drift, new scope, failed checks or uncertain writes stop edits and retain partial repairs under hold for review, without automatic rollback.

Future proof: Inject each failure around writes; inspect journal, backups and reports. Require new reviewed approval for continuation/restoration.

AC-008: Stop fences later writes and drains admitted ones. Partial repairs stay held; proven no-write holds may release. After verified handoff, stop returns status without undoing/repeating restart.

Future proof: Race stop against backup/write/verify/handoff on both entry points; reject stale calls and compare holds and restart counts.

AC-009: Crashes reconcile receipts and file digests without blind replay. Ambiguity needs review. Missing/invalid final results cannot erase writes or prove success.

Future proof: Crash at write/journal/result boundaries; exercise preflight/submit/receipt and reply loss. Verify no automatic writing-agent retry.

AC-010: Verified repair alone commits conversation closure and one restart intent. A new daemon acknowledges identity and checks state before releasing holds; lost responses never cause blind restart.

Future proof: Inject handoff/ack loss and count dispatches. Retain authentic launchd, foreground and down-start observations; separate simulations.

AC-011: Both entry points report changes, backups, restart and unresolved findings after recovery. CLI status survives disconnect; report retry neither repeats repair nor advances workflow cursors.

Future proof: Test bounded waits/status, destination and lost replies; compare durable outcomes, markers, effect counts and watermarks.

AC-012: Apply uses current flagship without downgrade, preserves secret/issue isolation and workflow/result/usage ownership, and creates no mode, history deletion or remote repair mutation.

Future proof: Test outages, injection, canaries and migrations; compare effects. Run make check/race tests and retain story-001 evidence prerequisites.

T-001 — Implement approval and operation/hold/journal persistence and migrations. Owner: go-implementation-engineer. Criteria: AC-001, AC-003, AC-009, AC-012. Dependencies: none.

T-002 — Implement target drain, writer guards and startup exclusion. Owner: go-implementation-engineer. Criteria: AC-003, AC-008. Dependencies: T-001.

T-003 — Implement scoped backup/repair tools and restoration manifests. Owner: go-implementation-engineer. Criteria: AC-002, AC-004, AC-005, AC-006. Dependencies: T-001, T-002.

T-004 — Implement apply orchestration, verification, fencing and reconciliation. Owner: go-implementation-engineer. Criteria: AC-002, AC-007, AC-008, AC-009, AC-012. Dependencies: T-003.

T-005 — Implement restart recovery, status and outcome delivery. Owner: go-implementation-engineer. Criteria: AC-010, AC-011. Dependencies: T-004.

T-006 — Test linked approval, backup, scope and persistence criteria. Owner: go-unit-test-engineer. Criteria: AC-001, AC-002, AC-004, AC-005, AC-006, AC-012. Dependencies: T-003, T-004.

T-007 — Test linked races, crash recovery and restart/report uncertainty. Owner: go-unit-test-engineer. Criteria: AC-003, AC-007, AC-008, AC-009, AC-010, AC-011. Dependencies: T-004, T-005.

T-008 — Capture authentic isolation, drain and restart evidence; label missing observations. Owner: go-integration-test-engineer. Criteria: AC-002, AC-003, AC-010, AC-012. Dependencies: T-005.

T-009 — Exercise all criteria across CLI/GitHub; retain evidence mapping. Run make check/race tests, without integration tags. Owner: go-integration-test-engineer. Criteria: AC-001, AC-002, AC-003, AC-004, AC-005, AC-006, AC-007, AC-008, AC-009, AC-010, AC-011, AC-012. Dependencies: T-006, T-007, T-008.

