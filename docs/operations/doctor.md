# Doctor

The Doctor is one agent that looks at one stuck factory task. It is a graph
workflow like Spike: everything it does is written in its role,
`teams/source/base-team/doctor/doctor.md`, and its flow in `doctor.dot`.

## Starting it

- Comment `/doctor [what to look at]` on the task's issue, or
- ask a chat client (Discord or MCP) to diagnose the task; the owner confirms the
  action as usual.

Only the owner starts or answers it. It starts only while the task is stopped:
no run of the task is running and no step of its workflow is pending or running.
Stop running work first with `litefactory doctor stop <task-ref>`.

## What it gets

Before it starts, the factory writes a read-only evidence bundle into the task
worktree's git directory, outside the committed tree: the task's recorded rows
(session, latest runs, workflow instances, steps, dispatches, owner waits), the
tail of each recent run log, and the branch's git state. Every file is redacted
with the factory's redactor first. The agent runs in the same sandbox as every
other agent: the factory's database, `$LF_HOME` and credentials are unreadable,
and `git add`, `commit`, `push` and `stash` are denied.

## What it does

It explains the cause in plain words and proposes a repair. It changes the
task's worktree and branch only after an explicit go. A finished diagnosis
without changes is posted as its report. A repair goes through the shared
delivery path: the factory saves, pushes and opens or updates the pull request,
posts a shipping review and merges only on the owner's `ship`.

Factory state (its database, queues, logs) is never changed by the Doctor. When
the cause is there, the Doctor says so and tells the owner what to do.

## State

`litefactory doctor <task-ref>` shows whether the task is stopped and the steps
of its latest Doctor run.
