# Research Brief — we-need-to-rework-the-config-yaml-i-have

## Request

[O] Explain the sections and structure of litefactory's main `config.yaml`, research everything that can be configured, provide examples and an outline, and show one complete configuration file so the owner can understand the current format before editing it.

## Codebase Context

### Ownership and lifecycle

- [V] Configuration is owned by the leaf package `internal/config`; the typed root schema is `Config`, with nested GitHub, Claude, tier, tool-profile, and agent structures (`internal/config/config.go:40`, `internal/config/config.go:99`, `internal/config/config.go:263`, `internal/config/tier.go:105`).
- [V] `$LF_HOME` defaults to `~/.litefactory`, may be overridden by `LF_HOME`, and the main file is `$LF_HOME/config.yaml` (`internal/config/paths.go:10`, `internal/config/paths.go:44`).
- [V] `litefactory init` creates the default file atomically only when it is absent, then loads it; it does not overwrite an existing configuration (`internal/cli/cmd_init.go:72`).
- [V] Loading starts with in-memory defaults, strictly overlays YAML, rejects unknown fields and multiple YAML documents, normalizes strings, fills path-dependent values, materializes the Claude tool ceiling, then validates (`internal/config/config.go:467`, `internal/config/config.go:496`, `internal/config/config.go:572`). Empty YAML is therefore valid and means “use all defaults.”
- [V] Validation accumulates independent failures so one load can report several bad settings (`internal/config/validate.go:55`). All decoded strings are also scanned for likely literal secrets; configuration is designed to contain credential references, not credential values (`internal/config/secretscan.go:12`, `internal/config/secretscan.go:73`).
- [V] The generated starter configuration is rendered from the same defaults and round-tripped in tests, reducing drift between defaults and the file emitted by `init` (`internal/config/template.go:11`, `internal/config/template.go:272`, `internal/config/template_test.go:84`).

### Complete section outline

```yaml
default_agent: <canonical agent display name>
poll_interval_seconds: <integer>
max_concurrent_runs: <integer>
default_base_branch: <branch or empty>
feature_prefix: <branch prefix>
worktree_root: <absolute path or empty>
worktree_retain_days: <integer>
log_level: <debug|info|warn|error>

github:
  api_base: <http(s) URL>
  token_keychain:                 # choose this OR token_env
    service: <keychain service>
    account: <keychain account>
  token_env: <environment variable name>

claude:
  binary: <bare executable or absolute path>
  model: <literal model>
  model_tier: <flagship|balanced|fast>
  autocompact: <auto|100k..1000k|1M>
  tools: [<bare tool names>]
  settings_path: <path or empty>
  allowed_tools: [<permission rules>]
  tool_ceiling: [<bare tool names>]
  tool_profiles:
    <profile-name>:
      claude:
        tools: [<bare tool names>]
        allowed_tools: [<permission rules>]
      codex:
        sandbox: <read-only|workspace-write>
  timeout_seconds: <integer>
  forward_anthropic_env: <boolean>

tiers:
  model_by_tier:
    claude:
      flagship: <model>
      balanced: <model>
      fast: <model>
    codex:
      flagship: <model>
      balanced: <model>
      fast: <model>

agents:
  claude: <reserved; currently untyped and unused>
  codex:
    binary: <bare executable or absolute path>
    model: <literal model>
    model_tier: <flagship|balanced|fast>
    default_sandbox: <read-only|workspace-write>
    timeout_seconds: <integer>
    writable_roots: [<absolute paths>]
    network_access: <boolean>
    allow_user_config: <boolean>
```

### Top-level daemon and repository behavior

