# Research brief

## Request

Owner instruction on a completed research pass: "these sound like bugs rather than additions. we should file them as bugs, separate independent issues!" — referring to the six candidate items already established in this issue's research findings.

## Decisions

- **d1** — Scope of the original pass: verify each of the six candidate items against current main. None was closed, so all six carried forward.
- **d2** — Triage in response to the owner's instruction: items 1, 2 and 4 describe broken behavior of code that already shipped (a value never written, no recovery path, stale instructions against a changed document) and were drafted as three separate bug issues. Items 3, 5 and 6 describe a capability the system doesn't have yet or a stated design choice, not broken behavior, and were left as candidates inside this issue rather than filed as bugs.
- **d3** — Drafted three separate issues via the issue-creation proposal tool, linked (not attached as sub-issues) to #351, matching the owner's 'separate independent issues' instruction. Proposal hash ecaa054a73fc687b49d9ef6bcf6a30aef0b3db2a91da46f0aac5c35241704a6c, 3 drafts, relationship linked. The factory presents this to the owner after this run; only the owner decides whether the issues are created.


## Assumptions

- **a1** — 'Adoptions' (from the prior owner comment) means the merges on main since 2026-09-19: shared workflow runtime, graph Plan, Claude role naming, tool inheritance and stage chaining. Rests on the commit history. None of them touches items 1, 2, 3 or 6.
- **a2** — #347 is still stuck. I cannot check live daemon data from the repository, so this is taken from the issue text.
- **a3** — How graph Plan receives the saved research scope was not traced. Shaping should confirm it before deciding item 4's fix.
- **a4** — Whether the three non-bug items (3, 5, 6) should also become separate issues, as feature requests rather than bugs, is left for shaping or the owner to decide; the instruction addressed here was specifically about bugs.
