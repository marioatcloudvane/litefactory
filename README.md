# litefactory

Litefactory runs coding work from GitHub issues on your Mac. Comment as the
configured owner; a Claude or Codex agent works in a separate Git worktree and
the factory reports back. Research, Plan, Implement, Validate and Spike are
explicit modes. You decide when to move to the next one.

**[Read the documentation](docs/README.md)** or begin with
[Getting started](docs/getting-started/README.md).

## Central teams

Install and manage specialist teams centrally under `$LF_HOME/teams` (default
`~/.litefactory/teams`), then select one when registering a repository. Management
does not install specialist files into the registered clone. Current runtime
code captures the selected mode's roster and supplies immutable instruction
bundles to Claude and Codex; older descriptions saying all external-team runs
are disabled are obsolete. This implemented path does not establish authentic
host verification or token benchmarks. See [teams](docs/cli/teams.md),
[coding agents](docs/configuration/coding-agents.md) and
[MCP evidence limits](docs/guides/mcp.md).

### GitHub story progress

Managed plans publish complete stories as GitHub child issues. Implement uses
frozen approved revisions. In the separated workflow, Implement records
engineering and executed checks; Validate assesses retained evidence for
acceptance. Engineering completion can release dependencies while child issues
remain open. Acceptance and confirmed closure are separate factory operations.

Legacy scopes keep their declared combined verification and invalidation
behavior. Do not apply their automatic close/reopen assumptions to separated
acceptance: historical acceptance remains retained and later work uses new
issues. Explicit parent replanning changes unfinished scope. See
[workflow](docs/guides/workflow.md) and
[validation and delivery](docs/guides/validation-and-delivery.md).

### Confirming conflict resolutions

Review the factory's numbered conflict proposal before confirming it. The
sanitized preview can omit diff content; apply uses retained, validated proposal
data and checks that the relevant revisions still match. See
[synchronization and conflicts](docs/guides/sync-and-conflicts.md).

## Automatic recovery after session limits

Quota recovery is opt-in and requires supported terminal provider evidence, a
usable conversation and a reliable future reset time. Saved eligibility is not
a promised start time. See [recovery](docs/operations/recovery.md) and
[configuration](docs/configuration/config-yaml.md).

## Reading polling diagnostics

Worker dispatch counts, prepared runs and completed runs measure different
things. Use [daemon and logs](docs/operations/running-the-daemon.md) to interpret
status and [report recovery](docs/report-recovery.md) when posting is pending
or uncertain.

## Structured story contracts

[The operator protocol](docs/story-contract.md) describes typed Plan sections,
helper feedback, evidence lists, editable issue reconciliation and explicit
legacy upgrade. [The instruction manifest](docs/story-contract-instruction-manifest.md)
records shared-source targets and the manual opposite-harness handoff. These
are deeper references; start with the user guides above.

For advisory retirement, daily cleanup and explicit cleanup controls, see
[cleanup](docs/operations/cleanup.md) and the retained
[Retiring finished work](docs/retirement.md) reference.
