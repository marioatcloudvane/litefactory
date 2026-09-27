# Research brief

## Request

Shape stage, continuing after the owner's bug-triage instruction was actioned: settle the scope for issue #351's three remaining candidates (letting a stage loop on its own, turning the graph path on by default, and owner-wait expiry) now that items 1, 2 and 4 have been drafted as separate bug issues.

## Decisions

- **d1** — Scope of the original pass: verify each of the six candidate items against current main. None was closed, so all six carried forward.
- **d2** — Triage in response to the owner's instruction: items 1, 2 and 4 describe broken behavior of code that already shipped (a value never written, no recovery path, stale instructions against a changed document) and were drafted as three separate bug issues. Items 3, 5 and 6 describe a capability the system doesn't have yet or a stated design choice, not broken behavior, and were left as candidates inside this issue rather than filed as bugs.
- **d3** — Drafted three separate issues via the issue-creation proposal tool, linked (not attached as sub-issues) to #351, matching the owner's 'separate independent issues' instruction. Proposal hash ecaa054a73fc687b49d9ef6bcf6a30aef0b3db2a91da46f0aac5c35241704a6c, 3 drafts, relationship linked. The factory presents this to the owner after this run; only the owner decides whether the issues are created.
- **sd1** — This shape covers only the three remaining candidates (loop, default activation, wait expiry); items 1, 2 and 4 are handled as separate bug issues and are out of scope here.
- **sd2** — Recommended, pending owner confirmation: split the three remaining candidates into their own issues rather than building them together under #351.
- **sd3** — The loop capability is a new stage answer alongside ask, done and approved, meaning "continue," bounded by the existing eight-dispatch-per-instance cap, and it must never publish an owner-facing comment for an internal iteration.
- **sd4** — Default activation needs an explicit, human-signed observation log per host against the documented checklist, not an agent's own claim of having run on the graph path.
- **sd5** — Recommended: owner waits get one reminder comment after a fixed number of days of silence, but keep the no-expiry guarantee and never auto-cancel a wait.
- **sd6** — The loop capability should ship after the dispatch-binding bug is fixed, since looping multiplies how often that gap gets exercised.
- **sd7** — A hard wait-expiry option is not recommended until the cancelled-dispatch recovery bug ships, since expiring a wait with no recovery path reproduces the same stranded-instance failure.
- **sd8** — Per the owner's request for a new publishing approval, re-drafted the same three bug issues into a fresh creation proposal (hash 1797f023e7fdf3ec1d1519f28b25c7bec36e2140b3984f2ce43b239e27a01553, 3 drafts, linked), superseding the earlier proposal (hash ecaa054a73fc687b49d9ef6bcf6a30aef0b3db2a91da46f0aac5c35241704a6c) which the owner indicated needed reissuing.


## Assumptions

- **a1** — 'Adoptions' (from the prior owner comment) means the merges on main since 2026-09-19: shared workflow runtime, graph Plan, Claude role naming, tool inheritance and stage chaining. Rests on the commit history. None of them touches items 1, 2, 3 or 6.
- **a2** — #347 is still stuck. I cannot check live daemon data from the repository, so this is taken from the issue text.
- **a3** — How graph Plan receives the saved research scope was not traced. Shaping should confirm it before deciding item 4's fix.
- **a4** — Whether the three non-bug items (3, 5, 6) should also become separate issues, as feature requests rather than bugs, is left for shaping or the owner to decide; the instruction addressed here was specifically about bugs.
- **sa1** — This run's own execution as a Claude graph node is treated as ordinary shaping work, not as the authorized observation the default-activation gate requires; that gate needs a deliberate, separate verification pass.
- **sa2** — Only the shaping-stage review-gate wait is meant by 'owner wait' here; any other durable wait is assumed to share the same no-expiry design and the same eventual fix.


## Open questions

### q1 — Should the three remaining candidates split into separate issues, like the three bugs just did?

_Asked of: Product Owner Questions_

- **a** — Split each into its own issue now, same treatment as the bugs.  *(recommended)*
- **b** — Keep all three combined under #351 and build them together.
- **c** — Keep them under #351 but tackle one at a time, starting with the loop capability.

### q2 — Does a graph-path run like this one count toward the proof needed before default activation, or is a separate explicit verification still required?

_Asked of: Product Owner Questions_

- **a** — A separate, explicitly authorized verification pass on both hosts is still required; this run does not count on its own.  *(recommended)*
- **b** — This run counts as the Claude-side proof; only the Codex-side verification remains outstanding.

### q3 — What should happen when an owner review sits unanswered for a long time?

_Asked of: Product Owner Questions_

- **a** — Nothing — keep the current no-expiry design as-is; close this candidate with no change.
- **b** — Send one reminder comment after 7 days of silence, but never auto-cancel the wait.  *(recommended)*
- **c** — Auto-cancel the wait after 14 days of silence — hold this until the cancelled-dispatch recovery bug ships, so cancelling does not strand the instance again.

### q4 — Should the loop capability wait until the dispatch-binding bug is fixed before it gets built?

_Asked of: Technical Questions_

- **a** — Yes, sequence the loop capability after the dispatch-binding fix ships.  *(recommended)*
- **b** — No, build the loop capability independently now.
