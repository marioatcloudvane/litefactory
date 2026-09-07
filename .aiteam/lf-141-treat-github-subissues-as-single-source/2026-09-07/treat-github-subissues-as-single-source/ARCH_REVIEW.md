# Architect: Go Architect
# Created: 2026-09-07
# Verdict: APPROVED FOR PLANNING

Reviewed `feature-spec.md` and this session's `RESEARCH_BRIEF.md`; story tasks below are the decomposition supplied to the product owner, because story publication bodies do not exist yet. This review writes no implementation code. Citation base: `ae5a20c104db9775facbb4c1ca648ac94709a3c6`; paths below refer to that repository revision. External platform facts are carried from this session's research, not freshly probed.

## System Overview

### How This Feature Fits Into the Existing System

The parent issue remains the task/session/worktree identity. Its same-repository, indexed direct children become complete editable story contracts. `internal/planbrief` continues to parse text without I/O; `internal/turn` reads GitHub, prepares context, coordinates publication and verifies results; `internal/store` holds durable lifecycle, immutable execution records and pending effects. `internal/daemon` discovers child activity and schedules the existing bounded parent workers. Children never allocate competing worktrees or independent implementation sessions.

Replace the existing best-effort file mirror in `internal/turn/subissues.go` with explicit Plan publication readiness. Current `internal/report/subissue.go` can discard technical content, and `internal/planbrief/storyfile.go` does not require a full contract; both must change before issue bodies can be authority. Implementation inputs flow through the existing shared prompt builder in `internal/harness/prompt.go`, which `internal/turn/prepare.go` uses for both runners. `agent.Request.Prompt` already provides a vendor-neutral stdin transport; do not duplicate context fetching inside either runner.

### New Packages / Components Introduced

No new Go package, module, service, CLI or goroutine is needed. Add durable plan/story/publication components inside `store`, a pure versioned publication contract inside `planbrief`, a vendor-neutral structured execution-outcome contract inside `agent`, orchestration inside `turn`, and bounded reconciliation through the existing `daemon` worker/tick machinery. Interfaces remain at consumers (TEST-5); `report` remains render-only and cannot import `turn`.

### Key Boundaries and Shared Decisions

1. **Authority and identity:** GitHub title/body are editable authority; SQLite snapshots are immutable evidence only. A managed identity combines registered repository identity, numeric parent/child issue IDs, stable story key and planning generation. Index membership plus confirmed direct relationship is required; a body marker or URL alone never grants execution authority. Validate repository, source identity, required sections, task ownership, dependency references and acyclic ordering before readiness. Keep unknown human content intact unless an authorized current-base proposal deliberately revises it.
2. **Parent serialization:** Existing daemon inflight key and cross-process task lock use the canonical parent for child events and parent commands. Admit at most one refinement into execution at a time; persist admission provenance before releasing the parent boundary. Promotion first waits for any admitted refinement to finish or visibly fail. Comments discovered after durable freeze are refused regardless of source timestamp. Do not add an in-memory work queue or child worker pool.
3. **Durable state:** A feature has generations, each with one selected revision set; a story has execution attempts tied to parent runs. A store transaction persists all unfinished frozen revisions and the freezing state together. Revisions digest exact fetched title/body with unambiguous encoding, alongside numeric identities and generation; `updated_at` is diagnostic only. Every required notice must be delivered before implementing starts. A retry reuses the same set. Explicit quiescent parent Plan creates a new generation for unfinished scope only; no inference from removable labels or externally reopened issues.
4. **External effects and recovery:** Persist operation intent before a network mutation, then render/redact, deliver, and transactionally record successful delivery and any associated watermark (CONC-4). Intent and approval transactions do not claim delivery or consume source comments. No DB transaction spans network or git I/O. GitHub provides no atomic transaction or exactly-once comment/create primitive: persist stable operation IDs, reconcile bot-authored markers/returned numeric identities and verified relationships after ambiguous responses, and fail visibly on ambiguity instead of blindly duplicating. Recovery executes pending effects without rerunning agents; only daemon owns retry timing (CONC-6).
5. **Agent contracts and completion:** Every cold run, resume and cold-after-resume-refusal receives exact selected revisions and source provenance. Plan proposals and implementation evidence are versioned, strict, bounded machine-readable contracts, never inferred from free-form success prose. Completion requires matching story/revision evidence, required review/tests, and durable saved/pushed code; a zero exit does not close anything. An undifferentiated failed gate blocks all stories in its run. Report completion before closing; record completed only after both effects succeed. A clean retry can reference already-pushed evidence without requiring a new commit.
6. **Security and parity:** Source bodies/comments and agent outputs remain untrusted. Trusted envelopes describe policy and delimit data; no source text enters argv, tool authorization, filesystem paths or forge targets (SEC-2, SEC-4, SEC-8, SEC-9). All outward issue bodies/notices pass `report` and redaction (SEC-5/6). Claude/Codex workflows must change together, including specialist context and review/test evidence, without widening tool permissions.

