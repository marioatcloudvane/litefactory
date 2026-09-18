# Research brief: Wire graph-based execution into Research mode (#316)

Status: Research INCOMPLETE. The investigation is done; six owner decisions (D1-D6) are open, each with a recommendation. Nothing is implemented; no child issue, Plan or story exists.

Revisions read: this branch base main 7d57998 (no graph code in it). The #315 engine was read from branch lf/315-foundations-for-graph-based-execution at 0271582 (worktree /Users/mariomh/.lf-worktrees/litefactory/task-315). #315 is NOT merged into main; its first increment reports engineering complete, aggregate gates (build, full tests, make check) were still being re-run on 2026-09-18 (#315 comments, runs 12-16 and two later /implement comments). All engine facts below are "as on the #315 branch", not on main.

## 1. Request

Issue #316: make Research the first complete graph-driven mode. The reasoning agent assesses clarity in its initial call and goes straight to investigation when scope is clear. Material questions save progress, end the run and resume on an owner reply without an agent waiting. Deliver via a compatibility bridge so Research does not wait for all team-management changes. Owner decisions (issue body plus latest comment): agents by bare name (`lf_agent_markdown`), one agent per node, several skills per node (`lf_skills`), graph owns order between agents, agent owns skill use (short description first, full text on request), skills have no order, one .md per agent per mode, model tier per node (`lf_model_tier`), new session per agent, resume only for the same node after an owner reply, unresolvable names fail validation before execution, no parallel fan-out, both execution paths supported, opt-in adoption, permission and result contracts preserved.

Asked for: evidence-based findings and recommendations on skill location/attribute and lazy loading, resolver and validation tightening, research.dot shape, task/result contracts, durable question/reply resume, session-per-agent, compatibility bridge, opt-in adoption and observed cases, risks and open Plan decisions, and 3-6 candidate child scopes (not created).

## 2. Current behavior (observations)

### 2.1 #315 engine (branch 0271582)
- Pure package graphexec: ParseProfile (Gonum DOT AST, `internal/graphexec/dot.go`), Validate (`validate.go:14`), Decide (`decide.go:7`), Registry of ActionSpec{Name, Mode, Outcomes} (`registry.go`). Profile attrs: graph `team`, `mode`, `profile_version`, `start`; node `lf_action`, `lf_outcomes`, `lf_limit`, `lf_terminal`; edge `on` or `lf_on`. Every declared outcome needs exactly one edge; every cycle member needs a positive `lf_limit`; terminals carry no action; subgraphs and ports rejected; other attributes kept as raw data in `Node.Attrs` (never evaluated).
- Action names must be `lf_<mode>_<capability>` and be registered (`registry.go`, `validate.go:103`).
- `lf_agent_markdown` is currently validated as a catalog-relative slash path (`invoke_agent.go:41`, `validateMarkdownRef`), only if present; `lf_model_tier` must be flagship/balanced/fast if present. Nothing is resolved or opened; graphexec has a purity test forbidding filesystem access (`purity_test.go`). No `lf_skills` exists.
- Turn layer: `ActivityRegistry` name->handler and `ExecuteNodeAction` (`internal/turn/graph_node.go:38`): reserves an attempt, runs one handler, decides via Decide, commits output plus transition atomically. Handler receives `ActivityRequest{Team, Mode, Node, Attrs, InstanceID, NodeAttemptID, RunID, ResumeRef}`. It "deliberately has no production caller until mode wiring." `ResumeRef` is a runtime-only field with no behavior.
- Attempt reservation takes `RunID` BEFORE dispatch (`graph_node.go:69`) and refuses a session mismatch; but a run row is created inside the existing prepare pipeline. There is no setter to attach a run to an existing attempt.
- Limit exhaustion: instance is set failed with a diagnostic output only (`graph_node.go:69-77`); nothing reports to the owner.
- Store (`0048_graph_execution_foundations.sql`): definitions pinned by path+digest (DOT bytes stay a file; optional `team_snapshot_digest`); instances `running|succeeded|failed|cancelled` with `next_due_at`; attempts (`instance,node,attempt` unique, one running attempt per node); outputs kinds text|json|diagnostic with optional `owner_comment_id`; append-only transitions. No waiting-for-owner state, no question/reply record, no crash reconciliation (a stale running attempt blocks new dispatch: `ErrGraphAttemptRunning`; recovery is documented as deferred).
- Daemon: `pollDueGraphInstances` and retry/backoff helpers exist, unwired.
- #315 spec explicitly hands #316: shipping the first real .dot, which root a .dot path resolves against (its OQ-2), and the agent-markdown layout (its OQ-1) (`.aiteam/lf-315.../feature-spec.md` open questions). #315 scope items 4 (restart/cancel/retry reconciliation) and 5 (old/new path admission) are recorded as remaining #315 debt (A-9), not delivered.

