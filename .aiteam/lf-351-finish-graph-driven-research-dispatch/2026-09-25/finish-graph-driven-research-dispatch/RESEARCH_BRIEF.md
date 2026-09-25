# Research brief

## Request

Check which of the six candidate items in issue #351 are still open after the many changes merged since 2026-09-19.

## Decisions

- **d1** — Scope of this pass: verify each of the six candidate items against current main. None is closed, so all six carry forward to shaping.


## Assumptions

- **a1** — 'Adoptions' means the merges on main since 2026-09-19: shared workflow runtime, graph Plan, Claude role naming, tool inheritance and stage chaining. Rests on the commit history. None of them touches items 1, 2, 3 or 6.
- **a2** — #347 is still stuck. I cannot check live daemon data from the repository, so this is taken from the issue text.
- **a3** — How graph Plan receives the saved research scope was not traced. Shaping should confirm it before deciding item 4.
