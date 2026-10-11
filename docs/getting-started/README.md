# Getting started

[Documentation](../README.md)

Follow this sequence on a Mac you operate. You need a GitHub repository you can
use for a trial, a local clone, separate owner and bot accounts, and an installed,
authenticated coding agent.

1. [Install litefactory and prerequisites](installation.md).
2. [Set up GitHub](github-setup.md): owner, bot, token, repository registration,
   daemon and optional webhooks.
3. Optional: [set up Discord](../discord.md) for chat and task notices.
4. [Request your first Research run](#request-your-first-research-run) below.

GitHub setup already covers `litefactory init`, registering your clone with
`litefactory repo add` and starting the daemon. If you have done those steps,
skip ahead to the first run.

## Check the setup

Before the first run, confirm each of these:

```sh
litefactory repo list        # your clone is registered
litefactory daemon status    # agent loaded, a recent tick
```

`litefactory init` should show two different accounts as owner and bot. A warning
about the bot is unfinished setup, even if the configuration and database were
created. Specialist teams are optional; see the [CLI reference](../cli/command-reference.md).

## Start the daemon

```sh
litefactory daemon start
litefactory daemon status
```

`start` installs and loads your macOS LaunchAgent; it runs in your GUI login
session and starts again at login. The current shell's PATH is saved in its
plist. Configure absolute agent paths when needed. `status` reports installation,
loading, process and last-tick information. A loaded LaunchAgent alone does not
prove the daemon is healthy. See [daemon and logs](../operations/running-the-daemon.md)
if the process exits or no tick completes.

## Request your first Research run

Create an issue in the registered repository. As the configured owner, post this
as a **new issue comment**:

```text
/research Explain this repository's main components and propose a small documentation improvement. Do not implement changes.
```

The factory reads owner comments when a webhook
arrives or at the next poll, runs Research (the `/research` word selects it) and
prepares a task worktree. Comments from anyone but the owner are ignored. Capacity, backoff and
access problems can delay dispatch; posting is not a start-time guarantee.

Expect a bot run report naming the run and its result, with a research brief
link when produced. Research may ask a question on the issue; answer there.
Inspect both the report and any linked brief. A failed run or missing report
requires investigation, not an assumption that the work completed.

When the research is useful, start the next stage yourself with a new comment
that begins with `/plan` and your instructions. Review the plan before moving on
with `/implement`. Implement and later `/validate` are separate stages; the
factory never moves to the next mode on its own. See the full list in
[owner commands](../guides/owner-commands.md), and read [workflow](../guides/workflow.md)
and [working with issues](../guides/working-with-issues.md).

## Common setup failures

| Symptom | Next check |
| --- | --- |
| Bot is “not configured” | Store its credential and rerun `init`; see [GitHub setup](github-setup.md#step-3-create-the-configuration-and-store-the-token). |
| Agent executable unavailable | Check configured binary path and daemon PATH; see [installation](installation.md). |
| Registration refused | Check that the clone has a GitHub `origin`, a resolvable base branch and no worktree-root collision. |
| No run after a comment | Confirm owner account, registered repository, open issue and daemon health; see [troubleshooting](../operations/troubleshooting.md). |
| Access denied during reporting or publishing | Check bot repository access and local Git transport separately. |
