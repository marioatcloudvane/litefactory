# Architect: Go Architect
# Created: 2026-09-09

## Verdict

APPROVED for story decomposition. No unresolved architectural blocker. Owner-authorized policy changes below must be recorded in the shared-document implementation story; they are not requests for another approval. This review addresses the feature spec before the product owner assigns final task identifiers. The capability/task hints below must be carried into each corresponding story's Architecture section.

## System Overview

### How This Feature Fits Into the Existing System

Preserve the current package direction. `internal/daemon` admits trusted owner actions and schedules recovery. `internal/turn` orchestrates one action, immutable context, decision ingestion and ordered forge delivery. `internal/store` owns workflow versions, engineering state, acceptance decisions, approvals and durable effect intent. `internal/agent` owns vendor-neutral runner values; `internal/harness` and `internal/codex` carry equivalent context and results. `internal/github` alone calls GitHub; `internal/report` renders sanitized values; `internal/planbrief` owns pure criterion parsing/body projection.

Replace the new-workflow meaning of engineering completion, not the legacy implementation in place. Current `agent.StoryEvidence.Validate` demands passing acceptance, review and tests together; current `prepareManagedScope` admits only Implement after approval. New engineering evidence and Validate results need separate contracts and dispatch branches. Existing immutable story revisions remain the source of scope on cold, resumed and fallback attempts. Acceptance is an historical decision about that supplied snapshot, not a lease invalidated by subsequent work.

### New Components

No new service, database, module or top-level package is needed. Add cohesive components within current owners: versioned lifecycle/evidence, Validate result ingestion, acceptance projection, owner exceptions, follow-up approval and delivery effects. Share durable-effect mechanics where current story/plan effects already fit; avoid building an unrelated generic workflow engine.

### Key Boundaries

- Agent results are untrusted proposed findings, never authority to waive, choose another target, create issues or merge.
- Numeric parent/story/run/generation/revision identities bind every decision. Deep-copy context at runner boundaries (SEC-8, SEC-20, CONC-1).
- Store transactions commit intent and local state before external I/O; never hold the single SQLite connection across GitHub calls (CONC-10). All SQL and schema migrations remain in `internal/store`.
- `turn` declares narrow consumer interfaces for GitHub operations; extend the concrete client and intentional surface tests together (TEST-5, TEST-10). Never exec `gh` for effects (SEC-9).
- Read-only Validate returns a bounded structured result through runner output. Do not require it to write a repository transport file or relax its tool profile to obtain output. The factory persists the result and renders any validation artifact/comment.

### Data Flow

Owner approves newly planned scope → store pins new workflow version and immutable revisions → Implement reports engineering checkpoints with executed check evidence → store unblocks dependent implementation work while GitHub children stay open → successful aggregate engineering/save produces a supported suggestion → explicit owner Validate → runner reads frozen scope and retained snapshots/check outcomes → factory validates complete criterion/review coverage → durable decision → visible criterion projection and report → child closure when accepted → all-child readiness → reviewable PR proposal → explicit approval bound to PR/head → verified merge into main → parent-close effect.

Owner exception comments form a separate trusted path into acceptance/readiness decisions. Follow-up proposals similarly require explicit approval before factory issue creation. Neither path edits the factual outcome of tests or supplies implicit merge permission.

### Concurrency and Context

Reuse existing parent effect ownership and daemon task/clone locks. No new background worker or timer is necessary. Retry eligibility belongs to daemon ticks; `turn` and `github` do not sleep/retry (CONC-6). Propagate context through all blocking calls; use existing bounded terminal durability patterns only where required, not detached long-running work. Approval parsing and delivery must be serialized with proposal/head changes. Lost responses reconcile remote identities and bot provenance before issuing another effect.

### Shared State and Side Effects

Keep engineering completion, acceptance/waiver, remote closed state, aggregate/save state and merged state distinguishable. An externally closed managed issue must never fabricate acceptance evidence. An owner can waive readiness, but cannot waive identity checks or pretend a merge succeeded. All attached children count for normal readiness, even children outside the approved managed set. Follow-ups are separately linked by default; attachment is explicit and immediately affects readiness. Do not put GitHub automatic-closing keywords in PR bodies in a way that closes the parent before the intended confirmed integration protocol.

## Binding Cross-Story Decisions

