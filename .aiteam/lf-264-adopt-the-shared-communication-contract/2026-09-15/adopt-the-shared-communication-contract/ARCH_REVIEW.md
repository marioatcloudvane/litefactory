# Architect: Go Architect
# Created: 2026-09-15

## Verdict

Ready for typed Plan synthesis. No standard deviation is required. Production adoption and compatibility retirement are separate milestones; retirement remains conditional on factual cohort/effect evidence. This review is source inspection only: no tests, live database inventory, provider captures or runtime changes occurred.

## System Overview

### How This Feature Fits

Reuse `internal/agentresult` for business-result types and merged validation, `internal/turn/agent_results.go` for scoped application service operations, and `internal/store` for revisions, receipts and durable authority. `internal/submission` and `internal/mcpadapter` remain transport owners. `internal/harness` and `internal/codex` remain native subprocess adapters. `internal/daemon` composes owners and owns scheduling/time; `internal/turn` performs one authorized attempt and its effects. `internal/report` renders outbound values without importing turn.

Current `runSubmissionBound` calls `runMCPBound`; the latter returns runner output directly and skips bindings for unavailable/disabled MCP and unmanaged/legacy engineering. Shared final-result ownership must sit above optional native MCP transport so those branches cannot silently escape the completion policy. Preserve the configured native auto-start option: disabling MCP selects a supported helper/terminal/compatibility route, not implicit success or compulsory native startup.

`AgentResultService.prepare` already refuses a publishable Plan under MCP-specific context. Preserve that authority boundary. A helper typed Plan contribution must reach shared semantic result validation and existing Plan callbacks/publication; enabling the application result service must not make `communication_submit` a new story-creation bypass. Research creation remains a separately gated, exactly approved operation.

### New Packages / Components

No new top-level package or MCP server is needed. Add narrowly scoped lifecycle/composition helpers in existing owners if needed. No schema addition is prescribed: reuse result, submission, publication, creation and continuation records first. Any demonstrated missing durable work enumeration belongs in `internal/store`, with an additive embedded migration only when required.

### Key Boundaries

- **Attempt ownership:** factory-selected identity, context hash, persisted versions, frozen story revisions and grants become a bound shared result service before an actual launch. Adapters receive only scoped descriptors/context; they do not construct authority.
- **Application versus workflow:** shared results supply final outcome, questions, comment and artifact references. `SubmissionSession`, Plan publication, StoryProgress and acceptance retain their existing typed workflow authority. Receipts link these outcomes; input acknowledgment cannot substitute for engineering, acceptance, publication or save.
- **Final processing:** normalize applicable terminal output and live/helper input through the same service/workflow validators; then enforce final completeness before success-only save/report gates. Final diagnostics remain reportable data, distinct from transport failure and original subprocess cause.
- **Validate:** `submission_runner.go` terminal assessment/reconciliation remains a valid reference-only route. It cannot acquire shell, new test execution, snapshot capture or delegation to satisfy finalization. `validation.go` legacy parsing is a separately gated retirement target.
- **External effects:** `IssuePublicationService` and `CreationPublicationService` receive explicit target/grant authority, forge consumers, redaction and numeric bot identity. Durable effect intent precedes dispatch. Daemon-driven observation calls existing reconciliation APIs after uncertain replies; it never blindly repeats a write.
- **Progress:** `ActivityService` remains process-local advisory state. Persistent receipts and pending effects outlive activity lease cleanup. Discovery and calls enforce the same mode/profile grants.

### Data Flow

1. Daemon admits current owner intent and composes configured owners → turn binds fresh result/workflow attempt → service reads authorized baseline and history scope → prompt/descriptor exposes complete editable state with stable IDs and revision.
2. MCP/helper/applicable terminal adapter → shared decode/merge/validation → retained result and receipt → applicable existing workflow checkpoint/publication intent → post-exit semantic final gate → existing save and confirmed report/effect settlement.
3. Uncertain external response → durable pending operation → daemon schedules bounded observation → reconciliation confirms remote provenance/content or leaves pending → receipt projection updates independently of the exited attempt.
4. Run ends, cancels or is replaced → progress terminal transition → revoke live capability and close broker → retain durable recovery records → daemon-owned bounded retention cleanup.

### Concurrency & Context

Use the existing parent/task/attempt ownership and broker worker limits. Do not hold SQL transactions over subprocess, sockets, output or forge I/O. Preserve the one-connection SQLite pool. Consumers declare narrow interfaces (TEST-5); copy nested baseline/scope collections (CONC-1). All blocking access takes context first (CONC-5). Daemon owns clocks/backoff (CONC-6), while turn consumes supplied time/lifecycle values. Terminal cleanup must remain bounded even after request cancellation; preserve the existing prohibition on uncancelled preparation state changes (CONC-7). Any new goroutine needs a documented bounded lifetime (CONC-2); a new polling loop in turn is inappropriate.

