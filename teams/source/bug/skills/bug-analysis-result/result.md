# Version-1 Bug analyst results

Use only communication_preflight, communication_submit and communication_receipt from the factory result service. The service binds the task, empty effective mode, workflow bug, role bug-analyst, invocation and base revision. Preserve those bindings and all stable IDs.

The result contains outcome, comment, reason when incomplete/blocked, common questions when asking, and payload.bug. Bug carries response, invocation, base_revision, optional analysis, optional approval and optional evidence. No writable execution state exists.

Analysis contains expected, actual, reproduction (steps, environment, observation: observed/owner_reported/not_observed), cause, evidence, remedy, exclusions, criteria (id, requirement, measurement), decisions, assumptions, questions (id, text, material), and answer_references. Use empty arrays rather than null. Criteria IDs and question meanings remain stable across revisions. Review requires complete evidenced cause/remedy/criteria and no unresolved material questions. If execution is unavailable, reproduction may explicitly be not_observed with concrete supporting evidence instead of steps; never claim observed reproduction without steps. Answer references must name retained authenticated replies as reply:<numeric comment ID>.

ask pairs with incomplete and includes matching common/analysis questions and partial analysis. done pairs with completed and supplies complete reviewable analysis. blocked pairs with blocked, a nonblank obstacle and evidence; it may retain partial analysis. In the simplified graph, analysts cannot return approved: only exact factory-authenticated owner consumption creates approval, without an analyst invocation.

Preflight complete input; correct every diagnostic. Submit, retain request_id, and read its matching receipt. On lost replies recover the known receipt before retrying identical input; corrections use a new request. Terminal text has no authority over the result or routing. End with a short plain-language note. Receipt acknowledgment, terminal text and process completion are separate facts.

`analysis.boundaries` is an optional version-1 typed detail of the immutable remedy: `{ "allowed_paths": ["src/search.go", "src/search_test.go"], "excluded_paths": ["src/ranking.go"] }`. Include the files you expect the fix to change in new review-ready analysis; they guide the fixer and review and do not limit it. These fields are covered by the analysis digest and the same exact owner approval. They never grant tools or authority by themselves.

A blocked result example (replace invocation and evidence with the bound facts):

```json
{"outcome":"blocked","comment":"Investigation cannot proceed.","reason":"Reproduction evidence is unavailable.","payload":{"bug":{"response":"blocked","invocation":"factory-bound-invocation","base_revision":0,"evidence":["Owner supplied no reproducible input."]}}}
```
