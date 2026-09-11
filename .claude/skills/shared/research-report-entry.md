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
4. **Links the brief.** Every run. The link is what makes the plain register
   affordable.

## Sections, in order

1. **Link to the brief.**
2. **Problems and risks** — named, never implied. No empty case: if the run found
   none, write "no problems found". Silence is not a finding.
3. **What was found** — the first analysis, in plain language.
4. **What to work on next** — below.

## What to work on next

The report ends by offering the questions worth pursuing, so the owner's reply is
enough to start the next run.

- **Two or three, ranked.** Drawn from the full inventory; a list of everything
  is not an offer.
- Each says **what it unblocks** and **who settles it** — owner, specialist, or
  next mode.
- **Invite a choice explicitly.** The owner names one on the thread.
- Where the full inventory is in the same comment, it goes after this shortlist.

**A shortlist is an offer, not a plan.** Do not start on any of it. Only a human
comment opens the next piece of work.

### The standing line

Every comment offering next work carries a line inviting the owner to reply
*"move to the next phase."* — every such message, not only the first.

That sentence invites the **owner** to open the next mode. It is not permission
for you to open one, and never licenses acting as though the next mode were open.

## Ordering, not budgeting

The final message is capped at 32 KiB (`maxResultBytes`,
`internal/report/run.go:25`) and cut **from the end** (`truncateEnd`,
`internal/report/run.go:713`). The tail is where links and question lists sit, so
order for the cut: link and problems first.

**This is a warning about order, not a size limit — do not turn it into one.**
There is no byte or line limit on the report or the brief. Do not budget against
32 KiB, count characters, or drop a finding to stay under a number.
