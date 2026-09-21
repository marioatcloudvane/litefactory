# Architect: Go Architect
# Created: 2026-09-21

## Verdict
Ready for three-story assembly, with an explicitly authorized rule change recorded below. No new owner decision is required. This review describes implementation structure, not completed implementation or runtime evidence.

## System Overview

### How This Feature Fits
The feature adds a Plan policy over existing graph execution. `internal/graphexec` remains pure topology/schema/control validation. `internal/turn` owns admitted effects and orchestration; `internal/store` owns SQL and durable revisions; `internal/daemon` owns polling, owner reply admission and scheduling. `internal/teamsource` captures the shipped DOT/Markdown closure, and `internal/render` provides native in-memory role delivery. `internal/planbrief` continues to validate complete typed stories. `internal/report` renders draft and review content; `internal/github` remains the only forge boundary.

The source already provides `WorkflowPolicy`, `WorkflowSpecs`, `ExecuteWorkflow`, `RecoverWorkflowPublications` and `CaptureWorkflow`. Their Research-oriented type names do not justify a second engine. Add Plan-specific decoding, policy and scoped effects around those seams. Generic machinery may need neutral contract extensions: do not stretch a Research-only payload into fake Research results or weaken Research validation.

### New Packages / Components
- A Plan story activity in `internal/turn`, registered as `lf_plan_story`, with pure input/control shapes beside existing graph contracts. No new service or external library is required.
- Plan candidate, approval, dependency and effect records in `internal/store`, using a new embedded SQL migration. Keep draft storage separate from complete/frozen managed-story validation.
- A factory-owned Plan asset closure containing `plan.dot`, three roles, shared plan-evidence resources and focused skills. Author neutral sources under `teams/source`; capture/package through `internal/teamsource`, with the actual embedded runtime closure under its own package directory if needed. Go embedding cannot reach a parent directory: use an explicit existing-style source-to-runtime packaging/check step, not filesystem traversal at runtime or hand-maintained independent contract copies.
- Plan workflow policy/admission and final publication adapter in `internal/turn`; scoped child reply routing in `internal/daemon`.

### Key Boundaries
Agents propose structured intent; they never choose forge credentials, arbitrary issue targets, approval authority or runtime mode. The factory binds parent, selected story, exact candidate and operation identity. Activity effects use consumer-owned interfaces and existing redacted forge mutation APIs (SEC-6, SEC-8, SEC-20, TEST-5).

Persist a story key, issue identity, stage candidate revision, upstream/shared-decision revisions and owner approval reference. Immutable candidates are snapshots; the live issue title/body remains the editable story surface (CONC-12). Do not create per-story Markdown or make a database draft a second independently editable source.

Model stage approval separately from managed implementation freeze. Per-story movement inside Plan does not invoke `ApproveManagedStories`, select Implement, release implementation dependencies or close accepted implementation work. Final handoff atomically binds the whole active set only after all complete-story validation and review requirements hold (CONC-9, CONC-11).

### Data Flow
1. Authenticated parent launch or admitted child reply enters persisted Plan routing.
2. Select an actionable story/stage from durable state; deliver the exact issue/candidate/upstream context to its role.
3. Validate output and persist a new candidate. Story-only candidates contain intent without architecture/tasks/criteria placeholders.
4. `lf_plan_story` persists effect intent, creates and attaches or updates the same issue, then confirms observed state. Retire requires exact owner confirmation and not-planned closure semantics.
5. Existing ask/comment mechanisms display a candidate and park a correlated wait on its sub-issue; parent waits are reserved for shared decisions and assembled scope review.
6. Authorized explicit approval records the displayed revision and dependencies. The policy selects the next eligible story/stage; unrelated stories retain progress. This is logical independence, not a requirement for concurrent child agents.
7. Third-stage final review binds complete current stories plus shared decisions. The adapter passes existing issue identities and typed sections through current publication, index and successful-save gates. Pending effects cannot establish readiness.

