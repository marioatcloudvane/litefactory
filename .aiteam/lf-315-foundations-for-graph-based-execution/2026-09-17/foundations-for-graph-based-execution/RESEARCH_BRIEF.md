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

## History

- 2026-09-17 (run 0001): Read #230's full unmerged Research history (12 runs)
  as prior art, verified its current-source citations against this branch's
  post-#259 HEAD, confirmed Gonum/DOT are not yet dependencies/artifacts
  anywhere in committed source, identified the `workflow_version` naming
  collision and the Gonum decoder attribute-preservation pitfall, and mapped
  existing recovery/mode/permission/team-source machinery #315 would need to
  integrate with. Research complete for this bounded pass; no blocking
  question; no implementation, Plan, or issue creation.
