# Run 0015 result

The requested one-off script is delivered and executed; no product feature or CLI command was added. US-009 remains incomplete.

## Deliverables

- `legacy_inventory.go`: session-local script using existing read-only store APIs.
- `legacy_inventory_test.go`: offline SQLite fixtures.
- `US-009-script-guide.md`: invocation and supported next steps.
- `US-009-live-inventory-run-0015.json`: actual runtime observation.
- `US-009-engineering-run-0015.md` and `US-009-script-test-report.md`: scoped engineering/test evidence.

## Findings

The live inventory reports 186 canonical session owners and 89 selected pending/unsuccessful records. Legacy cohorts with unfinished stories are #73 (4), #169 (1), and #207 (1). Their implementing state currently prevents the supported contract-upgrade safety check. The runtime database lacks comment_operations and mcp_creation_operations; the script records those errors and exits 2 (go run wrapper exit 1). All reader gates remain unknown; retain.

The next operational work is handling those actual cohorts through supported completion/admission and obtaining complete pending-effect evidence. No manual migration, runtime rewrite, forge access, daemon restart or reader deletion occurred. This run does not authorize upgrades or establish drain.

## Checks

Four SQLite fixture tests passed in plain and race runs, 64.9% coverage. They verify missing/unmigrated database rejection, empty inventory with conservative gates, legacy identity, and uncertain delivery preservation without report contents. Script compilation and gofmt -l . passed. make check passed vet and pinned lint, then exceeded its 480-second bound in the full race suite; recorded exit 124. Aggregate checks are incomplete.

## Factory checkpoint

US-009 revision ef612f5b50be87b3692b0d38b645e4eef2d87e61e1a5db067e8aab5e211cefb7 received a blocked contribution. Preflight was valid/complete. Request ff41e9a8c04bd7ccba92df44dbcf0922, receipt 163: input valid, engineering rejected, acceptance not_applicable, effect pending. Pending is not successful delivery. No engineering-complete claim was made. Root still owns the final application result, since the supplied descriptor is the story checkpoint route.

All new implementation files belong to this session. Completed dependencies and other stories were not reimplemented. No commit or push was performed.
