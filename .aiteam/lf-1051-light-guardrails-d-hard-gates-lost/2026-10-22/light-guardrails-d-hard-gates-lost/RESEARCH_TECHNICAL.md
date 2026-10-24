# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f01

Baseline and limits: inspected current checkout 6dc340a96a0e652a4f54a43c8c60e36730aa84e7 rather than assuming the issue's 032c55a6 line references still describe it. Working tree was clean. This is static code and repository-history research; no production database, daemon logs, or incidents #989/#1006 were inspected and no tests were executed.

Evidence:

- `internal/turn/validate_resolve.go:240`
- `internal/daemon/graph_dispatch.go:426`

## f02

D1 remains for suites and missing reviews. Validate resolve rejects shipBlockers before writing decisions. computeReadiness requires suite passed, assessment, usable reviewer results, settled findings and successful issue effects; shipBlockers only simulates passing awaiting checks and deferring optional findings. Existing accept-with-reason permits deliberate deviations for eligible blocking architecture/plan/code findings, but not suite or criteria. Suite blocked also represents missing recipe/runner, incomplete process cleanup/capture and snapshot drift; those are distinct causes, not merely flaky tests. Improvement requires a separately recorded owner waiver keyed to instance, tested commit and specific blocker identities, applied consistently to readiness, reports and delivery reentry while preserving original failure evidence.

Evidence:

- `internal/turn/validate_resolve.go:240`
- `internal/turn/validate_state.go:325`
- `internal/turn/validate_commands.go:239`
- `internal/turn/validate_suite.go:336`

## f03

D2 is not a parser/guidance omission: /validate-implement already accepts optional -- guidance, retrieves it and adds it to reopened story guidance. The resolver rejects an empty target list, and the handoff independently errors if no story is reopened. Targets normally derive from failed/unverified criteria, blocking reviews or handed-back failed suite. A pass-disagreement path therefore needs a defined destination story/coordinator and a handoff independent of inferred failures, not just removal of one refusal. Existing schedule identity is derived from instance and commit, so repeated guidance on one snapshot needs explicit replay semantics.

Evidence:

- `internal/turn/validate_implement.go:270`
- `internal/turn/validate_commands.go:121`
- `internal/turn/validate_resolve.go:229`

## f04

D3 confirmed with qualifications. prepare refuses too-far-behind unconditionally; base conflict refusal depends on BlockRunOnConflict and absence of an active story merge. Implement story workers/coordinator already have conflict-handling exceptions. Defaults are cap 100 and block-on-conflict true. Bug source preparation records synchronization concerns and paths as prompt notes; genuine source/preparation errors can still fail. Reuse sync observations and notes, keep cap as a limit on automatic merge work rather than on launching an authorized run, and preserve dirty edits. Delivery and checks must subsequently use the resulting source snapshot.

Evidence:

- `internal/turn/prepare.go:190`
- `internal/turn/bug_graph_prepare.go:17`
- `internal/config/config.go:599`

## f05

D4 confirmed. Predating replies and certain occupied-wait comments are visibly refused and consumed through reporting, rather than retained for later use. Bug not-applied and Implement no-parked-target paths also refuse. This is loss of guidance to future execution, not necessarily deletion of the original forge comment. Existing Implement early guidance only applies at the await node and resumes parked stories; Bug already retains consumed diagnosis guidance in order. A general queue needs authenticated source identity, scope, ordered claim/delivery/acknowledgment, deduplication and atomic retention before watermark advancement. Guidance must never become approval of a question published later.

Evidence:

- `internal/daemon/tick.go:2780`
- `internal/turn/implement_graph.go:335`
- `internal/store/graph_diagnoses.go:300`

## f06

D5 confirmed at the active public boundary, with reusable older machinery. The repair runner exposes eight typed operations. New diagnosis validation rejects legacy operations such as graph_wait_cancel, graph_dispatch_cancel and execute_sql; a regression explicitly proves this. Internal legacy row repair code still snapshots exact affected columns, checks task scope and before state, retains backup digests and requires target drain. Its existence does not make these operations sanctioned new repairs. An exact-row repair would need coordinated diagnosis, immutable manifest, tool catalog/broker, store validation, exclusion, apply/verify, audit and rollback changes. The runbook still prohibits manual runtime edits. Changing documentation alone does not create a safe repair path.

Evidence:

- `internal/doctor/typed_proposal_test.go:35`
- `internal/agent/repair.go:19`
- `internal/store/doctor_repair_rows.go:89`
- `docs/operations/recovery.md:38`

## f07

