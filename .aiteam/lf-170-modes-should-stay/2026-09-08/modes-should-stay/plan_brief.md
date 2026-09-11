# Plan Brief — modes-should-stay
# Branch: lf/170-modes-should-stay
# Date: 2026-09-09
# Parent issue: https://github.com/marioatcloudvane/litefactory/issues/170
# Citation base: 83cce6b22d7f2a43b1cc8e2a93b5088c5ec6d40a
# Research brief: [RESEARCH_BRIEF.md](RESEARCH_BRIEF.md)
# Status: Generation 2 editorial revision prepared; factory publication pending

## Shared Decisions
- Selected canonical issue labels persist until owner replacement/removal. Agents recommend executable owner actions; they never change modes through prose or access forge credentials. One run has one effective mode and returns one final report.
- Existing managed forced modes, eligible explicit parent replan and child-refinement authorization retain precedence. Otherwise an admitted trigger stays pinned; a fresh valid explicit owner command replaces one valid retained label before retained-label/default-classifier routing. Malformed/multiple labels remain refusals. Label changes cannot unlock frozen scope or completed stories.
- The daemon admits and records the effective mode and canonical label effect before execution, with matching prompts, reports and tools. The per-trigger journal is recovery state, not a second persistent preference. Retirement never removes/restores labels or overwrites later owner edits.
- Label transitions record intent before forge effects and confirm canonical selection before execution. Recovery completes only that transition, preserves unrelated labels and fails visibly on new conflicting mode edits; it holds no SQL transaction over forge I/O.
- Managed Plan distinguishes an explicit discussion outcome from completed publication. US-003 settles exact versioned outcome path/fields and factory parent/generation/run-attempt binding once, plus the identity-bound unfinished context artifact convention; US-004 consumes those same contracts. Preserve the existing proposal version 1 contract.
- Plan preparation invalidates prior readiness. Discussion may save context and report success but cannot publish stories or mark ready; only a freshly validated, published and successfully saved complete proposal restores readiness. Artifact existence or a prior successful run is never approval.
- Unfinished context carries scope, decisions, assumptions and material questions across dates and cold/resumed/fallback attempts, identified by task/feature rather than completion markers. Both runners use equivalent instructions. Plan permits material asynchronous questions by default, explicit no-question instructions win, and Research retains its existing opt-in.
- Completed Plan remains Plan and recommends an explicit owner transition. A plain reply continues Plan. No automatic phase progression is introduced.

## Workflow
- Shipping order: US-001 → US-002 → US-003 → US-004. Retention establishes journal semantics; explicit transitions extend those semantics; discussion lifecycle relies on retained Plan and admission guards; continuation consumes the settled outcome/context contract.
- Dependencies: US-001 has none; US-002 requires US-001; US-003 requires US-001 and US-002; US-004 requires US-002 and US-003. All four are needed for the complete owner-visible workflow.
- Core parser design may proceed alongside transition work, but overlapping daemon/turn changes must have one implementing owner at a time. No story becomes verified solely because a sibling or aggregate run passed.
- Implement consumes factory-supplied frozen GitHub story bodies and identities/revisions on cold, resumed and fallback attempts. Verify each finished story immediately with its acceptance checks, review and tests; publish the supplied per-run evidence event using every exact leading AC identifier before proceeding to siblings. Factory owns story closure/reopening and aggregate gates still apply.
- This Plan run ends with the complete transport proposal. Factory publication must validate the full proposal and fill actual GitHub links into the Story Index. Publication/readiness is pending, not claimed by this artifact.

