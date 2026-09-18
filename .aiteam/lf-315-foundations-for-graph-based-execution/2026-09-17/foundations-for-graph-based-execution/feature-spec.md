# Feature Spec: Foundations for graph-based execution (#315, first plan scope)
# Status: APPROVED
# Date: 2026-09-18

## Goal
Land the inert, non-I/O foundation of DOT-graph execution — a pure `internal/graphexec` parser/validator/transition core, durable `graph_*` persistence, a generic activity-dispatch seam in `internal/turn`, and an unwired poller skeleton in `internal/daemon` — so sibling issues #316–#321 can wire real modes onto proven machinery instead of inventing it.

## Scope frame (read first)

| Item | Value |
|---|---|
| Issue | #315, split from #230's nine-parent roadmap |
| Binding owner input this run | (a) drop the "Fable" tier question — stay vendor-neutral; (b) path convention `/teams/<team>/<mode>.dot` and `/teams/<team>/<mode>/<agent>.md`; (c) "draft the first plan scope now" |
| Prior evidence | `$session/RESEARCH_BRIEF.md` runs 0001–0005 (illustrative, non-binding). Every claim relied on below was re-verified against current source; corrections are marked **[corrects Research]** |
| Protected-tree split | **Does not apply.** This scope touches only `go.mod`/`go.sum`, `internal/**` and `internal/store/migrations/**`. No `.claude/**` or `.codex/**` edits, so no Codex/Claude story split is required |
| Code changes to existing files | None expected except `go.mod`/`go.sum`. New migration files self-register (`internal/store/migrate.go:20` `//go:embed migrations/*.sql`, ordered by numeric filename prefix) — verified |
| Nothing is wired | No `Daemon.Tick` call site, no real mode, no real agent invocation. Precedent for shipping inert named capability: `store.AgentCodex` (`internal/store/runs.go:55-61`), `store.SyncFailed` (`runs.go:78-90`) — verified |

## Verified baseline (re-checked this run, not taken from Research)

| Claim | Verdict | Evidence |
|---|---|---|
| Gonum absent from the module graph | Confirmed absent | `go.mod` (4 direct, 9 indirect requires, no gonum); `go.sum` grep: 0 matches |
| Gonum absent from local module cache | Confirmed absent | glob `gonum.org/**/dot` under `~/go/pkg/mod`: no files — **adding it needs module-proxy/network access** |
| Vendor-neutral tier vocabulary | Confirmed, closed set | `internal/config/tier.go:26-38` `Tier` = `flagship\|balanced\|fast`; `Valid()` strict; `UnknownTierError`, "never a silent fallback" |
| Per-role model resolution | Confirmed | `internal/config/tier.go:132-139` `Tiers.ResolveModel(Kind, Tier) (string, bool)`; a miss is **not** resolved to a fallback there — the caller decides. `internal/turn/delivery.go:69-72` calls it per captured agent and turns a miss into an explicit error |
| Path+digest pin-not-copy precedent | Confirmed | `internal/teamsource/snapshot.go:37-45` `Snapshot{Digest, Hashes map[string]string}`; `internal/team/hash.go:13-16` `HashBytes` = hex sha256; `internal/turn/delivery.go:73` records `SpecialistEvidence{SourcePath: captured.Definition.Path, SourceHash: snapshot.Hashes[...]}` — path + hash, never the bytes |
| Capture is already (root, team, mode)-shaped | Confirmed | `teamsource.CaptureRoot(ctx, root, name, mode)` (`snapshot.go:53`), `Snapshot{Team, Mode}` |
| Migration 0047 widened check identity | Confirmed | `0047_submission_check_snapshot_scope.sql`: `PRIMARY KEY(run_id,reference)` → `PRIMARY KEY(run_id,snapshot_ref,reference)`, "check identity is scoped to the snapshot that produced it" |
| Latest migration is 0047 → next is 0048 | Confirmed | `internal/store/migrations/` listing |
| `workflow_version` collision risk | Confirmed real | `workflow_version`/`WorkflowVersion` occurs in 30+ files (`internal/turn/story_progress.go`, `submissions.go`, `validation.go`, …). New naming must avoid it |
| `graph_*` table names free; `internal/graphexec` package name free | Confirmed | no `internal/graph*` package; existing "graph" hits are story-dependency-graph locals only |
| `sessions` / `runs` tables exist for FKs | Confirmed | `0001_init.sql:50,84` |
| daemon owns a reusable generic backoff | **[corrects Research]** No | `internal/daemon/backoff.go:42-45` `backoff` is unexported and specific to forge rate-limit refusals (`failures`, `until`, `MaxBackoff = time.Hour`). Its *discipline* ("hold an instant, never sleep, never cancel in-flight work") transfers; the type does not |
| A reusable cycle-detection helper exists | **[corrects Research/none claimed]** No reusable API, and it would be wrong here | `internal/planbrief/typed_decode.go:133-156` is an unexported DAG check. Workflow profiles **must permit cycles** (bounded refine loops), so acyclicity is not the validation to reuse |
| `docs/` tree exists | **[corrects instructions/APP_CONTEXT]** Deleted on this branch | glob `docs/**`: no files. `.aiteam/APP_CONTEXT.md:64` still links `../docs/decisions/nested-agent-evidence.md` (dangling); `docs/agent-result-contract.md` cited by Research is likewise gone |
| Gonum decoder drops `lf_*` attributes | **Unverified** — see Evidence Obligation EO-1 | Claim originates in #230's source review; not reproducible here without the dependency |

