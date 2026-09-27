# Research brief

## Request

Ease the Bug workflow (issue #412): let the fixer run the project's own checks itself, inside its own sandbox, instead of relying on a separate factory-run check cycle; and once the fixer's repair-attempt budget is spent, have the workflow ask the factory/owner for guidance instead of just posting a one-way failure notice. This sits inside the larger four-point simplification already recorded on #412 (a much smaller data store, evidence shown instead of hard gates, keeping the two owner approvals, and keeping the independent factory checks, the repair budget and the existing graph diagram).

## Decisions

- **d1** — Read 'vack' in the trigger comment as a typo for 'back' -- the ask is for the workflow to feed the outcome back to the factory/owner and request guidance, not a separate unnamed action.
- **d2** — Treat the comment's two asks as sharpening two of the four targets already recorded on the underlying ticket, not as replacing or narrowing them; the next step should design against the full four-point target, not just the two items restated in the comment.
- **d3** — Because the three-attempt budget already exists, scope the 'try 3 times' request as confirming current behavior, not building it; the actionable gap is changing what happens after the budget is spent, from a silent notice to an interactive ask.


## Assumptions

_Nothing is being assumed._
