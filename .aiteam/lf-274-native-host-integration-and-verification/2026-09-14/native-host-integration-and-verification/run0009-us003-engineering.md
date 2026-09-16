# Run 0009 — US-003 engineering remediation

Frozen issue #293 revision `4905bbbf909f786e54c46e750cdf06b2302f99c8861ff1494fcec22435f87c2d`; dependency US-001 remains satisfied and unchanged.

The existing Codex launch wiring was present. Engineering verification identified two defects within AC-005: reserved server declarations using an inline table escaped the line-prefix collision check, and `strconv.Quote` emitted Go-only control escapes that are not TOML string escapes. Both are fixed before recording engineering completion.

Production changes:
- `internal/codex/mcp.go`: TOML-compatible encoding for control characters, invalid UTF-8 rejection, collision inspection delegation. Existing override ordering, exact enabled-tools catalog, empty explicit environment, isolation flags and sandbox behavior remain in place.
- `internal/codex/mcp_config.go` (new): bounded TOML key scanner recognizes quoted, escaped, spaced, dotted and inline-table forms of the reserved root server. Input errors and excessive nesting fail before launch; comments and string values do not become keys. This isolation scanner is not a general TOML semantic validator; the host still owns that validation.

Unit engineering owns regression changes in `internal/codex/mcp_test.go` and retains actual scoped execution in `run0009-us003-unit.log`. Scoped `go vet ./internal/codex` exited 0 with no output; execution record is `run0009-us003-vet.log`.

Recommended immutable inventory for this story includes `internal/codex/mcp.go`, `internal/codex/mcp_config.go`, `internal/codex/mcp_test.go`, `internal/codex/runner.go`, `internal/codex/runner_test.go`, `internal/codex/delivery_test.go`, `internal/codex/skills.go`, and unchanged shared registration `internal/agent/mcp.go`. Include `Makefile`, `.golangci.yml`, `go.mod` and `go.sum` for check/toolchain attribution. Cross-host production lifecycle coverage belongs to the US-005 inventory.

The orchestrator owns aggregate checks, final manifest capture and factory preflight/submit/receipt confirmation. No host was launched, no acceptance review was performed and no completion receipt is claimed by this report. Authentic host compatibility remains an external evidence obligation.
