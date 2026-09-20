# Research brief

## Request

Fully convert the Research workflow to shared generic activities and mode-neutral names, with no backward compatibility; defer Plan entirely and provide a bounded scope sketch and effort assessment.

## Decisions

- **d1** — Owner requirement: share activity implementations across modes and make Plan authoring primarily Markdown roles and DOT topology.
- **d2** — Owner direction: replace all Research graph action names with generic names and reserve lf_plan_publish for later Plan-specific publication work.
- **d3** — Scope: share the existing package loading, admission, role delivery, activity execution, continuation and recovery mechanisms by passing explicit mode policy rather than copying Research handlers.
- **d4** — Shaping proposal: an author can add or reorder a role through Markdown and DOT without adding a handler when the role uses an existing result contract and permissions.
- **d5** — Scope: a small mode registration supplies package identity, allowed responses, result and artifact validation, report rendering and owner-gate policy.
- **d6** — Scope: domain behavior remains behind explicit mode contracts, with no Plan adapter or publisher implemented in this change.
- **d7** — Data model: retain definition mode and digest, roles, resources, source nodes, outcomes and durable limits without adding a general workflow schema.
- **d8** — Data model: reuse the existing instance, attempt, result, comment, owner-wait, reply and delivery records with their identity checks.
- **d9** — Interface: keep the versioned private result envelope and delegate its response vocabulary and payload validation to the registered mode contract.
- **d11** — Shaping proposal: asking requires an incomplete result with permitted questions and a durable owner wait, while finishing a stage carries no implicit approval or publication authority.
- **d12** — Validation: an approval report requires a matching authenticated owner reply for the reviewed Research candidate, and worker completion or a stale reply cannot grant approval.
- **d13** — Shaping proposal: Markdown and DOT may select registered capabilities but cannot enlarge the factory permission ceiling, choose another mode or execute arbitrary expressions.
- **d14** — Validation: generic action registration explicitly declares allowed modes, and graph admission rejects unknown actions, invalid sources or responses, ambiguous edges, unbounded cycles and paths bypassing owner waits.
- **d15** — Interface: lf_if and lf_loop retain true/false outcomes and compare the selected source response, using a declared initial default only before that source has been attempted.
- **d16** — Interface: lf_comment publishes final or failure reports, while lf_ask reuses the same delivery mechanism for question or review reports and opens a durable owner wait.
- **d17** — Shaping proposal: duplicate replies, lost delivery acknowledgments and restart recovery reuse retained identities without repeating an acknowledged effect.
- **d18** — Edge cases: invalid source output, failed activities and exhausted limits preserve diagnostics and cannot silently retry or advance.
- **d20** — Acceptance: Research uses only generic activity names, retains its two-stage behavior and recovery guarantees, and a second-mode fixture exercises the same handlers without copying runtime code.
- **d21** — Owner direction: perform a complete Research cutover with no old action aliases, compatibility decoders or support for resuming pre-cutover graph instances.
- **d22** — Out of scope: Plan graph changes, Plan review or publication implementation, old Research graph compatibility, unrelated mode conversions and default production activation.
- **d23** — Owner revision to q1: ignore the existing Plan DOT and defer all Plan workflow work to a later stage.
- **d24** — Owner revision to q2: Plan approval policy is deferred and the discarded Plan DOT does not constrain this refactor.
- **d25** — Interface: lf_agent retains success/failed outcomes and Research's ask/done/approved vocabulary, with approved reporting owner authority rather than creating it.
- **d26** — Interface: lf_ask names a source candidate and question or review kind, confirms delivery with the existing published outcome, and releases the invocation until an authenticated reply resumes the workflow.
- **d32** — Scope: convert every Research question and review publication to lf_ask and every ordinary report to lf_comment, updating role instructions and validation together.
- **d33** — Owner acceptance: the shared-runtime direction is accepted with a full Research cutover and Plan deferred.
- **d34** — Shaping assessment: this reduced scope is a modest small-to-medium refactor whose main uncertainty is Research-specific lifecycle coupling, not control comparison logic.


## Assumptions

- **a1** — Minor effort means a focused extraction of existing behavior, not a new workflow platform or a commitment to a specific hour estimate.
- **a2** — Retain application-owned workflow packages and existing permission ceilings because no broader workflow-authoring authority was requested.
- **a4** — The owner explicitly waives backward compatibility for Research, superseding the earlier proposal to retain aliases.
- **a5** — Existing activation defaults and non-graph workflows remain unchanged because this request concerns the Research graph refactor.
- **a6** — Retain the existing persisted question, review, final and failure kinds while exposing asking and reporting through generic activity names.
- **a7** — Renaming creates a new definition digest; old graph sessions are unsupported and may be rejected explicitly without migration or automatic deletion of stored data.
- **a8** — Generic lf_ask extracts the existing publish-and-wait behavior rather than introducing a new approval-routing language.
