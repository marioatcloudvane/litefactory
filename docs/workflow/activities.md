# Workflow activities

Every litefactory workflow (Research, Plan, Implement, Validate, Bug, Spike) is a
DOT graph shipped in a team package. Each node in a graph runs one registered
**activity**. This page lists all of them, grouped by category, with what they do,
the outcomes they can return, the attributes they take and their side effects.

## How a graph uses an activity

A node names its activity with `lf_action` and declares the outcomes it handles
with `lf_outcomes`. Each declared outcome needs exactly one outgoing edge, chosen
with `lf_on`:

```dot
suite [lf_action="lf_validate_suite", lf_outcomes="passed,failed,blocked,retry", lf_limit="16"];
suite -> repair_budget [lf_on="failed"];
```

- The graphs live in the team sources: `teams/source/base-team/<mode>/<mode>.dot`
  (go-development), `teams/source/python-fastapi/…`,
  `teams/source/swift-ios/…` and `teams/source/dotnet-web/…`. All four
  teams use the same activities. Released copies sit under `teams/<team>/v<version>/`.
- Topology belongs in the DOT, never in Go. The code registers activities and
  checks graphs; it does not define them.
- Naming rule (`internal/graphexec/registry.go:43`): generic activities have a
  plain name (`lf_agent`, `lf_if`, …); mode activities are
  `lf_<mode>_<capability>`. A mode only accepts its own activities plus the
  generic ones.
- Unknown `lf_*` attributes are not rejected, but they change the graph's
  semantic digest.

**Side effect legend:** 🤖 runs an agent · 🔀 writes git / pushes · 💬 posts on the
ticket · ⏸ asks the owner and waits · 🗄 store only · ▶ runs local commands ·
📌 creates, merges or closes on the forge · – none

## Generic activities

Registered for every mode by `WorkflowSpecs` (`internal/turn/research_admission.go:25`).
Implement and Validate reject `lf_if`, `lf_loop`, `lf_comment` and `lf_ask` in their
graph checks. Bug registers only `lf_agent` and its own `lf_loop` variant.

| Activity | What it does | Outcomes | Attributes | Effects | Used in |
|---|---|---|---|---|---|
| `lf_agent` | Runs one agent role and keeps its validated result. Publishes nothing itself. | `success, failed` (Bug: `success, failed, ask, done, approved, succeeded, reshape, blocked`) | `lf_agent_markdown` (role), optional `lf_skills`, `lf_model_tier`, `lf_task`, `lf_access`, `lf_budget`, `lf_response`, `lf_park`, `lf_stop` | 🤖 (🔀 in the worktree with write access) | all modes |
| `lf_if` | Compares the retained response of an agent with a value. | `true, false` | `lf_field` (=response), `lf_condition`, optional `lf_source` | – | research, plan, spike |
| `lf_loop` | Like `lf_if`, but bounded; uses `lf_default` before the first attempt. | `true, false` | `lf_field`, `lf_condition`, `lf_limit`, optional `lf_default` | – | research, plan, spike |
| `lf_loop` (Bug owner conversation) | Publishes the Bug analysis and waits; returns `approved` only after a recorded, exact owner approval. | `analyse, approved, blocked` | `lf_contract` (=owner_conversation), optional `lf_park`, `lf_stop` | 💬 ⏸ | bug |
| `lf_comment` | Publishes the retained result as a ticket comment. | `published` | `lf_kind` (final \| failure), optional `lf_source` | 💬 | research, plan, spike |
| `lf_ask` | Publishes a question or review and rests at the owner wait. | `published` | `lf_kind` (question \| review), `lf_source` (required in Plan), optional `lf_rights` | 💬 ⏸ | research, plan, spike |
| `lf_gate` | Publishes the reviewed draft with the owner command named on the node and rests at the owner wait. Replaces the `lf_if approved` and `lf_ask review` pair after a story effect; not allowed after the set stage. | `published` | `lf_command` (`/approve-plan`), `lf_args` (`story,stage,candidate`), `lf_source` (required) | 💬 ⏸ | plan |
| `lf_sync_base` | Merges the base branch into the working branch in a scratch worktree. | `synced, current, conflict, retry` | – | 🔀 | implement |

