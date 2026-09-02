# ST-6 — Run detail, per-agent tabs, follow

> As the operator, I want a run's detail with its log split into per-agent
> tabs, and a periodically self-refreshing "follow" mode, so I can see how
> each sub-agent is progressing and watch a live run without reloading by
> hand.

Builds `GET /runs/{runID}`, including the per-agent tab split of the run's
event log and the `?follow=1` self-refresh. The largest and riskiest story
in the plan.

## Why This Matters

A litefactory run isn't one agent working alone — it's an orchestrator that
delegates to sub-agents, and today their output is one interleaved stream
that's genuinely hard to follow: whose turn is it, and how far along is
each one? This story is what makes a run's log readable at a glance, by
splitting it into one tab per agent, and lets an operator leave a page open
and watch a run happen live instead of refreshing by hand. What's genuinely
unresolved here, and decided in this plan rather than left to guesswork: the
approved visual mockup's tab-switching mechanism turns out not to survive
contact with a live-refreshing page — clicking a tab would silently snap
back to the first one every five seconds — so this story ships a small,
deliberate change to how tabs are selected (still the same look, a
different click mechanism) to avoid shipping a feature that quietly breaks
itself.

## Architecture Notes

- **Tab selection is server-rendered links (`?agent={key}`), not the
  mockup's CSS-radio pattern** — see the plan brief's shared decision. All
  panels are still rendered in every response; exactly one carries the
  `.active` class from ST-2's `app.css` rules. This is what makes tab
  selection survive the `follow=1` refresh.
- **`internal/runlog` extraction**: move the claude stream-json wire model
  (`Event`/`Message`/`Block`, the eight kind constants, the
  block→kind classification) out of `internal/cli/cmd_logs_render.go`'s
  unexported types into a new stdlib-only leaf package, imported by both
  `cli` and `webui`. `renderSink` and all of its CLI-specific behaviour
  (plain-text output, `--grep`, `--full`, the tool-name map) stays in `cli`
  unchanged — this is a mechanical type swap, not a shared rendering path.
  **`internal/cli/cmd_logs_render_test.go` passing unmodified is the
  acceptance gate for this extraction.**
- Tab grouping: empty `parent_tool_use_id` is the first tab, labelled
  `orchestrator`; each distinct non-empty value is a tab labelled from the
  `Task` `tool_use` block's `input.subagent_type` whose `id` matches, falling
  back to `agent <first 4 chars of the id>` when that block falls outside
  the read window. Tab order is first-appearance order, deterministically —
  do not rely on map iteration.
- `NNNN.meta.json` decode: `webui`'s import allowlist excludes
  `internal/harness`, so declare a minimal local decode struct for the six
  rendered fields, treating every field as optional (a missing one is
  normal, never an error) — the same posture the CLI already takes toward
  claude's stream format. A `_test.go` file may import `harness` to pin the
  JSON tag names against `harness.Meta`; this does not violate the
  non-test-file boundary test from ST-1.
- Log path resolution: `runs.log_path` if set, else
  `naming.RunLogPath(home, naming.ParseRef(session.TaskRef), ordinal)` — never
  built from a request parameter.
- Bounded tail: 4 MiB of `NNNN.jsonl` via ST-2's shared tail reader; each tab
  caps at its own last 500 events with a truncation notice naming
  `litefactory logs <issue> --run <N>`.
- **Highest-severity risk in this story**: run log content is
  attacker-influenced end to end (issue text, tool output, fetched web
  pages, sub-agent names) and renders unredacted by design. No
  `template.HTML`/`template.JS`/`template.CSS`/`template.URL` conversion
  anywhere in this handler or its templates — `html/template`'s contextual
  escaping is the defence, and it only holds if nothing bypasses it.
- `follow=1` renders `<meta http-equiv="refresh" content="5">`; once the run
  is no longer `running`, the control renders `follow: off` so a stale
  follow link stops refreshing on its next load.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-6.1 | Create `internal/runlog` (leaf, stdlib-only): move `Event`/`Message`/`Block` + `ContentBlocks` + kind constants + block→kind classification out of `internal/cli/cmd_logs_render.go`; re-point the CLI at them. `cmd_logs_render_test.go` must pass unmodified. | logic | — | M |
| T-6.2 | Tab model: group by `parent_tool_use_id`; label from the matching `Task` block's `input.subagent_type`, fallback `agent <4 hex>`; first-appearance order; dot teal until the closing `tool_result` appears | logic | T-6.1 | M |
| T-6.3 | Event → HTML row model: reuse the CLI's kind vocabulary, hide `think` by default, cap each tab at its last 500 events with the truncation notice | logic | T-6.1 | M |
| T-6.4 | `GET /runs/{runID}` handler: `GetRun` → `GetSession` → `GetRepo`; log path resolution; 4 MiB tail; meta sidecar read with per-field fallback to the DB row; 404 on unknown ids | api | ST-2's T-2.8, T-6.2, T-6.3 | M |
| T-6.5 | Run template: breadcrumb, run meta strip, tab bar as links + all panels server-rendered, follow control | api | T-6.4, ST-2's T-2.1 | M |
| T-6.6 | Follow: `?follow=1` emits the refresh meta; control renders `follow: off` once the run is not `running`; tab selection survives the refresh via `?agent=` | api | T-6.5 | S |
| T-6.7 | Tests: fixture JSONL with two `parent_tool_use_id` values → three tabs, all three panels present, exactly one visible with JS disabled; naming block outside the read window → `agent <4 hex>`; >500 events → last 500 + notice; no `parent_tool_use_id` at all → single tab, bar still rendered | test | T-6.5 | L |
| T-6.8 | Tests: non-JSON line renders verbatim as `other`; a token-shaped literal renders verbatim and unredacted; log file missing → 200 with `no event log at <path>` and meta strip still rendered; meta file malformed → DB fallback, never `<nil>` | test | T-6.4 | M |
| T-6.9 | Test: `?follow=1` adds the refresh meta and its absence removes it; a finished run renders `follow: off` | test | T-6.6 | S |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-6.1 | `GET /runs/{id}` for a fixture log containing two `parent_tool_use_id` values renders three tabs — `orchestrator` plus both `subagent_type` names — with all three panels present in the HTML and only one visible with JS disabled. |
| AC-6.2 | A run log whose `Task` naming block falls outside the read window renders that tab as `agent <4 hex>` rather than failing. |
| AC-6.3 | A run log with more than 500 events per agent renders the last 500 and the truncation notice naming the `litefactory logs` command. |
| AC-6.4 | `?follow=1` adds `<meta http-equiv="refresh" content="5">`; without it, no refresh meta is present. A finished run renders `follow: off`. |
| AC-6.5 | A run log containing a token-shaped literal renders it verbatim in the UI, unredacted, mirroring the CLI's equivalent behaviour. |
