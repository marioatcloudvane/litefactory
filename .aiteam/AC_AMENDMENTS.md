# AC Amendments

Acceptance criteria that a later piece of work changed or reversed.

A plan's AC table is a historical record and stays as it was written. When a
later issue contradicts one of its criteria, the amendment is recorded here and
the original plan gets a one-line pointer to this file under its table.

Append new entries at the bottom, newest last: the AC id, where it was first
stated, the date and issue that amended it, what it used to say, what it says
now, and why the change is safe. Six months from now the argument should be
readable here without reconstructing it.

---

## AC-2.16 — an issue body may now start a run

- **First stated in:** `.aiteam/main/2026-08-23/lf-setup/IMPLEMENTATION_PLAN.md`
  (User Story LF-2, AC-2 table)
- **Amended:** 2026-08-26, issue #6 (`first-comment-pickup`), plan
  `.aiteam/lf-6-first-comment-pickup/2026-08-26/first-comment-pickup/IMPLEMENTATION_PLAN.md`,
  User Story 3
- **Status:** reversed in part — replaced by AC-2.16a–d

### It used to say

> AC-2.16 — A task opened by someone else never auto-starts anything — only
> comments are ever acted on, never the issue body alone.

### It now says

> AC-2.16a *(replaces AC-2.16)* — An issue that is not assigned to the owner
> auto-starts nothing, however its body is worded — including a body the owner
> wrote himself.
>
> AC-2.16b — An open issue assigned to the owner, with a non-empty body and no
> comments, runs its body as the first instruction on first sight — exactly
> once, verified by ticking again.
>
> AC-2.16c — A body that is empty or whitespace, or that parses to a command
> which spawns nothing, starts nothing and posts nothing.
>
> AC-2.16d — An issue that already has at least one comment does not fire its
> body; the comments drain as they do today.

### Why the reversal is safe

The original ban was not about issue bodies as such. It was about
**authorisation**: anyone can open an issue, so a body carried no signal that
the owner wanted anything done. A comment did, because the daemon matches the
comment author's numeric id against the stored owner id (AC-2.15).

Issue #6 supplies the missing signal from a different field. The body runs only
when the issue is **assigned to the owner**, matched on the numeric GitHub user
id exactly as comment authors are (D-I) — never on a login. Assigning an issue
to yourself is the act of instruction; a stranger cannot do it on your behalf.
The gate is assignment, not authorship, so a run started from a stranger's body
is still owner-authorised, and the prompt keeps that body's `opened by X`
attribution so the model knows who wrote what it was handed.

AC-2.16a preserves the surviving half of the old rule verbatim in effect: an
unassigned issue starts nothing, no matter how its body reads, and that includes
a body the owner wrote himself. That is the half that was load-bearing.

The remaining guards are unchanged: closed issues are skipped (AC-2.22), the
watermark is planted before anything runs (AC-2.10b), the body's trigger id of
`0` under `UNIQUE (session_id, trigger_comment_id)` makes it fire once ever
(AC-2.23), and the classifier still refuses a bare body — the body path does not
go through it, because there is no comment to classify.
