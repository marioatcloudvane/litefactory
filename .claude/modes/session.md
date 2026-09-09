# The session directory

Read this before scaffolding a session, and when you need to know what an
artifact is or where it belongs. The router reads it once per run; agents that
already know their own artifacts do not need it at all.

## Validate exception

Validate reads `.claude/modes/validate.md` and the installed shared workflow.
It does not scaffold a session, derive a path using shell commands, write or tick
MANIFEST.md, or create review/result files. Existing context may be read when
supplied, but artifacts are not prerequisites for returning structured findings.

## Deriving the path

All artifacts live under `.aiteam/<branch>/<date>/<feature>/`.

```
branch=$(git rev-parse --abbrev-ref HEAD | tr '/' '-' | tr '[:upper:]' '[:lower:]')
date=$(date +%Y-%m-%d)
feature=<sanitised-slug>
session=".aiteam/$branch/$date/$feature"
```

- `<branch>` — current git branch, slashes → hyphens, lowercased.
- `<date>` — today, `YYYY-MM-DD`.
- `<feature>` — the slug the user gave, lowercased, spaces and special characters
  → hyphens, max 40 characters.

Example: branch `feat/payments`, date `2026-05-20`, feature `stripe-webhook` →
`.aiteam/feat-payments/2026-05-20/stripe-webhook/`.

Each feature on the same branch and day gets its own directory, so two features
running in parallel never collide.

## Scaffolding

For Research, Plan, Implement or Spike on a new piece of work, write `$session/MANIFEST.md` before invoking anything:

```markdown
# Manifest
# Branch: <branch>
# Session: <date>

## Modes

- [ ] **Research** — `RESEARCH_BRIEF.md` *(not started)*
- [ ] **Plan** — `plan_brief.md` + per-story sub-issues *(not started)*
- [ ] **Implement** — code + tests *(not started)*
- [ ] **Spike** — `SPIKE.md` *(not started)*

**Validate** returns read-only structured findings through runner output; no
manifest checkbox is written by a Validate run.

Modes do not follow on from one another. A box is ticked by a run that a human
asked for; nothing here licenses starting the next one.

## History

- <date> — session created

## Notes

```

## It is a boundary, not just a location

`$session` is the only directory under `.aiteam/` that a run reads from or writes
to. Every other session directory belongs to work that is already finished, and
is **out of bounds**.

- **Never glob across sessions.** `.aiteam/**/*.md`, `.aiteam/*/*/*/MANIFEST.md`
  and anything else matching more than one session directory is forbidden. If you
  want a file, name its full path inside `$session`.
- **Never read another session's artifacts** — its `RESEARCH_BRIEF.md`,
  `IMPLEMENTATION_PLAN.md`, `plan_brief.md`, `feature-spec.md`, `CODE_REVIEW.md`,
  `notes.md`, `DRIFT_REPORT.md` or `SPIKE.md` — no matter how closely its slug
  resembles the work in hand. A similar name is the strongest signal that it is a
  *different* piece of work that already has its own record.
- **In bounds outside `$session`:** `.aiteam/APP_CONTEXT.md` and
  `.aiteam/APP_CONSTRAINTS.md`, for the agents told to read them, plus
  `.aiteam/rulebook.md` if it exists. Nothing else at `.aiteam/` root is a read
  target — `AC_AMENDMENTS.md` is written to, not read.

**Why this is a rule and not a preference.** An archived session is a dense,
heavily cross-cited record of a run that already finished: opening one leads to
three more, and a story that needed two files ends up reading thirty. It is also
stale by construction — an archive is never rewritten when a format or a decision
changes, so what it says was true *then*. Anything still true today lives in
`APP_CONTEXT.md`, `APP_CONSTRAINTS.md`, or the code. If it is in none of those, it
is history, and history is what `git log` is for.

**"Has this been done before?" is not a reason to cross the boundary.** Ask the
user, or say in your report that you could not tell. Do not go looking.

## Plan mode's output: `plan_brief.md` + per-story sub-issues

Plan mode writes **`$session/plan_brief.md`** — one short shared-context document
holding only what two or more stories need: shared decisions, shipping order,
cross-story constraints, out-of-scope, provenance. Each user story's own tasks,
acceptance criteria and architecture notes live in **its own sub-issue** under the
parent issue, and nowhere else.

`plan_brief.md` carries a **Story Index** — one row per story with its ID, title,
size, dependencies and sub-issue link. That index is the discovery mechanism: read
the brief, then follow the index to the story you need. The index carries identity
and location only, never content.

The brief stays a file in the repo, so it is versioned, diffable and reviewable.
GitHub is the sole editable story source during Plan. On promotion, litefactory
records complete immutable approved revisions and supplies them explicitly on
cold, resumed and fallback execution. Implement requires that approved bundle,
not just a brief's existence. Agents do not fetch live issues or use local story
Markdown files as a fallback; the factory validates current relationships.

Follow the factory's persisted version and `.claude/skills/shared/workflow.md`.
Legacy stories retain immediate scoped acceptance/review/test verification,
closure and affected invalidation. Separated Implement publishes immutable
engineering/check evidence, releases dependents and leaves boxes/issues open;
Validate performs read-only acceptance/code/architecture/plan review from retained
checks. Only the factory projects decisions and closes accepted/owner-waived
stories. Historical acceptance never expires after later code/main changes.
Final aggregate engineering/save gates remain required.

**The single-file plan is retired.** Plan mode used to write one document holding
every story in full. It no longer does. Sessions archived under `.aiteam/` before
that change keep whatever artifact they were written with — **archives are never
rewritten to match a new format** — so an older session directory is a record of
what that run produced, not a description of the current shape.

## Rulebook (optional)

If `.aiteam/rulebook.md` exists, the shared scanner skills read it for
project-specific rules. If it does not, they fall back to generic best practices.
Nothing needs to be done about its absence.
