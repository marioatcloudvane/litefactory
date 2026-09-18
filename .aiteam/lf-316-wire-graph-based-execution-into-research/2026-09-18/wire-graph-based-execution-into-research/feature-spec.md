# Feature Spec: wire-graph-based-execution-into-research

- Status: READY FOR PLAN AUTHORING; no implementation or acceptance claimed.
- Date: 2026-09-18.
- Authority: current Plan request; settled owner decisions summarized in RESEARCH_BRIEF.md runs 0007–0010 override historical alternatives.
- Owner questions: none.

## Goal

- Give owners complete graph-driven Research: assess clarity in the first reasoning call, investigate immediately when clear, ask material questions through an explicit comment activity, release the worker, and continue durably after an owner reply.

## User Outcomes

| Area | Outcome |
|---|---|
| Catalog | Install a complete team with a Research graph, referenced roles and skills; reject unresolved or ambiguous content before execution. |
| Claude delivery | Run the selected Research role as Claude's main agent with native selected skills and applicable project guidance. |
| Codex delivery | Run the equivalent main role in Codex, including correct continuation and cold-fallback instruction delivery. |
| Node execution | Retain bounded validated internal JSON with node, run and session attribution; returning from an agent does not publish. |
| Control | Express bounded loop and if activities over declared typed fields; initialize the first loop pass once. |
| Publication | Publish questions, final results and actionable failures through explicit comment handling and existing durable effects. |
| Owner continuation | Pause indefinitely without a waiting process; correlate and consume the authenticated owner's reply once. |
| Recovery | Reconcile interruption and uncertain effects honestly without automatically rerunning an agent. |
| Admission and updates | Default eligible new Research work to graphs, preserve declared legacy paths, and use updated team content at the next launch. |
| Standard workflow | Ship one simplified Research role and a complete agent/loop/if/comment workflow; completion stays in Research. |
| Usage | Attribute existing usage, timings and outcomes to graph work for the owner's manual comparison. |

## Existing Features to Reuse

| Capability | Existing owner / retained evidence |
|---|---|
| Graph parsing, validation, declared outcomes, deterministic edges, bounded cycles and saved outputs | #315 foundation; exact current integration is the architect's check, not a requirement to rebuild it. |
| Runner and session lifecycle | internal/agent.Runner; internal/harness; internal/codex; internal/turn. |
| Catalog, native rendering, installation and drift | internal/team, internal/teamsource, internal/render and existing publishing paths. |
| Run and reply state | internal/store; daemon mode admission and owner-comment handling. |
| Result validation and durable publication | Existing application-result contract, request/receipt identity and report/effect journals. |
| Usage | internal/agent usage capture, internal/meter normalization, store meter records and existing logs/inspection. |
| Artifact and mode lifecycle | Existing session artifact/save paths; explicit owner mode selection; no graph-selected mode transition. |

## Data and Relationships

| Entity | Required information | Constraints |
|---|---|---|
| Effective team content | Team, mode, DOT, resolved role and skill closure, inherited defaults, actual content identity | Runtime source is installed $LF_HOME/teams; capture identity per attempt; no permanent execution pin to superseded content. |
| Graph admission | Task/session, selected mode, graph or legacy path | Durable choice for the existing continuation; current repository defaults affect new admissions. |
| Graph instance | Definition identity, execution state, current node, dispatch counters, saved findings | Reuse foundation entities; bounded dispatch; waiting persists without a process. |
| Node attempt | Instance/node, attempt, run linkage, provider session reference, content identity, outcome | Link to the actual run; preserve stale-attempt protection and interruption cause. |
| Node result | Typed response, bounded summary/artifact references, questions where required | Validated before control flow; saved in graph_outputs; task-specific closed response vocabulary. |
| Owner question and reply | Originating node/attempt/question, owner identity, trigger identity, consumption state | One reply consumption per authorized correlation; stale/non-owner input cannot resume it. |
| Comment operation | Exact authored content, durable request identity, delivery state | Pending or uncertain remains recoverable under the same identity and content. |
| Usage attribution | Graph/path/node/attempt/run links, subprocess invocations, measured or missing usage, duration/outcome | Include failed invocation and refused-resume cold fallback costs; unknown is not zero. |