### Shared State and Side Effects

Retain immutable checkpoints and aggregates, independently projected receipt dimensions, retired result IDs, approved scope and workflow versions. Success-only saves and current outcome predicates remain distinct for live versus persisted runs (CONC-8, STYLE-10). Final result policy must not erase quota interruption/continuation facts or let valid input turn failure into success (CONC-13/14). Historical accepted snapshots and workflow-v1 completion behavior survive input-format cutover (CONC-10/11). Do not delete migrations to remove an ingress path.

## Cross-story Decisions

1. Bind shared completion independently of MCP enablement; every supported launch gets a declared final route. Preserve owner-controlled typed Plan publication and existing workflow services rather than implementing acceptance in adapters.
2. Prefer a complete authorized launch snapshot plus explicitly scoped historical receipt reads for first adoption; a new result-read tool is optional only if needed for bounded state delivery. Never treat prior prose or provider session reuse as the baseline.
3. Share authoritative aggregate/checkpoint resolution between preflight, submit and final enforcement. `ValidateAgentResultCompletion` and `ResultAggregateValidator` are the existing owners; final worktree comparison must occur before accepting claimed engineering completion.
4. Daemon composes publication/creation owners, trusted intake, bounded reconciliation and progress cleanup. Turn owns attempt-lifetime transitions, services and receipt projection; transports contain no independent business validation.
5. Shared canonical instruction work precedes two independent protected-tree stories. Codex writes `.claude/**`; Claude writes `.codex/**`. Scoped checks require only the owned tree; aggregate parity waits for both. Do not modify immutable releases or use a generator that writes both trees.
6. Inventory/delete dead discovery separately from compatibility readers. Keep compatibility retirement incomplete until supported cohort, upgrades/completions and pending-effect drain are evidenced. #274/#296 retain authentic host observations; offline tests do not close that evidence gap.

## Suggested Delivery Boundaries and Per-task Hints

These are architectural task groups for the orchestrator to translate into typed stories, not additional requirements. Keep each implementation task at most one day; split within a group where needed. Use the supplied engineering/test roster for typed tasks. The platform decomposer's generic single-role assumption does not replace that supplied roster.

### A — Shared completion and adapter cutover

**Agent:** go-implementation-engineer; separate focused test tasks to go-unit-test-engineer/go-integration-test-engineer as appropriate.

- **Pattern:** factory-owned lifecycle around existing service and stage callbacks.
- **Package placement:** `internal/turn/submission_runner.go`, `mcp_runtime.go`, `agent_results.go`, `plan_attempt.go` and `plan_handoff.go`, `internal/submission`, `internal/cli/cmd_submission.go`, `internal/agent`; native adapters only for their actual structured-output seam.
- **Key interfaces:** result service preflight/submit/receipt/observe, `SubmissionSession`, `agent.RunResult`, existing Plan and Validate typed processors.
- **Tasks:** establish binding/final route policy; adapt helper and terminal routing; connect post-exit result-to-report/workflow gate; add production-path matrix checks.
- **Watch out for:** nominal exit with absent output fails required finalization; failure/cancellation/interruption preserve primary cause and checkpoint/recovery facts. Reject malformed/conflicting live versus terminal findings without partial acceptance. Do not demand redundant agent submissions for the same authoritative contribution.
- **Evidence:** production constructor/runner paths for five modes, MCP on/off, supported compatibility routes, cold/resume/fallback, rejected/missing output and all termination cases. Existing `submission_reconciliation_test.go` is a useful live/terminal seam. SEC-20/21, CONC-8/11/22/23, EXT-3.

### B — Prior results, sparse edits and evidence-backed completion

**Agent:** go-implementation-engineer, with separate unit and real-store test tasks.

- **Pattern:** authorized projection and immutable evidence lookup, never reconstruction from narrative.
- **Package placement:** `internal/turn/mcp_guidance.go`, `approved_context.go`, `family_receipts.go`, `agent_results.go`, `agent_result_aggregates.go`, `internal/agentresult`, existing result/evidence store accessors.
- **Key interfaces:** `AgentResultService.Read`, `OpenAgentResultReader`, `NewReceiptService` historical scopes, `ValidateAgentResultCompletion` and aggregate capture/validator owners.
- **Tasks:** supply complete bounded editable baseline/empty initial state; grant scoped historical receipts; wire checkpoint/aggregate enrichment; check merge/stale/replay/restart isolation.
- **Integration:** depends on A's shared binding ownership. Completion authority must be wired before A is declared feature-complete; A may expose a reusable seam first.
- **Watch out for:** current runtime context sets `RequireAggregate` but omits the resolver; do not relax validation to make it pass. Retained history does not grant a writer, and a replaced attempt cannot regain mutation authority by querying a receipt. SEC-20/21, CONC-1/10/12.