### 2.2 How Research runs today (main 7d57998)
- Catalog: `teams/source/index.json` (schema 2): shared agents plus teams->five mode rosters. go-team research roster is exactly one agent, `research-orchestrator` (`agents/shared/research-orchestrator.md`, tier balanced, skill `research-evidence`). Agent files live at `agents/<team>/<id>.md` or `agents/shared/<id>.md`; skills are catalog-wide at `skills/<id>/SKILL.md` and shared across modes. There is no `<team>/<mode>/<agent>.md` layout and no `.dot` file anywhere. Installed root is `$LF_HOME/teams` (`internal/config/paths.go:67-84`).
- Snapshot: `teamsource.Capture` (`teamsource/snapshot.go`) reads the roster and the transitive skill closure through os.Root, verifies frontmatter (`name`, `description`, `tier` must equal registry values, `snapshot.go:117`), and produces a digest over roster and hashes. It is called on every run's prepare, only when the repository has an explicit `ExternalTeam` (`turn/prepare.go:139`; `store/repos.go:35`). Skill frontmatter requires a non-empty `description`. Release publishing and `team validate` also go through Capture (`teampublish/publish.go:42`, `cli/cmd_team.go:100`), so new files that Capture does not read are not published.
- Delivery: `planDelivery` (`turn/delivery.go:37`) renders each roster role natively; `tools` are intersected with the parent mode's tool list (permissions are a ceiling, `delivery.go` tools loop); the model comes from `opts.Tiers.ResolveModel(kind, tier)`. The bootstrap lists available skill file paths and tells the agent to read one only when relevant (`delivery.go:106`) - i.e. lazy loading by file read already exists. For Claude the native role renders a `skills:` frontmatter list (`render/native.go:44`).
- Segments/sessions: conversation segments per issue session; `prepareDeliverySegment` rotates to a new segment (fresh session ref) whenever the delivery identity or mode changes (`delivery.go:361-390`); resume requires same segment, same agent kind and a recorded resume token (`prepare.go: resumeDecision`); a refused resume falls back to a cold run (`PathColdAfterResumeRefused`).
- Questions: no agent ever waits. Research and Plan are asynchronous by default; `questionPermission` (`turn/ask.go:126`) and `forbidsQuestions` (`ask.go:133`, phrases such as "proceed with your assumptions") decide permission. A question ends the run with an incomplete result plus questions; the owner's reply is a new comment that starts a new run in the same session and normally resumes the provider conversation, else cold-restarts. Test `turn/research_continuation_test.go` covers question -> reply -> resume refused -> cold fallback -> "do not ask" revocation for both agents, and shows the brief and MANIFEST survive in the worktree session directory.
- One comment -> one run: `runs` has `UNIQUE (session_id, trigger_comment_id)` (`store/migrations/0001_init.sql:103`) and `Execute` replays instead of creating a second run for the same trigger (`turn/execute.go`, "AC-3.4"). Private trigger namespaces already exist for automatic recovery and schedules (`store/continuations.go:44-49`, `store/schedules.go:41-48`). One run -> one report; reports are durable, keyed by task/destination/trigger/run/purpose, with daemon retry (`turn/report_delivery.go`, `daemon/report_recovery.go`).
- Result contract: every adopted attempt is told "Required final route: submit a shared application result ... missing or malformed output prevents nominal success" (`turn/result_finalization.go:57`); the result schema has `payload.research{request, findings[{id,text,evidence[{path,line,revision}]}], decisions[], assumptions[]}`, outcome completed/incomplete/blocked, optional questions/recommendation/artifacts (`internal/agentresult/types.go:50-140`; serialization guide `teams/source/skills/agent-finalization/references/research.md` and `examples/research-*.json`). Questions require factory permission and an incomplete/blocked outcome. Cold starts receive an empty baseline, resumes the prior authorized state and revision.
- Session artifacts: the agent writes RESEARCH_BRIEF.md and MANIFEST.md in the session directory (`research-evidence` skill).
- Permissions: the daemon resolves the `research` profile per agent kind and passes tools/allowed tools/sandbox in the Request (`daemon/tools.go:25`); the mode comes from `admitMode`; the same request drives Execute (`daemon/tick.go:~1828`).
- Source sizes (bytes, not tokens; measured with wc -c): research-orchestrator.md 3,628; research-evidence SKILL.md 4,708; workflow SKILL.md 13,304. No token or time baseline exists, so no saving is claimed.

