# Aggregate fix cycle 2 — exchange cost documentation

US-005 #302, issue_id 5463753553, frozen revision `058e48341cc4ea550ae30441e0975e5c4c52e769ff875a34ac95892b08d270f1`; dependencies US-001–US-004. Status: assigned documentation correction DONE.

Updated only the five scenario rows and aggregate row in `docs/agent-result-exchange-costs.md` to the refreshed `totals.json` values. Full aggregate: 189615 input + 9744 output = 199359 bytes. Scoped aggregate: 42324 input + 3412 output = 45736 bytes. Existing synthetic UTF-8 byte, token/billing and host-observation limitations remain unchanged. No source, fixture, test or installed-tree edits.

## Executed checks

A Python consistency assertion loaded corpus.json and totals.json, verified every segment SHA-256, recomputed each scenario input/output/total from complete bytes, checked all six exact documentation rows and aggregate sums, and verified the documented full-guidance SHA-256. Executed against the current uncommitted worktree after the test specialist's fixture refresh.

```text
PASS: all segment SHA-256 pins and five scenario byte totals recomputed; six documentation rows, aggregates and full-guidance hash match fixtures.
PASS: full total 199359; scoped total 45736 UTF-8 bytes. Synthetic offline scope only.
```

Exit code: 0.

`git diff --check -- docs/agent-result-exchange-costs.md` returned no output, exit code 0.

No suites, lint or aggregate gate executed in this assignment. This consistency check establishes offline documentation/fixture agreement only; it does not establish authentic host invocation or complete feature acceptance.
