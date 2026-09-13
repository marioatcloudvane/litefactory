# US-006 unit/source tests

Frozen #271 revision `86f049513951db118b266e66a35d9205ba7256e20434e0dbcdfabd7b2abf839b`.
Applied unit-test role after implementation under explicit parent authorization.

- AC-001/002: source examples are byte-identical to the Go corpus; all five modes
  completed/incomplete/blocked exits pass with their trusted contexts. The source
  question shape exercises all mode/outcome/permission combinations. Update examples
  strictly decode and remain identical to executed update fixtures.
- AC-002: isolated temporary source catalog/role captures skill plus every resource;
  existing Native rendering produces Claude and Codex discovery instructions without
  eager serialization text. Captured references survive temporary source mutation.
  This proves packaging only; no model, installed tree or release is launched.
- AC-003: SHA-256 pinned UTF-8 segments independently counted in Go match the Python
  tool's raw totals. Both full-field and scoped contributions validate and produce
  identical desired results; stale requests reject at revision 1/current 2 and the
  counted second contribution succeeds. Every required scenario is asserted present.
- AC-004: focused race tests/pinned lint are executed here. Full repository make check
  and final attribution remain the orchestrator's final engineering gate.

Commands/output:
`go test -race -cover ./internal/agentresult ./internal/teamsource -run '^(TestFinalization_|TestExchangeCosts_)'`
-> US-006-focused-tests.log (passing final output).
Pinned golangci-lint v2.13.1 on both packages -> US-006-lint.log (0 issues).
`python3 scripts/agent-result-bytes.py` -> US-006-byte-comparison.log.
The script uses only Python stdlib; no tokenizer or billed-token estimate exists.
