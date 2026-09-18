# Getting started

[Documentation](../README.md)

Follow this sequence on a Mac you operate. You need a GitHub repository you can
use for a trial, a local clone, separate owner and bot accounts, and an installed,
authenticated coding agent.

1. [Install litefactory and prerequisites](installation.md).
2. [Set up GitHub identities and access](github-setup.md).
3. Register the clone and start the daemon below.
4. Post an explicit Research request and inspect its report.

## Initialize and register

After completing GitHub setup, run `litefactory init` again. Confirm that its
owner and bot rows show the intended, different accounts. An initialization
warning about the bot is unfinished setup, even if configuration and database
creation succeeded.

Install the bundled team, then register your local clone:

```sh
litefactory team add go-team
litefactory repo add /absolute/path/to/project --team go-team
litefactory repo list
```

Replace the path with your clone. It needs a supported GitHub `origin` and a
resolvable base branch. Registration prints the repository, selected team,
base branch and worktree location. It reads the clone without installing files
there. Without an external team, registration requires existing aiteam presence;
new users can use the explicit team selection above. See
[repositories](../cli/repositories.md) and [teams](../cli/teams.md) for options.

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
Explain this repository's main components and propose a small documentation improvement. Do not implement changes.
```

The factory reads eligible owner comments during
polling, selects Research and prepares a task worktree. Capacity, backoff and
access problems can delay dispatch; posting is not a start-time guarantee.

Expect a bot run report naming the run and its result, with a research brief
link when produced. Research may ask a question on the issue; answer there.
Inspect both the report and any linked brief. A failed run or missing report
requires investigation, not an assumption that the work completed.

When the research is useful, explicitly request the next step with
`/plan <your instruction>` in a new comment. Review the resulting plan before
requesting `/implement <your instruction>`. Engineering and later `/validate`
are separate stages; see [workflow](../guides/workflow.md) and
[working with issues](../guides/working-with-issues.md).

## Common setup failures

| Symptom | Next check |
| --- | --- |
| Bot is “not configured” | Store its credential and rerun `init`; see [GitHub setup](github-setup.md). |
| Agent executable unavailable | Check configured binary path and daemon PATH; see [installation](installation.md). |
| Registration refused | Check `origin`, base branch, installed team and worktree-root collision diagnostics. |
| No run after a comment | Confirm owner account, registered repository, open issue and daemon health; see [troubleshooting](../operations/troubleshooting.md). |
| Access denied during reporting or publishing | Check bot repository access and local Git transport separately. |

## Verification status

This journey is reconciled with source at
`8f556367c2d0a00d2736a53a326152ff43a6f96e`; it is an instruction sequence, not an
observed installation transcript. **An authentic isolated installation and
GitHub-access check, followed by Research on a disposable repository, has not
been recorded.** Its acceptance obligation remains unverified and unsatisfied.
A future authorized operator capture must record macOS, Go, Git, `gh`, agent and
litefactory versions, credential/access setup without secrets, commands and
actual observed feedback. Offline tests cannot replace that observation.
