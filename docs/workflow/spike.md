# Spike mode

Read this when a request has been classified as **Spike**. Nothing else needs it.

Spike is the fast lane: quick research, or a small change to code, time-boxed.
No orchestrator, no prerequisite. The top-level router handles it directly.

You still need a session directory, because a spike writes a file into one — ask
for the feature slug and derive `$session` exactly as `docs/workflow/session.md`
describes.

## The output

Every spike writes `$session/SPIKE.md` — a note someone can read a week later,
not two sentences:

```markdown
# Spike — <feature>

<date>. What was asked, in a sentence or two.

## What was done
## What was found
## Decisions worth remembering
## Not done
## Recommendation
```

Write it even when the answer is short, and write it even when the answer is
"no". An unrecorded spike is a question that gets asked again in three weeks.

Write it when the spike is blocked, too. Preserve the evidence gathered so far
in the relevant sections, describe the blocker under `## Not done`, and put the
decision needed from the user together with the viable options under
`## Recommendation`. A blocker does not turn a headless run into a live
conversation: finish the artifact and hand the decision back through
litefactory.

## Hand back rather than grow

Spike covers fast research and small changes. The moment the work turns out to
need more — a major change, a new package or dependency, a schema or API change,
a refactor spanning several files, anything you would want reviewed as a design
before it is written — stop there. Write `SPIKE.md` with what you learned, say
plainly that this exceeds a spike, and recommend Research or Plan. Do not start
the larger change.

A short `SPIKE.md` that ends in "this needs Research" is a successful spike, not
a failed one. It is the cheapest possible answer to a question that turned out to
be expensive.

## Staying cheap

A spike that reads forty files is not a spike. The time box is the point.

- **Research directly.** Read relevant code and authoritative web sources. If
  the investigation needs a broad sweep, recommend Research.
- **Do not read `.aiteam/` archives.** Other sessions are out of bounds — see the
  boundary rule in `docs/workflow/session.md`. "Has anyone looked at this before?"
  is not a reason to go looking.
- **Measure before you conclude.** A spike that reports a number someone can
  check is worth more than one that reports an impression.

## When you are done

Report what you produced and recommend a next mode. **Never enter one.** That is
*One Run, One Mode*, and it applies to Spike exactly as it applies to the others.
If the spike is blocked on a user decision, repeat the decision needed and its
viable options concisely in the final report so litefactory can surface them on
the issue. Never wait, poll for an answer, or enter another mode in the same run.
