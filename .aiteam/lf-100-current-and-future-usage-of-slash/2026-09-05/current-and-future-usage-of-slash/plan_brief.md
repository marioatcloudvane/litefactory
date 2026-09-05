# Plan Brief — current-and-future-usage-of-slash
# Branch: lf/100-current-and-future-usage-of-slash
# Date: 2026-09-05
# Parent issue: github:marioatcloudvane/litefactory#100
# Citation base: 55d0900fb64b51db2a4c230d43d1c783ef9687e9
# Research brief: [RESEARCH_BRIEF.md](RESEARCH_BRIEF.md)

## Shared Decisions

- `/schedule` is a Litefactory-only control command. Factory code owns parsing, validation, durable state, eligibility, capacity, dispatch, and immediate responses. Coding agents receive only a stored work payload after it is due.
- Supported direct forms are `/schedule -by HH:MM:SS <payload>`, `/schedule -to MM/DD/YYYY HH:MM:SS [ZONE] <payload>`, and `/schedule --after <id[,id...]> -by HH:MM:SS <payload>`. Omitted zone means the daemon process's local timezone; trailing `UTC` or valid IANA zone overrides it. Persist the resolved UTC instant only.
- `--after` waits for every listed schedule to succeed, then begins its delay at the latest prerequisite completion. Failed/cancelled prerequisites block dependent work and generate an immediate explanation.
- Every accepted scheduling control is stored and replied to before its source comment is consumed; it must never block later comments on the same issue.
- `scheduled=<count>` is the durable count of pending scheduled work, including clock and prerequisite waits, excluding running and terminal work.
- Proposals are advisory structured agent output. `/schedule --propose` creates a durable, ID-bearing proposal; only `/schedule --accept <proposal-id>` by the originating owner on the originating issue can atomically arm it. `--reject` closes a proposal. Proposals expire after seven days.

## Workflow

- Shipping order: T-100.1, then T-100.2, then T-100.3 and T-100.4, then T-100.5, with T-100.6 spanning and completing after all functional stories. The parser and pure grammar establish the boundary; persistence enables safe direct controls and dispatcher work; proposals build on both.
- Hard coupling: T-100.3 requires T-100.1/T-100.2; T-100.4 requires T-100.2 and the direct-control lifecycle; T-100.5 requires the schedule data model, grammar, reporting conventions, and due dispatcher; T-100.6 verifies every completed seam.

## Cross-Story Constraints

- Platform: all SQL and migrations remain in `internal/store`; the one SQLite connection remains; `internal/daemon` alone owns clock, tick, capacity, and dispatch. The new pure `internal/schedule` leaf may not open a DB, post, execute an agent, or read the clock.
- Security: authorization occurs before control handling; validate all control input and proposal output; render replies from structured safe data; preserve redaction and post-before-consume behavior.
- Tests: injected clocks and deterministic ticks only; no timer goroutines or sleeps. Cover real SQLite migrations/transactions, restarts, replay, concurrency, queue progression, and agent payload isolation.
- Ring-fenced: no exact-second wakeup service, recurring/calendar/cron syntax, natural-language times, configurable application timezone, agent-owned scheduling, or factory validation of implementation story IDs.

## Story Index

| ID | Title | Size | Depends On | Sub-issue |
|----|-------|------|-----------|-----------|
| T-100.1 | Command boundary and schedule grammar | M | — | Pending Litefactory sub-issue creation |
| T-100.2 | Durable schedules, prerequisites, and proposals | L | T-100.1 | Pending Litefactory sub-issue creation |
| T-100.3 | Direct scheduling controls and confirmations | L | T-100.1, T-100.2 | Pending Litefactory sub-issue creation |
| T-100.4 | Due dispatch, dependency outcomes, and logging | L | T-100.2, T-100.3 | Pending Litefactory sub-issue creation |
| T-100.5 | Agent schedule proposals and ID-based decisions | L | T-100.1, T-100.2, T-100.3, T-100.4 | Pending Litefactory sub-issue creation |
| T-100.6 | End-to-end deterministic verification | M | T-100.1–T-100.5 | Pending Litefactory sub-issue creation |

## Out of Scope

- Exact wall-clock wakeups outside normal polling.
- Recurring schedules, external schedulers, calendar integrations, or natural-language time parsing.
- Agents parsing, accepting, persisting, or dispatching schedules.
- Litefactory-level story-ID or plan-artifact validation for `/implement`.

## Provenance

- Citation base: 55d0900fb64b51db2a4c230d43d1c783ef9687e9.
- Known-stale areas: no current schedule/proposal schema, timezone config, or scheduler metric exists.
- Re-pinning requirements: re-check command/reply wording, report-marker behavior, tick capacity path, and agent-adapter capabilities before merge.
