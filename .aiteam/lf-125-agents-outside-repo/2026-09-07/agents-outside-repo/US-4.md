# US-4 — Publish focused workflows independently of the binary

> As an operator, I want each mode to retain its coordinator and only the necessary specialists so that content updates improve focus without disrupting workflow.

Relocate canonical content, complete focused workflows and remove embedded acquisition.

## Why This Matters

A smaller catalog still needs someone to sequence its specialists. Removing coordinators would save instructions by removing required behavior. The workflow content must also leave the binary so improvements can ship on their own schedule.

## Architecture Notes

- Preserves issue #130. This story contains no migration or backward-compatibility command.
- Apply the known `6eb29bf` correction as supplied: research, plan and implement orchestrators are first in their respective arrays. Preserve the complete relative resources and shared definitions when relocating publisher input.
- The redundant root router is distinct from mode orchestration. Update trusted mode headers, paired Claude/Codex output and session artifact conventions together; the small workflow skill is not an orchestrator substitute.
- Platform: preserve existing read-only Validate profile, language constraints and tier mapping. Downloaded content cannot expand local permissions.
- Risk: removing the embed before an installable publication path exists strands empty installations. Coordinate T-4.1 → T-6.1 → T-4.4; US-4 and US-6 ship together without circular story execution.
- Focused language skills are reusable instructions selected through declared dependencies; do not create duplicate full language stacks or require extra delegation merely to load a skill.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|------------|-----|
| T-4.1 | Move complete canonical agent/skill/resource trees into publisher inputs and preserve exact first-position orchestrator rosters from the known fix. Ref: Runtime and Workflow Requirements Retained, Offline, Cache and Publication Rules. Agent: go-implementation-engineer. | logic | T-1.6 | M |
| T-4.2 | Refine declared language/workflow skills and retain Implement orchestration of production/test authoring and verification; remove dedicated review/updater/admin phases and stale Codex updater references. Ref: Runtime and Workflow Requirements Retained. Agent: go-implementation-engineer. | logic | T-4.1 | L |
| T-4.3 | Align read-only Validate inline findings, trusted mode bootstrap and vendor parity; remove redundant top-level routing/full-body duplication while preserving session conventions. Ref: Runtime and Workflow Requirements Retained. Agent: go-implementation-engineer. | logic | T-4.2 | M |
| T-4.4 | Remove production team embed, embedded install default and seed/fallback after remote acquisition and publisher artifacts exist; retain only explicit test-owned fixtures. Ref: Offline, Cache and Publication Rules, Out of Scope. Agent: go-implementation-engineer. | logic | T-2.8, T-4.3, T-6.1 | M |
| T-4.5 | Test exact rosters, complete dependency closure, permission parity, no eager unrelated bodies and production-build independence from team content. Ref: Acceptance Criteria. Agent: go-unit-test-engineer. | test | T-4.4, T-3.5 | M |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-4.1 | Research resolves research-orchestrator, codebase-tour, web-researcher; Plan resolves plan-orchestrator, requirements-engineer, go-architect, proxy-product-owner, in those exact orders. |
| AC-4.2 | Implement resolves implement-orchestrator, go-implementation-engineer, go-unit-test-engineer, go-integration-test-engineer in that order; Validate resolves go-code-reviewer; Spike resolves an empty roster. Unreferenced shared definitions remain excluded. |
| AC-4.3 | Each orchestrator still sequences specialists and produces the expected session artifacts. Declared skills/resources survive relocation and selected-mode installation; a dependency-only content change alters effective identity. |
| AC-4.4 | Implement content covers production coding, test authoring and verification without dedicated review, context updating, admin or automatic remediation phases. Both vendor variants remove stale updater invocation and preserve necessary Go rules through focused skills. |
| AC-4.5 | Validate produces an inline acceptance/code/evidence/context-drift verdict under existing read-only profiles; it gains neither file writes nor Bash/test execution authority. |
| AC-4.6 | Root bootstrap retains project constraints and trusted mode/session context but no redundant classifier/router decision or duplicated full specialist bodies; Claude and Codex variants preserve equivalent behavior. |
| AC-4.7 | Production build inputs contain no embedded team prompt tree or embedded install/runtime fallback. Empty-home installation uses the remote configuration contract; explicit test fixtures cannot supply a production default. |
| AC-4.8 | Building/upgrading litefactory leaves installed teams untouched, and editing/releasing distribution content requires no binary rebuild. Consumer tests install the independently built fixture with complete corrected rosters. |