### C — Authorized MCP publication and recovery

**Agent:** go-implementation-engineer; real-store/HTTP test tasks to go-integration-test-engineer.

- **Pattern:** compose existing owners plus durable observed-effect recovery.
- **Package placement:** `internal/daemon/setup.go`, `mcp_creation.go`, owner-comment admission and tick/recovery owners; `internal/turn/mcp_runtime.go`, `mcp_creation.go`, `mcp_families.go`, `issue_publication.go`, `creation_publication.go`; existing store creation/comment records.
- **Key interfaces:** `AdmitMCPOwnerCommand`, `CreationAuthorizer`, retained proposal preparation, `BindCreationGrant`, publication registrations/projections, `ReconcileComment`, creation `Reconcile`.
- **Tasks:** wire authorized comment targets; present/retain exact creation proposals and authenticated owner grants; install loaded creation gate/eligible handler; enumerate and reconcile pending operations through daemon scheduling; verify production composition/restart.
- **Watch out for:** ordinary comments do not require the creation gate. Grants include exact relationship and approved bytes, not authority inferred from issue prose. Bind policy before permission digest/catalog freeze; creation is Research-only. Tests that hand-assemble a larger catalog do not prove production availability. Numeric identity and redacted mutations remain mandatory (SEC-6/8/20, CONC-4/6/10).

### D — Advisory progress lifetime

**Agent:** go-implementation-engineer, separate lifecycle test task to go-unit-test-engineer.

- **Package placement:** `internal/turn/activity.go`, `mcp_runtime.go`, `internal/submission/family_tools.go`, daemon cleanup scheduling.
- **Key interfaces:** retain activity owner in prepared attempt state; `ActivityService.Lifecycle`, `HandleUpdate`, `HandleGet`, existing attempt guard/revocation.
- **Tasks:** expose scoped read/update for eligible Research/Spike and existing profiles; connect terminal/replacement/expiry/cleanup; cover refused stale calls and durable-effect preservation.
- **Watch out for:** add only the required Spike activity permissions, not every family operation. Immutable family catalog version/digest must reflect changed call permissions. Activity terminal labels cannot set application/engineering/acceptance completion. Use deterministic channel-based lifetime checks, never test sleeps (TEST-8, CONC-2/6).

### E — Shared instructions, auxiliary conventions and current documentation

**Agent:** go-implementation-engineer; separate scoped source/render check task where warranted.

- **Package placement:** `teams/source/skills/agent-finalization/`, `workflow/`, `research-evidence/`, planning/review contracts and affected role sources; root guidance; `docs/story-contract-instruction-manifest.md`, `docs/agent-result-contract.md`, relevant current capture guidance. Auxiliary owners include `internal/turn/classify.go`, `internal/daemon/schedules.go`, `internal/schedule`, `internal/agent/conflict.go`, `internal/turn/conflict.go` and provider-owned parsing.
- **Tasks:** define actual final route/prior state/rejection/receipt guidance; map classifier/scheduler/conflict version/error conventions or justified exclusions; update source examples and handoff manifest; check in-memory native renders and focused typed auxiliary errors.
- **Watch out for:** do not force typed auxiliary payloads or vendor stdout into application results. No new dependency or protocol rewrite. Preserve mode-header authority. Declare required installed changes explicitly; native rendering does not byte-generate legacy installed copies. Shared work has no dependency on either protected-tree sibling. STYLE-1/8, CONC-23, EXT-4/5.
- **Dependency:** settled A–D routes inform final source guidance; shared-source work still precedes either installed-tree story.

### F — Claude installed instructions

**Agent:** go-implementation-engineer running under Codex.

- **Package placement:** only affected `.claude/agents/**`, `.claude/skills/**` and `.claude/modes/**` paths enumerated by E's manifest.
- **Tasks:** integrate the shared adopted contract, preserve harness wrapper/tool restrictions, run owned-tree source assertions.
- **Dependency:** E only for instruction source; never G. Do not write `.codex/**` or require its parity as a scoped check.

### G — Codex installed instructions

**Agent:** go-implementation-engineer running under Claude.

- **Package placement:** only affected `.codex/skills/**` paths enumerated by E's manifest.
- **Tasks:** integrate identical semantic guidance with Codex wrappers, preserve Validate read-only contract, run owned-tree source assertions.
- **Dependency:** E only for instruction source; never F. Codex cannot complete this story by editing shared source again or requesting wider permissions.

### H — Verified dead discovery removal

**Agent:** go-implementation-engineer; focused live-helper regression task to go-unit-test-engineer.

