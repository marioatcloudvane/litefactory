# Research Brief — lf-73-we-need-to-add-more-information-about-a

## Request

[O] From GitHub issue #73 (opened by marioatcloudvane): at the end of a run, litefactory already surfaces cost information (from Claude). The owner wants to promote this to a first-class citizen:

- Capture cost, tokens consumed, tokens per model (input/output, cached), and tokens/cost per mode (research, plan, implement, spike).
- This will require database changes — plain log capture is not enough.
- Add a CLI entrypoint, tentatively `litefactory meter`, with three levels of granularity:
  - `litefactory meter` — metering across everything.
  - `litefactory meter <repository>` — metering for one repository (e.g. total software cost).
  - `litefactory meter <repository> <task-id>` — metering for one feature/task.
- The owner explicitly asks us to evaluate whether **`meter`** is the right verb.
- The owner wants filter flags considered, e.g. `--mode research`, `--ca claude` (coding agent), contingent on what data is actually available.
- The owner explicitly asked for deep research: "This is extremely deep research, use all your power and might and go deep into it."

This is a **Research-mode-only** run. No planning or implementation is in scope here.

## Codebase Context

All findings in this section are `[V]` — verified this run by `codebase-tour` via direct `Read`/`Grep` against the current worktree (`/Users/mariomh/.lf-worktrees/litefactory/task-73`).

### The core gap: cost is captured, tokens are not

- `internal/agent/result.go:76-168` defines `agent.RunResult`, the struct litefactory builds from a coding-agent subprocess's output. It carries `Outcome`, `Model string`, `Tools []string`, `ResultText`, `CostUSD float64`, `HasCost bool`, `NumTurns int`, and timestamps. **It has no token fields of any kind** — no input/output tokens, no cache tokens, no per-model breakdown.
- `HasCost` exists specifically to distinguish "reported zero cost" from Codex's stream, which the code comment says "reports token usage but no monetary amount" (`internal/agent/result.go:120-122`) — i.e. the codebase already anticipated a coding agent that reports tokens without cost, but nothing consumes that signal today.
- `internal/harness/meta.go:41-124` — `harness.Meta`, the schema written to each run's `NNNN.meta.json` artefact (spec ID S-3). It mirrors `RunResult` plus turn-supplied fields (SessionID, Branch, SHAs). `CostUSD *float64` is nullable (lines 110-112). **No token fields here either.**
- **The database schema has no token or model columns.** `internal/store/migrations/0001_init.sql:84-105` (extended by `0003_agent.sql:47`) defines the `runs` table as: `id, session_id, segment_id, ordinal, mode, agent, trigger_comment_id, status, exit_code, error, started_at, finished_at, duration_ms, cost_usd REAL, log_path, commit_sha, result_comment_id`. Confirmed exhaustively via the `runColumns` list in `internal/store/runs.go:606-609`. **Only `cost_usd` persists. There is no `num_turns`, no `model`, and no token columns anywhere in the schema.**
- `store.RunOutcome` / `store.FinishRun` (`internal/store/runs.go:236-253`) — the only cost-shaped field written back after a run finishes is `CostUSD *float64`. `duration_ms` is computed by the store itself from `started_at`/`finished_at`, never trusted from the caller — an existing precedent for "derive, don't trust the subprocess" that a metering feature should probably follow for tokens/cost too, or explicitly decide not to.

### Token data already exists on the wire for at least one coding agent — and is thrown away

- `internal/codex/runner_test.go:22` proves Codex's own raw event stream **does** carry a `usage` object with `input_tokens`/`output_tokens` on its `turn.completed` event.
- `internal/codex/runner.go` has **zero** occurrences of "usage" or "token" (confirmed by grep) — this data is read off the wire during parsing and then dropped before it ever reaches `agent.RunResult`.
- This is very likely the cheapest, lowest-risk first slice of the feature: for Codex specifically, the raw data is already flowing through the process, and the fix is "stop discarding it," not "find a new source of it."
- **Not verified this run**: whether Claude's own raw result-JSON (the actual Claude Code / Claude Agent SDK output, which per the issue already contains a cost figure) is parsed anywhere lower-level than `agent.RunResult` before being narrowed down to `CostUSD`/`Model`/`NumTurns`. `internal/harness/stream.go` and `internal/harness/runner.go` were flagged by the tour as relevant but not read in full this run. **This is the single most load-bearing open question for scoping the feature** — see Open Questions.

### Run/session data model

