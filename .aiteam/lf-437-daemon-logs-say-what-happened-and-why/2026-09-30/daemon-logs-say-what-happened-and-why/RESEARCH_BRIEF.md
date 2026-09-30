# Research brief

## Request

Make daemon logs say what happened and why: outcome-per-tick line, pause/resume as state changes, per-comment decision lines, request accounting demoted, human field names, richer daemon status.

## Decisions

- **D-1** — Decided by me: pause is one WARN with reason and expected resume, resume is one INFO with duration, no per-tick repeat.
- **D-2** — Decided by me: one INFO line per owner comment, logged once, naming issue and outcome.
- **D-3** — Decided by me: a deferred tick still records its attempt time, separate from the completed-tick time the cursor logic relies on.
- **D-4** — Decided by me: the current tick-complete field names stay; renaming applies to the request-accounting fields.


## Assumptions

- **A-1** — The issue's acceptance criteria are the success bar. Tests that assert on the removed request-summary line will be updated; the tick-complete test is kept.
- **A-2** — Runs in flight can be derived from existing run records without new storage.
- **A-3** — If the daemon is not running, status shows the last stored facts labelled with their age.


## Open questions

### q1 — How loud should a tick be when nothing new happened?

_Asked of: Product Owner Questions_

- **a** — Quiet: log idle ticks at DEBUG only, and print one INFO health line every 10 minutes (healthy, nothing new, last attempt time). Ticks with news log at INFO.  *(recommended)*
- **b** — Every tick logs one INFO line, even 'tick: nothing new'. Complete, but two or three lines a minute again.
- **c** — Silent when idle. No heartbeat. Smallest log, but you cannot tell a healthy quiet daemon from a dead one.

### q2 — What happens to the request accounting?

_Asked of: Product Owner Questions_

- **a** — Full per-endpoint detail moves to DEBUG. Every 10 minutes the health line adds one compact line: requests, ok, not-modified, errors, quota left and reset time.  *(recommended)*
- **b** — Move everything to DEBUG. Nothing at INFO.
- **c** — Keep it at INFO but only every 10 minutes, with renamed fields.

### q3 — Where does the daemon keep the facts that `daemon status` shows?

_Asked of: Technical Questions_

- **a** — In the database, next to the last-tick time: last attempt time, paused flag with reason and resume time. Runs in flight are read from the existing runs records. Status keeps working the way it does now.  *(recommended)*
- **b** — In a small state file the daemon rewrites each tick. Simpler to read, but a second place for state and can disagree with the database.
- **c** — Ask the running daemon over a socket. Freshest, but status then fails when the daemon is down, which is when you need it.