| Key | Default | Accepted / behavior |
|---|---:|---|
| `default_agent` | `Claude` | [V] Canonical display name matching `^[A-Z][A-Za-z0-9_-]{0,39}$`; selects unlabelled work, but the selected runner must actually be configured (`internal/config/validate.go:44`, `internal/daemon/setup.go:265`). |
| `poll_interval_seconds` | `30` | [V] Inclusive range 10–60 (`internal/config/validate.go:13`, `internal/config/validate.go:67`). |
| `max_concurrent_runs` | `3` | [V] Inclusive range 1–16 (`internal/config/validate.go:16`, `internal/config/validate.go:71`). |
| `default_base_branch` | empty | [V] Empty or a validated branch-like name; passed to repository setup as its fallback (`internal/config/validate.go:79`, `internal/cli/cmd_repo.go:109`). |
| `feature_prefix` | `lf` | [V] Required; `/` and `..` are forbidden (`internal/config/validate.go:83`). |
| `worktree_root` | empty | [V] Empty uses the derived sibling location; an explicit value must be absolute (`internal/config/validate.go:88`). |
| `worktree_retain_days` | `14` | [V] Inclusive range 1–3650. It currently affects cleanup messaging/retention notes; no automatic sweeper exists (`internal/config/validate.go:21`, `internal/cli/cmd_task.go:76`). |
| `log_level` | `info` | [V] Case-normalized textual `debug`, `info`, `warn`, or `error`; logger bootstrap falls back to info when config cannot load (`internal/config/validate.go:145`, `cmd/litefactory/main.go:95`). |

### `github`

- [V] `api_base` defaults to `https://api.github.com`; it must be an HTTP(S) URL with a host and no query or fragment, and a trailing slash is removed (`internal/config/validate.go:156`, `internal/config/validate.go:286`).
- [V] Credentials are indirect and mutually exclusive: either `token_keychain.service` plus `token_keychain.account`, or `token_env` naming an environment variable. `init` may proceed without a source, but daemon startup requires one it can resolve (`internal/config/validate.go:172`, `internal/cli/cmd_daemon.go:127`).

Examples:

```yaml
# macOS Keychain
github:
  api_base: "https://api.github.com"
  token_keychain:
    service: "litefactory"
    account: "github-bot"
```

```yaml
# Environment indirection (the token itself does not belong in YAML)
github:
  api_base: "https://github.example.com/api/v3"
  token_env: "LITEFACTORY_GITHUB_TOKEN"
```

### `claude`

- [V] Defaults are executable `claude`, literal model `sonnet`, tier `balanced`, autocompaction `100k`, timeout 1800 seconds, no Anthropic environment forwarding, and the tool set `Read`, `Glob`, `Grep`, `Edit`, `Write`, `Bash`, `Task` (`internal/config/config.go:419`, `internal/config/policy.go:69`).
- [V] `model_tier`, when non-empty, takes precedence over `model`. Allowed tier names are `flagship`, `balanced`, and `fast`, and the Claude tier must resolve in `tiers.model_by_tier.claude` (`internal/config/config.go:283`, `internal/config/validate.go:104`).
- [V] `autocompact` accepts `auto`, values from `100k` through `999k`, `1000k`, or `1M` (`internal/config/policy.go:28`).
- [V] `tools` must be a non-empty list of bare tool names. `allowed_tools` may contain a bare `Tool` or qualified `Tool(rule)`, but cannot contain `*`, an unrestricted bare `Bash`, malformed rules, or rules for tools absent from the applicable tool set (`internal/config/policy.go:135`, `internal/config/policy.go:334`).
- [V] `tool_ceiling` is the maximum surface selectable by profiles and must include every default tool. If missing or explicitly empty, it becomes a copy of `tools` (`internal/config/policy.go:273`, `internal/config/toolprofiles_test.go:82`).
- [V] Profile names are at most 40 lowercase letters, digits, underscores, or dashes, start alphanumeric, and contain a Claude stanza, Codex stanza, or both. Claude profile tools cannot exceed the ceiling. Codex profile sandboxes are `read-only` or `workspace-write` (`internal/config/policy.go:62`, `internal/config/policy.go:334`, `internal/config/policy.go:411`).
- [V] A tools-profile label is a permission selector. Its safety boundary is the configured ceiling; profile selection can never add a Claude tool outside that ceiling (`internal/config/config.go:254`, `internal/daemon/tools.go:99`).
- [V] Timeout is inclusive 60–86400 seconds. An empty `settings_path` resolves to `$LF_HOME/claude-settings.json` (`internal/config/validate.go:203`, `internal/config/config.go:488`).

### `tiers`