### Data Flow

Plan → parent serialization → load complete current indexed issues → give agent current revisions → validate structured proposal → persist publication intent → create/update and attach verified issues → verify full published bodies → write link-only Story Index → readiness recorded after publication and index persistence.

Child activity → discover independently of parent updates → numeric authorization → resolve managed parent → parent admission boundary → Plan refinement with child provenance OR retryable lock explanation → successful child/parent reporting → consume source comment atomically with delivery state.

Parent Implement → validate readiness/relationships → freeze all unfinished revisions transactionally → deliver notices → launch parent implementation with selected story bundle → validate per-story results/gates → save/push → completion evidence → close verified children → persist completion. Failed/unknown stories stay open and report why.

### Concurrency & Context Notes

Reuse existing bounded dispatch and clone gate; no GitHub request or agent run belongs under the clone git critical section. `turn` owns one attempt and has no clock; timestamps come from existing injected daemon/run inputs. Store takes context first, retains one SQLite connection, embedded numbered migrations, and parameterized statements. Copy collections crossing request/result boundaries (CONC-1/5). Preserve `prepare`'s final session-running write (CONC-7). Concurrent parent/child tests rendezvous on channels and `Daemon.Quiesce`, never sleeps (TEST-8/9).

### Shared State and Side Effects

Schema belongs entirely to `store`: feature/generation metadata, managed children, source comment admission and cursors, immutable revisions, execution attempts/evidence, and operation intent/delivery state. Runtime transport is bounded per run. Prefer an ignored per-run workspace handoff whose exact path is supplied by the trusted prompt; consume it before harvest and exclude it from git staging on both success and failure. Durable accepted payloads/snapshots live in SQLite so deleting a transport cannot lose retry work. Any new path under LF_HOME must be provided by `config.Paths` (CFG-8); never make an agent depend on access to daemon-private runtime directories.

Poll managed children even when closed and independently of parent `updated_at`. Initially use existing per-issue comment reads with persisted per-child progress and a deterministic bounded scan rotation; no repository-wide API is required. Retain a pending discovery flag until the entire paginated child feed is drained. Ordinary unindexed issues retain existing behavior; managed child routing precedes their ordinary issue dispatch. On retry recheck numeric identity and relationships; detach/delete/move blocks later launch/resume, while an already-running attempt retains its snapshot and reports drift.

## Contract Decisions for Implementers

- Publication proposal carries schema version, parent identity, generation, stable story keys, explicit create/update operation, expected base revision for updates, complete desired title/body, dependencies, and no caller-selected filesystem or network target. Read current issue immediately before mutation, refuse known stale proposals, and reread after writing; report the remaining unavoidable external-edit race honestly. Validate the redacted form against full-body requirements and supported API size; reject oversize instead of trimming contract sections.
- Persist publication intent and its generated recovery token before creation. Match recovery candidates by bot numeric author, token, expected repository/parent/story contract, then attach and verify relationship. Multiple matches or unclear response state blocks readiness with a diagnostic. Do not trust a marker alone, and do not duplicate an issue simply because it is not attached yet.
- Execution envelope identifies parent run, generation and each selected revision with complete body, dependency references and source URL. Return outcomes containing story/revision identity, status, acceptance-evidence references, review/test gate records and changed-path/evidence references. Require one outcome per selected story; unknown, duplicate or mismatched identities cannot authorize completion. Missing stories become incomplete, not silently successful.
- Evidence parsing is application validation of agent-produced records, not independent proof the code is correct. Require named validation results and review artifacts under the supplied worktree, validate referenced paths without following escape/symlink targets, and bind accepted evidence to harvested commit/head and successful push. Do not infer success from arbitrary untrusted artifact text, external human closure or a model's standalone “done.”
- Milestones are frozen, started, blocked/failed and completed. Started means the execution attempt has been admitted for launch, and the notice states that meaning; a launch/setup failure gets a blocked/failed update. A restart reconciles that attempt before any next dispatch. Frozen notices are launch-blocking; later milestones remain retryable records and cannot authorize scope changes.
- Treat pending report/closure reconciliation as parent work for scheduling fairness, with no new agent run. Preserve post-before-watermark even for child refusals and dual-source attribution. A parent run can finish while child delivery remains pending; surface that distinction instead of claiming complete publication.