### Concurrency & Context
Use existing task/parent ownership; do not introduce a worker per story. No SQL transaction crosses forge, subprocess, comment or filesystem I/O. Record effect intent before delivery and confirm before consuming the source reply (CONC-4, CONC-10). Every store/effect operation takes context first; retries/backoff stay in daemon (CONC-5/6). Preserve the one-connection SQLite pool. Clone mutable nested candidate/context collections across boundaries (CONC-1).

### Shared State and Side Effects
Changing canonical intent invalidates that story from stage one; technical changes invalidate from stage two. Traverse recorded dependency edges to affected candidates/shared decisions, preserving provably unrelated approvals; uncertain impact parks affected scope. Any canonical scope/set change invalidates assembled approval. Cosmetic rendering alone does not change canonical content identity.

Creation and attachment are separately recoverable effects. Known created issue identity must survive an attachment failure. Lost create replies require provenance-based reconciliation or an explicit uncertain state, never blind creation. GET/PATCH/GET cannot promise atomic exclusion of invisible concurrent GitHub edits; detect observed conflicts conservatively and retain unresolved races as uncertainty rather than claim impossible compare-and-swap guarantees.

Workflow selection, content/submission versions and implementation workflow versions remain separate persisted authorities. New Plan admission must not migrate active legacy scopes implicitly. Plan retirement removes obsolete default dispatch and unused source assets only; supported legacy readers and pending-effect handlers remain with an explicit inventory.

## Cross-story Decisions
1. Story 1 owns the durable activity contract, effect persistence and forge behavior; Story 2 consumes that contract to deliver the neutral package; Story 3 activates orchestration and handoff. Story 2 depends on Story 1's contract, and Story 3 depends on both delivered stories.
2. There is exactly one new activity name. Stage scheduling, approval and final assembly are Plan policy capabilities using existing agent/if/loop/ask/comment mechanisms, not additional activity names.
3. Per-story candidates and review identities exist independently of the parent graph cursor. A parked story cannot block every unrelated story. Select eligible work through policy; use existing bounded execution/continuations, without inventing graph subgraphs or unbounded loops.
4. Third-stage incremental criteria approvals and one exact assembled-set approval are part of the same stage. No fourth stage; neither local candidate approval nor helper acknowledgment grants readiness.
5. Shared neutral assets and their embedded closure are the authoritative delivery target. No installed `.claude/**` or `.codex/**` edits belong to these three stories; native delivery is tested in memory/temporary roots.
6. Adoption and legacy removal have separate completion evidence. Research's `researchDefaultActive=false` and its external evidence gate are not to be changed by this feature; do not claim synthetic tests satisfy authentic host obligations.

## Per-task Structural Hints
Task labels below are architecture hint anchors for the orchestrator's typed task mapping, not additional stories or requirements.

### A1: Define draft/activity contracts and persistence
**Agent:** go-implementation-engineer
- **Pattern/placement:** Pure structured stage/operation validation at the graph/result boundary; persistence in `internal/store` with embedded migration.
- **Interfaces:** Separate usable draft shape from `planbrief`'s complete-story shape; activity requests bind operation, story key, candidate and expected issue revision.
- **Watch out:** Stable operation identity and explicit failed/conflict/uncertain outcomes; no factory identity accepted from role prose. Store retirement as history, not row deletion.

### A2: Implement create/update/retire effects
**Agent:** go-implementation-engineer
- **Pattern/placement:** `internal/turn` activity registered through `WorkflowPolicy.Specs/RegisterEffects`; extend existing `internal/github` methods only for demonstrated missing not-planned close semantics.
- **Integration:** Existing create/attach/update APIs, redacted bodies, durable effect ownership and receipt recovery.
- **Watch out:** A generic close-as-completed path is wrong for retirement. Check actual parent and bot provenance; preserve owner content and uncertain effects. Surface/purity tests are deliberate API contracts (TEST-10).

### A3: Activity unit coverage
**Agent:** go-unit-test-engineer
- **Pattern/placement:** Pure input/state and handler-boundary tests beside graph/turn owners, with handwritten fakes.
- **Watch out:** Invalid/foreign targets, stale revision, missing owner confirmation, duplicate key, unsupported payload and uncertain result must remain observable.

