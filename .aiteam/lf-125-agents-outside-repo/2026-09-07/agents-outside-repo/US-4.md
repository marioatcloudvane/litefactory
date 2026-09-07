# US-4 — Focused modes and language skills

> As an operator, I want implementation runs focused on building and testing, with review in validation, so that each stage loads the instructions needed for its work.

Refactor the Go workflow into small roles with declared skills and a truthful read-only validation handoff.

## Why This Matters

The current implementation workflow carries review and administration responsibilities as well as coding. Smaller mode rosters can reduce that overhead, but moving a role without its supporting instructions would break execution. Validation also cannot promise test execution where its permitted tools only allow reading evidence.

## Architecture Notes

- Keep current workflow roles where useful and move language/platform/testing bodies into focused skills. Research selects codebase/web guidance, Plan requirements/architecture/decomposition, Implement implementation/unit/infrastructure test authoring, Validate acceptance/review/evidence/drift. Spike may remain root-only.
- Audit each moved instruction's resource references and convert hardcoded repository-relative mode/template/hook/skill references into declared package dependencies or the selected bootstrap. No mandatory extra delegation just because a skill exists.
- Slim Implement choreography to authoring and required verification; remove dedicated review/environment-administration phases and automatic review-driven remediation. Test authoring stays in Implement, including infrastructure tests only where the repository supports them.
- Validate produces inline `pass`, `findings` or `insufficient evidence`, with acceptance, review and evidence rationale; it creates no review artifact or tracked change. Claude's exact profile allows Read/Glob/Grep/Task and no Bash; Codex checks remain bounded by read-only sandbox. Owner initiates any later implementation correction.
- Update paired Claude/Codex instruction sources, project bootstrap and parity fixtures together; preserve trusted mode headers and session artifacts while removing eager full-roster listings. Keep already retired updater and its tier absent.
- Platform: retain current policy/surface invariants. Risk: changing test expectations to permit shell/writes would silently expand Validate beyond this story.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|------------|-----|
| T-4.1 | Refactor Go roles and focused skill dependency declarations | logic | T-1.4 | L |
| T-4.2 | Slim Implement to authoring and required testing | logic | T-4.1 | M |
| T-4.3 | Add read-only Validate review/evidence/drift choreography | logic | T-4.1 | M |
| T-4.4 | Update bootstrap, tier references and paired instruction fixtures | logic | T-4.2, T-4.3, T-3.3 | M |
| T-4.5 | Test membership, closure, permissions and report parity | test | T-4.4, T-3.5 | M |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-4.1 | Implement roster contains implementation/test specialists and verifies authored changes, without dedicated review, environment administration or updater invocation. |
| AC-4.2 | Validate covers acceptance, Go-aware review, test evidence and context drift; missing evidence cannot yield pass and no code/test/durable-document edits occur. |
| AC-4.3 | Existing exact Validate permission tests remain valid; Claude cannot launch Bash and Codex receives no additional writable authority. |
| AC-4.4 | Each mode's selected closure contains every required resource; unrelated modes do not eagerly load all language-skill bodies or global roster descriptions. |
| AC-4.5 | Both vendor instruction outputs preserve equivalent observable workflow and report boundaries, including session conventions, without updater or Maintenance restoration. |
| AC-4.6 | Fixtures exercise pass/findings/insufficient-evidence outcomes, no tracked Validate changes, and an owner-driven subsequent correction rather than an automatic loop. |
