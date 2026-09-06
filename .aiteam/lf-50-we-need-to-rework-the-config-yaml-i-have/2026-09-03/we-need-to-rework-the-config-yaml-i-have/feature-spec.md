# Feature Spec: Rework config.yaml Structure
# Status: APPROVED
# Date: 2026-09-03

## Goal
- Give litefactory operators one coherent, extensible configuration hierarchy for global settings, providers, ordered coding agents, model tiers, and mode-selected permissions.

## User Stories

| ID | Story |
|----|-------|
| US-1 | As an operator, I want daemon and repository defaults grouped under `settings` so that global behavior is easy to find. |
| US-2 | As an operator, I want forge configuration grouped under `providers` so that GitHub is clear today and other providers can be added later. |
| US-3 | As an operator, I want every coding agent configured under `agents` so that Claude and Codex follow one discoverable structure. |
| US-4 | As an operator, I want the first configured agent to be the default so that I do not maintain a redundant `default_agent` key. |
| US-5 | As an operator, I want model selection expressed by capability tier so that literal vendor model names have one source of truth. |
| US-6 | As a security-conscious operator, I want permissions selected automatically from the run mode so that issue labels cannot choose a permission grant. |
| US-7 | As an operator, I want Research, Plan, Implement, and Validate permission profiles with equivalent intent for every configured agent so that agent choice does not change the workflow's authority. |
| US-8 | As an operator, I want the generated `config.yaml` to show the complete canonical structure so that I can edit it without consulting source code. |

## Data Model

| Entity | Field | Type | Required | Constraints |
|--------|-------|------|----------|-------------|
| Config | `settings` | Settings mapping | Yes | Exactly one mapping; unknown sibling keys rejected. |
| Config | `providers` | Providers mapping | Yes | Exactly one mapping; only `github` accepted in this release. |
| Config | `agents` | Ordered agent mapping | Yes | At least one entry; accepted keys are `claude` and `codex`; YAML declaration order is significant and preserved. |
| Config | `tiers` | Tiers mapping | Yes | Contains `model_by_tier`; unknown keys rejected. |
| Config | `tool_profiles` | ToolProfiles mapping | Yes | Contains exactly one `permissions` mapping. |
| Settings | `poll_interval_seconds` | Integer | No | Default `30`; range `10..60`; explicit zero rejected. |
| Settings | `max_concurrent_runs` | Integer | No | Default `3`; range `1..16`; daemon-wide rather than per repository. |
| Settings | `default_base_branch` | String | No | Default empty; empty means no fallback; otherwise a valid branch name. |
| Settings | `feature_prefix` | String | No | Default `lf`; valid first branch-path segment. |
| Settings | `worktree_root` | String | No | Default empty; empty uses per-repository sibling location; non-empty value must be absolute and clean. |
| Settings | `worktree_retain_days` | Integer | No | Default `14`; range remains the current validated range; retention behavior remains unchanged. |
| Settings | `log_level` | Enum | No | Default `info`; one of `debug`, `info`, `warn`, `error`. |
| Providers | `github` | GitHubProvider mapping | Yes | Sole supported provider; unknown provider names rejected. |
| GitHubProvider | `api_base` | URL string | No | Default `https://api.github.com`; current URL validation retained. |
| GitHubProvider | `token_keychain` | Keychain mapping | Conditional | Exactly one of `token_keychain` and `token_env`; keychain remains required for launchd operation. |
| GitHubProvider | `token_env` | String | Conditional | Environment-variable name only; exactly one credential reference method. |
| Keychain | `service` | String | Yes | Current non-empty validation retained. |
| Keychain | `account` | String | Yes | Current non-empty validation retained. |
| Agents | `claude` | ClaudeAgent mapping | Conditional | Optional; fully typed and active when present; no top-level `claude` alternative. |
| Agents | `codex` | CodexAgent mapping | Conditional | Optional; at least one of `claude` and `codex` required. |
| ClaudeAgent | `binary` | String | No | Default `claude`; existing executable and launchd path validation retained. |
| ClaudeAgent | `model_tier` | Tier enum | No | Default `balanced`; one of `flagship`, `balanced`, `fast`; selected tier must exist at `tiers.model_by_tier.claude`. |
| ClaudeAgent | `autocompact` | String | No | Default `100k`; current `auto` or `100k..1M` validation retained. |
| ClaudeAgent | `settings_path` | String | No | Empty resolves to `$LF_HOME/claude-settings.json`; current path validation retained. |
| ClaudeAgent | `tool_ceiling` | String sequence | Yes | Non-empty; hard upper bound for every Claude permission profile; existing forbidden-tool and forbidden-flag policy retained. |
| ClaudeAgent | `timeout_seconds` | Integer | No | Default `1800`; current timeout bounds retained. |
| ClaudeAgent | `forward_anthropic_env` | Boolean | No | Default `false`; current secret-forwarding behavior retained. |
| CodexAgent | `binary` | String | No | Default `codex`; existing executable and launchd path validation retained. |
| CodexAgent | `model_tier` | Tier enum | No | Default `balanced`; one of `flagship`, `balanced`, `fast`; selected tier must exist at `tiers.model_by_tier.codex`. |
| CodexAgent | `timeout_seconds` | Integer | No | Default `1800`; current timeout bounds retained. |
| CodexAgent | `writable_roots` | String sequence | No | Default empty; every entry absolute and clean; current sandbox widening behavior retained. |
| CodexAgent | `network_access` | Boolean | No | Default `false`; current Codex network behavior retained. |
| CodexAgent | `allow_user_config` | Boolean | No | Default `false`; current Codex user-config isolation behavior retained. |
| Tiers | `model_by_tier` | Agent-to-tier mapping | Yes | May contain only configured/supported agent keys; each inner key is `flagship`, `balanced`, or `fast`; values are validated literal model identifiers. |
| ToolProfiles | `permissions` | Mode-to-agent mapping | Yes | Exactly the keys `research`, `plan`, `implement`, `validate`; all four required. |
| PermissionProfile | `claude` | ClaudePermission mapping | Conditional | Required when Claude is configured; forbidden when Claude is not configured. |
| PermissionProfile | `codex` | CodexPermission mapping | Conditional | Required when Codex is configured; forbidden when Codex is not configured. |
| ClaudePermission | `tools` | String sequence | Yes | Non-empty subset of `agents.claude.tool_ceiling`; current tool-name validation retained. |
| ClaudePermission | `allowed_tools` | String sequence | No | Every rule names a tool in the same profile's `tools`; unqualified or wildcard Bash remains forbidden. |
| CodexPermission | `sandbox` | Enum | Yes | One of `read-only`, `workspace-write`; `danger-full-access` and equivalents rejected. |

