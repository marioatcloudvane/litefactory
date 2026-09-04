# Feature Spec: Complete the Post-Merge Config YAML Rework
# Status: APPROVED
# Date: 2026-09-04

## Goal
- Complete the configuration redesign against audit head `3003244b9d11b6644e1a21d4932ed84243fe8043` so attachment policy is agent-neutral, generated configuration is complete, mode-derived permissions remain secure, and the repository is releasable.

## User Stories
| ID | Story |
|----|-------|
| US-1 | As an operator, I want optional attachment ceilings under global settings so that the same ingestion limits govern every coding agent while omitted values retain safe defaults. |
| US-2 | As an operator, I want Research and Plan to accept attachments by default while Implement and Validate remain disabled so that attachments follow process-stage policy. |
| US-3 | As a Codex-only operator, I want attachment limits and per-mode sandbox policy to work without Claude configuration so that Codex is independent and unambiguous. |
| US-4 | As an operator, I want generated `config.yaml` to show every canonical setting and mode policy so that it is a complete reference. |
| US-5 | As a maintainer, I want obsolete label-selected-profile replies and guidance removed so that code describes only mode-derived permissions. |
| US-6 | As a maintainer, I want attachment routing and settled permission behavior covered at daemon level so that merges cannot silently alter mode policy. |
| US-7 | As a maintainer, I want applicable regressions, comments, quality gates, and delivery records completed so that the redesign is releasable. |

## Data Model
| Entity | Field | Type | Required | Constraints |
|--------|-------|------|----------|-------------|
| `settings` | `attachment_max_files` | Integer | No | Current established default and validation bounds apply when omitted. |
| `settings` | `attachment_max_file_bytes` | Integer | No | Current established default and validation bounds apply when omitted. |
| `settings` | `attachment_max_total_bytes` | Integer | No | Current established default and validation bounds apply when omitted. |
| `settings` | `attachment_download_timeout_seconds` | Integer | No | Current established default and validation bounds apply when omitted. |
| `tool_profiles.permissions.<mode>` | `attachments` | Boolean | No | Generated/default Research and Plan are `true`; Implement and Validate are `false`; explicit canonical values override defaults. |
| `tool_profiles.permissions.<mode>.codex` | `sandbox` | Enum | Yes for configured Codex | Per-mode profile is the sole Codex sandbox selector; Validate remains read-only. |
| `agents.codex` | `default_sandbox` | Removed | No | Rejected by strict decoding; no compatibility alias or fallback. |
| `agents.claude` | Attachment ceiling fields | Removed | No | Rejected by strict decoding; no compatibility alias or fallback. |
| Tool profile permissions | Mode | Enum | Yes | Exactly `research`, `plan`, `implement`, and `validate`; Spike has no separate profile. |

### Relationships
- Global `settings` attachment ceilings → attachment ingestion for every configured coding agent.
- Research → Research profile → attachments enabled by generated/default policy.
- Plan → Plan profile → attachments enabled by generated/default policy.
- Implement → Implement profile → attachments disabled by generated/default policy.
- Validate → Validate profile → attachments disabled by generated/default policy.
- Spike → Implement profile → attachments disabled by generated/default policy.
- Runtime mode and selected agent → mandatory per-mode permissions and Codex sandbox.

## API / Interface Changes
| Method | Endpoint / Action | Auth | Notes |
|--------|------------------|------|-------|
| Configuration load | Parse and validate canonical `config.yaml` | Local filesystem access | Read optional attachment ceilings from `settings`; apply established defaults; reject removed keys. |
| Configuration generation | Create default `config.yaml` | Local filesystem access | Emit all four ceilings, explicit attachment flags for all profiles, and no removed keys. |
| Turn dispatch | Resolve attachment permission from runtime mode | Existing daemon authority | Copy selected profile attachment policy into the turn request; Spike resolves through Implement. |
| Codex dispatch | Resolve sandbox from mode profile | Existing daemon authority | Use only `tool_profiles.permissions.<mode>.codex.sandbox`; no agent fallback. |
| Legacy label handling | Process any `lf:tools=` label | Existing issue-owner controls | Preserve pre-dispatch refusal; remove obsolete profile-selection replies and guidance. |
| Validate dispatch | Start Claude or Codex with Validate permissions | Existing daemon authority | Preserve Claude's read-only surface and Codex's read-only sandbox. |

