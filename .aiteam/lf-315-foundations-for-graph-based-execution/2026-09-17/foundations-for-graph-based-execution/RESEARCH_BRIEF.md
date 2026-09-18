# Research brief: Foundations for graph-based execution (#315)

Status: complete for this bounded pass. No implementation, dependency install,
Plan, or issue creation. No child issues were created or proposed as committed
deliverables — the six candidate child scopes remain candidates only, per the
issue's explicit instruction.

## Request

Issue #315, opened by @botatcloudvane, comment "research this!" by
@marioatcloudvane (owner). Independent main issue split from #230's approved
nine-parent roadmap; starts in Research, not an approved implementation story.
Goal: establish a small deterministic Go execution engine for editable DOT
workflow graphs, with litefactory owning transitions/persistence/recovery and
agents doing bounded reasoning/engineering. Six candidate child scopes are
listed for later Plan refinement, not to be created now.

## This is not a first look — #230 already did the exploratory research

Issue #315 was mechanically split off issue #230 ("DAG based execution of
Implementation", closed, 30 comments) after the owner approved a nine-parent
roadmap in #230's own thread. #230's Research session
(`.aiteam/lf-230-dag-based-execution-of-implementation/2026-09-13/dag-based-execution-of-implementation/`)
already ran 12 iterative rounds and is **not on this branch** — it lives only
on `origin/lf/230-dag-based-execution-of-implementation`, since #230 was never
implemented/merged (Research-only branches are not merged to `main`). I read it
via `git show origin/lf/230-dag-based-execution-of-implementation:<path>`
without checking it out. That branch/session is prior art for #315, not
current committed source — cite it as such in any Plan.

Key decisions already made and recorded there (revision-pinned, 2026-09-13/17,
by the same research-evidence discipline this run follows):

- **Format: Graphviz DOT**, chosen over SCXML/BPMN/GraphML/Serverless-Workflow
  DSL after a primary-source comparison (run 0004) and the owner's explicit DOT
  preference (run 0005). DOT supports directed cycles; `strict digraph` bans
  parallel edges, not cycles. A litefactory-specific `lf_*` attribute
  vocabulary is what actually defines execution semantics — DOT itself only
  supplies node/edge/attribute syntax.
- **Parser: Gonum v0.16.0** (`graph/formats/dot` for AST, `graph/encoding/dot`
  for decode), BSD-3-Clause, evaluated against `goccy/go-graphviz` (MIT
  wrapper + embedded Graphviz, version-sensitive EPL-1.0/EPL-2.0 on the bundled
  binary — avoid bundling a renderer for this reason) and `emicklei/dot`
  (diagram-writing only, not a loader). **Gonum is not currently a dependency**
  — confirmed absent from `go.mod`/`go.sum` in this worktree; #230 only
  source-reviewed it, never added it. Adding it is new work for #315, not
  already-integrated tooling. The brief flags a real decoder subtlety: Gonum's
  generic decoder only copies attributes onto destination types that implement
  the relevant setter interfaces, and drops standalone graph-attribute
  statements in its `ast.Attr` case — so parsing into a plain graph does not by
  itself preserve `lf_*` metadata; reading the AST directly (or a carefully
  implemented multigraph) was recommended over the generic decoder.
- **Action naming: `lf_<mode>_<capability>`**, e.g. `lf_research_agent`,
  `lf_research_owner_pause`, `lf_research_save_report` — the owner explicitly
  proposed this pattern and it was adopted. An illustrative
  `RESEARCH_WORKFLOW_EXAMPLE.dot` exists on the #230 branch (not on `main`,
  not executable) modeling investigate → synthesize → owner-pause →
  publish/partial → complete/incomplete/stopped, with `lf_limit`-bounded
  refine loops and named terminal states.
- **Licensing**: BSD-3-Clause (Gonum) coexists with an Apache-2.0 core plus a
  separate proprietary commercial wrapper/orchestration service; neither
  license requires disclosing the wrapper's source, and network-service use
  triggers no copyleft obligation under either license (general reading, not a
  legal audit).
- **Temporal**: explicitly deferred, not rejected outright. Rationale:
  Temporal's embedded-SQLite server mode is documented as dev/test-only, not a
  production replacement for litefactory's local daemon+SQLite architecture;
  production Temporal implies an operated service or Temporal Cloud; an
  Activity can still execute more than once, so application-level idempotency/
  reconciliation work does not disappear even with Temporal. Reconsider if
  multi-host workers or durable-timer-at-scale needs become central.