## Task Decomposition and Architectural Hints

Task ownership is `go-implementation-engineer` for every entry per the shared task-decomposer contract. Test entries are separate deliverables and may be assigned to the rostered testing specialist by the implementation orchestrator. All estimates are at most one day. Task tables belong in their matching published stories, not in `plan_brief.md`.
### US-1

| ID | Title | Type | Depends On | Story | Est |
|---|---|---|---|---|---|
| T-1.1 | Persist managed plans, generations and publication operations | data | — | US-1 | L |
| T-1.2 | Define full story and transient Plan proposal contracts | data | T-1.1 | US-1 | M |
| T-1.3 | Add validated parent reads and full issue body updates | api | — | US-1 | M |
| T-1.4 | Render and publish complete current-base stories with recovery | logic | T-1.1, T-1.2, T-1.3 | US-1 | L |
| T-1.5 | Persist link-only index and remove transient handoff before harvest | logic | T-1.4 | US-1 | M |
| T-1.6 | Test contract, rendering and publication failure boundaries | test | T-1.2, T-1.3, T-1.4, T-1.5 | US-1 | M |
| T-1.7 | Test durable publication crash recovery | test | T-1.1, T-1.4, T-1.5 | US-1 | M |

#### T-1.1: Persist managed plans, generations and publication operations
**Agent:** go-implementation-engineer

- **Package placement / integration:** internal/store; dependencies: —.
- **Architectural hints:** Add embedded migration and context-first accessors for parent/story identity and operation intent/delivery. Preserve one connection and uniqueness across retries; later stories extend these entities rather than inventing parallel stores.
- **Boundary:** Deliver this task only; use the shared contracts above and leave other lifecycle stages to their listed tasks.

#### T-1.2: Define full story and transient Plan proposal contracts
**Agent:** go-implementation-engineer

- **Package placement / integration:** internal/planbrief; internal/agent; internal/harness; dependencies: T-1.1.
- **Architectural hints:** Keep story/index parsing pure; strict versioned proposal validation requires complete acceptance, tasks, architecture and dependencies. The trusted prompt supplies the transient handoff location; target paths and forge authority never come from model output.
- **Boundary:** Deliver this task only; use the shared contracts above and leave other lifecycle stages to their listed tasks.

#### T-1.3: Add validated parent reads and full issue body updates
**Agent:** go-implementation-engineer

- **Package placement / integration:** internal/github; dependencies: —.
- **Architectural hints:** Use existing SDK/client and pagination/rate-limit conventions. Extend the explicit method-surface test deliberately (TEST-10); no forge interface inside this package, native locking API, sleep or new module.
- **Boundary:** Deliver this task only; use the shared contracts above and leave other lifecycle stages to their listed tasks.

#### T-1.4: Render and publish complete current-base stories with recovery
**Agent:** go-implementation-engineer

- **Package placement / integration:** internal/report; internal/turn/subissues.go; dependencies: T-1.1, T-1.2, T-1.3.
- **Architectural hints:** Replace technical excerpt/truncation and marker-only reuse. Capture intent before create, recover ambiguous create/attach, compare base before update and verify afterward. Publication failure prevents readiness and is a reportable result, not a log-only warning.
- **Boundary:** Deliver this task only; use the shared contracts above and leave other lifecycle stages to their listed tasks.

#### T-1.5: Persist link-only index and remove transient handoff before harvest
**Agent:** go-implementation-engineer