### 2.3 External fact
Claude Code subagent frontmatter `skills:` injects the full content of each listed skill into the subagent context at startup (preload), not lazily; the subagent can still discover and invoke other skills on demand. Source: https://code.claude.com/docs/en/sub-agents (retrieved 2026-09-18 via web search; not independently probed with a live run). Consequence: the owner's "short description first, load full text on request" must not use that field.

## 3. Findings and recommendations

### F1. Catalog layout and resolver (OQ-1, OQ-2 from #315)
The owner's layout `<catalog-root>/<team>/<mode>/<agent>.md` does not exist; today's layout is `agents/<team>|shared/<id>.md` referenced through index.json. Replacing it would break Plan/Implement/Validate delivery, release publishing and parity tests. Recommend an additive layout: `<catalog-root>/<team>/research.dot` and `<catalog-root>/<team>/research/<agent>.md`, legacy `agents/**` untouched until the dedicated retirement. Root: the INSTALLED catalog (`$LF_HOME/teams`), the only root real runs read (no repo/embedded fallback), captured under the existing catalog lock with os.Root confinement.
Resolver design: graphexec stays pure. Add a graph capture beside `teamsource.Capture` that (1) reads `<team>/<mode>.dot`, (2) ParseProfile + Validate, (3) for every agent node checks `<team>/<mode>/<name>.md` exists, parses frontmatter, (4) for every name in `lf_skills` checks `skills/<name>/SKILL.md` (existing closure logic incl. `requires`/`resources`), (5) hashes all bytes into one digest stored in `graph_definitions` (path+digest+`team_snapshot_digest`), and (6) returns immutable bytes for delivery. The same call serves admission, `team validate` and `team-publish`, so unresolvable names fail before any instance is created. Also verify the digest before every node (precedent: "automatic recovery instruction bundle changed; resume manually", `delivery.go:327`) so a mid-graph team update cannot mix versions; a retained snapshot copy is the later option.
Validation tightening in graphexec: `lf_agent_markdown` becomes a bare name (no slash, no `.md`); add `lf_skills` comma list of bare names, duplicates refused; keep both syntax-only in graphexec, existence in the resolver. Tighten only for the `lf_research_agent` action class, since #315 tests use path form.

### F2. Bare-name character set (conflict with owner's examples)
Existing identifier rules are kebab-case only: `team.KeyPattern` (`team/types.go:6`, no underscore), native role names (`render/native.go:16`) and delivery role names. Owner examples `code_reader`, `doc_reader` (underscore) would be rejected or need those rules changed. Recommend kebab-case agent and skill names (`code-reader`); DOT node names may keep underscores.

### F3. Skills: location, attribute, lazy loading
Attribute: `lf_skills="a,b"` (comma list like `lf_outcomes`). Location options: (a) catalog-wide `skills/<id>/SKILL.md` (exists today; all skills shared across modes, transitive closure and publish already work, zero duplication); (b) team-wide `<team>/skills/<id>/`; (c) per mode. Recommend (a) for the bridge: it is the only location with existing capture/publish/test support and skills are already the shared-behaviour vehicle across modes; (b) is a clean later move once legacy is retired and forces duplicating research-evidence/agent-finalization during dual support. Per-mode (c) defeats the owner's own "shared behaviour goes into skills" rule.
Short description / lazy full text: skill `description` frontmatter is already mandatory (`snapshot.go` skillHeader check). Engine builds, per node, an index of name + description + bundle path for exactly the listed skills and adds it to the node prompt; the agent reads the full file with its existing Read tool (already how the bootstrap works). No new tool, no permission widening; do NOT use the Claude `skills:` frontmatter (preloads). Only the node's listed skills (plus their `requires` closure) enter that node's bundle, so `lf_skills` is also a real context boundary. Trade-off: no engine-side record of which skill the agent actually opened; an engine-mediated load tool would need a new MCP tool family and is deferred.