- Three-level hierarchy: `sessions` (task-level) → `segments` (conversation/session-identity level; carries `agent` + `session_ref` + `resume_token` since `0003_agent.sql`) → `runs` (one row per coding-agent subprocess invocation).
- `mode_selections` (`0004_mode_selections.sql`) is separate GitHub-label routing bookkeeping — not a duplicate or historical log of `runs.mode`.
- **Mode is a real, live, persisted enum today.** `agent.Mode` (`internal/agent/mode.go`) defines `ModeResearch`/`ModePlan`/`ModeImplement`/`ModeSpike` with `.Valid()`/`.Label()`/`ParseModeLabel`. `internal/daemon/tick.go:1394-1517` resolves it per trigger (from a stored `mode_selections` row, a `Mode: <X>` GitHub label, or an LLM classifier) and passes it into `turn.Execute(Request{Mode: mode, ...})`, which flows to `AllocateRunParams.Mode` and lands in the `runs.mode` column. This means **`--mode research`-style filtering on the metering CLI is already fully supported by existing, populated data** — no schema change needed for that dimension.
  - A stale doc-comment was found at `internal/store/runs.go:40-42` claiming "ModeSpike is the only mode M0 runs" — contradicted by the live classifier/label pipeline above. Worth a drive-by fix, not part of this feature's core scope.
- **Coding agent ("--ca") is also a real, live, persisted dimension today**, not aspirational. `store.AgentClaude`/`store.AgentCodex` constants exist (`internal/store/runs.go:44-57`), and `internal/daemon/runners.go:46-50` constructs and dispatches `codex.New(...)` in production wiring; `internal/daemon/tick.go:1457-1467` resolves the agent choice per run from a GitHub label, defaulting to `AgentClaude`. So `--ca claude` / `--ca codex` filtering is also already supportable from existing schema, with the caveat that Codex's token data specifically is currently discarded (see above) even though its cost is presumably captured the same way Claude's is.
  - A second stale doc-comment was found at `internal/store/runs.go:51-56` ("nothing in this tree writes a … runs.agent row with [AgentCodex] yet") — also contradicted by the same live dispatch code. Both stale comments materially undersell the codebase's actual state to anyone reading it as ground truth; flagging so Plan mode doesn't design around a wrong assumption.

### One run = one top-level subprocess; sub-agent cost is invisible

- One `runs` row corresponds to one top-level coding-agent subprocess invocation, triggered once per triggering GitHub comment, resolved in `daemon.run` (`internal/daemon/tick.go:1380`).
- Nested Task-tool sub-agent calls (e.g. a research-orchestrator invoking `codebase-tour` and `web-researcher`, as happened in this very run) execute **inside** that one subprocess and are entirely invisible to litefactory's data model today — they roll up into the single parent run's `cost_usd` and whatever final result event Claude reports.
- Practical consequence: **cost/tokens per mode are already naturally separable** (one `runs.mode` per run, already populated). **Cost/tokens per sub-agent within a mode are not observable at all** with current instrumentation, and it is not yet established (see Open Questions) whether Claude's own result JSON even offers that breakdown to draw from.

### Existing reporting path (where cost currently surfaces)

- `internal/report/run.go` is the sole place cost/model reach the GitHub comment. `metadataList` (lines 582-588) emits `Cost: $%.4f` from `res.CostUSD` (only when `>0`) and `Model: <name>` from `res.Model`. No token counts appear anywhere in this file today — confirming the issue's framing that cost is currently "side-information" bolted onto the human-readable comment, not a structured, queryable artefact.
- `resultSection` (lines 704-736) truncates the agent's final message to `maxResultBytes = 32 << 10` (32 KiB) — this is the same truncation constant this orchestrator's own instructions reference for the business-summary ordering rule; it is unrelated to metering but confirms the report renderer is a single, well-understood chokepoint a metering feature would extend, not replace.
- Posting mechanics: `internal/report.Reporter.RenderRun` (pure renderer) → `internal/turn.Reporter` (`internal/turn/report.go:81-90`) posts via a `Poster` interface and, in one DB transaction, records `result_comment_id`, advances the watermark, and moves the run to `reported`. "Post first, then commit" is documented as deliberate at those lines.

### CLI architecture and closest prior art

- `internal/cli` is a **hand-rolled command dispatcher**, not cobra/urfave/cli. `internal/cli/command.go:1-28`'s doc comment states the pattern explicitly: one `*Command` per `cmd_<group>.go` file, one line added to `Commands()` in `commands.go`, no central switch statement. `Command.New func(fs *flag.FlagSet) Handler` binds flags per invocation using stdlib `flag`. A new `meter` command should follow this exact convention: a new `cmd_meter.go` plus one registration line.
- **`internal/cli/cmd_logs.go` is the closest existing analog to `litefactory meter`.** It already reads the DB (`store.DB`) and `NNNN.meta.json` directly, works with the daemon stopped (no IPC — spec ID AC-5.8), and resolves conflicts by letting the DB row win over the file where both speak (`writeRunSummary`, lines 439-446).
- `cmd_logs.go`'s `logsCost` helper (lines 924-931) **already exists** and formats `$%.4f` from `run.CostUSD`/`meta.CostUSD` — this is directly reusable/extensible for a `meter` command rather than something to build from scratch.
- The existing `--repo` / task-ref parsing convention (`parseTaskArg`, `parseRepoFlag`, `cmd_logs.go` lines 828-862) is the established pattern a `litefactory meter [repository] [task-id]` command should reuse for its own positional/flag parsing, for consistency with the rest of the CLI surface.