### Relationships

- `Config.providers.github` → existing GitHub client and credential resolver.
- First key in `Config.agents` → default runner for unlabelled issues and mode classification.
- `Config.agents.<agent>.model_tier` → `Config.tiers.model_by_tier.<agent>.<tier>` → literal model passed to that runner.
- Selected workflow mode → `Config.tool_profiles.permissions.<profile>` → active agent stanza → runner permissions.
- `research` mode → `research` permission profile.
- `plan` mode → `plan` permission profile.
- `implement` mode → `implement` permission profile.
- `validate` mode → `validate` permission profile.
- `spike` mode → `implement` permission profile.
- Claude permission `tools` → subset of `Config.agents.claude.tool_ceiling`.

## Canonical YAML Hierarchy

- `settings`
  - `poll_interval_seconds`
  - `max_concurrent_runs`
  - `default_base_branch`
  - `feature_prefix`
  - `worktree_root`
  - `worktree_retain_days`
  - `log_level`
- `providers`
  - `github`
    - `api_base`
    - exactly one of `token_keychain` or `token_env`
- `agents`
  - ordered `claude` and/or `codex` entries
  - first declared entry is the default agent
  - each configured agent selects one `model_tier`
  - agent-global execution and safety settings remain inside that agent entry
- `tiers`
  - `model_by_tier`
    - agent
      - `flagship`
      - `balanced`
      - `fast`
- `tool_profiles`
  - `permissions`
    - `research`
      - configured agent permission stanzas
    - `plan`
      - configured agent permission stanzas
    - `implement`
      - configured agent permission stanzas
    - `validate`
      - configured agent permission stanzas

## API / Interface Changes

| Method | Endpoint / Action | Auth | Notes |
|--------|------------------|------|-------|
| Config file | Decode `$LF_HOME/config.yaml` | Local file permissions | Accept canonical top-level keys `settings`, `providers`, `agents`, `tiers`, `tool_profiles`; retain strict single-document and unknown-key rejection. |
| Config file | Select default agent | Local file permissions | Use first YAML key declared under `agents`; remove `default_agent`. |
| Config file | Select runner model | Local file permissions | Remove per-agent literal `model`; resolve mandatory/defaulted `model_tier` through `tiers.model_by_tier`. |
| Config file | Select run permissions | Local file permissions | Choose permission profile from resolved run mode; no issue label may select a profile. |
| Mode vocabulary | Parse `validate` / `Mode: Validate` | Existing owner and issue controls | Add Validate as a canonical explicit mode with a distinct permission profile; workflow behavior beyond permission selection follows the existing mode-routing contract. |
| Mode vocabulary | Parse `spike` / `Mode: Spike` | Existing owner and issue controls | Preserve Spike and select the Implement permission profile because Spike may make small code changes. |
| GitHub labels | Encounter `lf:tools=<name>` | Existing issue label controls | Refuse the run with an actionable reply stating that permissions are mode-selected; do not ignore or honor the stale label. |
| GitHub labels | Encounter `Coding Agent: <name>` | Existing issue label controls | Preserve explicit runner selection; absent label selects first configured agent. |
| CLI | `litefactory init` generated config | Existing local CLI access | Write a fully commented, complete canonical file including both supported agent examples, all tier mappings, and all four permission profiles. |
| CLI | Daemon startup summary | Existing local CLI access | Report first configured agent as `default agent`; retain remaining startup information using new config paths. |