## Research

| Activity | What it does | Outcomes | Attributes | Effects | Used in |
|---|---|---|---|---|---|
| `lf_research_brief` | Writes `RESEARCH_BRIEF.md` and the technical document from an agent's result. Publication needs a successful brief first. | `success, failed` | `lf_source` (an agent node) | files in the session directory | research |

## Plan

| Activity | What it does | Outcomes | Attributes | Effects | Used in |
|---|---|---|---|---|---|
| `lf_plan_story` | Applies the agent's story draft (create, update, retire) to the parent's story sub-issues. | `applied, conflict, failed, uncertain` | `lf_source` (an agent node) | 💬 📌 (issues, sub-issues, comments) | plan |
| `lf_plan_approve` | Plan only. Approves a story, technical or acceptance stage that has no gate, on behalf of the graph, when the applied draft is still current and every guard holds at the moment of saving. Records the approval as given by the graph. Not allowed after the set stage. | `approved, review, superseded, blocked, failed` | `lf_source` (an agent node) | 📌 (saved Plan state) | plan |

## Implement

The coordinator path (parent issue) and the worker path (one story) share one graph;
`lf_implement_select` is the start node and splits them.

| Activity | Path | What it does | Outcomes | Attributes | Effects |
|---|---|---|---|---|---|
| `lf_implement_select` | start | Sends the parent down the coordinator path and a story down the worker path. | `coordinator, worker` | – | – |
| `lf_implement_ready` | coordinator | Starts up to (cap − running) stories whose prerequisites are merged, in plan order. | `started, waiting, drained` | – | 🗄 |
| `lf_implement_merge` | coordinator | Merges the oldest finished story into the integration branch; a conflict sends the story back to waiting. | `merged, parked, none, retry` | – | 🔀 💬 |
| `lf_implement_handoff` | coordinator | Writes and updates the one hand-off report on the parent (merged, parked with reasons, waiting). | `posted` | – | 💬 |
| `lf_implement_await` | coordinator | Rest point while stories build; must be the node named `await`. | `go` | – | – |
| `lf_implement_any_parked` | coordinator | Answers whether any story is parked. | `true, false` | – | – |
| `lf_implement_ask` | coordinator | Posts one batched owner question listing every parked story. | `published` | `lf_source` (the merge node, where the reply resumes) | 💬 ⏸ |
| `lf_implement_report_done` | coordinator | Posts the completion reply once every story is merged. | `published` | `lf_source` (the merge node) | 💬 |
| `lf_implement_resolved` | coordinator | Reads the sync resolver's answer: commits and pushes the merge, or asks the owner with the conflicting files. | `resolved, blocked, retry` | `lf_source` (the resolver agent) | 🔀 💬 ⏸ |
| `lf_implement_budget` | worker | Decides whether a story gets another attempt (first attempt plus two repairs). | `run, park` | – | 🗄 |
| `lf_implement_result` | worker | Reads the worker's accepted result; `incomplete` counts as a failed attempt. | `completed, incomplete, blocked` (`stopped` only in old releases) | `lf_source` (the worker agent) | 🗄 |
| `lf_implement_story_done` | worker | Commits and pushes the story branch, marks the story done and wakes the parent. | `done, retry` | – | 🔀 🗄 |
| `lf_implement_park` | worker | Parks the story with a plain reason and wakes the parent. | `parked` | – | 🗄 |

Shared activities in Implement: `lf_agent` (story worker, sync resolver) and
`lf_sync_base` (`sync_start`, `sync_done`).

## Validate

A Validate graph contains exactly one node of each of these activities (plus one
`lf_agent` for the repair), and at most one `lf_validate_implement`.

