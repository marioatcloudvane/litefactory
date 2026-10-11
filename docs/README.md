# litefactory documentation

Litefactory runs coding work from GitHub issues on your Mac. You describe a task
in an issue comment; the factory runs a configured Claude or Codex agent in a
separate Git worktree and reports back on the issue. Your registered clone stays
available for your own work.

Work has five explicit modes: **Research**, **Plan**, **Implement**, **Validate**
and **Spike**. You select the next mode; a successful run does not advance it
for you. Managed plans use GitHub child stories and frozen requirements.
Engineering completion, acceptance and delivery to the base branch are separate
steps.

| Category | Start here |
| --- | --- |
| [Getting started](getting-started/README.md) | Install, connect GitHub and request your first Research run. |
| [Using the factory](guides/README.md) | Direct issue work, use MCP and attachments, schedule requests, resolve conflicts and deliver accepted work. |
| [Using the CLI](cli/README.md) | Manage repositories and teams; look up terminal commands. |
| [Configuration](configuration/README.md) | Edit `config.yaml`, choose coding agents and understand permissions. |
| [Operations](operations/README.md) | Run the daemon, inspect logs and usage, recover work and clean up. |

The factory also supports central specialist teams, asynchronous questions,
reviewed conflict proposals, optional quota recovery and usage reporting. Their
guides explain prerequisites, supported controls and evidence limits.

New to the terminology? Read the [glossary](glossary.md).
