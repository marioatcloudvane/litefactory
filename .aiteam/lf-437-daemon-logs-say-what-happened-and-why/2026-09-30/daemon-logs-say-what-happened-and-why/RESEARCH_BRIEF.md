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
- **D-22** — Owner answered Q3: keep the log as a file, one file per date, so old logs can be cleaned up later.
- **D-23** — Decided by me: daily file named daemon-YYYY-MM-DD.log in local time; logs and doctor read the newest; old daemon.log is left as is; cleanup is out of scope.


## Assumptions

- **A-1** — The issue's acceptance criteria are the success bar. Tests that assert on the removed request-summary line will be updated; the tick-complete test is kept.
- **A-3** — If the daemon is not running, status shows the last logged facts labelled with their age.


## Open questions

### q4 — How should `daemon status` get last attempt, paused state and runs in flight from the log?

_Asked of: Technical Questions_

- **a** — The daemon writes one INFO status record on every change (pause, resume, run started, run finished) and every 10 minutes. Status reads the newest such record from the newest daily file and prints it with its age. Status can be up to 10 minutes behind on last attempt. Nothing else is stored.  *(recommended)*
- **b** — Same as a, but the status record is written every tick. Always fresh, but it brings back two or three INFO lines a minute, the noise this issue removes.
- **c** — Keep the log for reading and put the three facts in the database as well. Fresh and quiet, but it is the second store you did not want.