1. Version is selected at approval. Backfill existing approved/active generations as legacy, keep their pending effects and previously closed stories unchanged. Newly approved work uses separation; do not silently convert a live generation during a retry or replan. Document version inheritance for an existing active feature explicitly.
2. Implement supplies engineering/test facts, never acceptance for the new version. Validate performs acceptance, code, architecture and plan review without writing code or running/commissioning checks. Missing evidence becomes unverified; concrete failures may support an Implement suggestion. No automatic mode chain.
3. Accepted/waived decisions retain original snapshot/revision and do not expire or reopen on later code/main changes. Before first sign-off, unknown or mismatched evidence remains unverified. Changed delivery head invalidates that delivery approval only.
4. Explicit trusted owner comments may waive individual criteria, whole stories, review/test obligations or remaining readiness scope with a reason. Store actor/comment identity, expanded scope, original findings and explanation. Manual checkboxes/closure have no such authority. Waived is visibly different from proven.
5. Effect intents are durable and idempotent; projection preserves unrelated live body content and refuses ambiguous requirement conflicts. Successful merge confirmation precedes parent closure; failure leaves resumable delivery debt and parent open.
6. Protected instruction trees are separate, independently executable sibling stories, with a third story for shared sources/generator/root rules. Neither tree story depends on the other. Scoped checks must complete with temporary parity drift; final cross-tree consistency is an aggregate gate.

## Owner-Authorized Constraint Amendments

- SEC-21: preserve immutable rooted evidence and complete obligations; distinguish a proven criterion from an explicitly waived obligation, and move acceptance/review authority to Validate for the new workflow.
- CONC-10: retain serialized durable effects and uncertain-response handling; invalidation superseding closes remains legacy-only where it would reopen historical acceptance.
- CONC-11: retain legacy immediate verification/closure; new workflow closes per story after Validate or explicit waiver, never from engineering completion. Later code changes do not reopen accepted stories. Parent closure requires confirmed integration into main.
- EXT-4 and nested-agent evidence documentation: missing authentic evidence remains factually unsatisfied/unverified. An explicit owner exception may waive the obligation without claiming observation or passing evidence. No sandbox expansion or nested live capture requirement.

These are requested behavior in run 0003, not unapproved scope. Record rationale and affected rules in `.aiteam/AC_AMENDMENTS.md` during shared instruction work, and update APP_CONSTRAINTS/APP_CONTEXT accordingly. Remaining security and identity standards stand unchanged.

## Task Hints for Story Decomposition

### Lifecycle storage and engineering progress (CAP-1, CAP-6)

Agent: go-implementation-engineer; verification: go-unit-test-engineer.

Pattern: extend existing approved generations, progress journals, story completion and recovery rather than overload remote `closed` as implementation readiness. Placement: `internal/store`, `internal/agent`, `internal/turn`, existing daemon progress polling. Contract: a versioned engineering event with immutable implementation/check facts and distinct completed/blocked status; dependencies release on engineering completion. Migrations are additive numbered embedded SQL, connection cap stays one. Include restart, mixed legacy/new generations, stale events, duplicate events, dependency release with open children and aggregate/save recovery. No acceptance checkbox or close effect in this new branch. Avoid making passing code review a prerequisite of engineering completion.

### Managed Validate context and result boundary (CAP-2)

Agent: go-implementation-engineer; verification: go-unit-test-engineer.

Placement: `internal/agent`, `internal/harness`, `internal/codex`, `internal/turn` admission/context and `internal/config` only if policy representation changes. Extend frozen-context plumbing on cold/resume/fallback, including retained snapshots/check results and read-only dependencies. Structured output is versioned, bounded and rejects unknown/missing identities, duplicate/missing criteria and unsupported statuses. A successful process exit alone is not sign-off. Preserve explicit failure outcomes. Both adapters must produce the same semantic contract. No model-specific exported names, tests launched by Validate, executable story content, or forge credentials.

### Acceptance decisions and issue projection (CAP-3)

Agent: go-implementation-engineer; verification: go-unit-test-engineer.

Placement: criterion parsing/projection in `internal/planbrief`, decisions/effect debt in store, orchestration in turn, bounded comments in report, typed redacted mutation in github. Every frozen criterion is enumerated by the existing AcceptanceCriteria parser. Bind findings to retained snapshots and approved revisions. Order durable decision/report/body projection/closure so interrupted retries can reconcile each step and close each accepted child without sibling waits. Preserve full original requirement text and unrelated edits; distinguish recognized factory marks from live scope changes. Test response loss, partial projection, ambiguous edits, missing proof, advisory versus blocking findings, already-closed external state and no later automatic reopening.

### Owner exceptions (CAP-4)

Agent: go-implementation-engineer; verification: go-unit-test-engineer.

Placement: bounded explicit control parsing in turn, trusted numeric owner dispatch in daemon, expanded immutable exception records in store, outcome notes in report. Pick documented grammar consistent with existing controls; select proposal/generation and stable criteria/story keys, require reason. Scope expansion must be auditable and replay stable. Do not treat free prose, third-party comments, manual ticks, labels or child closure as authorization. Preserve original unverified/failed outcomes; broad override includes readiness but never implies approval to merge or authorization to fabricate successful delivery.

### Follow-up proposals and approval (CAP-4)

Agent: go-implementation-engineer; verification: go-unit-test-engineer.

