# Implementation notes — run 0008 / 304

## Current request and boundaries

The owner asked “So what is the actual status now?” after run 0007 exited with
status 1. This run is a bounded status audit within Implement, not an automatic
resumption of implementation. It inspected saved source and repository state,
executed build and required aggregate checks, and recorded results. No source
fixes, forge access, runtime-state changes, new engineering events, acceptance
assessment, review, or mode transition occurred.

## Current state

- Factory-supplied frozen scope marks US-001–US-004 engineering complete. These
  remain read-only satisfied implementation dependencies.
- US-005 has saved source/tests for submission binding, engineering and
  assessment records, recovery, and runner/daemon wiring. Both execution paths
  call `runSubmissionBound`, and daemon recovery calls `RecoverSubmissions`.
- US-006 has saved source/tests for typed Plan callbacks/publication, issue
  projections, frozen criterion lookup, approval and Plan handoff.
- Source presence is not completion: US-005–US-006 have no completion flag in
  the supplied bundle, and current aggregate checks fail.
- No corresponding feature changes were demonstrated for US-007–US-012: no new
  reconciliation implementation or `story-contract` CLI command was found,
  and no changes were observed under docs, teams/source, .claude, or .codex.
- US-011 requires manual execution using Claude for its .codex-only scope.
  US-012 additionally needs predecessor completion and authentic externally
  supplied two-harness evidence, which is not supplied in this run.

## Saved versus published

At inspection, HEAD and the cached origin task-branch ref both pointed to
`fd2a74c9a81a6accaeeaa98b44facfc2c3b0ae11`, the Plan commit. The inherited
implementation was present as 15 modified tracked files plus untracked source
and tests. This is local/cached repository evidence, not a fresh remote query.
Commit/push/save remain factory-owned; this run does not assert remote delivery.

## Current blocker

Build, formatting, vet and pinned lint pass. The aggregate race suite fails in
internal/turn with observed runner startup errors: `submission: configured
socket path is too long`. See TEST_REPORT.md for the executed checks and limits.
No fixes were attempted during this status request, and no conclusion is drawn
about the cause of the previous run’s exit status.

## Run 0009 / 305 — serial recovery execution sketch

The owner requested a sketch of recovery, one story at a time. This is an
execution sequence inside the existing Implement scope, not a new Plan proposal,
an implementation attempt, or a completion claim. The current factory bundle
still marks US-001–US-004 engineering complete and US-005–US-012 unfinished.
This run's filesystem inventory confirms that submission and typed-Plan source
and test files are present locally; that presence does not establish completion.
Earlier run 0008 test claims above remain historical; fresh reproduction is
recorded separately in TEST_REPORT.md by this run's root agent.

### Execution discipline

Use one story writer at a time. Preserve the inherited US-005 and US-006 work;
do not reset it or restart either story from scratch. Pass each selected story's
factory-frozen identity, full requirements, revision and dependencies to its
engineer. US-001–US-004 are read-only satisfied dependencies, not an invitation
to reimplement their internals.

For each story: identify its remaining engineering work, implement that work,
execute scoped tests and engineering checks, then immediately publish the exact
current snapshot and passing executed tests as an engineering_complete event.
Keep acceptance and review arrays empty. Do not start the next story while its
predecessor's implementation or scoped checks remain unfinished. If later changes
affect earlier unaccepted engineering snapshots, use the supplied invalidation
and replacement protocol; never silently keep stale evidence. Historical
acceptance remains permanent. Aggregate checks and factory save are separate
gates, never inferred from a scoped event.

### Ordered checkpoints

1. **US-005 / #219 — live submissions and recovery.** First reproduce the
   reported socket-path failure with the smallest existing test. Establish
   whether it comes from an overlong test temporary root, the new runner wiring,
   or a genuinely unsupported configured path. Fix the responsible unfinished
   story's code/fixture; do not conceal a production problem by shortening only a
   fixture. If the required fix falls inside a read-only completed dependency,
   identify that ownership conflict for the factory instead of editing it under
   US-005. Then finish both adapter bindings, same-run rejection/correction and
   receipts, stale-attempt rejection, restart/effect recovery and legacy/separated
   dispatch. Execute its scoped offline/race tests, then publish its snapshot.
