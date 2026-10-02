# Research brief

## Request

Settle a concrete Plan workflow package with three owner-led stages, first-loop story sub-issues, independent story progress, targeted invalidation and one proposed story activity, incorporating the owner's answers without switching out of Research shaping.

## Decisions

- **d1** — Owner requires three major loops: user stories without technical details or acceptance criteria; architecture/reuse/data-model shaping; then a separate acceptance conversation, with human promotion controlling progression.
- **d2** — Owner requires concrete plan.dot, role folders and Markdown skills using the Research structure, rather than another planning-only exercise.
- **d3** — Shaping decision: the primary user story is an owner refining user needs, understanding technical tradeoffs and agreeing observable completion before authorizing the finished plan.
- **d4** — Shaping decision: every stage retains questions with proposed answers and reuses settled decisions instead of starting the conversation again.
- **d5** — Shaping decision: story-stage records contain stable identity, actor, need, benefit, boundaries and optional priority, with no technical detail, tasks or criteria.
- **d6** — Shaping decision: technical-stage records link story identities to reuse decisions, interfaces, permissions, entities, relationships, invariants, dependencies, failure behavior and provisional tasks.
- **d7** — Shaping decision: acceptance records use stable criterion identities linked to stories, observable outcomes and verification methods, with shared quality requirements explicitly mapped to affected stories.
- **d8** — Shaping decision: immutable per-story stage candidates retain revisions, upstream story and shared-decision revisions, decisions, assumptions and unresolved questions.
- **d9** — Shaping decision: each approval binds an authenticated owner reply to a story or explicit story set, stage, displayed revision and dependency revisions.
- **d10** — Shaping decision: questions, review-ready candidates and authenticated promotion are distinct outcomes, and promotion advances only the explicitly approved stories.
- **d11** — Owner requires targeted reopening of changed stories and affected dependents while unrelated stories retain their progress and valid approvals.
- **d12** — Shaping decision: final assembled-plan review requires all included stories to have complete architecture, tasks, observable criteria and valid links under the existing complete-plan validator.
- **d13** — Shaping decision: stage validation rejects duplicate identities, dangling references, dependency cycles and unresolved material questions at promotion while permitting recorded nonmaterial assumptions.
- **d14** — Owner requires story bodies, discussions and reviews on sub-issues from the first loop, with the parent reserved for the index, shared decisions and assembled-plan review.
- **d15** — Shaping decision: workers propose story changes and the factory applies authorized draft operations, rendering, review routing and final readiness effects.
- **d16** — Shaping decision: approval of planning criteria defines future obligations and neither accepts implementation nor authorizes an automatic mode change.
- **d17** — Shaping decision: final handoff reuses the early sub-issue identities and existing current-base, publication reconciliation, index and readiness controls instead of creating duplicate stories.
- **d18** — Shaping decision: the workflow package contains plan.dot, stories.md, technical.md and acceptance.md plus shared plan-evidence and focused story-shaping, technical-shaping and acceptance-shaping skills.
- **d19** — Shaping decision: a successful scope must show that unanswered questions, stale approvals, exhausted loops and failed effects cannot advance the workflow or mark publication complete.
- **d20** — Owner accepts a combined acceptance and assembled-plan review as the third gate without a fourth approval round.
- **d21** — Owner requires architecture review to be able to identify a story as irrelevant and return that story for revision or owner-confirmed retirement.
- **d22** — Shaping decision: introduce one proposed Plan story activity for draft create-and-attach, update and retire operations, reusing existing forge operations and generic graph control.
- **d23** — Shaping decision: story activity inputs identify the operation, stable story key, parent, current issue identity when present, expected remote content revision, candidate revision and effect request identity.
- **d24** — Shaping decision: story activity results retain the issue identity and URL, applied content revision and a distinct applied, conflict, failed or uncertain effect status.
- **d25** — Shaping decision: duplicate requests reconcile the existing effect, and a created issue whose attachment failed must be attached on recovery rather than created again.
- **d26** — Shaping decision: updates verify the current parent relationship and content base, preserve owner edits by stopping on conflict, and read back remote changes before reporting success.
- **d27** — Shaping decision: durable question and review waits correlate replies from each child issue to that story's stage and candidate, without requiring the owner to repeat feedback on the parent.
- **d28** — Shaping decision: direct owner edits are ingested as new story revisions and cannot preserve approvals against changed content or dependencies.
- **d29** — Shaping decision: dependencies reference story stages and shared decisions so impact review can invalidate affected downstream approvals without restarting unrelated stories.
- **d30** — Shaping decision: draft publication validates story identity, actor, need and benefit without demanding later-stage fields or marking the story ready for implementation.
- **d31** — Shaping decision: final aggregate approval binds the exact set of included story revisions and shared decisions, and any change to that set requires renewed final review.
- **d32** — Shaping decision: only the authenticated owner promotes or retires stories, while draft create and update effects run within the authorized Plan scope.
- **d33** — Shaping decision: success requires an unrelated approved story to retain approval when another changes, an affected dependent to reopen, and an architecture-rejected story to leave active scope only after owner confirmation.