## Authoring and Interface Surface

| Surface | Required behavior |
|---|---|
| Mode workflow | One editable DOT per mode; standard Research workflow shipped first. Shared keys include lf_action, lf_task and lf_on; registered Research actions use lf_research_* names. |
| Role reference | lf_agent_markdown accepts a kebab-case bare name or shared/name, without .md; graph team/mode determines resolution. |
| Skill references | lf_skills is an explicit comma-separated list of bare names or shared/name; reject duplicate and ambiguous native names, invalid syntax, missing files and invalid dependency closure. |
| Runtime catalog layout | $LF_HOME/teams/<team>/<mode>.dot; <team>/<mode>/<agent>.md; <team>/skills/<skill>/SKILL.md; <team>/shared/agents/<agent>.md; <team>/shared/skills/<skill>/SKILL.md. |
| Sharing | shared/name always resolves within the selected team; shared agents can serve different modes; variants combine roles and skills. |
| Defaults | Node task/model-tier overrides take precedence over documented inherited role/team defaults; validate effective task and vendor-neutral lf_model_tier before launch. |
| Conditions | Declarative comparison of a declared typed result field to a declared value; explicit first-pass default; no executable expressions. |
| Repository choice | Add an explicit repository legacy override using existing configuration mechanisms; default eligible new Research to graph; no silent fallback for invalid required graph content. |
| Node result transport | Define an internal validated node-result contract and an explicit bridge to the existing application result obligation; agent result ingestion and external publication remain separate. |
| Host delivery | Selected role is the main agent, not a generic parent delegating to a specialist; native on-demand skills include only the node-selected dependency closure. |
| Owner controls | Existing owner reply, issue closure/retirement and explicit mode selection; no new cancellation command. |

## Required Behavior

