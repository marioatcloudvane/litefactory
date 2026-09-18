# Plan: wire-graph-based-execution-into-research

- Branch: lf/316-wire-graph-based-execution-into-research
- Date: 2026-09-18
- Parent: [#316](https://github.com/marioatcloudvane/litefactory/issues/316)
- Status: run 0013 scope correction authored against authoritative result-12 revision 2; revised submission/publication/readiness remain pending factory processing.
- Citation base: prior saved Research handoff fc33e4867553d47bf452842896c656631d42f3ed; retained #315 foundation 1fbd2c9716b6fea8d4c321a1ac713a6dbdbe4d5b. Current architecture evidence is the supplied task checkout; neither historical revision is asserted to be its HEAD.
- Supporting context: [feature-spec.md](feature-spec.md), [ARCH_REVIEW.md](ARCH_REVIEW.md), [RESEARCH_BRIEF.md](RESEARCH_BRIEF.md).
- Owner questions: none. No engineering, execution, acceptance or authentic-host proof is claimed by this Plan.

## Shared Decisions

Research uses the selected graph by default for new eligible work after the rollout evidence gate. Existing legacy continuations and an explicit repository override keep legacy execution. Users select modes; graph completion never selects Plan.

The built-in Research DOT declares control flow; fixed asset selection does not hardcode its transitions. Research actions are lf_research_agent, lf_research_loop, lf_research_if and lf_research_comment. Conditions use lf_field, lf_condition and lf_default; initially string response equality supports ask and done. A default initializes only the first evaluation before any output exists. ask means an OWNER question: explicitly publish, preserve state, end the run and release the worker. Another agent pass needs an authorized owner reply. Durable waiting does not expire by age.

Agent JSON is validated and retained internally. It contains a strict versioned response plus a Research application-result candidate. ask requires an incomplete candidate with permitted questions; done requires a completed candidate without questions. A 1 MiB node ceiling coexists with tighter existing result limits. Explicit comment handling alone publishes; a valid node result still satisfies its declared subprocess final-result boundary. Pending acknowledgment, process outcome, graph lifecycle and external effects remain separate.

Use a fixed built-in selection of the shipped Research DOT, role and skills. Resolve lf_agent_markdown and lf_skills names only within that package, with any shipped shared/name references confined to it. Graph team/mode attributes validate its namespace, not a selectable installed team. Capture and validate the selected dependency closure before launch. Node overrides take precedence over documented shipped role/package defaults. There is no catalog root, installation prerequisite or new team-management subsystem.

The main role runs directly in each host with selected native skills and normal applicable project guidance, within existing permissions. Different agent nodes use fresh sessions; only compatible same-node continuation resumes. Prior output is bounded untrusted data. Codex continuation avoids repeated whole-bootstrap insertion.

At the next authorized launch use current application-shipped Research definitions, retaining actual attempt provenance. This requires no installer, updater or hot reload and does not alter an in-flight invocation. Instruction changes rotate provider sessions with saved findings. Semantic topology changes create a successor at the new graph start with saved brief and owner reply; do not repoint a pinned foundation instance. Reconcile older uncertain publications under their original identities. Formatting alone need not change semantic topology.

No automatic agent retry follows crash, timeout or restart. Report-only recovery is independent. Closure, retirement and explicit mode selection use existing cancellation authority.

## Workflow

Eleven existing story boundaries are retained. The original review found the #315 increment absent from its checkout; integrate and revalidate it before graph-dependent work rather than assuming that historical inspection proves its present integration state. This is a prerequisite, not another story to reconstruct its parser, persistence or dispatcher.

Start built-in definition loading/validation (#335) and internal node-result work (#338). Those enable both host adapters; node results also enable loop/if, comments and usage work. Owner continuation follows control, comments and both adapters. Recovery follows continuation; default admission/current-definition support follows recovery and built-in resolution. The complete shipped workflow follows admission/current-definition support and usage.

Default-selection machinery can be delivered while activation remains gated. Final activation requires assembled recovery scenarios and authentic host observations. This avoids a dependency cycle between admission code and shipped-workflow evidence.

After successful factory publication and save, the owner can request:
`/implement Integrate and revalidate #315, then implement the 11 planned Research stories in dependency order; keep default rollout gated on required evidence.`

## Cross-Story Constraints

- Reuse existing Runner, graph foundation, bounded asset capture/native rendering, application-result validation, effect journals, owner admission and usage machinery. Do not add a second execution engine, scheduler, expression evaluator or telemetry pipeline.
- Keep SQL in internal/store, context first, short transactions and a one-connection SQLite pool; choose new migration numbers after foundation integration. daemon owns time and scheduling, turn owns a bounded turn, report owns rendering.
- Preserve existing permission ceilings, filesystem confinement, allowlisted environment, redaction, stale-attempt checks and request/receipt correction rules. Shipped assets or repository instructions cannot grant authority.
- Use real migrated SQLite and HTTP fixtures for durable effects/recovery; fake host processes for deterministic launch tests. No invented integration build tags or test-name convention. Implementation retains meaningful executed evidence and required make check; this Plan runs no engineering checks.
- Authentic host delivery and helper/correction/receipt evidence must be externally authorized and bound to exact versions, launch settings and implementation snapshots. Historical fixtures and documentation prove only their recorded assertions. Missing observations remain unverified and block default rollout.
- Shared asset-source and native-renderer work do not imply installed-tree edits. If those become necessary, independently assign Codex-owned .claude and Claude-owned .codex stories after shared-source work; do not require installed sibling parity for scoped completion.
- No daemon mutation, build into dist, commits, pushes or paid nested host launches are authorized by this planning result.

## Story Index

| ID | Title | Sub-issue |
| --- | --- | --- |
| research-catalog | Load and validate built-in Research definitions | [#335](https://github.com/marioatcloudvane/litefactory/issues/335) |
| research-claude-main | Run the Research role directly in Claude | [#336](https://github.com/marioatcloudvane/litefactory/issues/336) |
| research-codex-main | Run the Research role directly in Codex | [#337](https://github.com/marioatcloudvane/litefactory/issues/337) |
| research-node-results | Execute an agent node and retain its internal result | [#338](https://github.com/marioatcloudvane/litefactory/issues/338) |
| research-control | Control Research with bounded loop and if activities | [#339](https://github.com/marioatcloudvane/litefactory/issues/339) |
| research-comments | Publish through explicit durable comment activities | [#340](https://github.com/marioatcloudvane/litefactory/issues/340) |
| research-owner-resume | Pause for the owner and consume their reply once | [#341](https://github.com/marioatcloudvane/litefactory/issues/341) |
| research-recovery | Reconcile interruptions and cancellation without agent retries | [#342](https://github.com/marioatcloudvane/litefactory/issues/342) |
| research-admission-updates | Default new Research to graphs and use current shipped definitions | [#343](https://github.com/marioatcloudvane/litefactory/issues/343) |
| research-standard-workflow | Ship and verify the complete Research workflow | [#344](https://github.com/marioatcloudvane/litefactory/issues/344) |
| research-usage | Attribute usage and outcomes to Research graph work | [#345](https://github.com/marioatcloudvane/litefactory/issues/345) |

## Assumptions and Risks

Fixed built-in asset selection is the recorded assumption for the narrowed Research scope, not an unresolved question. Engineering owns concrete override-key spelling, physical asset packaging and documented finite workflow limits, within the typed criteria. One worker advances ordinary deterministic activities using durable checkpoints; there is no job-per-node requirement.

Native role/skill/resume behavior is version-sensitive. Existing whole-turn publication and automatic recovery need explicit adaptation. Long owner pauses must outlive provider sessions and workers. These are implementation risks, not reopened product questions.

## Out of Scope

New team installation/selection, catalog publication/install/update/status/drift management, general multi-mode shared infrastructure, hot reload, legacy retirement, other modes' graph conversion, automatic mode selection, cross-team shared content, marketplace/editor/fleet service, Temporal, specialist fan-out, automatic agent retries, pause expiry, old-version execution retention, topology mapping, unrestricted expressions, global budgets, automated benchmarking and guaranteed savings.

## Provenance

Run 0013 corrects an actual scope overreach in the earlier Plan: team lifecycle management was unnecessary for this Research feature. The authoritative source for this revision is result-12 revision 2, with existing issue links preserved. feature-spec.md and ARCH_REVIEW.md now constrain selection to built-in shipped Research assets; the historical Research brief is not authority to restore removed catalog scope. Only #335, #343 and #344 change; other story fields and dependencies remain intact. Re-pin implementation evidence after #315 integration rather than treating the retained branch as merged current code.

Referenced nested-agent-evidence and story-contract-instruction-manifest documents were absent in this checkout; the supplied evidence/submission obligations remain binding. Historical Claude Code 2.1.276 and codex-cli 0.154.0 are research bounds, not proof of delivered launch behavior. Authentic observations remain future obligations.

