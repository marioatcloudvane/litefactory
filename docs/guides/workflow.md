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

Workflow authors can opt into [private advisory judging](workflow-judging.md) in Research and Plan, with a separate activity for publication.

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

## Execution capacity

`settings.max_concurrent_runs` sets shared execution capacity in slots (default
3, supported range 1–16). Each executing workflow step reserves its full slot
cost until its activity and cleanup finish. The next step must acquire its own
cost. Factory-started nested work uses the same pool, counting the parent and
child separately while both execute. An independent child without a workflow
step costs 1; a workflow child uses its pinned step's cost.

Workflow authors set `lf_slot_budget` on executable DOT nodes:

```dot
graph [lf_slot_budget="1"];
node [lf_slot_budget="2"];
worker [lf_slot_budget="3", lf_action="lf_agent", /* other activity attributes */];
```

Effective node attributes, including DOT node defaults, override the graph
value; omission at both levels means 1. Every supplied value must be a positive
decimal integer within the supported integer representation, even if another
value overrides it. Empty, zero, negative, fractional and overflowing values
are refused. `lf_budget` continues to mean a time budget. Terminal nodes run no
activity and reserve no step capacity.

Only the Go Implement story worker initially costs 3 slots. Other workers and
actions retain their effective costs. Costs come from the admitted, pinned
workflow. Editing team source does not change a retained run, and historical
releases and captures keep their original bytes. Release updated source through
the existing team release process; do not edit a published release.

When capacity is busy, work stays pending for a later scheduling tick without
spending an execution attempt or repair allowance. Completed nested reviewer
results are retained so only missing work resumes. Capacity diagnostics name the
pool, workflow/node, requested cost and used, available and total capacity.
Temporary contention is distinguished from an impossible fit: a step costing
more than total capacity cannot start, and a parent plus its smallest required
child must fit together. Operators can raise capacity through normal
configuration or admit a new workflow; waiting never reduces the declared cost.

The scheduler keeps its existing priorities and bounded chaining and continues
inspecting other eligible work when one cost does not fit. There is no waiting
queue or starvation guarantee: cheaper work can postpone expensive work. Doctor
diagnostics and classified Implement bookkeeping retain separately bounded
reserved lanes; agents and nested work use shared capacity. These limits cover
factory-managed local execution in one daemon, not distributed quotas, provider
rate limits or parallel subprocesses inside an admitted activity. Restart still
requires existing interrupted-work reconciliation before readmission.

### Standards book access

A node grants optional, read-only access to the standards book with
`lf_standards`, a comma-separated list of the roles it runs that may read it:
its own `lf_agent_markdown`, or names from its `lf_roles`. Any other name is
refused when the graph is admitted.

```dot
worker_agent [lf_action="lf_agent", lf_agent_markdown="story-worker", lf_standards="story-worker", /* ... */];
review [lf_action="lf_validate_review", lf_roles="validate-acceptance,validate-code,validate-architecture", lf_standards="validate-architecture", /* ... */];
```

Granted roles get the standards tools and may consult them; nothing requires a
call. Roles without the grant get no standards tools. The canonical teams grant
it to the Implement story worker, Plan technical and the Validate architecture
reviewer.