- [V] The default mappings are Claude: `opus` / `sonnet` / `haiku`, and Codex: `gpt-5.6-sol` / `gpt-5.6-terra` / `gpt-5.6-luna`, ordered flagship / balanced / fast (`internal/config/config.go:406`, `internal/config/tier.go:148`).
- [V] Tiers make agent model policy centrally replaceable. A configured agent tier takes precedence over its literal model (`internal/config/config.go:283`, `internal/codex/runner.go:84`).

### `agents`

- [V] `agents.codex` is optional. When present, it configures executable, literal model or model tier, default sandbox, timeout, additional writable roots, network access, and whether the user's own Codex configuration is allowed (`internal/config/config.go:125`, `internal/config/validate.go:230`).
- [V] The Codex executable, sandbox, and timeout must be valid; sandbox accepts exactly `read-only` or `workspace-write`, timeout is 60–86400 seconds, and every writable root must be absolute (`internal/config/validate.go:230`, `internal/config/validate.go:262`).
- [V] `network_access` and `allow_user_config` are opt-in booleans. Writable roots widen the sandbox. Allowing user configuration removes litefactory's normal CLI isolation from ambient user settings (`internal/config/config.go:156`, `internal/codex/runner.go:84`).
- [V] Executable fields accept either a bare command name resolved through startup `PATH` or an absolute path. Any value containing `/` must be absolute; absolute paths are operationally more reliable under launchd (`internal/config/validate.go:191`, `internal/config/executable.go:72`).
- [V] `agents.claude` is currently an untyped reserved field. It cannot be present together with top-level `claude`, and it does not configure the active Claude runner, which is still built from the top-level section (`internal/config/config.go:99`, `internal/config/config.go:533`, `internal/daemon/runners.go:30`). It is shown in the outline for schema completeness but intentionally omitted from the valid full example.

### Complete illustrative `config.yaml`

[V] This is a structurally complete, valid example of all currently operational settings. It deliberately chooses `github.token_env` rather than the mutually exclusive Keychain form, and omits the reserved `agents.claude` placeholder. Replace example absolute paths with paths that exist on the target Mac; loading validates that they are absolute, while runner startup separately resolves executables.

```yaml
default_agent: "Codex"
poll_interval_seconds: 30
max_concurrent_runs: 3
default_base_branch: "main"
feature_prefix: "lf"
worktree_root: "/Users/you/dev/.lf-worktrees"
worktree_retain_days: 14
log_level: "info"

github:
  api_base: "https://api.github.com"
  token_env: "LITEFACTORY_GITHUB_TOKEN"

claude:
  binary: "/opt/homebrew/bin/claude"
  model: "sonnet"
  model_tier: "balanced"
  autocompact: "100k"
  tools:
    - "Read"
    - "Glob"
    - "Grep"
    - "Edit"
    - "Write"
    - "Bash"
    - "Task"
  settings_path: ""
  allowed_tools:
    - "Read"
    - "Glob"
    - "Grep"
    - "Edit"
    - "Write"
    - "Bash(git status:*)"
    - "Bash(git diff:*)"
    - "Bash(git log:*)"
    - "Bash(git show:*)"
    - "Bash(git rev-parse:*)"
    - "Bash(git ls-tree:*)"
    - "Bash(git merge-base:*)"
    - "Bash(git rev-list:*)"
    - "Bash(mkdir:*)"
    - "Bash(go build:*)"
    - "Bash(go test:*)"
    - "Bash(go vet:*)"
    - "Bash(gofmt:*)"
  tool_ceiling:
    - "Read"
    - "Glob"
    - "Grep"
    - "Edit"
    - "Write"
    - "Bash"
    - "Task"
  tool_profiles:
    research:
      claude:
        tools: ["Read", "Glob", "Grep", "Bash", "Task"]
        allowed_tools:
          - "Read"
          - "Glob"
          - "Grep"
          - "Bash(git status:*)"
          - "Bash(git log:*)"
      codex:
        sandbox: "read-only"
    implementation:
      claude:
        tools: ["Read", "Glob", "Grep", "Edit", "Write", "Bash", "Task"]
        allowed_tools:
          - "Read"
          - "Glob"
          - "Grep"
          - "Edit"
          - "Write"
          - "Bash(go build:*)"
          - "Bash(go test:*)"
          - "Bash(go vet:*)"
          - "Bash(gofmt:*)"
      codex:
        sandbox: "workspace-write"
  timeout_seconds: 1800
  forward_anthropic_env: false

tiers:
  model_by_tier:
    claude:
      flagship: "opus"
      balanced: "sonnet"
      fast: "haiku"
    codex:
      flagship: "gpt-5.6-sol"
      balanced: "gpt-5.6-terra"
      fast: "gpt-5.6-luna"

agents:
  codex:
    binary: "/opt/homebrew/bin/codex"
    model: "gpt-5.6-terra"
    model_tier: "balanced"
    default_sandbox: "workspace-write"
    timeout_seconds: 1800
    writable_roots:
      - "/Users/you/Library/Caches/go-build"
      - "/Users/you/go/pkg/mod"
    network_access: true
    allow_user_config: true
```

