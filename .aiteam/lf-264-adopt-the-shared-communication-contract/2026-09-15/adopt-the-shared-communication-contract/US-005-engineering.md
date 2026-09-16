# US-005 — Align shared instructions and auxiliary conventions

Frozen story #302 / issue_id 5463753553 / parent_id 5439211198 / revision
`058e48341cc4ea550ae30441e0975e5c4c52e769ff875a34ac95892b08d270f1`;
dependencies US-001/002/003/004 supplied as engineering-recorded. Workflow 2.

## Status: implementation DONE; specialist verification pending

Canonical guidance now describes adopted shared/helper/terminal routes, separate
checkpoint/result descriptors, explicit prior baseline and revisions, atomic merged
validation/correction, request replay, independent receipt states, final enforcement,
restricted reference-only Validate, compatibility and separate retirement milestones.
Workflow declares the existing agent-finalization skill dependency for native source
capture; Research remains self-contained through its neutral research-evidence
contract. Root mode headers/routing and session conventions remain intact.

Updated actual runtime prompt strings to remove obsolete Plan discussion-only guidance
and recognize the already-wired aggregate capture adapter. Existing Plan publication
and evidence/acceptance owners retain authority. No new transport or business logic.

Added an explicit classifier/scheduler/conflict caller inventory. Each already has
an independent closed typed contract and owner validation; application envelopes,
versions and result-receipt authority are deliberately excluded with per-caller
reasons and unsupported-input expectations. No speculative error layer introduced.

Current documentation distinguishes implemented Claude/Codex integrations from the
historical deferred-Codex statement. Four authentic working/Validate observations
remain outstanding on #274/#296. Required legacy reader removal and installed-tree
work are not claimed complete.

## Changed paths for this story

- AGENTS.md
- CLAUDE.md
- docs/agent-auxiliary-contracts.md (new)
- docs/agent-evidence-capture.md
- docs/agent-result-contract.md
- docs/mcp-tool-families.md (extends completed US-003 documentation for US-004 activity)
- docs/story-contract-instruction-manifest.md
- docs/workflow/spike.md
- internal/turn/mcp_guidance.go (extends US-002 prompt changes)
- teams/source/skills/agent-finalization/SKILL.md
- teams/source/skills/agent-finalization/references/common.md
- teams/source/skills/agent-finalization/references/implement.md
- teams/source/skills/agent-finalization/references/validate.md
- teams/source/skills/planning-contracts/SKILL.md
- teams/source/skills/research-evidence/SKILL.md
- teams/source/skills/review-evidence/SKILL.md
- teams/source/skills/workflow/SKILL.md

No test files, protected installed tree, immutable release, daemon state or dist file
was edited. No generator installed files, provider ran, or forge mutation occurred.
No acceptance, contribution submission, commit or push was performed.

## Executed scoped checks

Executed against base HEAD `16634ed33075ea3acfc449bcb6316163bdc0b807` plus current
uncommitted US-001 through US-005 state. Orchestrator must capture the final bytes;
base HEAD alone does not identify these changes. These are offline checks only.

1. `go test ./internal/teamsource ./internal/agent -run 'TestSharedWorkflow|TestFinalization_TemporaryDiscoveryAndBothRenderings|TestModeVocabularyAndLabels|TestParseScheduleProposalAcceptsOnlyTheContract|TestScheduleProposalPromptKeepsDescriptionAsData|TestRegionResponse' -count=1 -timeout=8m`

   PASS: teamsource 0.916s; agent 0.500s. Temporary/in-memory rendering succeeds with
   the finalization dependency and its resources; no installed-tree output.

2. `go test ./internal/turn -run 'TestMCPGuidance_RegistrationRequired|TestMCPGuidance_InstalledCards|TestMCPGuidance_Checkpoint|TestMCPApplicationGuidancePayloadContract' -count=1 -timeout=8m`

   PASS: turn 1.273s.

3. `go vet ./internal/turn ./internal/teamsource ./internal/agent`: PASS, exit 0,
   no output. No Go changes followed this vet.
4. `git diff --check`: PASS, no output.
5. `gofmt -l internal/turn/mcp_guidance.go`: PASS, no output.

No whole-package/repository suite or make check was run here.

## Required test-specialist handoff

- Update the stale plan expected substring in
  `TestMCPGuidance_ModeRestrictions`: old text says completed Plan communication
  cannot publish and discussion-only; new guidance delegates typed completed results
  to the existing Plan owner and keeps input acknowledgment separate from publication.
  This exact stale assertion was located, not executed during this story.
- Add retained assertions for adopted shared/root and neutral Research guidance,
  explicit baseline, retry/receipt dimensions, final enforcement, compatibility and
  #274/#296 evidence bounds. Verify source capture includes finalization resources
  without installed-tree writes.
- Verify exact manifest ownership US-005 shared / US-006 Claude / US-007 Codex /
  US-010 aggregate, with no ambiguous prior-feature ownership.
- Add targeted auxiliary unsupported-input tests at existing owners: unknown mode
  or application-shaped routing output; schedule unknown version/application fields;
  conflict unknown version/application shape and identity mismatch. Existing parser
  checks passed; no new wire mapping is intended.

## Exact installed handoff

`docs/story-contract-instruction-manifest.md` contains exact source-to-target rows.
US-006 under Codex integrates into these existing Claude files only:

- .claude/agents/requirements-engineer.md
- .claude/agents/proxy-product-owner.md
- .claude/agents/research-orchestrator.md
- .claude/agents/plan-orchestrator.md
- .claude/agents/implement-orchestrator.md
- .claude/agents/go-architect.md
- .claude/agents/go-implementation-engineer.md
- .claude/agents/go-unit-test-engineer.md
- .claude/agents/go-integration-test-engineer.md
- .claude/agents/go-code-reviewer.md
- .claude/skills/shared/workflow.md
- .claude/skills/shared/review-evidence.md
- .claude/modes/session.md
- .claude/modes/validate.md

US-007 requires Claude and integrates only these existing Codex files:

- .codex/skills/requirements-engineer/SKILL.md
- .codex/skills/proxy-product-owner/SKILL.md
- .codex/skills/research-orchestrator/SKILL.md
- .codex/skills/plan-orchestrator/SKILL.md
- .codex/skills/implement-orchestrator/SKILL.md
- .codex/skills/go-architect/SKILL.md
- .codex/skills/go-implementation-engineer/SKILL.md
- .codex/skills/go-unit-test-engineer/SKILL.md
- .codex/skills/go-integration-test-engineer/SKILL.md
- .codex/skills/go-code-reviewer/SKILL.md
- .codex/skills/workflow/SKILL.md
- .codex/skills/review-evidence/SKILL.md

Integrate shared serialization semantics with legacy role wrappers; do not replace
legacy files with native render output or add missing skill references blindly.
Research keeps its neutral contract. No new installed finalization skill is required.
Temporary sibling drift is expected; neither story requires sibling parity. US-010
owns aggregate parity after both independent stories complete. Unavailable Claude
leaves US-007 incomplete with this handoff, without a permission retry or substitute.
