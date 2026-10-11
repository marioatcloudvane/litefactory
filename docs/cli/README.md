# CLI — advanced

Use the terminal to register repositories, manage central teams and inspect the factory. Start with [Getting started](../getting-started/README.md) for installation and initial identities.

| Topic | Use it for |
| --- | --- |
| [Repositories](repositories.md) | Register a clone and choose its team, base branch and worktree location. |
| [Teams](teams.md) | Install, inspect, update and override centrally stored instructions. |
| [Command reference](command-reference.md) | Every command, argument, flag and default. |

## Terminal conventions

Examples assume `litefactory` is on `PATH`. Replace placeholders such as `OWNER/REPO`, `TEAM` and `/absolute/path`; omit angle brackets. Quote paths with spaces and task references containing `#`. Optional arguments appear in square brackets in syntax descriptions, not in commands you paste.

```sh
litefactory --help
litefactory --version
litefactory repo add --help
```

Help prints syntax and flags without starting the command. Place positional arguments before flags as shown. Configuration and state use `$LF_HOME`, defaulting to `~/.litefactory`; there is no general `--config` flag. Exit codes are `0` success, `1` runtime failure, `2` invalid invocation and `3` unmet precondition. Read stderr even after success: initialization may report an unresolved bot identity.

If a command is unavailable, compare its help and binary version with this reference. For missing initialization or invalid YAML, follow [troubleshooting](../operations/troubleshooting.md). GitHub comments such as `/implement` belong in the [issue command reference](../guides/issue-command-reference.md); YAML belongs in [Configuration — advanced](../configuration/README.md). Submission and MCP descriptor commands are factory operations, not onboarding steps.

[Documentation home](../README.md)
