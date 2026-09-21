# Plan node result, version 1
Return exactly one JSON object with these fields:
- version: integer 1.
- response: ask or review_ready. approved is factory-policy-only, never role output.
- request: typed lf_plan_story request, or null for an ask before a usable draft.
- comment: bounded plain-language explanation (at most 8192 UTF-8 bytes).
- questions: array of scoped nonblank question strings (at most 32, each 2048 bytes).
An ask has at least one question; review_ready has none and requires a request.
Unknown fields, duplicate object members, trailing values, unsupported versions,
missing required fields or malformed stage content must fail validation. Total
result size is bounded at 128 KiB; JSON nesting at 32.

The request fields are request_id, operation (create/update/retire), stage
(story/technical/acceptance), candidate, expected_revision when updating/retiring,
upstream (exact immutable references), shared_revision, draft, changed_sections
and replacements for retirement only. Request/candidate/shared/upstream references
are nonblank and at most 256 bytes; upstream/replacements at most 100 entries.
The factory binds the admitted parent, selected story, stage and active issue.
No parent, ordinal, issue target, credential or owner assertion is accepted from
roles. Preserve the request ID and exact content after lost replies; corrections
use a new request. A retire request does not itself constitute owner confirmation.

Draft fields: key, name, user_story {actor,want,benefit}, problem, in_scope,
not_in_scope, dependencies {story_keys,issue_refs}, frontend; progressive optional
architecture, criteria, tasks, design, guidance, decisions, assumptions, questions.
Stage story omits architecture, design, guidance, tasks and criteria entirely.
Technical supplies architecture and applicable guidance/design, never tasks or
criteria. Acceptance adds criteria [{id,text,proof_method}] and tasks
[{id,description,owner_role,criterion_ids,depends_on}]. IDs remain stable and every
criterion must be linked to a task. Authorized roles and issue references come
only from factory scope. Do not invent task ownership or dependency authority.

Architecture is {general:[{area,decision,rationale}],
data_model:[{entity,definition,constraints}],
service_design:[{component,responsibility,access}],
solution_design:[{element,contract,rationale}]}; only general is mandatory when
populated. Optional inapplicable arrays may be absent. Design is an array of
frontend references, not invented designs. Guidance values are go and agent-setup.
On update the activity merges unchanged sections; changed_sections explicitly
selects intent, architecture, criteria, tasks, dependencies, design, guidance or
conversation. Empty selected optional sections intentionally remove prior content.
Never clear applicable settled content implicitly.

A proof_method describes how future work will be proven, not a passing check.
Retain assumptions and material unresolved questions honestly. The factory binds
assembled third-stage review to current story/dependency/shared-decision revisions;
no agent-provided ready or approved string can substitute that owner evidence.