2. **US-006 / #220 — typed Plan publication and freezing.** Finish the saved
   implementation next. Exercise discussion/completion, success-only readiness,
   exact issue revisions, redaction refusal, frozen criterion IDs, checkbox
   projection and legacy dispatch. Preserve shared link-only Story Index and
   factory-only publication. Publish engineering evidence only after scoped
   executed checks pass.
3. **US-007 / #221 — owner edits.** Reconcile all five sections, preserve ID
   history and unrelated issue text, route owner comments through existing
   admission locks, and keep ambiguous/conflicting edits pending. Exercise
   replacement, reorder, inline/comment races and frozen-scope preservation.
4. **US-008 / #222 — explicit upgrade.** Implement read-only preview first,
   then revision-bound recoverable apply. Exercise unsafe-scope refusal, partial
   publication replay and legacy history/version preservation before progressing.
5. **US-009 / #223 — shared instructions and docs.** Document the actual
   completed protocol, editing and upgrade paths; produce a precise
   source-to-target manifest using temporary/in-memory generation only.
6. **US-010 / #224 — Claude artifacts, under Codex.** Update only the assigned
   .claude outputs and execute their scoped consistency checks. Report temporary
   parity drift without making the sibling .codex update a prerequisite.
7. **US-011 / #225 — manual Claude handoff.** The operator selects Claude for
   the assigned .codex-only outputs. No permission retries, nested paid-agent
   launch or shared-source substitute. Retain scoped evidence independently.
8. **US-012 / #226 — aggregate completion.** After its predecessors' engineering
   completion, exercise both workflow versions, both synthetic adapter paths,
   all stages, 256-criterion capacity, correction/recovery, compatibility and
   both-tree parity. Retain a final make check result and the capture procedure.
   Authentic observations for both actual agents remain separately supplied
   operator evidence; synthetic checks cannot satisfy that acceptance criterion.

### Run boundaries and honest completion

The next engineering target is US-005 alone in the serial sequence. This is
ordering, not a claim that an owner comment automatically narrows the factory's
selected bundle. The current bundle selects all eight unfinished stories, so a
run ending after one story is still partial at the aggregate gate. If the owner
wants independently successful one-story subprocesses, the factory must supply
the corresponding narrowed scope through an actually supported selection path.
Do not invent a selector command or describe a prose instruction as such a path.
At every genuine run boundary, retain evidence and leave commit/push/save to the
factory; verify its save confirmation before calling the checkpoint saved.

No new implementation, completion event, acceptance assessment, code/architecture
review, Plan transport or automatic Validate transition is part of this sketch.

### Fresh source diagnosis supporting the first checkpoint

The bounded analysis-only engineer traced the freshly reproduced failure:
`internal/turn/fixture_test.go:75–83` constructs LF_HOME under the test's long
temporary root; `internal/config/submission_paths.go:7` adds `/s`;
`internal/turn/submissions.go:157–166` passes that root to the broker; and
`internal/submission/broker.go:75–77` adds a 16-character attempt directory plus
`/s` and rejects paths exceeding 100 bytes. The fresh root log shows this
failure before the stub runner starts. Configuration documentation explicitly
retains the long-home rejection. This is evidence for a fixture repair, not for
removing containment/path limits or claiming arbitrary production homes work.

Concrete first action: use a short, private, resolved temporary LF_HOME with
cleanup for broker-enabled turn fixtures in unfinished US-005/US-006 scope;
leave completed config/broker implementation unchanged. Re-run the exact failing
test, then scoped submission and typed-Plan checks. If later facts establish a
production requirement beyond the existing supported path bound, surface the
separate ownership/scope issue rather than concealing it in a fixture change.

The same engineer inspected actual command behavior: `internal/turn/command.go:
210–215` treats trailing `/implement` text as free prompt text;
`internal/turn/approved_context.go:36–43` includes all approved revisions; and
`internal/turn/harvest.go:66–75` checks generation-wide PendingEngineering.
Consequently `/implement finish US-005 only` is not a scope selector and cannot
make a one-story subprocess a fully successful implementation run. The supported
recovery recommendation is serial story work with immediate evidence checkpoints;
independent successful one-story runs would require factory selection/gate
capability beyond this approved feature scope. No such change is proposed here.

