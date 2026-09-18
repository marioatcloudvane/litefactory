# Plan: wire-graph-based-execution-into-research

- Branch: lf/316-wire-graph-based-execution-into-research
- Date: 2026-09-18
- Parent: [#316](https://github.com/marioatcloudvane/litefactory/issues/316)
- Status: authored completed typed Plan; submission, publication, freezing and readiness remain pending factory processing.
- Citation base: prior saved Research handoff fc33e4867553d47bf452842896c656631d42f3ed; retained #315 foundation 1fbd2c9716b6fea8d4c321a1ac713a6dbdbe4d5b. Current architecture evidence is the supplied task checkout; neither historical revision is asserted to be its HEAD.
- Supporting context: [feature-spec.md](feature-spec.md), [ARCH_REVIEW.md](ARCH_REVIEW.md), [RESEARCH_BRIEF.md](RESEARCH_BRIEF.md).
- Owner questions: none. No engineering, execution, acceptance or authentic-host proof is claimed by this Plan.

## Shared Decisions

Research uses the selected graph by default for new eligible work after the rollout evidence gate. Existing legacy continuations and an explicit repository override keep legacy execution. Users select modes; graph completion never selects Plan.

One editable DOT exists per mode. Research actions are lf_research_agent, lf_research_loop, lf_research_if and lf_research_comment. Conditions use lf_field, lf_condition and lf_default; initially string response equality supports ask and done. A default initializes only the first evaluation before any output exists. ask means an OWNER question: explicitly publish, preserve state, end the run and release the worker. Another agent pass needs an authorized owner reply. Durable waiting does not expire by age.

Agent JSON is validated and retained internally. It contains a strict versioned response plus a Research application-result candidate. ask requires an incomplete candidate with permitted questions; done requires a completed candidate without questions. A 1 MiB node ceiling coexists with tighter existing result limits. Explicit comment handling alone publishes; a valid node result still satisfies its declared subprocess final-result boundary. Pending acknowledgment, process outcome, graph lifecycle and external effects remain separate.

Resolve lf_agent_markdown bare kebab-case names through the graph's team/mode, and lf_skills through the selected team. shared/name is confined to that team's shared/agents or shared/skills. Team skills live under skills/<name>/SKILL.md; mode agents under <mode>/<name>.md. No cross-team fallback exists. Capture and validate the selected dependency closure before launch. Effective defaults are node override > role default > team default.

The main role runs directly in each host with selected native skills and normal applicable project guidance, within existing permissions. Different agent nodes use fresh sessions; only compatible same-node continuation resumes. Prior output is bounded untrusted data. Codex continuation avoids repeated whole-bootstrap insertion.

At the next authorized launch use latest installed content, retaining actual attempt provenance. Instruction changes rotate provider sessions with saved findings. Semantic topology changes create a successor at the new graph start with saved brief and owner reply; do not repoint a pinned foundation instance. Reconcile older uncertain publications under their original identities. Formatting alone need not change semantic topology.

No automatic agent retry follows crash, timeout or restart. Report-only recovery is independent. Closure, retirement and explicit mode selection use existing cancellation authority.

## Workflow

Eleven independently assessable capabilities are proposed. The existing #315 increment is absent from this checkout: integrate and revalidate it before graph-dependent work. This is a prerequisite, not another story to reconstruct its parser, persistence or dispatcher.

Start catalog and internal node-result work. Those enable both host adapters; node results also enable loop/if, comments and usage work. Owner continuation follows control, comments and both adapters. Recovery follows continuation; default admission/update support follows recovery and catalog. The complete shipped workflow follows admission/update support and usage.

Default-selection machinery can be delivered while activation remains gated. Final activation requires assembled recovery scenarios and authentic host observations. This avoids a dependency cycle between admission code and shipped-workflow evidence.

After successful factory publication and save, the owner can request:
`/implement Integrate and revalidate #315, then implement the 11 planned Research stories in dependency order; keep default rollout gated on required evidence.`

## Cross-Story Constraints

- Reuse existing Runner, graph foundation, team capture/rendering, application-result validation, effect journals, owner admission and usage machinery. Do not add a second execution engine, scheduler, expression evaluator or telemetry pipeline.
- Keep SQL in internal/store, context first, short transactions and a one-connection SQLite pool; choose new migration numbers after foundation integration. daemon owns time and scheduling, turn owns a bounded turn, report owns rendering.
- Preserve existing permission ceilings, filesystem confinement, allowlisted environment, redaction, stale-attempt checks and request/receipt correction rules. Catalog or repository instructions cannot grant authority.
- Use real migrated SQLite and HTTP fixtures for durable effects/recovery; fake host processes for deterministic launch tests. No invented integration build tags or test-name convention. Implementation retains meaningful executed evidence and required make check; this Plan runs no engineering checks.
- Authentic host delivery and helper/correction/receipt evidence must be externally authorized and bound to exact versions, launch settings and implementation snapshots. Historical fixtures and documentation prove only their recorded assertions. Missing observations remain unverified and block default rollout.
- Shared teams/source and native-renderer work do not imply installed-tree edits. If those become necessary, independently assign Codex-owned .claude and Claude-owned .codex stories after shared-source work; do not require installed sibling parity for scoped completion.
- No daemon mutation, build into dist, commits, pushes or paid nested host launches are authorized by this planning result.

## Story Index

| ID | Title | Sub-issue |
| --- | --- | --- |
| research-catalog | Install and resolve a complete Research team | [#335](https://github.com/marioatcloudvane/litefactory/issues/335) |
| research-claude-main | Run the Research role directly in Claude | [#336](https://github.com/marioatcloudvane/litefactory/issues/336) |
| research-codex-main | Run the Research role directly in Codex | [#337](https://github.com/marioatcloudvane/litefactory/issues/337) |
| research-node-results | Execute an agent node and retain its internal result | [#338](https://github.com/marioatcloudvane/litefactory/issues/338) |
| research-control | Control Research with bounded loop and if activities | [#339](https://github.com/marioatcloudvane/litefactory/issues/339) |
| research-comments | Publish through explicit durable comment activities | [#340](https://github.com/marioatcloudvane/litefactory/issues/340) |
| research-owner-resume | Pause for the owner and consume their reply once | [#341](https://github.com/marioatcloudvane/litefactory/issues/341) |
| research-recovery | Reconcile interruptions and cancellation without agent retries | [#342](https://github.com/marioatcloudvane/litefactory/issues/342) |
| research-admission-updates | Default new Research to graphs and use current team content | [#343](https://github.com/marioatcloudvane/litefactory/issues/343) |
| research-standard-workflow | Ship and verify the complete Research workflow | [#344](https://github.com/marioatcloudvane/litefactory/issues/344) |
| research-usage | Attribute usage and outcomes to Research graph work | [#345](https://github.com/marioatcloudvane/litefactory/issues/345) |

## Assumptions and Risks

Engineering owns concrete override-key spelling and documented finite workflow limits, within the typed criteria. One worker advances ordinary deterministic activities using durable checkpoints; there is no job-per-node requirement.

Native role/skill/resume behavior is version-sensitive. Current catalog ownership does not yet include the complete mode graph layout. Existing whole-turn publication and automatic recovery need explicit adaptation. Long owner pauses must outlive provider sessions and workers. These are implementation risks, not reopened product questions.

## Out of Scope

Legacy retirement, other modes' graph conversion, automatic mode selection, cross-team shared content, marketplace/editor/fleet service, Temporal, specialist fan-out, automatic agent retries, pause expiry, old-version execution retention, topology mapping, unrestricted expressions, global budgets, automated benchmarking and guaranteed savings.

## Provenance

Run 0011 follows the completed Research handoff and current Plan request. feature-spec.md records settled owner decisions over superseded alternatives; ARCH_REVIEW.md identifies current reusable code and absent foundation integration. Re-pin implementation evidence after #315 integration rather than treating the retained branch as merged current code.

Referenced nested-agent-evidence and story-contract-instruction-manifest documents were absent in this checkout; the supplied evidence/submission obligations remain binding. Historical Claude Code 2.1.276 and codex-cli 0.154.0 are research bounds, not proof of delivered launch behavior. Authentic observations remain future obligations.