### Prior art inside this repo, not read this run (session-boundary respected)

- `codebase-tour` surfaced, by path only, a prior spike specifically on this topic: `.aiteam/lf-parallelization/2026-09-01/token-improvements/SPIKE.md`. Per this run's session-boundary rules, that path was **not** read (it belongs to a different session). **Plan mode should read this file directly** — it may already contain design thinking, false starts, or constraints relevant to this exact feature, and skipping it would risk Plan mode re-deriving ground already covered.

### Redaction boundary (relevant if metering output is ever forge-facing)

- `[V]` Outbound text to GitHub is routed through `internal/redact` as a strict boundary (spec ID S-7); `internal/cli/cmd_logs.go:151-155` has a named test asserting `logs` never imports `redact`, since local/CLI output is deliberately unredacted. If any part of a `meter` command's output were ever posted back to a GitHub comment (e.g. a periodic cost summary), it would need to go through the same redaction path that `report`/`turn` already use — local CLI output does not.

## Domain / Problem Space

**This section is materially incomplete.** `web-researcher` was invoked with seven specific factual questions about what the Claude Code CLI headless JSON output and the Claude Agent SDK actually expose (per-session `total_cost_usd`, a `usage` object with input/output/cache tokens, whether a `modelUsage`-style per-model breakdown exists, whether Task-tool sub-agent usage is separately broken out anywhere in the wire format, cache token pricing stability, streaming vs. final-only cost reporting, and prior art for metering-database schemas built on top of Claude Code sessions).

`web-researcher` reported it currently has **no web access** on this run and returned no findings: *"No web access on this run; the issue needs the `lf:tools=web` label to enable WebSearch/WebFetch."* Per this team's standing instruction, these questions are **not** answered from the orchestrator's own general knowledge — they are carried forward below as open questions requiring a re-run with web access, rather than smoothed over with an unverified answer that Plan mode might build a schema on top of.

What we do know, from the codebase itself rather than from web research: the issue's premise ("we already get cost information from Claude") is confirmed true and located precisely (`internal/report/run.go:582-588`, `res.CostUSD`) — so at minimum a per-model cost figure is already parsed out of whatever Claude Code returns per run somewhere in the pipeline. What is **not** established without web access is: the exact shape of that source data (field names, nesting, per-model/per-cache breakdown), whether litefactory is even reading the richest form of it, and whether a static pricing table would be needed to recompute cost independently for cases (like Codex, per the codebase findings above) where tokens are reported without a dollar figure attached.

## Constraints & Risks

