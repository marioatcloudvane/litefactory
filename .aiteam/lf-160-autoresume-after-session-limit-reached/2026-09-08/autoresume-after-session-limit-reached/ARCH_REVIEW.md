# Architect: Go Architect
# Created: 2026-09-08
# Verdict: READY FOR STORY DECOMPOSITION — no major deviation

Reviewed current source `ca9d101b217695b94f16a03b5688fc7f5c2c5754`, APP_CONTEXT, full APP_CONSTRAINTS, feature-spec, platform constraints and preserved #142 architecture/provider evidence. This review reuses prior findings and verifies affected current boundaries; it does not repeat provider research or claim implementation completion. Story task identifiers below are proposed coordination identifiers for the product owner to retain or map explicitly.

## System Overview

This feature extends the existing scheduler and both existing CLI adapters. `internal/codex` and `internal/harness` interpret provider-owned terminal diagnostics; `internal/agent` carries a vendor-neutral interruption value. `internal/turn` records one attempted execution and its recovery intent, preserves conversation identity, and delivers reports. `internal/daemon` alone decides when to dispatch and retries on later ticks. `internal/store` owns all SQL, migrations, automatic-origin identity and transactional lifecycle transitions. `internal/report` renders neutral values and uses existing redaction.

No new package, service, public command grammar, migration library or scheduler dependency is needed. A factory-authored `/schedule` comment describes an already-persisted automatic action. It is not an owner command and bot admission must remain unchanged.

### Data flow

Provider terminal quota diagnostic → adapter classification and reset normalization → neutral result → durable interrupted outcome plus continuation intent → partial-work inspection and truthful report → existing daemon due-work selection → current-intent validation → existing worker/reservation/turn-lock path → explicit same-conversation continuation. Further verified quota failures repeat within the root budget. Terminal chain completion settles the original scheduled prerequisite.

### Concurrency and state

Reuse pool limits, per-session inflight exclusion, cross-process turn lock and clone gate. No sleeping goroutines, detached contexts or retry loops below daemon (CONC-2/5/6/7). Inject adapter observation timestamps/location into pure parsing; daemon supplies eligibility time. Snapshot continuation context by value, cloning nested collections (CONC-1).

The authoritative current-intent check is necessarily an observation boundary: an owner comment arriving after the final check follows ordinary already-running behavior. Do not promise an atomic GitHub-read/subprocess-launch transaction. If owner/issue observation fails, defer; never infer that no newer intent exists.

## Shared Decisions

1. **Configuration and retry assumption.** Global strict YAML `auto_resume_on_session_limit`, false in defaults/template, follows existing loaded-configuration lifecycle without hot reload (CFG-1/2/3/5). Three actual automatic invocations per root interruption is explicitly present in the salvaged feature-spec, plan_brief and ARCH_REVIEW AD-8. It is a carried-forward headless product assumption, not a user-specified number, normative rule or newly invented requirement. Preserve its disclosure. Count allocated automatic runs, not reservations or reporting retries.
2. **Evidence and precision.** Both adapters ship together using existing transports. Claude top-level rejected quota plus terminal error semantics is required; `subtype=success,is_error=true` is a failure. Codex narrowly recognized provider-owned terminal error text is admissible; arbitrary model prose is not. Claude epoch seconds use +60 elapsed seconds; Codex displayed local minute uses its next-minute boundary +60 seconds. Use the latest reset among explicitly known exhausted windows; known missing reset blocks scheduling, absent hypothetical windows do not. Time-only Codex observations must share a local calendar date; ambiguous dates, DST folds/gaps, contradictory zones or inconsistent local context are report-only. Leave child environments unchanged and persist UTC once.
3. **Durability and reporting.** A continuation is uniquely identified by interrupted run ID, with explicit automatic origin, root attempt and saved context. Preserve manual `(session, source_comment, ordinal)` uniqueness and private schedule TriggerID; never forge a comment ID, depend on nonzero issue-body triggers or overload ordinals. Persist semantic interruption and recoverable intent atomically before reporting; report delivery acknowledgement, reported state and watermark follow confirmed post (CONC-4). This is execution bookkeeping, consistent with existing FinishRun-before-report ordering; no deviation is required. Discover pending announcements independently of comment watermarks. Existing at-least-once comment delivery can duplicate an announcement after an uncertain response, but cannot duplicate schedule/run allocation.
4. **Resume and managed scope.** Save discovered handles on verified interruption as well as success. Quota must not trigger Codex's broad pre-turn invalid-session fallback. Automatic recovery explicitly selects resume and fails terminally for unusable handles; retain ordinary manual fallback. Snapshot task/worktree/agent/mode/segment/original request identity. Managed Implement recovery reloads the factory-frozen approved generation on every attempt and preserves completed-story/read-only dependency semantics, immediate progress evidence, invalidation, and aggregate-only parent recovery (SEC-20/21, CONC-9/10/11/12). Do not snapshot credentials, resolve current live edits as scope, reclassify mode, or relax permissions.
5. **Cancellation and dependency ownership.** New admitted work, reset or context change, observed closure and effective disable permanently cancel pending automatic work, including future-due work; reenable/reopen does not resurrect it. Schedule controls alone do not supersede unrelated continuations. The original scheduled prerequisite remains suspended/nonterminal through recoverable interruptions. Descendants stay attached to it, release only after final semantic success using final completion time, and block on terminal failure/cancellation/exhausted budget. Avoid leaving this as an ordinary running reservation which startup reconciliation will finalize.
6. **Persistence and validation.** Store owns one embedded migration and real SQLite transactional tests; retain one connection. Coordinate schema contracts before dependent implementation. Semantic result must survive `RunReported`, and scheduler/startup reconciliation must never treat exit zero alone as success. Preserve the distinction between live `agent.RunResult.Succeeded()` and persisted `store.Run.Succeeded()` (STYLE-10). Interrupted work remains uncommitted under CONC-8; no speculative commit/push of partial work.

