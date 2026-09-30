# Research brief

## Request

Make daemon logs say what happened and why: outcome-per-tick line, pause/resume as state changes, per-comment decision lines, request accounting demoted, human field names, richer daemon status.

## Decisions

- **D-1** — Decided by me: pause is one WARN with reason and expected resume, resume is one INFO with duration, no per-tick repeat.
- **D-2** — Decided by me: one INFO line per owner comment, logged once, naming issue and outcome.
- **D-3** — Decided by me: a deferred tick still records its attempt time, separate from the completed-tick time the cursor logic relies on.
- **D-4** — Decided by me: the current tick-complete field names stay; renaming applies to the request-accounting fields.
- **D-20** — Owner answered Q1 a: idle ticks at DEBUG, INFO health line every 10 minutes, ticks with news at INFO.
- **D-21** — Owner answered Q2 a: per-endpoint accounting at DEBUG, compact request summary on the 10-minute health line.


## Assumptions

- **A-1** — The issue's acceptance criteria are the success bar. Tests that assert on the removed request-summary line will be updated; the tick-complete test is kept.
- **A-2** — Runs in flight can be derived from existing run records without new storage.
- **A-3** — If the daemon is not running, status shows the last stored facts labelled with their age.


## Open questions

### q3 — Where should the daemon store the facts that `daemon status` will show?

_Asked of: Technical Questions_

- **a** — In the database, beside the last-tick time it already reads. The daemon saves last attempt, paused flag with reason and resume time. In-flight runs come from the run records already in the database. Status keeps its current shape and works when the daemon is down.  *(recommended)*
- **b** — In a small state file the daemon rewrites each tick. Easy to read, but it creates a second place for state that can disagree with the database.
- **c** — Ask the running daemon over a socket. Freshest, but status fails exactly when the daemon is down.