- **Package placement / integration:** internal/turn/harvest.go; internal/turn/subissues.go; internal/git existing gateway; dependencies: T-1.4.
- **Architectural hints:** Restrict publication to explicit Plan; save index after confirmed issue publication using existing short clone gate. Exclude runtime transport on every save path. Do not depend on source-file permalinks or claim Plan ready before index persistence.
- **Boundary:** Deliver this task only; use the shared contracts above and leave other lifecycle stages to their listed tasks.

#### T-1.6: Test contract, rendering and publication failure boundaries
**Agent:** go-implementation-engineer

- **Package placement / integration:** internal/planbrief; internal/report; internal/github; internal/turn fixtures; dependencies: T-1.2, T-1.3, T-1.4, T-1.5.
- **Architectural hints:** Cover full content, oversize, absent sections, stale base, duplicate marker, partial attachment, API failures and no tracked transport. HTTP via httptest, no live GitHub.
- **Boundary:** Deliver this task only; use the shared contracts above and leave other lifecycle stages to their listed tasks.

#### T-1.7: Test durable publication crash recovery
**Agent:** go-implementation-engineer

- **Package placement / integration:** internal/store; internal/turn fixtures; dependencies: T-1.1, T-1.4, T-1.5.
- **Architectural hints:** Use real SQLite for uniqueness and intent/delivery transitions. Inject create success with lost response and crashes after attach/before index save; recovery must find existing identity or visibly block, never blindly create again.
- **Boundary:** Deliver this task only; use the shared contracts above and leave other lifecycle stages to their listed tasks.
### US-2

| ID | Title | Type | Depends On | Story | Est |
|---|---|---|---|---|---|
| T-2.1 | Persist child comment provenance, admission and scan progress | data | T-1.1 | US-2 | M |
| T-2.2 | Discover managed child activity and route to parent dispatch | logic | T-2.1, T-1.3 | US-2 | L |
| T-2.3 | Execute Plan refinements with child attribution | logic | T-2.2, T-1.4 | US-2 | M |
| T-2.4 | Test parent routing, authorization and ordered admission | test | T-2.1, T-2.2, T-2.3 | US-2 | L |

#### T-2.1: Persist child comment provenance, admission and scan progress
**Agent:** go-implementation-engineer

- **Package placement / integration:** internal/store; dependencies: T-1.1.
- **Architectural hints:** Store child/comment/actor/parent generation and consumed state distinctly from parent run trigger and discovery cursor. Admission is not consumption; advance only after report delivery per CONC-4.
- **Boundary:** Deliver this task only; use the shared contracts above and leave other lifecycle stages to their listed tasks.

#### T-2.2: Discover managed child activity and route to parent dispatch
**Agent:** go-implementation-engineer

- **Package placement / integration:** internal/daemon/tick.go; internal/daemon dispatch; dependencies: T-2.1, T-1.3.
- **Architectural hints:** Scan tracked children including closed, paginate deterministically and use parent inflight key. Numeric owner checks precede instruction admission. Distinguish pending refinement from ordinary parent discovery; no child worktree or competing session.
- **Boundary:** Deliver this task only; use the shared contracts above and leave other lifecycle stages to their listed tasks.

#### T-2.3: Execute Plan refinements with child attribution
**Agent:** go-implementation-engineer

- **Package placement / integration:** internal/turn/prepare.go; internal/report; internal/turn reporting; dependencies: T-2.2, T-1.4.
- **Architectural hints:** Provide parent plan and targeted current child context while preserving source comment URL/ID. Acknowledge/report on child with parent run reference; publication uses current-base proposal contract. Refusals and failed reports leave source retryable.
- **Boundary:** Deliver this task only; use the shared contracts above and leave other lifecycle stages to their listed tasks.

#### T-2.4: Test parent routing, authorization and ordered admission
**Agent:** go-implementation-engineer

- **Package placement / integration:** internal/daemon/fixture_test.go; internal/turn/fixture_test.go; dependencies: T-2.1, T-2.2, T-2.3.
- **Architectural hints:** Prove unrelated/marker-spoofed issues do not inherit authority, renamed owner numeric identity works, parent/child simultaneous activity serializes and undelivered child report does not advance cursor. Real SQLite plus channels/Quiesce, no sleep.
- **Boundary:** Deliver this task only; use the shared contracts above and leave other lifecycle stages to their listed tasks.
### US-3

