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

## Notes
