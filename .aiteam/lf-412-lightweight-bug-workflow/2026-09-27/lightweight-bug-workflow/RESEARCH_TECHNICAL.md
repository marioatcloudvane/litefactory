# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

The fixer's own instructions explicitly forbid it from executing any command or choosing a check recipe, so it cannot run tests itself today.

Evidence:

- `internal/teamsource/bug/bug-fixer.md:33`

## f2

All testing happens afterward in a separate child workflow the factory runs on its own, in three sequential phases (regression, focused, full) executed through a sandboxed command runner, not by the fixer.

Evidence:

- `internal/teamsource/bug/bug_tests.dot:9`
- `internal/bugchecks/executor.go:1`

## f3

The repair budget is already fixed at three lifetime reservations (one initial fix plus two repairs), enforced in the data layer, matching what the owner asked for.

Evidence:

- `internal/store/bug_repairs.go:13`
- `internal/teamsource/bug/bug-fixer.md:21`

## f4

When the repair budget is exhausted (or any other step fails), the workflow's only response is a one-way notice or failure comment that ends the task; it does not open an interactive question the owner or factory can answer to steer the outcome, unlike the workflow's two existing approval points.

Evidence:

- `internal/turn/bug_parent_reports.go:56`
- `internal/teamsource/bug/bug.dot:47`

## f5

The bug-specific part of the database schema has 33 dedicated tables out of roughly 149 tables total, closely matching the underlying ticket's '33 of 146' figure.

Evidence:

- `internal/store/migrations/0066_bug_analysis.sql:1`
- `internal/store/migrations/0071_bug_checks.sql:1`

## f6

Write code exists for every table the underlying ticket lists as never having held a row; the code paths are reachable, so whether they stay empty in production is a runtime fact the ticket asserts rather than dead code left unwritten.

Evidence:

- `internal/store/bug_records.go:1`
- `internal/store/bug_execution_sections.go:1`

## f7

A small evidence model already exists for the fixer to report which approved requirement each change and test covers, which is a building block a lighter, evidence-based workflow can extend rather than invent from scratch.

Evidence:

- `internal/bugrepair/evidence.go:11`

## f8

The five specific defects the underlying ticket lists as already fixed are all present in the repository's history, so the current workflow already includes those corrections.

Evidence:

- `internal/teamsource/bug/bug.dot:1`