| ID | Title | Type | Depends On | Story | Est |
|---|---|---|---|---|---|
| T-3.1 | Persist atomic approval sets and lifecycle transitions | data | T-1.1, T-2.1 | US-3 | M |
| T-3.2 | Validate promotion and deliver required freeze notices | logic | T-3.1, T-1.4, T-2.3 | US-3 | L |
| T-3.3 | Enforce frozen comment policy and explicit replanning | logic | T-3.2, T-2.2 | US-3 | L |
| T-3.4 | Test approval races, refusal retry and generation recovery | test | T-3.1, T-3.2, T-3.3 | US-3 | L |

#### T-3.1: Persist atomic approval sets and lifecycle transitions
**Agent:** go-implementation-engineer

- **Package placement / integration:** internal/store; dependencies: T-1.1, T-2.1.
- **Architectural hints:** Store whole unfinished revision set and freezing state in one transaction; enforce immutable snapshots and generation transition guards. Reuse child admission to prevent promotion overtaking accepted refinements.
- **Boundary:** Deliver this task only; use the shared contracts above and leave other lifecycle stages to their listed tasks.

#### T-3.2: Validate promotion and deliver required freeze notices
**Agent:** go-implementation-engineer

- **Package placement / integration:** internal/turn preparation; internal/report; dependencies: T-3.1, T-1.4, T-2.3.
- **Architectural hints:** Revalidate all indexed direct relationships and complete issue contracts before persisting approval. Post each notice from stable pending operation and block launch until all are delivered; preserve prepare session-state invariant.
- **Boundary:** Deliver this task only; use the shared contracts above and leave other lifecycle stages to their listed tasks.

#### T-3.3: Enforce frozen comment policy and explicit replanning
**Agent:** go-implementation-engineer

- **Package placement / integration:** internal/daemon mode admission; internal/turn; internal/report; dependencies: T-3.2, T-2.2.
- **Architectural hints:** Durable lifecycle overrides temporary labels for managed scope. Refuse authorized late comments even on closed stories; deduplicate by source comment. Quiescent explicit parent Plan opens only unfinished next-generation scope; active implementation and external reopening cannot unfreeze.
- **Boundary:** Deliver this task only; use the shared contracts above and leave other lifecycle stages to their listed tasks.

#### T-3.4: Test approval races, refusal retry and generation recovery
**Agent:** go-implementation-engineer

- **Package placement / integration:** internal/store; internal/daemon and turn fixtures; dependencies: T-3.1, T-3.2, T-3.3.
- **Architectural hints:** Use real SQLite and rendezvous to cover admitted refinement vs promotion, comments discovered after freeze, partial notice success/restart, cancellation before session running and completed-story exclusion from replanning.
- **Boundary:** Deliver this task only; use the shared contracts above and leave other lifecycle stages to their listed tasks.
### US-4

| ID | Title | Type | Depends On | Story | Est |
|---|---|---|---|---|---|
| T-4.1 | Assemble selected issue revisions into execution context | logic | T-3.2 | US-4 | M |
| T-4.2 | Refresh revision context across resume and fallback paths | logic | T-4.1 | US-4 | M |
| T-4.3 | Update both implementation and specialist input workflows | logic | T-4.1, T-4.2 | US-4 | M |
| T-4.4 | Test cold, resumed and fallback revision parity | test | T-4.1, T-4.2, T-4.3 | US-4 | M |

#### T-4.1: Assemble selected issue revisions into execution context
**Agent:** go-implementation-engineer

- **Package placement / integration:** internal/turn/prepare.go; internal/harness/prompt.go; internal/agent; dependencies: T-3.2.
- **Architectural hints:** Load immutable approved story bundle, dependency context and parent identity. Revalidate live relationships before dispatch without substituting edited bodies. Both runners consume the same stdin prompt and no runner gains GitHub access.
- **Boundary:** Deliver this task only; use the shared contracts above and leave other lifecycle stages to their listed tasks.

#### T-4.2: Refresh revision context across resume and fallback paths
**Agent:** go-implementation-engineer

