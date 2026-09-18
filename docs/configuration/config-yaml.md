# config.yaml reference

[Configuration](README.md) · [Agents](coding-agents.md) · [Permissions](permissions.md)

All keys below are nested YAML paths, not literal dotted keys. Types are string, integer, boolean, list or mapping. **Every setting requires a daemon restart for the daemon to use a change**; CLI consumers load the file when invoked. Repository defaults affect registration, not already stored repository settings. No field supports hot reload.

Defaults describe omitted values in a loaded file unless explicitly marked **generated**. `init` writes a complete configuration; decoder defaults alone are not a runnable file: at least one agent and a complete permission matrix are required. Empty strings/lists and explicit zero do not generally mean omission. Unknown keys and a second YAML document fail. Use YAML `true`/`false`; the two MCP booleans explicitly reject quoted strings, null and YAML yes/no coercions.

## settings

| Field under `settings` | Type; default | Values and effect |
| --- | --- | --- |
| `poll_interval_seconds` | integer; `30` | `10..60`; GitHub polling interval |
| `max_concurrent_runs` | integer; `3` | `1..16`; maximum concurrent runs |
| `default_base_branch` | string; `""` | Empty discovers cached `origin/HEAD`; otherwise branch-shaped name, starting alphanumeric, letters/digits/dot/underscore/dash/slash, no `..`. Registration `--base` wins |
| `feature_prefix` | string; `lf` | Nonempty alphanumeric first character, then letters/digits/dot/underscore/dash; no slash or `..`. Registration `--prefix` wins |
| `worktree_root` | string; `""` | Empty uses sibling `.lf-worktrees/<clone-name>`; otherwise absolute directory. Registration `--worktrees` wins |
| `team_config` | string; `""` | Deprecated local team-index path; empty or absolute. Accepted for compatibility, but current team resolution reads the centrally installed catalog; use `teams` below for distribution |
| `worktree_retain_days` | integer; `14` | `1..3650`; safe closed-task cleanup retention, measured from last completed run; not unconditional deletion |
| `log_level` | string; `info` | `debug`, `info`, `warn`, `error` (Go slog also accepts signed offsets); case normalized |
| `attachment_max_image_bytes` | integer; `5242880` | `1024..209715200`; per image limit |
| `attachment_max_document_bytes` | integer; `10485760` | `1024..209715200`; per document limit |
| `attachment_max_total_bytes` | integer; `26214400` | `1024..209715200`; combined per-turn limit |
| `attachment_max_count` | integer; `10` | `1..100`; per-turn attachment count; ingestion also requires profile permission |
| `auto_resume_on_session_limit` | boolean; `false` | Opt into bounded recovery for recognized session-limit interruptions; see [recovery](../operations/recovery.md) |
| `mcp_auto_start` | boolean; `true` | Prepare factory MCP registration per eligible attempt; does not itself prove host consumption |
| `allow_mcp_story_creation` | boolean; `false` | Allow the creation capability subject to an exact short-lived owner approval and other gates |
| `auto_sync_base` | boolean; `true` | Synchronize feature branch with base before eligible runs |
| `auto_sync_max_behind` | integer; `100` | Nonnegative; over-cap distance refuses the run. **Explicit `0` removes the cap**, omission retains `100` |
| `block_run_on_conflict` | boolean; `true` | Block the run when base synchronization conflicts; `false` permits continuation on the unsynchronized feature branch |
| `suggest_conflict_resolution` | boolean; `false` | Request bounded model proposals for blocked conflicts; does not authorize applying them |
| `conflict_proposal_ttl_hours` | integer; `24` | `1..720`; pending proposal lifetime |
| `conflict_content_max_bytes` | integer; `16384` | `256..1048576`; total conflicting content budget; additional per-file/type limits still apply |

See [attachments](../guides/attachments.md), [MCP](../guides/mcp.md), [conflicts](../guides/sync-and-conflicts.md) and [cleanup](../operations/cleanup.md) for behavior and feedback.

## providers.github

| Field | Type; default | Values and effect |
| --- | --- | --- |
| `api_base` | string; `https://api.github.com` | HTTP or HTTPS base URL with host, without query/fragment; surrounding whitespace and trailing slash normalized |
| `token_keychain.service` | string; empty (**generated:** `litefactory`) | macOS generic-password service reference; requires account |
| `token_keychain.account` | string; empty (**generated:** `github-bot`) | Account reference; requires service |
| `token_env` | string; empty | Environment variable **name**, matching `[A-Za-z_][A-Za-z0-9_]*`; foreground development alternative |

