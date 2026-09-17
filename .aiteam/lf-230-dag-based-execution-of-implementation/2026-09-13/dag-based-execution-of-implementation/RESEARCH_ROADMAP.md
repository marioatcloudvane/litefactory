# Proposed graph execution roadmap

Research recommendation, run 0009, 2026-09-17. Not an approved plan or published story set.

Keep the owner's eight scopes, add Spike coverage, and build replacement team setup before removing existing setup. Numbers below are suggested planning order, not a requirement to execute every parent serially. Each parent has 3–6 candidate child scopes, to be refined independently during Plan.

## 1. Foundations for graph-based execution (owner Story 1)

1. Versioned DOT profile, Gonum parsing and validation, registered `lf_<mode>_*` actions and allowed outcomes. No arbitrary executable code in graph attributes.
2. Pinned workflow and role snapshots; durable instance/node/attempt records, outputs, owner-reply correlation and explicit version identities.
3. Deterministic serial transition/action dispatch over existing runner/submission boundaries; preserve provider permissions and selected mode.
4. Restart, cancellation and bounded-retry handling; reconcile uncertain external effects and prevent stale attempts from completing newer work.
5. Explicit old/new execution-path admission and compatibility adapter; active work remains pinned, rollback affects eligible new admissions, never silently rewinds effects.
6. Node/attempt status and traces plus recovery/effect-boundary verification infrastructure. Record token/time baselines without promising unmeasured savings.

Deliver a usable vertical engine slice, not a general workflow product. Later mode parents extend only their required actions. SQL stays in store, time/backoff in daemon, bounded action execution in turn; exact package/API design belongs to Plan.

## 2. Graph-based Research execution (owner Story 2)

1. Ship research.dot and bounded agent task/result contracts using mode-namespaced actions.
2. Optional clarification and durable question/reply resume without a waiting agent.
3. Investigation/synthesis/refinement with bounded loops and complete/incomplete reports through the existing result contract.
4. Opt-in adoption and observed clear-request, question/restart, failure and report-recovery cases.

Depends on 1. Use a compatibility bridge to current team delivery so Research does not wait for every team-management change.

## 3. New team/workflow setup for graph execution (owner Story 8, moved earlier)

1. Bundle model connecting standard workflow definitions to action registrations, roles, skills, tiers and provider permissions.
2. Install/inspect/update and validation experience for graph-capable bundles; initially ship the standard bundle only.
3. Local customization, drift/provenance and immutable running-instance snapshots.
4. Explicit migration/compatibility bridge from existing setup, preserving existing user customizations and safe recovery.
5. Fresh-install and upgrade checks for supported provider delivery and role resolution.

Depends on 1; Research's first working slice informs the interface. Can progress beside subsequent mode work. Define the exact UX and what older installation surfaces become obsolete in this parent's Plan. New setup means graph-aware packaging and management, not deleting the team/role abstraction.

## 4. Graph-based Plan execution (owner Story 3)

1. Draft story descriptions/problem statements and saved owner discussion before publication.
2. Prose acceptance intent, concrete criteria and explicit completion signals.
3. Architecture review and dependency-aware rewind/invalidation of drafts.
4. Final consistency/readiness check and existing durable factory publication, with replay-safe receipts and retained issue identity.
5. Resume/adoption verification, including no publication during intermediate discussion.

Depends on 1 and reusable human-pause patterns from 2; use the compatibility bridge until 3 is ready.

## 5. Graph-based Implement execution (owner Story 4)

1. Approved/frozen-story DAG readiness and deterministic specialist dispatch; default serial writers before optional concurrency.
2. Structured test-specialist recipes: command/arguments, scope, criteria, prerequisites and timeout.
3. Factory-executed story-tailored tests with immutable provenance; no empty/unexecuted check accepted as passing.
4. Bounded diagnosis/fix/retest loops, cancellations and honest exhausted-retry outcomes without routine human approval gates.
5. Engineering completion, dependency release and result handoff without premature acceptance/closure.
6. Versioned change to Implement aggregate-test policy plus resume/failure/conformance verification.

Depends on 1; full new-mode flow consumes approved Plan output from 4. The engine can initially consume existing frozen plans where explicitly compatible. Full-suite movement must be coordinated with 6: do not enable a reduced-check default without the corresponding Validate path and versioned contract.