## Business Rules
- Canonical roots remain `settings`, `providers`, `tiers`, `agents`, and `tool_profiles`.
- Settled provider, declaration-order defaulting, tier-only model selection, mandatory profiles, Validate policy, and Spike mapping MUST remain unchanged.
- Planning and implementation MUST be pinned to audit head `3003244b9d11b6644e1a21d4932ed84243fe8043`.
- All four attachment ceilings MUST move from `agents.claude` to `settings`.
- Every ceiling MUST be optional and retain its current default and bounds when omitted.
- Attachment ceilings MUST work with Codex as the only configured agent.
- Generated/default Research and Plan profiles MUST set `attachments: true`.
- Generated/default Implement and Validate profiles MUST set `attachments: false`.
- Spike MUST inherit Implement attachment policy through the existing mapping.
- Explicit canonical profile values MAY enable or disable attachments where existing validation permits.
- `agents.codex.default_sandbox` MUST be removed without compatibility behavior.
- Each Codex mode profile MUST be the sole source of its sandbox.
- Legacy `lf:tools=` labels MUST continue to refuse execution and MUST NOT grant authority.
- Unreachable old-profile reply kinds, mappings, messages, tests, and comments MUST be removed without compatibility behavior.
- Generated configuration MUST be a complete valid example of the canonical schema.
- Applicable interrupted-redesign regression coverage MUST remain or be restored canonically.
- Completion MUST include formatting, vet, pinned lint, race tests, `make check`, review, drift, context, and manifest artifacts.

## Edge Cases & Error Handling
- Any attachment ceiling omitted → use its current established default.
- All ceilings omitted in Codex-only configuration → load successfully with established defaults.
- Any ceiling violates its existing bound → fail validation with the canonical `settings` field identified.
- Removed ceiling appears under `agents.claude` → strict decoding fails; no alias is accepted.
- `agents.codex.default_sandbox` appears → strict decoding fails; no fallback is accepted.
- Research or Plan selected → selected attachment flag reaches the turn request; generated/default policy enables it.
- Implement or Validate selected → selected attachment flag reaches the turn request; generated/default policy disables it.
- Spike selected → Implement attachment flag reaches the turn request.
- Operator changes a canonical attachment flag → dispatch honors the selected profile value.
- Codex mode profile missing → existing mandatory-profile validation fails before daemon operation.
- Codex Validate uses workspace-write → existing read-only validation fails.
- Any `lf:tools=` label present → execution is refused before dispatch.
- Removed reply kind retains a call site or test reference → compilation or focused tests expose and eliminate it.
- Quality gate reveals a regression → correct it within the canonical design and rerun the complete gate.

## Out of Scope
- Reopening or redesigning the settled canonical hierarchy.
- Reintroducing root-level configuration, literal per-agent models, or an explicit default-agent key.
- Adding providers other than GitHub or agents other than Claude and Codex.
- Adding a separate Spike profile or enabling Spike attachments independently of Implement.
- Enabling attachments by default for Implement or Validate.
- Restoring old-profile reply compatibility, label-selected profiles, or Codex sandbox fallback.
- Changing attachment fetching, ingestion, storage, manifest formats, or existing ceiling values and bounds.
- Automatically rewriting or migrating existing configuration files.
- Adding database changes, external APIs, build tags, or integration-test infrastructure.

## Acceptance Criteria
- [ ] Four attachment ceiling fields exist only under `settings` and retain current defaults and bounds when omitted.
- [ ] Load tests cover explicit ceilings, omission/defaulting, each existing bound, and rejection of former Claude locations.
- [ ] Codex-only configuration loads with omitted ceilings and receives established defaults.
- [ ] `agents.codex.default_sandbox` is absent from types, defaults, validation, selection, template, comments, and tests.
- [ ] Strict decoding rejects `agents.codex.default_sandbox` and former Claude ceiling keys.
- [ ] Per-mode Codex profiles are the only sandbox selectors, including read-only Validate enforcement.
- [ ] Generated configuration includes all four optional ceilings with effective defaults documented.
- [ ] Generated configuration sets attachments true for Research and Plan and false for Implement and Validate.
- [ ] Generated configuration contains the complete hierarchy, parses successfully, and has no removed keys.
- [ ] Daemon tests prove attachment permission reaches dispatch for Research, Plan, Implement, Validate, and Spike-to-Implement.
- [ ] Daemon attachment tests cover Claude and Codex, including an explicit profile override.
- [ ] Existing tests continue to prove automatic permission routing for all modes and both agents.
- [ ] Existing tests continue to prove every `lf:tools=` label refuses execution before dispatch.
- [ ] Existing tests continue to prove Claude Validate restrictions and Codex read-only Validate enforcement.
- [ ] Obsolete reply kinds, render branches, mappings, guidance, comments, and compatibility-only tests are removed.
- [ ] Surviving config, turn, daemon, and report comments accurately describe mode-derived permissions and attachments.
- [ ] Regression coverage remains for strict roots, bounds, declaration order, tiers, profile completeness, agent mismatch, secrets, runners, ceilings, and slice ownership.
- [ ] `gofmt -l .` reports no files.
- [ ] `go vet ./...` passes.
- [ ] Pinned lint passes.
- [ ] Race-enabled full tests pass.
- [ ] `make check` passes without writing to `dist/` or modifying runtime state.
- [ ] Final review reports no blocking findings.
- [ ] Drift, persistent-context, and implementation manifest artifacts record post-merge behavior and verification.
