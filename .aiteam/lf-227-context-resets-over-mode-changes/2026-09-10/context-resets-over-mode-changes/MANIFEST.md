# Manifest
# Branch: lf/227-context-resets-over-mode-changes
# Session: 2026-09-10

## Modes

- [x] **Research** — `RESEARCH_BRIEF.md` *(complete; run 0004 simplified recommendation)*
- [ ] **Plan** — run 0007 replacement and retirement explicitly authorized; narrow spec and architecture ready, current factory preflight still rejects removal of indexed stories. Earlier `plan_brief.md` is superseded historical scope, not current readiness.
- [ ] **Implement** — code + tests *(not started)*
- [ ] **Validate** — verification findings *(not started)*
- [ ] **Spike** — `SPIKE.md` *(not started)*

**Validate** returns read-only structured findings through the supplied contract; no manifest checkbox is written by a Validate run.

Modes do not follow on from one another. A box is ticked by a run that a human asked for; nothing here licenses starting the next one.

## History

- 2026-09-10 — session created for issue #227, run 0001.
- 2026-09-10 — Research completed for issue #227, run 0001: documented configuration-dependent conversation resets, durable phase handoffs, context-loss risks and decisions for Plan. Headless instruction applied; no confirmation requested.

- 2026-09-11 — Research completed for issue #227, run 0002: reverified reset and handoff source behavior, consolidated eight prior questions into three owner policy decisions with recommendations/tradeoffs, and documented independently answered facts and engineering defaults. Reused the existing session; no interactive questions or implementation.

## Notes

- 2026-09-11 — Plan completed for issue #227, run 0003: adopted the three owner-approved recommendations, produced feature-spec.md, approved ARCH_REVIEW.md, and shared plan_brief.md with seven typed stories. Contribution awaits authorized factory preflight/submission, publication and save; local artifacts do not establish readiness.
- No implementation, commits or pushes. Authentic feature-specific lifecycle evidence remains an unsatisfied external prerequisite in US-007; US-006 requires manual Claude execution for the protected Codex instruction tree.

## Factory submission — run 0003

- Preflight: valid and complete; no diagnostics or unchecked paths.
- Submitted request: `c7b606e2cf950aff3498bcefc890b634`.
- Receipt: input valid; Plan pending; engineering, acceptance and effects not applicable.
- Publication, story links, save and readiness remain factory-owned and pending at subprocess completion.

- 2026-09-12 — Research completed for run 0004: rechecked current code at c8b7bf0e42b8209aa13f745bc678a7909f8d5ab1; recommendation reduced to mode-aware existing segment selection plus an existing-session prompt pointer. Prior seven-story Plan direction is superseded by this research recommendation; its artifacts and factory state were not modified. A later explicit Plan iteration must reconcile scope. No implementation, tests, forge mutations, commits or pushes.

## Plan run 0005 — 2026-09-12

- Owner approved the simplified direction. Requirements and architecture now describe one bounded story, three tasks and five candidate criteria; no product questions remain.
- Updated `feature-spec.md`, `ARCH_REVIEW.md` and `PLAN_CONTEXT.json`. Earlier `plan_brief.md` and its Story Index remain historical factory-published scope; they were not overwritten to simulate retirement.
- Completed-candidate preflight: invalid, complete inspection, no unchecked paths. Diagnostic at `$.plan`, code `typed_plan`: `story US-001: preserve indexed and completed dependencies`. Candidate was not submitted and its transient file was removed.
- Source review confirms omission cannot retire indexed stories and the current contract has create/update/unchanged only. Safe completion requires factory-supported reconciliation/replacement of superseded unfinished scope and refreshed authoritative context. No invented retirement CLI, no-op story workaround or retained unwanted scope.
- Saved discussion contribution handed to the root for authorized helper preflight/submission. Publication and readiness are not established.
- No implementation, test execution, forge mutations, commits or pushes.

## Factory submission — run 0005

- Replacement proposal preflight rejected: `story US-001: preserve indexed and completed dependencies`. No completed proposal submitted.
- Discussion preflight passed: valid and complete, no diagnostics or unchecked paths.
- Discussion request: `d443bfae6108c68a145833fa72da666b`; input valid, Plan discussion; engineering/acceptance/effects not applicable.
- Simplified scope saved as working context; replacement publication and readiness remain blocked on factory reconciliation of prior indexed stories.

## Plan run 0006 — 2026-09-12

- Owner requested a brief explanation of mode-change detection; no scope change or new product question. Answer based on retained architecture: compare admitted mode with last actual use of the candidate conversation, allocate forward on differences, preserve eligible same-mode resume.
- Updated saved context and prepared typed discussion contribution for the current authorized helper handoff. No completed proposal retried; prior seven-story reconciliation blocker remains unresolved.
- No code changes, tests, commits or pushes. Submission receipt remains to be recorded by the dispatcher.

## Plan run 0007 — 2026-09-12

- Owner explicitly authorized finalizing the simple plan and retiring the old user stories. No further product decision or permission is required. Existing one-story, three-task, five-criterion specification and architecture remain the intended scope.
- Current read-only protocol review confirms only create/update/unchanged intents (`docs/story-contract.md:57`, `internal/planbrief/typed_content.go:181`); no whole-story retirement collection exists. Omission is rejected by `internal/turn/typed_plan.go:280`. Criterion-ID retirement is not story retirement.
- Current-bound one-story replacement preflight: valid=false, complete=true, no unchecked paths. Diagnostic `$.plan`, `typed_plan`: `story US-002: preserve indexed and completed dependencies`. No completed proposal submitted; no old story retired.
- Saved explicit retirement authorization and exact blocker in PLAN_CONTEXT.json; prepared typed discussion contribution for root helper submission. Input acknowledgment, publication, effects and readiness remain distinct; root records returned receipt separately.
- Required next step: factory-supported retirement/reconciliation of superseded unfinished indexed scope, preserving historical acceptance, then a fresh bound Plan publication. No invented command or manual daemon-state/forge workaround.
- No implementation, test execution, commits or pushes. Existing historical Story Index remains intact.

- Run 0007 discussion submission: request `f0fb4e6cc470ed53ed1dd0b91c774355`; receipt states: {"InputState": "valid", "EngineeringState": "not_applicable", "AcceptanceState": "not_applicable", "EffectState": "not_applicable", "PlanState": "discussion"}. No retirement or replacement publication performed.

## Plan run 0008 — 2026-09-12

- Owner asked whether manual issue closure is required. Answer from retained protocol evidence: closing the GitHub issues alone does not remove indexed factory scope or resolve replacement validation. No new product or permission question; explicit retirement authorization remains settled.
- Updated PLAN_CONTEXT.json and prepared a typed discussion contribution for the current root helper handoff. No repeated requirements/architecture work or completed-candidate preflight.
- Factory-supported retirement/replan reconciliation remains the technical prerequisite. No verified operator workaround is known; none was invented. Publication/readiness remain unestablished.
- No code changes, tests, forge/state edits, commits or pushes. Root records the discussion receipt separately.

- Run 0008 discussion request `c553941bf2e5fb1ac5e2391262577178`: {"InputState": "valid", "EngineeringState": "not_applicable", "AcceptanceState": "not_applicable", "EffectState": "not_applicable", "PlanState": "discussion"}. No stories closed or retired.
