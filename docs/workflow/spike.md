# Spike

[Documentation](../README.md) · [Workflow guide](../guides/workflow.md)

Spike is one conversational agent working with the owner on one issue. It reads
the code, answers questions, asks when something is unclear, changes code only
when the owner clearly says to, runs whatever checks it judges useful, and
reports. All of that behaviour lives in the role's Markdown; the factory only
posts the agent's message and waits for the owner when the agent asks.

## Activation

Spike runs only on the graph. Start it with `/spike <request>` or `Mode: Spike`
(see [owner commands](../guides/owner-commands.md)). Every new eligible Spike
session is admitted to the graph once, before anything is dispatched. A leftover
`.litefactory/spike.yaml` has no effect and is never read; delete it.

A session whose recorded history belongs to the retired single-agent route, or
whose provenance cannot be told apart from it, is refused before any agent is
dispatched, with a message to start a fresh Spike. An explicit switch to another
mode cancels a running Spike graph.

## Permissions

The Spike agent runs with write access. Its tool surface is the
`spike-fixer` profile under `tool_profiles.permissions` (edits and the
project's build and test commands, no delegation), narrowed by
`claude.tool_ceiling`. The `spike-investigator` profile must still be present
and must not exceed it. If either binding is missing or inconsistent, Spike is
refused for that agent instead of falling back to another profile.
`litefactory init` writes both.

## The graph

The flow lives in one file,
[`teams/source/base-team/spike/spike.dot`](../../teams/source/base-team/spike/spike.dot):

- `spike`: the agent ([`spike.md`](../../teams/source/base-team/spike/spike.md))
  with the
  [`spike-result`](../../teams/source/base-team/spike/skills/spike-result/SKILL.md)
  skill. It declares `lf_response="result"`, so its own response is the outcome.
- `ask` → `waiting`: on `ask`, the factory posts the agent's comment and
  questions and waits for the owner.
- `report` → `complete`: on `done`, the factory posts the agent's report.
- `failure` → `stopped`: a failed attempt posts why and stops.

## The conversation

- **The agent asks:** the question is posted and the instance waits. The owner's
  reply resumes the same agent with its saved result and the reply. There is no
  cap on question rounds; each reply gets a fresh node allowance.
- **The agent finishes:** the report is posted and the Spike completes. A later
  owner comment starts the next Spike, which receives the finished one's result
  and the comment as context.
- **The agent fails:** one failure report is posted and the Spike stops. Comment
  again to start a fresh one.

The factory does not judge the size of a change, choose or run checks, count
files or repairs, write a Spike note, file follow-up issues or parse approvals.
Check results in a report are the agent's own evidence, not a factory
certification. Changes are committed and pushed with the run's branch, as for any
run; Spike opens no pull request and merges nothing.