- **Package placement / integration:** internal/turn/harvest.go; internal/harness/prompt.go; dependencies: T-4.1.
- **Architectural hints:** Explicitly repeat selected revisions for resume and cold-after-resume-refusal; retain generation on retries. Report body drift, block detached/deleted relationships, and never consult legacy story files as fallback.
- **Boundary:** Deliver this task only; use the shared contracts above and leave other lifecycle stages to their listed tasks.

#### T-4.3: Update both implementation and specialist input workflows
**Agent:** go-implementation-engineer

- **Package placement / integration:** .claude/agents; .codex/skills; .claude/modes; AGENTS.md and CLAUDE.md relevant contracts; dependencies: T-4.1, T-4.2.
- **Architectural hints:** Orchestrator assigns each specialist its selected GitHub-derived story and revision from runtime context. Remove live per-story file assumptions in implementation/review/testing. Preserve exact mode-header coordination (STYLE-8) and tool boundaries.
- **Boundary:** Deliver this task only; use the shared contracts above and leave other lifecycle stages to their listed tasks.

#### T-4.4: Test cold, resumed and fallback revision parity
**Agent:** go-implementation-engineer

- **Package placement / integration:** internal/turn/resume_test.go; internal/harness prompt tests; internal/codex runner fixtures; dependencies: T-4.1, T-4.2, T-4.3.
- **Architectural hints:** Prove changed live bodies and stale conversation history cannot replace approved data on either runner. Exercise relationships changing between attempts, hostile source text and context collection isolation; no real coding-agent subprocess.
- **Boundary:** Deliver this task only; use the shared contracts above and leave other lifecycle stages to their listed tasks.
### US-5

| ID | Title | Type | Depends On | Story | Est |
|---|---|---|---|---|---|
| T-5.1 | Define structured story evidence and persist execution attempts | data | T-3.1, T-4.1 | US-5 | M |
| T-5.2 | Add completed issue state update to GitHub adapter | api | T-1.3 | US-5 | S |
| T-5.3 | Validate evidence and emit story milestones | logic | T-5.1, T-4.2 | US-5 | L |
| T-5.4 | Reconcile completion reporting and closure without reexecution | logic | T-5.2, T-5.3, T-1.1 | US-5 | L |
| T-5.5 | Test malformed evidence and successful/partial story outcomes | test | T-5.1, T-5.2, T-5.3 | US-5 | M |
| T-5.6 | Test milestone/closure recovery across restarts | test | T-5.4, T-3.3 | US-5 | M |

#### T-5.1: Define structured story evidence and persist execution attempts
**Agent:** go-implementation-engineer

- **Package placement / integration:** internal/agent; internal/store; dependencies: T-3.1, T-4.1.
- **Architectural hints:** Strict versioned outcome contract identifies selected story/revision, AC evidence and gates. Persist run/revision/attempt/evidence and milestone intents. Keep process outcome independent from story verification.
- **Boundary:** Deliver this task only; use the shared contracts above and leave other lifecycle stages to their listed tasks.

#### T-5.2: Add completed issue state update to GitHub adapter
**Agent:** go-implementation-engineer

- **Package placement / integration:** internal/github; dependencies: T-1.3.
- **Architectural hints:** Add minimal issue-close capability with completed reason via existing client; check response, errors and rate-limit classification. No reopen/lock/unlock capability is required by this spec.
- **Boundary:** Deliver this task only; use the shared contracts above and leave other lifecycle stages to their listed tasks.

#### T-5.3: Validate evidence and emit story milestones
**Agent:** go-implementation-engineer

- **Package placement / integration:** internal/turn/harvest.go; internal/report; both implementation orchestrator copies; dependencies: T-5.1, T-4.2.
- **Architectural hints:** Reject missing/unknown/duplicate/mismatched identities; require named AC/review/test evidence bound to approved revision and saved/pushed head. Feature-wide gate failure conservatively blocks all without finer evidence. Distinguish launch-admitted started notice from proven running process.
- **Boundary:** Deliver this task only; use the shared contracts above and leave other lifecycle stages to their listed tasks.

#### T-5.4: Reconcile completion reporting and closure without reexecution
**Agent:** go-implementation-engineer

