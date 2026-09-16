# Task report: US-009 — one-off script prerequisite

Status: requested script delivered; complete frozen US-009 remains BLOCKED.
Identity: #306, issue ID 5463756074, parent ID 5439211198, revision
`ef612f5b50be87b3692b0d38b645e4eef2d87e61e1a5db067e8aab5e211cefb7`.
US-001/US-002 are satisfied, read-only dependencies.

Files created within this session:

- `legacy_inventory.go`: standalone inventory using existing read-only store
  APIs, canonical owner enumeration, current contract versions, unfinished story
  keys, upgrade safety/intent, run/continuation records and selected pending
  effect/receipt ledgers. No raw queries or connections outside `internal/store`.
- `US-009-live-inventory-run-0015.json`: real runtime observation with errors and
  conservative per-reader gates; no credentials or body payloads.
- `US-009-script-guide.md`: invocation, actual findings and supported next steps.
- This engineering report.

Executed checks by implementation worker:

- `gofmt -w <session>/legacy_inventory.go`: completed.
- `go run <session>/legacy_inventory.go --help`: compiled and passed, output
  documented `-db` and existing default resolver.
- `go run <session>/legacy_inventory.go > <session>/US-009-live-inventory-run-0015.json`:
  ran actual read-only inventory; partial evidence retained, child exit 2 due to
  two absent runtime ledgers. This is a real environment limitation, not a passing
  complete inventory or drain check.
- Parsed the emitted JSON with Python and extracted cohort counts, exact version
  tuples, unfinished keys and upgrade refusals successfully.

The orchestrator owns offline fixture tests and aggregate checks. This worker
did not run `make check` or claim unexecuted tests. No product files, installed
instructions, database state, forge records or obsolete readers changed; no
commit or push. Remaining constraints are explicit in the guide. US-009 AC-002
through AC-006 removal/upgrade obligations are not completed by prerequisite
inventory tooling.