Use one complete Keychain reference or `token_env`, never both. Omitting both credential references passes structural validation but cannot start an authenticated daemon. LaunchAgent operation requires Keychain. Credentials are resolved at process start. Never put token values in YAML. See [GitHub setup](../getting-started/github-setup.md).

## teams

| Field | Type; default | Values and effect |
| --- | --- | --- |
| `index_url` | string; `https://github.com/marioatcloudvane/litefactory/teams/teams.json` | HTTPS file URL; omission uses the default, explicit empty is invalid |
| `default_source` | string; empty | A configured alias from `sources`; overrides `index_url` |
| `sources` | mapping; empty | Alias keys match `[a-z0-9][a-z0-9-]{0,63}` |
| `sources.<alias>.index_url` | string; required | HTTPS file URL for this source |
| `sources.<alias>.token_env` | string; empty | Optional credential environment variable name; same name syntax as above |
| `sources.<alias>.token_keychain.service` | string; empty | Optional Keychain service; requires account |
| `sources.<alias>.token_keychain.account` | string; empty | Optional Keychain account; requires service |

Source URLs forbid credentials, query, fragment, backslashes and whitespace. Each source may have one complete credential reference or none. Explicit CLI alias/HTTPS URL wins over `default_source`, then `index_url`. These settings configure catalog distribution; they do not automatically update installed teams. See [team commands](../cli/teams.md).

## agents

`agents` is an ordered mapping with only `claude` and/or `codex`. Neither is implicitly configured when absent. The first declared entry is the default; **generated order is Claude, then Codex**. Each declared stanza receives its own omitted-field defaults. Issue labels may select another configured agent; model selection remains tier-based.

| Field | Type; default | Values and effect |
| --- | --- | --- |
| `agents.claude.binary` | string; `claude` | Absolute executable path or bare command name; empty and relative paths containing slash fail |
| `agents.claude.model_tier` | string; `balanced` | `flagship`, `balanced`, `fast`; must resolve through `tiers.model_by_tier.claude` |
| `agents.claude.autocompact` | string; `100k` | `auto`, `100k` through `1000k`, or `1M`; explicit empty fails |
| `agents.claude.tools` | string list; implementation tools below | Nonempty bare tool names matching `[A-Za-z][A-Za-z0-9_]*`; actual tool boundary |
| `agents.claude.allowed_tools` | string list; implementation rules below | Additive tool permission rules, not the tool boundary; empty list grants no extra approvals |
| `agents.claude.tool_ceiling` | string list; omitted/empty copies `tools` | Hard upper bound; must contain global tools and every profile's tools. **Generated:** implementation tools plus `WebSearch`, `WebFetch` |
| `agents.claude.settings_path` | string; empty resolves to `$LF_HOME/claude-settings.json` | Explicit path must be absolute; factory-controlled Claude settings |
| `agents.claude.timeout_seconds` | integer; `1800` | `60..86400`; wall-clock limit per invocation |
| `agents.claude.forward_anthropic_env` | boolean; `false` | Opt into forwarding `ANTHROPIC_*` variables; can override the CLI's usual authentication |
| `agents.codex.binary` | string; `codex` | Same executable path constraints as Claude |
| `agents.codex.model_tier` | string; `balanced` | Same three tiers, resolved through `tiers.model_by_tier.codex` |
| `agents.codex.timeout_seconds` | integer; `1800` | `60..86400`; explicit zero fails |
| `agents.codex.writable_roots` | string list; empty | Additional nonempty absolute directories; workspace-write grants, not a way to override read-only mode |
| `agents.codex.network_access` | boolean; `false` | Enable workspace-write network access, including outbound networking, not just loopback |
| `agents.codex.allow_user_config` | boolean; `false` | Ordinary runs may read personal Codex config when true; native team delivery still isolates user config |

Binary resolution searches the supplied process PATH for bare names and verifies executability. Prefer absolute paths with launchd. Availability/authentication of a model or executable is not guaranteed by syntactically valid YAML.

## tiers

`tiers.model_by_tier` is a mapping of `claude`/`codex` to mappings of `flagship`/`balanced`/`fast`. Each value is a nonempty model string matching `[A-Za-z][A-Za-z0-9._-]*`. The selected agent tier must exist. Unknown agents/tiers fail; removed `agents.<agent>.model` fields fail rather than override this table.

