# Research Brief — context-resets-over-mode-changes

## Current request and recommendation

Run 0004, 2026-09-12: the owner returned this feature to Research because the seven-story proposal was too complicated. Reassess the actual problem and remove unjustified scope.

**Recommend one bounded change: start a new conversation when the admitted mode changes, and point it at the existing task artifacts.** Keep ordinary same-mode replies resumable. Research → Plan can start by reading RESEARCH_BRIEF.md; Plan → Implement uses the existing shared brief and factory-frozen stories. The next agent does not need the preceding transcript to perform its role.

This recommendation supersedes the *direction* of run 0003's seven-story proposal. Existing plan/spec/architecture files and published stories are historical proposal evidence, not requirements for this research to preserve. They were not modified or remotely reconciled here. A later owner-selected Plan iteration must reconcile approved/published scope before implementation; this research does not alter factory readiness.

## Decisions retained

The prior “Ok looks good, plan it out!” was interpreted in run 0003 as accepting:
- Fresh conversations on permitted mode changes, including returning to an earlier mode, across supported agents/configurations.
- Existing bounded issue-comment history remains available.
- Incomplete Research may feed Plan; existing approval/evidence gates continue to control work that requires them.

The current request rejects unnecessary implementation complexity, not these simple behaviors. This is an explicit assumption; no new owner decision is needed for this bounded recommendation.

Reset means native conversation reset. Preserve task, worktree, files and durable workflow records. Unsaved reasoning may be lost; reconstructing every thought is not the requested outcome.

## Verified current behavior

All repository citations below refer to **c8b7bf0e42b8209aa13f745bc678a7909f8d5ab1**, inspected on 2026-09-12 in the supplied macOS worktree. Observations are source/test-source inspection, not executed tests or live-agent observations.

| Observation | Evidence |
|---|---|
| External instruction delivery already includes selected mode in its identity. A changed identity creates the next conversation segment. | internal/turn/delivery.go:40, :139, :295, :346 |
| With no external delivery and no delivery history, selection unconditionally returns segment 1. Resume eligibility does not compare modes. This is the concrete inconsistency. | internal/turn/delivery.go:318; internal/turn/prepare.go:1025, :1064 |
| Persistence needed for the decision already exists: runs record Mode; segments have ordinals and independent identities; creation/listing APIs already exist. | internal/store/runs.go:109; internal/store/segments.go:19, :62, :147 |
| A new segment ordinal produces a distinct native session identity. Merely setting resume=false on the old Claude identity is insufficient. | internal/naming/session.go:48, :62; internal/harness/runner.go:545, :589 |
| Codex already omits its resume invocation on a cold request. Both adapters therefore already support the desired fresh-start route. | internal/codex/runner.go:445; internal/harness/runner.go:589 |
| Automatic interrupted recovery is explicitly bound to its saved segment. Segment selection is serialized by the existing task lock. | internal/turn/delivery.go:296; internal/turn/prepare.go:221 |

**Inference:** extend the existing segment-selection decision to account for the mode of the last actually launched conversation run, including the no-delivery path. Reuse recorded run modes and current segment creation. A separate mode table, epoch, persisted counter or migration is not justified by these findings. Returning Research → Plan → Research should allocate another segment, not select the first Research segment.

Implementation must respect prelaunch/refused attempts and automatic recovery; the existing previousRun function already distinguishes terminal non-refused runs (prepare.go:1040). Those are focused regression cases, not separate features. No code change was made here.

## Is enough context retained?

| Boundary | Existing usable handoff | Small remaining requirement |
|---|---|---|
| Research → Plan | RESEARCH_BRIEF.md; saved Plan context when present; current instruction and bounded comments. | Tell the new agent where the existing session is and to read the brief. |
| Plan → Implement | Shared plan_brief.md and full approved frozen story bodies already supplied by the factory. | Point to the same session for shared decisions; keep frozen stories authoritative. |
| Implement → Validate | Frozen criteria and retained engineering evidence already reconstructed for Validate. | Preserve this route; no new transfer mechanism needed. |
| Returning to Research/Plan or leaving Spike | Existing files, current instruction and bounded comments. | Reuse the task directory and read relevant saved work. Do not promise recovery of unwritten context. |

Evidence: Plan locates identity-bound context and falls back to plan_brief.md/RESEARCH_BRIEF.md before deriving a new directory (internal/turn/plan_attempt.go:39). The existing discovery helper handles earlier dates with rooted filesystem checks (internal/turn/sessiondir.go:38, :54, :81). Plan-context discovery refuses multiple matching contexts (internal/turn/plan_context.go:18, :50). Frozen implementation scope is reconstructed in internal/turn/approved_context.go:18; validation inputs in internal/turn/validation.go:17. Both render before the prompt's resume early-return (internal/harness/prompt.go:463). Cold prompts retain bounded history (internal/harness/prompt.go:497).

