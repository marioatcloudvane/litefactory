# litefactory documentation

Litefactory runs coding work from GitHub issues on your Mac. You describe a task
in an issue comment; the factory runs a configured Claude or Codex agent in a
separate Git worktree and reports back on the issue. Your registered clone stays
available for your own work.

Work has five explicit modes: **Research**, **Plan**, **Implement**, **Validate**
and **Spike**. You select the next mode; a successful run does not advance it
for you. Engineering completion, acceptance and delivery to the base branch are
separate steps. New to the terminology? Read the [glossary](glossary.md).

## Getting started

- [Getting started](getting-started/README.md): the path from nothing to a first Research run.
- [Installation](getting-started/installation.md)
- [GitHub setup](getting-started/github-setup.md): token, identities, repositories and webhooks.
- [Discord setup and use](discord.md): optional chat with the factory.

## Using the factory

- [Guides overview](guides/README.md)
- [Working with issues](guides/working-with-issues.md)
- [Workflow](guides/workflow.md)
- [Validation and delivery](guides/validation-and-delivery.md)
- [Scheduling](guides/scheduling.md)
- [Attachments](guides/attachments.md)
- [Sync and conflicts](guides/sync-and-conflicts.md)
- [MCP](guides/mcp.md)
- [Workflow activities](workflow/activities.md): every graph activity and attribute, by category
- [Research](workflow/research-graph.md)
- [Plan and Implement](workflow/plan-graph.md)
- [Validate](workflow/validate.md)
- [Spike](workflow/spike.md)
- [Bug](workflow/bug-runtime.md) and [bug checks](bug-checks.md)
- [Repositories](cli/repositories.md) and [teams](cli/teams.md) (how-to)

## Reference

- [Owner commands](guides/owner-commands.md): every issue comment the factory accepts.
- [CLI reference](cli/command-reference.md) ([CLI overview](cli/README.md))
- [Configuration overview](configuration/README.md) and [config.yaml reference](configuration/config-yaml.md)
- [Coding agents](configuration/coding-agents.md)
- [Retained evidence handoffs](retained-evidence.md)
- [Permissions](configuration/permissions.md)
- [Glossary](glossary.md)

## Operations

- [Operations overview](operations/README.md)
- [Running the daemon](operations/running-the-daemon.md)
- [Troubleshooting](operations/troubleshooting.md)
- [Doctor](operations/doctor.md)
- [Recovery](operations/recovery.md)
- [Cleanup](operations/cleanup.md)
- [Usage and costs (meter)](meter.md)
- [Codex initialization troubleshooting](codex-initialization-troubleshooting.md)

## Maintainers

- [Release runbook](release-runbook.md)
- [Stack profiles](stack-profiles.md)
- [Agent evidence capture](agent-evidence-capture.md)
