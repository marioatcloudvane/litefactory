# Research brief

## Request

Identify Research-scoped activities that should coexist with shared generic workflow mechanics, including explicit Research brief generation; retain the full cutover without backward compatibility and defer Plan.

## Decisions

- **d1** — Owner requirement: share activity implementations across modes and make Plan authoring primarily Markdown roles and DOT topology.
- **d2** — Owner direction: use generic names for reusable mechanics and permit Research-scoped activities for domain work such as generating the Research brief.
- **d3** — Scope: share the existing package loading, admission, role delivery, activity execution, continuation and recovery mechanisms by passing explicit mode policy rather than copying Research handlers.
- **d4** — Shaping proposal: an author can add or reorder a role through Markdown and DOT without adding a handler when the role uses an existing result contract and permissions.
- **d5** — Scope: mode registration supplies Research response and payload validators, presentation templates and owner-gate policy, while document generation is an explicit Research activity.
- **d6** — Scope: domain behavior may use scoped activities as well as mode contracts, with no Plan adapter or publisher implemented in this change.
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
- **d20** — Acceptance: Research combines generic mechanics with its scoped brief activity, preserves its two-stage behavior and recovery guarantees, and a second-mode fixture reuses the mechanics without invoking Research document generation.
- **d21** — Owner direction: perform a complete Research cutover with no old action aliases, compatibility decoders or support for resuming pre-cutover graph instances.
- **d22** — Out of scope: Plan graph changes, Plan review or publication implementation, old Research graph compatibility, unrelated mode conversions and default production activation.
- **d23** — Owner revision to q1: ignore the existing Plan DOT and defer all Plan workflow work to a later stage.
- **d24** — Owner revision to q2: Plan approval policy is deferred and the discarded Plan DOT does not constrain this refactor.
- **d25** — Interface: lf_agent retains success/failed outcomes and Research's ask/done/approved vocabulary, with approved reporting owner authority rather than creating it.
- **d26** — Interface: lf_ask names a source candidate and question or review kind, confirms delivery with the existing published outcome, and releases the invocation until an authenticated reply resumes the workflow.
- **d32** — Scope: Research question and review paths use lf_ask and ordinary reports use lf_comment, with successful Research document generation required before publishing a successful stage result or handing it to the next stage.
- **d33** — Owner acceptance: the shared-runtime direction is accepted with a full Research cutover and Plan deferred.
- **d34** — Shaping assessment: extracting the existing brief writer into a scoped activity keeps the refactor bounded, but graph ordering and result-validation changes still require verification.
- **d35** — Shaping decision: add lf_research_brief as one Research-only activity that generates both the owner brief and technical findings document from one validated retained candidate.
- **d36** — Interface: lf_research_brief takes an explicit source node bound to the current instance and candidate revision and returns success with source-bound artifact references or failed with an actionable diagnostic.
- **d37** — Validation: validate the Research payload and artifact path declarations before retention, require existing data artifacts immediately, and require generated document existence after lf_research_brief succeeds.
- **d38** — Validation: graph admission requires the brief activity on every successful Research reporting and stage-handoff path, while failed-agent diagnostics may bypass it.
- **d39** — Permissions: lf_research_brief writes only the two factory-owned documents inside the admitted session and grants no publication, owner approval or mode-transition authority.
- **d40** — Recovery: repeating brief generation for the same candidate reproduces the same documents, and partial writes or artifact-validation failures cannot release success or allow downstream publication.
- **d41** — Shaping decision: keep Research result validation, document checks and report templates scoped internally without creating separate optional validation, report or completion activities.
- **d42** — Shaping decision: investigation and shaping remain Markdown roles executed by lf_agent, rather than dedicated Research agent activities.


## Assumptions

- **a1** — Keep scoped activities at meaningful domain-effect boundaries, reusing existing rendering and validation code rather than building a generic document framework.
- **a2** — Retain application-owned workflow packages and existing permission ceilings because no broader workflow-authoring authority was requested.
- **a4** — The owner explicitly waives backward compatibility for Research, superseding the earlier proposal to retain aliases.
- **a5** — Existing activation defaults and non-graph workflows remain unchanged because this request concerns the Research graph refactor.
- **a6** — Retain the existing persisted question, review, final and failure kinds while exposing asking and reporting through generic activity names.
- **a7** — Renaming creates a new definition digest; old graph sessions are unsupported and may be rejected explicitly without migration or automatic deletion of stored data.
- **a8** — Generic lf_ask extracts the existing publish-and-wait behavior rather than introducing a new approval-routing language.
