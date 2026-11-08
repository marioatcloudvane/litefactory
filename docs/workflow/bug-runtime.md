# Bug

[Documentation](../README.md) · [Workflow guide](../guides/workflow.md)

The Bug workflow fixes one reported bug. It has the shape of [Spike](spike.md): one agent, the role `bug.md`, and the factory's shared delivery. The flow is in `teams/source/base-team/bug/bug.dot`.

## Starting a Bug task

Label the issue `bug`. An issue you write and label starts on its own, like any owner-filed issue; on any other bug-labelled issue your first comment starts it. If the issue has no `Mode:` label and the comment is not a mode command, the factory treats it as a Bug task. This is fixed for the life of the task: a mode command (`/plan`, `/implement`, ...) on a Bug task is refused. Commands are listed in [owner commands](../guides/owner-commands.md).

Bugs filed by a run (a Validate follow-up, an agent) carry the label too, but wait for your first comment, so nothing spends money until you ask.

### Filing a bug from a chat client

You can also ask your connected chat client to "file a bug". The client proposes the `bug_file` action (through the `bug_file_propose` tool, or `action_propose` with the same three inputs). You check the proposal and say yes; the client then confirms it. The factory then:

1. creates one issue on the chosen repository with the factory's bot account. On GitHub the body is your description, the line "Filed for the owner from a chat client." and a hidden marker line. Before any create the factory looks for an issue carrying that marker, so a retry or a restart never files a second issue. On Linear the replay marker is the creation UUID kept in the ticket-effect ledger;
2. adds the existing bug label (it never creates a label; a repository or Linear project without one is refused before anything is filed);
3. starts the Bug as your scheduled work, so you do not need to comment first.

Ask the client what happened to the proposal (`action_inspect`): it shows the issue link once it is known, and a plain state such as "filed but not yet labelled" or a failure with its reason. Setup and a live check are in the [remote connector guide](../guides/remote-connector.md#filing-a-bug-from-chat).

## Choosing the agent

The Bug's explicit `Coding Agent:` label wins, then the optional top-level `bug_agent`, then the default agent. See [configuration examples](../configuration/config-yaml.md#bug_agent).

## The steps

1. **Reproduce and diagnose.** The agent writes the smallest failing test that shows the bug and runs it, then finds the cause. It works with write access in its sandbox.
2. **Fix or ask.** When the cause is shown and the fix is small, it fixes the bug and runs the test again, red then green. When the fix would be a redesign, the cause stays unclear or it is not a bug, it asks you with options and a recommendation; your reply resumes the same agent. When there is nothing to fix it finishes with a report and no change.
3. **Review.** A finished fix is saved, pushed and opened as a pull request by the factory, which posts a shipping review with the commit and the agent's red/green evidence.
4. **Ship.** Only your exact, authenticated `ship` reply to that review merges the pull request at the reviewed head and closes the issue. Any other reply resumes the agent. A changed source or base sends the work back to the agent for fresh checks and a new review.

The factory alone commits, pushes, merges and closes. Each delivery step observes the real state first and does only what is missing, so repeating it after a failure is safe.

Advisory [`/judge`](../guides/judge.md) is separate from the Bug graph; the Bug's saved results with findings are its sources.

## Bug history from older versions

Bug tasks started under the earlier analyst/fixer graph are not restarted or converted; open a new issue for the same bug.

The repository check recipe used by [Validate](validate.md) and [Spike](spike.md) is described in [Bug checks](../bug-checks.md).