## Decision D1 — model selection stays vendor-neutral (closes the "Fable" question)

| Aspect | Decision |
|---|---|
| Node-level model knob | DOT attribute `lf_model_tier`, value restricted to the closed set `flagship\|balanced\|fast` |
| Validation | `graphexec` validates the attribute against `config.Tier.Valid()` at parse time; an unrecognised value is a parse/validation fault, never a silent default (mirrors `UnknownTierError`'s rule) |
| Resolution | Deferred to execution time via the existing `Tiers.ResolveModel(Kind, Tier)` path (`delivery.go:69`), unchanged |
| Rejected | A literal model slug (`claude-fable-5-1`) escape hatch; a new fourth tier; any per-node vendor model name. Owner-resolved this run |
| Consequence | `InvokeAgentConfig.ModelTier` is typed `config.Tier`, and the "Fable" tension recorded in RESEARCH_BRIEF run 0003 and chunk 10 of run 0004 is **closed, not deferred** |

## Decision D2 — DOT and agent-markdown path convention, reconciled against the tree

Owner convention: `/teams/<team>/<mode>.dot`, `/teams/<team>/<mode>/<agent>.md`.

Observed tree (verified):

| Path | What it is |
|---|---|
| `teams/` | publishing/distribution tree, **not** a catalog root |
| `teams/teams.json` | discovery index; `teams: {"go-team": {stable, releases}}` |
| `teams/source/` | the editable catalog root (`index.json` + `agents/**` + `skills/**`) |
| `teams/releases/<team>/<version>/` | immutable published catalog roots, same internal shape |
| `$LF_HOME/teams` | the installed catalog root real runs read (`teamsource.Resolve` reads `paths.TeamsDir()` only — no repo/embedded fallback) |
| `teams/source/index.json` | `schema_version: 2`; `shared: {…}` + `teams: {"go-team": {research, plan, implement, validate, spike: [roster]}}` |
| `teams/source/agents/go-team/*.md`, `agents/shared/*.md` | agent markdown today: team-namespaced, **not** mode-namespaced; shared agents reused across modes via `{"ref": …}` |

**Reconciliation (stated, not silently picked):** `<team>` is the team id — `go-team` — the key already used by `teams.json`, `index.json`'s `teams` map, `agents/<team>/` and `releases/<team>/`. `source` is **not** a team name; it names a catalog *channel/root* (editable source vs. released vs. installed), an orthogonal axis. `teams/README.md:3` ("`source/` is the editable, vendor-neutral `go-team` catalog") conflates the two only because `go-team` is the sole team today; `index.json` is explicitly multi-team and its agents are already team-namespaced, so reading `source` as a team name breaks at exactly the plural-team case the owner asked to design for.

Therefore the convention is **catalog-root-relative**:

| Concept | Convention | Concrete today (editable tree) |
|---|---|---|
| Workflow DOT | `<catalog-root>/<team>/<mode>.dot` | `teams/source/go-team/plan.dot` |
| Agent markdown | `<catalog-root>/<team>/<mode>/<agent>.md` | `teams/source/go-team/plan/go-architect.md` |
| Installed equivalent | same, under `$LF_HOME/teams` | `$LF_HOME/teams/go-team/plan.dot` |

**What #315 commits to:** the convention above is recorded as the target shape, and `graph_definitions.path` stores exactly such a catalog-root-relative slash path as an opaque string alongside its sha256 digest. **#315 ships no `.dot` file, reads no catalog from disk, and does not restructure `teams/source/agents/**`.** `graphexec.ParseProfile` takes `[]byte`; who reads the file, from which root, is a #316/#317 decision. This keeps the two genuinely-unresolved questions (OQ-1, OQ-2 below) off #315's critical path instead of forcing a guess.

## User Stories

| ID | Story |
|----|-------|
| US-1 | As a litefactory maintainer, I want a pure `internal/graphexec` package that parses a DOT workflow profile into typed nodes/edges/actions, validates it against a registry, and decides the next node from an outcome, so graph semantics are testable with no database, clock, or agent process. |
| US-2 | As a litefactory maintainer, I want durable `graph_*` tables and store accessors recording a pinned definition (path+digest), an instance, per-node attempts with snapshot-scoped identity, outputs, and an append-only transition trace, so a graph run's history survives restart and is inspectable. |
| US-3 | As a litefactory maintainer, I want a named activity registry and a bounded `ExecuteNodeAction` entry point in `internal/turn`, exercised end to end with a fake handler, so a later issue supplies a real `invoke_agent` handler without redesigning dispatch. |
| US-4 | As a litefactory maintainer, I want a daemon-side poller skeleton that selects due graph instances and computes retry/backoff decisions as pure functions, wired to nothing, so time/backoff ownership stays in `internal/daemon` when a later issue calls it. |
| US-5 | As a litefactory maintainer, I want the four layers exercised together against a real migrated SQLite database with a fake activity handler, so the foundation is proven to compose before any real mode depends on it. |

## Data Model

### `internal/graphexec` (pure, in-memory)

| Entity | Field | Type | Required | Constraints |
|--------|-------|------|----------|-------------|
| Profile | Mode | `agent.Mode` | yes | closed vocabulary, `Mode.Valid()` (`internal/agent/mode.go:25-30`) |
| Profile | Team | `string` | yes | team id from the DOT graph attribute; non-empty |
| Profile | ProfileVersion | `int` | yes | ≥ 1; **never** named `workflow_version` |
| Profile | Digest | `string` | yes | hex sha256 of the parsed bytes, computed with `team.HashBytes` semantics |
| Profile | Start | `string` | yes | must name a declared node |
| Profile | Nodes | `[]Node` | yes | ≥ 1; names unique |
| Profile | Edges | `[]Edge` | yes | endpoints must be declared nodes |
| Node | Name | `string` | yes | unique within profile |
| Node | Action | `string` | yes unless Terminal | `lf_<mode>_<capability>` form; must resolve in the registry |
| Node | Outcomes | `[]string` | yes unless Terminal | non-empty, unique, subset of the registry's `ActionSpec.Outcomes` |
| Node | Limit | `int` | no (default 0 = unbounded-forbidden) | > 0 required for any node on a cycle |
| Node | Terminal | `bool` | yes | terminal nodes carry no Action/Outcomes and no outgoing edges |
| Node | Attrs | `map[string]string` | yes (may be empty) | every raw `lf_*` attribute, unfiltered, so future config is a new key not a new syntax |
| Edge | From / To | `string` | yes | declared node names |
| Edge | On | `string` | yes | member of `From`'s Outcomes; `(From, On)` unique |
| ActionSpec | Name | `string` | yes | registry key |
| ActionSpec | Mode | `agent.Mode` | yes | must equal the profile's Mode when used by it |
| ActionSpec | Outcomes | `[]string` | yes | non-empty, closed. **No executable-code field, ever** |
| Registry | entries | `map[string]ActionSpec` | yes | `Register` / `Resolve`; validity is registry-driven, mirroring `turn.RunnerSet` (`internal/turn/execute.go:33-43`) |

### `internal/store` (durable)

| Entity / table | Field / column | Type | Required | Constraints |
|--------|-------|------|----------|-------------|
| GraphDefinition / `graph_definitions` | id | INTEGER PK | yes | |
| | team | TEXT | yes | |
| | mode | TEXT | yes | |
| | profile_version | INTEGER | yes | deliberately not `workflow_version` |
| | path | TEXT | yes | catalog-root-relative slash path per D2; opaque to #315 |
| | digest | TEXT | yes | hex sha256 of the `.dot` bytes at pin time |
| | team_snapshot_digest | TEXT | no | correlates `teamsource.Snapshot.Digest` |
| | created_at | TEXT | yes | |
| | | | | `UNIQUE(team, mode, digest)` |
| GraphInstance / `graph_instances` | id | INTEGER PK | yes | |
| | session_id | INTEGER | yes | `REFERENCES sessions(id)` |
| | definition_id | INTEGER | yes | `REFERENCES graph_definitions(id)` |
| | status | TEXT | yes | `CHECK(status IN ('running','succeeded','failed','cancelled'))` |
| | current_node | TEXT | yes | |
| | next_due_at | TEXT | no | what the daemon poller selects on; NULL = not due |
| | started_at / finished_at | TEXT | yes / no | |
| GraphNodeAttempt / `graph_node_attempts` | id | INTEGER PK | yes | |
| | instance_id | INTEGER | yes | `REFERENCES graph_instances(id)` |
| | node | TEXT | yes | |
| | attempt | INTEGER | yes | 1-based |
| | run_id | INTEGER | no | `REFERENCES runs(id)` — reuses existing run identity, invents none |
| | status | TEXT | yes | `CHECK(status IN ('pending','running','succeeded','failed','cancelled'))` |
| | outcome | TEXT | no | non-NULL only when status = 'succeeded' |
| | started_at / finished_at | TEXT | yes / no | |
| | | | | `UNIQUE(instance_id, node, attempt)` — identity scoped to its own instance from day one, per 0047's lesson |
| GraphOutput / `graph_outputs` | id | INTEGER PK | yes | |
| | node_attempt_id | INTEGER | yes | `REFERENCES graph_node_attempts(id)` |
| | kind | TEXT | yes | closed vocabulary |
| | payload | BLOB | yes | |
| | owner_comment_id | INTEGER | no | correlates an `lf: ask` reply (`internal/turn/ask.go`) to the attempt that asked |
| | created_at | TEXT | yes | |
| GraphTransition / `graph_transitions` | id | INTEGER PK | yes | append-only; never updated or deleted |
| | instance_id | INTEGER | yes | `REFERENCES graph_instances(id)` |
| | from_node / to_node / on_outcome | TEXT | yes | |
| | at | TEXT | yes | |

### Relationships
- `graph_instances` → `graph_definitions` via `definition_id`; → `sessions` via `session_id`
- `graph_node_attempts` → `graph_instances` via `instance_id`; → `runs` via `run_id`
- `graph_outputs` → `graph_node_attempts` via `node_attempt_id`
- `graph_transitions` → `graph_instances` via `instance_id` (separate from attempts so a retried node's earlier transitions survive)
- `store.GraphDefinition` ↔ `graphexec.Profile`: correlated by `(team, mode, digest)` only — the store holds no `graphexec` type and no copy of the DOT bytes

## API / Interface Changes

All Go-internal. No HTTP surface, no new CLI command, no forge interaction.

| Package | Signature (indicative) | Purity | Notes |
|---|---|---|---|
| `internal/graphexec` | `ParseProfile(dot []byte) (*Profile, error)` | pure | no filesystem read; caller supplies bytes |
| | `Validate(p *Profile, reg Registry) error` | pure | aggregated faults, not first-error-wins (matches `team.Decode` / planbrief fault aggregation) |
| | `Decide(p *Profile, node, outcome string) (next string, ok bool)` | pure | deterministic serial transition |
| | `(Registry) Register(ActionSpec) error` / `Resolve(name string) (ActionSpec, bool)` | pure | |
| `internal/store` | `(*DB) CreateGraphDefinition(ctx, GraphDefinition) (GraphDefinition, error)` | SQL | idempotent on `(team, mode, digest)` |
| | `(*DB) CreateGraphInstance`, `GetGraphInstance`, `SetGraphInstanceStatus`, `ListDueGraphInstances(ctx, now, limit)` | SQL | `ListDue…` mirrors `ListBacklogSessions`'s shape |
| | `(*DB) StartGraphNodeAttempt`, `FinishGraphNodeAttempt`, `RecordGraphOutput`, `RecordGraphTransition`, `ListGraphTransitions` | SQL | one transaction each, atomic evidence + intent (as `FinishInterruptedRun` does) |
| `internal/turn` | `type ActivityRequest struct { Team, Mode, Node string; Attrs map[string]string; InstanceID, NodeAttemptID, RunID int64; ResumeRef string }` | — | `ResumeRef` is runtime-only, never a DOT attribute |
| | `type ActivityResult struct { Outcome string; Kind string; Output []byte }` | — | outcome membership checked by the caller, not the handler |
| | `type ActivityHandler interface { Execute(ctx, Options, ActivityRequest) (ActivityResult, error) }` | impure | |
| | `type ActivityRegistry map[string]ActivityHandler` + `Register`/`Resolve` | — | mirrors `RunnerSet`: validity is registry membership, never a switch |
| | `ExecuteNodeAction(ctx, Options, GraphNodeRequest) (GraphNodeResult, error)` | impure | consults `graphexec.Registry`/`Decide`, dispatches one activity, writes attempt/output/transition rows |
| | `type InvokeAgentConfig struct { AgentMarkdownRef string; ModelTier config.Tier }` | — | decoded from `lf_agent_markdown` / `lf_model_tier`; **type only in #315, no handler** |
| `internal/daemon` | `pollDueGraphInstances(ctx, *store.DB, now, limit) ([]store.GraphInstance, error)` | impure, unwired | not called from `Tick` |
| | `nextGraphAttemptAt(attempt int, last time.Time, policy retryPolicy) (time.Time, bool)` | pure | "hold an instant, never sleep, never cancel in-flight work" |

**New dependency-map edges** (for architecture review): `graphexec → agent, config` (both importable leaves); `turn → graphexec`; `daemon → graphexec` (types only, if needed). No package imports `turn` that does not already.

## Business Rules
- `internal/graphexec` imports no `store`, no `daemon`, no clock, no filesystem, no `os` — same purity class as `turn.ParseCommand`.
- Cycles in a profile are **legal and expected** (bounded refine loops). Validation requires every node on a cycle to carry `lf_limit > 0`; it must not reuse or imitate `planbrief`'s DAG/acyclicity check.
- Every reachable path must reach a terminal node; every terminal node is reachable; no node unreachable from `Start`.
- Action names are `lf_<mode>_<capability>`; a node's action must resolve in the registry and its `Mode` must equal the profile's.
- A node's declared outcomes must be a subset of its `ActionSpec.Outcomes`; every declared outcome must have exactly one outgoing edge; every edge's `On` must be a declared outcome.
- No DOT attribute value is ever evaluated, templated, or executed. `Attrs` carries strings only.
- `lf_model_tier` is validated against the closed `config.Tier` set at parse time (D1). No literal model slug is accepted anywhere.
- A definition is pinned by `(path, digest)`; the DOT bytes are never copied into SQLite.
- New table names carry the `graph_` prefix; the version column is `profile_version`. `workflow_version` and `workflow_*` naming are prohibited for this feature — that name already means managed content/submission schema generation in 30+ files.
- Migration is a single new file `0048_graph_execution_foundations.sql`; no existing migration is edited, no existing store file's bytes change.
- Attempt identity is `(instance_id, node, attempt)` from the first migration — not retrofitted (0047 is this repo's paid lesson).
- `graph_transitions` is append-only. A retry never rewrites an earlier transition.
- Nothing in this scope is reachable from `Daemon.Tick`, from `turn.Execute`, or from any CLI command. Inertness is a deliverable, not an omission.
- Mode remains a ceiling: nothing here selects, changes, or chains a mode (`internal/turn/delivery.go:74`; CONC-21).

## Edge Cases & Error Handling
- DOT bytes that parse but silently drop `lf_*` attributes → must fail loudly. A fixture with a standalone graph-attribute statement plus node/edge attributes asserts survival (EO-1).
- Unknown action name → validation fault naming the node and the action; no silent skip.
- Outcome not in `ActionSpec.Outcomes` → validation fault; `Decide` returns `ok=false` and never guesses a next node.
- Outcome with no outgoing edge, or two edges for one outcome → validation fault (non-determinism is refused, not resolved by order).
- Node on a cycle without `lf_limit` → validation fault.
- Unreachable node / unreachable terminal / no terminal at all → validation fault.
- `lf_model_tier="fable"` (or any non-tier string) → validation fault naming the key and the value, listing the three valid tiers.
- Malformed/empty DOT, duplicate node names, edge to an undeclared node → aggregated faults, not first-error abort.
- Re-pinning an identical `(team, mode, digest)` → returns the existing row, not a duplicate or a conflict.
- Same path, different digest → a **new** definition row; instances stay pinned to the digest they started on.
- Attempt N+1 started while attempt N is `running` → refused; a stale attempt may never finish newer work.
- Finishing an attempt that is already terminal → refused as an immutable-evidence violation, not a silent update.
- Recording an outcome outside the node's declared set → refused at the `ExecuteNodeAction` boundary even if a handler returned it.
- Unknown activity name at execution time → reportable failure, never dispatch to a nil handler (the `RunnerSet` rule).
- Handler returns an error → attempt recorded `failed` with a truthful diagnostic; a post-execution fault is not reported as "the agent did not start" (#259's harvest lesson).
- Instance `cancelled` while an attempt is due → poller must not select it; cancellation does not rewind recorded effects.
- `ListDueGraphInstances` with a NULL `next_due_at` → not due; never treated as "due now".
- `ResolveModel` miss → an explicit error at the call site; never a silent default (deferred to whoever builds a real handler).

## Out of Scope
- Shipping any real `.dot` file (production or example) — synthetic inline DOT strings carry all tests. Plausibly #316's `research.dot`.
- Restructuring `teams/source/agents/**` to the mode-namespaced layout of D2 — that is #317 ("new team and workflow setup").
- Reading a `.dot` file from any root (worktree or installed catalog), and deciding which root is authoritative (OQ-2).
- A real `invoke_agent` activity handler, any agent invocation, any `harness`/`codex` call, session resume behavior.
- Wiring anything into `Daemon.Tick`, `daemon/executor.go`, `turn.Execute`, or any CLI command.
- Old/new execution-path compatibility **admission** (#315 scope item 5) beyond naming the precedent to reuse — there is no second live path to admit into yet. Carried as remaining #315 capability debt, not dropped.
- Token/time baseline instrumentation (#315 scope item 6, second half). Same: remaining #315 debt for a later Plan increment.
- Any change to `config.Tier`, `defaultModelByTier`, `AGENT_TIERS.md`, or `.claude/agents/**` frontmatter.
- Any change to existing recovery/continuation code paths (`FinishInterruptedRun`, continuation dispatch) — graph recovery is additive and separate.
- Labels, issue mutation, child-issue creation.

## Acceptance Criteria

Story boundaries (owner_role mapping is Plan's call; boundaries are drawn so `go-implementation-engineer` / `go-unit-test-engineer` / `go-integration-test-engineer` split cleanly):

**S1 — `internal/graphexec` core + Gonum dependency**
- [ ] `go.mod`/`go.sum` require `gonum.org/v1/gonum v0.16.0`; `go build ./...` succeeds; no other dependency is added or upgraded.
- [ ] `graphexec` compiles with imports limited to stdlib, `internal/agent` and `internal/config`; a purity test asserts it imports no `store`, `daemon`, `turn`, `harness`, `codex`, `os` or `time`-clock use.
- [ ] `ParseProfile` returns a `Profile` whose `Nodes`, `Edges`, per-node `Attrs` and graph-level attributes (team, mode, profile_version, start) all round-trip from a synthetic DOT string, including at least one standalone graph-attribute statement and one attribute defined via a `node [...]`/`edge [...]` default statement (EO-1).
- [ ] `Validate` produces an aggregated fault list (not first-error) for: unknown action, outcome outside `ActionSpec.Outcomes`, outcome with zero edges, outcome with two edges, unreachable node, unreachable terminal, missing terminal, node on a cycle without `lf_limit`, duplicate node name, edge to undeclared node, invalid `lf_model_tier`.
- [ ] A profile containing a bounded cycle validates **successfully** — a cycle is not an error.
- [ ] `Decide` is deterministic and total: for every (node, declared outcome) it returns the unique target; for any undeclared outcome it returns `ok=false` and no target.
- [ ] `InvokeAgentConfig` decodes `lf_agent_markdown` and `lf_model_tier` from `Attrs`, rejecting any tier outside `flagship|balanced|fast` with an error naming the key, the value, and the three valid tiers.
- [ ] No `.dot` file is added to the repository.

**S2 — `internal/store` graph persistence**
- [ ] `0048_graph_execution_foundations.sql` creates `graph_definitions`, `graph_instances`, `graph_node_attempts`, `graph_outputs`, `graph_transitions` with the constraints tabled above; no existing migration file is modified.
- [ ] `Migrate` applies it on a fresh database and is idempotent on a second call; `SchemaVersion` reports 48.
- [ ] No column or table in this feature is named `workflow_version` or `workflow_*` (asserted by a schema test reading `sqlite_master`).
- [ ] `CreateGraphDefinition` is idempotent on `(team, mode, digest)`; the same path with a different digest yields a distinct row, and an existing instance still resolves to the digest it pinned.
- [ ] `StartGraphNodeAttempt` refuses a new attempt while a prior attempt for the same `(instance, node)` is `running`, and refuses a duplicate `(instance, node, attempt)`.
- [ ] `FinishGraphNodeAttempt` refuses to modify an already-terminal attempt and refuses a non-NULL outcome on a non-`succeeded` status; each accessor performs its write in one transaction.
- [ ] `RecordGraphTransition` appends; a second run of the same node leaves the earlier transition row byte-identical and both are returned in order by `ListGraphTransitions`.
- [ ] `ListDueGraphInstances` returns only `running` instances with a non-NULL `next_due_at <= now`, bounded by the supplied limit, in deterministic order; `cancelled`/terminal instances are never returned.
- [ ] Attempt rows survive a close/reopen of the database with identical field values.

**S3 — `internal/turn` activity layer**
- [ ] `ActivityRegistry` resolves by name; an unknown name from `ExecuteNodeAction` is a reportable failure, never a nil dispatch (asserted, mirroring the `RunnerSet` rule).
- [ ] With a fake handler returning a declared outcome, `ExecuteNodeAction` writes exactly one attempt row, its output row, one transition row, and advances `graph_instances.current_node` to `Decide`'s target — all observable in the store afterwards.
- [ ] A fake handler returning an outcome outside the node's declared set is refused: the attempt is recorded `failed`, no transition row is written, and `current_node` is unchanged.
- [ ] A fake handler returning an error records `failed` with the handler's diagnostic preserved (not replaced by a startup-failure message).
- [ ] A terminal node sets the instance to `succeeded` and dispatches no activity.
- [ ] `ExecuteNodeAction` contains no clock-derived scheduling and no retry loop; retry/backoff decisions are the daemon's (asserted structurally, e.g. no `time.Sleep`/`time.After` in the graph files of this package).
- [ ] No production call site invokes `ExecuteNodeAction`; a test asserts the only callers are tests.

**S4 — `internal/daemon` poller skeleton**
- [ ] `pollDueGraphInstances` returns the store's due instances, bounded, and performs no dispatch.
- [ ] `nextGraphAttemptAt` is pure: same inputs → same instant; it returns "no further attempt" once the node's `lf_limit` is exhausted; it never sleeps, never blocks, and holds no `context.CancelFunc` (asserted structurally, the `TestBackoffTouchesNoContext` pattern).
- [ ] Backoff grows monotonically per consecutive failed attempt and is capped; the cap is a named constant in this package, not a magic number.
- [ ] A test asserts `Daemon.Tick` does not reference the graph poller in this scope.

**S5 — composition against a real database**
- [ ] Against a real migrated SQLite database and a registry holding one fake handler: parse a synthetic DOT profile, pin a definition (path + computed sha256 digest), create an instance, drive it through `ExecuteNodeAction` to a terminal node, and assert the resulting `graph_*` rows and transition trace exactly match the profile's expected path.
- [ ] A bounded-loop profile reaches its `lf_limit` and stops with a truthful terminal status, and the full retry history remains visible in `graph_transitions`.
- [ ] Re-open the database mid-run and resume from the persisted `current_node` — the run completes with no duplicate attempt row.
- [ ] `go build ./...` and `go test ./...` pass; `make check` passes; no `.claude/**`, `.codex/**` or existing-file edits outside `go.mod`/`go.sum` and new files.

## Evidence obligations (candidate — story authoring carries these, orchestrator checks before freeze)

| ID | Obligation | Proof method | Availability | Version bounds | Missing prerequisite |
|---|---|---|---|---|---|
| EO-1 | Gonum's DOT decoder drops `lf_*` attributes unless explicitly handled | A unit test that parses a synthetic DOT string carrying node, edge, default-statement and standalone graph attributes, asserting each survives into `Profile`. Whether the AST path (`graph/formats/dot`) or a setter-implementing carrier type is used is the implementer's call — the test, not the claim, is the proof | Not provable in Plan: the module is absent from `go.mod`, `go.sum` **and** the local module cache | Assert against the exact pinned version `v0.16.0`; a version bump re-opens the obligation | Module-proxy/network access to fetch `gonum.org/v1/gonum` |
| EO-2 | The dependency can be added at all | `go build ./...` after `go get` | Unknown — see EO-1's cache finding | `v0.16.0` | Network/proxy access in the implementation sandbox. **If unavailable, S1 is blocked at task 1 and every downstream story with it** |
| EO-3 | `Migrate` reaches version 48 and is idempotent | Real SQLite via `store.Open` + `Migrate` in-test | Available offline | — | none |
| EO-4 | Inertness ("nothing is wired") | Structural test asserting no non-test caller of `ExecuteNodeAction` / the poller, plus `Tick` not referencing them | Available offline | — | none |
| EO-5 | Nested-agent evidence discipline for any criterion about coding-agent behavior | `docs/decisions/nested-agent-evidence.md` | **Unavailable — `docs/` was deleted on this branch; `.aiteam/APP_CONTEXT.md:64` still links it** | — | The decision record itself. Mitigated in this scope because no criterion above asserts coding-agent runtime behavior: every one is provable by Go tests. If a later increment adds such a criterion, this obligation becomes live and unsatisfied |
| EO-6 | Tier resolution behavior relied on by D1 | Already verified by reading `internal/config/tier.go` and `internal/turn/delivery.go:69`; `#315` adds no new resolution code | Satisfied | current HEAD | none |

## Assumptions (non-material gaps, decided on the most reasonable reading)

| # | Assumption | Based on |
|---|---|---|
| A-1 | `graphexec` may import `internal/agent` and `internal/config` without breaking layering | `config` is a documented leaf every package may import (`APP_CONTEXT.md:24-47`, `config/purity_test.go`); `agent → naming` only. Flagged for go-architect to confirm the new map edges |
| A-2 | Next migration number is 0048 and file-prefix ordering suffices | `migrations/` listing ends at 0047; `migrate.go:26-32` orders by numeric prefix with no registration code |
| A-3 | `graph_definitions.path` is opaque to #315 (stored, never resolved) | Avoids pre-empting OQ-2; `ParseProfile` takes bytes |
| A-4 | A profile carries `team` as well as `mode` | D2's `/teams/<team>/<mode>.dot` makes team part of the definition's identity; `teamsource.Snapshot{Team, Mode}` already pairs them |
| A-5 | Definition identity is `(team, mode, digest)`, with `path` descriptive | Research proposed `(mode, digest)`; D2 adds the team axis |
| A-6 | `next_due_at` lives on `graph_instances` | The poller needs a selectable column; mirrors `ListBacklogSessions`' "mark and select" shape |
| A-7 | Retry/backoff for graph attempts is new code, not a reuse of `daemon.backoff` | `daemon/backoff.go` is unexported and forge-rate-limit specific — verified, corrects Research |
| A-8 | Cycle validation must permit cycles with `lf_limit`; planbrief's DAG check is not reused | `planbrief/typed_decode.go:133-156` is unexported and enforces acyclicity, the opposite requirement |
| A-9 | Compatibility admission (scope 5) and token/time baselines (scope 6b) are a later #315 increment, not cancelled | Owner asked for the **first** plan scope; both are named as remaining debt above |
| A-10 | `InvokeAgentConfig` ships as a decoded type with no handler | Owner's earlier framing that concrete activities land per-mode in later phases; keeps S3 testable with a fake handler |

## Open questions (material — do not resolve by guessing)

| # | Question | Options | Blocks |
|---|---|---|---|
| OQ-1 | Does `<catalog-root>/<team>/<mode>/<agent>.md` **replace** today's `agents/<team>/<agent>.md` + `agents/shared/<agent>.md`, and if so what happens to mode-shared agents (`requirements-engineer`, `plan-orchestrator`, … are `{"ref": …}`-shared and would be duplicated per mode)? | (a) new layout replaces `agents/**`, with a `shared/` escape hatch retained; (b) new layout is additive and only DOT-referenced agents live there; (c) the markdown half of the convention is #317's to land and `AgentMarkdownRef` stays an opaque string until then | Nothing in this spec's scope — #315 stores/decodes the string only. Blocks #317 and any real `invoke_agent` handler |
| OQ-2 | Which root does a `.dot` path resolve against at pin time — the git worktree (`teams/source/…`) or the installed catalog (`$LF_HOME/teams`, the only root `teamsource.Resolve` reads today)? The two have different failure classes (missing file vs. missing installation) and different digest-stability properties | (a) installed catalog, reusing `CaptureRoot`'s lock/verify discipline; (b) worktree read, new discipline; (c) both, with the pin recording which | Nothing in this spec's scope (deliberately, per D2). Blocks #316/#317 |
| OQ-3 | Should `graph_definitions.team_snapshot_digest` be required (an instance pinned to graph **and** roster together) or optional? | (a) required — stronger reproducibility, but forces #315 to capture a snapshot it otherwise never touches; (b) optional, populated by whoever wires a real mode | Only the NOT NULL choice on one column; spec currently assumes (b) |

## Notes for architecture review
- Confirm the new package-map edges in A-1 and whether `internal/graphexec` is the right name (no collision exists).
- Confirm that a single migration file covering five tables is preferred here over five files (repo precedent is one file per feature, e.g. `0021_engineering_workflow.sql`).
- Confirm the attempt-identity key `(instance_id, node, attempt)` is sufficient, or whether a `snapshot_ref`-style third axis is wanted now rather than at 0047-style retrofit cost later.