| Field | Default model string |
| --- | --- |
| `tiers.model_by_tier.claude.flagship` | `opus` |
| `tiers.model_by_tier.claude.balanced` | `sonnet` |
| `tiers.model_by_tier.claude.fast` | `haiku` |
| `tiers.model_by_tier.codex.flagship` | `gpt-6-astra` |
| `tiers.model_by_tier.codex.balanced` | `gpt-6-astra` |
| `tiers.model_by_tier.codex.fast` | `gpt-5.6-luna` |

These are this revision's factory defaults, not a claim about today's provider catalog. Per-field decoding overlays the default maps; use explicit valid model values to change entries. Model aliases do not configure reasoning effort.

## tool_profiles.permissions

A required mapping with exactly `research`, `plan`, `implement`, `validate`. No implicit matrix is added when omitted. Every profile requires a stanza for every configured agent and rejects cells for unconfigured agents. Spike uses `implement`; there is no `spike` key. These cells replace the selected agent's run surface; a missing cell never falls back to global tools.

| Field under `tool_profiles.permissions.<mode>` | Type; default | Values and effect |
| --- | --- | --- |
| `attachments` | boolean; `false` when omitted | Gates factory ingestion; generated Research/Plan use `true`, Implement/Validate `false` |
| `claude.tools` | string list; required when Claude configured | Nonempty bare tools, all within `agents.claude.tool_ceiling` |
| `claude.allowed_tools` | string list; empty | Each rule must name a tool in this cell's `tools` |
| `codex.sandbox` | string; required when Codex configured | `read-only` or `workspace-write`; Validate must be `read-only`; runner construction also checks supported modes |

Claude rule syntax is `Tool` or `Tool(qualifier)`. Wildcard-only rules and unqualified/wildcard Bash approvals are rejected. Validate permits only tools `Read`, `Glob`, `Grep`, `Task` and approval rules `Read`, `Glob`, `Grep`. It grants no shell, writes or fresh test execution.

### Generated tool lists and rules

The generated matrix uses these exact sets (order has no additional permission meaning):

- **Implementation tools:** `Read`, `Glob`, `Grep`, `Edit`, `Write`, `Bash`, `Task`.
- **Artifact tools (Plan):** `Read`, `Glob`, `Grep`, `Write`, `Bash`, `Task`.
- **Research tools:** artifact tools plus `Edit`, `WebSearch`, `WebFetch`.
- **Validate tools:** `Read`, `Glob`, `Grep`, `Task`.
- **Common Bash rules:** `Bash(git status:*)`, `Bash(git diff:*)`, `Bash(git log:*)`, `Bash(git show:*)`, `Bash(git rev-parse:*)`, `Bash(git ls-tree:*)`, `Bash(git merge-base:*)`, `Bash(git rev-list:*)`, `Bash(mkdir:*)`.
- **Implementation rules:** `Read`, `Glob`, `Grep`, `Edit`, `Write`, common Bash rules, `Bash(go build:*)`, `Bash(go test:*)`, `Bash(go vet:*)`, `Bash(gofmt:*)`, `Bash(grep:*)`, `Bash(sed -n:*)`, `Bash(head:*)`, `Bash(tail:*)`, `Bash(wc:*)`.
- **Artifact rules:** `Read`, `Glob`, `Grep`, `Write`, common Bash rules.
- **Research rules:** artifact rules plus `Edit`, `WebSearch`, `WebFetch`.
- **Validate rules:** `Read`, `Glob`, `Grep`.

Generated Codex Research, Plan and Implement use `workspace-write`; Validate uses `read-only`. Factory mode restrictions and native delivery may narrow these configured surfaces further. See [permissions](permissions.md).

## Migration and source bounds

Root `github`, `claude`, `default_agent` and former root settings keys are rejected. Move values to `providers.github`, `agents.claude`, ordered `agents`, and `settings` respectively. Old permission labels do not select these profiles. Do not paste legacy flat configuration into a current file.

Reconciled against [config decoder/defaults](../../internal/config/config.go), [validators](../../internal/config/validate.go), [policy](../../internal/config/policy.go), [template](../../internal/config/template.go), [tiers](../../internal/config/tier.go) and [team sources](../../internal/config/teams.go) at `8f556367c2d0a00d2736a53a326152ff43a6f96e`. Some historical source comments describe earlier behavior; the executable decoder, validators and template determine this reference.
