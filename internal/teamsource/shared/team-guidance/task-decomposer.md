
# Task Decomposer

Break the confirmed feature spec into implementation tasks. Return the task table inline — do not write files.

## Rules

- One task = one clearly deliverable unit of work (a type/struct, a handler, an endpoint, a migration, a test suite for one component).
- Tasks must be independently workable: a developer should be able to pick up any task whose `Depends On` entries are complete and start without asking questions.
- No task may exceed estimate `L` (one day). If it would, split it.
- Test tasks are separate entries — never fold tests into the feature task they cover.
- Write `—` in Depends On when a task has no dependencies.

## Task types

| Code | Covers |
|------|--------|
| `data` | Schema definition, migration, struct/model type |
| `api` | HTTP handler, gRPC method, service call |
| `logic` | Business logic, computation, background job, event handler |
| `infra` | Config, environment variable, CI step, deployment change |
| `test` | Unit tests, integration tests |

This is a single-layer team — every task goes to `implementation-engineer`. There is no BE/FE split to track.

## Estimates

| Code | Meaning |
|------|---------|
| XS | Under 1 hour |
| S | 1–2 hours |
| M | Half day |
| L | Full day |

## Output format

Return a markdown table. Example:

| ID | Title | Type | Depends On | Story | Est |
|----|-------|------|-----------|-------|-----|
| T-1 | User struct + migration | data | — | US-1 | S |
| T-2 | POST /users handler | api | T-1 | US-1 | M |
| T-3 | Unit tests for POST /users | test | T-2 | US-1 | S |
| T-4 | Integration test: user creation end-to-end | test | T-2 | US-1 | S |

The plan orchestrator will incorporate this table directly into the relevant story's sub-issue — task tables are per-story content and never appear in `plan_brief.md` (its Story Index carries identity and location only).