## Business Rules

- `settings.default_agent` and top-level `default_agent` do not exist in the canonical schema.
- YAML declaration order under `agents` is configuration data and MUST determine the default agent.
- Reordering agent keys MUST change the default without changing explicit `Coding Agent: ...` routing.
- Mode classification MUST use the first configured agent and MUST remain tool-free.
- Literal per-agent `model` keys MUST be removed.
- Per-agent `model_tier` MUST remain because `tiers.model_by_tier` defines mappings but does not select a mapping entry.
- Agent model selection MUST resolve exactly once at startup and fail if the selected tier has no model for that agent.
- Existing specialist-specific model declarations outside `config.yaml` are out of this schema and remain unchanged.
- Agent-independent intent MUST be consistent across same-named Claude and Codex permission stanzas.
- Research permissions MUST allow repository inspection and writing the required research artifact but MUST NOT grant implementation-oriented mutation or unrestricted shell access.
- Plan permissions MUST allow repository inspection, planning-artifact creation, and orchestrator delegation but MUST NOT grant production-code editing or unrestricted shell access.
- Implement permissions MUST allow production-code and test editing, delegation, and project-scoped build/test/format commands while preserving existing prohibitions on commit, push, destructive git, and unrestricted Bash.
- Validate permissions MUST allow repository inspection and project-scoped build/test/vet/format/lint commands but MUST NOT grant source editing, committing, pushing, destructive git, or unrestricted Bash.
- Codex `read-only` MUST be used only where the workflow writes no required artifact; profiles that require artifact or code writes MUST use `workspace-write`.
- Profile validation MUST compare agent-specific mechanics by intent rather than translate Claude tool names into Codex sandbox values.
- All four permission profiles MUST contain a stanza for every configured agent; missing stanzas MUST be startup errors rather than run-time fallback.
- Permission selection MUST occur after an inferred mode is classified and before the work run starts.
- A profile MUST NOT widen any agent-global ceiling, writable-root list, network setting, environment-forwarding setting, or user-config policy.
- Existing literal-secret scanning MUST recurse through every new section and refuse credentials at any string location.
- Existing normalization, deterministic error ordering, forbidden-flag scanning, path validation, and security policy MUST apply at the new dotted paths.
- The generated config MUST derive values from the same defaults used by runtime decoding.
- The generated config MUST show safe, runnable values rather than commented-out structural placeholders.
- GitHub MUST remain the only provider implemented in this change.
- Configured agents MUST be initialized, validated, and registered in YAML order.
- Unsupported provider or agent names MUST fail startup and identify the exact dotted path.
- The new schema is canonical-only; legacy top-level operational keys, top-level `github`, top-level `claude`, nested Claude `tool_profiles`, agent literal `model`, and `default_agent` MUST fail with an actionable replacement path.
- Litefactory MUST NOT rewrite an existing operator config automatically.

## Edge Cases & Error Handling

- Empty `agents` mapping → startup error requiring at least one supported agent.
- Duplicate YAML agent key → parse error; no last-key-wins behavior.
- First agent fails executable or configuration validation → daemon startup fails; no fallback to the next agent.
- Explicit Coding Agent label names an unconfigured supported agent → existing unknown/unavailable-agent refusal; no fallback to default.
- Missing selected model-tier mapping → startup error naming `agents.<agent>.model_tier` and `tiers.model_by_tier.<agent>.<tier>`.
- Literal `model` appears under an agent → unknown-key error directing operator to `model_tier` and `tiers.model_by_tier`.
- Missing canonical permission profile → startup error naming `tool_profiles.permissions.<mode>`.
- Missing configured-agent stanza in a canonical profile → startup error naming the profile and agent.
- Permission stanza exists for an unconfigured agent → startup error rather than dormant configuration.
- Free-text instruction has no Mode label → classify tool-free, then select the resulting canonical profile.
- Classification returns unknown mode → existing classification failure path; no profile fallback.
- Spike run → use Implement permissions and record `implement` as the selected permission profile while retaining `spike` as the run mode.
- Validate run → use Validate permissions and persist `validate` as both mode and selected permission profile.
- Any `lf:tools=` label → refuse before dispatch with no permission fallback.
- Both GitHub credential methods configured → existing startup error at new provider paths.
- No GitHub credential method configured where current runtime requires one → existing startup failure at new provider paths.
- Unknown legacy top-level key → startup error includes its canonical replacement path where one exists.
- Additional YAML document → startup error; first document never partially accepted.