### A4: Activity persistence/forge coverage
**Agent:** go-integration-test-engineer
- **Pattern/placement:** Real migrated SQLite plus httptest forge through normal Go test files (no tags or Integration suffix).
- **Watch out:** Crash between create/attach/receipt, replay, lost response, external closure/reparenting and conflicting update. Test transactions against real SQLite (TEST-6).

### B1: Author Plan roles, focused skills and DOT
**Agent:** go-implementation-engineer
- **Pattern/placement:** Neutral `teams/source` package using Research's graph-relative role and `skills/<name>/SKILL.md` layout; shared result/state contract referenced explicitly.
- **Integration:** `CaptureWorkflow` recognizes bounded resource closure, role task/tier metadata and declared skill resources. Supply file and semantic digest coverage.
- **Watch out:** Stage-one examples must not smuggle future acceptance or architecture requirements. All graph cycles need positive limits; exhaustion parks without promotion. No direct edits to Research package or protected installed trees.

### B2: Package and validate native delivery
**Agent:** go-implementation-engineer
- **Pattern/placement:** `internal/teamsource` factory-owned closure and `internal/render` in-memory delivery.
- **Integration:** Use the Story 1 action schema with generic graph specs; define contract fixtures independently of production activation in Story 3.
- **Watch out:** Every referenced resource must ship; a source-tree-only DOT is not runtime delivery. Avoid copying roles into both installed trees.

### B3: Package/graph contract tests
**Agent:** go-unit-test-engineer
- **Pattern/placement:** Existing teamsource/graphexec contract tests, temporary resource roots and native render outputs.
- **Watch out:** Verify three-stage separation, human gate paths, invalid references and Research unchanged behavior; source strings alone do not prove execution.

### C1: Wire Plan policy and admitted production dispatch
**Agent:** go-implementation-engineer
- **Pattern/placement:** `internal/turn/workflow_policy.go`, shared execution seams in `research_workflow.go`, Plan admission in its own focused files; daemon/CLI constructors supply validated dependencies.
- **Integration:** Add Plan-specific decode/control/result contracts with shared engine reuse. Persist selection independently of managed workflow/content versions.
- **Watch out:** Default new eligible Plan goes to the graph; active legacy resumes its saved route. No implicit Research activation or mode chaining.

### C2: Persist stage approval, scheduling and child routing
**Agent:** go-implementation-engineer
- **Pattern/placement:** `internal/store` candidate/dependency/approval state, `internal/turn` Plan policy, `internal/daemon/story_refinements.go` admission and independent reply consumption.
- **Integration:** Existing durable ask/wait correlation generalized to child target, selected story/stage and exact candidate.
- **Watch out:** Mixed change-and-approval, stale or ambiguous replies cannot promote; role output cannot impersonate owner evidence. A child wait does not globally block other eligible stories. Final managed freeze remains parent-scoped.

### C3: Final typed handoff and readiness
**Agent:** go-implementation-engineer
- **Pattern/placement:** Reuse `internal/turn/typed_plan.go`, `plan_handoff.go`, `plan_publication.go`, `plan_index.go` and `internal/planbrief` complete contract validation.
- **Integration:** Convert approved current stage data into typed complete stories, reusing already-created issue identities. Confirm publication and link-only index/save before ready.
- **Watch out:** Reject empty active set, dangling/retired dependencies, pending edits/effects and stale assembled approval. No local per-story body transport as editable truth.

### C4: Retire obsolete dispatch and document compatibility
**Agent:** go-implementation-engineer
- **Pattern/placement:** Remove replaced new-Plan dispatch and genuinely unused neutral orchestrator sources/references after call-site and persisted-cohort inventory.
- **Integration:** Retain compatibility owners explicitly where old plan attempts, outcome formats, saved contexts or pending publication require them.
- **Watch out:** Record the specific retention prerequisite for each retained path; do not call it retired. Reconcile the authorized CONC-22 change in project documentation and affected tests.

