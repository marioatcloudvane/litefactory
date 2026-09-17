# Aggregate fix cycle 2 — exchange byte pins

Story: US-005, issue #302, revision `058e48341cc4ea550ae30441e0975e5c4c52e769ff875a34ac95892b08d270f1`, dependencies US-001–US-004.

Updated only test-owned corpus instruction SHA-256 pins and expected totals to the current shared-source bytes. The changed references are workflow/SKILL.md (13304 bytes), agent-finalization/SKILL.md (2541 bytes), and agent-finalization/references/common.md (4334 bytes). All other referenced hashes were verified unchanged. Recalculation sums complete raw UTF-8 byte lengths by each listed direction and scenario, including repeated delivery. No exchange segments, expected typed results, baselines, retry cases or Go assertions changed.

## Changed files and content hashes

- `internal/agentresult/testdata/v1/exchanges/corpus.json`: `0733b58879c58235bb5319858abe6f1fdebea1d721d10fe9e2dcfe1a895da8f5`
- `internal/agentresult/testdata/v1/exchanges/totals.json`: `8d0ca4fb0cd9419db8b6ec0593f185bd7038fe217b61850c72a3a501e46831ee`

## Executed check

`go test ./internal/agentresult -run '^TestExchangeCosts_PinnedTotalsAndIdenticalResults$' -short -race -cover -count=1 -timeout=8m -v`

```text
=== RUN   TestExchangeCosts_PinnedTotalsAndIdenticalResults
=== RUN   TestExchangeCosts_PinnedTotalsAndIdenticalResults/cold
=== RUN   TestExchangeCosts_PinnedTotalsAndIdenticalResults/continuation
=== RUN   TestExchangeCosts_PinnedTotalsAndIdenticalResults/stale-retry
=== RUN   TestExchangeCosts_PinnedTotalsAndIdenticalResults/incomplete
=== RUN   TestExchangeCosts_PinnedTotalsAndIdenticalResults/blocked
--- PASS: TestExchangeCosts_PinnedTotalsAndIdenticalResults (0.12s)
    --- PASS: TestExchangeCosts_PinnedTotalsAndIdenticalResults/cold (0.01s)
    --- PASS: TestExchangeCosts_PinnedTotalsAndIdenticalResults/continuation (0.03s)
    --- PASS: TestExchangeCosts_PinnedTotalsAndIdenticalResults/stale-retry (0.03s)
    --- PASS: TestExchangeCosts_PinnedTotalsAndIdenticalResults/incomplete (0.03s)
    --- PASS: TestExchangeCosts_PinnedTotalsAndIdenticalResults/blocked (0.02s)
PASS
coverage: 37.6% of statements
ok  	github.com/cloudvane/litefactory/internal/agentresult	1.698s	coverage: 37.6% of statements
```

Exit code 0: five scenarios passed, zero failed; package coverage 37.6% for this targeted run. Full aggregate totals now equal 199359 bytes (189615 input, 9744 output); scoped totals equal 45736 bytes (42324 input, 3412 output). Output totals remain unchanged.

## Bounds

This is offline fixture execution against the current uncommitted worktree. It verifies byte pins, independent total recomputation and equal typed results including stale rejection/retry. It does not establish host invocation, authenticated receipts, live retirement or aggregate make-check success. No production source, immutable release or installed tree was edited.

The table in docs/agent-result-exchange-costs.md still records the previous totals. Documentation was outside this test-only assignment; the orchestrator should retain this discrepancy or separately authorize updating its table.
