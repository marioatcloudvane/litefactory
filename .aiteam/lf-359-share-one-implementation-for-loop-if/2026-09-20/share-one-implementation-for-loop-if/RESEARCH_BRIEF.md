# Research brief

## Request

Assess how to make graph workflows generic across modes without copying activity implementations, with Plan added primarily through agent Markdown files and a DOT graph plus minor mode-specific code. Disregard the issue's proposed fix and everything following it as a prescribed solution.

## Decisions

- **d1** — Owner requirement: share activity implementations across modes and make Plan authoring primarily Markdown roles and DOT topology.
- **d2** — Owner direction: the issue's proposed fix is not a prescribed design.
- **d3** — Shaping proposal: share package loading, launch admission, role delivery, agent execution, branching, bounded loops, comments, owner waits and recovery across registered modes.
- **d4** — Shaping proposal: an author can add or reorder a role through Markdown and DOT without adding a handler when the role uses an existing result contract and permissions.
- **d5** — Shaping proposal: each mode registration declares its package, response vocabulary, result and artifact validators, comment rendering, control policy and completion adapter.
- **d6** — Shaping proposal: mode adapters contain domain validation and completion effects but no copies of scheduling, provider invocation, comment delivery or recovery.
- **d7** — Shaping proposal: workflow definitions identify mode, version, content digest, roles, resources, nodes, selected source, outcomes and persistent execution limits.
- **d8** — Shaping proposal: retained execution records bind each result to its workflow instance, pinned definition, node, attempt and revision, with correlated comments, waits, replies and effect receipts.
- **d9** — Shaping proposal: a private stage envelope carries a schema version, registered response and validated stage result, while whole-mode completion is a separate factory decision.
- **d10** — Shaping proposal: Plan stages may retain validated partial planning context and story drafts, but the designated completion boundary requires the full existing typed Plan contract.
- **d11** — Shaping proposal: asking requires an incomplete result with permitted questions and a durable owner wait, while finishing a stage carries no implicit approval or publication authority.
- **d12** — Shaping proposal: any approval-gated transition requires an authenticated owner reply correlated to the reviewed candidate, and a changed candidate requires fresh approval.
- **d13** — Shaping proposal: Markdown and DOT may select registered capabilities but cannot enlarge the factory permission ceiling, choose another mode or execute arbitrary expressions.
- **d14** — Shaping proposal: graph validation rejects foreign-mode actions, unknown responses or kinds, invalid sources, ambiguous outcomes, unbounded cycles and paths that bypass a required wait.
- **d15** — Shaping proposal: controls use the latest valid attempt of their selected source and reject missing or invalid output after an attempt instead of applying a first-pass default.
- **d16** — Shaping proposal: question and review comments open owner waits, final comments report the selected stage result, and failure comments stop execution without autonomous activity retries.
- **d17** — Shaping proposal: duplicate replies, lost delivery acknowledgments and restart recovery reuse retained identities without repeating an acknowledged effect.
- **d18** — Shaping proposal: stale replies, exhausted limits, malformed output and incompatible pinned definitions stop with an actionable reason instead of silently restarting or advancing.
- **d19** — Shaping proposal: Plan readiness requires successful existing managed publication and saved publication index, independently of a successful graph comment.
- **d20** — Shaping proposal: acceptance requires both modes to exercise the same runtime, a role addition requiring only files, owner-wait and crash-recovery checks, and rejection of unauthorized Plan completion.
- **d21** — Shaping proposal: existing Research behavior, captured definitions, outstanding waits and delivery identities must survive adoption or be refused explicitly without being rewritten.
- **d22** — Shaping proposal: exclude unrelated mode conversions, new review features from related tickets, arbitrary workflow uploads, dynamic code plugins and automatic production activation.


## Assumptions

- **a1** — Interpret minor code additions as ongoing mode-registration and domain-adapter work after a one-time shared-runtime refactor, not a line-count limit on this refactor.
- **a2** — Retain application-owned workflow packages and existing permission ceilings because no broader workflow-authoring authority was requested.
- **a3** — Preserve existing managed Plan validation, generation identity, publication recovery and readiness; owner-review placement is pending q2.
- **a4** — Keep mode-scoped action names because renaming does not reduce handler duplication and would add definition compatibility work.
- **a5** — Keep Research opt-in and any new Plan graph opt-in; activation and live-provider certification are separate from this scope.
- **a6** — Use the existing question, review, final and failure comment kinds for both modes; new domain semantics do not require arbitrary new persisted kinds.
- **a7** — Do not promise unchanged digests or migration-free adoption; preserve old captures and effect keys, with explicit compatibility handling where storage generalization requires it.


## Open questions

### q1 — Should this issue deliver a working opt-in Plan graph as well as the shared runtime?

_Asked of: Product Owner Questions_

- **a** — Recommended: deliver the shared runtime, migrate Research onto it, and include a minimal opt-in Plan graph using Markdown roles, DOT and the existing Plan completion adapter; leave additional review features to their own issues.  *(recommended)*
- **b** — Deliver the shared runtime and migrate Research only, with a second-mode test package proving reuse; deliver the real Plan graph separately, so its end-to-end integration remains unproven here.

### q2 — Where should the minimal Plan graph require owner approval?

_Asked of: Product Owner Questions_

- **a** — Recommended: let completed internal stages advance automatically, but require owner approval of the complete Plan candidate before managed story publication; questions always pause for a reply.  *(recommended)*
- **b** — Require owner approval after every stage and again before publication, giving more control with more pauses.
- **c** — Add no new approval gate beyond existing Plan authorization; publish when the full candidate passes the current completion contract.