## Cross-Story Constraints
- Preserve existing internal package ownership: daemon owns time/admission/recovery; turn owns one attempt; planbrief owns pure validation; store owns all SQL and readiness/generation transactions; report cannot import turn. No new package, external service, dependency, background worker or clock is required.
- Store access is ctx-first, uses existing pure-Go SQLite with a one-connection pool, and adds a numbered embedded migration only for an actual schema extension. Do not introduce a second preference store or redundant readiness state.
- Retain parent/session admission serialization, bounded existing workers and immutable retry mode/tool selection. Use context propagation and existing error/report paths; no sleep-based coordination or new goroutines.
- Keep existing untrusted issue/story/history framing, frozen revision/identity checks and per-story evidence authorization. New transport/context access follows rooted path, symlink-component and stable-descriptor requirements. Reserved transport is transient, factory-owned and never staged/committed.
- Test observable behavior with handwritten fakes, real migrated SQLite and existing httptest/fake-executor fixtures. No integration tags, new external environment or real quota-consuming agent calls. Use channels/Quiesce rather than sleeps.
- Per-story review and scoped acceptance tests are required. Final implementation gates: go build ./..., make check, and go test -race -cover ./.... Preserve existing formatting, vet, pinned lint and race requirements.
- Synchronize changed Claude agent/mode sources and Codex skill copies. Keep literal mode-header coupling and relevant APP_CONTEXT/APP_CONSTRAINTS accurate.
- Never commit/push from an agent, write dist/, run make build, restart the live daemon, edit LF_HOME state or construct permission-bypass flags.

## Story Index

| ID | Title | Sub-issue |
| --- | --- | --- |
| US-001 | Keep the selected mode through runs and recovery | [#188](https://github.com/marioatcloudvane/litefactory/issues/188) |
| US-002 | Switch persistent modes through owner commands and labels | [#189](https://github.com/marioatcloudvane/litefactory/issues/189) |
| US-003 | Let Plan finish a discussion without publishing unfinished stories | [#190](https://github.com/marioatcloudvane/litefactory/issues/190) |
| US-004 | Continue planning conversations consistently in Claude and Codex | [#191](https://github.com/marioatcloudvane/litefactory/issues/191) |

## Out of Scope
- Autonomous agent mode changes, natural-language transition inference while a valid mode is selected, and automatic phase progression.
- New mode names, scheduling changes, live chat, question-round limits or generic classifier redesign.
- Reopening completed managed stories, relaxing immutable approved/frozen revisions or changing evidence authorization.
- Changes to Research question opt-in except wording needed to distinguish Plan.

## Provenance
- Scope: [feature-spec.md](feature-spec.md); architecture and task guidance: [ARCH_REVIEW.md](ARCH_REVIEW.md); prior investigation: [RESEARCH_BRIEF.md](RESEARCH_BRIEF.md).
- Parent github:marioatcloudvane/litefactory#170; factory run 0010; proposal generation 2. The header citation base records the original planning investigation, not the current implementation revision.
- Before implementation, re-pin affected mode admission/recovery, managed handoff/readiness, session discovery and prompt seams against the factory-supplied implementation base and frozen stories. Do not substitute current live story edits or locally reconstructed stories.
- The factory supplied current planning stories US-001–004 as existing issues #188–191, with numeric identities and expected-base digests. All four generation-2 proposal records use intent update and those exact bases. The only story-body change removes the Evidence contract paragraph from Acceptance Criteria; all AC identifiers, requirements, tasks, architecture and dependencies remain intact. The factory alone reads GitHub, verifies bases and publishes. No per-story Markdown files are used.
- Existing implementation is preserved. Prior implementation/test reports do not establish factory-accepted completion; renewed Implement must inspect retained work, resolve outstanding gaps and publish fresh criterion-named evidence against the newly selected revisions. Evidence publication obligations remain in the factory handoff and shared workflow above.
- This editorial revision does not change the acceptance parser or approved-revision policy, apply protected Codex skill updates, or claim any story completed. The known Codex source/copy synchronization obligation remains for implementation.
- Assumptions resolved from the issue/research: owners control transitions; supported explicit commands replace one valid selected mode; clearing labels returns normal routing; Plan discussion is explicitly successful but unready; unfinished context survives date changes.
- Headless instruction authorized completing this proposal without questions. This is approval to produce planning artifacts, not approval to implement or a claim that publication has succeeded.
