# config.yaml reference

[Documentation home](../README.md) · [Configuration](README.md) · [Coding agents](coding-agents.md) · [Permissions](permissions.md)

This page lists every key litefactory reads from `config.yaml`. The file lives at `$LF_HOME/config.yaml` (by default `~/.litefactory/config.yaml`). `litefactory init` creates it and never overwrites an existing one.

## How the file is read

- **Restart needed: yes, for every key.** The daemon reads the file once, when it starts. There is no hot reload. After an edit, stop and start the daemon (see [running the daemon](../operations/running-the-daemon.md)). Other commands (`repo add`, `doctor`, and so on) read the file each time you run them.
- **Unknown keys are refused.** A misspelled or removed key stops the load with a message that names it. A file with more than one YAML document is refused too.
- **Missing keys are fine.** Each key has a default. A key you write is checked, so a written `0` is not replaced by the default; it is either accepted or refused by the range check.
- **All problems are reported at once.** A bad file lists every failure, so you can fix them in one pass.
- **No secrets in the file.** Every string value is scanned for credential shapes. A literal token anywhere stops the load. Use a Keychain reference or the name of an environment variable instead.
- **Booleans are strict** for `validate.auto_start`, `settings.mcp_auto_start` and `settings.allow_mcp_story_creation`. Write plain `true` or `false`; quoted strings, null and `yes`/`no` are refused.
- **Empty file.** An empty file loads as all defaults, but it is not runnable: you still need at least one agent and the four required permission profiles.

Types below: string, integer, boolean, list, mapping. "Range" is the allowed values; outside it, the load fails (nothing is silently clamped).

## Worked example

`litefactory init` writes the file below (shortened here; the real file also lists the full tool lists and every permission profile). The numbers are the defaults in this version.

```yaml
validate:
  auto_start: false

webhook:
  listen: "127.0.0.1:8787"
  public_url: ""

settings:
  team_source: "https://raw.githubusercontent.com/marioatcloudvane/litefactory/main/teams/teams.json"
  poll_interval_minutes: 60
  max_concurrent_runs: 3
  default_base_branch: ""
  feature_prefix: "lf"
  worktree_root: ""
  worktree_retain_days: 14
  min_free_disk_gb: 15
  log_level: "info"
  auto_sync_base: true
  auto_sync_max_behind: 100
  block_run_on_conflict: true

providers:
  github:
    api_base: "https://api.github.com"
    token_keychain:
      service: "litefactory"
      account: "github-bot"

agents:            # first entry is the default agent
  claude:
    binary: "claude"
    model_tier: "balanced"
    timeout_seconds: 1800
  codex:
    binary: "codex"
    model_tier: "balanced"
    timeout_seconds: 1800

tiers:
  model_by_tier:
    claude: {balanced: "sonnet", fast: "haiku", flagship: "opus"}
    codex: {balanced: "gpt-6-astra", fast: "gpt-5.6-luna", flagship: "gpt-6-astra"}

tool_profiles:
  permissions:
    implement:
      attachments: false
      claude: {tools: [...], allowed_tools: [...]}
      codex: {sandbox: "workspace-write"}
    # research, plan, validate, spike-investigator, spike-fixer follow
```

Nested keys below are written with dots, for example `settings.log_level` means `log_level` under `settings:`.

## validate

| Key | Type | Default | Range | Effect |
| --- | --- | --- | --- | --- |
| `validate.auto_start` | boolean | `false` | `true` or `false` | When true, the daemon starts Validate by itself once Implement has finished every approved story of a parent issue. When false, the owner starts it with `/validate`. See [owner commands](../guides/owner-commands.md). |

## webhook

For setup, security and troubleshooting, see [GitHub setup](../getting-started/github-setup.md) and the [webhook guide](../getting-started/github-setup.md).

| Key | Type | Default | Range | Effect |
| --- | --- | --- | --- | --- |
| `webhook.listen` | string | `127.0.0.1:8787` | `host:port`, host not empty, port 1 to 65535; or empty | Address the local webhook listener binds. Empty turns the listener off. The default is loopback only; to accept outside traffic you must write another address. |
| `webhook.public_url` | string | empty | absolute `http` or `https` URL with a host; or empty | Address GitHub sends webhooks to. When set, `repo add` and `repo webhook` register the webhook. Empty means no registration and polling only. An `http` URL sends payloads unencrypted. |

## chat

Optional read-only Discord chat. Leave the whole `chat` mapping out to turn it off. For the full setup and use, see [Discord](../discord.md).

