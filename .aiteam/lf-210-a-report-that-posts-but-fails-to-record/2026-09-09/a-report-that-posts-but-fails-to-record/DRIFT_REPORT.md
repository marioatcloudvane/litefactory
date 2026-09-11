# Engineering scope report — a-report-that-posts-but-fails-to-record

Based on implementation and test specialist reports; this is not acceptance, code, architecture or approved-plan review.

## Implemented scope

- US-001: explicit prelaunch allocation and typed readiness refusal, same-trigger retry, terminal report guard, stopped-result repair preserving ownership while releasing worker capacity, and selection/restart fault checks.
- US-002: narrow durable delivery journal, exact final redacted bytes, possibly-sent boundary, protected receipt, atomic source/run completion, conservative trusted comment recovery, and refusal replay before reevaluating readiness.
- US-003: persisted source/delivery recovery metadata, daemon-owned retry timing and bounded redacted diagnostics, in-memory outage debt, retained resolved state, operator documentation, and fake-clock/SQLite queue tests.

## Implementation decisions within scope

- Existing unconstrained run status storage supports prelaunch without a status-schema migration. Delivery intent and recovery metadata use two new embedded migrations.
- A source-only scheduling key is retained when storage cannot expose a delivery identity. It is not represented as a fabricated delivery journal key.
- Startup failures without an available tick timestamp acquire first eligibility at the first tick plus the polling interval.
- Final resolved diagnostic/escalation metadata remains durable but is excluded from active retry scheduling.
- Atomic delivery completion replaces the old runless source-consumption helper. Reporter failures return categorized errors; daemon owns bounded loud diagnostics.
- Existing test expectations that treated transient preparation or uncertain POST failures as automatically reportable/repostable were updated to the approved behavior.

## Outside this run

Historical deployed-incident reconstruction, duplicate cleanup, forced resend/skip controls, new configuration/frameworks, installed instruction changes, deployment and runtime editing remain outside scope. No such work was introduced.

## Current gate status

All scoped story tests and final aggregate format/vet/pinned lint/full race suite passed, as did the safe build. All three final immutable engineering captures are recorded; factory save and remote effects remain pending. No scope deferral or added feature scope was reported by the implementation/test specialists. Final review belongs to an explicitly requested Validate run; none was performed here.


## Run 0006 — aggregate recovery (2026-09-11)

No implementation or plan comparison was performed in this recovery-only run. Existing completed engineering was unchanged. The executed aggregate gate detected an instruction-handoff failure: `.claude/agents/research-orchestrator.md` lacks the exact expected `.claude/skills/shared/workflow.md` handoff according to `internal/teamsource/aggregate_instruction_parity_test.go:127`. No source inspection or correction was undertaken; this is an executed-test finding, not an acceptance or design assessment. See TEST_REPORT.md.
