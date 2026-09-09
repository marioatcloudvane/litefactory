# Feature Spec: Explain polling worker activity
# Status: APPROVED
# Date: 2026-09-09

## Goal
- Let operators identify which sessions were dispatched, why they were selected, and whether they requested execution, without mistaking polling workers for prepared agent runs.

## User Stories
| ID | Story |
|----|-------|
| US-1 | As an operator, I want each successful dispatch to name its session and discovery reasons so that recurring polling is attributable. |
| US-2 | As an operator, I want a terminal worker diagnostic after cleanup with execution-request count so that I can distinguish worker lifetime from per-trigger run completion. |
| US-3 | As an operator, I want explicit worker and forge counters with a quiet-polling regression check so that summaries accurately describe their measured activity. |

## Data Model
| Entity | Field | Type | Required | Constraints |
|--------|-------|------|----------|-------------|
| Worker diagnostic | repo, task, session_id | Existing identity values | Yes | Canonical task identity; one occurrence of each attribute. |
| Worker diagnostic | dispatch_sources | Bounded ordered categories | Yes | forge_discovery, managed_registry, backlog, due_schedule, automatic_continuation; unique categories; stable order. |
| Worker diagnostic | schedule_id / continuation_id | Existing durable identity | When applicable | Preserve source identity; no invented GitHub identities. |
| Ordinary worker diagnostic | dispatch tick time | Existing tick timestamp | Yes | Grouping context only; not a unique worker identifier. |
| Terminal worker diagnostic | duration, status, execution_requests | Duration, bounded status, nonnegative integer | Yes | Count calls to the executor requested by this worker, including calls that fail; never infer prepared runs or process launches. |
| Tick summary | workers_dispatched, workers_in_flight, forge_issues | Integers | Yes | Additive aliases of dispatched, in_flight, issues. |

### Relationships
- Worker diagnostics retain the same existing repository/task/session/source context from admission through terminal cleanup.
- No schema migration, new persistent entity, new public API, new configuration, or permission change.

## API / Interface Changes
| Method | Endpoint / Action | Auth | Notes |
|--------|------------------|------|-------|
| Structured DEBUG log | worker dispatched | Existing daemon logging | Emit after admission/reservation succeeds and immediately before launching the worker. |
| Structured DEBUG log | worker finished | Existing daemon logging | Emit once after worker body cleanup and in-flight ownership release, before the final pool release/join action, on every admitted worker's return path. |
| Structured DEBUG log | Continuation admission refusal | Existing daemon logging | Match existing duplicate-refusal convention with complete identity. |
| Structured INFO log | Tick summary | Existing daemon logging | Keep exported result fields and legacy log attributes; add explicit aliases. |
| Documentation | Discovery and activity interpretation | None | Describe managed registry polling and existing run prepared evidence. |

## Business Rules
- Discovery reason indicates candidate enrollment, not an actionable command or pending refinement.
- Ordinary candidates union every encountered source category without changing deduplication, queue ordering, rotation, eligibility, or cadence.
- Successful dispatch events correspond exactly to handoffs counted by Dispatched across ordinary, schedule, and continuation paths.
- A worker with execution_requests=0 can perform useful control work; never classify that fact alone as idle or no_actionable_work.
- Terminal status distinguishes normal completion, deferral, failure, and shutdown cancellation using actual worker result/context; existing ERROR diagnostics remain intact.
- Terminal completion promises worker body cleanup and in-flight ownership release have run. The final pool release/join action follows the diagnostic, so Quiesce still joins the entire worker and its log. The diagnostic is not a free-slot observable and does not promise successful backlog clearing when that helper only logs failures.
- Preserve existing INFO run prepared as preparation evidence; it is neither success evidence nor a guaranteed process launch.
- Never publish an asynchronous tick's eventual agent-run count as if it were available at dispatch completion.
- Preserve factory authority over GitHub stories and proposal publication; no live forge reads or writes are needed for planning or acceptance.

## Edge Cases & Error Handling
- Candidates discovered by forge, managed registry, and backlog simultaneously receive one handoff with all three source categories.
- Busy, full-pool, closed, reservation-rejected, and pre-launch deferred candidates receive no successful-dispatch event.
- Fast workers still have admission logged before terminal completion.
- Early return, cancellation, executor error, and multiple comments must yield one terminal worker diagnostic with correct count and cleanup boundary.
- Tick counts are timing-dependent snapshots; tests must control worker lifetime or assert bounds instead of assuming fast workers always remain in flight.
- A closed managed child and a child changed without a parent timestamp change remain discoverable under existing polling behavior.

## Out of Scope
- Fixing the withdrawn in-flight leak diagnosis or reconciling reservations against database locks.
- Reducing polling, changing schedules, changing admission concurrency, or changing run execution behavior.
- A new global worker ID, agent-process counter, control-action taxonomy, or metrics backend.
- Modifying runtime state, restarting/deploying the daemon, or establishing the two production session identities during implementation.
- Live nested coding-agent probes, sandbox changes, external captures, and recovery claims concerning other issues.

## Assumptions and Provenance
- From RESEARCH_BRIEF.md run 0003: managed registry enrollment explains the observed pattern; dispatch/completion diagnostics, additive aliases, and polling regression coverage are the recommended scope.
- Assumed: retain the existing legacy fields indefinitely in this change because consumers are unknown; aliases are sufficient for compatibility.
- Assumed: include execution_requests rather than only a boolean because ordinary workers can process multiple comments and the count remains precisely defined.
- Assumed: production identities and deployed revision confirmation are follow-up operational evidence enabled by these diagnostics, not a prerequisite to implementing the bounded logging behavior.
- Supplied observations of deployed revision 03d0481 are unverified; local tests prove repository behavior only.
- Architecture refinement before criterion freeze: terminal logging precedes final pool release/join to preserve Quiesce semantics and TEST-9; the earlier research wording promising both releases before the log is superseded.
- No material question requires owner input for this scope.

