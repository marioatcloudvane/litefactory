# Code review — run 0006 (US-003)

Scoped Phase 1.5 review of US-003 T-001 (internal/codex Codex MCP server override encoding), against issue #293 revision 4905bbbf909f786e54c46e750cdf06b2302f99c8861ff1494fcec22435f87c2d and plan_brief.md.

## Verdict: ADVISORY_ONLY

No blocking findings. Constraint-by-constraint check confirmed: argument placement before resume subcommand/stdin marker, no shell wrapper, features.shell_tool=false/multi_agent=false and read-only Validate untouched, ignore-user-config/delivery-strictness threading correct, no persistent user config writes (bounded read-only scan), no broker-root widening, collision refusal present and wired through `SupportsMCP()`/`internal/turn/mcp_runtime.go`, signature-change propagation correct at all call sites, additive-only nil-spec behavior preserved, idiom matches existing `skillOverrides` pattern.

## Advisory findings (non-blocking)

1. `internal/codex/mcp.go` `checkMCPCollision` — the raw line-prefix scan does not detect the alternative valid TOML form of a bare `[mcp_servers]` table header followed by an un-prefixed `litefactory = {...}` key underneath it. Narrow, operator-config-driven (not attacker-controlled), and Codex's own documented examples use the dotted-header style. Suggest a follow-up hardening note/ticket, not blocking.
2. `internal/codex/mcp.go:46` — `spec.Validate()` duplicates validation already performed unconditionally by `agent.Request.Validate()` before `args()`/`mcpOverrides` is ever reached. Harmless defense-in-depth, currently dead on the real call path.
3. `internal/codex/mcp.go` — `strconv.Quote` used for TOML string literals mirrors the pre-existing `skillOverrides` idiom but its escape set is not identical to TOML's basic-string escapes for rare control characters outside the null-byte check in `MCPLaunchSpec.Validate()`. Values in practice come from litefactory's own binary/descriptor paths, not arbitrary external input — very low probability, pre-existing pattern, not specific to this story's delta.

## Disposition

Advisories recorded in notes.md; proceeding to Phase 3 testing without a fix cycle (ADVISORY_ONLY does not require rework).
