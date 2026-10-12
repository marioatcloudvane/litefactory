# Getting started

[Documentation](../README.md)

Follow this sequence on a Mac you operate. You need a GitHub repository you can
use for a trial, a local clone, separate owner and bot accounts, and an installed,
authenticated coding agent.

1. [Install litefactory and prerequisites](installation.md).
2. [Set up GitHub](github-setup.md): owner, bot, token, repository registration,
   daemon and optional webhooks.
3. For Linear tickets, [set up Linear](../project-connections.md#linear-tickets-with-github-code) after registering GitHub code.
4. Optional: [set up Discord](../discord.md) for chat and task notices.
5. [Request your first Research run](#request-your-first-research-run) below.

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
created. Confirm a verified human ticket approver and an installed team linked
to the clone, with a compatible stack profile; [GitHub step 4](github-setup.md#step-4-register-your-repository) covers both.

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

After appointment and ticket-source enrollment, create an open issue **as the
human ticket approver**, in the configured GitHub repository or connected Linear
team/project. The publishing bot cannot instruct work. For Linear, use the
mapped `research` team label, or allow mode inference (all inferred modes still
need label mappings).

For the body path, put this instruction in a new, nonempty issue body and leave
it without comments until the factory first sees it:

```text
/research Explain this repository's main components and propose a small documentation improvement. Do not implement changes.
```

An eligible approver-authored body is the first instruction. It must contain an
actionable request, have no factory marker or existing comments, satisfy the
appointment activation boundary and not predate source enrollment. Creating it
after setup avoids those historical boundaries.

For an existing issue or an ineligible body, wait until the daemon has first
seen it (check the task's first-sight entry in the daemon logs), then post the
same request as a **new comment** from the approver. Historical comments are
skipped at bootstrap; for enrolled ticket sources, comments created at or after
enrollment are retained for processing even if posted before first sight.
Do not depend on old comments or edit one to authorize new work. See
[daemon and logs](../operations/running-the-daemon.md) for log access.

A webhook or poll discovers work. Capacity, backoff and access problems can
delay dispatch; posting is not a start-time guarantee.

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