## Assumptions

- `validate` becomes a first-class mode because automatic permission selection requires a resolvable mode name.
- `spike` remains supported and maps to `implement` permissions because it may perform small code changes and the requested profile list excludes `spike`.
- `agents` remains a YAML mapping, not a sequence; a custom ordered decode preserves mapping-key order for default selection.
- `tool_profiles.permissions.<mode>.<agent>` is the intended interpretation of the requested `permissions → profile name → coding agent → settings` hierarchy.
- `model_tier` remains per agent as the selection key; `tiers.model_by_tier` remains the central literal-model lookup table.
- Backward-compatible legacy decoding and automatic config rewriting are not required; startup diagnostics provide migration guidance.
- Worktree retention semantics are not expanded; this change relocates the existing setting only.

## Out of Scope

- GitLab, Bitbucket, or any provider implementation other than GitHub.
- Automatic editing, backup, or migration of an existing `$LF_HOME/config.yaml`.
- Backward-compatible execution from the legacy config hierarchy.
- Changes to credential storage or resolution.
- Automatic worktree cleanup or changes to retention behavior.
- Repository registration schema changes.
- Changes to Coding Agent label syntax or explicit agent-selection behavior.
- Per-repository or per-issue permission overrides.
- Issue-label-selected permission profiles.
- Translation of Claude tool rules into Codex sandbox settings.
- Changes to specialist model frontmatter or specialist tier assignments.
- Adding coding agents other than Claude and Codex.

## Acceptance Criteria

- [ ] A canonical config with all global fields under `settings` decodes to the same runtime values and defaults as the current equivalents.
- [ ] No canonical daemon or repository-global field remains at the document root.
- [ ] GitHub configuration is accepted only at `providers.github` and powers all existing GitHub operations.
- [ ] `agents.claude` is fully typed, validated, and used by the Claude runner.
- [ ] `agents.codex` remains fully typed, validated, and used by the Codex runner.
- [ ] A config with only Claude starts and uses Claude as default.
- [ ] A config with only Codex starts and uses Codex as default.
- [ ] A config declaring Codex before Claude defaults unlabelled runs and classification to Codex.
- [ ] A config declaring Claude before Codex defaults unlabelled runs and classification to Claude.
- [ ] Explicit Coding Agent labels still select any configured non-default agent.
- [ ] `default_agent` is absent from generated configuration and rejected with migration guidance when supplied.
- [ ] Per-agent literal `model` is absent from generated configuration and rejected with guidance when supplied.
- [ ] Each configured agent defaults `model_tier` to `balanced` and resolves it through its tier table.
- [ ] Missing, unknown, and invalid selected tier mappings fail startup without fallback.
- [ ] Generated configuration contains `tool_profiles.permissions.research`, `.plan`, `.implement`, and `.validate`.
- [ ] Every generated permission profile contains both Claude and Codex stanzas with equivalent workflow intent.
- [ ] Research and Plan profiles cannot edit production-code paths through their Claude tool grants.
- [ ] Implement permissions allow source/test edits and the project's approved verification commands without allowing commit or push.
- [ ] Validate permissions cannot edit source files and can run the project's approved verification commands.
- [ ] Explicit Research, Plan, Implement, and Validate modes select the same-named permission profile automatically.
- [ ] An inferred mode selects permissions only after classification succeeds.
- [ ] Spike selects Implement permissions without changing the persisted run mode from `spike`.
- [ ] An `lf:tools=` label refuses dispatch and explains that permissions are chosen from the mode.
- [ ] Missing mode profiles or configured-agent stanzas fail at startup rather than during a run.
- [ ] Claude profile tools outside `agents.claude.tool_ceiling` fail startup.
- [ ] Existing forbidden Bash, forbidden flag, absolute-path, timeout, range, URL, log-level, unknown-key, duplicate-key, single-document, and literal-secret checks pass against new paths.
- [ ] The generated config is a complete, uncommented structural example that validates without edits except supplying a credential reference appropriate to the environment.
- [ ] Runtime defaults and generated config values are derived from one source and are covered by an equality test.
- [ ] Legacy keys fail with errors that name their canonical replacement paths.
- [ ] Existing configs are never modified automatically.
- [ ] Daemon startup output reports the first configured agent as the default.
- [ ] Run logs and metadata continue to record the run mode, resolved model, effective permissions, and selected automatic permission profile.
