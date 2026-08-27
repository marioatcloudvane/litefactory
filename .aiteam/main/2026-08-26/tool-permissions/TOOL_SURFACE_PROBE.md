# Tool surface probe — T-12 (AC-2.14)

Companion to `.aiteam/main/2026-08-25/session-resume/CLAUDE_CLI_CONTRACT.md`, in the same style and
for the same reason: this feature's whole value proposition is that an operator-authored profile
actually widens the *resolved* tool surface of a real `claude` process. That was asserted from run
logs during planning; here it is observed directly.

**Verified against:** `claude 2.1.247 (Claude Code)`, macOS (darwin 25.5.0), 2026-08-27.
**Note the version drift:** `CLAUDE_CLI_CONTRACT.md` was verified against `2.1.246`. Every behaviour
below is consistent with what that document records.

Probe method, per the established convention: read-only, in a scratch directory outside the
repository. No daemon, no live store, no GitHub, no worktree. Each invocation used litefactory's own
argv shape — `-p --output-format stream-json --verbose --session-id <fresh uuid> --tools <surface>
--settings <file> [--allowedTools <rules>] --strict-mcp-config` — with the prompt on stdin.

These probes are **not** committed as Go tests. A test that spawns a real `claude` inside
`go test ./...` would be slow, quota-consuming and flaky, and the repository has no build-tag or
env-var gate for integration tests. Recording the observation here is the established alternative.

## The three probes

| # | `--tools` requested | init event `tools` | Exit |
|---|--------------------|--------------------|------|
| A | `Read,Glob,Grep` | `[Glob, Grep, Read]` | 0 |
| B | `Read,Glob,Grep,WebFetch` | `[Glob, Grep, Read, WebFetch]` | 0 |
| C | `Read,Glob,Grep,Edit,Write,Bash,Task,TodoWrite` | `[Task, Bash, Edit, Glob, Grep, Read, Write]` | 0 |

All three reported `permissionMode: default` and `mcp_servers: []`, so `--settings` and
`--strict-mcp-config` still compose with a widened surface. All three ended `result/success`.

### A vs B — a profile genuinely widens the resolved surface (AC-2.14)

`WebFetch` is absent from litefactory's `defaultTools()` surface. Granting it on `--tools` puts it in
the resolved init event; withholding it keeps it out. The resolved surface tracks `--tools` exactly,
which is what makes a profile a real grant rather than a decorative one — and equally, what makes
`tool_ceiling` the boundary that matters.

Note the CLI returns the surface **sorted**, not in the order it was given. Nothing should depend on
argv order round-tripping.

### C — the `TodoWrite` worked example, reproduced

Eight tools requested, seven resolved. `TodoWrite` is dropped:

- exit code **0**
- **stderr empty** — no warning, no diagnostic
- no error event on stdout; the run completed `result/success`

This is the failure this feature's US-4 exists to make visible: a tool can be requested and silently
not granted, and nothing in the process's own output says so. It is why the run report's
`**Tools:**` line names requested-but-not-granted tools rather than only listing what resolved.

`TodoWrite` was removed from `defaultTools()` in this feature (AC-1.12) as a consequence.

## What this does not establish

- Nothing here tests `--allowedTools` semantics. Per invariant C1 that flag is additive to the
  operator's ambient settings and is defence in depth, not the boundary; that was measured
  separately (SD-2) and is unchanged by this feature.
- Probe C was run without `--allowedTools` to keep the requested-vs-resolved comparison clean.

## Re-verification

Re-run these three probes on a `claude` minor-version bump, alongside the five in
`CLAUDE_CLI_CONTRACT.md`. The properties that must still hold: the resolved surface equals the
`--tools` set intersected with tool names the CLI knows; an unknown tool name is dropped silently
rather than rejected; and `permissionMode` stays `default` when `--settings` is passed.
