# Research report — entry point

Read this at the end of a Research run, when you are about to write the run's
final message. It governs the **entry report**: the first thing a business
reader sees, and the thing litefactory posts as the comment on the issue.

The full inventory of unresolved questions is a separate procedure —
`.claude/skills/shared/research-open-questions.md`. This file covers the entry
report and how it hands off to that list.

## Two audiences, two registers, one run

| Artifact | Audience | Register |
|---|---|---|
| `$session/RESEARCH_BRIEF.md` | **Technical readers** — Plan mode, engineers, the architect | Full technical depth, tagged and cited |
| The run's final message | **Business analysts and business users** | Plain language, no code |

These are not two drafts of one text. They are the same findings written for two
different people. Do not paste the brief into the comment, and do not thin the
brief to match the comment. The report is an **addition, not a replacement** —
nothing is taken out of the brief to make room for it.

## Where the report goes

**The report is the run's final message.** Not a section in the middle of the
run, not a heading inside the brief, not a preamble you write before carrying on
with more work. The reason is mechanical: litefactory takes the run's **last**
message and posts *that* as the comment on the issue. A report written anywhere
else is a report nobody reads. **Nothing follows it** — once you have written
it, the run is over.

**And you still write `$session/RESEARCH_BRIEF.md`, on every run.** Delivering
the report as the final message takes nothing away from the brief: the file is
written, in full, whether or not this run changed much in it. A run that ends
with a good report and no brief on disk has failed — the report links the brief,
and the link has to resolve.

## The four hard rules

A report that breaks one of these is wrong even when every sentence in it is true.

1. **Plain language.** No `path:line` citations. No provenance tags (`[V]`,
   `[O]`, `[W]`, `[R0]`). No Go identifiers — no package, type, function, method,
   field or constant names, and no file paths. Where a thing has to be named, name
   it by what it *does*. A business reader must never have to open the code to
   follow a sentence.

2. **Honest — it does not swallow technical complexity to sound tidy.** If
   something is hard, say it is hard **and why**, in business terms: what it costs,
   what it risks, what it blocks, what is still unknown. A report that reads
   smoothly because the awkward parts were left out is a false report, not a clear
   one. Simplifying the *language* is required; simplifying the *picture* is not
   allowed.

3. **It names the problems found.** As its own section, every time. **A report
   that reports only progress is a failing report**, however accurate the progress
   is.

4. **It always links the detailed artifact.** This is what makes the
   plain-language register affordable — nothing is lost by leaving detail out of
   the comment, because the detail is one click away. Link the brief on every run,
   not only when the findings look interesting.

## Required sections

The entry report carries each of these as its own explicit section:

- **The link to the full brief.** Early — see *Ordering* below.
- **What was found** — in plain language. This is the first analysis: what the
  run actually established, stated so a business reader can act on it.
- **Problems and risks** — named, not implied, and never omitted. If the run
  genuinely found no problems, say so in as many words: *"no problems found"* is a
  finding; silence is not. This section is mandatory and has no empty case.
- **What to work on next** — see below. This is what turns a report into a
  decision the owner can make on the thread.

## What to work on next

The entry report does not end at "here is what I found". It ends by **offering
the open questions that are worth working on next**, so that the owner's reply to
the posted comment is enough to start the following run.

- Draw the shortlist from the full inventory — the procedure in
  `.claude/skills/shared/research-open-questions.md` is what produces it. The
  entry report carries the ones worth acting on; the inventory carries all of them.
- Keep the shortlist **short and ranked** — the two or three that would change
  what gets built, most consequential first. A list of everything is not an offer.
- For each one, say in plain language **what it unblocks** and **who can settle
  it** — the owner, a specialist, or the next mode.
- **Invite a choice explicitly.** The owner replies on the thread naming what to
  pursue, and the next run continues the same conversation.
- Where the full inventory belongs in the same comment, it goes **after** this
  shortlist, never before it.

**The shortlist is an offer, not a plan.** Do not start on any of it, and do not
write as though the choice has been made. Only a human comment opens the next
piece of work.

### The standing "move to the next phase" line

**Every comment that offers next work carries a standing line inviting the owner
to reply *"move to the next phase."*** Every such message, not only the first —
the escape hatch has to be in front of the owner at the moment they want it.

Be clear about what that sentence is. It is the *One Run, One Mode* handoff: **an
invitation to the owner to open the next mode with a human comment.** It is not
permission for you to open one. Saying it does not license you to continue into
Plan mode, and it never lets you act as though the next mode had already been
opened. Offer the sentence; you do not act on it yourself.

## Ordering — a warning, not a budget

**Put the link and the problems near the top.**

The reason is mechanical. The run's final message is capped at 32 KiB
(`maxResultBytes`, `internal/report/run.go:25`) and the cut is taken **from the
end** (`truncateEnd`, called at `internal/report/run.go:713`). A long report
therefore loses its **tail** — and the tail is exactly where a link and a list of
questions tend to sit. Ordering is what decides which parts survive a cut, so
order for the cut.

**This is a warning about ordering. It is not a size limit and must not be turned
into one.** There is deliberately **no byte limit and no line limit** on the
report, and none on the brief. Do not add one. Do not budget against 32 KiB, do
not count characters, and do not drop a finding to stay under a number. Write
what needs saying — and put the link and the problems where a cut cannot reach
them.

## What the brief keeps

`RESEARCH_BRIEF.md` is unchanged by all of the above. It keeps its full technical
detail, its provenance tagging, its citations and its section depth. The report is
an **addition to** the brief, never a replacement for it.
