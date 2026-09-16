# Run 0009 engineering completion

## Run 0009 — engineering remediation completed (2026-09-16)

Selected scope: US-003 (#293, revision 4905bbbf909f786e54c46e750cdf06b2302f99c8861ff1494fcec22435f87c2d) and US-005 (#295, revision fc563a920b331e13af6377620c0bddfffc6372dea0508fc9ff0d4895e70ec105). US-001/002/004 remained satisfied read-only dependencies. This run implements the first remediation step only; US-006 authentic captures were not acquired or claimed.

US-003 required two bounded fixes before truthful completion: equivalent inline/quoted/dotted reserved-server TOML declarations could bypass collision detection, and Go-only control-character escapes were not valid TOML strings. The current code rejects reserved collisions with bounded parsing, encodes TOML-compatible control escapes, and refuses invalid UTF-8. Production changes: internal/codex/mcp.go and new internal/codex/mcp_config.go; regression coverage: internal/codex/mcp_test.go. US-005 needed no source change.

Fresh executed checks on final source bytes:

- US-003: 14 targeted test functions/38 subtests with race detection and coverage; zero failures; scoped vet passed. See run0009-us003-unit.log and run0009-us003-vet.log.
- US-005: 2 fake-boundary test functions/6 subtests passed with race detection; 11 production preparation/reconciliation tests passed with race detection; 5 supplementary real-local-I/O recovery/correlation tests passed both plain and race. See run0009-us005-unit.log, run0009-us005-targeted.log and run0009-us005-local-io.log.
- Aggregate: go build ./..., go vet ./..., and gofmt -l . passed; formatting output was empty.
- make check PKGS='./internal/codex ./internal/agent' TEST_TIMEOUT=8m passed: formatting, vet, pinned linter (0 issues), and Codex/agent race suites. Linter cache-write warnings were non-failing and retained in the full log.
- Every remaining package/test function was exercised through 103 bounded race batches with -count=1 and -timeout=8m; all 103 passed. The current complete inventory contains 35 packages and 2,200 test functions, including Codex/agent covered by make check. This is exhaustive batched aggregate execution, not a claim that the default unbounded single make check invocation ran. Exact selections are in run0009-test-batches.json; actual results, command logs and SHA256 locators are in run0009-race-summary.log and run0009-race-ledger.json.

No live-host acquisition, review/acceptance transition, issue closure, daemon changes, commit or push was performed. Authentic host evidence remains a separate US-006 obligation.

### Final receipt observation

- US-003: request `2a3a35ba372f06235a76729cd45fd1c1`; input valid, engineering recorded, acceptance not_applicable, remote effect delivered. Snapshot `run0009-us003-final`, SHA256 `302950afb2e20a2b212e6ac5eed9cd0c9d5f85f1ce7891f942e7be208c0b3ff8`.
- US-005: request `c6eb135c8fcea66ff1d92b39e8877360`; input valid, engineering recorded, acceptance not_applicable, remote effect delivered. Snapshot `run0009-us005-final`, SHA256 `97eb1c525b3e3cd20b830f682fe962f2f648fb57cac860a690546a5a0af28acb`.

Both captured file sets were rechecked against current bytes after all gates. No mismatches.