- **Package placement / integration:** internal/turn reporting; internal/daemon pending work scheduling; internal/store; dependencies: T-5.2, T-5.3, T-1.1.
- **Architectural hints:** Completion comment precedes close; completed state follows both. Retry pending effects using stored evidence even after original run finishes. Handle remote close success/local-write failure by observing intended state; human closure alone never verifies.
- **Boundary:** Deliver this task only; use the shared contracts above and leave other lifecycle stages to their listed tasks.

#### T-5.5: Test malformed evidence and successful/partial story outcomes
**Agent:** go-implementation-engineer

- **Package placement / integration:** internal/agent; internal/github; internal/report; internal/turn fixtures; dependencies: T-5.1, T-5.2, T-5.3.
- **Architectural hints:** Cover process success with no evidence, bad revision IDs, duplicate entries, failed gates, missing push, already-pushed clean retry, scoped partial success and failed subprocess. Use fake runner and real git fixture.
- **Boundary:** Deliver this task only; use the shared contracts above and leave other lifecycle stages to their listed tasks.

#### T-5.6: Test milestone/closure recovery across restarts
**Agent:** go-implementation-engineer

- **Package placement / integration:** internal/store; internal/daemon and turn fixtures; dependencies: T-5.4, T-3.3.
- **Architectural hints:** Real SQLite and httptest exercise report failure, successful comment with lost acknowledgement, close failure, close success before local commit, and restart. Assert no additional agent run and no lost refusal on completed child.
- **Boundary:** Deliver this task only; use the shared contracts above and leave other lifecycle stages to their listed tasks.
### US-6

| ID | Title | Type | Depends On | Story | Est |
|---|---|---|---|---|---|
| T-6.1 | Reconcile legacy file bodies with current child contracts | logic | T-1.4 | US-6 | L |
| T-6.2 | Remove confirmed migrated live story files and references | logic | T-6.1, T-1.5 | US-6 | M |
| T-6.3 | Align Plan publication instructions and persistent context documentation | logic | T-6.2, T-4.3, T-5.3 | US-6 | M |
| T-6.4 | Test safe migration and end-to-end workflow parity | test | T-6.1, T-6.2, T-6.3, T-5.6 | US-6 | L |

#### T-6.1: Reconcile legacy file bodies with current child contracts
**Agent:** go-implementation-engineer

- **Package placement / integration:** internal/planbrief; internal/turn/subissues.go; dependencies: T-1.4.
- **Architectural hints:** Use indexed current file and existing provenance only; compare full technical content and human changes. Reuse current-base publication protocol. Missing source, competing edits, missing relationship or uncertain origin blocks migration with explicit evidence; never fetch arbitrary local paths from issue text.
- **Boundary:** Deliver this task only; use the shared contracts above and leave other lifecycle stages to their listed tasks.

#### T-6.2: Remove confirmed migrated live story files and references
**Agent:** go-implementation-engineer

- **Package placement / integration:** internal/turn worktree harvesting; internal/planbrief; dependencies: T-6.1, T-1.5.
- **Architectural hints:** Only remove files confirmed as this active plan’s sources after full issue reread matches publication. Preserve historical commits/links. Recover post-publication/pre-removal crash without republishing stale data; do not bulk-migrate archived sessions.
- **Boundary:** Deliver this task only; use the shared contracts above and leave other lifecycle stages to their listed tasks.

#### T-6.3: Align Plan publication instructions and persistent context documentation
**Agent:** go-implementation-engineer

- **Package placement / integration:** .claude and .codex instruction copies; .aiteam/APP_CONTEXT.md; relevant README/docs; dependencies: T-6.2, T-4.3, T-5.3.
- **Architectural hints:** Replace tracked story authoring with transient publication contract and index-only artifacts; cover proposal source revisions, readiness failure, lock semantics, milestones and migration. Update stale inventory prose without weakening normative redaction, method surfaces or security policy.
- **Boundary:** Deliver this task only; use the shared contracts above and leave other lifecycle stages to their listed tasks.

#### T-6.4: Test safe migration and end-to-end workflow parity
**Agent:** go-implementation-engineer

