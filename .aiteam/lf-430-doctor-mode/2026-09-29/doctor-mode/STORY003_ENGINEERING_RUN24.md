# Doctor repair engineering — run 24

Frozen scope: story-003 / issue #434 / revision `36627c9d83e9ce38219bd9db4574f85c58e431bdd6d0260b2835b49bfd81f8c2`; dependencies story-001 and story-002. Engineering work only. This is not acceptance, code, architecture or plan review. No real repair, provider invocation, daemon restart, forge mutation, commit or push was performed.

## Implemented

- Migration 0085 retains exact displayed-proposal approvals, immutable numbered manifests, issue holds, backups, write journals and one restart intent. Approval and GitHub inbox admission share one transaction. Local presentation is recorded only after successful complete CLI output. Duplicate source and proposal execution cannot create another operation. Dispatch is claimed once; interrupted writing agents are never automatically replayed.
- Shared CLI/GitHub Apply orchestration checks current flagship/provider isolation and lifecycle capabilities, drains the selected target, captures fresh scoped evidence, invokes only scoped tools, verifies actual journal/file effects and commits durable closure/restart intent. Missing final results retain effects and failure status. Native diagnosis and repair transports are supplied by the companion engineer and still refuse unverified isolation.
- Closed corrective row catalog covers polling flags, stranded run failure, lock settlement, confirmed report completion without workflow source consumption, watermark rewind only, and cancellation/failure of graph attempts, dispatches, owner waits and instances. Before/after values and exact issue joins constrain every operation. Successful/test/approval state cannot be fabricated. Shared aggregates that cannot be isolated are refused.
- Normal SQL writers are held through explicit embedded migration triggers, including session/run/graph ownership and parent aggregates affecting a selected child. Daemon admission rechecks holds around inflight ownership; selected workers are cancelled and joined, parent writers joined without cancelling sibling activity. Startup orphan, graph, Bug and continuation recovery excludes held targets. No repair transaction spans filesystem/provider/network/process I/O.
- File repair supports captured regular session-data files under the exact issue worktree session root. Engine handles bind root and original file device/inode/digest. Symlinks, hard links, path escapes, root drift, authority/source/config files and unknown targets are refused. Original bytes/mode are retained privately before all writes; atomic replacement and sync precede fresh verification. Copies are never deleted.
- Interrupted file intents are reconciled by observation of original/intended/third state, without rewriting files. Partial or uncertain work remains held for reviewed continuation. Stop fences later writes and joins already admitted effects; no-write holds may release.
- Scoped backup/write/verify tools share the existing application request/receipt/observation ledger, with no fabricated workflow runs or separate result ledger. Tool receipts do not adopt application revisions. Replay checks exact request bytes first, including after stop; a settled file is verified instead of rewritten. Uncertain file intents cannot replay.
- CLI status and GitHub rendering show durable operation, steps, backups, restart and unresolved status. Recovery observes the exact new instance/token and rechecks repaired state before releasing a hold. A bounded daemon recovery pass retains a separate multipart outcome in the existing doctor delivery namespace. Publication retry does not repeat repair/restart or advance workflow cursors.
- Local daemon-down exclusion owns the actual daemon singleton lock. The companion host implementation adds authenticated live drain, same-binary foreground/down replacement and token observation; a stale socket is not evidence of daemon death. `launchd.Manager.Restart` uses the existing installed service identity without rewriting its plist.

## Executed checks

- `go test -race ./internal/doctor ./internal/store ./internal/daemon ./internal/cli ./internal/launchd ./internal/github ./internal/turn -run 'Test(Doctor|ParseDoctor|Command|ReconcileOrphans|Worker|Result|Meter)' -count=1` — passed: doctor 13.389s; store 85.636s; daemon 26.680s; cli 54.268s; launchd 3.454s; github 3.124s; turn 51.635s. Includes restored original story-002 presentation/parser and local I/O regressions.
- `GOLANGCI_LINT_CACHE=/tmp/lf-doctor-lint golangci-lint run ./internal/doctor/... ./internal/store/... ./internal/daemon/... ./internal/cli/... ./internal/launchd/...` with installed pinned 2.13.1 — `0 issues.`
- Additional subsequent `go test ./internal/store -run TestDoctorRepairGitHub -count=1` — passed 0.596s, including retained post-recovery outcome deduplication and destination/source binding.
- Final `go test ./internal/store -run 'TestDoctorRepairGitHub|TestDoctorRepairConfirmedReport|TestDoctorToolReceipt' -count=1` — passed 0.815s after strengthening numeric actor/principal binding.
- Earlier meaningful targeted tests exercised all-backups-before-first-write, ordered run/lock settlement, held startup versus sibling recovery, forged/foreign transitions, graph history retention, confirmed-report source isolation, exact watermark rewind, file aliases/missing backups, before/after/third-state crash observation, one-shot dispatch and shared tool-receipt recovery after fencing.

One targeted test found a SQLite string-parameter comparison error for rewind-to-zero; changed the predicate to an explicit integer cast and reran successfully. A temporary source collision overwrote the untracked story-002 presentation renderer; the original engineer restored its exports and behavior, the new CLI helper was moved to a distinct filename, and all presentation and daemon regressions above passed. A first lint attempt used an unwritable default cache; the bounded writable cache invocation above completed normally. These failed intermediate runs are not passing evidence.

## Remaining limits — not claimed complete

- No authentic installed-provider isolation observation exists; public provider gates remain closed. Controlled fixtures establish code behavior, not host isolation evidence.
- No authentic process-drain, launchd, foreground or down-start restart observation was made. Host tests use controlled clients/process starters and temporary local dependencies.
- The known stuck #406 designation is retained context, not an invented live database/log capture. Story-001 authentic field-guide capture remains an evidence gap under the owner's instruction to continue implementation.
- Superseded source gap: launchd-managed route selection/token carryover is now implemented by the companion host engineer; see IMPLEMENT_RUN_0024_HOST_CONTROL.md. Authentic host observations remain unavailable.
- Superseded source gap: general changed worktree files and captured absent originals are now implemented; see STORY003_FILE_SCOPE_RUN24.md. Authority/factory files and paths without issue-associated evidence remain refused.
- Aggregate `make check` and the complete race-enabled suite are owned by the orchestrator and are not claimed by this scoped report. No story completion checkpoint or acceptance state was submitted by this worker.

## Principal changed files

New store files: `doctor_repairs.go`, `doctor_repair_journal.go`, `doctor_repair_rows.go`, `doctor_repair_recovery.go`, `doctor_restart.go`, `doctor_repair_outcomes.go`, migration `0085_doctor_repairs.sql`, and scoped repair/catalog/tool-receipt tests.

New doctor files: `repair.go`, `repair_files.go`, `repair_broker.go`, `repair_admission.go`, `repair_recovery.go`, `restart.go`, `cli_presentation.go`, and controlled file/apply/recovery tests. Shared result/service/evidence/broker files extend immutable file handles, typed exact actions and final repair summary validation.

New daemon files owned here: `doctor_repair_control.go`, `doctor_repair_environment.go`, `doctor_repair_recovery.go`, `doctor_drain_test.go`. Existing inflight/admission/worker/report and startup recovery paths enforce holds. Companion engineer owns `doctor_repair_host*.go`, provider repair adapters and related wiring/tests.

CLI `cmd_doctor.go` and new `doctor_native.go` share Apply/status/presentation/provider/lifecycle services. `launchd/restart.go` and `restart_test.go` implement and test the installed-service primitive. Store result history, doctor inbox/deliveries and source recovery paths were extended without replacing normal workflow ownership.