If the `chat.discord` mapping is present, all of its keys are checked, even with `enabled: false`.

| Key | Type | Default | Range | Effect |
| --- | --- | --- | --- | --- |
| `chat.discord.enabled` | boolean | `false` | `true` or `false` | Turns the Discord adapter on. When true, `webhook.listen` must be non-empty and `webhook.public_url` must be an `https` URL with a host and no user info. |
| `chat.discord.application_id` | string | none (required) | numeric ID, no leading zero, up to 20 digits | The Discord application. |
| `chat.discord.public_key` | string | none (required) | 64 hex characters (a 32-byte Ed25519 key) | Key Discord signs requests with, from the developer portal. |
| `chat.discord.token_keychain.service` | string | empty | non-empty when `account` is set | Keychain service holding the bot token. Needs `account`. |
| `chat.discord.token_keychain.account` | string | empty | non-empty when `service` is set | Keychain account for the bot token. |
| `chat.discord.token_env` | string | empty | environment variable name `[A-Za-z_][A-Za-z0-9_]*` | Name (not value) of an environment variable holding the bot token. Give exactly one of `token_keychain` or `token_env`. |
| `chat.discord.owner_id` | string | none (required) | numeric ID | The one Discord user the adapter listens to. |
| `chat.discord.channel_ids` | list of strings | none (required) | at least one unique numeric ID | Channels the adapter may use. |
| `chat.discord.notice_channel_id` | string | empty | numeric ID that is also in `channel_ids`; or empty | Channel for task notices. |

## settings