- **`[V]` Schema change is unavoidable, as the issue owner anticipated.** The `runs` table has no columns for tokens, per-model breakdown, or even `num_turns` (which `agent.RunResult` already computes in memory but never persists). Any metering feature beyond "cost per run, already exists" requires new migrated columns at minimum, and quite possibly a normalized child table if per-model breakdown within a single run needs to be queryable (a single run can, per Anthropic's typical session model, involve more than one model — e.g. an orchestrator model plus differently-configured sub-agents) — this shape question depends on the still-open web-research questions above.
- **`[V]` Sub-agent-level cost attribution is not currently observable at all**, and it is not yet known (pending web research) whether Claude's underlying result format even supports that level of breakdown. If it does not, "tokens/cost per mode" (which the codebase already supports today) may be the practical ceiling of granularity in the near term, not "tokens/cost per sub-agent."
- **`[V]` Two coding agents exist in production (Claude, Codex) with asymmetric data availability.** Codex already emits token usage on the wire but litefactory discards it; Claude's underlying wire format is not yet confirmed. A metering feature that assumes uniform data availability across coding agents risks building a schema/CLI that one agent can't actually populate.
- **`[V]` Two stale doc-comments in `internal/store/runs.go` (lines 40-42 and 51-56) understate what the codebase already does** (multi-mode dispatch, live Codex dispatch). Low risk in itself, but a reason Plan mode should verify current behavior against live dispatch code (`daemon/tick.go`, `daemon/runners.go`) rather than trusting nearby comments.
- **`[O]` The owner explicitly wants us to question the verb `meter`.** No alternative was evaluated this run beyond noting the request — this is a naming/UX decision better suited to Plan mode's requirements conversation with the owner than something Research mode should pre-decide. Candidate alternatives worth raising in Plan mode: `cost`, `usage`, `spend`, `bill(ing)` — `meter` itself is defensible (utility-metering connotation fits "measuring consumption after the fact") but is unconventional as a CLI verb compared to more common `cost`/`usage` naming in adjacent tools; this is a judgment call for the requirements conversation, not a fact this run can settle.
- **`[V]` Redaction boundary applies only if metering output ever becomes forge-facing.** CLI output (local) is currently exempt from redaction by design; if a future iteration posts periodic cost summaries as GitHub comments, that would cross into `report`/`turn`'s redacted path and needs to be an explicit design decision, not an oversight.
- **`[R0]` A prior spike on this exact topic exists** (`.aiteam/lf-parallelization/2026-09-01/token-improvements/SPIKE.md`, a different session, not read this run) and should be treated as required reading for Plan mode before finalizing scope, to avoid re-deriving or contradicting prior thinking.

## Open Questions

These require a follow-up research run with the `lf:tools=web` label enabled (or an owner-supplied answer) before Plan mode can safely lock a schema. None of these were answered from the orchestrator's own memory, per this team's standing rule against smoothing over unresolved web-research questions.

1. **What does Claude Code's headless JSON output (`--output-format json`/`stream-json`) or the Claude Agent SDK actually return per session?** Specifically: is there a `total_cost_usd` field, a `usage` object with `input_tokens`/`output_tokens`/`cache_creation_input_tokens`/`cache_read_input_tokens`, `num_turns`, `duration_ms`, `session_id`, and model name — and since which version?
2. **Does Claude Code/the Agent SDK expose a per-model breakdown within one session** (e.g. a `modelUsage`-style map), relevant when an orchestrator and its sub-agents use different models?
3. **Is Task-tool sub-agent usage separately attributable anywhere in Claude's own output**, or only aggregated at the session/per-model level? This directly determines whether "cost per sub-agent" is ever achievable without Anthropic adding that capability, versus being achievable today if litefactory just isn't reading far enough into the existing wire format.
4. **Are cache-write vs. cache-read token prices stable enough to hardcode**, or does a cost-recomputation feature (for cases like Codex where tokens are reported without a dollar amount) need a config-driven, updatable pricing table?
5. **Is any cost/usage data available incrementally during a run (streaming), or only in the final result message?** Determines whether "live" in-progress metering is feasible or whether metering is inherently a post-run, batch capture.
6. **Does the Claude Agent SDK's typed result schema (TypeScript/Python) match the CLI JSON schema**, or add/omit fields relevant to metering — relevant if litefactory ever moves from shelling out to the CLI to using the SDK directly.
7. **What does Claude's raw result JSON look like as parsed today inside litefactory**, if at all, below `agent.RunResult`? `internal/harness/stream.go` and `internal/harness/runner.go` were flagged as relevant but not read this run — a targeted codebase-tour follow-up (not a web question) could settle whether Claude's fuller data is already being parsed and silently dropped, mirroring the confirmed Codex situation, before assuming new wire-format work is needed at all.
8. **What should the CLI verb be** — is `meter` right, or should the requirements conversation test `cost`/`usage`/`spend` against the owner's mental model?
9. **What does the prior spike (`.aiteam/lf-parallelization/2026-09-01/token-improvements/SPIKE.md`) already conclude or rule out?** Required reading before Plan mode finalizes scope; not read this run due to session-boundary rules.

## Recommended scope for Plan mode

Plan mode should cover:
- A schema design for token/model capture on the `runs` table (or a normalized child table if per-model-within-a-run granularity is confirmed as both available and needed), informed by resolving Open Questions 1-3 first.
- Wiring Claude's (and Codex's, per the confirmed-discarded data at `internal/codex/runner.go`) already-available or newly-parsed token data into `agent.RunResult` → `harness.Meta` → the `runs` table, following the existing `CostUSD *float64` nullable-field convention for "unknown" vs. "zero."
- A new `internal/cli/cmd_meter.go` command following the `cmd_logs.go` pattern exactly (DB-first with meta.json fallback, `--repo`/task-ref parsing via the existing helpers, daemon-stopped/no-IPC precedent), with `[repository]`/`[task-id]` positional scoping and `--mode`/`--ca` filters, both of which are supportable today against existing populated columns without a schema change.
- A decision, made in the Plan-mode requirements conversation with the owner, on the CLI verb (`meter` vs. alternatives) and on how deep sub-agent-level attribution should go given its current infeasibility.
- Reading and reconciling with the prior spike at `.aiteam/lf-parallelization/2026-09-01/token-improvements/SPIKE.md` before finalizing scope.

Plan mode should **not** attempt to resolve Open Questions 1-7 itself from memory — a research re-run with `lf:tools=web` enabled (or explicit owner sign-off to proceed on stated assumptions) should settle the Claude Code/SDK wire-format questions before schema columns are finalized, since getting the shape wrong (e.g. missing a `modelUsage`-style breakdown that already exists) would mean a second migration shortly after the first.