## Domain / Problem Space

[V] The file combines four distinct concerns: daemon scheduling and repository defaults; GitHub endpoint and credential lookup; Claude execution permissions and profiles; and multi-agent model/sandbox policy. This makes it both ordinary application configuration and a security boundary. Unknown-key rejection helps catch misspellings, while the tool ceiling, Codex sandbox, network flag, writable roots, and user-config isolation control how much authority a run receives (`internal/config/config.go:4`, `internal/config/policy.go:273`, `internal/codex/runner.go:84`).

[V] The literal `model` fields and abstract `model_tier` fields coexist. When a tier is set, changing the literal model alongside it has no effect. Operators who want direct model pinning must leave the relevant tier empty; operators who want centralized policy should set tiers and change the mapping table (`internal/config/config.go:283`, `internal/codex/runner.go:84`).

## Constraints & Risks

- [V] Unknown fields and extra YAML documents fail loading. A future rework must preserve deliberate compatibility behavior or provide migration guidance (`internal/config/config.go:496`).
- [V] Literal secrets are rejected. Examples must continue to demonstrate only Keychain or environment-variable references (`internal/config/secretscan.go:73`).
- [V] `token_keychain` and `token_env` cannot both be active; a “show every key” example cannot include both and remain valid (`internal/config/validate.go:172`).
- [V] Top-level `claude` and `agents.claude` also conflict. More importantly, the latter is reserved and currently does not drive execution (`internal/config/config.go:533`, `internal/daemon/runners.go:30`).
- [V] Profiles can be selected by issue label, so an overly broad configured ceiling is an operational authorization risk even though profile validation is fail-closed (`internal/daemon/tools.go:99`, `internal/config/policy.go:273`).
- [V] The generated starter template does not show accepted Codex fields `model`, `model_tier`, or `allow_user_config`, creating documentation/schema drift (`internal/config/template.go:226`, `internal/config/config.go:125`).
- [V] Comments describing Codex tier/sandbox support have drifted from executable behavior: mappings and two sandbox values now exist although some comments still describe them as unmeasured or absent (`internal/config/config.go:141`, `internal/config/tier.go:148`, `internal/config/validate.go:244`).
- [V] `worktree_retain_days` sounds like an automatic cleanup setting but no automatic sweeping currently occurs (`internal/config/config.go:80`, `internal/cli/cmd_task.go:76`).
- [V] A syntactically valid absolute executable path can still fail when the daemon starts if the binary is absent or not executable (`internal/config/executable.go:72`).

## Open Questions

- [V] Should a later configuration rework finally move active Claude settings under `agents.claude`, remove the reserved field, or formally retain the top-level section for compatibility?
- [V] Should the generated default file expose every accepted Codex option, especially model selection and ambient user-config access, or intentionally remain a minimal starter with a separate exhaustive reference?
- [V] Should `worktree_retain_days` be renamed/documented to match its current non-automatic behavior, or should cleanup automation be added in a later implementation?
- [O] The owner has not yet specified whether the desired rework prioritizes backward compatibility, a simpler operator-facing layout, or a clean breaking schema. Plan mode must settle that before changing the format.

## Recommended scope for Plan mode