- **Package placement / integration:** internal/planbrief; internal/turn and daemon fixtures; dependencies: T-6.1, T-6.2, T-6.3, T-5.6.
- **Architectural hints:** Prove truncated legacy restoration, human-addition preservation, ambiguous refusal, publish-before-removal, restart and old historical links. Exercise complete Plan→child refinement→freeze→resume→verify→close with both fake runners and no tracked story transport.
- **Boundary:** Deliver this task only; use the shared contracts above and leave other lifecycle stages to their listed tasks.

## Platform Constraints

- Platform: Layout and interfaces — reuse internal packages and consumer-side interfaces; no public package or new service.
- Platform: Errors — expected rejected/failed work is a reportable result (CONC-3); setup/persistence failures are wrapped typed errors, with rate limits returned to daemon. Check every returned error.
- Platform: Concurrency — existing bounded workers only, context propagation and copied collections; preserve clone-gate and session-running contracts.
- Platform: Database — new embedded migration files, SQL only in store, one SQLite connection, no migration or assertion dependencies (TEST-2/6).
- Platform: Dependencies — no new module required; existing SDK handles transport. Read-only official documentation compatibility verification belongs to the API implementation tasks before claiming endpoint support; fixtures alone do not establish live permissions/version compatibility.
- Platform: HTTP listener, router and service shutdown — not applicable (no new service).
- Platform: Logging — injected slog with parent/story/run/revision/operation identifiers; no sensitive source bodies in routine diagnostics.
- Platform: Testing and lint — existing stdlib fixtures, httptest, real SQLite and gittest. Run `make check` and `go build ./...`; no invented integration tags or live agent/API tests. Do not use `make build` or modify dist/runtime state.

### Security Findings

**Scope:** Proposed data flow and inspected package contracts, not a repository-wide vulnerability audit.
**Rulebook:** `.aiteam/APP_CONSTRAINTS.md` security section (SEC); shared scanner generic principles supplement its topic checklist.

| # | Area | Status | Finding |
|---|---|---|---|
| 1 | Input validation | ⚠️ concern | Current story parser allows incomplete bodies; US-1 must validate full published contracts, dependencies, bounded proposals and managed numeric identities before readiness. |
| 2 | Authentication and authorization | ⚠️ concern | Current child comments dispatch independently; US-2/3 must preserve numeric owner checks and verified relationship admission before parent routing. |
| 3 | Secrets/configuration | ✅ compliant design | Existing token resolution and runner environment remain; agent never needs forge credentials. No permission expansion or new secret config. |
| 4 | Data handling | ⚠️ concern | New body writes and notices require redaction and safe path/reference validation; immutable runtime records cannot become tracked specifications. |
| 5 | Dependencies/supply chain | ✅ compliant design | No new dependency proposed. No fresh vulnerability audit performed; do not construe this as a clean dependency scan. |
| 6 | Error handling | ✅ compliant design | Failures remain actionable report values; pending state survives API errors without cursor advance or blind duplicate mutation. |

Highest-priority safeguards are canonical parent authorization, complete contract validation and post-before-consumption recovery. GitHub editor races remain an explicit platform limitation, not solved by application checks.

## Deviations, Documentation Drift and Review Gates

**DEVIATION: none requested.** The plan does not require weaker secrets, authorization, redaction, transaction, goroutine or clone boundaries. No amendment ledger is written because no normative departure is proposed.

Existing research documented stale SEC-6/API inventory: the rulebook describes a sole PostComment consumer and no exported extraction, whereas current code also creates redacted issues and exposes Body.String. Do not silently treat that stale wording as permission to add an unredacted write route or further extraction methods. US-6 documentation work must accurately describe the existing redacted transport boundary; any actual relaxation of normative security is a separate deviation and remains prohibited. The implementation reviewer must specifically inspect new body update/close method-surface changes under TEST-10. Expected behavior changes to file mirroring, truncation and close-discovery tests must be explained as this feature's intentional design changes.

Planning assumptions in the feature spec are adopted: application lock with visible refusal, all unfinished stories frozen at parent promotion, completion after saved/pushed evidence without merge, explicit quiescent replanning of unfinished scope, and no cross-repository/nested execution. No specialist outside Go backend architecture is required; workflow prose follows the established mirrored instructions and reviewable contracts.

Architecture artifact complete; no code, GitHub mutation, commit or push was performed. Testing described here is planned, not executed.