**There is one real small delivery gap:** generic prompt construction has no task artifact-directory field (internal/turn/prepare.go:350; internal/harness/prompt.go:427). Existing session instructions require reusing a supplied session but otherwise derive today's path and prohibit agent archive scans (docs/workflow/session.md:3). Plan sometimes knows the right directory internally, but that is not a universal task-session pointer.

Recommend a short factory-supplied session path plus “read the relevant existing brief/context before continuing” in the common prompt. Reuse existing discovery and worktree-rooted checks; use the known Plan context directory where available. For other modes the existing MANIFEST/phase artifacts can identify the task directory. Keep this a path-selection/prompt change, not a new persisted binding subsystem or content bundle. A missing brief is not proof that a new Research phase is required. Multiple conflicting candidates should be reported rather than silently claiming continuity; existing discovery's newest-match policy needs care if reused for that case. Exact helper factoring is ordinary engineering.

Path references are sufficient for agents already permitted to read their task worktree. Inlining every document increases prompt size and duplicates potentially stale text. File access does not prove perfect agent comprehension, but that is not a reason to build a new handoff protocol.

## What to remove from the previous proposal

| Previous expansion | Revised disposition and reason |
|---|---|
| New persistent artifact binding/schema and legacy migration work | Drop. Existing task/run/segment records and artifact discovery cover the demonstrated need. No observed failure requires a new data model. |
| Generalized per-mode handoff bundles, provenance envelopes and missing-input framework | Drop. Existing files and factory inputs already carry the material. Add a directory pointer and use ordinary reports for genuine gaps. |
| Automatically feeding all blocking Validate findings back into Implement | Defer as a separate workflow improvement if a concrete correction-input failure warrants it. It is absent from the inspected approved-context builder, but universal corrective-feedback delivery is broader than resetting conversations. Existing comments remain bounded; do not promise all prior findings will always appear. |
| New shared/installed instruction rollout across both protected trees | Drop as a prerequisite. Put the small runtime instruction in the existing common prompt; existing session rules already require reuse. Edit supporting documentation only where behavior needs explanation. No demonstrated need for a cross-harness maintenance project. |
| Mandatory authentic cross-agent observation acceptance gate | Drop from this feature's proposed scope. Adapter command selection and delivered inputs can be regression-tested with existing fixtures. Optional operator smoke checks may provide confidence; never claim they occurred or that offline tests prove model comprehension. |
| Seven stories and a separate aggregate proof story | Replace in a later Plan with one small behavioral slice, or at most reset selection plus session pointer if independently useful. Normal required checks remain part of engineering, not a new product capability. |

The previous proposal conflated “the next phase can work independently” with “guarantee perfect information transfer for every workflow.” The former is the owner's problem. The latter has no finite completeness guarantee and caused most of the expansion.

## Proportionate verification and remaining risks

Existing test sources already cover delivery identity changes/segment reuse (internal/turn/delivery_test.go:27, :166), admitted-mode propagation (internal/turn/mode_transition_test.go:14), and cross-date directory discovery (internal/turn/sessiondir_test.go:46). Extend those fixtures in implementation to check different-mode fresh identity, same-mode resume, return to an earlier mode, both delivery configurations/adapters, prelaunch/recovery behavior, and reuse of the brief directory across dates. Test missing/conflicting paths where the pointer is introduced. Run the repository's required checks for implementation.

No new test code, tests, native-agent launches, runtime inspection or token measurement was performed in this research. Current live delivery configuration is unknown; its inspection is unnecessary to recommend consistent behavior. No external API claim was needed.

Residual limitations:
- Unwritten decisions and tool output are lost; briefs must preserve the useful conclusions.
- Bounded history can omit old feedback. This feature does not guarantee complete corrective-workflow feedback.
- Existing artifact discovery needs a modest careful extension for generic prompts and ambiguity; it is not currently universal.
- Failed/prelaunch attempts must not accidentally reuse a populated old conversation or manufacture repeated resets. Resolve with the existing run-state evidence and focused tests.
- Existing admission and evidence gates remain authoritative. Research cannot silently retire the prior published stories.

## Open and answered questions

**Owner decisions still required: none for the simplified research recommendation.** The three prior product decisions remain usable. Repository inspection answered feasibility, the existing reset inconsistency, available stored state, handoff contents and the narrow path-discovery gap. Exact implementation factoring and tests are engineering decisions.

Research is complete. Suggested next step: owner-selected Plan to replace/reconcile the earlier oversized proposal around this smaller scope; no automatic mode transition.

## History

- 2026-09-10, run 0001: established fresh-start feasibility and handoff risks.
- 2026-09-11, run 0002: separated three owner policy choices from engineering questions.
- 2026-09-11, run 0003 (Plan): interpreted owner assent as accepting those choices and produced seven stories; artifacts retained as history.
- 2026-09-12, run 0004 (Research): rechecked current source; reduced recommendation to existing segment rotation plus a task-session pointer. Removed generalized persistence/handoff/instruction/evidence work from the recommended direction. No implementation or forge changes.
