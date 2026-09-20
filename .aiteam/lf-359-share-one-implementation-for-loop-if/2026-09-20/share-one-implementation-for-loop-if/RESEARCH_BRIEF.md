# Research brief

## Request

Share generic graph activities and lifecycle across modes, including renaming to mode-neutral actions; adapt the existing Plan workflow on main, preserve its review rules, and retain lf_plan_publish for managed story publication.

## Decisions

- **d1** — Owner requirement: share activity implementations across modes and make Plan authoring primarily Markdown roles and DOT topology.
- **d2** — Owner direction: rename shared actions to lf_agent, lf_if, lf_loop and lf_comment, keep asking generic, and retain lf_plan_publish for Plan publication.
- **d3** — Shaping proposal: share package loading, launch admission, role delivery, agent execution, branching, bounded loops, comments, owner waits and recovery across registered modes.
- **d4** — Shaping proposal: an author can add or reorder a role through Markdown and DOT without adding a handler when the role uses an existing result contract and permissions.
- **d5** — Shaping decision: mode registration supplies package, stage contracts, closed response and routing vocabularies, validators, rendering and domain completion hooks to the shared runtime.
- **d6** — Shaping decision: lf_plan_publish owns Plan consistency checks and managed publication while mode adapters never duplicate scheduling, runner invocation, asking, comment delivery or recovery.
- **d7** — Shaping proposal: workflow definitions identify mode, version, content digest, roles, resources, nodes, selected source, outcomes and persistent execution limits.
- **d8** — Shaping proposal: retained execution records bind each result to its workflow instance, pinned definition, node, attempt and revision, with correlated comments, waits, replies and effect receipts.
- **d9** — Shaping decision: private agent results contain schema version, stage response and typed candidate data, while gate approvals and publication receipts are factory-owned records outside agent authority.
- **d10** — Shaping decision: Plan drafts retain stable story IDs, set and stage revisions, stage content, decisions, assumptions, questions, dependencies, criteria, task mappings and supersession links with stage-specific completeness checks.
- **d11** — Shaping proposal: asking requires an incomplete result with permitted questions and a durable owner wait, while finishing a stage carries no implicit approval or publication authority.
- **d12** — Shaping decision: approvals bind authenticated owner replies to the displayed stage and set revision, with changes invalidating the earliest affected stage and all later approvals.
- **d13** — Shaping proposal: Markdown and DOT may select registered capabilities but cannot enlarge the factory permission ceiling, choose another mode or execute arbitrary expressions.
- **d14** — Shaping decision: registration metadata authorizes each generic or domain activity for the graph mode, while validation rejects unknown actions, outcomes, sources, unbounded cycles and bypassed gates.
- **d15** — Shaping decision: lf_if and lf_loop compare declared response or route values from the latest selected activity attempt, rejecting invalid or missing retained output rather than falling back to an older success.
- **d16** — Shaping decision: lf_comment delivers a report without opening a wait, while lf_ask displays a question or review and owns durable reply correlation through the same shared delivery machinery.
- **d17** — Shaping proposal: duplicate replies, lost delivery acknowledgments and restart recovery reuse retained identities without repeating an acknowledged effect.
- **d18** — Shaping decision: stale replies cannot advance gates, malformed output and incompatible definitions stop explicitly, and limit exhaustion preserves drafts for an explicit bounded continuation without approval.
- **d19** — Shaping decision: Plan readiness requires confirmed publication, owner review of the exact published set, and successful shared lifecycle save, with no automatic transition to Implement.
- **d20** — Shaping decision: acceptance covers Research and the existing Plan graph using shared handlers, file-only role additions, registered actions, revision-bound gates, recovery and prevention of premature publication or readiness.
- **d21** — Shaping decision: new definitions use generic names and new digests, while pinned Research definitions retain legacy aliases backed by shared handlers and preserve outstanding waits and effect identities.
- **d22** — Shaping decision: exclude a replacement Plan workflow, new approval policy, unrelated mode conversions, arbitrary workflow uploads, dynamic plugins and automatic default activation.
- **d23** — Owner answer q1: adapt the Plan workflow already on main and correct its unsupported activities instead of creating the previously proposed minimal Plan workflow.
- **d24** — Owner answer q2: preserve reviews after stories, engineering and acceptance plus final review after publication, as specified by the existing Plan package.
- **d25** — Shaping decision: lf_agent returns success or failed and retains a stage response of ask or done, neither of which can create an owner approval.
- **d26** — Shaping decision: lf_ask uses an explicit candidate source, stage and question-or-review kind and returns waiting, answered, approved, changed or failed with factory-validated routing data.
- **d27** — Shaping decision: lf_plan_publish returns confirmed, pending, failed or the earliest invalid stage and performs no remote write until all three current whole-set approvals and the complete typed set pass validation.
- **d28** — Shaping decision: replace Plan review placeholders with lf_ask and lf_if routing, fold consistency into lf_plan_publish, and replace the save placeholder with shared lifecycle finalization after final approval.
- **d29** — Shaping decision: meaningful membership or intent changes reopen stories, architecture or dependency changes reopen engineering, and criteria-only changes reopen acceptance unless earlier meaning changes.
- **d30** — Shaping decision: final review displays every confirmed active issue and exact revision, and any published-content change invalidates final approval.
- **d31** — Shaping decision: publication preserves surviving issue identities and supersession history, removes discarded drafts only from the active set, and reconciles uncertain effects before any retry.
- **d32** — Shaping decision: new owner-gated Research paths use lf_ask while existing pinned comment nodes retain their historical wait behavior through compatibility handling.


## Assumptions

- **a1** — Interpret minor code additions as ongoing mode-registration and domain-adapter work after a one-time shared-runtime refactor, not a line-count limit on this refactor.
- **a2** — Retain application-owned workflow packages and existing permission ceilings because no broader workflow-authoring authority was requested.
- **a3** — The existing Plan review, consistency, publication and final-review contracts settle q2 and remain authoritative for behavior in the adapted graph.
- **a4** — Superseded by owner direction: mode-scoped generic names are not retained for new graphs; old names exist only to resume already pinned definitions.
- **a5** — Keep existing activation defaults unchanged; adapting the existing Plan package does not itself authorize production activation or establish live-provider compatibility.
- **a6** — Use question and review as generic ask kinds and final and failure as report kinds; compatibility handling preserves the existing persisted comment-kind vocabulary.
- **a7** — Renaming changes semantic digests; never rewrite running captures or effect keys, and explicitly refuse an unsupported old capture rather than silently adopting new topology.
- **a8** — Use lf_ask as the generic name for the durable question/review gate, interpreting the owner's request for generic asking as a shared factory capability rather than an agent permission.
- **a9** — Wording-only changes may preserve stage approvals only with retained evidence of unchanged meaning; uncertainty reopens review, and final published-content changes always require renewed final review.
- **a10** — Preserve the existing Plan graph's limit of eight per declared bounded activity and require explicit continuation after exhaustion.