## Run 0010 / 306 — test repair only

The owner explicitly requested stopping after test unblocking. Fresh diagnosis
identified a legacy/v2 fixture mismatch, not a need to weaken the socket-path
check. `setupOutcomeForge` now explicitly selects legacy content/submission v1,
matching its unchanged file-producing stubs. Existing typed broker fixtures keep
v2 and their short private resolved temporary homes. Only
`internal/turn/plan_outcome_test.go` was edited in source; no production changes,
new features, acceptance/code/architecture/plan review, or story engineering
completion was performed. Scoped and full turn race checks pass; see
TEST_REPORT.md for exact results and the final aggregate gate.

US-005–US-012 remain unfinished in the supplied factory scope. This checkpoint
does not declare those stories complete, reopen completed dependencies, or
advance modes. The next separately requested step remains finishing US-005.
Saving, committing and pushing remain factory-owned. No new completion events
are appropriate for a fixture-only checkpoint.

The first run 0010 aggregate attempt then hit the package-wide 600-second test
deadline. Fresh stack diagnosis plus the isolated 594.698-second pass justified
a bounded infrastructure adjustment: Makefile test now explicitly allows 20
minutes per package, retaining race detection, assertions and operation
cancellation. This changes test infrastructure only and introduces no feature
work. The run's source/infrastructure delta is therefore the legacy Plan fixture
binding plus the Makefile timeout/comment. Both the failed aggregate attempt and
subsequent final outcome remain in TEST_REPORT.md.

Run 0010 final checkpoint: root `make check` passed (exit 0), including the full
race suite; internal/turn took 672.931 seconds under the explicit 20-minute
package allowance. Root `go build ./...` passed as well. Tests are unblocked and
work stops at the owner's requested boundary. US-005 is the next unfinished
story; no later story work, completion events or mode transition occurred.


## Run 0011 / 307 — bounded continuation

Owner scope is US-005 and US-006 only. US-001–US-004 remain satisfied read-only dependencies; US-007–US-012 are outside this run. Preserved inherited work and serialized shared writers. No forge access, commits, pushes, acceptance/review decisions, protected-tree edits or mode transitions.

US-005 engineering fixes current-worktree verification for reference-only evidence and crash recovery, and requires a correlated recorded receipt for terminal Validate findings. US-006 consumes frozen typed IDs, projects canonical checkbox rows while preserving explanatory prose, and preserves completed dependency checkbox state and immutable projections during unchanged republication. Semantic inline reconciliation remains US-007. Submission intent alone is ignored for equivalent immutable projection comparison; requirement bytes are not rewritten.

Offline synthetic tests exercise both adapter paths, in-run correction, real local Unix brokers, real SQLite, reference integrity, fresh-connection recovery, concurrent correlation, cancellation/drain and fake forge publication. These tests do not establish authentic provider helper delivery, which remains US-012 evidence.

Initial scoped regression failures and cycle 1 fixes are recorded in TEST_REPORT.md. Test flags remain `-race -timeout=20m`; no background log-tail loop or test-flag changes were used.

Run 0011 final checkpoint: both selected stories have current snapshot-bound engineering events and passing scoped/aggregate checks. `make check` and `go build ./...` exited 0. Initial two regression failures were fixed in one cycle; no further test failures occurred. Lint cache warnings were nonfatal (0 issues, aggregate exit 0); no permissions or operational state changed. Stop within Implement after this report. Saving remains factory-owned; no Validate suggestion or dispatch is made before successful-save evidence.


## Run 0012 / 308 — selected US-007, US-008, US-009

Owner requested stories 7, 8 and 9. Execute sequentially with the supplied immutable issue revisions; retain engineering-complete US-001–006 as dependencies. No forge access, acceptance/review, issue closure, protected-tree edits, commit/push, or mode transition.

