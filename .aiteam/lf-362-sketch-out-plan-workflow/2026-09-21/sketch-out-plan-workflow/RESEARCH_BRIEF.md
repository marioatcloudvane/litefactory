# Research brief

## Request

Shape the proposed three-loop Plan workflow into a build-ready scope covering drafts, interfaces, permissions, validation, revisions, recovery and final handoff, while preserving the owner's story-first direction.

## Decisions

- **d1** — Owner requires three major loops: user stories without technical details or acceptance criteria; architecture/reuse/data-model shaping; then a separate acceptance conversation, with human promotion controlling progression.
- **d2** — Owner requires DOT and reuse of existing activities where they fit, preservation of the Research agent structure, and a first workflow and Markdown-layout draft.
- **d3** — Shaping decision: the primary user story is an owner refining user needs, understanding technical tradeoffs and agreeing observable completion before authorizing the finished plan.
- **d4** — Shaping decision: every stage retains questions with proposed answers and reuses settled decisions instead of starting the conversation again.
- **d5** — Shaping decision: story-stage records contain stable identity, actor, need, benefit, boundaries and optional priority, with no technical detail, tasks or criteria.
- **d6** — Shaping decision: technical-stage records link story identities to reuse decisions, interfaces, permissions, entities, relationships, invariants, dependencies, failure behavior and provisional tasks.
- **d7** — Shaping decision: acceptance records use stable criterion identities linked to stories, observable outcomes and verification methods, with shared quality requirements explicitly mapped to affected stories.
- **d8** — Shaping decision: each immutable stage candidate records its revision, upstream revisions, decisions, assumptions, unresolved questions and references to its story, technical or acceptance records.
- **d9** — Shaping decision: approval records identify the authenticated owner reply, stage, displayed candidate and upstream revisions, and an agent response alone cannot create approval.
- **d10** — Shaping decision: questions, ready-for-review candidates and authenticated promotion are distinct interface outcomes, and only promotion can advance to a later stage.
- **d11** — Shaping decision: owner edits reopen the earliest affected conversation and retain superseded records for comparison without silently rewriting approved intent.
- **d12** — Shaping decision: the final candidate must satisfy the existing complete-plan validator and contain nonempty stories, resolved dependencies, architecture, tasks, criteria and valid task-to-criterion links before owner review.
- **d13** — Shaping decision: stage validation rejects duplicate identities, dangling references, dependency cycles and unresolved material questions at promotion while permitting recorded nonmaterial assumptions.
- **d14** — Shaping decision: existing issue comments and durable review waits remain the owner interface, showing the current stage, changed content, outstanding decisions and the exact approval target.
- **d15** — Shaping decision: workers may investigate and retain drafts, while the factory owns rendering, publication, authorization and handoff through existing Plan controls.
- **d16** — Shaping decision: approval of planning criteria defines future obligations and neither accepts implementation nor authorizes an automatic mode change.
- **d17** — Shaping decision: final handoff preserves current-base validation, managed story identity, publication reconciliation, index replacement and readiness invalidation.
- **d18** — Shaping decision: the deliverable preserves three role documents, one shared evidence/result/state contract and one focused skill per stage using the Research role structure.
- **d19** — Shaping decision: a successful scope must show that unanswered questions, stale approvals, exhausted loops and failed effects cannot advance the workflow or mark publication complete.


## Assumptions

- **a1** — DOT means the repository's Graphviz workflow format, as supported by the shipped Research graph.
- **a2** — Story creation means private structured draft shaping until final handoff, because early drafts cannot satisfy the existing complete-story contract.
- **a3** — Proposed pending q2 and q3: use three candidate-bound approval gates and invalidate the changed stage plus all downstream stage approvals.
- **a4** — Reuse generic activities and existing Plan effect owners; a new activity is justified only by a documented missing durable effect boundary, not by introducing another role.
- **a5** — Proposed pending q1: this request delivers a build-ready workflow design rather than installing the workflow, based on the issue's request for a sketch and first draft.
- **a6** — Use the existing authenticated owner as the promoter rather than introducing delegates or multiple approvers, because no broader permission model was requested.
- **a7** — An approval combined with requested changes counts as feedback, and an ambiguous or stale reply requires a refreshed review rather than inferred promotion.
- **a8** — Loop limits are operational bounds, not approval rules: exhaustion retains the draft and stops for owner-directed continuation without discarding answers or advancing stages.
- **a9** — Removing, splitting or merging stories retires prior identities and records replacement links; retired identities are never reassigned, and an empty story set cannot become a completed plan.
- **a10** — Acceptance discoveries that change user intent return to stories, while discoveries that change only technical choices return to technical shaping.
- **a11** — Existing active plans keep their admitted workflow, and adopting the proposed workflow for an existing plan requires an explicit owner-directed replan.
- **a12** — Failed or uncertain publication resumes from the recorded effect and reconciles remote state before retrying, preserving request identity to avoid duplicate stories.
- **a13** — The earlier workflow-draft artifact is historical reference; the current structured decisions take precedence where its routing or stage descriptions conflict.
- **a14** — Formatting changes that do not change canonical candidate content preserve approval, while any canonical content change requires a fresh candidate review.


## Open questions

### q1 — Should this scope stop at a build-ready design or include delivering an executable Plan workflow?

_Asked of: Product Owner Questions_

- **a** — Recommended: finish the design and contracts for this sketch request, with production admission, adapters and rollout explicitly reserved for later authorized implementation.  *(recommended)*
- **b** — Scope an executable workflow as the eventual deliverable, including Plan admission, draft persistence, document generation, publication handoff and recovery; this Research run still only defines that work.

### q2 — Should acceptance criteria and the assembled plan share the third approval gate?

_Asked of: Product Owner Questions_

- **a** — Recommended: use three gates total; show the validated complete plan with the criteria in the third review, so one explicit approval covers both.  *(recommended)*
- **b** — Use four gates: approve criteria first, then separately review the assembled plan, adding a review round for stronger separation.

### q3 — How broadly should a changed stage invalidate approval?

_Asked of: Technical Questions_

- **a** — Recommended: invalidate approval of the changed stage and every later stage as whole sets; retain drafts and answers so the owner reviews the differences without repeating the conversation.  *(recommended)*
- **b** — Invalidate only affected stories and dependent decisions; this requires explicit dependency tracking and a reliable impact check before any unaffected approval can remain valid.