- R-01: Initial clarity assessment occurs inside the first Research agent call; clear scope proceeds to investigation without a preliminary reasoning-only process.
- R-02: Ship one simplified version of the current Research role; no investigator/writer split or specialist fan-out.
- R-03: Provide agent, loop, if and comment activities. Start/end markers and durable waiting are state/control boundaries, not extra agents.
- R-04: Initialize the loop default only before any result exists. The first agent call runs once. After valid output, loop/if decisions use the declared response field and graph outcomes.
- R-05: response=ask means questions for the OWNER. Persist findings, validated result and correlation; explicitly publish the question; end the run and release the worker. Do not automatically spin another agent pass.
- R-06: An authenticated correlated owner reply permits the next pass at the originating logical node when content/topology is unchanged. Persist consumption so replay/restart does not consume it twice.
- R-07: Initial-research completion exits the loop, explicitly publishes the selected report, and may advance to another phase inside Research; graph completion never selects Plan or another mode.
- R-08: Malformed JSON, missing required field, wrong type, unknown response, oversized input/output, invalid artifact reference or an undeclared branch fails visibly; never substitute the first-pass default.
- R-09: Every dispatch is bounded by finite node limits and existing subprocess timeout controls. Exhaustion retains findings and reports an honest incomplete/actionable result; no new global budget subsystem.
- R-10: Use a new provider session when moving to a different agent node. Resume only the same node with compatible effective instructions. Provide saved prior output as bounded untrusted data/artifact references, not privileged instructions.
- R-11: Cold start, same-node continuation, host compaction and refused-resume cold fallback preserve the required role, permissions, native skills and result rules. Factory-authored bootstrap must not be repeatedly appended to Codex user-message history on ordinary continuation.
- R-12: Keep ordinary repository guidance applicable. AGENTS.md/CLAUDE.md do not select graph admission or permissions. Avoid promoting legacy dispatcher text over the factory-selected role or triggering duplicate orchestration; do not overwrite project files to achieve graph selection.
- R-13: Preserve current permission ceilings and filesystem restrictions; catalog content cannot grant authority. Native skill delivery must work through the supported delivery bundle without broadening access to daemon state.
- R-14: Agent return saves internal progress only. Public question/final/actionable failure paths use explicit durable comment handling; final application-result validation still applies at its declared boundary. Input acknowledgment is not success, acceptance or publication.
- R-15: Resume uncertain publication using its exact prior identity/content. Recover after send-before-acknowledgment without duplicating the comment or rerunning the agent.
- R-16: Daemon restart reconciles actual run/attempt/result/effect state. Do not automatically retry interrupted/crashed agents, including existing automatic-recovery paths; another agent invocation needs owner continuation. Recovery of already-authored reports is allowed independently.
- R-17: Closure, retirement or explicit mode switch cancels an active/paused graph through existing ownership mechanisms; cancellation preserves retained findings/evidence and does not rewrite an uncertain effect. Restart alone is neither cancellation nor retry authorization.
- R-18: Owner pauses have no silent expiry. Retain findings and question/reply state until completion/cancellation; no live worker/process remains waiting.
- R-19: Use the latest installed team at the next launch/continuation boundary; a content update does not forcibly interrupt an already-running invocation. Record actual bytes/identity used for that attempt.
- R-20: If effective role/skills change, start a fresh provider session with saved findings. If workflow structure changes, start the latest graph from its beginning with the saved brief and owner reply; do not invent a node mapping. Reconcile old uncertain effects under their prior identities.
- R-21: Preserve legacy execution for existing legacy continuations and explicit repository overrides. Graph default rollout requires the recovery and authentic host evidence gates; compatibility remains until dedicated retirement.
- R-22: Team publish/install/update/status must cover the complete graph content closure, confinement and drift, while retaining legacy package behavior. Missing or invalid references fail before any agent launch.
- R-23: Record useful logs and available usage/timing/outcome for each execution, attempt and invocation. No automatic benchmark framework or unmeasured performance claims.

## Candidate Evidence Obligations

| Requirement coverage | Proof method | Available source / version bounds | Missing prerequisite |
|---|---|---|---|
| R-01–08, R-14, R-18 | Deterministic graph tests plus real migrated store and actual HTTP delivery fixtures: clear request, first-pass default, ask/pause, correlated reply, done, malformed outputs | Existing graph/run/store/report code and repository test patterns; bind evidence to implemented snapshot | Implementation and executed retained checks. |
| R-09, R-15–17 | Boundary interruption/restart tests before allocation, during invocation, after authored result, after send before acknowledgment; assert no agent auto-retry and no duplicate effect; cancellation and stale attempts | Fake runner + real migrated store + deterministic HTTP path; offline evidence is not a live host observation | Implementation and executed retained checks. |
| R-10–13 on Claude | Authentic externally authorized launch/continuation/compaction/refused-resume observations with selected main role, native on-demand skill and project-context precedence under actual permissions | Earlier research observed Claude Code 2.1.276 only; official-doc evidence is historical, not delivered-configuration proof | Authorized operator capture tied to exact host version, feature snapshot, launch configuration and assertions. No nested paid launch in this Plan. |
| R-10–13 on Codex | Equivalent authentic operator observations; verify factory bootstrap is not appended per resume and fallback retains role/skills | Earlier research observed codex-cli 0.154.0 only; no authentic composed-launch proof supplied | Authorized external capture with exact versions/configuration; unsupported nested Codex observation cannot establish capability. |
| R-14–15 shared result bridge | Offline contract tests plus authentic helper invocation, same-run correction and authoritative receipt observation on BOTH hosts where #274/#296 prerequisites apply | Existing shared contract and durable receipts; synthetic fixtures establish only deterministic behavior | Required authentic host observations remain unsatisfied until supplied; do not relabel as offline pass. |
| R-19–22 | Catalog packaging/confinement/drift and admission tests; same-node compatible resume, changed-instruction cold launch, changed-topology restart preserving reply/effect identity | Existing team/render/catalog/foundation sources; current architecture inspection settles precise implementation seams | Implemented checks and end-to-end assembled Research adoption evidence. |
| R-23 | Meter/log assertions including unknown counters, failed launch and cold-fallback attribution; owner-readable actual observations | Existing meter captures and version-bounded host evidence | Implementation/executed checks; no comparison result required from owner to ship logging. |