### C5: Policy and compatibility unit coverage
**Agent:** go-unit-test-engineer
- **Pattern/placement:** Pure control/decoder and policy fixtures; legacy dispatch regression coverage with current frozen contracts.
- **Watch out:** Per-story versus assembled approval, independent stage movement, transitive invalidation, unauthorized replies, loop exhaustion and absent final dependencies.

### C6: Workflow/recovery end-to-end fixtures
**Agent:** go-integration-test-engineer
- **Pattern/placement:** Existing turn/daemon fixtures with fake runner, actual SQLite, controlled HTTP forge and real temporary git fixture.
- **Integration:** Exercise three stories, two advancing while one remains; dependent and unrelated changes; retirement; restart at wait/publication/save; legacy restart.
- **Watch out:** Rendezvous with channels and Quiesce, never sleeps (TEST-8). These are offline behavior evidence, not actual provider/host certification.

### C7: Aggregate engineering checks
**Agent:** go-implementation-engineer
- **Pattern/placement:** Repository `make check` including formatting, vet, pinned lint and race tests after combined work.
- **Watch out:** Never build into dist or restart the daemon. Retain factual results; no checks were executed during this architectural review.

## Standards Reconciliation
**Authorized deviation: CONC-22's statement that discussion publishes no stories conflicts with the explicitly requested early draft sub-issues.** The owner has already authorized the new behavior. Record a scoped amendment during implementation: newly admitted graph Plan may publish incomplete draft sub-issues through `lf_plan_story`, while remaining unready; legacy discussion keeps its existing no-publication contract. Complete Plan still requires complete typed validation, confirmed publication, link-only index and save. Do not silently weaken all Plan paths. This is an implementation documentation/test task within the requested wiring story, not a reason to re-ask the owner.

CONC-9 requires no deviation: distinguish Plan-stage approvals from final managed freeze. CONC-12 requires no deviation: candidates are retained observations/proposals and GitHub remains the editable story surface. Repository-specific constraints override generic platform examples: one SQLite connection, hand-rolled migrations, no new dependencies, no extra recover boundary, no tagged integration suite.

## Security Findings
**Scope:** Prospective Plan contracts and effect boundaries; not an audit or implementation verification.
**Rulebook:** `.aiteam/APP_CONSTRAINTS.md` SEC rules.

| Area | Status | Finding |
|---|---|---|
| Input validation | Concern | New incomplete-stage schemas need strict bounded validation and no agent-supplied factory identities. |
| Authentication/authorization | Concern | Child replies and retirement must bind numeric owner, parent, stage and exact candidate; generic prose cannot promote. |
| Secrets/configuration | Compliant design | Existing factory-only forge credentials and config path ownership are retained. |
| Data handling | Concern | Drafts and comments must use redacted mutation APIs and untrusted prompt framing; candidate history is not a second edit surface. |
| Dependencies/supply chain | Compliant design | Existing graph/parser/render/store modules suffice; no new dependencies planned, no vulnerability audit claimed. |
| Error handling | Concern | Lost effects need durable uncertainty and recoverable diagnostics, not success or blind retry. |

## Platform and Evidence Notes
- Internal package ownership, consuming interfaces, typed errors, context cancellation and injected logging apply. No HTTP listener or new service is needed.
- Store access, single-connection transaction discipline and migration tests apply. There is no cache or new goroutine requirement.
- No frontend/infra specialist is required; package Markdown is existing agent-delivery content, not a new UI.
- Missing manifest/evidence documentation is compensated by the supplied workflow contract and canonical `teams/source/skills/workflow/SKILL.md` guidance reflected in the feature spec; do not invent missing document contents.
- Proof uses source, real temporary stores, fake runners and controlled forge HTTP. Real hosted operations and authentic provider execution are separate, currently unverified evidence classes.

## Run 0005 Architectural Amendment: Activity-Owned Presentation

**Verdict:** Ready for assembly within the existing three stories. The presentation change is an explicit owner requirement; no additional activity, story or owner decision is needed. This note supersedes earlier hints wherever they would omit empty later-stage headings or delegate rendered titles/bodies to agents. Current factory rendering is not claimed compliant.