## Acceptance Criteria
- [ ] AC-1: Every actual ordinary, scheduled, and automatic-continuation worker handoff emits exactly one DEBUG worker dispatched event before launch with repository, canonical task, session, source categories, and applicable durable source identity; rejected admissions emit no such event.
- [ ] AC-2: Ordinary candidate deduplication preserves a stable, duplicate-free union of forge_discovery, managed_registry, and backlog reasons while maintaining existing dispatch ordering and one handoff per admitted candidate; continuation duplicate refusal includes identity at DEBUG.
- [ ] AC-3: Every admitted worker emits exactly one DEBUG worker finished event after worker body cleanup and in-flight ownership release, before the final pool release/join action, on normal, early-return, deferred, error, and shutdown-cancellation paths, with matching identity/source context, duration, and truthful terminal status; Quiesce joins the diagnostic, the log does not promise a free pool slot, and existing error diagnostics remain present.
- [ ] AC-4: Terminal execution_requests equals executor calls requested by that worker, including zero, multiple, and failed calls; zero is never labelled as idle or proof of no useful work, and per-trigger turn complete is not represented as final worker cleanup.
- [ ] AC-5: INFO tick summaries expose workers_dispatched, workers_in_flight, and forge_issues equal to the preserved legacy dispatched, in_flight, and issues values; exported TickResult meanings and end-of-dispatch sampling remain unchanged.
- [ ] AC-6: Repeated ticks with two eligible open managed parents and empty forge listings hand off two polling workers per tick without executor requests or prepared runs; after quiescence each worker releases admission, clears clean-drain backlog, and can be admitted on the next tick; logs name the parents and managed_registry source.
- [ ] AC-7: New managed-child input remains detected without a parent timestamp change, including existing closed-child and post-promotion refusal behavior, with no change to eligibility, cadence, queue rotation, or concurrency admission.
- [ ] AC-8: Operator and package documentation explain forge-only issue counts, managed-parent polling, worker snapshot timing, legacy aliases, terminal execution-request semantics, and run prepared as preparation rather than completion/process-launch evidence; the repository aggregate make check gate passes.

## Evidence Inventory
| Criterion | Proof method | Source availability | Version/context bounds | Missing prerequisite / disposition |
|-----------|--------------|---------------------|------------------------|------------------------------------|
| AC-1 | Captured structured-log assertions for all three successful paths plus admission rejection cases. | Repository daemon logging, schedule, continuation, HTTP/SQLite fixtures available; new assertions required. | Offline tests of implementation revision; no production attribution or live agent capability claim. | Implement and run criterion-named checks; unsatisfied until executed. |
| AC-2 | Multi-source candidate fixture and captured refusal logs; ordering/deduplication assertions. | Repository discovery and backlog fixtures available; new combined-source assertion required. | Offline synthetic discovery inputs exercise actual repository code. | Implement and run criterion-named checks. |
| AC-3 | Synchronized workers and failure/cancellation injection assert one terminal event after body cleanup and in-flight release, with Quiesce joining the diagnostic; subsequent ticks verify capacity reuse. | Repository Quiesce, worker, logging and concurrency fixtures available; branch-specific assertions required. | Tests use bounded synchronization, not production timing assumptions; shutdown cancellation is distinct from process death. | Implement and run criterion-named checks for all reachable terminal branches. |
| AC-4 | Counting fake executor covers no-execution, multiple-comment, error, and control-only work paths. | Existing fake execution and comment fixtures available. | Proves invocation accounting, not authentic agent launches or runtime capability. | Implement and run criterion-named checks. |
| AC-5 | Captured tick summary equality checks with blocked workers for deterministic snapshot assertions. | Existing tick/logging fixtures and documented TickResult contract available. | Same-tick equality only; no promise about the next instant. | Implement and run criterion-named checks. |
| AC-6 | Two-parent local HTTP/real SQLite regression, repeated ticks and Quiesce; assert executor count and stored prepared-run absence, backlog and admission state. | Existing managed refinement, backlog and store fixtures available; dedicated quiet-polling test required. | Synthetic local forge proves repository path, not identities/count of production managed parents. | Implement and run criterion-named check; production identity remains outside this acceptance claim. |
| AC-7 | Existing and focused managed-child regression checks with unchanged parent timestamps, closed child, promotion refusal; scoped review of eligibility/order/cadence paths. | Repository managed-child/refinement fixtures available; focused gap coverage required. | Offline repository behavior only; no production request-cost claim. | Run relevant checks and record review evidence. |
| AC-8 | Scoped documentation review and make check transcript. | Repository documentation and Makefile available; implementation revision and check transcript not yet produced. | Documentation describes repository contract; aggregate checks prove only covered local behavior. | Update documentation and execute aggregate gate. |

## Evidence Boundaries
- Reviewed docs/decisions/nested-agent-evidence.md before defining candidate criteria.
- No criterion promises authentic nested-agent observation, historical issue recovery, or deployed-session attribution; externally captured authentic evidence is therefore not required for these bounded criteria.
- All acceptance evidence must use its leading AC identifier exactly; story authoring must carry each assigned criterion's inventory row into its full body and retain unsatisfied checks until run.
