# Feature Spec: Complete the Config YAML Rework
# Status: APPROVED
# Date: 2026-09-04

## Goal
- Complete and verify the interrupted configuration redesign so operators can use the canonical schema and receive secure, mode-derived permissions without regressions from obsolete behavior.

## User Stories
| ID | Story |
|----|-------|
| US-1 | As an operator, I want every runtime mode to select its matching tool profile automatically so that permissions reflect the requested process step. |
| US-2 | As an operator, I want Validate mode to expose only read-only capabilities so that validation cannot modify the worktree or execute shell commands. |
| US-3 | As an operator upgrading litefactory, I want obsolete permission labels to be rejected clearly so that legacy labels cannot grant authority. |
| US-4 | As a maintainer, I want applicable configuration regressions preserved under the canonical schema so that the redesign does not discard established safety guarantees. |
| US-5 | As a maintainer, I want production comments and generated guidance to describe the implemented hierarchy so that future changes are based on accurate behavior. |
| US-6 | As a maintainer, I want the repository's complete quality gate and delivery records finished so that the interrupted implementation is demonstrably releasable. |

## Data Model
| Entity | Field | Type | Required | Constraints |
|--------|-------|------|----------|-------------|
| Configuration schema | Existing canonical roots | YAML mappings | Yes | Remains `settings`, `providers`, `tiers`, `agents`, and `tool_profiles`; no schema redesign in this continuation. |
| Tool profile permissions | Mode | Enum | Yes | Exactly `research`, `plan`, `implement`, and `validate`. |
| Tool profile permissions | Agent | Enum | Yes | Every configured agent has settings in every required mode profile. |
| Runtime permission selection | Resolved mode and agent | Derived association | Yes | Research, Plan, Implement, and Validate select like-named profiles; Spike selects Implement. |

### Relationships
- Runtime mode → tool profile via the canonical mode mapping.
- Spike mode → Implement tool profile via the documented exception.
- Tool profile → configured coding agent via the agent-specific settings nested in that profile.
- Configured agent → Validate policy via the mandatory Validate profile entry.

## API / Interface Changes
| Method | Endpoint / Action | Auth | Notes |
|--------|------------------|------|-------|
| Configuration load | Parse and validate canonical `config.yaml` | Local filesystem access | Existing canonical schema remains unchanged; regression behavior is verified rather than redesigned. |
| Turn dispatch | Resolve permissions from runtime mode and selected agent | Existing daemon authority | No issue label may choose or elevate a tool profile. |
| Legacy label handling | Process any `lf:tools=` label | Existing issue-owner controls | Refuse the run with an actionable migration error before agent dispatch. |
| Validate dispatch | Start Claude or Codex with Validate permissions | Existing daemon authority | Claude exposes only Read, Glob, Grep, and Task; Codex uses a read-only sandbox. |

## Business Rules
- The existing canonical configuration design and its prior approved decisions are fixed for this continuation.
- Permission selection MUST be derived from the resolved runtime mode and selected agent.
- Research MUST select Research permissions.
- Plan MUST select Plan permissions.
- Implement MUST select Implement permissions.
- Validate MUST select Validate permissions.
- Spike MUST select Implement permissions.
- Legacy `lf:tools=` labels MUST always refuse execution and MUST NOT select, override, or grant a profile.
- Claude Validate MUST allow only Read, Glob, Grep, and Task on its exposed tool surface.
- Claude Validate MUST reject Bash, Write, Edit, and any non-read-only permission rule.
- Codex Validate MUST require a read-only sandbox and MUST reject workspace-write.
- Deleted tests MUST be classified as retired-behavior coverage or still-applicable regression coverage.
- Still-applicable regression coverage MUST be restored against the canonical schema.
- Tests for deliberately retired behavior MUST be replaced with refusal or canonical-behavior tests and MUST NOT reintroduce compatibility behavior.
- Production comments and generated operator guidance MUST describe the canonical hierarchy and mode-derived permission model.
- Completion MUST include formatting, vet, lint, race-enabled tests, and the repository's full `make check` gate.
- Completion MUST include the implementation workflow's required review, drift, persistent-context, and manifest artifacts.

## Edge Cases & Error Handling
- Validate profile exposes Bash, Write, Edit, or a non-read-only Claude permission → configuration validation fails with the offending mode, agent, and setting identified.
- Codex Validate uses workspace-write → configuration validation fails with an actionable read-only requirement.
- Runtime mode is Spike → permissions resolve deterministically to Implement for the selected agent.
- Runtime mode is Validate → permissions resolve deterministically to Validate for the selected agent.
- Issue includes one or more legacy `lf:tools=` labels → execution is refused before dispatch regardless of label value or other labels.
- Configured agent is missing from any required profile → strict configuration validation fails before daemon operation.
- A deleted test covers a removed legacy grant path → retain no compatibility path; add or preserve refusal coverage where the behavior remains externally observable.
- A deleted test covers general validation, model-tier resolution, runner construction, secret protection, ownership copying, or permission ceilings → restore equivalent canonical-schema coverage.
- A quality-gate failure reveals a behavioral regression → correct within the approved canonical design and rerun the complete gate.
- A stale comment contradicts runtime behavior → update the comment without expanding feature behavior.

## Out of Scope
- Redesigning the canonical configuration hierarchy.
- Reintroducing root-level daemon settings, root-level GitHub configuration, root-level Claude configuration, or literal per-agent model fields.
- Adding providers other than GitHub.
- Adding coding agents other than Claude and Codex.
- Restoring label-selected tool profiles or any legacy compatibility grant path.
- Changing the first-declared-agent default-selection rule.
- Changing tier-only model selection.
- Changing automatic worktree-retention behavior.
- Adding database schema changes, external APIs, integration-test infrastructure, or build tags.
- Automatically rewriting or migrating an operator's existing configuration file.

## Acceptance Criteria
- [ ] Canonical mode tests include Validate and pass for every supported mode value.
- [ ] Daemon-level tests prove Research, Plan, Implement, Validate, and Spike-to-Implement routing for Claude and Codex.
- [ ] Tests prove every `lf:tools=` label refuses execution before either agent is dispatched.
- [ ] Tests prove Claude Validate accepts only Read, Glob, Grep, and Task.
- [ ] Tests prove Claude Validate rejects Bash, Write, Edit, and non-read-only permission rules.
- [ ] Tests prove Codex Validate accepts read-only and rejects workspace-write.
- [ ] Legacy daemon and profile tests no longer expect label-selected profiles or missing-profile fallback.
- [ ] Every previously deleted configuration or tier regression case is documented by its replacement, restored in canonical form, or classified as testing deliberately retired behavior.
- [ ] Applicable coverage remains for strict YAML roots, settings bounds, agent declaration order, tier resolution, profile completeness, agent/profile mismatch, secrets, runner construction, permission ceilings, and slice-copy ownership.
- [ ] Production comments in configuration, store, turn, and report code no longer describe the retired hierarchy or label-selected permission behavior.
- [ ] Generated configuration guidance remains complete and consistent with the canonical schema.
- [ ] `gofmt -l .` reports no files.
- [ ] `go vet ./...` passes.
- [ ] The pinned lint gate passes.
- [ ] The race-enabled full test suite passes.
- [ ] `make check` passes without writing to `dist/` or modifying runtime state.
- [ ] Final code review reports no blocking findings.
- [ ] Drift, persistent-context, and implementation manifest artifacts record the completed behavior and verification results.
