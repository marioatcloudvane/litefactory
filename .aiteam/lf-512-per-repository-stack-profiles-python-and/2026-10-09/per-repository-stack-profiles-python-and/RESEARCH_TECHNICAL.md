# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## F-1

Release generation is Go-specific and rewrites the catalogue to one release; normal daemon workflow loading is generic and already uses installed repository bindings, so earlier claims that all loaders block a second team were overstated.

Evidence:

- `internal/teamrelease/release.go:46`
- `internal/teamrelease/release.go:126`
- `internal/daemon/workflow_source.go:33`
- `internal/turn/workflow_capture.go:45`

## F-2

Plan draft validation only accepts go and agent-setup guidance.

Evidence:

- `internal/planbrief/plan_draft.go:116`

## F-3

Shared guidance contains mandatory Go race/check and Go platform examples, so neutralizing only story-worker is insufficient.

Evidence:

- `internal/teamsource/shared/team-guidance/platform-constraints.md:158`
- `internal/teamsource/shared/team-guidance/go-unit-test-engineer.md:39`

## F-4

Agent cache/network configuration is held by the Codex runner, Claude and Codex inherit a fixed environment list, and Bug check isolation is constructed from daemon configuration, not repository profiles.

Evidence:

- `internal/codex/runner.go:93`
- `internal/codex/runner.go:615`
- `internal/harness/env.go:46`
- `internal/daemon/setup.go:405`
- `internal/config/policy.go:71`

## F-5

Rights CLI defaults to NewBuiltinSource unless injected; that source ignores the repository, captures built-in Go graphs, and omits Validate. No production assignment of RightsCatalogue was found in the inspected source.

Evidence:

- `internal/cli/cmd_rights.go:312`
- `internal/rights/catalogue.go:51`
- `internal/rights/catalogue.go:112`
- `internal/cli/command.go:96`

## F-6

ValidateBugParent accepts only builtin/go-development and a pinned digest, but source callers found are CaptureBugParent and the rights built-in catalogue; generic installed workflow validation does not call this gate.

Evidence:

- `internal/teamsource/bug_parent.go:38`
- `internal/rights/catalogue.go:58`
- `internal/turn/team_validation.go:51`

## F-7

team.config is a strict nonempty mapping of six workflow names to paths and currently rejects stack metadata; team linking already persists source/slug/version/digest.

Evidence:

- `internal/teampackage/format.go:285`
- `internal/cli/cmd_team_binding.go:129`

## F-8

Factory runtime access is capped by mode permissions; Bug check isolation explicitly treats recipes as unable to grant runtime resources.

Evidence:

- `internal/turn/workflow_access.go:86`
- `internal/config/bug_isolation.go:10`
- `internal/bugchecks/executor.go:33`

## F-9

Spike dependency detection is already multilingual but lacks the listed .NET and Swift manifests and setup.cfg; extending recognition must not remove existing protections.

Evidence:

- `internal/turn/spike_check.go:117`

## F-10

Codex runner takes only writable roots, a network switch and environment; Claude has per-command Bash permissions. A command allowlist cannot bind Codex.

Evidence:

- `internal/codex/runner.go:93`
- `internal/config/policy.go:103`

## F-11

Spike dependency detection matches exact base names, so .NET project/solution files and Swift project files need suffix matching.

Evidence:

- `internal/turn/spike_check.go:117`

## F-12

team.config is parsed as a strict YAML workflow mapping; the owner asked for JSON, so the profile needs its own file or a format change.

Evidence:

- `internal/teampackage/format.go:285`

## F-13

Catalogue is JSON listing releases with files and a checksum; the package holds the YAML team config. A profile held only in the catalogue entry would not be covered by the release checksum.

Evidence:

- `internal/teampackage/format.go:52`
