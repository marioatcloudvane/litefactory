# Working with issues

[Documentation](../README.md) · [Using the factory](README.md)

Keep the outcome you want, the boundaries and how you will check it in the issue. Then use a new owner comment to ask for the next step (setup is in [getting started](../getting-started/README.md)).

```text
/plan Add password reset to the existing login flow. Keep the current mail provider. Describe expiry and invalid-token behavior before implementation.
```

Expect either questions or a plan proposal. Read what the factory posts before you ask for the next step. A quoted note, an attached file or a copied command is context only; it cannot approve anything, change the mode or give credentials. Only the configured owner counts, and the factory checks the numeric GitHub user ID, not the display name.

## How comments are read

- A comment is an instruction. Editing an old comment is not a reliable way to ask again; write a new one.
- A mode command must be the first word and needs text after it. An empty command gets a reply asking for text.
- Plain owner text can also start work. It continues the mode already chosen, or the factory picks one. See [workflow](workflow.md).
- Comments from the bot are ignored. Comments from anyone else are recorded and ignored.

The full list of commands and labels is in [owner commands](owner-commands.md).

## Refining a story

While the parent is in Plan, you can comment on one of its story issues to ask for a change to that story. The factory handles the discussion through the parent and acknowledges it on the story. It is a request to change the plan, not a request to implement the story.

Stories are approved stage by stage (story, technical, acceptance), then as a whole plan. Once the plan is approved and Implement is working, the stories are frozen. Editing a story issue by hand does not change what Implement builds.

## Changing a frozen plan

When requirements change after the plan is approved, wait until the parent is quiet and comment:

```text
replan
```

An explicit `/plan <instruction>` on the parent does the same. A comment on a story or a label change cannot unlock the plan. Work that was already accepted stays as history; describe later changes as new work.

## If nothing runs

Check that:

- the comment is from the configured owner,
- the repository is registered and the daemon is running (`litefactory daemon status`),
- the issue has at most one label of each kind (`Mode:`, `Coding Agent:`, `Sync:`),
- the daemon has had a chance to poll (see the poll interval in the [configuration](../configuration/config-yaml.md)).

A missing report does not always mean nothing happened. The run may have finished while posting failed. See [recovery](../operations/recovery.md) and [troubleshooting](../operations/troubleshooting.md) before you repeat an action. To ask the factory to examine a stuck issue, use [Doctor](../operations/doctor.md).

See also [workflow](workflow.md), [attachments](attachments.md) and [owner commands](owner-commands.md).
