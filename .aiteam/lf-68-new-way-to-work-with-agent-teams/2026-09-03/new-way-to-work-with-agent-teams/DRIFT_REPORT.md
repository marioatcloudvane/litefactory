# Drift Report — Configurable Agent Teams (Run 19)
# Branch: lf/68-new-way-to-work-with-agent-teams | Date: 2026-09-06

Scope note: this run was explicitly narrowed by the sponsor's issue #68 comment to
US-6, US-7, US-8 only (Workflow step 5, the three renderers), run in parallel on
top of US-5's already-landed renderer contract. US-4, US-9, US-18 were
explicitly excluded and not started. This report compares delivery against that
narrowed scope, not against the full plan.

## Delivered as planned

- **US-6 (Claude renderer)** — `internal/render/claude.go` implements
  `Render` for harness key `claude`, output `.claude/agents/<agent>.md`,
  `tools` mapped to a comma-joined string, standalone-file output only (as
  explicitly scoped this run — marker-tagged-section output deferred to
  US-18, per the sponsor's instruction and per US-6.md's own architecture
  notes anticipating that split). AC-1 and AC-2 met (US-6.md defines only two
  ACs). Loud failure on unsupported `Frontmatter.Extra` fields, wrapped
  sentinel for missing name.
- **US-7 (Codex renderer)** — `internal/render/codex.go` implements `Render`
  for harness key `codex`, output `.codex/skills/<agent>/SKILL.md`,
  standalone-file only **permanently** (Codex has no marker-section
  equivalent — documented in US-7.md itself, not a deferral). Closes fully
  this run as instructed. AC-1, AC-2, AC-3 met: exact output path, loud
  failure naming `model`/`tools`/any `Extra` key with no Codex equivalent,
  determinism verified by its own render-twice test.
- **US-8 (opencode renderer)** — `internal/render/opencode.go` implements
  `Render` for harness key `opencode`, output `.opencode/agents/<agent>.md`,
  `tools` mapped to a native YAML list (a documented assumption — opencode's
  real schema is still an open research item per US-8.md's own architecture
  notes), standalone-file output only this run (marker-section path deferred
  to US-18, exactly as scoped). AC-1, AC-2, AC-3 met.
- All three renderers registered via `init()` + `render.Register`, coexisting
  cleanly with no collisions; `go build ./...`, `go vet ./...` clean;
  `internal/render` still imports only stdlib + `gopkg.in/yaml.v3` +
  US-5's own package, per the cross-story import constraint.
- 22/22 unit tests passing (5 Claude, 8 Codex, 6 opencode, 3 pre-existing
  US-5 smoke tests), 94.0% statement coverage in `internal/render`, verified
  against every AC in US-6/7/8 rather than assumed sufficient.

## Added scope

- A shared sentinel, `render.ErrMissingAgentName`, was added to `errors.go`.
  This was not specified in any of US-5/6/7/8's text; it emerged from Phase
  1.5 code review (CR-B-001) because the three renderers, built in parallel
  with no visibility into each other, each solved the same missing-name
  precondition differently. Consolidating onto one sentinel is a
  correctness fix required by the plan's own cross-story constraint (errors
  wrapped and `errors.Is`-checkable), not a scope addition in the feature
  sense — noted here for completeness rather than as unplanned work.

## Dropped or deferred

- **US-6 and US-8's marker-tagged-section output path** — not implemented
  this run. This is an explicit sponsor instruction for this run ("go as far
  as the standalone-path output only... wait for US-18"), not a gap
  discovered during implementation. Both US-6.md and US-8.md's own
  architecture notes already anticipated this split.
- **US-5's shared T-17 determinism/golden-file suite** — each story's AC-3
  (or AC-2 for US-6) names this suite as the source of determinism
  coverage, but it does not exist yet in the worktree. Each renderer's own
  implementing engineer wrote a self-contained render-twice-byte-compare
  test instead, and the unit test engineer confirmed these are adequate
  substitutes for this run's scope. Building the shared T-17 suite itself
  remains open — it belongs to whichever future run closes out the rest of
  US-5, not to this one.
- US-4, US-9, US-18 — not started, per explicit sponsor instruction.

## Architectural deviations

None from US-5's renderer contract or the cross-story constraints in
`plan_brief.md`. The one design choice made mid-run (opencode's `tools` as a
native YAML list rather than a comma-joined string like Claude's) was already
flagged as an open assumption in US-8.md itself, not introduced silently here.

## Verdict

CLEAN — delivered matches the sponsor's narrowed scope for this run
(US-6/7/8 standalone-path output, parallel, on top of US-5's contract)
within acceptable micro-decisions. No follow-up planning cycle is needed for
this run's scope; US-18 (marker-tagged-section composition) remains the
known next step for US-6 and US-8 to close fully, exactly as the plan already
anticipates.
