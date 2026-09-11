# The session directory

Read this before scaffolding a session, and when you need to know what an
artifact is or where it belongs. The router reads it once per run; agents that
already know their own artifacts do not need it at all.

**The Plan submission contract is not here.** Story fields, limits, transport and
the discussion/completed outcomes live in `.claude/agents/plan-orchestrator.md`,
which is the only role that writes them, and in
`.claude/skills/shared/workflow.md`. The router does not need them.

## Validate exception

Validate reads `.claude/modes/validate.md` and the shared workflow. It does not
scaffold a session, derive a path with shell commands, write or tick
`MANIFEST.md`, or create review/result files. Supplied context may be read, but
no artifact is a prerequisite for returning structured findings.

## Deriving the path

All artifacts live under `.aiteam/<branch>/<date>/<feature>/`.

**Reuse the factory-supplied session path first**, including the directory
holding its identity-bound `PLAN_CONTEXT.json` — that unfinished context is
enough to continue Plan without a completed brief. A date change, or a cold or
fallback attempt, never creates a new session by itself. Only a new task with no
supplied prior session derives a new path:

```
branch=$(git rev-parse --abbrev-ref HEAD | tr '/' '-' | tr '[:upper:]' '[:lower:]')
date=$(date +%Y-%m-%d)
feature=<sanitised-slug>
session=".aiteam/$branch/$date/$feature"
```

- `<branch>` — current branch, slashes → hyphens, lowercased.
- `<date>` — the date the session began; today only for a new session.
- `<feature>` — the user's slug, lowercased, spaces and specials → hyphens, max
  40 characters.

Branch `feat/payments`, date `2026-05-20`, feature `stripe-webhook` →
`.aiteam/feat-payments/2026-05-20/stripe-webhook/`. Each feature on the same
branch and day gets its own directory, so parallel features never collide.

## Scaffolding

For Research, Plan, Implement or Spike on new work, write `$session/MANIFEST.md`
before invoking anything:

```markdown
# Manifest
# Branch: <branch>
# Session: <date>

## Modes

- [ ] **Research** — `RESEARCH_BRIEF.md` *(not started)*
- [ ] **Plan** — `plan_brief.md` + per-story sub-issues *(not started)*
- [ ] **Implement** — code + tests *(not started)*
- [ ] **Validate** — verification findings *(not started)*
- [ ] **Spike** — `SPIKE.md` *(not started)*

**Validate** returns read-only structured findings through the supplied contract;
a Validate run writes no checkbox here.

Modes do not follow on from one another. A box is ticked by a run a human asked
for; nothing here licenses starting the next one.

## History

- <date> — session created

## Notes

```

## It is a boundary, not just a location

`$session` is the only directory under `.aiteam/` a run reads from or writes to.
Every other session belongs to finished work and is **out of bounds**.

- **Never glob across sessions.** `.aiteam/**/*.md`, `.aiteam/*/*/*/MANIFEST.md`
  and anything else matching more than one session directory is forbidden. Name
  the full path inside `$session` instead.
- **Never read another session's artifacts**, no matter how closely its slug
  resembles the work in hand. A similar name is the strongest signal that it is a
  *different* piece of work with its own record.
- **In bounds outside `$session`:** `.aiteam/APP_CONTEXT.md` and
  `.aiteam/APP_CONSTRAINTS.md` for the agents told to read them, plus
  `.aiteam/rulebook.md` if it exists. Nothing else at `.aiteam/` root is a read
  target — `AC_AMENDMENTS.md` is written to, not read.

**Why this is a rule, not a preference.** An archived session is a dense,
heavily cross-cited record of a finished run: opening one leads to three more,
and a story that needed two files ends up reading thirty. It is also stale by
construction — an archive is never rewritten when a format or decision changes,
so what it says was true *then*. Anything still true today is in
`APP_CONTEXT.md`, `APP_CONSTRAINTS.md` or the code. If it is in none of those it
is history, and history is what `git log` is for.

**"Has this been done before?" is not a reason to cross the boundary.** Ask the
user, or say in your report that you could not tell. Do not go looking.

## Plan selection persists

Plan stays selected across successful, failed and discussion iterations. Only an
owner command or canonical label replacement selects another mode; label removal
returns new triggers to command/classifier routing. Agent recommendations never
change it.

Plan permits material asynchronous questions unless explicitly prohibited for the
run; Research stays opt-in. Finish with one final report and never wait for an
answer. Recommend `/implement <instruction>` after completion and leave Plan
selected. Only successful factory publication, link replacement and save
establish readiness — never an agent-written proposal or an old local brief.

## Plan's output: `plan_brief.md` + per-story sub-issues

`$session/plan_brief.md` is one short shared-context document holding only what
two or more stories need: shared decisions, shipping order, cross-story
constraints, out of scope, provenance. Each story's own tasks, acceptance
criteria and architecture notes live in **its own sub-issue**, and nowhere else.

The brief carries a **Story Index** — one row per story with ID, title, size,
dependencies and link. That index is the discovery mechanism: read the brief,
follow the index to the story you need. It carries identity and location only,
never content.

The brief stays a file in the repo, so it is versioned, diffable and reviewable.
GitHub is the sole editable story source during Plan. On promotion litefactory
records complete immutable approved revisions and supplies them explicitly on
cold, resumed and fallback execution. **Implement requires that approved bundle,
not a brief's existence.** Agents never fetch live issues or fall back to a local
story Markdown file.

Legacy stories retain immediate scoped acceptance, review, test verification,
closure and affected invalidation. Separated Implement publishes immutable
engineering evidence, releases dependents and leaves boxes and issues open;
Validate does read-only acceptance, code, architecture and plan review from
retained checks. Only the factory projects decisions and closes accepted or
owner-waived stories. Historical acceptance never expires after later code or
main changes. Final aggregate engineering and save gates remain required.

**The single-file plan is retired.** Plan used to write one document holding
every story in full. Sessions archived before that change keep whatever artifact
they were written with — **archives are never rewritten to match a new format** —
so an older session directory records what that run produced, not the current
shape.

## Rulebook (optional)

If `.aiteam/rulebook.md` exists, the shared scanner skills read it for
project-specific rules; otherwise they fall back to generic practice. Its absence
needs no action.