Reuse plan-publication issue creation/reconciliation patterns and numeric identity checks. Persist full proposed title/body/source links before suggesting approval. Approval is to concrete content; editing content creates a new proposal. Default to linked-only issues; explicit attachment invokes existing sub-issue gateway. Handle uncertain creates with stable markers and verified creator identity; no duplicate issue on retry. Creating a follow-up never waives its source criterion.

### Readiness, PR integration and parent closure (CAP-5)

Agent: go-implementation-engineer; verification: go-unit-test-engineer.

Use existing GitHub gateway with narrow added PR lookup/create/merge/confirmation capabilities, deliberate surface inventory update and HTTP fixtures. Use existing git Worktree reads/task-branch save; registered Clone stays read-only (SEC-7). Readiness observes every attached child and distinguishes managed acceptance from remote state; documented owner override is explicit. PR proposals identify branch/head and target main. Require explicit approval to concrete PR/head; changed head needs new approval without acceptance invalidation. Journal create/merge/close separately and reconcile lost responses. Confirm remote merged state into the intended main target before closing parent. Do not direct-push main, create another forge client or put credentials in runner context.

### Shared sources, rules and generator (CAP-7)

Agent: go-implementation-engineer; app-context-updater for persistent references; verification: go-unit-test-engineer where rendering changes.

Own only paths outside `.claude/**` and `.codex/**`: shared team sources/index/renderer or generator source, root instructions, documentation and amendments as needed. Inspect actual source ownership before edits. Tests render to temporary directories or compare in memory; do not execute a generator into either protected tree. Shared contract is the common predecessor if necessary, never a back dependency on both installed trees. Include explicit legacy/new-version instruction gating so active features retain their current lifecycle while new features use Validate.

### Claude installed instructions (CAP-7)

Agent role: go-implementation-engineer running Codex. Own `.claude/**` only. Update affected modes/orchestrators/reviewer/task guidance from the shared approved contract. No `.codex/**` writes, no mixed-tree generator run, no per-story parity condition requiring the sibling first. Tests/review inspect owned content and shared source in isolation. Other shared/root edits belong to the shared story.

### Codex installed instructions (CAP-7)

Agent role: go-implementation-engineer running Claude. Own `.codex/**` only. Same lifecycle and isolation requirements; no `.claude/**` writes. May consume shared contract but cannot require the Claude tree story's completion. A manual harness switch is an operational handoff, not a blocked cyclic task graph. No architect is rostered for agent-prompt design; engineer follows approved behavior, source conventions and APP_CONSTRAINTS with bounded offline evidence.

## Platform and Evidence Notes

- Platform: application layout/interfaces — no new external module, exported service API or generic state engine; consumer-owned interfaces.
- Platform: concurrency/database — existing bounded daemon workers, context-first access, embedded migrations, one SQLite connection, no transaction over I/O.
- Platform: testing — stdlib tests, handwritten fakes, real migrated SQLite for transaction claims, `internal/gittest` for offline git, httptest for API contracts, no live forge dependency or integration tags. Run scoped tests per story and `make check` aggregate during Implement; preserve their results for Validate.
- Platform: dependencies/HTTP service — no new dependency, router or listener required.
- Platform: logging/errors — injected slog, redacted report values, typed failures, stable diagnostic ordering and explicit resumable domain outcomes.
- Every adapter/instruction criterion carries its own evidence inventory in its full story body. Synthetic fixtures prove parsing, authorization and state transitions only; they do not claim real agent behavior or actual hosted delivery. Missing live evidence is stated, never manufactured. Actual delivery is observed by the future runtime only after owner approval.

## Managed Proposal Contract for the Product Owner

`internal/planbrief/proposal.go` requires envelope `version:1`, `parent:"github:marioatcloudvane/litefactory#195"`, `generation:1`, nonempty stories. No existing story context is supplied, so every story uses `intent:"create"`, `issue_number:0`, `issue_id:0`, `expected_base:""`. Those zero/empty values are the exact required create representation, not invented GitHub identities. Do not look up live story data.

Each story key matches `^[A-Za-z0-9][A-Za-z0-9._-]{0,39}$`, title is nonempty, single-line and at most 256 bytes; full body is at most 65,536 bytes. Dependencies are unique keys present in this complete proposal, no self-reference or cycles. Include all fields including empty dependencies arrays. Five nonempty level-2 sections are required: User Story, Acceptance Criteria, Tasks, Architecture, Dependencies. Use stable leading AC identifiers in every criterion and matching evidence inventory names.

Write only `.litefactory-plan-proposal.json` at the worktree root as transient story transport; do not commit it or create per-story Markdown. Shared `plan_brief.md` has `## Story Index` with `| ID | Title | Sub-issue |`; until publication Sub-issue cells may be `—` (parser explicitly tolerates this). Explain that real links are pending factory publication, rather than inventing issue numbers or placeholder URLs. `turn.PublishPlan` regenerates this table with real GitHub issue links after verified publication. Story bodies and task details live solely in the proposal for transport, then GitHub; shared architectural decisions remain in the brief.