## Assumptions and Engineering Decisions

- A-01: The concrete team-local shared layout above resolves a non-material spelling detail left to Plan; no new global shared package is introduced.
- A-02: Exact registered action names, internal JSON byte limits, artifact-reference grammar, finite dispatch defaults and configuration spelling are architecture decisions; they must be explicit and tested before execution, not runtime guesses.
- A-03: The standard completed response value can be done, alongside ask; the internal contract may carry a typed incomplete/failure result. The owner has not requested an automatic internal continue loop value.
- A-04: A single worker may advance normal deterministic activities with durable checkpoints; the owner's loop/activity design supersedes the earlier worker-versus-scheduler preference question. No new scheduling framework is required.
- A-05: Per-attempt content provenance is required; retaining immutable provenance does not authorize future execution against an old team.
- A-06: Public artifact save boundaries follow explicit question/final/actionable failure reporting; normal internal node progress is durable without publishing a comment per agent return.
- A-07: The previous estimate of 11 stories is advisory. Story authors may split or combine implementation boundaries without reopening settled product decisions.
- A-08: Shared shipped role/source changes and native bundle rendering should suffice; no installed .claude/** or .codex/** edits are presumed. If required, split protected-tree edits into independent Codex-owned .claude and Claude-owned .codex stories; shared-source work precedes them and does not depend on either.
- A-09: Referenced docs/decisions/nested-agent-evidence.md and docs/story-contract-instruction-manifest.md are absent in this checkout. Supplied workflow/evidence/submission rules remain authoritative; missing documents do not waive authentic evidence obligations.

## Risks

- Host instruction/skill/resume behavior is version-sensitive; source and documentation do not prove the exact composed launch.
- Existing whole-turn finalization and automatic recovery must be explicitly adapted for private node results and no automatic agent retry.
- Shared installed catalog updates can occur while multiple repositories are active; latest-content selection must preserve attempt provenance and old effect identity.
- An indefinite pause requires durable findings and reply correlation independent of worker lifetime or provider-session availability.
- Graph default must not become active before complete recovery/evidence gates; unavailable authentic evidence remains a recorded unsatisfied obligation.

## Out of Scope

- Legacy retirement; graph conversion of other modes; automatic Research-to-Plan selection.
- Cross-team/global shared content, marketplace, visual editor, Temporal adoption or fleet-management service.
- Separate investigator/writer agents, specialist fan-out, agent auto-retry, pause expiry or new cancellation command.
- Long-lived old-content execution pins, topology mapping, unrestricted expression language, global budget subsystem.
- Automated performance benchmarking, guaranteed savings or complete provider telemetry where measurements are unavailable.
- Permission widening, fabricated host evidence, paid nested host launches or daemon/runtime-state manipulation by this Plan.

## Planning Completion Criteria

- [x] Intent, current owner decisions, superseded alternatives, bounded scope and evidence limits are explicit.
- [x] Requirements identify input validation, permissions, lifecycle, restart and publication semantics.
- [x] No unsettled material owner question remains; architecture and typed story authoring can proceed.
- [ ] Engineering, executed tests, authentic host observations, acceptance and rollout remain future obligations.
