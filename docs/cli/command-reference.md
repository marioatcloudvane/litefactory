# Command reference

All invocations start with `litefactory`. This inventory matches `internal/cli/commands.go` and its handlers at source revision `8f556367c2d0a00d2736a53a326152ff43a6f96e`. Examples are syntax illustrations, not observed live operations.

`--help` (also `-h`/`-help` and top-level `help`) prints help; `--version` (also `-version`/`version`) prints the binary version. Boolean flags default to `false`; optional string flags default to empty unless stated otherwise. `--flag=value` and `--flag value` work for value flags; use `--flag=false` to explicitly disable a boolean. Prefer positionals before flags. See [terminal conventions and exit codes](README.md#terminal-conventions).

## Initialization and repositories

| Syntax | Flags/defaults and effect |
| --- | --- |
| `init` | No flags. Creates missing state/config and refreshes owner/bot identities; preserves existing config. Requires authenticated `gh`; missing bot credentials warn and leave daemon startup blocked. |
| `repo add PATH` | `--team TEAM`: installed central team; `--base BRANCH`: base branch; `--worktrees DIR`: worktree root; `--prefix NAME`: task branch prefix. Omitted values on an existing registration retain its stored values. First-registration fallback is [documented here](repositories.md#options-and-precedence). |
| `repo list` | No flags. Lists registrations and last fetch; does not fetch. |

## Daemon

| Syntax | Flags/defaults and effect |
| --- | --- |
| `daemon run` | `--level LEVEL`: foreground log-level override, otherwise configured level. Levels: `debug`, `info`, `warn`, `error`. Acquires the singleton lock; stops on interruption. |
| `daemon start` | `--level LEVEL`: install the user LaunchAgent with this level; same values, default configured level. Starts under launchd; requires resolved identities and valid configuration. |
| `daemon stop` | No flags. Unloads the LaunchAgent. |
| `daemon status` | No flags. Reports loaded/running state, PID and last tick where available. |

LaunchAgent use requires macOS and a logged-in user session. See [lifecycle, PATH and restart](../operations/running-the-daemon.md).

## Logs

Syntax: `logs [ISSUE] [FLAGS]`. `ISSUE` is a positive issue number or quoted full ref such as `'github:OWNER/REPO#123'`. No issue selects the app log. Output is local and unredacted.

| Flag | Default | Scope and effect |
| --- | --- | --- |
| `--run N` | `0` | Issue: newest run at `0`, otherwise a positive run ordinal. |
| `--runs` | `false` | Issue: list runs instead of displaying one. |
| `--repo OWNER/NAME` | Empty | Issue: disambiguate a number across repositories. |
| `-f` | `false` | Issue: follow appended events until termination/interruption. |
| `--only KINDS` | Empty | Issue: comma-separated `system,text,think,tool,output,result,user,other`; shortcuts `agent` (text, think, tool) and `all`. Empty shows all except think. |
| `--full` | `false` | Issue: do not shorten tool arguments/output. |
| `--raw` | `false` | Either: raw JSONL, byte-for-byte when unfiltered. |
| `--grep REGEXP` | Empty | Either: select matching output lines. |
| `-i` | `false` | Either: case-insensitive grep. |
| `--invert` | `false` | Either: invert grep matches. |
| `-n N` | `200` | App only: last N records; positive integer. |
| `--all` | `false` | App only: whole app log. |
| `--level LEVEL` | Empty | App only: minimum `debug`, `info`, `warn` or `error`. |

App-only flags are rejected with an issue; issue-only flags require one. `--runs` selects the listing view before run rendering. Raw output bypasses kind filtering and shortening; use `--grep` for raw-line filtering. Without a grep pattern, `-i` and `--invert` have no effect. A full task ref already identifies its repository and rejects `--repo`. `-n` and `--all` cannot be combined. See [log recipes](../operations/running-the-daemon.md).

## Metering

| Syntax | Flags/defaults and effect |
| --- | --- |
| `meter [REPOSITORY [TASK-ID]]` | `--mode MODE`, `--ca AGENT`, `--model MODEL`, `--json`, `--details`. No repository means all; task ID is a positive issue number. Details include run/attempt/invocation rows. Actual-model filtering keeps unresolved attribution visible. |
| `meter export PROJECT` | `--coding_agent AGENT`, `--issue NUMBER`, `--mode MODE`, `--time_from RFC3339`, `--time_to RFC3339`. Project must be `github:OWNER/REPO`. Writes CSV to stdout. |
| `meter recover REPOSITORY TASK-ID --attempt ID` | Required `--attempt`: an existing finalized attempt matching repository/task; optional `--apply` writes supported missing historical Codex descendant usage. Default previews. Unresolved evidence prevents apply. |
| `meter prices` | No flags. Prints price-command usage. |
| `meter prices list` | `--json`; shows current local price revision. |
| `meter prices set MODEL --coding_agent AGENT --input RATE --output RATE` | Required agent and input/output rates; optional `--cache_read RATE`, `--cache_write RATE`, `--cache_write_5m RATE`, `--cache_write_1h RATE`. Creates a new revision replacing this agent/model entry. |

Repository selectors accept exact `github:OWNER/REPO`, `OWNER/REPO`, or the exact registered clone path. `AGENT` is `claude` or `codex`. Meter `MODE` currently accepts `research`, `plan`, `implement`, `spike`; `validate` is rejected by these filters. Empty filters select all. Filters combine with AND. Export time bounds are timezone-aware RFC3339: inclusive start, exclusive end, selected by attempt start; start must precede end. Export limits are 1,000,000 rows, 256 MiB encoded CSV and 30 seconds preparation. Missing values are blank.

Rates are nonnegative decimal USD per million tokens, with at most 18 integral and 9 fractional digits, not currency-formatted strings. Omitted optional rates remain unavailable, distinct from explicit `0`. Existing attempts retain pinned prices; new rates affect later attempts. See [usage and costs](../operations/usage-and-costs.md) and the retained [CSV/schema reference](../meter.md).

## Task resources and story contracts

| Syntax | Flags/defaults and effect |
| --- | --- |
| `task clean ISSUE` | `--force`: allow removal despite uncommitted changes. Removes the task worktree and clears its recorded path. ISSUE accepts a number or full task ref; ambiguous numbers need the full ref. |
| `task retire` | No flags. Advisory preview across registered resources; fetches fresh refs and checks issues. Deletes nothing and does not change automatic sweep timing. |
| `story-contract upgrade PARENT-REF` | `--preview` or `--apply`, mutually exclusive. Default is read-only preview. Explicit apply upgrades/resumes the revision-bound unfinished legacy cohort; refuses active/frozen scope or ambiguous conversion. |

Cleanup is destructive with `--force`; preserve wanted edits first. Contract upgrade requires initialized identities and forge access, preserves completed children and historical acceptance, and still requires a newly published/approved Plan. See [cleanup](../operations/cleanup.md) and [story-contract protocol](../story-contract.md).

## Teams

| Syntax | Flags/defaults and effect |
| --- | --- |
| `team list` | `--mode MODE`, `--json`; list installed central definitions, optionally one mode. |
| `team show TEAM` | `--mode MODE`, `--json`; inspect one installed team. |
| `team validate TEAM` | `--mode MODE`, `--json`; validate and inspect its captures. |
| `team add TEAM` | `--source SOURCE`, `--version SEMVER`, `--sha256 DIGEST`, `--force`; install a complete package centrally. |
| `team status [TEAM]` | `--refresh`: explicitly check upstream without installing; `--json`. Default uses recorded observations. |
| `team update [TEAM]` | `--source SOURCE --switch-source`, `--version SEMVER`, `--sha256 DIGEST` or `--clear-pin`, `--force`, `--json`. Update from recorded origins; omitted team selects installed teams. |
| `team manage SCOPE ACTION [source=PATH]` | `--json`. Scope is `shared:ID` or `team:TEAM:AGENT`; actions `set`, `unset`, `show`. Only `set` takes and requires `source=PATH`. |

`MODE` is any of the five lowercase workflow modes; empty means all. Source selection: explicit alias/HTTPS URL/local package path, otherwise configured default alias, then `teams.index_url`. Local paths must use explicit path syntax and reject release pins. Empty version follows stable releases on new remote installs; exact pins persist on update. `--sha256` is the manifest digest, not a credential. Source switching and release-pin changes require one named team; replacement source requires `--switch-source`. Changing/clearing a digest requires `--version`; `--sha256` and `--clear-pin` conflict. `--force` permits replacing modified sources. Update JSON reports the pre-update status and force choice; confirm final state with `team status`. See [team recipes and drift behavior](teams.md).

## Factory helper operations

These commands require a factory-supplied absolute attempt descriptor and live broker authority. They are advanced factory operations, not general setup or arbitrary third-party MCP configuration. Never construct a descriptor or copy its capabilities into logs.

| Syntax | Input/output and effect |
| --- | --- |
| `submission preflight --descriptor PATH` | Contribution/application JSON exclusively on stdin; JSON diagnostics on stdout. Reports discoverable problems without recording proof or approval. |
| `submission submit --descriptor PATH` | Same input route; records an authorized contribution and returns durable correlation plus independent states. |
| `submission receipt --descriptor PATH` | JSON `{"request_id":"RETURNED_ID"}` on stdin; retrieves that request's recorded outcome. |
| `mcp serve --descriptor PATH` | Serves the attempt's authorized tools over interruptible stdio. Requires a live descriptor before handshake, independently of auto-start convenience settings. |

`--descriptor` has no usable default. Use the factory's absolute executable and matching descriptor for the operation; result and checkpoint descriptors may differ. Correct all preflight diagnostics before submit. Recover a lost reply with its known request ID before retrying; corrected content uses a new request. Read input validity, engineering, acceptance and remote effects separately: acknowledgment or pending is not completion. Descriptor expiry or unavailable broker authority must remain a failure, not a claimed successful action. See [MCP guide](../guides/mcp.md) and [submission protocol](../story-contract.md).

[CLI index](README.md) · [Configuration](../configuration/README.md) · [Operations](../operations/README.md)
