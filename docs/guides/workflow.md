# Workflow

[Documentation](../README.md) · [Using the factory](README.md)

Tell the factory what result you want by choosing a mode. You need a registered repository, a running daemon and configured owner and bot identities (see [getting started](../getting-started/README.md)).

| Mode | Result | Details |
| --- | --- | --- |
| Research | Investigates the problem against the real code, asks you what it cannot decide, and saves a brief. | [Research](../workflow/research-graph.md) |
| Plan | Splits the work into stories with acceptance criteria and gets your approval stage by stage. | [Plan](../workflow/plan-graph.md) |
| Implement | Builds the approved stories, each in its own worktree, and merges them into a feature branch. | [Plan](../workflow/plan-graph.md#after-the-plan) |
| Validate | Runs the full test suite on the result, reviews it, repairs a little if needed, and helps you ship. | [Validate](../workflow/validate.md) |
| Spike | Answers one narrow question with a small experiment or one small fix. | [Spike](../workflow/spike.md) |
| Bug | Diagnoses a bug, waits for your approval, fixes it and delivers the fix. | [Bug](../workflow/bug-runtime.md) |

Start with an owner comment:

```text
/research Explain the current login flow and identify what a password reset needs.
```

You get a run report and, for Research, a saved brief. Moving on is always your decision: ask for `/plan <instruction>`, then `/implement <instruction>` when the plan is approved, then `/validate <instruction>`. A successful run never moves the issue to the next mode by itself. (The one optional exception is `validate.auto_start` in the [configuration](../configuration/config-yaml.md), which starts Validate once Implement has merged every story.)

## How the mode is chosen

- An explicit command (`/research`, `/plan`, `/implement`, `/validate`, `/spike`) picks the mode for that comment.
- A plain comment continues the mode already chosen for the issue. If none has been chosen, the factory picks one.
- The chosen mode is stored as a `Mode:` label. Two mode labels, or a malformed one, are refused. Exact syntax and the other labels are in [owner commands](owner-commands.md).
- An issue with the `bug` label and no mode label or command is treated as a Bug task. A Bug task cannot be switched to another mode.
- Once a plan is approved, ordinary work on that parent goes to Implement. An explicit `/plan` or the word `replan` reopens Plan; see [working with issues](working-with-issues.md).

## Questions

Research and Plan ask you questions in the issue thread when they need a decision. Answer in a normal comment; there is no terminal waiting for you, and waiting has no time limit. Writing "no questions" in your instruction turns questions off for that request. Implement, Validate and Spike do not ask, even with `lf: ask`.

## Approvals and separate results

Asking for review is not approval. Plan stages, Bug repairs, shipping, merging and story creation each need your own exact command, shown in the comment that asks for it. A run that succeeds does not mean the work is accepted or delivered; engineering, review, push, merge and closing are separate steps. See [validation and delivery](validation-and-delivery.md).

If a mode is refused, read the reply: it names the cause, such as ambiguous labels or a Bug task being switched. For the exact commands, see [owner commands](owner-commands.md).