### Durable Identity and Title Ownership
Story 1's store model must bind a monotonically allocated ordinal to the stable story identity within the canonical parent scope. Allocate durably before publication, atomically with the story/effect binding under existing parent ownership. Retries resolve the existing identity; restart, failed or uncertain publication, update and retirement never allocate a replacement ordinal or release an allocated one. Different parents have independent sequences starting at one. A retained allocation may leave a gap; gap filling must not reuse an ordinal. The store owns allocation and uniqueness; rendering must not infer ordinals from current issue count, listing order, child issue number or run identity.

The activity constructs `[parent issue number]-[ordinal]: Story Name`, formatting the ordinal to a minimum width of three digits without truncating larger values: `362-001: Story Name`, then eventually `362-1000: Story Name`. The current typed story name remains editable while its factory prefix and issue identity remain stable. Agent content supplies the name, never an authoritative preformatted identity prefix.

### Rendering Boundary and Exact Layout
Keep orchestration and identity binding in `internal/turn`'s story activity, SQL in `internal/store`, and deterministic issue title/body rendering in `internal/report`. `internal/render` continues to own native agent instruction delivery, not this issue-body format. Agents supply structured actor/want/benefit, problem, in-scope and out-of-scope lists, and later task/criteria/architecture content. Typed draft/final adapters must preserve those fields without treating full Markdown bodies as input contracts.

The activity-owned rendering contract is exactly this heading order and spelling:

1. `# Userstory`
2. `## What this story solves`
3. `## Scope`, containing separate `In-Scope` and `Not in Scope` lists
4. `# Task`, containing table columns `Task Id | Description | Owner | Dependencies` in that order
5. `# Acceptance Criterias`
6. `# Architecture`

The first usable draft renders all headings. Its task table contains column headers and no task rows; acceptance and architecture sections contain no invented obligations or decisions. Later stages populate these sections from typed content. Preserve existing criterion-ID/checklist and architecture content formats inside the specified sections. Empty scaffolding is presentation, not stage-one technical scope or evidence of final completeness.

### Semantic Contract and Final Handoff
Preserve criterion/task identities, task-to-criterion links, task owners, story dependencies and factory metadata separately from visible section ordering. Do not add a visible Dependencies heading to the specified sequence. Adapt existing renderer/parser/reconciliation seams so semantically complete typed data round-trips without losing metadata or weakening complete-plan validation. Persisted dependency relationships and approved snapshots remain authoritative for execution; presentation must not manufacture or discard them. Avoid introducing a second editable story source.

Story 3's final publication adapter must retain the allocated prefix, exact body contract and existing issue identity, rather than re-rendering the approved draft into a competing legacy layout. Draft, technical and final updates share the same issue presentation owner. Approval binding and conflict detection continue to use the canonical content and exact reviewed revisions; a layout conversion must not overwrite unreviewed owner edits. Retain legacy rendering/parsing only behind supported persisted compatibility routes.

### Existing Story/Task Mapping and Evidence
- **Story 1 / A1–A4:** Add durable parent-scoped allocation, typed problem/scope presentation fields, activity-owned renderer and recovery tests. Cover independent parents, retirement, lost responses, partial create/attach, restart and ordinals beyond 999; assert the same identity receives the same ordinal.
- **Story 2 / B1–B3:** Roles and skills return typed content only; update contract examples to show stage-specific content without prescribing agent-authored Markdown titles or bodies. Verify initial empty sections derive from rendering, not fabricated stage output.
- **Story 3 / C3–C6:** Carry new fields and identity through complete typed handoff, publication, parsing/reconciliation and compatibility boundaries. Add first-draft, technical and final renderer fixtures plus a round-trip check that retains semantic dependencies/IDs while rejecting incomplete final plans and conflicting edits.

Repository constraints remain unchanged except the previously recorded authorized CONC-22 draft-publication amendment. No source changes, tests or runtime/remote verification were performed for this architectural amendment.

## Run 0006 Architectural Amendment: Technology-Neutral Reviewer

**Verdict:** Ready within the existing three stories. This amendment specifies the future Plan workflow's technical reviewer; it does not replace the current factory-supplied Go architect or implementation roster. It supersedes run 0005's instruction to retain the old architecture presentation format. No production source change or verification is claimed.

