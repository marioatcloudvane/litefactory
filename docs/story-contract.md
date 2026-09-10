# Structured story protocol

Content/submission contract 2 makes the agent supply typed content and the factory
render Markdown. Contract 1 remains supported for existing generations. This
selection is independent of workflow: separated Implement records engineering and
executed tests; read-only Validate assesses retained evidence. Legacy workflow
retains its existing scoped verification and effects. Neither a comment nor
republication upgrades a contract or selects another mode.

## Contributions and helper feedback

Use the absolute executable and descriptor supplied for the current attempt:

```sh
"$helper" submission preflight --descriptor "$descriptor" < contribution.json
"$helper" submission submit --descriptor "$descriptor" < contribution.json
"$helper" submission receipt --descriptor "$descriptor" < receipt-query.json
```

These variables stand for supplied paths, not new configuration or credentials.
JSON goes on stdin. Receipt query content is `{"request_id":"returned-request-id"}`.
The helper sends bounded requests over an owner-only Unix socket with an opaque
attempt capability. Never expose the descriptor/capability or invent a permissive
environment. Cold, resumed and fallback launches receive their current binding;
old attempt authority does not transfer. The factory supplies parent, run,
generation, issue identity, revision, workflow, sequence and attempt. Contributions
must not override these fields or access the forge.

Preflight and submit share schema and semantic validation. Diagnostics list
indexed paths, codes, expected/actual types and correction messages in stable
order. `complete=false` and unchecked paths disclose incomplete inspection; they
are not validity. Preflight allocates no event and grants no acceptance. Submit
journals correlation before sending. Retrying identical bytes recovers the same
request/receipt after a lost reply; corrected bytes acquire a new request identity.
Keep the original correlation when a connection fails.

Read each receipt dimension: input validity, durable engineering record,
acceptance decision and pending/failed remote effects. Recorded evidence does not
mean a checkbox was projected, an issue closed or parent delivery completed. A
clean process exit and generated summary establish none of those facts.

The contribution has `stage`, `outcome` and stage-specific content. Plan uses
`plan` for completed or `context` for discussion. Story stages select an authorized
`story`; engineering references snapshot/check IDs (or the supplied capture
contract), and assessment references retained snapshots with criteria/reviews.
Use the exact stage shape in the factory prompt; incompatible combinations reject.
Discussion saves working context without a proposal. Plan completion acknowledges
validated durable input while publication/readiness stay pending until terminal
success, publication, link-only Story Index replacement and successful save.

## Typed Plan shape

The Plan object contains `summary` and `stories`. Each story requires:

| Field | Shape |
|---|---|
| key, intent, title | Strings; create/update/unchanged intent must match factory-known scope |
| user_story | actor, want, benefit strings; notes string array |
| acceptance_criteria | Array of objects with id and text |
| tasks | Array of id, description, owner_role, criterion_ids array, depends_on array |
| architecture | summary; components(name/responsibility); interfaces(name/input/output); constraints strings; decisions(decision/reason) |
| dependencies | story_keys and issue_refs arrays |

All fields and collections are required; use an empty array when allowed. Missing,
null, duplicate, unknown or wrongly typed fields reject. IDs are canonical AC-001
through AC-999999, padded to at least three digits without excess zeros. Require
1–256 active criteria, at least one task, known roles/references and acyclic graphs.
The factory renders all five sections deterministically and freezes typed criteria
against the exact issue revision. Narrative never declares another criterion.

Limits: 4,096 UTF-8 bytes per criterion; 65,536 rendered story bytes; 8 MiB escaped
Plan transport; 4 MiB submission/assessment metadata; 64 MiB retained data per run.
Aggregate validation can reject a combination of individually valid maxima.

New criterion/review evidence is exclusively list B. For example, the `evidence`
field of a criterion finding is:

```json
[
  {"check_ids":["check-01"],"summary":"The retained boundary test passed."},
  {"check_ids":[],"summary":"Required external observation is unavailable."}
]
```

Require 1–16 entries, each summary at most 8,192 UTF-8 bytes. Every reference must
resolve to properly attributed immutable retained proof. Empty references may
explain source review or missing evidence; required execution still needs real
retained results. Legacy string evidence retains its old meaning only on contract
1. Unknown criteria and incomplete required review coverage cannot prove a result.

## Changing an issue