US-007 engineering checkpoint passed and was published immediately before starting US-008. Reused existing numeric-owner refinement/dedup/admission and durable revision-conflict journal. Automatic presentation equivalence is conservative (whitespace/reordering); uncertain inline meaning remains pending for Plan. Established clarification can be explicitly supplied by an admitted authenticated owner comment `Clarify AC-001: <replacement text>`; ID retention is bound to the exact comment/session, child, generation and proposed text. Material replacements receive fresh IDs and retire prior IDs. GET/PATCH/re-read checks detect observable conflicts but provide no atomic forge guarantee.


US-008 engineering checkpoint passed. CLI `story-contract upgrade <parent-ref>` previews by default; `--apply` explicitly selects the revision-bound unfinished cohort. Recognizable legacy conversion is conservative: ambiguous narrative/tables/missing sections/inconsistent dependencies refuse, and legacy size/status are retained in task descriptions. Durable pending upgrade intent locks unsafe admission, resumes exact partial publication, and finishes with a new unapproved typed generation while preserving workflow and historical/completed legacy records. The next approved scope remains required. US-007 overlap was explicitly invalidated before two shared source edits and reissued after the full affected checks. US-009 begins only after both engineering checkpoints.


US-009 shared instructions and operator documentation implemented and checked without modifying `.claude/**` or `.codex/**`. `docs/story-contract-instruction-manifest.md` enumerates exact target handoffs and reproducible in-memory rendering. Installed wrapper/protocol parity intentionally remains for US-010 (Codex updates `.claude/**`) and US-011 (manual Claude updates `.codex/**`). US-012 must retain authentic external observations for BOTH actual harnesses; none were supplied or commissioned. No permission widening, nested paid-agent launch, or automatic Validate. Final aggregate checks underway.


Final run0012 outcome: US-007/008/009 engineering checkpoints complete and current; root build and the single final make check passed. Work remains uncommitted for litefactory to save. No live daemon/runtime state changed, forge calls, protected-tree edits, acceptance/review, issue closure, or mode transition. All prior work preserved. Continue only by explicit owner selection; no automatic Validate suggestion before factory confirms save.


## Run 0013 / 309 — selected US-010–US-012

Persisted workflow 2; requirements are the supplied frozen issue revisions. US-001–009 remain read-only satisfied engineering dependencies. This is Codex, so US-010 is independently executable. US-011 explicitly requires manual Claude execution of the `.codex/**` targets in `docs/story-contract-instruction-manifest.md`; no permission retry, harness launch or shared-source substitute is authorized by that requirement. US-012 remains dependency-blocked until US-011 engineering completes. Treating “do stories 10, 11 and 12” as selection preserves their explicit harness/dependency constraints.

Blocked events 309/000001.json (US-011) and 309/000002.json (US-012) preserve these facts. No authentic external two-harness helper/diagnostic/correction/decision capture was supplied in this run. No exception or waiver was supplied. Synthetic checks cannot supply that observation. No automatic Validate, acceptance/review findings, forge action, commit or push.


Run 0013 checkpoint: US-010 engineering complete, event 309/000003.json; 16 Claude manifest targets, snapshot `b1945927ccba057b85a2b5ba8d93f3b73012891cd2139e5d2d691ed26cd21e95`. Scoped assertion/render/whitespace checks and build passed. Final make check running. US-011/012 remain blocked as recorded above; temporary installed-tree parity drift is intentional pending manual Claude handoff.


Run 0013 final: `go build ./...` and the single final `make check` passed, exit 0. Format, vet, pinned golangci-lint v2.13.1 (zero issues) and full race suite passed; turn 730.400s, daemon 391.349s, CLI 259.218s. Linter emitted nonfatal cache-write permission warnings; no permission changes or check retries occurred. Published US-010 event 309/000003.json was verified against all 16 final worktree files. No `.codex` diff exists.

Selected scope remains partial: US-010 engineering complete; US-011 blocked for manual Claude execution of the exact Codex targets in docs/story-contract-instruction-manifest.md; US-012 blocked on US-011 and cannot claim aggregate installed-tree parity. Authentic external evidence for both actual harnesses remains missing/unverified without a waiver. Existing US-001–009 work was preserved. Session artifacts record progress outside the exclusively `.claude/**` US-010 implementation snapshot. No acceptance/review, issue closure, forge actions, commit/push or automatic mode transition. Factory saving remains pending.