## Assumptions

- **a1** — DOT means the repository's Graphviz workflow format, as supported by the shipped Research graph.
- **a2** — Superseded by owner instruction: first-loop stories are published draft sub-issues and are not private-only drafts waiting for final handoff.
- **a3** — Owner resolved q2 with a combined third review and q3 with per-story progression and targeted dependency invalidation rather than whole-stage resets.
- **a4** — One Plan story effect activity is justified by early draft publication; no separate role, approval, deletion or document activity is required by this scope.
- **a5** — Owner resolved q1 by requesting concrete workflow and Markdown files now; the factory's Research shaping ceiling prevents implementing or switching to Spike in this invocation.
- **a6** — Use the existing authenticated owner as the promoter rather than introducing delegates or multiple approvers, because no broader permission model was requested.
- **a7** — An approval combined with requested changes counts as feedback, and an ambiguous or stale reply requires a refreshed review rather than inferred promotion.
- **a8** — Loop limits are operational bounds, not approval rules: exhaustion retains the draft and stops for owner-directed continuation without discarding answers or advancing stages.
- **a9** — Removal means retirement and closure with a reason, preserving identity and discussion; splits and merges create replacement identities with links, and an empty active set cannot be a ready plan.
- **a10** — Acceptance discoveries that change user intent return to stories, while discoveries that change only technical choices return to technical shaping.
- **a11** — Existing active plans keep their admitted workflow, and adopting the proposed workflow for an existing plan requires an explicit owner-directed replan.
- **a12** — Failed or uncertain publication resumes from the recorded effect and reconciles remote state before retrying, preserving request identity to avoid duplicate stories.
- **a13** — The earlier workflow-draft artifact is superseded wherever it delays publication, assumes whole-stage progression or lacks per-story routing; it is historical evidence rather than the final build contract.
- **a14** — Formatting changes that do not change canonical candidate content preserve approval, while any canonical content change requires a fresh candidate review.
- **a15** — Use lf_plan_story as the proposed activity spelling because current admission permits mode-prefixed capabilities but not lf_story.
- **a16** — A ready story may enter its next stage while other stories remain earlier, provided its required dependency revisions are approved.
- **a17** — When impact cannot be established, hold the changed story and potential dependents for impact review while leaving demonstrably unrelated approvals intact.
- **a18** — Retirement closes as not planned rather than completed, requiring a retirement-specific state operation because the current completion helper closes with the completed reason.
- **a19** — Unrecognized external closure, deletion or reparenting blocks the affected story for reconciliation and never silently counts as owner-approved retirement or implementation completion.
- **a20** — The combined third review can approve criteria for individual stories as they become ready, but the final review must display and approve the assembled current set before feature readiness.
