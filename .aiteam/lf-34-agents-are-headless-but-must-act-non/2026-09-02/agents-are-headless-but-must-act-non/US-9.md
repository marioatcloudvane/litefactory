# US-9 — go-architect.md: remove hardcoded personal name

> As the repo owner, I want the specific person's name hardcoded into
> go-architect.md's escalation pattern replaced with a role, so that the
> agent's instructions describe who to escalate to in general terms, not one
> individual by name.

This story replaces a hardcoded personal name ("Mario") in go-architect.md's
deviation-request block with role-based phrasing.

## Why This Matters

This is a small, independent cleanup, unrelated to the ask/escalate behaviour
work elsewhere in this plan — the owner asked for it separately. The research
audit found one place in the entire `.claude/` tree where an agent
definition names a specific individual rather than describing a role: the
"decision needed from" line in go-architect.md's deviation-request block.
Hardcoding a name into an agent's instructions is brittle in an obvious way —
it stops being accurate the moment that's no longer the right person to ask —
and it doesn't belong in a file that otherwise describes roles and processes,
not individuals.

## Architecture Notes

- **Shares one code block with US-8** (the same deviation-request block).
  Land as one edit: US-8's structural extension first, then this story's
  substitution — so US-8's newly added lines are never written with a name
  and then corrected afterward.
- Replace "Decision needed from: Mario (human)" with role-based phrasing
  (e.g. "the owner" / "the human" — consistent with how the rest of the
  repo's agent files refer to the person on the other end of an escalation).
- Confirmed by the architect during plan review: a repo-wide grep for the
  name under `.claude/` returns exactly this one hit — no other file needs
  touching for this story.
- Platform: none — no Go file in scope.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-9.1 | Replace the "Decision needed from: Mario (human)" line with role-based phrasing; confirm US-8's newly added lines carry no personal name | logic | US-8 | XS |
| T-9.2 | Verify a repo-wide case-insensitive grep for the name under `.claude/` returns no match | test | T-9.1 | XS |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-1 | The "Decision needed from: Mario (human)" line is replaced with role-based phrasing naming no individual. |
| AC-2 | US-8's newly added escalation-trigger text also carries no personal name. |
| AC-3 | A repo-wide case-insensitive grep for the name under `.claude/` returns no match. |
| AC-4 | This story's edit lands together with US-8, after US-8's structural extension. |
| AC-5 | `make check` passes; `git status --short` shows only `.claude/agents/go-architect.md` changed for this story (landed together with US-8). |