| Key | Type | Default | Range | Effect |
| --- | --- | --- | --- | --- |
| `settings.team_source` | string | `https://raw.githubusercontent.com/marioatcloudvane/litefactory/main/teams/teams.json` | absolute `https` catalogue URL, or an absolute file path (stored as `file://`); no credentials, query or fragment; no trailing slash; or empty for the default | Where `litefactory team` commands fetch the machine-level team catalogue. See [teams](../cli/teams.md). |
| `settings.poll_interval_minutes` | integer | `60` | 1 to 180 | How often the daemon lists GitHub when no webhook woke it sooner. Webhooks are the fast path; this is the safety net. The old `poll_interval_seconds` is refused and is not converted. |
| `settings.max_concurrent_runs` | integer | `3` | 1 to 16 | Shared execution capacity in slots. Each step reserves its DOT cost; omitted costs are 1. See [slot costs](../guides/workflow.md#execution-capacity). |
| `settings.default_base_branch` | string | empty | branch name: starts with a letter or digit, then letters, digits, `.`, `_`, `-`, `/`; no `..`; or empty | Base branch for new repository registrations. Empty finds it from the remote. `repo add --base` wins. Stored registrations do not change. |
| `settings.feature_prefix` | string | `lf` | not empty; letters, digits, `.`, `_`, `-`; starts with a letter or digit; no `/` or `..` | First segment of every task branch. `repo add --prefix` wins. |
| `settings.worktree_root` | string | empty | absolute path, or empty | Folder for task worktrees. Empty uses a per-repository sibling folder. `repo add --worktrees` wins. Stored registrations do not change. |
| `settings.worktree_retain_days` | integer | `14` | 1 to 3650 | Daily cleanup removes a safe, closed task's worktree this many days after its last completed run. See [cleanup](../operations/cleanup.md). |
| `settings.deploy_backup_keep` | integer | `5` | 1 to 100 | Newest pre-deploy database backups the daily cleanup keeps. |
| `settings.log_retain_days` | integer | `30` | 1 to 3650 | Days rotated global log files are kept before daily deletion. |
| `settings.archive_retain_days` | integer | `30` | 1 to 3650 | Days archive files and the run logs they cover are kept before daily deletion. |
| `settings.min_free_disk_gb` | integer | `15` | 0 to 10000 | Runs and check commands wait while free disk is below this many GB. `0` turns the guard off. |
| `settings.log_level` | string | `info` | `debug`, `info`, `warn`, `error` (case does not matter) | Log detail. `--level` on `daemon run` or `daemon start` overrides it for that run. |
| `settings.attachment_max_image_bytes` | integer | `5242880` (5 MiB) | 1024 to 209715200 | Largest single image. |
| `settings.attachment_max_document_bytes` | integer | `10485760` (10 MiB) | 1024 to 209715200 | Largest single document. |
| `settings.attachment_max_total_bytes` | integer | `26214400` (25 MiB) | 1024 to 209715200 | Largest total size of attachments in one turn. |
| `settings.attachment_max_count` | integer | `10` | 1 to 100 | Most attachments in one turn. Ingestion also needs `attachments: true` on the mode's profile. See [attachments](../guides/attachments.md). |
| `settings.auto_resume_on_session_limit` | boolean | `false` | `true` or `false` | Opt in to bounded automatic recovery after a verified provider quota limit. Setting it to `false` cancels pending automatic recoveries for good. See [recovery](../operations/recovery.md). |
| `settings.mcp_auto_start` | boolean | `true` | `true` or `false` (strict) | Prepare the factory MCP registration for each run. See [MCP](../guides/mcp.md). |
| `settings.allow_mcp_story_creation` | boolean | `false` | `true` or `false` (strict) | Allows the story-creation capability. Each creation still needs an exact, short-lived owner approval. |
| `settings.auto_sync_base` | boolean | `true` | `true` or `false` | Bring a task branch up to date with its base branch before a run. See [sync and conflicts](../guides/sync-and-conflicts.md). |
| `settings.auto_sync_max_behind` | integer | `100` | 0 or more (no upper limit) | A branch further behind than this many commits refuses the run. `0` means no limit. Negative values are refused. |
| `settings.block_run_on_conflict` | boolean | `true` | `true` or `false` | When syncing hits a merge conflict, stop the run. When false, the run goes ahead on the unsynced branch. |
| `settings.suggest_conflict_resolution` | boolean | `false` | `true` or `false` | After a blocked conflict, ask a model (with no tools) to suggest a fix and post it. This sends repository file content to a model. Applying a suggestion needs separate approval. |
| `settings.conflict_proposal_ttl_hours` | integer | `24` | 1 to 720 | Hours a conflict suggestion stays pending before it expires. |
| `settings.conflict_content_max_bytes` | integer | `16384` (16 KiB) | 256 to 1048576 | Total conflicting file content sent to the model in one blocked merge. |

Refused old root keys: `max_concurrent_runs`, `default_base_branch`, `feature_prefix`, `worktree_root`, `worktree_retain_days`, `log_level` (move them under `settings`), `poll_interval_seconds`, `github` (now `providers.github`), `claude` (now `agents.claude`), `default_agent` (the first entry under `agents` is the default), `teams` and `settings.team_config` (delete them; agents are built in).

## providers.github

| Key | Type | Default | Range | Effect |
| --- | --- | --- | --- | --- |
| `providers.github.api_base` | string | `https://api.github.com` | `http` or `https` URL with a host, no query or fragment; trailing `/` is removed | GitHub REST API root. Change it for GitHub Enterprise. |
| `providers.github.token_keychain.service` | string | empty (init writes `litefactory`) | not empty when `account` is set | Keychain service of the bot token. |
| `providers.github.token_keychain.account` | string | empty (init writes `github-bot`) | not empty when `service` is set | Keychain account of the bot token. |
| `providers.github.token_env` | string | empty | environment variable name `[A-Za-z_][A-Za-z0-9_]*` | Name (not value) of an environment variable holding the bot token. Meant for a foreground `daemon run`. With `daemon start`, the value is copied into the private LaunchAgent plist, so prefer the Keychain. |

Set at most one of `token_keychain` and `token_env`. Setting neither passes the file check, but the daemon will not start without a bot token. See [GitHub setup](../getting-started/github-setup.md).

## agents

`agents` is an ordered mapping. Only `claude` and `codex` are allowed, and at least one must be present. Nothing is configured implicitly. The first entry is the default for unlabelled work; reorder to change it. If you write an agent's mapping, its omitted keys take the defaults below. `agents.<name>.model` is refused; pick a tier and set the model under `tiers`. See [coding agents](coding-agents.md).

| Key | Type | Default | Range | Effect |
| --- | --- | --- | --- | --- |
| `agents.claude.binary` | string | `claude` | bare command name or absolute path (a relative path with `/` is refused) | Claude Code executable. Use an absolute path for the launchd agent. |
| `agents.claude.model_tier` | string | `balanced` | `flagship`, `balanced`, `fast`; must have a model in `tiers` | Capability tier of the model. |
| `agents.claude.autocompact` | string | `100k` | `auto`, `100k` to `999k`, `1000k` or `1M`; not empty | Context size at which Claude compacts the conversation. |
| `agents.claude.tools` | list of strings | `Read, Glob, Grep, Edit, Write, Bash, Task` | at least one bare tool name | The real tool boundary (`--tools`). |
| `agents.claude.allowed_tools` | list of strings | the init list (read-only git rules, `go build/test/vet`, `gofmt`, `grep`, `sed -n`, `head`, `tail`, `wc`, `mkdir`, plus `Read`, `Glob`, `Grep`, `Edit`, `Write`) | `Tool` or `Tool(qualifier)`; bare `Bash` and `Bash(*)` are refused | Extra approvals (`--allowedTools`). Defence in depth, not the boundary. |
| `agents.claude.tool_ceiling` | list of strings | same as `tools` when left out | must contain every tool in `tools` and in every profile | Upper bound on any tool list. `init` writes `tools` plus `WebSearch` and `WebFetch`, so Research can use the web. |
| `agents.claude.settings_path` | string | `$LF_HOME/claude-settings.json` | absolute path, or empty | Factory-controlled Claude settings file. |
| `agents.claude.timeout_seconds` | integer | `1800` | 60 to 86400 | Hard time limit for one run. |
| `agents.claude.forward_anthropic_env` | boolean | `false` | `true` or `false` | Forward `ANTHROPIC_*` variables to Claude. A wrong key can break Keychain sign-in; leave it off unless you need it. |
| `agents.codex.binary` | string | `codex` | as for Claude | Codex executable. |
| `agents.codex.model_tier` | string | `balanced` | `flagship`, `balanced`, `fast` | Capability tier of the model. |
| `agents.codex.timeout_seconds` | integer | `1800` | 60 to 86400 | Hard time limit for one run. |
| `agents.codex.writable_roots` | list of strings | empty | absolute paths, none empty | Extra folders Codex may write besides the worktree (for example a build cache). Each one widens the sandbox. |
| `agents.codex.network_access` | boolean | `false` | `true` or `false` | Allow network in the workspace-write sandbox. This also allows outbound traffic, not just local test servers. |
| `agents.codex.allow_user_config` | boolean | `false` | `true` or `false` | Let Codex read your personal Codex config. Off keeps runs isolated from it. |

## tiers

| Key | Type | Default | Range | Effect |
| --- | --- | --- | --- | --- |
| `tiers.model_by_tier.claude.flagship` | string | `opus` | model name: starts with a letter, then letters, digits, `.`, `_`, `-` | Model for the flagship tier. |
| `tiers.model_by_tier.claude.balanced` | string | `sonnet` | same | Model for the balanced tier. |
| `tiers.model_by_tier.claude.fast` | string | `haiku` | same | Model for the fast tier. |
| `tiers.model_by_tier.codex.flagship` | string | `gpt-6-astra` | same | Model for the flagship tier. |
| `tiers.model_by_tier.codex.balanced` | string | `gpt-6-astra` | same | Model for the balanced tier. |
| `tiers.model_by_tier.codex.fast` | string | `gpt-5.6-luna` | same | Model for the fast tier. |

Only `claude` and `codex` and the three tiers are accepted names. Every agent you configure needs a non-empty model for its chosen tier. If you write an agent's mapping, write all three tiers; unmentioned agents keep their defaults. A valid name does not prove the provider offers that model.

## tool_profiles

`tool_profiles.permissions` maps a profile name to what a run in that mode may do. The generated file holds the four required profiles plus the two Spike roles. See [permissions](permissions.md) for the meaning.

| Key | Type | Default | Range | Effect |
| --- | --- | --- | --- | --- |
| `tool_profiles.permissions.<name>` | mapping | none | `<name>` must be `research`, `plan`, `implement`, `validate` (all four required), or an optional role: `validate-repair`, `bug-analyst`, `bug-fixer`, `spike-investigator`, `spike-fixer` | One profile. Any other name is refused. A missing optional role blocks only that role's work. |
| `...<name>.attachments` | boolean | `false` | `true` or `false`; must be `false` for the optional roles | Lets runs in this mode ingest attachments. Init sets `true` for Research and Plan. |
| `...<name>.claude.tools` | list of strings | none | non-empty; all inside `agents.claude.tool_ceiling` | Tool boundary for this profile. |
| `...<name>.claude.allowed_tools` | list of strings | empty | same rules as `agents.claude.allowed_tools`; each rule must name a tool in this profile's `tools` | Extra approvals for this profile. |
| `...<name>.codex.sandbox` | string | none | `read-only` or `workspace-write` | Filesystem boundary for Codex. |

Rules the check enforces:

- Each of the four required profiles needs a `claude` cell if Claude is configured and a `codex` cell if Codex is configured, and no cell for an agent that is not configured.
- `validate` must stay read-only: Codex sandbox `read-only`; Claude tools only `Read`, `Glob`, `Grep`, `Task` and `Bash`, where `Bash` is allowed only with the fixed read-only reviewer rules (`Bash(git diff:*)`, `git log`, `git show`, `git ls-tree`, `git grep`, `git status`, `git rev-parse`, `cat`, `head`, `ls`, `wc`, `rg`).
- Roles: `bug-analyst` and `spike-investigator` are read-only (Claude `Read`, `Glob`, `Grep`; Codex `read-only`). `bug-fixer` and `validate-repair` need Codex `workspace-write`, with a Claude surface limited to `Read, Glob, Grep, Edit, MultiEdit, Write, Bash, Task, SendMessage, WebFetch, WebSearch`. `spike-fixer` needs `workspace-write` and only `Read, Glob, Grep, Edit, MultiEdit, Write`. A missing `spike-investigator` or `spike-fixer` blocks Spike.
- A profile name must be lower case: letters, digits, `_`, `-`, up to 40 characters.
- No value may contain the Claude flag that disables permission checks.

## bug_checks

Optional. This is the operator's ceiling for the sandbox that runs Bug checks. Without it, Bug checks are blocked and other work is not affected. The check commands themselves live in the repository, not here; see [Bug checks](../bug-checks.md). Per-repository stack overrides are in [stack profiles](../stack-profiles.md).

Checks run under the Codex sandbox: files are readable except `deny_read` and `$LF_HOME`, writes go only to the worktree, a private temp folder and `writable_roots`, and the network is off.

| Key | Type | Default | Range | Effect |
| --- | --- | --- | --- | --- |
| `bug_checks.binary` | string | none (required if `bug_checks` is present) | absolute path to the Codex CLI | Executable that applies the sandbox. |
| `bug_checks.path` | string | none (required) | colon-separated list of absolute directories, none `/` | `PATH` the checks run with. It must also reach `node` if Codex is the npm launcher. |
| `bug_checks.writable_roots` | list of strings | empty | clean absolute directories other than `/`, no quotes, backslashes or control characters | Extra writable folders, for example compiler caches. |
| `bug_checks.deny_read` | list of strings | empty | clean absolute paths other than `/`, same character limits | Paths checks may not read, for example `~/.ssh` written out in full. |
| `bug_checks.environment` | list of strings | empty | distinct environment variable names | Names of your environment variables passed to checks. Never put values here. |
| `bug_checks.network` | boolean | `false` | `true` or `false` | Opens the network, loopback included, for tests that start local servers. There is no loopback-only mode. |

## Environment variables

These are the variables litefactory reads or hands on. Values you export in a terminal do not reach a daemon started by launchd; `daemon start` records `LF_HOME` and `PATH` when you run it, so run it again after changing them.

| Variable | Read by | Effect |
| --- | --- | --- |
| `LF_HOME` | every command | Folder that holds `config.yaml`, the database, logs and worktree state. Default `~/.litefactory`. A relative value is made absolute. Use the same value for the CLI and the daemon. |
| `PATH` | daemon, git, agent runs | Used to find bare command names such as `claude`, `codex` and `git`. `daemon start` copies yours into the LaunchAgent. If it is empty, a minimal system path is used. |
| the variable named in `providers.github.token_env` | daemon | Bot token (foreground use). `daemon start` writes it into the private plist and warns you. |
| the variable named in `chat.discord.token_env` | daemon | Discord bot token. `daemon start` does not copy this one into the LaunchAgent, so under launchd use `token_keychain`. |
| names in `bug_checks.environment` | Bug checks | Passed from the daemon's environment into checks. |
| `HOME`, `USER`, `SHELL`, `LANG`, `LC_ALL`, `TERM`, `TMPDIR` | agent runs | Passed through to the coding agent. All other variables, including `GITHUB_TOKEN`, `GH_TOKEN` and anything named like a secret, are not passed. |
| `ANTHROPIC_*` | Claude runs | Passed only if `agents.claude.forward_anthropic_env` is `true`. |
| `SSH_AUTH_SOCK` | git only | Passed to the git commands the factory runs, never to the agent. |

[Documentation home](../README.md) · [Configuration](README.md) · [CLI reference](../cli/command-reference.md) · [Usage meter](../meter.md)
