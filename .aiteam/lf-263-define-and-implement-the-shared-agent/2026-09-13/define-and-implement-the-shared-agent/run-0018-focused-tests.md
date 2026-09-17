# Run 0018 focused US-006 verification

US-006 (#271), frozen revision `86f049513951db118b266e66a35d9205ba7256e20434e0dbcdfabd7b2abf839b`; US-001 through US-005 remain satisfied dependencies. Existing tests were rerun after the authorized Claude instruction parity repair. No tests or production files were changed by this verification worker.

Executed command (exit 0):

```sh
go test -race -cover -count=1 -timeout=8m ./internal/agentresult ./internal/teamsource -run '^(TestFinalization_|TestExchangeCosts_)'
```

All four selected top-level tests passed, zero failed: executable example parity, questions across modes/exits, temporary discovery and both renderings, pinned exchange totals and identical results. Scoped coverage: agentresult 52.1%; teamsource 13.4%. Output: `run-0018-focused-tests.log`.

Executed command (exit 0):

```sh
python3 scripts/agent-result-bytes.py
```

Output: `run-0018-byte-comparison.log`. The pinned five-scenario corpus totals 186,859 full-exchange UTF-8 bytes and 37,551 scoped-exchange UTF-8 bytes, including the stale retry. These are offline corpus byte counts, not billed tokens or observed runtime savings.

AC-001 through AC-003 focused checks pass. AC-004 aggregate checks and final snapshot attribution belong to the orchestrator. Temporary source/rendering tests establish packaging and serialization behavior only; they do not establish actual model late loading, deployment, acceptance, or publication.