- **Nine-parent roadmap** (not eight, not #315's six candidates in isolation):
  #315 Foundations → #316 Research mode → #317 new team/workflow setup →
  #318 Plan mode → #319 Implement mode → #321 Validate mode → #322 Spike mode
  → #323 retire legacy execution → #324 retire obsolete team setup. All nine
  are open now (verified via `gh issue view`), none carry a `Mode: Research`
  label — #230's own research explicitly recorded that label application was
  attempted and rejected ("MCP tool call requires approval, but approval
  policy is never"), so body text saying "start in Research" does not by
  itself mean these issues are mode-initialized. This #315 run itself is
  running under an explicit `Mode: Research` header, so that gap does not
  block this pass, but Plan/owner should be aware siblings may still lack the
  label.
- Foundations (#315) was deliberately kept as one six-item parent rather than
  split further "unless later planning shows its six scopes are too large."

I did not re-derive any of this from scratch; I verified it is still
accurate against current source below, since #230's citations are pinned to a
2026-09-13/17 revision that predates #259's merge and other recent work.

## Current execution reality #315 would sit alongside or replace

Verified directly in this worktree (branch `lf/315-foundations-for-graph-based-execution`,
current HEAD `ee45930`, i.e. after #259's merge):

| Area | Current observation | Evidence |
|---|---|---|
| Mode dispatch entry | This run's own `CLAUDE.md` header table routes Research/Plan/Implement to named orchestrators, Validate to a read-only reviewer contract, Spike to "nobody — read `docs/workflow/spike.md` and do it here." This is a prompt-header routing table, not a stored graph. | `CLAUDE.md` (session header, this run) |
| Turn/comment dispatch | `internal/turn` "executes one comment": `ParseCommand` classifies, `Execute` does everything after. No clock, no polling, no backoff live here by design. | `internal/turn/doc.go:1-30` |
| Time/scheduling owner | `internal/daemon` "is litefactory's sense of time": tick loop, discovery then dispatch phases, per-repo/per-session round robin, cursor persistence gated on tick completion, safe retirement/shutdown. | `internal/daemon/doc.go:1-40` |
| Runner abstraction | `turn.RunnerSet` maps `agent.Kind → Runner` (Claude via `internal/harness`, Codex via `internal/codex`); adding a third implementation means registering it in the daemon's factory, no switch statement to touch. `internal/turn/submission_runner.go` wraps runner execution with attempt-bound submission-session cleanup, distinguishing startup failure from post-run cleanup fault via `agent.RunResult`. | `internal/turn/execute.go:20-50`, `internal/turn/submission_runner.go:1-40` |
| Mode persistence/selection | `daemon/mode.go` resolves exactly one `Mode: X` label (ambiguous/malformed labels are explicit refusals, not silent defaults); a `retireModeSelections` helper closes trigger-recovery journals without touching the owner's persistent selection, and can finish scheduling debt for a session that is "no longer polled." Canonical mode labels persist through runs/recovery/journal retirement (`internal/store/modes.go`, per APP_CONTEXT). | `internal/daemon/mode.go:1-60`; APP_CONTEXT.md "retain and switch the selected mode" row |
| Permission ceiling / mode-change refusal | `internal/turn/delivery.go:74`: "The parent mode is a ceiling, not an additive permission hint." The bootstrap prompt handed to the coding agent literally says "Do not change mode." This is the exact contract #315 must preserve — "completing a graph does not select the next mode." | `internal/turn/delivery.go:74,106` |
| Question/asking contract | `internal/turn/ask.go` defines a byte-exact, case-sensitive `lf: ask` tag and `questionPermission(mode, command)`, distinct per mode (Research/Plan default on). This is the existing async-question mechanism #315's `owner_pause` action would need to reuse or formalize, not reinvent. | `internal/turn/ask.go:55-80,123-126` |
| Publication/story machinery | `internal/turn/plan_publication.go` validates the batch, retains replayable effects, returns a link-only Story Index that must be saved before readiness. `internal/turn/engineering_progress_test.go` covers dependency release without acceptance/closure, and restart retention — exactly the engineering-vs-acceptance separation a graph-based Plan/Implement/Validate split must not blur. | APP_CONTEXT.md rows "publish a managed Plan", "finish engineering while children stay open" |
| Existing recovery/restart infra | `store.FinishInterruptedRun`, `continuations.go`, `continuation_settlement.go` give atomic evidence/intent, source-run dedup, bounded chain, original prerequisite settlement. `daemon/continuation_dispatch.go` / `turn/continuation_dispatch.go` admit/cancel automatic recovery with saved context and explicit resume. This is real, already-built restart/recovery machinery — #315's scope item 4 ("restart, cancellation and bounded-retry handling") is not starting from zero; it needs to *integrate with or extend* this, not duplicate it. | APP_CONTEXT.md rows "recover an interrupted run", "admit or cancel automatic recovery" |
| Recent recovery work (#259, merged) | #259 ("Scope check identity to snapshots, isolate recovery, report real failures", merged as `ee45930`) fixed: check IDs colliding across snapshots (storage previously keyed by `(run_id, reference)` only), global foreground recovery propagating one run's failure to unrelated runs, and harvest mislabeling post-execution errors as "coding agent did not start." This is directly relevant prior art for #315's scope item 4 (isolation of stale attempts, truthful failure reporting) and should be read before designing node/attempt-level recovery. | git log (`24234dc`, `9308144`); `.aiteam/` session for #259 has been retired from this worktree post-merge (only visible via git log, not `.aiteam/lf-259-*` — that directory no longer exists here) |
| Team/role source ("existing workflow" concept #1) | `internal/teamsource` resolves one centrally installed catalog only (no embedded/static/repo fallback at current revision — this *corrects* an older APP_CONTEXT description that #230's own run 0006 caught and fixed mid-research). `teamsource.CaptureRoot`/`RenderTeam`/`Resolve` and `internal/team`'s `Index`/`Classify`/overrides machinery are the current "workflow" in the loose sense of "which roles/skills a mode gets" — this is roster/permission plumbing, not execution-graph plumbing. Scope item 5's "old/new execution-path admission and compatibility adapter" and the sibling #317 ("new team and workflow setup") would need to coexist with this, not replace it outright — the issue and #230's roadmap are explicit that team/role abstraction is *kept*, only the setup/installation surface changes. | APP_CONTEXT.md rows on `internal/teamsource`, `internal/team`; `internal/teamsource/resolve.go`, `snapshot.go` |
| Managed content/submission versioning ("existing workflow" concept #2 — naming collision risk) | `managed_features.workflow_version` (0/1/2) is an **already-existing, unrelated** versioning concept: it distinguishes legacy story completion (`workflow_version=1`, `internal/store/story_completion.go:16`) from the current engineering/acceptance-separated schema (`workflow_version=2`, e.g. `internal/store/delivery.go:90`, `acceptance.go:82`, `story_progress.go`). `docs/agent-result-contract.md:24,378` also talks about "Workflow **1**" retaining "legacy verified" checks and lists required-reader dispositions for "Plan v1 file input, Implement event input, Validate v1 parsing" as still-outstanding retirement milestones (tied to #274/#296 per that doc). **This is the "workflow-v1 legacy reader" concept referenced in the task brief** — it is a content/submission schema version, not a DOT graph version, but the term "workflow" is already overloaded in this codebase before #315 adds a third meaning (the DOT definition's own version). #230's own brief explicitly flagged this and recommended keeping "workflow-definition version distinct from existing managed workflow/content/submission versions" — confirmed still necessary and, if anything, more urgent now that I can point at the exact SQL columns and doc language that collide. | `internal/store/managed_stories.go:20`, `story_completion.go:16,25`, `delivery.go:90,116`, `acceptance.go:82`, migrations `0021_engineering_workflow.sql`, `0040_application_results.sql`; `docs/agent-result-contract.md:24,378-383` |
| Compatibility-adapter precedent | The existing `workflow_version` 0→1→2 legacy/dual-path pattern (old and new schema coexisting, `story-contract upgrade` CLI previewing by default and requiring `--apply`, per APP_CONTEXT's "Structured story contracts" section) is a concrete, already-shipped precedent for exactly the kind of "old/new execution-path admission, active work stays pinned" pattern #315's scope item 5 describes in the abstract. Plan should look at this as a template, not invent the dual-path mechanics from nothing. | APP_CONTEXT.md "Structured story contracts" section; `turn/story_contract_upgrade.go` |
| Spike | `docs/workflow/spike.md`: "No orchestrator, no prerequisite. The top-level router handles it directly," with an explicit "Do not read `.aiteam/` archives" boundary and a "hand back rather than grow" scope-stop rule. `teams/source/index.json` has an empty Spike roster (confirmed by grep — a match exists for "spike" in that file but Spike's roster is empty per #230's own citation, consistent with Spike needing minimal wiring). This is the smallest existing mode surface and matches sibling #322's minimal scope. | `docs/workflow/spike.md:1-10,40-60` |
| Existing constraint contracts #315 must version, not silently break | `SEC-21 [enforced]`: Validate MUST NOT edit or commission/run fresh checks; separated engineering binds frozen identity/revision/snapshot to factual executed checks without granting acceptance. `CONC-11`: separated Implement releases dependencies on engineering completion, finishes only after passing executed tests and aggregate/save gates; read-only Validate owns acceptance/code/architecture/plan review; "automatic mode chaining ... MUST NOT occur." `CONC-21 [enforced]`: canonical modes persist through runs/recovery; "Managed authorization and frozen scope MUST remain authoritative; agents MAY recommend owner transitions" (not select them). `CONC-23`: Research/Plan permit material async questions by default. These are the exact rules #315's roadmap says must be "versioned... together with code and tests," e.g. moving full-suite testing from Implement to a factory-run Validate action directly touches `CONC-11`/`SEC-21`'s current wording. | `.aiteam/APP_CONSTRAINTS.md` (SEC-21, CONC-11, CONC-21, CONC-23 — verified current text, not #230's older line-number citations) |

## What's genuinely new for #315 vs. what already exists

**Already exists and #315 should integrate with, not duplicate:**
- Runner/submission execution boundary (`turn.RunnerSet`, `submission_runner.go`)
- Restart/interruption recovery primitives (`store.FinishInterruptedRun`, continuation dispatch/settlement), freshly hardened by #259
- Mode persistence, selection, and the "mode is a ceiling, agents cannot select it" contract
- Async owner-question tagging (`lf: ask`) and per-mode question permission
- Team/role roster resolution and delivery snapshotting (`internal/teamsource`, `internal/team`)
- A dual-schema-version compatibility pattern (`workflow_version` 0/1/2, `story-contract upgrade` CLI) as a template for old/new execution-path coexistence
- Engineering-vs-acceptance separation, and read-only Validate's no-fresh-checks rule

**Genuinely new, not present anywhere in the codebase today:**
- Any DOT/graph parsing at all — Gonum is not a dependency; no `.dot` file exists on `main` (only an *illustrative, non-executable* example on the unmerged `lf/230-...` branch)
- Any `lf_<mode>_*` action registry or validated-outcome vocabulary
- Any concept of a "workflow instance/node/attempt" as a durable persisted unit — current persistence is turn/run/attempt-per-comment, not node-per-graph-step
- Any notion of a workflow/role "snapshot pinned to an instance" beyond the existing per-turn delivery snapshot (which pins roster/skills for one turn, not a whole multi-turn graph run)
- Any "old vs. new execution path" admission/compatibility layer at the *execution-engine* level (the existing `workflow_version` pattern is at the *content schema* level, a related but distinct axis)

## Risks and open questions for Plan

1. **Terminology collision.** "Workflow" and "workflow_version" already mean a
   specific, shipped thing (managed-story content/submission schema
   generation) that has nothing to do with DOT graphs. Plan should pick
   unambiguous naming for the new DOT-definition version/digest/profile
   concept (#230's brief suggested "keep them distinct" but did not propose
   actual names) to avoid confusing this with `managed_features.workflow_version`
   or the "Plan v1/Implement event/Validate v1" legacy-reader retirement work
   referenced in `docs/agent-result-contract.md` and tied to #274/#296.
2. **Gonum decoder pitfall is real and unaddressed.** The generic
   `graph/encoding/dot` decoder silently drops attributes that don't hit a
   setter interface and ignores standalone graph-attribute statements. Any
   Plan/implementation must decide up front whether to read the AST directly
   (`graph/formats/dot`) or build a decoder-compatible carrier type — this
   is a correctness question, not a style preference, because a "successful"
   parse that silently drops `lf_*` attributes would look like it worked.
3. **No production DOT file, dependency, or executable profile exists
   anywhere in this repository yet** (confirmed via `go.mod`/`go.sum` grep and
   `git log --all` for `*.dot` — none found on `main` or any merged branch).
   Everything DOT-related lives only as prose/illustration on the unmerged
   `lf/230-...` branch. Plan should not assume any of that is "already built,"
   only "already designed at a sketch level."
4. **Recovery/dispatch integration surface is larger than six bullet points
   suggest.** #259's fixes (snapshot-scoped check identity, isolated recovery,
   truthful harvest) are recent (merged just before this branch forked) and
   directly touch the same `store`/`turn` recovery machinery scope item 4
   proposes extending. Plan should re-read #259's actual diff (not just the
   commit message) before designing node/attempt-level recovery so the new
   graph-execution recovery model does not regress or duplicate that fix.
5. **Sibling main issues (#316–#324) may lack `Mode: Research` labels.**
   #230's own research recorded a rejected label-mutation attempt for all
   nine. This is not blocking for #315 itself (which has a valid `Mode:
   Research` header in this run), but Plan/owner should verify label state on
   siblings before assuming they are independently pollable/dispatchable.
6. **#230's roadmap, not #315's issue body alone, is the fuller scope
   record.** #315's issue body lists six candidate child scopes; #230's
   `RESEARCH_ROADMAP.md` (unmerged branch) has the same six with slightly more
   detail plus explicit dependency notes to the other eight parents. Plan
   should treat the roadmap doc as background, not as a frozen contract —
   both are candidates per the issue's own instruction.

No blocking clarification is needed to end this Research pass: the direction
(DOT + Gonum, `lf_<mode>_*` naming, dual-path compatibility, mode-selection
preserved) was already settled across #230's twelve iterations with the same
owner, and this pass's job — mapping current execution reality against that
settled direction — surfaced integration points and one real correctness
pitfall (Gonum decoder), not a scope ambiguity requiring an owner decision.

## Evidence boundary

Repository citations above refer to this worktree's checked-out state at
branch `lf/315-foundations-for-graph-based-execution`, HEAD `ee45930` (after
#259's merge), inspected 2026-09-17. `#230` citations refer to
`origin/lf/230-dag-based-execution-of-implementation`, read via `git show`
without checkout; that branch is unmerged prior-art, not current source.
Issue #230's full comment thread (30 comments) and issue #315's own thread
were read via `gh issue view --repo marioatcloudvane/litefactory --comments`.
No builds, tests, dependency installs, or agent/LLM executions were run. No
token-cost or latency measurement was performed. Source-byte counts and billed
tokens were not measured, per research-evidence's evidence-measurement
discipline.


## Run 0002: illustrative technical sketch (owner request, not a Plan proposal)

Status: **illustrative only.** Nothing below is a committed Plan, typed
proposal, feature-spec or story decomposition. No code, dependency, SQL file
or migration was added to the repository. Per the issue's own text — "exact
package/API design belongs to Plan" — Plan makes the binding decisions; this
section exists so the owner can see a concrete shape before choosing to start
Plan. Grounded in this worktree's current source (branch
`lf/315-foundations-for-graph-based-execution`) and issue #315's verbatim six
candidate child scopes (fetched via `gh issue view 315`, quoted below where
relevant), not invented from generic Go patterns.

### The issue's own text already answers "where does this code live"

Issue #315's body says, verbatim: "SQL stays in store, time/backoff in
daemon, bounded action execution in turn; exact package/API design belongs to
Plan." That sentence is itself a placement instruction, not just a layering
metaphor — it matches this repo's existing M0 package-boundary discipline,
stated almost word-for-word in the two relevant package docs:

- `internal/turn/doc.go:1-8`: "A turn is the whole business path triggered by
  a single instruction comment... It deliberately knows nothing about
  time... That belongs to internal/daemon."
- `internal/daemon/doc.go:1-8`: "Package daemon is litefactory's sense of
  time. It owns the tick loop... What to do about one comment belongs to
  internal/turn, which has no clock."

So a graph-execution engine that respects this repo's own architecture is not
one single new package doing everything — it is a small new **pure** package
for the graph model itself, plus new files distributed into the three
existing package directories (`store`, `daemon`, `turn`) for the parts that
are SQL, time, or bounded action execution, respectively. This directly
informs the "where we modify code" answer below.

### Proposed new package: `internal/graphexec`

A new, dependency-light leaf package (no SQL import, no clock import — same
purity discipline as `turn.ParseCommand`) holding the DOT/Gonum parsing,
validation and pure transition logic. This is where Gonum v0.16.0
(`graph/formats/dot` for AST, `graph/encoding/dot` only if a
decoder-compatible carrier type is built — see run 0001's flagged pitfall)
would become a new, currently-absent dependency (confirmed again this run:
`grep -in gonum go.mod go.sum` finds nothing).

Candidate types (rough fields/responsibilities, names illustrative):

- **`Profile`** — one parsed, validated, versioned DOT workflow definition:
  `Mode agent.Mode` (reuses the existing `agent.Mode` closed vocabulary from
  `internal/agent/mode.go:9`, not a new mode type), `Version int`, `Digest
  string`, `Nodes []Node`, `Edges []Edge`, `Source []byte` (raw DOT,
  immutable once pinned). Answers scope item 1 ("Versioned DOT profile...
  validation") and scope item 2's "explicit version identities."

  **Revised by run 0003 — see that section.** The owner corrected the
  `Source []byte`/DB-blob framing below; run 0003 replaces it with a
  path+digest identity. This paragraph is left as originally written so the
  correction is visible in context, not silently edited away.
- **`Node`** — one graph vertex: `Name string`, `Action string` (an
  `lf_<mode>_<capability>` action name), `Outcomes []string` (closed per-node
  outcome vocabulary), `Limit int` (the `lf_limit`-bounded refine-loop cap
  from run 0001's `RESEARCH_WORKFLOW_EXAMPLE.dot`), `Terminal bool`.
- **`Edge`** — `From, To string`, `On string` (the outcome/condition that
  fires this transition).
- **`ActionSpec`** — one registry entry: `Name string`, `Mode agent.Mode`,
  `Outcomes []string`. Explicitly excludes any executable-code field — scope
  item 1 states "No arbitrary executable code in graph attributes," so
  `ActionSpec` binds a name to a closed outcome vocabulary and (in a later
  issue) a Go-side handler, never to attribute text that gets evaluated.

  **See run 0003**: this run's "Activity" design is the concrete shape of
  that deferred "Go-side handler," not a replacement for `ActionSpec`.
- **`Registry`** — `map[string]ActionSpec` plus `Register`/`Resolve`. This
  mirrors `turn.RunnerSet`'s existing "validity is registry-driven, not a
  hardcoded switch" pattern almost exactly (`internal/turn/execute.go:33-43`:
  "Adding a third implementation is registering it here... nothing in this
  package... has to change") — the same shape reused for actions instead of
  runners.
- **`ParseProfile(dot []byte) (*Profile, error)`** — reads the Gonum AST
  directly rather than using the generic `graph/encoding/dot` decoder, per
  run 0001's pitfall finding (the generic decoder drops attributes that miss
  a setter interface and ignores standalone graph-attribute statements).
- **`Validate(p *Profile, reg Registry) error`** — closed action/outcome
  vocabulary check against `reg`, terminal-reachability check, bounded-loop
  check.
- **`Decide(p *Profile, node, outcome string) (next string, ok bool)`** — the
  actual "deterministic serial transition" function (scope item 3): pure,
  synchronous, no I/O. What runs an action and when is deliberately not this
  function's job, for the same reason `turn.ParseCommand` is pure and
  `Execute` is "everything after it" (`internal/turn/doc.go:11-12`).

### New files inside the existing `internal/store` package (no edits to existing files)

Durable instance/node/attempt records (scope item 2), shaped like this
repo's existing persistence types (`store.Continuation`,
`store.StoryProgressRun`, `store.Segment` — plain structs, `RunStatus`-style
closed-vocabulary string statuses, `(db *DB) MethodName` transactional
methods):

- **`GraphDefinition`** — the durable row for a pinned `graphexec.Profile`:
  `ID int64`, `Mode string`, `ProfileVersion int` (deliberately **not**
  named `workflow_version` — see naming note below), `Digest string`,
  `Source []byte`, `TeamSnapshotDigest string` (correlates to
  `teamsource`/delivery's existing snapshot digest, so a graph run is pinned
  to both a graph definition and a role roster snapshot together —
  addresses run 0001's "workflow/role snapshots" gap), `CreatedAt time.Time`.

  **Revised by run 0003 — the `Source []byte` field is corrected there per
  the owner's remark 1.** Left unedited here for visible before/after.
- **`GraphInstance`** — one durable run of a pinned definition against one
  session: `ID int64`, `SessionID int64`, `DefinitionID int64`, `Status
  string` (`running|succeeded|failed|cancelled`, same closed-vocabulary
  style as `store.RunStatus`), `CurrentNode string`, `StartedAt, FinishedAt
  time.Time`.
- **`GraphNodeAttempt`** — one node visitation: `ID int64`, `InstanceID
  int64`, `Node string`, `Attempt int`, `RunID int64` (FK to the *existing*
  `runs` table — reuses current run identity rather than inventing a
  parallel one, directly serving scope item 4's "prevent stale attempts from
  completing newer work" via the same snapshot/attempt scoping #259 just
  hardened for `submission_checks`), `Status string`, `Outcome string`,
  `StartedAt, FinishedAt time.Time`.
- **`GraphOutput`** — durable per-attempt output and owner-reply
  correlation: `ID int64`, `NodeAttemptID int64`, `Kind string`, `Payload
  []byte`, `OwnerCommentID int64` (correlates to the existing `lf: ask`
  tagging in `internal/turn/ask.go`, so an owner reply can be matched back to
  the node attempt that asked, rather than a new question mechanism).
- DB methods such as `CreateGraphInstance`, `RecordGraphNodeAttempt`,
  `FinishGraphNodeAttempt` would follow the same one-transaction,
  atomic-evidence discipline as `store.FinishInterruptedRun`
  (`internal/store/continuations.go:53`), which is exactly the discipline
  scope item 4 (restart/cancellation/bounded-retry) needs and which #259 just
  finished hardening for the existing run/continuation model.

**Naming note (resolves run 0001's flagged collision):** `ProfileVersion` (or
similar) is used instead of `workflow_version`, because
`managed_features.workflow_version` already means something else — legacy
vs. engineering/acceptance-separated content schema
(`internal/store/story_completion.go:16`, `internal/store/delivery.go:90`).
Table names use a `graph_` prefix (`graph_definitions`, `graph_instances`,
`graph_node_attempts`, `graph_outputs`) rather than `workflow_*`, for the same
reason.

### New files inside the existing `internal/daemon` package (no edits to existing files)

Time/backoff (scope items 4 and 6): a poller — e.g. finding `GraphInstance`
rows due for their next tick, analogous to `store.DB.ListBacklogSessions`
(`internal/daemon/doc.go:23`) and reusing the same exponential-backoff
mechanism already used for GitHub 403/429 handling
(`internal/daemon/doc.go:166-170`). For #315 itself, this can be delivered as
tested, standalone functions that nothing calls yet (see "is zero
modification achievable" below) — not wired into `Daemon.Tick`.

### New files inside the existing `internal/turn` package (no edits to existing files)

Bounded action execution (scope item 3, and the issue's own explicit
placement: "bounded action execution in turn"): a function such as
`ExecuteNodeAction(ctx, Options, GraphActionRequest) (GraphActionResult,
error)` that reuses the *existing* per-task lock, `RunnerSet`,
`submission_runner.go`'s attempt-bound cleanup wrapping, and
`delivery.go`'s mode-ceiling permission bundle — i.e., one graph node's
action becomes one bounded coding-agent invocation running through the exact
machinery a plain turn already uses, not a second parallel execution path.
This preserves "provider permissions and selected mode" (scope item 3's own
wording) and the existing "mode is a ceiling, not an additive permission
hint" contract (`internal/turn/delivery.go:74`) by construction, since it is
the same delivery path.

**See run 0003**: this is very plausibly where an `ActivityHandler` for
`invoke_agent` would live, since it is exactly the impure, I/O-performing
counterpart `graphexec.ActionSpec` was always meant to defer to.

### Class-diagram-style overview

```
graphexec.Profile "1" *-- "many" graphexec.Node
graphexec.Profile "1" *-- "many" graphexec.Edge
graphexec.Node --> graphexec.ActionSpec        : Action name resolves via Registry
graphexec.Registry "1" o-- "many" graphexec.ActionSpec
graphexec.Profile ..> agent.Mode               : reuses existing closed Mode vocabulary

store.GraphDefinition ..> graphexec.Profile     : durable row pins one parsed+validated Profile (by Digest)
store.GraphInstance --> store.GraphDefinition   : DefinitionID FK
store.GraphInstance "1" *-- "many" store.GraphNodeAttempt
store.GraphNodeAttempt --> store.runs           : RunID FK (reuses existing run/segment identity)
store.GraphNodeAttempt "1" *-- "many" store.GraphOutput
store.GraphOutput ..> turn.AskTag               : OwnerCommentID correlates to existing "lf: ask" replies

turn.ExecuteNodeAction --uses--> turn.RunnerSet, turn.Options, turn.submission_runner
turn.ExecuteNodeAction --writes via store methods--> store.GraphNodeAttempt, store.GraphOutput
turn.ExecuteNodeAction --consults--> graphexec.Registry, graphexec.Decide

daemon.graph poller --reads due rows from--> store.GraphInstance
daemon.graph poller --would invoke (once a later issue wires it)--> turn.ExecuteNodeAction
  (the same relationship daemon/executor.go's NewExecutor already has to
  turn.Execute today — new code following an existing wiring pattern, not
  a new pattern)
```

**Superseded by run 0003's revised diagram** for the `graph_definitions`/DOT
identity portion and the new Activity layer; left here unedited for
before/after visibility.

### DB schema sketch (illustrative new migration, e.g. `0048_graph_execution_foundations.sql`)

Following this repo's existing migration conventions (INTEGER/TEXT/BLOB
columns, `CHECK(... IN (...))` closed vocabularies as in `0021_engineering_
workflow.sql` and `0047_submission_check_snapshot_scope.sql`, `REFERENCES`
to existing tables where a concept is reused rather than duplicated):

```sql
CREATE TABLE graph_definitions (
  id INTEGER PRIMARY KEY,
  mode TEXT NOT NULL,
  profile_version INTEGER NOT NULL,
  digest TEXT NOT NULL,
  source BLOB NOT NULL,
  team_snapshot_digest TEXT,
  created_at TEXT NOT NULL,
  UNIQUE(mode, digest)
);

CREATE TABLE graph_instances (
  id INTEGER PRIMARY KEY,
  session_id INTEGER NOT NULL REFERENCES sessions(id),
  definition_id INTEGER NOT NULL REFERENCES graph_definitions(id),
  status TEXT NOT NULL CHECK(status IN ('running','succeeded','failed','cancelled')),
  current_node TEXT NOT NULL,
  started_at TEXT NOT NULL,
  finished_at TEXT
);

CREATE TABLE graph_node_attempts (
  id INTEGER PRIMARY KEY,
  instance_id INTEGER NOT NULL REFERENCES graph_instances(id),
  node TEXT NOT NULL,
  attempt INTEGER NOT NULL,
  run_id INTEGER REFERENCES runs(id),
  status TEXT NOT NULL CHECK(status IN ('pending','running','succeeded','failed','cancelled')),
  outcome TEXT,
  started_at TEXT NOT NULL,
  finished_at TEXT,
  UNIQUE(instance_id, node, attempt)
);

CREATE TABLE graph_outputs (
  id INTEGER PRIMARY KEY,
  node_attempt_id INTEGER NOT NULL REFERENCES graph_node_attempts(id),
  kind TEXT NOT NULL,
  payload BLOB NOT NULL,
  owner_comment_id INTEGER,
  created_at TEXT NOT NULL
);

-- Trace/verification infrastructure (scope item 6): an append-only edge log,
-- separate from graph_node_attempts so a retried node's earlier transitions
-- remain visible rather than overwritten.
CREATE TABLE graph_transitions (
  id INTEGER PRIMARY KEY,
  instance_id INTEGER NOT NULL REFERENCES graph_instances(id),
  from_node TEXT NOT NULL,
  to_node TEXT NOT NULL,
  on_outcome TEXT NOT NULL,
  at TEXT NOT NULL
);
```

**`graph_definitions.source BLOB NOT NULL` is corrected by run 0003 — see
that section for the revised `path`/`digest` shape and why.** Left here
unedited for before/after visibility; the other three tables are unaffected
by either of the owner's two remarks this run.

Scope item 5's "old/new execution-path admission and compatibility adapter"
is deliberately **not** sketched as new tables here: run 0001 already
identified the existing `managed_features.workflow_version` 0/1/2 dual-path
pattern plus the `story-contract upgrade` CLI as the concrete precedent Plan
should adapt, and reusing that pattern (an admission flag or version gate,
not a new schema) is more consistent with "rollback affects eligible new
admissions, never silently rewinds effects" than a parallel table would be.
Left for Plan to design; noted here only so the owner sees it is addressed,
not silently dropped from this sketch.

### Is "zero existing-code modification" actually true? Direct answer: yes for #315 as scoped, with two caveats worth knowing about

**Yes, achievable, and it has a real precedent already in this codebase.**
Three separate pieces of evidence support this, not just an agreeable
default:

1. **New migration files require no code change to register them.**
   `internal/store/migrate.go:20` embeds the whole directory
   (`//go:embed migrations/*.sql`) and `loadMigrations` orders files purely
   by their numeric filename prefix (`internal/store/migrate.go:26-32`, no
   hardcoded table list). Adding `0048_graph_execution_foundations.sql` is a
   new file, not an edit to `migrate.go` or any other existing file. I also
   checked for a schema-enumeration test that might need updating
   (`grep -n "sqlite_master" internal/store/store_test.go`) and found only a
   parameterized single-table existence check, not an enumerated list — so no
   existing test file needs editing either.
2. **This repo already ships the exact "declare new capability, wire it to
   nothing yet" pattern #315 would need.** `store.AgentCodex`
   (`internal/store/runs.go:55-61`) is a named constant that exists purely so
   future code has something to compare against; its own doc comment says
   "Nothing in this tree writes a segments.agent or runs.agent row with it
   yet." `store.SyncFailed` is described the same way. #315's engine,
   registry, persistence and dispatch primitives can land the same way: real,
   tested, compilable, genuinely inert until a later issue calls them.
3. **New files added inside `internal/store`, `internal/daemon` and
   `internal/turn`'s existing directories are additions, not modifications**,
   in the sense the owner's question is actually asking (no existing file's
   bytes change). Go's per-file compilation model and this repo's existing
   many-small-files convention in those three packages (dozens of files each,
   per the `ls` above) make this a normal, idiomatic way to add a feature
   here, not a workaround.

**Two honest caveats, not hidden:**

- **This is not the same as "one wholly new, untouched-elsewhere package."**
  The issue's own text ("SQL stays in store, time/backoff in daemon, bounded
  action execution in turn") places most of the new code as new files
  *inside* the three existing package directories, alongside dozens of
  existing files there already. If "modify code" is read as "touch these
  directories at all," the honest answer is no — new files land in
  `store/`, `daemon/`, `turn/` and one new `graphexec/` directory. If it is
  read as "change any existing file's contents" (the more natural reading of
  "modify"), the honest answer is yes, zero, achievable.
- **Adding Gonum as a dependency touches `go.mod` and `go.sum`.** That is an
  unavoidable, mechanical two-file diff for any new Go module dependency —
  not a design or logic change, but not literally zero bytes changed either.
  Worth naming explicitly since the owner's phrasing was "we modify code,"
  and a dependency-manifest diff is adjacent to that even though it carries
  no behavioral risk to existing code.
- **Nothing yet calls any of this**, by design — that is what makes zero
  modification possible. The moment a later sibling issue (most plausibly
  #319 Implement mode, per "Later mode parents extend only their required
  actions" in the issue text) wires a real mode into this engine, *that*
  issue will need a small, real edit to an existing dispatch point —
  plausibly `internal/daemon/executor.go` (which already exists solely to
  bind `turn.Execute` for the daemon's loop, `internal/daemon/executor.go:16`)
  or `internal/daemon`'s `Tick`/`dispatch` path. That is honestly out of
  #315's scope, not a gap in this analysis — the issue's own dependency
  section says "None; this is the foundation for the other rollout issues,"
  and the roadmap explicitly sequences wiring into later parents.

### Evidence boundary (run 0002 additions)

Read in this pass, current worktree HEAD (branch
`lf/315-foundations-for-graph-based-execution`, unchanged from run 0001's
`ee45930` — no commits landed between runs): `internal/turn/doc.go`,
`internal/daemon/doc.go`, `internal/turn/execute.go:1-120`,
`internal/daemon/mode.go:1-120`, `internal/turn/ask.go:1-90`,
`internal/turn/delivery.go:55-115`, `internal/store/continuations.go:1-100`,
`internal/store/runs.go:1-80`, `internal/store/segments.go:1-60`,
`internal/store/story_progress.go:1-40`, `internal/store/migrate.go:1-60`,
`internal/store/migrations/0021_engineering_workflow.sql`,
`internal/store/migrations/0047_submission_check_snapshot_scope.sql`,
`internal/teamsource/resolve.go:1-100`, `internal/daemon/executor.go`,
`internal/agent/mode.go`, directory listings of `internal/*` and
`internal/store/migrations/*`, `internal/store/store_test.go` (grep for
`sqlite_master`/`DELETE FROM`), `go.mod` (module path, Go 1.26, no gonum),
`go.sum` (grep for gonum, none found). Issue #315's title and full body
fetched fresh via `gh issue view 315 --repo marioatcloudvane/litefactory
--json title,body`, quoted verbatim above for the six candidate child scopes.
No builds, tests, dependency installs or code/schema changes were made. No
child issues created; no Plan, Implement or Validate work started.

## Run 0003: revising the sketch for the owner's two remarks (DOT-as-file, and Activities)

Status: **illustrative only, same as run 0002.** This section is a follow-up
deepening of the same bounded pass, not a new mode start (Research checkbox
stays checked from run 0001). It directly revises two parts of run 0002's
sketch per an owner comment on issue #315, quoted verbatim in the task
header. Prior sections above are left unedited — corrections are called out
inline where they occur and fully resolved here. No code, dependency, SQL
file or migration was added to the repository this run either.

### Remark 1: the DOT file stays a file — revised identity-pinning approach

**The owner is right, and run 0002's `graph_definitions.source BLOB NOT
NULL` was the wrong shape.** Two independent pieces of current-repo evidence
support pinning by path+digest instead of copying bytes into the database:

1. **This repo just consolidated scattered per-mode instruction/workflow
   files into one git-versioned catalog, away from a loose `docs/` tree —
   the opposite direction from "put it in the database."** `docs/workflow/`
   (which held `spike.md`, `session.md`, `delivery.md`, `followups.md`,
   `owner-exceptions.md`) was deleted from this worktree's history in commit
   `bd0b355` ("Delete docs directory", 23 files, 4,149 deletions, merged into
   this branch via `21e5d2a`). Its content is not gone — the equivalent
   guidance now lives as `teams/source/skills/workflow/SKILL.md`, a single
   git-tracked file inside the same `teams/source/` catalog this repo already
   uses for shipped agent/skill content (`teams/source/agents/**/*.md`,
   `teams/source/skills/**/SKILL.md`). This is directly on point: the repo's
   own recent history shows shipped instruction/workflow content moving
   *into* a git-versioned, catalog-captured file tree, not into SQLite. A
   `.dot` workflow graph is the same kind of artifact — shipped, human-edited,
   diff-reviewable text — and the most consistent placement is inside that
   same `teams/source/` catalog (e.g., illustratively, a new
   `teams/source/workflows/<mode>.dot` per mode, or one file per profile
   version), not a new scattered `docs/`-style location (which this repo is
   actively moving away from) and not `.aiteam/` (that is the *session
   artifact* namespace — `RESEARCH_WORKFLOW_EXAMPLE.dot` living under
   `.aiteam/lf-230-.../` on the unmerged branch was Research's own illustrative
   output, not a proposal for where a production `.dot` file should ship;
   `.aiteam/` archives are explicitly off-limits to read for unrelated work
   per `docs/workflow/spike.md`'s former text and this session's own
   `teams/source/skills/workflow/SKILL.md:25` "Never scan archived sessions").
   Exact placement (a new top-level directory vs. inside `teams/source/`) is
   still a Plan decision — the evidence here supports "a git-versioned file
   participating in the existing catalog/capture discipline," not one exact
   path.

2. **This repo already has a working, tested "pin external file content by
   digest, not by DB copy" mechanism, and it is the team/role snapshot
   machinery the owner named.** `internal/teamsource.CaptureRoot` /
   `CaptureUnlocked` (`internal/teamsource/snapshot.go:52-67`) reads the
   `teams/source/` catalog and produces a `Snapshot` (`snapshot.go:37-45`)
   carrying `Digest string` (a whole-snapshot content hash) and `Hashes
   map[string]string` (a per-file content hash), computed via
   `team.HashBytes` (`internal/team/hash.go:13-16`, a plain hex-encoded
   SHA-256 — "Exported so internal/teamsource can hash an agent's raw source
   bytes... without duplicating this implementation"). That digest, not the
   file bytes, is what survives into durable per-run evidence:
   `agent.DeliveryEvidence.SnapshotDigest` (`internal/agent/delivery.go:30`)
   and, per specialist, `agent.SpecialistEvidence{SourcePath, SourceHash}`
   (`delivery.go:37-44` — path plus hash, explicitly *not* the rendered
   bytes). The type's own doc comment states the intent directly:
   "`DeliveryEvidence` stays in per-run metadata even if an operator later
   removes a retained bundle" (`delivery.go:21-22`) — i.e., this is already
   designed as "pin an identity that outlives and does not require the
   original bytes," exactly what scope item 2's "explicit version identities"
   and "pinned... snapshots" language asks for, and exactly the pattern the
   owner is asking #315 to reuse rather than reinvent.

**Revised `graph_definitions` sketch** (illustrative; corrects run 0002's
version above, not a new table):

```sql
CREATE TABLE graph_definitions (
  id INTEGER PRIMARY KEY,
  mode TEXT NOT NULL,
  profile_version INTEGER NOT NULL,
  path TEXT NOT NULL,               -- repo-relative path to the .dot file, e.g. "teams/source/workflows/research.dot"
  digest TEXT NOT NULL,             -- sha256 of the file's bytes at the moment an instance pinned it (team.HashBytes style)
  team_snapshot_digest TEXT,        -- unchanged from run 0002: correlates to teamsource.Snapshot.Digest
  created_at TEXT NOT NULL,
  UNIQUE(mode, digest)
);
```

`source BLOB NOT NULL` is dropped entirely. `path` is a repo-relative string
(git-versioned, so `git blame`/history already answers "what changed and
when" — the database does not need to be the audit trail for graph-definition
content, the same way it is not the audit trail for `teams/source/agents/**`
content today). `digest` is computed the same way `team.HashBytes` computes
every other content-identity hash in this codebase, so a `graph_instances`
row that references a `graph_definitions` row is pinned to *exact* graph
content (a path alone is not enough — the file at that path can change on
`main` after an instance starts; the digest is what actually answers "which
version of this graph ran," matching scope item 2's requirement) without the
database ever holding a second copy of the file. `Profile.Source []byte` in
the pure `graphexec` package is unaffected by this change — that field is an
in-memory value the parser produces when reading the file at parse time, not
a persisted column; only the *durable row* changes shape.

**One open question this raises for Plan, not resolved here:** unlike
`teams/source/`, which is captured from a separately installed, centrally
managed catalog directory (`paths.TeamsDir()`, outside the git worktree —
see `teamsource.Resolve`, `internal/config.Paths.TeamsDir`), a `.dot`
workflow file most plausibly ships *inside* the git worktree itself (it is
closer to `CLAUDE.md` or `docs/workflow/*.md` used to be — project-local
instruction content — than to the centrally-installed team catalog). If so,
"digest at pin time" needs a read from the *working tree*, not from a
`teamsource`-style captured/locked catalog root, and the existing
`CaptureRoot`/lock discipline may not directly apply as-is — only the
*hash-not-copy identity pattern* transfers cleanly, not necessarily the exact
capture/lock code path. Flagging this rather than picking a resolution,
since it changes where `ParseProfile`/digest-computation code would actually
run from (worktree vs. installed catalog root) and that decision has real
consequences for Gonum-parsing failure modes (a missing/renamed file in the
worktree vs. a missing catalog installation are different failure classes).

### Remark 2: "Activities" — relationship to `ActionSpec`, and a first sketch

**Conclusion: Activity and ActionSpec are two distinct, complementary
layers, not synonyms — and this conclusion follows directly from run 0002's
own text, not from importing an external framework's vocabulary.**

Run 0002 already drew this boundary without naming the second half of it:
`ActionSpec` was deliberately specified as declarative-only — "`ActionSpec`
binds a name to a closed outcome vocabulary and (in a later issue) a Go-side
handler, never to attribute text that gets evaluated" (run 0002, `ActionSpec`
bullet, unedited above). That parenthetical — "a later issue... a Go-side
handler" — is precisely what the owner is now asking #315 itself to name and
shape generically (not implement per-mode; the owner is explicit that
"concrete implementation of activities is done in later phases"). So:

- **`ActionSpec`** (unchanged from run 0002) stays the *declarative* half:
  `Name`, `Mode`, `Outcomes` — lives in the pure `graphexec` package, has no
  I/O, is what `Validate` checks a `Profile`'s nodes against. It answers "is
  this graph well-formed" — never "what actually happens."
- **`Activity`** (new this run) is the *executable* half: a named, typed unit
  of work with its own configuration, invoked when a node's action actually
  runs. It answers "what actually happens" — never "is the graph well-formed."

They are joined by name: a `Node.Action` string (e.g.
`lf_research_agent`) resolves through `graphexec.Registry` to one
`ActionSpec` for *validation*, and — separately — through a new
`ActivityRegistry` to one `ActivityHandler` for *execution*. Keeping these as
two registries rather than one, with `ActionSpec` gaining a `Handler` field,
is not arbitrary: it preserves run 0002's own purity split. `graphexec` was
specified as "no SQL import, no clock import" (run 0002, `internal/graphexec`
section) — a real `invoke_agent` handler necessarily calls into
`internal/turn` (session/runner/delivery machinery, all impure), so binding
it directly onto `graphexec.ActionSpec` would pull I/O into the one package
run 0002 explicitly kept pure. The natural home for `ActivityHandler`
implementations is therefore `internal/turn` (next to the already-sketched
`ExecuteNodeAction`), consulting `graphexec.Registry`/`Decide` for
validation/transition but not living inside `graphexec` itself — the same
pure/impure split run 0002's own class diagram already drew for
`ExecuteNodeAction`, now extended one layer further.

This is **new, generic infrastructure for #315 only.** No mode's actual
`invoke_agent` behavior is being wired in — consistent with run 0001/0002's
finding that concrete wiring belongs to sibling issues (most plausibly #319),
and with the owner's own words this run ("concrete implementation of
activities is done in later phases (for each mode)").

**Illustrative sketch (names illustrative, not binding):**

```go
// internal/graphexec (pure, unchanged package boundary)
type ActionSpec struct {
    Name     string
    Mode     agent.Mode
    Outcomes []string
}

// internal/turn (impure — where real execution already lives)
type ActivityRequest struct {
    Node       string
    Config     map[string]string // raw lf_* attribute values from the DOT node, before typed decoding
    RunID      int64             // existing run identity, reused per run 0002's GraphNodeAttempt.RunID
    ResumeRef  string            // populated only when a prior attempt's session exists; empty means cold start
}

type ActivityResult struct {
    Outcome string // must be a member of the matching ActionSpec.Outcomes — checked by the caller, not by the handler
    Output  []byte // stored via store.GraphOutput, unchanged from run 0002
}

type ActivityHandler interface {
    Execute(ctx context.Context, opts Options, req ActivityRequest) (ActivityResult, error)
}

type ActivityRegistry map[string]ActivityHandler // Register/Resolve, mirrors graphexec.Registry and turn.RunnerSet's existing shape
```

### `invoke_agent`'s config shape, and where it plausibly lives

**Typed Go config, illustrative:**

```go
type InvokeAgentConfig struct {
    AgentMarkdownRef string      // e.g. "research_intro.md" — a source path, resolved the same way captured.Definition.Path already is
    ModelTier        config.Tier // vendor-neutral: reuses the existing flagship/balanced/fast vocabulary, NOT a literal model string — see finding below
    // ResumeRef is deliberately absent from this struct — see "static vs. runtime" below
}
```

**Config lives in both places, split by whether a field is static graph
structure or a runtime value — not an either/or choice:**

- **Static, per-node, git-diffable fields — `AgentMarkdownRef` and
  `ModelTier` — belong on the DOT node as `lf_*` attributes**, e.g.
  `lf_agent_markdown="research_intro.md"`, `lf_model_tier="flagship"`. This
  is consistent with scope item 1's existing `lf_*` vocabulary and its "no
  arbitrary executable code in graph attributes" constraint (attribute
  *values* — plain strings — are exactly what DOT's `key="value"` syntax
  carries; there is no way to put executable code in a DOT attribute value
  short of a handler choosing to `eval` a string, which this design does not
  do). `ParseProfile` would decode these into the typed
  `InvokeAgentConfig` at parse time, the same way `Node.Action` is already
  sketched as being read off a DOT attribute in run 0002.
- **Runtime, per-instance values — a resume session id — must NOT be a DOT
  attribute.** A `.dot` file is one static definition shared by every
  instance/session that runs it (that is the whole point of pinning it by
  digest in the revised `graph_definitions` table above); a session id is a
  fact about one running instance, not about the graph. It is supplied by
  `internal/turn` code at the moment `ActivityHandler.Execute` is actually
  called, read from the same place a resume ref comes from today (see
  finding below) — mirrored here as `ActivityRequest.ResumeRef`, not a
  config struct field.
- The owner's own words — "this might have more configuration in the
  future" — is exactly why `ActivityRequest.Config` above is left as a raw
  `map[string]string` (every `lf_*` attribute on the node, unfiltered) rather
  than only the two fields known today: a future field (e.g. a timeout
  override, an additional tool allowlist) is a new attribute name and a new
  struct field decoded from the same map, not a schema migration or a new
  DOT syntax.

**A real tension worth flagging, not silently resolved:** the owner's
example model name, "Fable," is a real, currently-priced model slug in this
repo (`claude-fable-5-1`, `scripts/seed-meter-prices.sh:82`) — but it is
**not** one of the three values `config.defaultModelByTier`'s Claude column
currently maps to (`opus`/`sonnet`/`haiku`,
`internal/config/tier.go:158-162`). This repo very recently and deliberately
replaced literal per-specialist model names with the vendor-neutral `Tier`
vocabulary specifically so configuration would never again say a literal
model name (`internal/config/tier.go:10-16`: "A specialist's definition used
to say `model: opus` or `model: sonnet`... Tier replaces that with a
vendor-neutral vocabulary"; `.aiteam/AGENT_TIERS.md:30-31`: "No specialist's
actual model assignment changes in this feature: every row below is a pure
relabelling of the `model:` value the file already had"). Choosing
`ModelTier config.Tier` above keeps `invoke_agent` consistent with that
already-completed design; it also means "Fable" specifically is not directly
reachable without one of two Plan-level decisions this run does not make:
(a) extend `defaultModelByTier` so some tier resolves to
`claude-fable-5-1`/`claude-fable-5` (keeping the vendor-neutral vocabulary,
losing per-node granularity finer than three tiers), or (b) give
`InvokeAgentConfig` a literal-model escape hatch bypassing `Tier` entirely
(finer granularity, but reopens exactly the failure mode `Tier` was built to
close). Naming this as a real open question, not picking one.

### What I found about how markdown/prompt selection and model selection actually work today

**(d1) Markdown/prompt selection is not currently a per-invocation Go-side
pointer at all — it is two-layer, roster-driven, and partly resolved by the
coding-agent CLI itself, not by litefactory's own dispatch code.**

1. `teams/source/index.json` maps each mode to an ordered roster of role
   entries (`ref` to a shared entry, or an inline `id`/`path`/`tier`/`skills`
   block) — e.g. `teams.go-team.research: [{"ref": "research-orchestrator"}]`
   (`teams/source/index.json:48-52`). This is the closest existing analogue
   to "which markdown for this step" — but it selects a *roster for a whole
   mode*, not a markdown file for one graph node.
2. `internal/turn/delivery.go:planDelivery` (`delivery.go:37-130`) is where
   markdown selection is actually resolved into bytes: for every roster
   entry captured in the mode's `teamsource.Snapshot`, it takes
   `captured.Source.Body` (the specialist's raw markdown), resolves a model
   via `opts.Tiers.ResolveModel(config.Kind(req.Agent),
   config.Tier(captured.Definition.Tier))` (`delivery.go:69`), and renders
   both into a harness-native role file via `render.Native` (`delivery.go:81`
   — for Claude this produces exactly the kind of file this session's own
   `.claude/agents/research-orchestrator.md` is, complete with a `model:`
   frontmatter line already baked in). A generic `bootstrap.md` is also
   written, telling the top-level agent process which named roles it may
   invoke ("Use only its ordered specialist roster: ...",
   `delivery.go:106`).
3. The actual "which file backs this named role, right now" lookup happens
   inside the coding-agent CLI itself (Claude Code's own subagent/Task-tool
   resolution against the delivered bundle), not inside litefactory's Go
   code — this session's own dispatch (`CLAUDE.md`'s "Research →
   research-orchestrator" table, read and acted on by the top-level agent)
   is a live example of exactly this mechanism.

There is currently **no** concept of "one graph node directly names one
markdown file." Selection today is mode→roster→(orchestrator decides at
runtime which specialist to invoke next), not node→markdown. `invoke_agent`
naming a specific `AgentMarkdownRef` per graph node would be new — a more
direct, node-level pointer than anything wired today, though it would reuse
the same underlying render/delivery mechanics (`captured.Source.Body` →
`render.Native`) rather than inventing new ones.

**(d2) Model selection already exists as a real per-invocation knob — more
granular than the task brief's framing of "fixed or CLI-default" suggested.**
It is resolved **per specialist role**, not merely once per session:
`internal/turn/delivery.go:69` calls `opts.Tiers.ResolveModel(kind, tier)`
once for *every* roster entry in `planDelivery`'s loop
(`delivery.go:67-87`), so two different specialists in the same mode's
roster already can and do run on different models today (e.g.
`go-architect.md` is `flagship`/opus, `go-code-reviewer.md` is
`balanced`/sonnet — `.aiteam/AGENT_TIERS.md:35-44`). The resolution table
itself (`config.Tiers.ModelByTier`,
`internal/config/tier.go:105-139,156-169`) is vendor-neutral by design
(`Tier` is `flagship`/`balanced`/`fast`, never a literal model string), with
a consistency test (referenced in `.aiteam/AGENT_TIERS.md:9-12` as
`TestAgentTiersMatchClaudeAgentsFrontmatter`, T-6.6) keeping the
`.claude/agents/*.md` frontmatter's literal `model:` value honest against
it. Separately, `internal/harness.Options.Model` — "the concrete model
resolved once by startup configuration" (`internal/harness/runner.go:245`)
— is the coarser, session/runner-construction-level knob (set once when a
`turn.RunnerSet` is built, `internal/daemon/runners.go:31-36`). The
finer-grained, already-proven mechanism `invoke_agent`'s `ModelTier` should
plug into is `delivery.go:69`'s per-role resolution, not the coarser
runner-level default.

**(d3) Session-id-for-resume is already first-class, existing capability —
not something #315 needs to invent.** `internal/harness/runner.go` documents
`--resume` semantics in detail (lines ~544-604): a resumed run passes the
same session id captured from a prior run's stream, with explicit,
tested handling for a refused resume (`resumeRefused`,
`runner.go:904-909` — "no init event, so Model is empty... a run that got as
far as an init event was not refused") versus an unusable resumed
conversation that must be rerun cold (`resumeUnusable`, `runner.go:936` and
surrounding doc). `invoke_agent`'s "session id, if present" requirement maps
directly onto this — `ActivityRequest.ResumeRef` above is a new *caller* of
this existing mechanism (a graph node instead of a plain turn), not new
resume logic.

### Revised class-diagram-style overview (supersedes the `graph_definitions`/DOT portion of run 0002's diagram; adds the Activity layer)

```
graphexec.Profile "1" *-- "many" graphexec.Node
graphexec.Profile "1" *-- "many" graphexec.Edge
graphexec.Node --> graphexec.ActionSpec        : Action name resolves via Registry (validation-time, pure)
graphexec.Registry "1" o-- "many" graphexec.ActionSpec
graphexec.Profile ..> agent.Mode               : reuses existing closed Mode vocabulary

turn.Node --> turn.ActivityHandler             : same Action name resolves via ActivityRegistry (execution-time, impure)
turn.ActivityRegistry "1" o-- "many" turn.ActivityHandler
turn.invokeAgentHandler ..|> turn.ActivityHandler : first concrete example; NOT wired to any mode's real behavior in #315
turn.invokeAgentHandler ..> turn.InvokeAgentConfig
turn.InvokeAgentConfig ..> config.Tier          : ModelTier, vendor-neutral — same table delivery.go:69 already resolves
turn.ActivityRequest ..> turn.ResumeRef         : runtime-only; never a DOT attribute (see remark 2 above)

store.GraphDefinition ..> graphexec.Profile     : durable row pins one parsed+validated Profile by PATH + DIGEST (never by copying Source bytes)
store.GraphInstance --> store.GraphDefinition   : DefinitionID FK
store.GraphInstance "1" *-- "many" store.GraphNodeAttempt
store.GraphNodeAttempt --> store.runs           : RunID FK (reuses existing run/segment identity)
store.GraphNodeAttempt "1" *-- "many" store.GraphOutput
store.GraphOutput ..> turn.AskTag               : OwnerCommentID correlates to existing "lf: ask" replies

turn.ExecuteNodeAction --consults--> graphexec.Registry, graphexec.Decide   : is this graph/transition valid
turn.ExecuteNodeAction --consults--> turn.ActivityRegistry                  : what actually runs for this node's action
turn.ExecuteNodeAction --uses--> turn.RunnerSet, turn.Options, turn.submission_runner  : how invoke_agent actually calls the coding agent
turn.ExecuteNodeAction --writes via store methods--> store.GraphNodeAttempt, store.GraphOutput

daemon.graph poller --reads due rows from--> store.GraphInstance
daemon.graph poller --would invoke (once a later issue wires it)--> turn.ExecuteNodeAction
```

### Evidence boundary (run 0003 additions)

Read in this pass, current worktree state (branch
`lf/315-foundations-for-graph-based-execution`, HEAD moved since run 0002 —
`21e5d2a` merged `origin/main`, which included `bd0b355` "Delete docs
directory"; confirmed via `git log --oneline -3 -- docs/` and `git show
bd0b355 --stat`): `docs/` directory absence (`ls docs/` → no such file),
`teams/source/skills/workflow/SKILL.md` (full read),
`teams/source/agents/shared/research-orchestrator.md`,
`teams/source/index.json` (full read), `.claude/agents/research-orchestrator.md`
frontmatter, `.aiteam/AGENT_TIERS.md` (full read),
`internal/config/tier.go` (full read), `internal/config/config.go:545-546`,
`internal/team/doc.go`, `internal/team/hash.go` (full read),
`internal/teamsource/resolve.go` (full read), `internal/teamsource/snapshot.go:1-70`,
`internal/agent/delivery.go` (full read), `internal/turn/delivery.go:1-120`,
`internal/harness/runner.go` (grepped for `Model`/`Resume`/`resume`,
lines ~245, ~328-334, ~544-604, ~820-870, ~880-936),
`internal/daemon/runners.go` (grepped for `Model`/`Tiers`/`ResolveModel`),
`internal/daemon/confirm.go`/`confirm_test.go` (grepped, unrelated conflict-proposal
`Model` field, noted and set aside), `scripts/seed-meter-prices.sh`
(grepped for `Fable`/`claude-fable`), `git log --oneline --follow -- teams/source/skills/workflow/SKILL.md`.
No builds, tests, dependency installs or code/schema changes were made. No
child issues created; no Plan, Implement or Validate work started. The
`docs/` deletion observed in git history is reported as evidence only (it
explains why `docs/workflow/spike.md`, cited by this session's own
`CLAUDE.md` routing table and by run 0001/0002 above, no longer exists in
this worktree) — it is not something this Research pass acted on, evaluated
for correctness, or was asked to investigate further; flagged here only
because it directly informs the DOT-file-location question in remark 1.

## History

- 2026-09-17 (run 0001): Read #230's full unmerged Research history (12 runs)
  as prior art, verified its current-source citations against this branch's
  post-#259 HEAD, confirmed Gonum/DOT are not yet dependencies/artifacts
  anywhere in committed source, identified the `workflow_version` naming
  collision and the Gonum decoder attribute-preservation pitfall, and mapped
  existing recovery/mode/permission/team-source machinery #315 would need to
  integrate with. Research complete for this bounded pass; no blocking
  question; no implementation, Plan, or issue creation.
- 2026-09-18 (run 0002): Owner asked for a concrete illustrative technical
  design (classes, class-diagram overview, DB schema, where existing code
  would need to change) before committing to Plan. Produced an illustrative
  (non-binding) sketch: a new pure `internal/graphexec` package for DOT/Gonum
  parsing and transition logic, plus new files distributed into the existing
  `internal/store` (persistence: GraphDefinition/GraphInstance/
  GraphNodeAttempt/GraphOutput), `internal/daemon` (time/backoff poller) and
  `internal/turn` (bounded ExecuteNodeAction) package directories — matching
  the issue's own explicit placement instruction and this repo's existing
  M0 package-boundary discipline. Sketched a `graph_*`-prefixed migration
  (avoiding the `workflow_version` collision flagged in run 0001) and a
  class-diagram-style relationship overview. Answered "zero existing-code
  modification" honestly: yes for #315 as scoped (no existing file's bytes
  change; confirmed via migrate.go's embed-glob registration and a real
  "declare now, wire later" precedent already in this codebase,
  `store.AgentCodex`), with two named caveats — new files still land inside
  existing package directories, not only a fresh one, and adding Gonum
  touches go.mod/go.sum. No code, dependency or schema file was actually
  added to the repository; no Plan, story decomposition or child issue was
  created. Research checkbox remains checked from run 0001; this is a
  follow-up deepening, not a new mode start.
- 2026-09-18 (run 0003): Owner posted two corrective remarks on issue #315.
  Remark 1: the DOT graph must stay a file, not a DB blob. Verified evidence
  for both halves of the fix — this repo's own recent history shows shipped
  workflow/instruction content consolidating into git-versioned
  `teams/source/skills/workflow/SKILL.md` (replacing the now-deleted
  `docs/workflow/` tree, confirmed via `git show bd0b355 --stat`), and
  `internal/teamsource`'s `CaptureRoot`/`Snapshot`/`DeliveryEvidence`
  machinery already pins external content by path+SHA-256 digest
  (`team.HashBytes`) rather than by DB copy, exactly the pattern the owner
  named. Revised `graph_definitions` to drop `source BLOB NOT NULL` in favor
  of `path TEXT NOT NULL` + `digest TEXT NOT NULL`, flagged one open
  worktree-vs-installed-catalog question for Plan. Remark 2: designed a
  generic `Activity`/`ActivityHandler` layer, concluded it is distinct from
  but joined-by-name to run 0002's `ActionSpec` (`ActionSpec` = declarative
  validation contract in pure `graphexec`; `Activity` = executable behavior +
  config, living in impure `internal/turn` alongside the already-sketched
  `ExecuteNodeAction`, to preserve `graphexec`'s own no-I/O purity
  discipline). Sketched `InvokeAgentConfig` (AgentMarkdownRef, ModelTier)
  split across static DOT `lf_*` node attributes and a runtime-only
  `ActivityRequest.ResumeRef`, never a DOT attribute. Investigated current
  agent-invocation machinery as new ground: found markdown selection today is
  roster-driven (`teams/source/index.json` → `internal/turn/delivery.go`'s
  `planDelivery` → CLI-side subagent resolution), not node-level; found model
  selection already exists as a real per-specialist-role knob
  (`delivery.go:69`, vendor-neutral `Tier`, not "fixed/CLI-default" as
  guessed); found session-resume is already first-class in
  `internal/harness/runner.go`. Flagged an unresolved tension: the owner's
  example model "Fable" (`claude-fable-5-1`, real in `scripts/seed-meter-prices.sh`)
  is not currently reachable through the existing three-tier vocabulary,
  which this repo deliberately built to avoid literal model names in
  configuration. No code, dependency, SQL file or migration was added to the
  repository. No Plan, story decomposition or child issue was created.
  Research checkbox remains checked; this is a further deepening of the same
  bounded pass, not a new mode start.

## Run 0004: how the accumulated design would be sequenced (owner request, "list me how we would structure the work")

Status: **illustrative and advisory only — not a Plan proposal, not a story
decomposition, not acceptance criteria, not task assignment.** This section
answers "if we had to sequence this, what would the shape look like," one
level up from runs 0002/0003's type/schema sketch (structure/order, not
types). Per the issue's own text, "exact package/API design belongs to
Plan" — the binding decomposition into stories with acceptance criteria
remains Plan's job, not this run's. No code, dependency, SQL/migration file
was added to the repository. No child issue was created. Grounded in runs
0001-0003's own accumulated sketch (cited by type/file/table name below, not
re-described abstractly) plus fresh re-reads this run of issue #315's
verbatim six candidate scopes and #230's `RESEARCH_ROADMAP.md` dependency
notes (both re-fetched this run, not recalled from run 0001's summary alone).

### Re-confirmed this run, not assumed from memory

- Issue #315's six candidate scopes, re-fetched verbatim via `gh issue view
  315 --json body` this run: identical wording to run 0001's citation. The
  issue's own ordering (1 profile/parsing, 2 snapshots/persistence, 3
  dispatch, 4 recovery, 5 compatibility, 6 observability) carries no stated
  claim of being a build sequence — the issue only says "Numbers... are
  suggested planning order, not a requirement to execute every parent
  serially" (that sentence is actually in `RESEARCH_ROADMAP.md`'s own preface,
  re-read this run, about the nine *parents*, not #315's six *scopes*
  specifically — but no sentence anywhere claims the six scopes are an
  execution order either; they read as a checklist of required capabilities).
- `RESEARCH_ROADMAP.md` (`origin/lf/230-dag-based-execution-of-implementation`,
  re-read this run, not just run 0001's summary of it) states, in its own
  "Dependency summary" section: "Recovery and basic observability are
  required throughout, already allocated above" and, for #315 specifically,
  "If Foundations' six scopes prove too large, split definition/runtime
  foundations from durable execution and recovery as a tenth parent during
  its own Plan; no evidence yet requires that expansion." Two direct
  consequences for this run's sequencing question: (a) the roadmap's own
  authors already anticipated recovery/observability might need to be a
  *separate* later split, which argues against reading scopes 4/6 as
  "do these last and lightly"; (b) "required throughout" is a completeness
  requirement on the whole rollout, not a build-order instruction — it does
  not by itself say recovery must be the *first* thing coded inside #315,
  only that #315 cannot ship without addressing it before later parents rely
  on it.
- `RESEARCH_ROADMAP.md`'s per-parent dependency lines, re-read this run:
  parent 2 (Research mode, #316) "Depends on 1" (#315); parent 3 (new team/
  workflow setup, #317) "Depends on 1; Research's first working slice informs
  the interface" — i.e. #317 depends on *both* #315's foundations and on
  #316 having produced a first working slice, not on #315 alone; parents 4/5/6
  (#318/#319/#321) each "Depends on 1" plus their own cross-parent notes. This
  directly answers part of "what's #315 vs. a sibling issue's job" below.

### Build-order reasoning: what has zero dependencies, and what depends on what

Reasoning from the concrete pieces runs 0002/0003 already named, not from
generic layering:

1. **`internal/graphexec`'s pure core has no dependency on anything else
   sketched.** Run 0002 specified it with "no SQL import, no clock import"
   (run 0002, `internal/graphexec` section). Concretely: `Profile`, `Node`,
   `Edge`, `ActionSpec`, `Registry`, `ParseProfile`, `Validate`, `Decide` take
   only `[]byte` (a DOT string) and in-memory registry values as input — no
   `store.DB`, no `turn.Options`, no `daemon` clock, no filesystem read beyond
   whatever `ParseProfile`'s caller decides to pass in. This is the same
   purity level as `turn.ParseCommand`, which run 0002 explicitly cited as
   precedent (`internal/turn/doc.go:1-8`, quoted in run 0002). It can be
   fully unit-tested against **inline/synthetic DOT strings** with zero
   database, zero daemon, zero turn machinery, zero real `.dot` file on disk.
   This is the one piece of the whole sketch with a genuinely empty
   dependency set.
2. **Adding Gonum to `go.mod`/`go.sum` is a precondition for (1) to compile
   at all**, not a separate downstream chunk — `graphexec.ParseProfile`
   reads the Gonum AST directly (run 0002's `ParseProfile` bullet, reaffirmed
   by run 0001's decoder-pitfall finding). It has no dependency of its own
   (`go get` needs only network/module-proxy access), so it sits at or before
   step 1, not after it.
3. **A real, shipped example `.dot` file is not required to start (1), but
   is required before (1) can be called "done" in a way that matches scope
   item 1's "registered `lf_<mode>_*` actions."** `graphexec.Validate`'s
   closed-vocabulary/terminal-reachability/bounded-loop checks (run 0002) can
   be exercised against synthetic strings written inline in `_test.go` files
   from day one — no need to wait for a production file. But run 0001 already
   found the only DOT file that exists anywhere (`RESEARCH_WORKFLOW_EXAMPLE.dot`
   on the unmerged `#230` branch) is explicitly "illustrative, non-executable,"
   and run 0003's remark-1 revision means a *real* shipped file's eventual
   home (e.g. `teams/source/workflows/<mode>.dot`, still an open Plan question
   per run 0003) is itself unresolved. So: synthetic DOT strings can and
   should carry `graphexec`'s own unit tests from the start; a real shipped
   `.dot` file is better sequenced as its own later chunk once run 0003's
   open ship-location question is resolved (plausibly by Plan, or by #316
   which actually needs a real `research.dot` per the roadmap's parent-2
   scope item 1) — conflating the two would block `graphexec`'s own testing
   on an unrelated open question.
4. **The `store` persistence layer (`GraphDefinition`/`GraphInstance`/
   `GraphNodeAttempt`/`GraphOutput` + the illustrative `graph_*` migration)
   does not need `graphexec` to exist first, in the narrow sense that its
   SQL schema and CRUD methods reference no `graphexec` Go type** — run
   0002's own schema sketch is plain SQL columns (`TEXT`, `INTEGER`,
   `CHECK(...)`), and `store.GraphDefinition`'s Go struct fields
   (`ID`, `Mode`, `ProfileVersion`, `path`/`digest` per run 0003's revision,
   `TeamSnapshotDigest`, `CreatedAt`) are all primitive types, not
   `*graphexec.Profile`. Its CRUD could be built and tested standalone against
   fabricated rows (e.g. `CreateGraphInstance` called with a literal digest
   string, no real parsed `Profile` involved) — the same "declare now, wire
   later" precedent run 0002 already cited (`store.AgentCodex`,
   `internal/store/runs.go:55-61`). **However**, this is a narrower claim than
   "fully independent": `graph_definitions.digest` is only *meaningful* once
   something computes it consistently with `team.HashBytes`'s convention (run
   0003), and `GraphNodeAttempt.Outcome`/`Status` closed vocabularies only
   make sense once they mirror `graphexec.Node.Outcomes`/`ActionSpec.Outcomes`
   — so `store` can be *built and compiled* first, but calling it "finished
   and correct" in a way useful to the rest of the engine still wants
   `graphexec`'s vocabulary settled first, to avoid the store's `CHECK(status
   IN (...))` and `graphexec.ActionSpec.Outcomes` silently drifting apart.
   **Net: `store` can start in parallel with or immediately after `graphexec`
   begins, but should not be considered "done" before `graphexec`'s
   vocabulary stabilizes.**
5. **`internal/turn`'s `ExecuteNodeAction` + the new `Activity`/
   `ActivityHandler` layer needs both `graphexec` and `store` to exist first,
   concretely and directly** — not just architecturally. Run 0003's own class
   diagram states this as direct calls: `turn.ExecuteNodeAction --consults-->
   graphexec.Registry, graphexec.Decide` (validation/transition) and
   `turn.ExecuteNodeAction --writes via store methods--> store.GraphNodeAttempt,
   store.GraphOutput` (run 0003, "Revised class-diagram-style overview"). A
   function that calls `graphexec.Decide(...)` and `store.RecordGraphNodeAttempt(...)`
   cannot compile before those two exist. This chunk genuinely sits after (1)
   and (4), not merely "conceptually layered above" them — it has a hard Go
   compile-time dependency on both. Within this chunk, the generic
   `ActivityRegistry`/`ActivityHandler` mechanism (run 0003) can be built and
   tested with a **fake/no-op handler** before any real `invoke_agent`
   handler exists — the registry/dispatch shape does not need a working
   `InvokeAgentConfig` to be exercised, only *a* handler satisfying the
   interface.
6. **The `internal/daemon` poller needs `turn.ExecuteNodeAction` to exist
   first**, per run 0002's own wording: "the daemon.graph poller ... would
   invoke (once a later issue wires it) turn.ExecuteNodeAction... the same
   relationship daemon/executor.go's NewExecutor already has to turn.Execute
   today" (run 0002, class diagram). Even the standalone, uncalled version run
   0002 describes ("tested, standalone functions that nothing calls yet") is
   only *meaningfully testable* once there is a `turn.ExecuteNodeAction`
   signature to poll toward — before that, "poller" is just a `store.DB`
   query with no execution partner to demonstrate against. This is the last
   of the four core pieces in build order, consistent with the issue's own
   sentence "SQL stays in store, time/backoff in daemon, bounded action
   execution in turn" listing daemon and turn as the two impure layers, with
   turn as what daemon calls.

**Resulting bottom-up order, reasoned from actual Go-level dependencies, not
restated from the issue's 1-6 list:** Gonum dependency addition →
`graphexec` core (tested against synthetic DOT) → `store` schema/CRUD (can
overlap with `graphexec`, but should not finalize vocabulary before it) →
`turn.ExecuteNodeAction` + generic `Activity`/`ActivityHandler` mechanism
(hard dependency on both prior chunks) → `daemon` poller (hard dependency on
`turn.ExecuteNodeAction`'s signature existing) → a real shipped example
`.dot` file (dependency only on run 0003's open ship-location question being
resolved, not on the Go code chunks — could in principle land anywhere after
`graphexec` exists, but is more usefully sequenced late, once there is
something real to exercise it against end-to-end).

### Does the issue's own 1-6 scope numbering match this build order? No — and here is where it diverges, concretely

- **Scope 1 (profile/parsing) does map onto the first *code* chunk**
  (`graphexec`), so the issue's numbering is not wrong here.
- **Scope 2 (snapshots/persistence) is not simply "second."** As reasoned in
  point 4 above, `store`'s schema can be drafted early and in parallel with
  `graphexec`, but its closed-vocabulary columns (`graph_node_attempts.status`,
  `.outcome`) want `graphexec`'s `ActionSpec.Outcomes` settled first to avoid
  drift — so "build `store` fully before touching `graphexec`" (a literal
  reading of "2 comes after 1... build 1 then 2") is not the soundest
  reading; "start both close together, let `graphexec` settle the vocabulary
  `store` depends on" is closer to what the actual coupling supports.
- **Scope 3 (dispatch) is correctly late relative to 1 and 2** — this matches
  the issue's numbering and the hard compile dependency found in point 5.
- **Scope 4 (recovery/restart/bounded-retry) is the most-diverging item.**
  The issue lists it fourth, after dispatch — but run 0001 already found this
  is not new-from-zero work: #259 (merged `24234dc`/`9308144`, this branch's
  own recent history) just hardened exactly the kind of per-attempt isolation
  and identity-scoping (migration `0047_submission_check_snapshot_scope.sql`
  widening `submission_checks`' key from `(run_id, reference)` to `(run_id,
  snapshot_ref, reference)`; `RecoverSubmissionsForAttempt` isolating
  foreground recovery to one attempt instead of scanning every pending
  receipt) that `GraphNodeAttempt`'s own design (run 0002: `Attempt int`,
  `RunID int64` FK reusing existing run identity, "exactly the discipline
  scope item 4... needs") is explicitly modeled to extend. Reasoned view: the
  *general shape* of scope 4 (an attempt has a stable identity scoped to its
  own instance/snapshot, and recovery must not cross that boundary) should be
  designed into `GraphNodeAttempt`'s schema **from the start of the `store`
  chunk**, not bolted on as a numbered-fourth afterthought — re-litigating
  attempt identity after `GraphNodeAttempt` rows already exist and are
  referenced by `turn.ExecuteNodeAction` would be exactly the kind of schema
  rework #259 shows this codebase has already paid for once (per-story
  snapshot scoping added after `submission_checks` shipped without it). The
  *executable* recovery logic (retry loops, backoff, cancellation handling)
  can still legitimately wait until after a working dispatch loop exists —
  there is nothing to retry until dispatch runs at least once — but the
  *data model* for it should not.
- **Scope 5 (old/new execution-path compatibility) is correctly late, and
  arguably later than the issue's own numbering implies.** Run 0002 already
  found the concrete precedent (`workflow_version` 0/1/2 dual-path pattern,
  `story-contract upgrade` CLI) is a *content-schema* mechanism, not an
  *execution-engine* one — reusing that pattern's shape for graph-engine
  admission is meaningful only once there is a second, real execution path
  running side-by-side to admit into, which by #315's own scope does not
  exist yet (nothing in #315 wires a real mode to the graph engine — run
  0002's "is zero modification achievable" section is explicit that the
  wiring point is a later sibling issue's job, most plausibly #319). This
  scope item is closer to "design the admission *mechanism*'s shape as
  reusable groundwork" within #315, with the actual old/new *coexistence*
  only becoming real once #316-#319 exist to be the "new" side.
- **Scope 6 (status/traces/observability) sits earlier than a literal
  "do it last" reading would suggest, for a concrete reason found in run
  0002's own schema sketch, not a general best-practice argument**: the
  append-only `graph_transitions` trace table was already drafted alongside
  the other three tables in run 0002's single migration file, specifically
  "so a retried node's earlier transitions remain visible rather than
  overwritten" (run 0002, schema comment) — i.e. it is coupled to the same
  attempt/retry data-model decision flagged under scope 4 above, not an
  independent later add-on. Writing `graph_transitions` rows is cheap to add
  at the same time `graph_node_attempts` rows are written (same
  `turn.ExecuteNodeAction` write path, same transaction), so deferring it
  until after dispatch is "working" risks the same kind of retrofit #259 just
  demonstrated is costly for attempt-identity scoping. The *token/time
  baseline recording* half of scope 6 ("Record token/time baselines without
  promising unmeasured savings" — the issue's own words) is more legitimately
  deferrable — that is additive instrumentation, not foundational schema.

### Chunk list (numbered as requested, one entry each)

Each entry: what it is (citing runs 0002/0003's concrete names), what it
depends on, and whether it is squarely #315's own scope or borderline/later.

1. **Add Gonum v0.16.0 as a dependency** (`go.mod`/`go.sum` diff only,
   `graph/formats/dot` + `graph/encoding/dot` per run 0001/0002's finding it
   is currently absent). Depends on nothing internal. Squarely #315 — it is
   literally the enabling precondition for scope item 1.
2. **Build `internal/graphexec`'s pure core** — `Profile`, `Node`, `Edge`,
   `ActionSpec`, `Registry`, `ParseProfile`, `Validate`, `Decide` (run 0002),
   tested against inline/synthetic DOT strings, with the AST-direct parsing
   approach run 0001 recommended over the generic decoder to avoid the
   attribute-drop pitfall from the first test written, not discovered later.
   Depends on (1) only. Squarely #315 — this is scope item 1 verbatim.
3. **Draft the `store` schema and CRUD in parallel** —
   `GraphDefinition`/`GraphInstance`/`GraphNodeAttempt`/`GraphOutput` plus the
   illustrative `graph_*`-prefixed migration (run 0002, revised for
   `path`+`digest` identity per run 0003's remark 1), including
   `graph_transitions` from the start (see scope-6 reasoning above) and an
   attempt/snapshot identity shape informed by #259's just-hardened
   `(run_id, snapshot_ref, reference)` scoping pattern (see scope-4 reasoning
   above) even though the executable retry/backoff logic itself is not built
   yet. Depends on nothing to *start* (can begin same day as (2)), but should
   not be finalized/considered stable before (2)'s `ActionSpec.Outcomes`
   vocabulary settles, to avoid the two silently drifting. Squarely #315 —
   scope item 2, with scope item 4's and 6's *data-model* half pulled forward
   into this chunk rather than sequenced as separate later chunks.
4. **Build the generic `ActivityRegistry`/`ActivityHandler` mechanism plus
   `turn.ExecuteNodeAction`**, tested with a fake/no-op handler — the
   dispatch skeleton (run 0003) without any real `invoke_agent` behavior
   wired in. Depends on (2) and (3) (hard compile-time dependency per run
   0003's own class diagram: consults `graphexec.Registry`/`Decide`, writes
   via `store.GraphNodeAttempt`/`GraphOutput`). Squarely #315 — scope item 3,
   deliberately built as reusable generic machinery per the owner's own
   framing ("concrete implementation of activities is done in later phases,"
   run 0003).
5. **Build the `daemon` poller skeleton** — tested standalone functions
   analogous to `store.DB.ListBacklogSessions` (run 0002), reading due
   `GraphInstance` rows, "not wired into `Daemon.Tick`" (run 0002's own
   phrasing). Depends on (4)'s `turn.ExecuteNodeAction` signature existing to
   poll toward. Squarely #315 — scope items 3/4's daemon-side half, still
   inert/uncalled by design.
6. **Sketch (not implement) the old/new execution-path admission mechanism's
   shape**, reusing the `workflow_version` 0/1/2 dual-path pattern and
   `story-contract upgrade` CLI as a named precedent (run 0002), as groundwork
   only — no real second execution path exists yet to admit into within
   #315's own scope. Depends conceptually on (2)-(5) existing as "the new
   path" to admit into, though the admission mechanism's own shape (a flag/
   version gate per run 0002) could be drafted earlier without a working
   engine behind it. Borderline: the issue lists this as #315's scope item 5,
   but its *real* exercise (an actual old-vs-new user-visible choice) cannot
   happen until a later sibling issue (#319 most plausibly, per run 0002's
   "is zero modification achievable" section) wires a real mode in — so this
   chunk's #315-side deliverable is closer to "a reusable pattern reference
   and possibly a stub gate," not a working compatibility layer.
7. **Add token/time baseline recording** (scope item 6's second half, "record
   token/time baselines without promising unmeasured savings," issue's own
   words) as additive instrumentation once (4)/(5) exist to measure. Depends
   on (4). Squarely #315, but legitimately last among #315's own chunks
   since it measures a working (even if uncalled) dispatch path rather than
   shaping it.
8. **Ship a real, parseable example `.dot` file** — this run finds it is
   *not* a hard prerequisite for (2)'s testing (synthetic strings suffice),
   and its ship location is explicitly still open per run 0003 (e.g.
   `teams/source/workflows/<mode>.dot` vs. some other path — not resolved
   here or by this run). Depends only on run 0003's open ship-location
   question being answered, not on any Go-code chunk above. Borderline/later:
   this is plausibly better owned by #316 (which the roadmap's own text says
   needs "Ship `research.dot`... using mode-namespaced actions" as *its own*
   scope item 1) rather than #315 shipping a production file for a mode
   #315 itself does not wire in. #315's own responsibility is narrower: make
   sure `ParseProfile`/`Validate` work correctly against *some* real DOT
   content (which synthetic test fixtures already cover) — not necessarily
   ship the production file for a mode nothing yet calls.
9. **Wire a real mode into the engine (invoke a real `invoke_agent`
   `ActivityHandler`, call `turn.ExecuteNodeAction` from `daemon.Tick`, admit
   real traffic through the scope-6 gate)** — explicitly **not #315's job**.
   Per run 0002's "is zero modification achievable" section: "the moment a
   later sibling issue... wires a real mode into this engine, that issue will
   need a small, real edit to an existing dispatch point... plausibly
   `internal/daemon/executor.go`." Per the freshly re-read `RESEARCH_ROADMAP.md`
   this run: parent 2 (#316, Research mode) and parent 4/5/6 (#318/#319/#321,
   Plan/Implement/Validate mode) each list "Depends on 1" (#315) as their own
   first dependency — confirming #315 is meant to end before any of them,
   not include any of them.
10. **Resolve `invoke_agent`'s exact `InvokeAgentConfig` model-tier shape**
    (the "Fable" tension, run 0003) — explicitly **deferred, not sequenced as
    a #315 chunk at all.** This run carries the open status forward rather
    than resolving it: run 0003 already named the two options (extend
    `defaultModelByTier`, or add a literal-model escape hatch) without
    choosing, and this run does not choose either. The generic
    `ActivityHandler`/`ActivityRegistry` mechanism in chunk (4) does not need
    this resolved to be built and tested (a fake handler suffices); only a
    *real* `invoke_agent` handler would need it, and building a real
    `invoke_agent` handler is itself sibling-issue territory per chunk (9).

### Sequencing risks pulled forward, not generic project-management advice

- **#259 should be re-read (full diff, not just the commit message) before
  chunk (3)'s schema is finalized, not after.** Concretely: migration
  `0047_submission_check_snapshot_scope.sql` and `RecoverSubmissionsForAttempt`
  (commit `24234dc`, re-inspected this run via `git show 24234dc --stat`) are
  the exact shape of "attempt identity must be scoped to its own snapshot/
  instance, and recovery must not cross that boundary" that `GraphNodeAttempt`
  needs to get right the first time — this was already flagged in run 0001's
  risk list, and this run's sequencing conclusion (point 4 above) makes it
  concrete: it belongs in chunk (3), not deferred to a later "scope item 4"
  chunk.
- **The Gonum decoder attribute-drop pitfall (run 0001) must shape chunk
  (2)'s first test, not be discovered after `graphexec` looks done.** A test
  that parses a DOT string with a standalone graph-attribute statement (not
  just node/edge attributes) and asserts it survives should exist from
  `graphexec`'s first commit, per run 0001's finding that the generic
  `graph/encoding/dot` decoder silently drops exactly this case.
- **The "Fable"/tier tension (run 0003) blocks only chunk (10), not chunk
  (4).** Restated here to guard against silently resolving it as a side
  effect of sequencing, per this run's own constraint: the generic
  `Activity`/`ActivityHandler` mechanism is unaffected by which of the two
  options (extend `defaultModelByTier` vs. literal-model escape hatch) gets
  chosen, so sequencing the generic mechanism ahead of the concrete
  `invoke_agent` config is not just an ordering convenience — it is what lets
  #315's own chunk (4) ship without that open question being resolved at all.
- **The `.dot` ship-location question (run 0003) similarly should not be
  quietly resolved by this run's sequencing.** Chunk (8) above is sequenced
  late and marked borderline precisely so that "when do we need a real file"
  does not implicitly force "where does the file go" to be answered here;
  both remain open for Plan/owner, carried forward, not decided.

### Evidence boundary (run 0004 additions)

Read fresh this run: issue #315's body via `gh issue view 315 --repo
marioatcloudvane/litefactory --json title,body -q .body` (verbatim six
candidate scopes and roadmap-adjacent sections re-confirmed identical to run
0001's citation); `origin/lf/230-dag-based-execution-of-implementation`'s
`RESEARCH_ROADMAP.md` in full via `git show`, specifically its per-parent
dependency lines and "Dependency summary and scope limits" section (not
merely recalled from run 0001's summary); `git log --oneline -5` (confirmed
no commits landed on this branch between run 0003 and this run, so all
runs 0001-0003 evidence citations remain current); `git show 24234dc --stat`
and `git show 9308144 --stat` (re-inspected #259's actual commit content —
migration `0047_submission_check_snapshot_scope.sql` and
`RecoverSubmissionsForAttempt`/foreground-recovery isolation — rather than
relying only on run 0001's prose summary of that commit). No builds, tests,
dependency installs, or code/schema changes were made. No child issues
created; no Plan, Implement or Validate work started.
