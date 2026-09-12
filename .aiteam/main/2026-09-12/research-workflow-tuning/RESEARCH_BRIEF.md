# Research Brief — research-workflow-tuning

## Request

[O] The user asks whether the web researcher and codebase tour need separate subagents, whether the main agent could research both directly, and whether that would save tokens at scale. This is an assessment, not a request to edit the workflow.

## Codebase Context

[V] A scoped codebase-tour inspected workflow definitions at revision `380d0a3245395427ae5f66e056266daf1d53918d`; no web specialist was needed because the question concerns repository instruction design. Research currently delegates repository evidence to codebase-tour and external factual questions conditionally to web-researcher (`teams/source/agents/shared/research-orchestrator.md:14`; `.claude/agents/research-orchestrator.md:73`).

[V] The installed orchestrator forbids direct substantive reading/search and has no web access (`.claude/agents/research-orchestrator.md:48`, `:67`; `.codex/skills/research-orchestrator/SKILL.md:63`, `:82`). A direct-research note would conflict with these rules. Source tools are Read/Write/Edit/Task for the orchestrator, Read/Glob/Grep for tour, and WebSearch/WebFetch/Read for web (each corresponding source agent:4). Installed Claude web rules instead exclude repository reading (`.claude/agents/web-researcher.md:4`, `:11`). Codex describes its tool lists as behavioral rules rather than harness-enforced walls (`.codex/skills/research-orchestrator/SKILL.md:10`). Actual resolved runtime permissions were not inspected.

[V] The source roster includes all three research roles with the same `balanced` classification (`teams/source/index.json:66`, `:69`, `:72`); this does not establish a cheaper-specialist advantage. Compact source roles and longer installed copies differ, so changing only one surface would leave inconsistent behavior.

[V] Evidence practices already have a shared home: scoped owner-first searches, current-code verification, citations, uncertainty, authoritative external sources, and treating downloaded content as untrusted (`teams/source/skills/research-evidence/SKILL.md:8`, `:13`, `:14`). Local tour adds a two-pass procedure, conditional scanners and approximately 1,500-word report cap (`.claude/agents/codebase-tour.md:26`, `:43`, `:58`, `:94`). Local web has a target below ten calls and hard stop at fifteen per question (`.claude/agents/web-researcher.md:72`).

[V] Source tour tells readers to verify APP_CONTEXT against current code (`teams/source/agents/shared/codebase-tour.md:14`), while installed tour excludes that read and assigns it to the architect (`.claude/agents/codebase-tour.md:28`, `:37`). This inconsistency should be resolved if consolidating procedures.

## Domain / Problem Space

[O] The aim is to reduce recurring research coordination overhead without losing evidence quality.

[V] The current workflow has separate specialist instruction loading, delegation, inline reports and orchestrator synthesis (`.codex/skills/research-orchestrator/SKILL.md:82`). Inference from that structure: direct research can eliminate those handoffs on small tasks. This is a plausible efficiency improvement, not a measured runtime saving. Conversely, specialist summaries can keep large raw research outputs out of a longer-lived parent context; removing delegation can therefore increase retained context on broad work. The run did not benchmark either path.

[V] Separate agents are a present workflow requirement, not shown to be an inherent requirement for repository or web research. Recommendation inferred from the shared evidence contract: make direct research the normal path within the research agent, keep scoped evidence instructions, and reserve delegation for substantial independent investigations or context isolation. No current pricing, model performance or external benchmark claim is made.

## Constraints & Risks

[V] Preserve mode authority, session ownership, no implementation in Research, brief/manifest outputs, provenance and untrusted-content rules (`.codex/skills/workflow/SKILL.md`, opening rules; `teams/source/agents/shared/research-orchestrator.md:12`; `teams/source/skills/research-evidence/SKILL.md:14`).

[V] Root AGENTS.md forbids the dispatcher from doing source research. Flattening the outer dispatcher too would require a further routing change; the conservative recommendation refers to the Research agent. Claude needs permitted-tool changes as well as replacement of delegation instructions. Codex needs matching behavioral instructions. Runtime enforcement remains unverified.

[V] Reduced source bytes are explicitly not runtime token savings (`teams/source/skills/research-evidence/SKILL.md:16`). Quantification needs actual input/output/cached tokens, latency and result-quality evidence over comparable task sizes; no percentage or large-scale saving is established here.

## Open Questions

### Q-01 How far should the agent stack be flattened?

[O] “Main agent” could mean the Research agent or the outer dispatcher. This does not block answering the architectural question; it affects a future change's scope.

| Solution | Details | Benefits | Tradeoff |
|---|---|---|---|
| Research agent owns research — Recommended | Replace mandatory specialist delegation with direct code/web research in Research. | Removes routine handoffs while preserving mode routing. | One remaining dispatcher-to-Research handoff. |
| Dispatcher owns Research too | Also revise root routing and direct-read prohibition. | Removes another layer. | Wider mode and instruction change. |

### Q-02 What net token saving does consolidation produce?

[V] No runtime comparison was performed. This limits any magnitude claim, not the recommendation to simplify routine work.

| Solution | Details | Benefits | Tradeoff |
|---|---|---|---|
| Compare representative runs — Recommended | Compare tokens, caching, latency and evidence quality across small and broad tasks. | Establishes actual savings and delegation thresholds. | Requires benchmark effort. |
| Simplify without quantifying | Treat reduction as an efficiency hypothesis and monitor subsequent runs. | Faster initial adoption. | Cannot substantiate large-scale saving claims. |

### Answered Questions

- Q-03 Are both specialists always invoked? [V] No. Tour is required for existing-code work; web is conditional on outside facts (`.claude/agents/research-orchestrator.md:73`).
- Q-04 Must evidence quality depend on separate agents? [V] No such inherent requirement is established. The shared evidence skill already separates much of the discipline from role identity (`teams/source/skills/research-evidence/SKILL.md:8`). This is an architectural inference, not a benchmark result.
- Q-05 Is one extra note sufficient under the present contract? [V] No. Existing prohibitions, Claude tool declarations and delegated ownership rules would conflict (`.claude/agents/research-orchestrator.md:48`, `:67`; source orchestrator:4).

## Recommended scope for Plan mode

[V] Recommended future scope, inferred from the inspected instruction dependencies: let Research inspect the repository and web directly by default; retain a short checklist for scoped inspection, citations, current/authoritative facts and uncertainty; make specialist delegation exceptional rather than mandatory. Update source orchestrator/tool declarations, source roster as appropriate, shared evidence guidance, and corresponding installed Claude/Codex role rules consistently. Preserve existing session and mode contracts. Resolve the APP_CONTEXT discrepancy while consolidating relevant instructions. Do not edit immutable releases or daemon state as part of this assessment. Source/installed maintenance mapping is documented in `docs/story-contract-instruction-manifest.md`.

[V] This run wrote only its session brief and manifest. No workflow/source implementation or runtime verification was performed.