## Current Code Drift Verified

- Current `internal/turn/harvest.go` additionally registers StoryProgress, validates approved relationships and records aggregate success; unlike the old branch, continuation must preserve these managed lifecycle boundaries.
- `internal/turn/harvest.go:94` still persists a discovered resume token only on success; `:101` records FinishRun before reporting. Both are relevant extension points, without changing successful-save gates.
- `internal/codex/runner.go:312` already records invocation start/finish timestamps; its pre-turn failure classification still broadly returns ErrSessionNotResumable. The quota case must take precedence.
- `internal/daemon/schedules.go` still dispatches schedules before ordinary comments and uses exit zero for schedule success. Newer owner intent therefore needs explicit preflight. Current startup reconciliation in `internal/store/runs.go` has the matching exit-code assumption.
- `internal/store/migrations/0005_schedules.sql` still establishes manual source-comment uniqueness. Add automatic origin/source-run ownership with a new numbered migration; do not rewrite that old migration.
- Current managed Plan proposal/index validation is in `internal/planbrief/proposal.go`, `internal/turn/plan_handoff.go` and `internal/turn/plan_index.go`. The old Markdown story files are historical evidence only and must not be recreated.

## Task Hints

Dependencies: US-1 and US-2 independent; US-3 depends on both; US-4 depends on US-3; US-5 depends on US-3 and US-4. Agree neutral contract and complete migration requirements, including suspended prerequisites, before US-3 lands.