## 6. Graph-based Validate execution (owner Story 5)

1. Versioned policy permitting factory-run full-suite checks before a read-only reviewer, preserving legacy rules for legacy instances.
2. Repository-declared full-suite execution and snapshot-bound retained results, including failed/missing prerequisites.
3. Read-only criterion/code/architecture/plan assessment using retained evidence.
4. Existing acceptance, reporting and external-effect projection with recovery; no implicit mode switch to fix code.
5. Integrated Implement→owner-selected Validate adoption and recovery verification.

Depends on 1 and test-execution contract from 5. Implementation work can overlap 5 once that contract is agreed. This parent does not grant the reviewer fresh-check or edit authority.

## 7. Graph-based Spike execution (additional parent)

1. Minimal bounded workflow and `lf_spike_*` action binding, retaining the existing scope stop rule.
2. Applicable scoped verification, SPIKE.md and shared result handoff; broader work becomes a recommendation, never an automatic mode switch.
3. Compatibility and representative completion, blocked and restart verification.

Depends on 1 and shared result patterns from 2. This closes the “every mode” gap without growing Research's scope. If the owner prefers exactly eight parents, fold these three scopes into Research rollout explicitly, rather than dropping Spike.

## 8. Retire legacy orchestration/execution path (owner Story 6)

1. Inventory active/pending legacy work and define drain or explicitly authorized migration; preserve retained evidence/readers required by historical work.
2. Establish real cutover evidence across all five modes/providers plus recovery, permission and result-contract behavior; documented rollback/admission gates.
3. Switch eligible new work to graph defaults while pinned active work finishes safely.
4. Remove obsolete execution/orchestrator paths only after retirement criteria; update diagnostics, documentation and compatibility coverage.

Depends on 2, 4, 5, 6 and 7, and a supported graph bundle installation path from 3. Both execution paths remain supported until this parent meets its gate. Do not confuse this with the existing managed-feature workflow/content/submission versions or #274/#296 reader retirement. Adoption and retirement are distinct claims; offline tests do not replace required authentic host evidence.

## 9. Retire obsolete team setup (owner Story 7)

1. Inventory old installation/management surfaces and migrate remaining configurations and user overrides to 3's replacement.
2. Verify fresh install, upgrade, pinned-instance recovery and rollback with no dependency on the obsolete setup.
3. Remove only superseded commands/rendered orchestration assets/compatibility plumbing, preserving needed role resources, provenance and permissions; finalize documentation.

Depends on 3 and on 8 wherever the old engine still consumes old setup. Do not delete old setup and then build its replacement. Precise removal targets require Plan inventory.

## Dependency summary and scope limits

Core spine: Foundations → Research → remaining mode integrations → legacy engine retirement. New setup can proceed after Foundations/Research discovery; it must exist before setup retirement. Plan/Implement/Validate are separately planned parents with shared contracts; this does not change the rule that users select runtime modes. Spike can ship beside the other integrations.

Recovery and basic observability are required throughout, already allocated above. Do not add standalone parents for Temporal, a visual editor, a workflow marketplace or commercial fleet management: they are outside the agreed first rollout. If Foundations' six scopes prove too large, split definition/runtime foundations from durable execution and recovery as a tenth parent during its own Plan; no evidence yet requires that expansion.

## Evidence and publication boundary

Current source inspected at `1a59feb8777a9fd60e881ab99b93401f4fbffed4` on 2026-09-17: `internal/teamsource/resolve.go:35` (central catalog), `internal/teamsource/snapshot.go:35` and `:48` (mode/role snapshots), `docs/workflow/spike.md:5` and `:39` (bounded Spike), `internal/turn/plan_publication.go:52` (publication validation), `internal/turn/validation.go:17` (retained evidence), `.aiteam/APP_CONSTRAINTS.md:102`, `:207`, `:221`, `:275` (authority/version/evidence boundaries). Proposed scope and order are research judgments, not observed runtime features.

No implementation, tests, commits or issue creation. Root inspection reports creation preflight exists but a creation submission tool is not exposed in this attempt. A research-result receipt is not story publication; defer the live creation test to authorized publication with the appropriate operation available.
