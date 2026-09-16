# One-off legacy inventory — run 0015

The owner correction supersedes run 0014's proposal for a new CLI command. This
session supplies a disposable Go script using existing read-only store APIs;
no product feature, database schema or CLI command was added.

From the repository root:

```sh
go run .aiteam/lf-264-adopt-the-shared-communication-contract/2026-09-15/adopt-the-shared-communication-contract/legacy_inventory.go > /tmp/legacy-inventory.json
```

The default database is resolved by `config.Locate().DBPath()`; `--db` selects an
existing database explicitly. The script opens it with `store.OpenMeterReader`
(`mode=ro`) and never calls migrations, upgrade apply or the forge. It emits
identifiers and state, excluding submission bodies, provider context and
capabilities. It does not migrate saved Markdown files.

Exit 0 means all selected reads completed. Exit 2 means at least one read failed;
the partial JSON retains the error and available observations. `go run` reports
the child's exit 2 in stderr but itself exits 1. Neither exit means that a reader
can be removed. Output lists three independent `unknown; retain` gates because
the existing APIs do not establish complete drain or a future admission boundary.

## Actual observation

`US-009-live-inventory-run-0015.json` records the actual local database read on
2026-09-16 between 17:04:59.781712Z and 17:05:01.178343Z. It contains 186 canonical
session owners and 89 selected pending/unsuccessful records (not 89 independent
unfinished tasks). Reads occur over an interval, not one atomic snapshot.

Three current legacy cohorts have unfinished story records:

| Parent | Generation | Workflow/content/submission | Unfinished stories |
| --- | --- | --- | --- |
| #73 | 1 | 1/1/1 | US-1, US-2, US-3, US-7 |
| #169 | 1 | 1/1/1 | US-1 |
| #207 | 1 | 2/1/1 | US-012 |

All three are recorded as `implementing`. The existing upgrade safety check
refuses each because upgrade requires a planning scope. These records are
candidates for operator/factory reconciliation, not proof the corresponding
remote issues remain open. Ten additional legacy current cohorts have zero
unfinished story rows; they do not require blind migration of completed work.

Two ledger reads failed: this runtime database has neither `comment_operations`
nor `mcp_creation_operations`. The script deliberately retained the errors;
missing ledgers are not evidence that pending effects are zero. The run exited
2 as designed. No schema migration was attempted.

## Next supported action

Use the factory's current state to disposition #73, #169 and #207: finish their
remaining work under the existing contract, or deliberately return eligible
scope to planning through the supported workflow, then use the existing
`story-contract upgrade <parent-ref> --preview` and authorized `--apply` flow.
Preview performs authenticated forge reads and is therefore an operator/factory
step, not something this local script invokes. Preserve the upgrade receipts;
publish and approve a new Plan afterward. Content/submission upgrades preserve
workflow versions. Do not update state/version columns by hand.

Resolve runtime-ledger coverage through the factory's supported lifecycle and
recover pending effects. Refresh this report afterward. The report explicitly
lists remaining coverage limits: orphan managed features, historical attempt
bindings, child continuation details, schedules, further recovery ledgers and
future admissions. Their disposition and compatibility proof are still required
before removing any of the three legacy readers. No retirement or acceptance is
claimed by this script.
