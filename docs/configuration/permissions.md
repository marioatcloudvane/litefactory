# Permissions

[Documentation home](../README.md) · [Configuration](README.md) · [Key reference](config-yaml.md#tool_profiles) · [GitHub setup](../getting-started/github-setup.md)

Litefactory separates the human owner's instructions, the bot's GitHub access and the coding agent's local permissions. Configure all three before running work; a grant in one does not grant another.

## Identities and credentials

The owner is the authenticated human identity discovered at initialization. The bot is the separately configured GitHub identity that reads issues and publishes factory results. Authority checks compare numeric identities; matching a display name is insufficient. Agent subprocesses do not receive the factory's forge token, and issue text cannot authorize new forge targets or credential access.

Configuration stores references, never secret values. Example for an existing Keychain item:

```yaml
providers:
  github:
    token_keychain:
      service: litefactory
      account: github-bot
```

This names an item; it does not create it. Resolve access through [GitHub setup](../getting-started/github-setup.md). Use `token_env` only as the name of an environment variable for foreground development. Keychain and environment references are mutually exclusive. Missing credentials or a missing recorded bot identity prevent daemon operation. Recognized literal credential shapes fail configuration validation without printing the matched secret.

## Mode permissions

Each configured agent needs a cell in the four profiles `research`, `plan`, `implement` and `validate`. The selected mode determines its profile. Spike, Bug and Validate-repair roles have their own optional profiles. The generated posture is:

| Mode | Claude surface | Codex sandbox | Attachment ingestion |
| --- | --- | --- | --- |
| Research | Read/search, artifact writing, bounded shell and delegation tools | workspace-write | enabled |
| Plan | Read/search, artifact writing, bounded shell and delegation tools | workspace-write | enabled |
| Implement | Editing, build/test and delegation tools | workspace-write | disabled |
| Validate | Read, Glob, Grep, Task, and Bash limited to fixed read-only reviewer rules | read-only | disabled |
| Spike investigator | Read, Glob, Grep | read-only | disabled |
| Spike fixer | Read, Glob, Grep, Edit, MultiEdit, Write | workspace-write | disabled |

These are tool surfaces, not permission to exceed a mode's scope. Validate is read-only: the config check refuses any Claude tool outside Read, Glob, Grep, Task and Bash, allows Bash only with the fixed reviewer rules, and requires the Codex `read-only` sandbox. The optional `validate-repair`, `bug-analyst` and `bug-fixer` profiles grant the roles that need them; a missing optional profile blocks only that role. A missing Spike investigator or fixer blocks Spike.

The four main profiles are mandatory and have no missing-cell fallback. The [key reference](config-yaml.md#tool_profiles) lists exact keys, defaults and validation rules. Deprecated `lf:tools=...` labels are refused; remove them and select the intended mode through current [issue controls](../guides/owner-commands.md).

## Claude boundaries

`tools` defines the built-in tool boundary. `allowed_tools` adds approvals and may combine with ambient permissions; it cannot make a granted Bash tool intrinsically read-only. `tool_ceiling` must contain every configured profile tool. An omitted or empty ceiling defaults to the global `tools`, so a Research profile with web tools needs a ceiling that includes them. The generated configuration supplies that ceiling.

`settings_path` selects factory-controlled settings. Do not use personal allow rules as a substitute for a narrow surface. Unqualified or wildcard Bash approvals and permission-bypass options are rejected. The child environment is restricted; `forward_anthropic_env` is an explicit opt-in that can alter Claude authentication.

## Codex boundaries

The mode's `sandbox` is the filesystem boundary; Claude permission lists are never translated into Codex grants. `writable_roots` adds explicit absolute directories for workspace-write runs. Read-only remains read-only. Factory-owned attempt directories may also be supplied so result helpers can function; they do not authorize editing arbitrary runtime state.

`network_access: false` is the default for workspace-write. Enabling it allows network calls generally, including loopback test listeners and outbound connections. This is independent of filesystem grants.

Ordinary runs default to ignoring personal Codex configuration and rules. `allow_user_config: true` permits personal configuration and may expose repository skills as supported by the host CLI. Native team delivery retains configuration isolation and supplies factory-generated role configuration. Do not infer that a personal option overrides factory mode restrictions.

## Check sandbox for Bug checks

Bug checks run under a separate operator ceiling, `bug_checks` in `config.yaml`: a Codex sandbox with no network by default. See the [key reference](config-yaml.md#bug_checks) and [Bug checks](../bug-checks.md).

## Feedback and limits

Configuration rejects incomplete mode/agent matrices, invalid tools, missing ceiling entries and invalid Validate grants. A later sandbox refusal usually means a workload needs an explicit permitted path or a different authorized task; adding an issue label cannot widen the configured surface. Fix the relevant setting and restart, then inspect a new run's effective permissions and diagnostics.

Outbound reports pass through redaction; local `$LF_HOME/logs` copies are **unredacted** and may contain sensitive input or output. Inspect locally and sanitize excerpts before sharing. Redaction is report hygiene, not the subprocess secret boundary.

Factory [MCP](../guides/mcp.md) capabilities have their own mode, scope, receipt and approval gates; registration does not authorize arbitrary third-party servers or effects. See [troubleshooting](../operations/troubleshooting.md) for missing runs, access failures and uncertain outcomes.