- **Package placement:** `internal/turn/subissues.go`, exclusive discovery callers/options/rendering/tests, exact owner references discovered during implementation.
- **Tasks:** confirm always-taken production PlanForge gate and exclusive call graph; remove unreachable Markdown discovery/wiring/tests; retain live helper regression coverage.
- **Watch out for:** retain `PublishPlan`, `RenderCompleteStory`, link-only indexes and any shared publication helpers. Whole-file deletion without caller ownership proof is unsafe. This removal need not wait for live legacy scope drain; do not confuse it with I. TEST-10, CONC-12.

### I — Compatibility reader retirement

**Agent:** go-implementation-engineer; real SQLite compatibility/restart tasks to go-integration-test-engineer.

- **Package placement:** legacy branches of `internal/turn/plan_handoff.go`, event-directory ingress in `approved_context.go`/`story_progress.go`, `validation.go` legacy parser path, their exclusive prompt/options/parser wiring; supported upgrade/admission owners in turn/store/CLI.
- **Tasks:** record per-path consumer/version/cohort/effect inventory; use supported upgrade/completion evidence where required; remove each proven unnecessary ingress separately; verify older database history, pending recovery and remaining workflow-v1 semantics.
- **Dependency:** adopted replacement routes and baseline/evidence behavior (A/B); verified runtime cohort/effect prerequisite before deletion, not merely before reporting completion.
- **Watch out for:** preserved content/submission upgrade does not upgrade workflow. Keep StoryProgress records/recovery/delivery, v2 internal Plan handoffs/publication and historical reads/migrations. No hand-edit of daemon DB/config. If inspection/access or operator-controlled drain is unavailable, keep the affected removal criterion incomplete and return the concrete focused follow-up/scope revision required by the approved spec. Fixtures cannot prove an actual supported cohort drained. CONC-10/11/12/22.

### J — Aggregate adoption/retirement verification

**Agent:** engineering/test roster according to actual check ownership; Validate later assesses retained evidence only.

- **Tasks:** retain executed production-composition and failure-matrix checks; compare both installed instruction contracts after F/G; verify each old-path disposition against actual diff and inventory; run `make check` and confirm formatting.
- **Dependencies:** A–I for full feature completion. Record adoption independently if I remains blocked; do not equate milestone status with all-feature acceptance.
- **Evidence:** use existing turn real-git/migrated-SQLite/fake-runner and daemon controlled-HTTP fixtures (TEST-3/6). No build tags or specially named integration tests; no live paid providers. Explicitly distinguish source/synthetic proof from #274/#296 authentic observations (EXT-4/5). No tests were executed by this architecture review.

## Platform Notes

- Platform: layout/interfaces — existing internal owners, consumer-defined narrow interfaces, no speculative shared package (TEST-5, STYLE-3).
- Platform: errors — typed diagnostics and recorded operation outcomes; wrap infrastructure errors without masking primary causes (CONC-3, STYLE-5).
- Platform: concurrency — attempt-bounded workers, explicit cleanup, copied scopes and daemon-owned time (CONC-1/2/5/6).
- Platform: dependencies — no new module, router, migration framework or assertion library (TEST-2).
- Platform: HTTP/API — existing MCP broker and forge adapter, no new listener/server; keep current cancellation and transport limits.
- Platform: database — internal/store only, parameterized contextual access, single connection and embedded additive migrations when needed; no I/O-spanning transaction.
- Platform: tests — table-driven stdlib tests and hand-written fakes; real SQLite for transaction promises; project `make check` is the mandatory gate (TEST-1/3/6/8).
- Platform: logging — injected slog and sanitized outbound diagnostics; do not leak descriptors/grants/credentials or add observability infrastructure (SEC-6, STYLE-2).
- Platform: lint/format — pinned project lint plus gofmt; surface/purity/clock/gate failures require design review (TEST-10).

### Security Findings

Scope: forward-looking adoption architecture, not an audit or verified implementation.
Rulebook: `.aiteam/APP_CONSTRAINTS.md`.

| Area | Status | Plan implication |
|---|---|---|
| Input validation | Concern | Adapters must converge on full merged validation and immutable evidence rather than independently accepting partial payloads. |
| Authentication/authorization | Concern | New production publication wiring must retain numeric owner authority, exact content/targets and attempt replacement refusal. |
| Secrets/configuration | Compliant design | Existing private descriptors, factory-held forge credentials and loaded gates suffice; no new secret path is required. |
| Data handling | Concern | Baseline/history projection must stay scoped and bounded; rooted evidence and redacted outbound mutation boundaries remain. |
| Dependencies | Compliant design | Existing stack suffices; no new dependency is proposed or vulnerability certification claimed. |
| Error handling | Concern | Post-exit rejection, uncertain remote outcome and cancellation must remain visible/resumable without fabricated success. |

## Deviations

None. Operator evidence prerequisites and opposite-harness instruction work are explicit execution dependencies, not permission exceptions or a reason to weaken frozen criteria.
