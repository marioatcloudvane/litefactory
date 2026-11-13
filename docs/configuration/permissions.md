# Permissions

[Documentation home](../README.md) · [Configuration](README.md) · [Key reference](config-yaml.md#tool_profiles) · [GitHub setup](../getting-started/github-setup.md)

Litefactory separates the human owner's instructions, the bot's GitHub access and the coding agent's local permissions. Configure all three before running work; a grant in one does not grant another.

## Identities and credentials

The owner is the authenticated human identity discovered at initialization. The bot is the separately configured GitHub identity that reads issues and publishes factory results. Authority checks compare numeric identities; matching a display name is insufficient. Agent subprocesses do not receive the factory's forge token, and issue text cannot authorize new forge targets or credential access.

Configuration stores references, never secret values. Example for an existing Keychain item:

```yaml
providers:
  github:
    token_keychain:
      service: litefactory
      account: github-bot
```

This names an item; it does not create it. Resolve access through [GitHub setup](../getting-started/github-setup.md). Use `token_env` only as the name of an environment variable for foreground development. Keychain and environment references are mutually exclusive. Missing credentials or a missing recorded bot identity prevent daemon operation. Recognized literal credential shapes fail configuration validation without printing the matched secret.

## Mode permissions

Each configured agent needs a cell in the four profiles `research`, `plan`, `implement` and `validate`. The selected mode determines its profile. Spike, Bug and Validate-repair roles have their own optional profiles. The generated posture is:

| Mode | Claude surface | Codex sandbox | Attachment ingestion |
| --- | --- | --- | --- |
| Research | Read/search, artifact writing, bounded shell and delegation tools | workspace-write | enabled |
| Plan | Read/search, artifact writing, bounded shell and delegation tools | workspace-write | enabled |
| Implement | Editing, build/test and delegation tools | workspace-write | disabled |
| Validate | Read, Glob, Grep, Task, and Bash limited to fixed read-only reviewer rules | read-only | disabled |
| Spike investigator | Read, Glob, Grep | read-only | disabled |
| Spike fixer | Read, Glob, Grep, Edit, MultiEdit, Write | workspace-write | disabled |

These are tool surfaces, not permission to exceed a mode's scope. Validate is read-only: the config check refuses any Claude tool outside Read, Glob, Grep, Task and Bash, allows Bash only with the fixed reviewer rules, and requires the Codex `read-only` sandbox. The optional `validate-repair` and `bug-fixer` (Bug) profiles grant the roles that need them; a missing optional profile blocks only that role. A missing Spike investigator or fixer blocks Spike.

The four main profiles are mandatory and have no missing-cell fallback. The [key reference](config-yaml.md#tool_profiles) lists exact keys, defaults and validation rules. Deprecated `lf:tools=...` labels are refused; remove them and select the intended mode through current [issue controls](../guides/owner-commands.md).

## Claude boundaries

`tools` defines the built-in tool boundary. `allowed_tools` adds approvals and may combine with ambient permissions; it cannot make a granted Bash tool intrinsically read-only. `tool_ceiling` must contain every configured profile tool. An omitted or empty ceiling defaults to the global `tools`, so a Research profile with web tools needs a ceiling that includes them. The generated configuration supplies that ceiling.

`settings_path` selects factory-controlled settings. Do not use personal allow rules as a substitute for a narrow surface. Unqualified or wildcard Bash approvals and permission-bypass options are rejected. The child environment is restricted; `forward_anthropic_env` is an explicit opt-in that can alter Claude authentication.

## Codex boundaries

The mode's `sandbox` is the filesystem boundary; Claude permission lists are never translated into Codex grants. `writable_roots` adds explicit absolute directories for workspace-write runs. Read-only remains read-only. Factory-owned attempt directories may also be supplied so result helpers can function; they do not authorize editing arbitrary runtime state.

`network_access: false` is the default for workspace-write. Enabling it allows network calls generally, including loopback test listeners and outbound connections. This is independent of filesystem grants.

Ordinary runs default to ignoring personal Codex configuration and rules. `allow_user_config: true` permits personal configuration and may expose repository skills as supported by the host CLI. Native team delivery retains configuration isolation and supplies factory-generated role configuration. Do not infer that a personal option overrides factory mode restrictions.

## Named deny groups

Define optional `settings.denyrules` in operator configuration and select group names with `lf_deny` in your [local team](../cli/teams.md#select-deny-groups-in-a-local-team). The default catalogue is empty. Each group has only a required, nonempty `deny` string list. Names are case-sensitive, start with an ASCII letter and then contain letters, digits, `_` or `-`. Unknown fields, duplicate YAML keys, nested groups, bare script names, blank entries and unknown tools are rejected. Graph selections contain names only, never raw rules.

Graph defaults and an agent node's selections combine in first-occurrence order: graph groups first, then node groups. Repeated names and identical entries are removed. Omitted, empty and whitespace-only node values add nothing and cannot clear graph defaults. Empty items in a nonempty comma list are errors. A node selection is allowed only on a registered agent activity; even an empty selection on a terminal, judge or control node is rejected. Graph defaults also reach agents launched by special activities, including Validate reviewers and reporters.

Claude supports these explicit native forms:

| Entry | Meaning and limits |
| --- | --- |
| `Bash(make check:*)` | Native command-prefix deny; literal command patterns and `*` wildcards are supported. No extra invocation forms are generated. Parameter selectors, embedded colons other than trailing `:*`, parentheses, brackets, braces, `?` and backslashes are refused. |
| `Read(secrets/**)` or `Edit(generated/*.json)` | Relative file patterns are anchored explicitly at the launch worktree, independent of the settings file location. Native depth is preserved: bare filenames such as `.env` and single-directory patterns such as `secrets/**` match at any depth below that worktree; multi-component paths such as `generated/*.json` stay rooted there. Literal components, `*` and `**` are supported. A leading `./` is accepted; interior `.`, `..` and empty components are refused. |
| `Read(//absolute/path/**)` or `Edit(~/private/**)` | Native absolute and home-relative anchors are retained. A single leading `/` in a selected rule is settings-source-relative and is refused; use `//` for absolute paths. File negation (`!`), colons and unsupported glob or parameter forms are refused. Launch worktree and factory settings paths must not contain native wildcard syntax. |
| `Agent` or `Agent(reviewer)` | Deny all native subagents or one literal native subagent name. Named rules do not disable unrelated native agents. Names may contain letters, digits, `.`, `_` and `-`; selectors and wildcards are refused. |

Command matching is a guard rail within the [native permission rules](https://code.claude.com/docs/en/permissions), not a guarantee against all indirect execution. List the invocation forms your team needs to deny. Read/Edit are native file-tool restrictions, not a promise to prevent shell file access; Read rules may also prevent writes. Existing sandbox limits remain in force. An agent CLI started through Bash is an ordinary command, not native delegation. [Native subagents](https://code.claude.com/docs/en/sub-agents) inherit session permissions; rules are delivered through session settings rather than subagent tool frontmatter.

Selected entries append to existing permission denies and shared credential/state protections. Existing allow/ask lists and sandbox limits are preserved. Existing single-`/` file rules in the operator settings are made explicitly absolute against that settings file's directory so moving the attempt file retains their meaning. Claude receives one private attempt settings file even when Bash is absent. Unsupported selected rules or malformed settings refuse launch before an agent process starts; restrictions are never partially applied. Runs with no selected groups retain their existing settings, authentication and session behavior.

Admission resolves selected names; static team validation checks syntax without requiring your operator catalogue. Each new or resumed attempt reloads current operator configuration and freezes its selected rules. Capacity retries and a cold fallback after resume refusal retain that snapshot. A changed or removed rule takes effect on the next attempt, including resume. Claude uses separate attempt files. Codex serializes attempts sharing a factory session or native resume identity, including an attempt that removes all groups; independent sessions use separate protected homes. A reload error refuses launch instead of using stale rules.

Selected Claude shell-enabled launches retain factory confinement defaults. Custom sandbox proxy ports (`httpProxyPort`, `socksProxyPort`) and enforcement-helper overrides (`bwrapPath`, `socatPath`, `ripgrep`) cannot be combined safely and are refused; remove those overrides before retrying. Graph judges retain their isolated read-only tool ceiling and receive shared plus selected denies without importing ordinary operator allow rules. Standalone judge, doctor and conversation calls are unchanged.

To recover from a refusal, use its graph/node selection, group, entry and reason to correct the catalogue or local graph. For an unknown group, define it or correct its case. For an unsupported path, use a supported anchor and glob; for Codex, use a supported literal prefix or bare `Agent`, or choose Claude when you need its supported file or named-agent restrictions. Removing a selection removes its protection. A capability or unsafe-path refusal needs its reported runtime, login or writable-root cause corrected first. Retry only after correcting the cause. Factory-run checks and workflow routing are unaffected.

### Codex support and session compatibility

| Selected entry | Claude | Codex 0.160.0 |
| --- | --- | --- |
| `Bash(make check:*)` | Native prefix rule | Forbidden native literal-token prefix rule |
| Bash forms with internal wildcards or exact-command meaning | Subject to the Claude limits above | Refused; only plain literal tokens followed by `:*` are supported |
| `Read(...)`, `Edit(...)` | Native file-tool restrictions with the anchors above | Refused; an edit-only rule is never converted into a read ban |
| `Agent` | Deny all native delegation | Disable all native delegation, preserving any existing delegation ban |
| `Agent(reviewer)` | Deny the named native subagent | Refused; a named restriction is never converted into an all-agent ban |

Codex command entries must end in `:*` and contain plain space-separated literal tokens, using ASCII letters, digits and `/._-+:,@%` within tokens. Surrounding whitespace, tabs, Unicode, quoting, expansions, shell operators, assignments, internal wildcards and unsupported punctuation are refused before an agent starts. The factory emits only the stated token prefix, with a forbidden decision; it does not invent absolute-path, wrapper or alternate executable forms. Codex's native shell parsing and executable resolution determine matches. A separately shell-launched CLI is still an ordinary command. List each form you need and check it on the supported runtime; this is a guard rail, not protection against every indirect command.

Selected Codex runs require the pinned 0.160.0 capability set and a successful native rule/configuration admission check. Other versions or missing required capabilities refuse the whole launch, even when part of the selection could be enforced. Admission also reads the effective native configuration without starting an agent and refuses managed settings that would change the selected permission profile, project isolation, delegation ban or canonical state bindings. Mandatory managed restrictions remain active. A rule-checker or configuration-check success establishes syntax and mapping only; it does not prove live tool enforcement. Permitted native descendants receive the parent's policy and existing permission ceiling; selecting bare `Agent` disables delegation instead.

The factory stores generated rules and configuration in a protected, stable session home outside agent-writable roots. It excludes unrelated personal and project policy layers, including when `allow_user_config` is enabled, while retaining managed restrictions, sandbox choice, network limits, caches and shared credential/state protections. Unsafe placement or changed policy artifacts refuse launch. For workspace-write runs, writable temporary roots such as `/tmp` and `TMPDIR` count too: keep the factory policy area and canonical Codex state outside them and outside configured worktree, cache and submission roots. Rules are fixed for the attempt, checked before start retries, and replaced from the next snapshot while the session lease is held. Cancellation and startup failure must finish process teardown and usage drain before another attempt can replace that session's policy.

Supported file-based login continues to use the canonical `HOME/.codex` credential and local session store. The `.codex` directory and its `auth.json` must be a real directory and regular file, respectively, rather than symlinks; current and historical usage keep their existing safe-read checks. `HOME`, factory session identity and native resume identity remain unchanged. Adding, changing or removing groups does not migrate conversations or usage records. Usage reads the real canonical rollout directory, including descendant evidence, rather than following the protected home's session link. Keyring and other credential modes without a faithful bridge refuse before launch. A different `CODEX_HOME` or nonlocal experimental thread store also refuses rather than silently splitting or losing the existing session store. Correct the reported cause and retry. No-selection graph launches retain their existing arguments and rule loading and gain no deny-related private home; standalone calls are unchanged.

Live verification remains an owner check. On Claude, try the supported command, Read, Edit and named Agent actions and an unrelated allowed action. On Codex 0.160.0, try a harmless denied literal command prefix and an unrelated allowed command inside the sandbox, then repeat in a permitted native descendant and after parent/child resume. On both, select `Agent` and check that native delegation is blocked. With the supported Codex file login, add, change and remove groups across resumed attempts and check conversation and root/child usage continuity without signing in again. Controlled-process tests prove factory delivery; they do not establish installed runtime enforcement or satisfy these live checks.

## Check sandbox for Bug checks

Bug checks run under a separate operator ceiling, `bug_checks` in `config.yaml`: a Codex sandbox with no network by default. See the [key reference](config-yaml.md#bug_checks) and [Bug checks](../bug-checks.md).

## Feedback and limits

Configuration rejects incomplete mode/agent matrices, invalid tools, missing ceiling entries and invalid Validate grants. A later sandbox refusal usually means a workload needs an explicit permitted path or a different authorized task; adding an issue label cannot widen the configured surface. Fix the relevant setting and restart, then inspect a new run's effective permissions and diagnostics.

Outbound reports pass through redaction; local `$LF_HOME/logs` copies are **unredacted** and may contain sensitive input or output. Inspect locally and sanitize excerpts before sharing. Redaction is report hygiene, not the subprocess secret boundary.

Factory [MCP](../guides/mcp.md) capabilities have their own mode, scope, receipt and approval gates; registration does not authorize arbitrary third-party servers or effects. See [troubleshooting](../operations/troubleshooting.md) for missing runs, access failures and uncertain outcomes.