### F4. Model tier
Node `lf_model_tier` is authoritative for graph nodes (resolved through the existing `Tiers.ResolveModel`, so vendor-neutral). Today's agent frontmatter `tier` is mandatory and must equal registry tier (`snapshot.go:117`); graph-layout agents should carry no tier (or an ignored one) to avoid two sources of truth. Note #315 makes `lf_model_tier` optional at validation but `DecodeInvokeAgentConfig` requires both attributes; Plan should decide which one the agent action uses (recommend required for `lf_research_agent`).

### F5. research.dot shape (illustrative, not implemented configuration)
Registered actions (mode-namespaced, closed outcomes): `lf_research_agent` {done, question, gap, blocked}; `lf_research_owner_pause` {answered}; `lf_research_save_report` {saved}.
```
digraph research {
  team="go-team"; mode="research"; profile_version=1; start="investigate";
  investigate [lf_action="lf_research_agent", lf_agent_markdown="investigator", lf_skills="research-evidence",
               lf_model_tier="balanced", lf_outcomes="done,question,blocked", lf_limit=4];
  ask_investigate [lf_action="lf_research_owner_pause", lf_outcomes="answered", lf_limit=3];
  synthesize [lf_action="lf_research_agent", lf_agent_markdown="writer", lf_skills="research-evidence",
              lf_model_tier="balanced", lf_outcomes="done,gap,question,blocked", lf_limit=3];
  ask_synthesize [lf_action="lf_research_owner_pause", lf_outcomes="answered", lf_limit=3];
  save_report [lf_action="lf_research_save_report", lf_outcomes="saved"];
  finish [lf_terminal=true];
  investigate -> synthesize [on="done"];       investigate -> ask_investigate [on="question"];
  ask_investigate -> investigate [on="answered"];  investigate -> save_report [on="blocked"];
  synthesize -> save_report [on="done"];       synthesize -> investigate [on="gap"];
  synthesize -> ask_synthesize [on="question"]; ask_synthesize -> synthesize [on="answered"];
  synthesize -> save_report [on="blocked"];    save_report -> finish [on="saved"];
}
```
Notes: (1) Decide takes exactly one edge per outcome, so "resume the originating node" means one pause node per asking node (or an engine-level pause construct); a shared pause node cannot return to different askers. (2) Every cycle member needs `lf_limit`, counted per dispatch across the whole instance, including a re-dispatch after an owner reply and every gap loop; limits must be sized as first pass + refinement passes + allowed questions. (3) A multi-reader variant (`read_code -> read_docs -> synthesize`, owner example) is the same mechanism with more `lf_research_agent` nodes; the shipped go-team graph should stay near today's one-agent behaviour so the bridge is comparable. (4) Terminal node encodes graph completion only. The research outcome (completed/incomplete/blocked) lives in the authored result; the graph instance ending "succeeded" with an incomplete or blocked report is correct and must not be conflated. (5) The optional Q-first shape in D6 (separate `intro` node) would add `intro` in front.

### F6. Task and result contracts
- Agent task (per node, engine to agent): node name/attempt, `lf_agent_markdown` role, the skills index, the previous node's saved output from `graph_outputs` (text/json, size-bounded, treated as data), pending owner reply if resuming, remaining loop budget, question permission from the existing `questionPermission` (unchanged), the existing session artifact pointer.
- Agent result (agent to engine): one declared outcome plus output text/json plus optional diagnostic. Outcome is authored via the existing result contract, not new prose parsing: intermediate nodes return their declared outcome and text output; the node that ends the path (final synthesize, or any node emitting `question`/`blocked`) submits the application result (`payload.research`, questions, reason). Mapping: done -> completed; question -> incomplete plus questions (permission required); blocked -> blocked; a loop limit reached -> incomplete with a reason.
- Gap to solve: the existing finalization requires an application result from EVERY adopted run (`result_finalization.go:57`) and validates it after exit. Intermediate reader nodes need a per-node "result required: yes/no" obligation supplied by the bridge, otherwise they fail nominal success. This is the most contract-sensitive change; it must keep the schema, permission checks and post-exit validation untouched for nodes that do submit.
- `lf_research_save_report` is deterministic and engine-side: confirm the final run's result receipt, record the graph output, and trigger the existing durable report delivery; it does not invent report text. The brief and MANIFEST stay agent-authored session artifacts.