Prefer an authenticated numeric-owner comment describing the change. Ordinary
comments continue the selected mode and existing child-refinement/parent Plan
admission gates; they cannot bypass active-run/replan locks or authorize unattached
follow-ups. Durable deduplication prevents repeated processing.

Recognizable inline edits to all five sections are supported. Indented criterion
continuations remain with their item; explanatory prose stays narrative. Reordering
and proven presentation equivalence preserve IDs. New items get the next unused
canonical ID; replaced/removed obligations retire IDs permanently. Uncertain
wording remains pending for ordinary-language Plan refinement.

The supported explicit clarification is a child-owner comment on its own line:

```text
Clarify AC-001: String must be x, as described in the existing specification.
```

It preserves that ID only for the authenticated admitted child refinement, parent
Plan run, generation and exact replacement text. A quoted, unrelated or unmatched
comment cannot grant that mapping. Without an established clarification, a Plan
replacement such as “String must be x” to “String must be y” gets a new ID. Inline
semantic uncertainty stays pending rather than guessing equivalent meaning.

The factory preserves known unrelated issue bytes, rechecks the exact base before
update/freeze and confirms resulting bytes. Missing sections, contradictory
dependencies and revision conflicts retain specific pending explanations. A
post-write mismatch retains recoverable conflict state. GET/PATCH/GET cannot
protect against an unobservable intervening edit and is not an atomic transaction.
Semantic edits need the next approved scope; active frozen bundles and historical
separated acceptance remain unchanged. Proven presentation-only equivalence does
not require requirement reapproval.

Each rendered story ends outside obligations with:

> Want to change this story? Comment with the requested change and litefactory will update it. Direct edits are supported, but comments help avoid conflicting revisions.

## Explicit owner upgrade

```sh
litefactory story-contract upgrade github:owner/repo#123
litefactory story-contract upgrade github:owner/repo#123 --preview
litefactory story-contract upgrade github:owner/repo#123 --apply
```

Preview is the default, uses existing configured identities and registered repo,
and lists the unfinished feature cohort, exact observed/expected/target revisions,
contract/generation and conversion conflicts. It does not migrate the database,
write issues, create upgrade intent or change readiness. `--preview` and `--apply`
are mutually exclusive. Configured bot credentials must match stored numeric bot
identity; credentials are never supplied to a coding agent.

Conversion is conservative: all five recognizable legacy sections, standard
“As …, I want …, so that …” user story, explicit canonical criterion checklist,
legacy six-column task table and consistent managed dependencies are required.
Legacy size/status are retained in task description; tasks reference all criteria
because legacy tables lack explicit links. Architecture prose becomes its summary.
Ambiguous prose criteria, missing sections and contradictory dependencies refuse.
Resolve those through normal Plan editing and preview again.

Apply requires a safe planning cohort: active/frozen or mixed contracts and pending
legacy effects refuse before publication. It rechecks every source revision,
retains immutable publication intent, blocks new admission and updates only that
cohort. A partial failure remains pending: rerun the same command to resume it,
without duplicating children or overwriting competing revisions. Already confirmed
bytes are recognized; confirmation conflicts need the preserved source reconciled
before retry can finish. Successful apply creates an unapproved next generation
and leaves readiness false. Publish and approve the next Plan before engineering.

Completed legacy stories remain unchanged, opaque historical dependencies in typed
Plans; the factory retains their identities/bodies rather than asking the agent to
synthesize typed obligations. Old frozen generations, evidence/acceptance, pending
legacy effects and workflow versions remain readable under their original contract.

## Evidence and delivery boundaries

Synthetic tests cover offline behavior, not actual sandbox/helper usability.
Authentic observations of BOTH actual Claude and Codex invoking the helper,
receiving diagnostics, correcting within the same run and observing a correlated
authoritative decision before exit remain unavailable for this feature. Follow
[external capture guidance](agent-evidence-capture.md); never acquire these through
nested paid agents, permission widening or new checks in Validate.

Validate uses retained checks only. Numeric-owner reasoned exceptions can waive
exact substantive scope while preserving failed/unverified facts; they cannot
fabricate checks or integration. Mode changes are explicit. All-attached-child
readiness and approved linked follow-ups remain required. Final delivery needs
explicit concrete PR/current-head/main approval, renewed after a changed head,
and confirmed main integration before parent closure. See [shared workflow](../teams/source/skills/workflow/SKILL.md).