[V] Plan mode should use this inventory to define the desired operator experience, decide the fate of top-level `claude` versus `agents.claude`, decide whether exhaustive reference material belongs in the generated file or separate documentation, specify backward-compatibility/migration behavior, and align comments/templates/tests with the resulting schema. It should preserve strict decoding, indirect credential handling, secret scanning, permission ceilings, sandbox controls, and explicit validation. It should not add automatic worktree cleanup or redesign runtime orchestration unless the owner explicitly brings those behaviors into scope.

## Evaluation — Research run 0002

### Overall assessment

[V] The current configuration implementation is technically strong but conceptually unfinished. Loading, validation, credential indirection, permission constraints, and configuration-to-runner translation are defensive and well tested. The user-facing structure, however, exposes an incomplete migration from a Claude-centric schema to a multi-agent schema. The result is safe in most failure cases but harder to understand than its size warrants, and one accepted field can be silently ignored.

**Verdict:** retain the validation and security machinery, but redesign the public hierarchy and its documentation as one compatibility-aware change. The current format is serviceable for experienced maintainers, not yet a coherent long-term operator interface.

### Evaluation scorecard

| Dimension | Assessment | Evidence |
|---|---|---|
| Validation and failure behavior | Strong | [V] Known-field decoding, one-document enforcement, aggregated path-specific errors, bounded numeric/enumerated values, and literal-secret scanning make most mistakes explicit (`internal/config/config.go:511`, `internal/config/validate.go:55`, `internal/config/secretscan.go:94`). |
| Security defaults | Strong, with two exceptions | [V] Credentials are referenced rather than embedded; Claude profiles cannot exceed their configured ceiling; Codex network and ambient user configuration are opt-in (`internal/config/policy.go:273`, `internal/codex/runner.go:91`). Plain HTTP API endpoints remain allowed, and anyone able to apply a tools-profile label can select configured authority (`internal/config/validate.go:159`, `internal/daemon/tools.go:99`). |
| Conceptual organization | Weak | [V] Agent-neutral model tiers live at the root, Codex lives under `agents`, active Claude remains top-level, and cross-agent tool profiles remain nested below Claude (`internal/config/config.go:47`, `internal/config/config.go:315`). |
| Discoverability | Mixed | [V] Generated comments explain many operational tradeoffs well, but the repository has no substantive standalone configuration guide and the generated Codex example omits three supported fields (`README.md:1`, `internal/config/template.go:226`, `internal/config/config.go:125`). |
| Behavioral consistency | Weak in migration seams | [V] `agents.claude` is accepted as arbitrary YAML, intentionally not validated, and not consumed; a file can therefore appear to configure Claude while execution uses top-level defaults (`internal/config/config.go:99`, `internal/config/validate.go:218`, `internal/config/agents_test.go:294`, `internal/daemon/runners.go:31`). |
| Default experience | Mixed | [V] An empty file receives predictable defaults, but the generated file cannot start the daemon without credentials, and a bare executable name is fragile under launchd/Homebrew path differences (`internal/config/config.go:403`, `internal/config/template.go:75`, `internal/config/template.go:230`). |
| Compatibility readiness | Weak | [V] Strict decoding and non-overwriting initialization mean a renamed or removed key breaks an existing hand-maintained file, while no schema version or migration mechanism exists (`internal/config/config.go:513`, `internal/config/template.go:321`). |
| Test quality | Strong but documentation-incomplete | [V] Tests directly cover defaults/template equivalence, validation, secret and forbidden-flag rules, profile routing/refusal, and process arguments. They prove documented active values match defaults, but do not require every live schema key and semantic caveat to appear in generated documentation (`internal/config/template_test.go:15`, `internal/config/purity_test.go:82`, `internal/codex/runner_test.go:39`, `internal/daemon/profile_agent_test.go:56`). |

### What should be preserved

