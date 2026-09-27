# Research brief

## Request

Ease the Bug workflow (issue #412): let the fixer run the project's own checks itself, inside its own sandbox, instead of relying on a separate factory-run check cycle; and once the fixer's repair-attempt budget is spent, have the workflow ask the factory/owner for guidance instead of just posting a one-way failure notice. This sits inside the larger four-point simplification already recorded on #412 (a much smaller data store, evidence shown instead of hard gates, keeping the two owner approvals, and keeping the independent factory checks, the repair budget and the existing graph diagram).

## Decisions

- **d1** — Read 'vack' in the trigger comment as a typo for 'back' -- the ask is for the workflow to feed the outcome back to the factory/owner and request guidance, not a separate unnamed action.
- **d2** — Treat the comment's two asks as sharpening two of the four targets already recorded on the underlying ticket, not as replacing or narrowing them; the scope is drafted against the full four-point target.
- **d3** — Because the three-attempt budget already exists, scope the 'try 3 times' request as confirming current behavior, not building it; the actionable gap is changing what happens after the budget is spent, from a silent notice to an interactive ask.
- **d4** — Read the owner's 'ask for advice' instruction as requiring a real human decision point when attempts run out, never an automatic extra try and never an automatic shutdown.
- **d5** — Any extra attempt granted after exhaustion must be tied to the exact request it answers, the same way today's two approvals are bound to an exact analysis or an exact delivery review, so an old answer can never be replayed for a different bug or a later exhaustion.
- **d6** — A bug stopped this way (the person chooses not to continue) is recorded as its own distinct outcome, separate from a normal close, so history never implies a fix that never shipped.
- **d7** — The owner explicitly rejected deferring the database rebuild and the evidence-instead-of-gates review to a later follow-up; both are in scope for this build.
- **d8** — Adopt the underlying ticket's own test for which hard-stop rules survive verbatim as the in-scope rule: a refusal stays only if removing it would let something merge, close or spend budget without one of the two owner approvals already covering it; every other refusal becomes a visible fact in review.
- **d9** — Because the merge/close retry bookkeeping is live, used behavior rather than dead code, the consolidation folds it into the relevant step's row as extra columns rather than deleting it outright.
- **d10** — Owner settled Q2 as unlimited grants: every time the budget runs dry, a person can grant one more attempt, always after seeing the full history of what was tried, with no cap on how many times this can happen.
- **d11** — Owner settled Q4 as a clean cutover: drop the 33 old tables outright in the same change that introduces the roughly 6 new ones, with no migration logic and no archived history, because no bug task is in flight today.
- **d12** — Drop the exact-byte replay cache for posted reports (guards against a crash-and-retry ever rewording a report) as part of the simplification; a retried report after a crash may be reworded but will state the same facts. Named explicitly per the owner's request for a full list of what changes.
- **d13** — Reshape rather than lose the per-question answer log, the analysis review-change log and the test-resolution log: their content moves into the shared analysis record and the unified event log instead of staying in their own narrowly-typed tables; nothing they recorded stops being recorded.


## Assumptions

- **a1** — The fixer is allowed to self-run only the faster two of the three check phases; the slowest, most thorough phase stays exclusively the factory's own final check, so the final gate keeps meaning something and self-checking stays fast enough to iterate on.
- **a2** — The fixer gets a small fixed number of self-check runs per attempt (a handful, not unlimited), so a stuck fixer cannot loop indefinitely; the exact number is an implementation choice for the next stage.
- **a3** — A self-check never blocks the fixer from finishing its attempt -- if the sandbox is unavailable, the fixer reports its work the same way it does today, just without the extra self-check evidence.
- **a4** — Self-check results now appear in the delivery review as evidence, consistent with the evidence-instead-of-gates goal that is in scope for this build; this replaces the earlier default of keeping them private to the fixer's own process.
- **a5** — The self-check must run the identical pinned commands, in the identical sandbox, that the factory's own final check would run, so a pass during self-check reliably predicts a pass in the real gate.
- **a6** — Assume no Bug task is actually running at the exact moment the table-dropping migration deploys, matching the owner's stated current reality; because Bug runs are short and infrequent, a quick check immediately before shipping is enough to keep this safe without building any in-flight handling.