| Task | Agent | Architectural hints |
|---|---|---|
| T-1.1 Configuration/wiring | go-implementation-engineer | Use existing config defaults, strict YAML/template and daemon/turn Options. Config remains a leaf; no runtime config edits or new lifecycle. |
| T-1.2 Operating documentation | go-implementation-engineer | Explain default false, both providers, conservative reset timing, three-attempt assumption, loaded config lifecycle and manual recovery. |
| T-1.3 Config verification | go-unit-test-engineer | Existing config/template round-trip tables, malformed bool, absent/false/true and wiring; preserve purity. |
| T-2.1 Evidence contract | go-implementation-engineer | Reuse imported selected authentic envelopes and sanitized fixture provenance. Historical Codex binary version is unknown; Claude versions are 2.1.261/2.1.263. Document supported forms/current version verification separately (EXT-1/2); no paid quota exhaustion or transport migration. |
| T-2.2 Neutral result/adapters | go-implementation-engineer | Contract in agent, vendor syntax only in adapters. Continue draining streams; distinguish warnings and generic failures from completed terminal rejection; quota is a result value (CONC-3), not a session-refusal error. |
| T-2.3 Provider verification | go-unit-test-engineer | Fake CLI streams exercise both real runner paths, resumed pre-turn failures, duplicate events, warning then success, malformed/incomplete reset, multiple windows, local date/DST/precision and unchanged environments. Label synthetic edge cases honestly. |
| T-3.1 Durable state | go-implementation-engineer | Store owns migration, semantic outcome, origin/source-run uniqueness, root/attempt/context and announcement state. Eliminate crash gaps through atomic completion+intent or a durable reconstructable intent. Coordinate US-5 suspended prerequisite metadata here. |
| T-3.2 Turn/report integration | go-implementation-engineer | Persist handle and intent with interruption outcome; report absolute UTC due time via renderer validation/redaction (SEC-5/6). Preserve partial-work uncommitted policy and truthful failure reason. Bot announcement cannot become scheduling authority. |
| T-3.3 Durability/report verification | go-unit-test-engineer | Real migrated SQLite for uniqueness and crash boundaries (TEST-6), existing turn fixtures and report tests for issue-body/comment/private schedule triggers, replay and failed post. |
| T-4.1 Resume dispatch | go-implementation-engineer | Extend existing due/reserve/worker path with explicit continuation context and under-lock checks. Bypass mode reclassification and automatic cold fallback; consume frozen managed scope using existing machinery. |
| T-4.2 Intent cancellation | go-implementation-engineer | Reuse numeric admission and command classification for newer owner work without advancing its watermark. Failure defers. Persist cancellation of future as well as due automatic work, respecting current loaded configuration. |
| T-4.3 Dispatch verification | go-unit-test-engineer | Channel rendezvous, Quiesce and real subsequent ticks (TEST-8/9) cover contention, cancellation ordering, missing handles, permission/manual fallback regression and managed revision replay. |
| T-5.1 Restart reconciliation | go-implementation-engineer | Extend current ReconcileOrphans/recovery discovery. No-run reservation may requeue; allocated abandoned run remains terminal absent already-durable verified quota evidence. Recover pending intent/report without rerunning source execution. |
| T-5.2 Chain settlement | go-implementation-engineer | Transactionally maintain root attempt budget and original suspended prerequisite outcome. Persist fresh reset evidence per retry; release descendants only on semantic success using final completion timestamp. |
| T-5.3 Lifecycle verification | go-unit-test-engineer | Real SQLite restart/fault matrix, httptest forge and fake agents cover both providers across the normal runner→turn→scheduler chain, cancellation, multiple prerequisites, manual schedule regression and managed child/aggregate semantics. |

Each task belongs to Go backend architecture. No separately provisioned infrastructure or integration-tagged tests are warranted. Preserve stdlib table tests/handwritten fakes, injected slog, context-first I/O and stable outputs (TEST-1/2/3/6/7). Platform HTTP/router constraints are inapplicable: no new listener. Run appropriate scoped tests, `go build ./...`, `make check`, and coverage checks during implementation. Do not write dist or change dependency inventory without a demonstrated need.

## Managed Proposal Handoff Notes

All five stories are creates because no existing managed identities/bases were supplied. Explicit JSON fields for each: `intent: "create"`, `issue_number: 0`, `issue_id: 0`, `expected_base: ""`; key/title/full body/dependencies remain required. Use an empty array for no dependencies. Envelope: version 1, parent `github:marioatcloudvane/litefactory#160`, generation 1. No legacy #149–#153 identity adoption. Parser limits are 100 stories, 8 MiB proposal, 65536 bytes per body, 256 bytes single-line title; unknown fields/trailing JSON and cyclic/unknown/duplicate dependencies are rejected.

Bodies require exact nonempty level-2 User Story, Acceptance Criteria, Tasks, Architecture and Dependencies sections. Give every acceptance bullet a stable leading AC identifier and reuse it exactly for acceptance evidence. Do not imply task rows are additional unlabeled criteria.

Story Index columns remain `ID | Title | Size | Depends On | Sub-issue`; before factory publication, `—` (or empty) is the supported pending Sub-issue value per internal/planbrief/index.go. There is no special pending-link URI convention. Do not fabricate issue links or use local story paths. Explain pending publication outside the table. Factory rewrites real links and removes transient proposal before save. Shared context belongs in plan_brief; no per-story Markdown files.

## Deviations

None. Three-attempt cap and permanent cancellation are disclosed carried-forward product assumptions, not standards amendments. Prior forced-UTC proposal and provider-detection blocker remain superseded. No implementation or GitHub publication was performed in this review.
