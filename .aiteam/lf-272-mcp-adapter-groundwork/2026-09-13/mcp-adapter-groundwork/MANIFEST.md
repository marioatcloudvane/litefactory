# Session manifest

- Branch: lf/272-mcp-adapter-groundwork
- Session date: 2026-09-13
- Feature: mcp-adapter-groundwork

## Modes
- [x] Research
- [x] Plan
- [ ] Implement
- [ ] Validate
- [ ] Spike

## History
- 2026-09-14: Plan run 0005 completed in the reused session. Revised #276/US-002 to make MCP explicitly additive and #277/US-003 to add strict true-default mcp_auto_start with a neutral per-attempt launch specification; #279/US-005 records the consumer documentation boundary. #274 retains actual host registration/startup/teardown and authentic connection proof. Five stories, 38 active criteria; US-002 AC-004 retired for AC-007, new obligations receive unused IDs. Preflight valid/complete with no diagnostics, submit and receipt request `9bebc924a36ac03ba36a27b5ea8cab83` confirm valid input and PlanState pending; publication/readiness await terminal success and save. No owner questions, implementation or source tests.
- 2026-09-13: Plan run 0004 completed and typed input submitted: five stories, 32 explicit acceptance criteria, architecture and per-criterion evidence inventories. Original owner approval expires after 30 minutes, clamped to attempt expiry; no model-visible creation bearer. Preflight valid/complete with no diagnostics; durable request `c0326db1fe8eb8421a2d07f35f9fc6fc` acknowledged valid input, PlanState pending. Publication/readiness remain pending until terminal success and factory save. No owner questions, source implementation or source tests.
- 2026-09-13: Research run 0003 complete; clarified public approval UX and credential storage, recommended factory-side elevated grant lookup without a model-visible bearer token, and documented security limits. RESEARCH_BRIEF.md updated; no production changes or open owner questions.
- 2026-09-13: Research run 0001 started for #272; architecture of MCP groundwork over delivered JSON communication.

## Notes
- Scope and success criteria supplied by issue #272 and owner request.
- Run 0005: native tool access and currently allowed unrelated MCP configuration remain under existing policy; no global MCP-only restriction or lifting of Validate/project-MCP restrictions. Auto-start defaults true as a recorded assumption, false preserves explicit startup, idle daemons start no stdio child. The 30-minute grants and caps remain unchanged. Existing published links #275–#279 are retained; revised publication is pending.
- Plan artifacts: feature-spec.md, ARCH_REVIEW.md, plan_brief.md, PLAN_CONTRIBUTION.json and sanitized PLAN_HANDOFF.json. Story bodies exist only in typed submission input, not per-story Markdown.
- Conservative cap assumptions: one batch/up to five artifacts per attempt, twenty artifacts per repository per rolling 24 hours; pending/uncertain reservations remain charged. SDK preference resolved to stdlib under the repository no-new-module constraint.
- Implementation must first verify landed #263/#264 lifecycle integration; this checkout still documents the newer service as unwired. The helper did not authorize external issue dependency bindings, so typed issue_refs arrays are empty and those prerequisites remain explicit in each story and shared plan. Publication will fill story issue links; no links or readiness were fabricated.

- 2026-09-13: Research run 0001 complete; RESEARCH_BRIEF.md records architecture, class diagram, delivered JSON reuse, current adoption qualification, profile/grant/recovery contracts and test handoff. No implementation or open owner questions.
