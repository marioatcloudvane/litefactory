# Research report — entry point

Read when writing the run's final message. The full inventory of unresolved
questions is separate: `.claude/skills/shared/research-open-questions.md`.

## Two registers

| | Audience | Register |
|---|---|---|
| `$session/RESEARCH_BRIEF.md` | Plan mode, engineers, the architect | full technical depth, tagged and cited |
| the run's final message | business analysts and business users | plain language, no code |

Same findings, two readers. Not two drafts of one text: never paste the brief
into the comment, never thin the brief to match it. The brief is written in full
on every run — a run ending with a good report and no brief on disk has failed.

## Placement

The report is the run's **last message**. litefactory posts the last message as
the issue comment, so a report written anywhere else is a report nobody reads.
Nothing follows it.

litefactory's header above it already links the brief, pinned to this run's
commit. Your own view of the brief is a worktree path — dead text in a comment —
so the link is the factory's job.

## The four hard rules

1. **Plain language.** No `path:line`, no provenance tags, no Go identifiers, no
   file paths. Name a thing by what it does. A business reader never opens code
   to follow a sentence.
2. **Honest.** If something is hard, say it is hard and why, in business terms:
   what it costs, risks, blocks, or leaves unknown. Simplify the language, never
   the picture. A report that reads smoothly because the awkward parts were cut
   is false, not clear.
3. **Names the problems.** Own section, every run. A report that carries only
   progress is failing, however accurate.
4. **Never links or names the brief.** No link, no path, no "full research
   brief" opening line. The header did it; a second reference duplicates or
   contradicts it.

## How you build the response: always in exactly this structure!

Open with section 1 — no preamble, no title, no "run completed", no note on what
was written. Then, in exactly this order:

1. **What was found** — the first analysis, in plain language.
2. **Problems and risks** — named, never implied. No empty case: if the run found
   none, write "no problems found". Silence is not a finding.
3. **What to work on next** — required on every run; a report ending on findings
   leaves the owner no move. Say **how many** open questions there are and ask
   the user to work on them. **Do not list them** — the next cycle is for
   thinking about them, and that is where they get shown in full. If none are
   open, say so and still name the next move.

### The standing line

Every comment offering next work carries a line inviting the owner to reply
*"move to the next phase."* — every such message, not only the first.

That sentence invites the **owner** to open the next mode. It is not permission
for you to open one, and never licenses acting as though the next mode were open.

## Ordering, not budgeting

The final message is capped at 32 KiB (`maxResultBytes`,
`internal/report/run.go:25`) and cut **from the end** (`truncateEnd`,
`internal/report/run.go:713`). The tail is where question lists sit, so order for
the cut — which is why the problems come before anything optional.

**This is a warning about order, not a size limit — do not turn it into one.**
There is no byte or line limit on the report or the brief. Do not budget against
32 KiB, count characters, or drop a finding to stay under a number.
