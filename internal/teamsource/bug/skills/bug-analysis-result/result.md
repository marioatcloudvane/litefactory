# Version-1 Bug analyst results

Use only communication_preflight, communication_submit and communication_receipt from the factory result service. The service binds the task, empty effective mode, workflow bug, role bug-analyst, invocation and base revision. Preserve those bindings and all stable IDs.

The result contains outcome, comment, reason when incomplete/blocked, common questions when asking, and payload.bug. Bug carries response, invocation, base_revision, optional analysis, optional approval and optional evidence. No writable execution state exists.

Analysis contains expected, actual, reproduction (steps, environment, observation: observed/owner_reported/not_observed), cause, evidence, remedy, exclusions, criteria (id, requirement, measurement), decisions, assumptions, questions (id, text, material), and answer_references. Use empty arrays rather than null. Criteria IDs and question meanings remain stable across revisions. Review requires complete evidenced cause/remedy/criteria and resolved material questions.

ask pairs with incomplete and includes matching common/analysis questions and partial analysis. done pairs with completed and supplies complete reviewable analysis. blocked pairs with blocked, a nonblank obstacle and evidence; it may retain partial analysis. approved pairs with completed and reports the supplied approval revision/digest/review without changing analysis. Only authenticated factory owner consumption creates approval.

Preflight complete input; correct every diagnostic. Submit, retain request_id, and read its matching receipt. On lost replies recover the known receipt before retrying identical input; corrections use a new request. The final version-1 response/result envelope confirms the retained bytes. Receipt acknowledgment, terminal text and process completion are separate facts.

`analysis.boundaries` is an optional version-1 typed detail of the immutable remedy: `{ "allowed_paths": ["src/search.go", "src/search_test.go"], "excluded_paths": ["src/ranking.go"] }`. Include the files you expect the fix to change in new review-ready analysis; they guide the fixer and review and do not limit it. These fields are covered by the analysis digest and the same exact owner approval. They never grant tools or authority by themselves.

A blocked result example (replace invocation and evidence with the bound facts):

```json
{"outcome":"blocked","comment":"Investigation cannot proceed.","reason":"Reproduction evidence is unavailable.","payload":{"bug":{"response":"blocked","invocation":"factory-bound-invocation","base_revision":0,"evidence":["Owner supplied no reproducible input."]}}}
```
