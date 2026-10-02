# Research brief

## Request

Make daemon logs say what happened and why: outcome-per-tick line, pause/resume as state changes, per-comment decision lines, request accounting demoted, human field names, richer daemon status.

## Decisions

- **D-1** — Decided by me: pause is one WARN with reason and expected resume, resume is one INFO with duration, no per-tick repeat.
- **D-2** — Decided by me: one INFO line per owner comment, logged once, naming issue and outcome.
- **D-3** — Decided by me: a deferred tick still records its attempt in the log, separate from the completed-tick time the cursor logic relies on.
- **D-4** — Decided by me: the current tick-complete field names stay; renaming applies to the request-accounting fields.
- **D-20** — Owner answered Q1 a: idle ticks at DEBUG, INFO health line every 10 minutes.
- **D-21** — Owner answered Q2 a: per-endpoint accounting at DEBUG, compact request summary on the 10-minute line.
- **D-22** — Owner answered Q3: keep the log as a file, one file per date, so old logs can be cleaned up later.
- **D-23** — Decided by me: daily file named daemon-YYYY-MM-DD.log in local time; logs and doctor read the newest; old daemon.log left as is; cleanup out of scope.
- **D-24** — Owner answered Q4: every poll logs at DEBUG whatever happens; INFO is the 10-minute summary; verbosity is chosen through the log level.
- **D-25** — Decided by me: daemon status reads last attempt, paused state and in-flight runs from the newest digest or event record in the newest daily file, shown with its age. No database change.


## Assumptions

- **A-1** — The issue's acceptance criteria are the success bar. Tests that assert on the removed request-summary line will be updated; the tick-complete test is kept.
- **A-3** — If the daemon is not running, status shows the last logged facts labelled with their age.
- **A-4** — Pause, resume and per-comment decision lines stay at INFO or above beside the 10-minute digest; the owner can move them to DEBUG.
- **A-5** — The daemon's existing configured log level is the verbosity control, so no new setting is added.