### F7. Durable question/reply resume without a waiting agent
Existing behavior already ends the run on a question. What is missing in graph terms: (a) a durable "waiting for owner" state: `graph_instances.status` has none (CHECK constraint, so a new status means a table rebuild; #259's 0047 shows that retrofit cost), (b) a record linking question set, originating node/attempt, run and posted report, and the reply comment (`graph_outputs.owner_comment_id` is a start), (c) daemon routing: a new owner comment on a session with a waiting instance resumes it instead of starting a new instance, (d) a mode-selecting comment (`/plan`, etc.) must keep working: the user selects modes; decide whether that abandons or parks the waiting instance, (e) revoke path: "do not ask; proceed with assumptions" must convert to an instruction that forbids further questions for the resumed node.
Recommendation: additive `graph_owner_waits` table (instance, attempt, question ids, source comment, reply comment, state) plus instance kept `running` with `next_due_at` NULL while waiting, rather than widening the status CHECK; resume dispatches the SAME node as a new attempt (counted by `lf_limit`) with provider resume via the existing segment/resume-token logic and the existing cold fallback that re-supplies brief, question and reply. Restart-safe because the state is in SQLite and the worktree brief.

### F8. Session per agent
Fits existing machinery: a different agent means a different single-role delivery bundle and therefore a different delivery identity; `prepareDeliverySegment` already rotates to a fresh segment (new session ref) on identity change (`delivery.go:361-390`), and resume applies only to the same segment/agent/token. So "new session per agent node, resume only for the same node after an owner reply" is achievable mostly by giving each node its own bundle identity. Risks: bundle root is per issue (`BundlesDir/forge/owner/repo/issue`) and retention/overwrite behavior across successive node bundles must be checked; the same agent used by two different nodes (loop back to `investigate`) needs an explicit rule (fresh session by default, resume only after pause); prior-node output must be passed as data through the prompt, never as authority.

### F9. Runs, triggers and reporting in the bridge
- Node runs need private trigger identities (new namespace next to automatic/schedule) because of `UNIQUE (session_id, trigger_comment_id)`.
- Per-node reports must be suppressed; owner sees at most: a question report (pause), a final report, or a failure report. Reuse report_deliveries keys for idempotent post and daemon report recovery (`daemon/report_recovery.go`), keyed to the final/pausing run.
- Drive loop: synchronous inside the existing worker slot for the trigger (keeps session lock, in-flight set and ordering), with the #315 due-poller used only for retry/backoff and restart recovery.
- Need: a store setter to attach `run_id` to a reserved attempt, reconciliation of a stale `running` attempt against its run/receipt on restart, and a report for limit exhaustion (today: instance failed silently).

### F10. Compatibility bridge and opt-in
Attach point: after `admitMode` (mode == research) and before `turn.Execute` in `daemon/tick.go` `run` (~line 1828); otherwise the legacy path is unchanged. Precedent for opt-in: repository `ExternalTeam` association and `auto_resume_on_session_limit` global bool (`config/config.go:106`). Recommend a per-repository explicit opt-in that requires ExternalTeam, an installed team that contains a valid research.dot, AND the flag; presence of a research.dot alone must not switch behavior (teams README: publishing does not silently change installed teams). Active instances stay pinned to their definition and to the path they started on; rollback affects new admissions only and never rewinds effects. Both paths remain supported until dedicated retirement; no automatic mode selection.

### F11. Observed cases required before adoption (from the issue)
1. Clear request: single trigger -> investigate -> synthesize -> saved report; no questions; result validated.
2. Question and restart: unclear request -> question report -> daemon restart -> owner reply -> same node resumed (provider resume success) and cold fallback when resume is refused; "do not ask" revocation; `/plan` while waiting.
3. Failure: agent process failure mid-graph, invalid/missing result on an intermediate and a final node, loop limit exhaustion reported honestly (incomplete, not silent failure).
4. Report recovery: report post fails, daemon restart, report delivered exactly once; instance reflects delivery.
Offline fake-runner tests can establish graph semantics; they do NOT establish real Claude/Codex host behavior (#274/#296 still lack four authentic captures) - keep those claims separate. Record token/time baselines for legacy vs graph without promising savings.

## 4. Options considered
- Layout: replace legacy (rejected: breaks four other modes, publishing, parity tests) vs additive (recommended) vs opaque string until #317 (defers the owner's resolver requirement).
- Skills: catalog-wide (recommended), team-wide, per mode. Loading: file read from bundle (recommended), Claude `skills:` preload (rejected: eager), engine-mediated tool (deferred).
- Pause: dedicated table (recommended) vs new instance status vs "running with null due" only.
- Drive: synchronous per trigger plus poller for recovery (recommended) vs purely poller-driven (adds latency and a second scheduler for a mode that today completes within a turn).
- Loop exhaustion: keep #315 behavior (silent failed instance) vs declared fallback edge (recommended): a node attribute naming the node to route to when its limit is reached (for example `lf_on_limit="save_report"`), validated like an edge; needs a #315 engine extension.

## 5. Risks
1. #315 not merged; #316 cannot start implementation on main until it lands and until #315's own crash-reconciliation and admission scopes are delivered or absorbed here.
2. Result-contract change (per-node obligation) touches the most safety-critical code path; regression risk to Plan/Implement/Validate finalization.
3. Digest drift or partial catalog install mid-graph; requires digest verification per node and clear failure text.
4. Name-charset mismatch with owner examples (F2).
5. Session artifact and bundle root reuse across node bundles (F8) is unverified.
6. Doubling `research-evidence` content in agent markdown and skill: keep one source (skill) to avoid drift; existing parity tests check instruction parity across renderers and will need graph-layout coverage.
7. A bad or edited research.dot bricks Research only for opted-in repos; validation at `team validate`/publish must be the gate.
8. Honest reporting: no token/time claims; baselines only.

## 6. Open decisions (owner) with recommendations
- D1 Catalog layout: additive `<team>/research.dot` + `<team>/research/<agent>.md` alongside legacy (recommended) vs replace.
- D2 Skill location: catalog-wide `skills/<skill>/SKILL.md` (recommended) vs team-wide `<team>/skills/` vs per mode. Attribute `lf_skills`.
- D3 Names: kebab-case agents/skills (recommended) vs allow underscores (requires changing role/key validators).
- D4 Loop exhaustion: add declared fallback (`lf_on_limit`) so exhaustion produces an honest incomplete report (recommended) vs keep failure-only.
- D5 Opt-in: per-repository flag requiring ExternalTeam and a valid installed research.dot (recommended) vs global config flag vs presence of research.dot.
- D6 Clarity assessment: fold it into the first investigator call, outcome `question` ends the node early (recommended; matches "within its initial call" and avoids an extra session) vs separate cheap `intro` node before investigation (costs one extra cold session; clearer trace).
Plan-level (not owner): per-node result obligation design, whether an owner-reply resume consumes loop budget (recommend yes, size limits accordingly), waiting-table schema, trigger namespace, bundle retention.

## 7. Candidate child scopes (refined, not created)
1. Catalog format, resolver and validation: additive layout, bare-name `lf_agent_markdown`, `lf_skills`, teamsource graph capture and digest pin, `team validate`/publish coverage, go-team research.dot plus investigator/writer markdown, per-node skill index. Depends on #315 merge.
2. Research actions and bridge: registry entries, agent-activity handler over the existing prepare/run/harvest pipeline, private trigger namespace, run-to-attempt link, per-node result obligation, output capture, session-per-agent, synchronous drive loop.
3. Owner pause and resume: waiting state, reply correlation, daemon routing, permission and revocation, same-node resume with cold fallback, mode-switch interaction.
4. Bounded loops, failure and honest reporting: refinement loop, limit exhaustion route, stale-attempt/restart reconciliation, final report mapping to completed/incomplete/blocked, report recovery reuse.
5. Opt-in adoption and observation: per-repo opt-in and rollback, legacy fallback, the four observed case groups, token/time baselines.
Scope 4 of #315 (reconciliation) and 5 (admission) overlap children 4 and 5; Plan should decide whether they are absorbed.

## 8. History
- 2026-09-18 run 0001: first Research pass for #316; read #315 engine at 0271582, current team/Research delivery and result contract at 7d57998; recorded D1-D6.