| Activity | What it does | Outcomes | Attributes | Effects |
|---|---|---|---|---|
| `lf_validate_suite` | Runs the project's full test phase on one exact commit; reuses a finished result for the same commit. Start node. | `passed, failed, blocked, retry` | – | ▶ 🗄 |
| `lf_validate_repair_budget` | Starts a repair round (cleaning up one a restart left behind) or reports the bound as spent. | `run, spent` | `lf_rounds` (1 \| 2), `lf_suite` | ▶ (git reset) 🗄 |
| `lf_validate_repair_check` | Measures the real repair diff: keeps a small change as a commit, otherwise resets. | `kept, too_big, handback, unfinished, blocked` | `lf_max_files` (1–5), `lf_forbid`, `lf_source`, `lf_suite` | 🔀 (local) 🗄 |
| `lf_validate_review` | Runs the three read-only reviewers (acceptance, code, architecture) on the tested commit. | `done, retry` | `lf_roles`, `lf_suite`, optional `lf_skills` | 🤖 🗄 |
| `lf_validate_outcome` | Joins all findings and records the result. | `assessed, handed_back, blocked` | `lf_suite` | 🗄 |
| `lf_validate_resolve` | Applies the owner's reply (verdict, choice, confirmation) and names the next step. | `wait, create_issue, push, merge, close, stale` (+ optional `repair`, `implement`) | `lf_suite` | 🗄 |
| `lf_validate_report` | Keeps the one report comment current. | `done` | – | 💬 |
| `lf_validate_ask` | Publishes the owner wait. | `published` | `lf_source` (the resolve node) | 💬 ⏸ |
| `lf_validate_issue` | Creates (or recovers) a follow-up issue for each finding the owner sent to an issue. | `done` | – | 📌 |
| `lf_validate_push` | Pushes the tested commit and finds or opens the PR. | `done` | – | 🔀 📌 |
| `lf_validate_merge` | Merges the PR at the confirmed head. | `done` | – | 📌 |
| `lf_validate_close` | Closes the parent and the accepted story issues; Validate only succeeds here. | `closed, open` | – | 📌 |
| `lf_validate_implement` | Sends blocking findings back: fast-forwards story branches, reopens stories with the findings as guidance and schedules `/implement` once. | `started` | `lf_suite` | 🔀 🗄 (schedules a run) |

## Bug

The Bug graph registers only `lf_agent`, the owner-conversation `lf_loop` (above)
and its delivery step.

| Activity | What it does | Outcomes | Attributes | Effects |
|---|---|---|---|---|
| `lf_bug_finalize` | Delivers the accepted repair: commit, push, find or open the PR, merge, close the issue. Safe to rerun. | `completed, revalidate, stale, blocked` | `lf_owner_continuation` (=required), optional `lf_park`, `lf_stop` | 🔀 📌 |

## Spike

All four Spike steps read an agent's result (`lf_source`). For `lf_spike_check`
and `lf_spike_issue`, `failed` is a finding, not a fault.

| Activity | What it does | Outcomes | Attributes | Effects |
|---|---|---|---|---|
| `lf_spike_note` | Writes `SPIKE.md` with the status the node declares and records it. | `success, failed` | `lf_status` | files, 🗄 |
| `lf_spike_gate` | Classifies the investigator's proposal against the limits; saves HEAD as the baseline when small. | `none, small, too_big, blocked` | `lf_max_files` (1–5), `lf_forbid` | 🗄 |
| `lf_spike_check` | Measures the fixer's real change and, when small, runs the named recipe phases. | `passed, failed, too_big, blocked` | `lf_max_files`, `lf_forbid`, `lf_phases` | ▶ 🗄 |
| `lf_spike_issue` | Files one plain issue for a change that is too big. | `filed, failed` | – | 📌 |

## Node, edge and graph attributes