D6 issue-body silence is intentional in current admission code: obsolete tools and unresolved coding-agent labels log and return when trigger ID is zero. Mode refusal has exceptions that already report some body-trigger errors. The stated origin is a reply/report API built around consuming a real comment, plus avoiding implicit permission fallback. Use a standalone durable admission notice with a stable session/trigger/reason identity instead of inventing a comment ID or falling back to broader tools.

Evidence:

- `internal/daemon/tick.go:3100`

## f08

D6 is partly superseded: requeueGraphStep now uses RequeueGraphDispatchWithStop and reports cancelled unallocated dispatches through ReportGraphDispatchStop, whose journal-backed message says work stopped and offers an owner reply to resume. Regression TestDispatchExhaustionPublishesOneStopNotice exists. Stranded-dispatch reconciliation and persistent recovery tests also exist. Reuse and audit this path before adding any second exhaustion notifier; do not describe all three-start exhaustion as currently silent.

Evidence:

- `internal/daemon/graph_dispatch.go:426`

## f09

D6 remaining cases differ. Legacy Bug profile admission rejects unsupported pins before reporting, and dispatchGraphSteps logs/continues on admission failure. Implement discovery parks after three failures and records a reason without posting directly in that function; full downstream visibility needs an integration check. Generic graph failure terminalizes when StopOnFailure is set, but explicit owner-parking metadata takes precedence. Current Bug graphs already declare owner parking on analyst, fixer and finalization failures; Research's inspected graph does not and brief-generation failure routes directly to stopped. Restart on current graph should create an explicit successor preserving the old capture/history and reconcile uncertain effects before replay.

Evidence:

- `internal/turn/graph_node.go:281`
- `internal/daemon/implement_story_task.go:60`
- `internal/turn/bug_runtime.go:104`
- `teams/go-development/v1.0.3/research/research.dot:20`
- `teams/go-development/v1.0.3/bug/bug.dot:7`

## f10

D7 combines different safeguards. Plan story-set promotion requires exactly 64 lowercase hex characters and current set identity. Validate report commands require 12 lowercase hex characters; the digest includes findings, verdicts, choices and effect states. Separate checks bind actual branch head, PR number/head, and exact close set. Bare commands can be resolved to one pending item server-side, while retaining internal compare-and-set and effect identity. Treating a changed display revision as a warning must not silently retarget a merge or close to changed resources.

Evidence:

- `internal/turn/validate_commands.go:105`
- `internal/turn/plan_graph_promote.go:100`
- `internal/turn/validate_state.go:376`
- `internal/turn/validate_resolve.go:205`

## f11

D8 confirmed with scope corrections. Classic Execute readiness refusal follows graph admission paths; changing that line alone will not settle every Implement path. Planning context refusal covers malformed, foreign and multiple matching contexts; transient read failures are distinguished. Repository history identifies eb893565 (2026-09-15), 'Refuse an unusable planning context instead of stalling the queue': the refusal was itself a fix for silent repeated retries. Rebuilding from admitted task state and recording a caveat can address usability while preserving task identity and avoiding arbitrary selection among multiple conversations.

Evidence:

- `internal/turn/plan_context.go:107`

## f12

Reusable infrastructure already separates local work, report delivery and remote effects. Validate uses effect ledgers/replay; graph stops persist owner waits and durable reports; Doctor uses scoped immutable repair steps and exclusion. The root-cause inference is multiple locally reasonable protections implemented as independent binary gates: evidence validity, stale-input prevention, finite retry budgets, workflow provenance and approval authority. A light-guardrail policy needs each concern classified and recorded without conflating 'failed test' with 'owner not authorized' or 'remote effect uncertain'.

Evidence:

- `internal/turn/graph_stops.go:33`

## f13

Shaping inspection confirms Validate recognizes distinct ticket and code owners and holds code-owner shipping until ticket acceptance decisions settle; legacy row repair snapshots task-scoped rows and rejects mutations that strand related graph work.

Evidence:

- `internal/turn/validate_resolve.go:245`
- `internal/store/doctor_repair_rows.go:65`
- `internal/store/doctor_repair_rows.go:176`

## f14

The judge’s issue comment 6383284663 identifies two scope defects: a sole pending target does not prove owner intent, and exact before/after row values do not ensure legal transitions or cross-row consistency; inspection supports both concerns because current Plan approval rejects stale sets, Validate binds commit and revision, and legacy repair checks related live work.

Evidence:

- `internal/turn/plan_graph_promote.go:108`
- `internal/turn/validate_resolve.go:113`
- `internal/store/doctor_repair_rows.go:176`
