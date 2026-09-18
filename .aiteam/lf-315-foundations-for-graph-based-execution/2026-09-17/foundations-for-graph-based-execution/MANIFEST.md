# Session manifest

- Branch: lf/315-foundations-for-graph-based-execution
- Session date: 2026-09-17
- Feature: foundations-for-graph-based-execution

## Modes

- [x] Research
- [ ] Plan
- [ ] Implement
- [ ] Validate
- [ ] Spike

## History

- 2026-09-17: Research run 0001 started; scaffolding session for issue #315 (foundations for graph-based execution), split from #230's roadmap. Independent main issue, starting in Research; candidate child scopes are not to be created.
- 2026-09-17: Research run 0001 completed. Read #230's full unmerged Research history (12 runs, on `origin/lf/230-dag-based-execution-of-implementation`, not merged to main) as prior art: DOT format, Gonum v0.16.0 parser (BSD-3-Clause), `lf_<mode>_*` action naming and nine-parent roadmap (#315,#316,#317,#318,#319,#321,#322,#323,#324) were already settled there. Verified against this branch's current (post-#259) source that Gonum/DOT are not yet dependencies or artifacts anywhere in committed `main` source — everything DOT-related is illustrative-only on the unmerged #230 branch. Mapped current turn/daemon/store execution, mode-selection/permission-ceiling, recovery (incl. recent #259 fixes), and team/role-source machinery #315 would integrate with or replace. Identified a naming collision (existing `managed_features.workflow_version` legacy/v1/v2 content-schema versioning vs. the proposed new DOT workflow-definition version) and a real Gonum decoder attribute-preservation pitfall. See RESEARCH_BRIEF.md. Research complete for this bounded pass; no blocking question; no implementation, Plan, or issue creation.
- 2026-09-18: Research run 0002 (same session, follow-up deepening — Research checkbox stays checked from run 0001, not a new mode start). Owner asked for a concrete illustrative technical design: new "classes," where existing code would need modification, a class-diagram overview, and a DB schema sketch. Produced an illustrative-only sketch grounded in current source and issue #315's verbatim six candidate scopes: a new pure `internal/graphexec` package (Profile/Node/Edge/ActionSpec/Registry types, Gonum AST parsing, pure `Decide` transition function) plus new files inside the existing `internal/store` (GraphDefinition/GraphInstance/GraphNodeAttempt/GraphOutput persistence, `graph_*`-prefixed tables avoiding the `workflow_version` collision), `internal/daemon` (time/backoff poller) and `internal/turn` (bounded `ExecuteNodeAction` reusing RunnerSet/submission_runner/delivery ceiling) package directories — matching the issue's own explicit "SQL stays in store, time/backoff in daemon, bounded action execution in turn" placement instruction. Answered "is zero existing-code modification achievable" directly: yes for #315 as scoped (no existing file's bytes change), evidenced by migrate.go's embed-glob migration registration (no code edit needed to add a migration file) and the repo's own existing "declare now, wire later" precedent (`store.AgentCodex`); flagged two honest caveats (new files still land inside existing package directories, and adding Gonum touches go.mod/go.sum). No code, dependency, or schema file was added to the repository; no Plan, story decomposition, or child issue was created. See RESEARCH_BRIEF.md "Run 0002" section for full detail.

## Notes
