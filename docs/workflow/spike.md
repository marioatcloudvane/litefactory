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
- `prepare` (`lf_deliver`, `push`): on `done`, the factory saves, synchronizes
  and pushes the task branch and finds or opens the PR. An answer without branch
  changes is `unchanged` and goes to `report` → `complete`, which publishes the
  agent's final report.
- `review` → `waiting`: a prepared candidate (`ready`) gets a shipping review
  (`lf_kind="review"`, `lf_approve_reply="ship"`) showing the saved commit, base,
  PR and the agent's checks, and waits for the owner.
- `deliver` (`merge`) → `close` (`close`) → `complete`: only an authenticated
  owner `ship` or `ship it` reply to the current review (any case, surrounding
  whitespace and trailing `.`, `!` or `?` ignored) releases `approved`; the
  factory merges the reviewed commit and closes the issue. Every other reply
  resumes the Spike, which finishes with `done` to get a fresh review.
- `revalidate` from `prepare`, `deliver` or `close` returns to `spike` when the
  source or base changed: the agent reruns its checks and a new review follows.
  Other feedback on the review resumes the same agent. Remote failures park for
  the owner's retry. See [Factory delivery](delivery.md).

- `failure` → `stopped`: a failed attempt posts why and stops.

## The conversation

- **The agent asks:** the question is posted and the instance waits. The owner's
  reply resumes the same agent with its saved result and the reply. There is no
  cap on question rounds; each reply gets a fresh node allowance.
- **The agent finishes:** answers finish; checked changes await the shipping review and the owner's `ship`. A later
  owner comment starts the next Spike, which receives the finished one's result
  and the comment as context.
- **The agent fails:** one failure report is posted and the Spike stops. Comment
  again to start a fresh one.

The agent chooses and runs checks; its report is evidence, not factory
certification. Markdown defines that behavior. DOT defines preparation, review,
revalidation and shipping routes. The generic Go activity owns delivery effects;
it grants no permission on its own. Shipping approval is bound to the displayed
source, commit and base. Retries reconcile remote state before repeating effects.

The same `lf_deliver` activity is available to Bug and Validate with their own
approval rules. Their old action names remain compatibility wrappers; Validate's
owner commands and durable effect records keep their existing boundaries.