- [V] Keep strict unknown-key and extra-document rejection. It turns misspellings into startup errors instead of silent behavior (`internal/config/config.go:511`).
- [V] Keep overlay-on-defaults semantics, centralized defaults, and template/default round-trip tests (`internal/config/config.go:403`, `internal/config/template_test.go:15`).
- [V] Keep aggregated, path-specific validation errors; they make manual editing substantially less frustrating (`internal/config/validate.go:55`).
- [V] Keep credential references and recursive literal-secret detection (`internal/config/config.go:210`, `internal/config/secretscan.go:94`).
- [V] Keep the Claude ceiling and Codex opt-in widening controls as explicit security boundaries (`internal/config/policy.go:273`, `internal/codex/runner.go:242`).
- [V] Keep configuration ownership isolated from daemon, credential, and runner behavior; repository layering is healthy (`internal/config/config.go:1`, `internal/daemon/runners.go:30`).

### Problems that justify a rework

1. **Accepted configuration can be silently ignored.** [V] `agents.claude` accepts arbitrary YAML, is excluded from validation, and has no runtime consumer. This is the most serious usability defect because it contradicts the otherwise fail-closed design (`internal/config/config.go:99`, `internal/config/validate.go:218`, `internal/daemon/runners.go:31`).
2. **The hierarchy expresses implementation history rather than operator concepts.** [V] Claude is simultaneously a top-level legacy section and a reserved agent, while profiles capable of configuring both agents remain owned by Claude (`internal/config/config.go:87`, `internal/config/config.go:315`).
3. **The generated file is neither clearly minimal nor exhaustive.** [V] It carries extensive comments and advanced permission controls, yet omits live Codex model, tier, and user-config settings (`internal/config/template.go:226`, `internal/config/config.go:125`).
4. **Some source comments contradict runtime behavior.** [V] Comments say Codex tier mappings or sandbox values are absent/unmeasured, while defaults and validators implement them (`internal/config/config.go:141`, `internal/config/tier.go:141`, `internal/config/validate.go:244`).
5. **Model selection is ambiguous when fully written out.** [V] Both a literal model and a tier may be present, but the tier silently wins. Claude's template explains this; Codex's does not (`internal/harness/runner.go:184`, `internal/codex/runner.go:83`, `internal/config/template.go:116`).
6. **Several settings have non-obvious lifecycle semantics.** [V] Repository defaults are captured when a repository is registered rather than obviously remaining live, while the retention-days setting does not trigger automatic cleanup (`internal/cli/cmd_repo.go:111`, `internal/store/repos.go:28`, `internal/cli/cmd_task.go:159`).
7. **Schema evolution has no explicit mechanism.** [V] Existing files are never overwritten, strict decoding rejects removed keys, and there is no schema version or migration path. A cleanup can therefore become an abrupt startup failure for every existing installation (`internal/cli/cmd_init.go:72`, `internal/config/config.go:513`).
8. **Plain HTTP is accepted for authenticated GitHub traffic.** [V] This is useful for local testing but unsafe as an unrestricted production option because bearer credentials can travel without transport encryption (`internal/config/validate.go:159`, `internal/cli/cmd_init.go:181`).

### Refined open questions for Plan mode

- [V] Is `agents.claude` near-term migration scaffolding, or should accepting-but-ignoring it be fixed before any broader reorganization?
- [V] Should agent-specific execution settings live uniformly under `agents`, with tool profiles moved to an agent-neutral policy section?
- [V] Should simultaneous literal model and model tier be rejected as ambiguous, or retained with prominently documented precedence?
- [V] Is the generated file intended to be the exhaustive reference, a minimal starter, or should those become two separate artifacts?
- [V] What compatibility promise applies to existing hand-maintained files, and should transition support use aliases, explicit schema versions, a migration command, or a documented breaking change?
- [V] Should non-TLS GitHub endpoints be limited to loopback/development use or require an explicit insecure override?
- [V] Should repository-default snapshot behavior and non-automatic retention remain as-is with clearer wording, or be brought into a separate behavior-change scope?

### Updated recommendation for Plan mode

[V] Treat this as an information-architecture and compatibility project, not a rewrite of the proven loader. First define a coherent operator model for global settings, forge credentials, shared policy/profiles, model tiers, and per-agent execution. Then define how existing files transition and make the generated starter/reference/tests express every supported key and precedence rule. Preserve the current strictness and least-privilege defaults. Keep automatic retention and broader daemon behavior out of scope unless deliberately added as separate user stories.