### Reviewer and Guidance Boundary
Story 2's technical-shaping role owns technology-neutral review of reuse, feasibility, boundaries, data fit and technical decisions. Its Markdown has a separate literal `# Skills` section with an extensible roster initially containing only Go guidance and agent-setup guidance. The role chooses guidance from the actual story scope: Go guidance for Go work, agent-setup guidance for roles/skills/instruction/workflow work, both for mixed scope, and neither for unrelated work. A Go host repository does not make Markdown-only agent work Go work. Future roster additions do not change the reviewer's general responsibility or make every listed skill mandatory.

Keep this distinction in the delivered resource closure: availability of a skill is not an instruction to apply it on every review. The shared contract describes applicability, and focused guidance supplies the relevant domain checks. Do not change the existing factory role routing to deliver this future reviewer or infer a new agent beyond the three planned workflow roles.

### Typed Content and Activity-Owned Architecture Tables
Story 1 extends typed technical content with overall architecture decisions, explicit subsection applicability and optional frontend reference content. `internal/report` deterministically renders these through the existing story activity; the agent supplies semantic fields rather than Markdown tables. Complete typed-plan adapters preserve interfaces, constraints, dependencies and identities regardless of their visible placement.

Initial story-only drafts keep an unpopulated `# Architecture`. Once architecture is populated, `## Architecture - General` is mandatory. Conditional subsections appear only when relevant, in the order General, Data Model, Service Design, Solution Design. Service Design covers layers, services, repositories, database access and decomposition. Solution Design covers collaboration, interfaces and abstractions; inheritance is included only when the actual solution uses it. Neither subsection imposes object-oriented design on Go or instruction-only work.

Use the column assumptions already recorded in the run 0006 feature-spec amendment as the single planning baseline:

| Subsection | Columns |
|---|---|
| `## Architecture - General` | `Area`, `Decision`, `Rationale` |
| `## Data Model` | `Entity / Relationship`, `Definition`, `Constraints / Rationale` |
| `## Service Design` | `Layer / Component`, `Responsibility`, `Dependencies / Access` |
| `## Solution Design` | `Element`, `Collaboration / Contract`, `Rationale` |

These column labels resolve the ordinary difference between preliminary proposed columns and the saved requirements; do not leave two competing formats in the typed stories. The owner-specified heading names and applicability are unchanged.

Optional `# Design` follows `# Architecture` only for frontend scope with relevant supplied design references. Preserve links/assets as structured references subject to existing validation and untrusted-content boundaries. Do not invent references, emit an empty Design section universally, or treat a linked design as permission to fetch or execute arbitrary content.

### Iteration, Validation and Existing Story Mapping
Every cycle loads current story sections, decisions, applicability and references. Update affected fields and retain unaffected content; save superseded decisions with their rationale in revision history. Scope changes reassess applicable guidance and subsections. Removing previously relevant content needs a recorded reason and the existing dependent-approval invalidation, rather than silent omission by a renderer.

- **Story 1 / A1–A4:** Own typed applicable architecture/frontend reference fields and renderer fixtures for initial, agent-only, data/service, collaboration, frontend and non-frontend cases. The old activity AC-009 architecture-format obligation is superseded; retain its retirement identity and express the replacement obligations using new AC-011/AC-012 as assigned by the orchestrator. Never reuse a retired criterion ID.
- **Story 2 / B1–B3:** Own the neutral role, separate selectable skill roster and guidance applicability fixtures. Verify Markdown-only work does not force Go, and mixed scope can use both initially available skills. Existing story/acceptance roles remain within the three-stage design.
- **Story 3 / C2–C6:** Preserve typed data through repeated shaping, final handoff and reconciliation. Cover retained unaffected sections, superseded decision history, justified applicability changes, semantic metadata round-trip and invalidation of affected approvals. Complete-plan validation remains strict even when some architecture subsections are inapplicable.

No additional standard deviation is required. The prior scoped CONC-22 amendment remains applicable. Current renderer compliance, runtime behavior and remote publication remain unverified by this Plan amendment.
