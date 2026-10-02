# Bug repair result

Use communication_preflight, communication_submit and communication_receipt. The factory binds workflow bug with no mode, role bug-fixer, invocation, base_revision, reservation, approved analysis digest and original source commit/tree.

Set payload.bug.response to done, reshape or blocked. Send payload.bug.repair with reservation, analysis_digest, source_commit, source_tree, summary and coverage. Each coverage entry has criterion, changes and tests arrays of contained relative paths, plus optional gap. Done requires every approved criterion exactly once, changes and tests for each, and no gaps. Unknown or duplicate criteria and escaping paths are refused. Partial blocked/reshape work preserves references and gaps; no further edit is authorized by that result.

Do not submit analysis, approval, budgets, recipes or delivery fields. Done is authored work, not executed passing checks. Shared result input alone cannot create the downstream testing handoff: successful invocation, graph consistency, factory scope observations and immutable source persistence remain required.

Preflight and correct diagnostics before submission; read the receipt matching request_id. A lost reply reuses request identity and identical bytes after receipt lookup. Corrected content has a new identity. The terminal version-1 envelope confirms only the already retained response and result. Pending is not success.

A blocked repair example (replace all bindings with the factory-supplied values):

```json
{"outcome":"blocked","comment":"The repair cannot proceed.","reason":"The approved source cannot be reproduced.","payload":{"bug":{"response":"blocked","invocation":"factory-bound-reservation","base_revision":1,"repair":{"reservation":"factory-bound-reservation","analysis_digest":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","source_commit":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","source_tree":"cccccccccccccccccccccccccccccccccccccccc","summary":"No product changes were made; the source is unavailable.","coverage":[{"criterion":"C1","changes":[],"tests":[],"gap":"Cannot inspect the approved source."}]}}}}
```