| Attribute | Where | Meaning | Values |
|---|---|---|---|
| `lf_action` | node | The activity the node runs. | a registered activity |
| `lf_outcomes` | node | Outcomes the node handles. | comma list, subset of the activity's outcomes; one edge each (none for owner-stop outcomes) |
| `lf_limit` | node | How often the node may run. | positive integer; required on cycles, `lf_loop`, Bug and Plan nodes; Implement ≤ 256, Validate ≤ 64 |
| `lf_terminal` | node | End node: no activity, no outcomes, no outgoing edges. | boolean |
| `lf_on` (alias `on`) | edge | The outcome the edge follows. | one declared outcome |
| `lf_source` | node | Node whose result this step reads, or where an owner reply resumes. | node name |
| `lf_agent_markdown` | agent | Role file of the agent. | kebab-case name, ≤ 64 chars, no `.md` |
| `lf_skills` | agent, reviewers | Skills given to the role(s). | comma list, optional `shared/` prefix |
| `lf_model_tier` | agent | Model tier. | `flagship`, `balanced`, `fast` (node, then role frontmatter, then graph default) |
| `lf_task` | agent | Task text. | 1–65536 bytes (node, then role, then graph default) |
| `lf_access` | agent | Tool access. | `read`, `write` (Validate repair: write; Bug analyst: read) |
| `lf_budget` | agent | Time budget per run in minutes. | 1–1440 |
| `lf_response` | agent | Routes the agent's retained result as the outcome. | `result` (only with `lf_park=owner`) |
| `lf_park` | Bug nodes | Parks at the owner instead of failing. | `owner` |
| `lf_stop` | Bug nodes | Outcomes that park at the owner (no edge). | from blocked, failed, invalid, exhausted, stale (first four required) |
| `lf_contract` | Bug `lf_loop` | Marks the owner conversation. | `owner_conversation` |
| `lf_owner_continuation` | `lf_bug_finalize` | Marks the delivery step. | `required` |
| `lf_field` | `lf_if`, `lf_loop` | Field to compare. | `response` |
| `lf_condition` | `lf_if`, `lf_loop` | Value to compare with. | research: ask, done, approved, continue · plan: set, story, technical, acceptance, create, ask, answer, approved · spike: ask, done |
| `lf_default` | `lf_loop` | Value before the first attempt. | research/spike: ask, done · plan: set, story, technical, acceptance |
| `lf_kind` | `lf_comment`, `lf_ask` | Publication kind. | comment: final, failure · ask: question, review |
| `lf_rights` | `lf_ask` | Human actions that may be delegated. | answer, request_changes, approve (not in Implement; unused in shipped graphs) |
| `lf_command` | `lf_gate` | The owner command the gate posts and waits for. Plan only. | `/approve-plan` |
| `lf_args` | `lf_gate` | Ordered parameters of that command, filled from saved Plan state. Plan only. | `story,stage,candidate` (exactly this order) |
| `lf_suite` | Validate nodes | Names the suite node. | node with `lf_validate_suite` |
| `lf_rounds` | repair budget | Repair-round bound. | 1, 2 |
| `lf_max_files` | Validate repair check, Spike gate/check | File limit for a change. | 1–5 |
| `lf_forbid` | Validate repair check, Spike gate/check | Kinds of change not allowed. | package, dependency, schema, api (+ tests, criteria in Validate) |
| `lf_roles` | `lf_validate_review` | Reviewer roles. | exactly `validate-acceptance,validate-code,validate-architecture` |
| `lf_status` | `lf_spike_note` | Spike status to record. | asking, answered, hand_back, blocked, fix_proposed, fixed, fix_unverified, fix_too_big |
| `lf_phases` | `lf_spike_check` | Recipe phases to run. | regression, focused, full |
| `team`, `mode`/`workflow`, `profile_version`, `start` | graph | Graph identity, version and entry node. | Bug uses `workflow="bug"`, the others `mode=` |

## Where the registry lives

- Naming rule and registration: `internal/graphexec/registry.go` (`validWorkflowActionName`, `Register`).
- Shipped workflow policies: `internal/turn/workflow_registry.go` (`RegisteredWorkflowPolicies`).
- Per mode: `ResearchSpecs` (`internal/turn/research_admission.go`), `PlanSpecs`
  (`internal/turn/plan_graph.go`), `ImplementSpecs` (`internal/graphexec/implement.go`),
  `ValidateSpecs` (`internal/graphexec/validate_flow.go`), `SpikeSpecs`
  (`internal/turn/spike_workflow.go`), `SimplifiedBugSpecs` (`internal/turn/bug_conversation.go`).
