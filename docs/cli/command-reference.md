# Command reference

Every `litefactory` command, with its arguments, flags, defaults and exit codes. The text comes from the command registry in the code and from `litefactory <command> --help`. If this page and `--help` ever differ, trust `--help` for your installed version.

Back to the [documentation index](../README.md) or the [CLI index](README.md).

## How commands work

```text
litefactory <command> [<subcommand>] [arguments] [flags]
```

- `litefactory --help` lists all commands. `litefactory <command> --help` shows one command's description, syntax and flags. `litefactory <group>` (for example `litefactory repo`) lists the subcommands of a group. `litefactory --version` prints the version.
- In the syntax lines below, `<name>` is required, `[name]` is optional, and `a | b` means one of the two. Do not type the brackets.
- Flags can be written `--flag` or `-flag`. A flag with a value can be written `--flag value` or `--flag=value`. Yes/no flags (for example `--json`) take no value.
- Positional arguments can come before or after the flags: `litefactory logs 123 --run 2` and `litefactory logs --run 2 123` are the same.
- Everything reads and writes under `$LF_HOME` (default `~/.litefactory`). There is no general `--config` flag. See [config.yaml](../configuration/config-yaml.md) for the settings file.
- Most commands need `litefactory init` to have run first. When they do not find the config or the database they exit with code 3 and tell you to run it.

### Exit codes

| Code | Meaning |
| --- | --- |
| `0` | Success. |
| `1` | The command ran and failed (a runtime error). |
| `2` | The command line was wrong: unknown command, unknown flag, missing or extra argument, or a value that is not valid. The command's usage is printed on stderr. |
| `3` | The command line was fine but the machine is not ready: for example no config or database, no bot credential, a repository that is not registered, `git` or `gh` missing, or a bad clone. |

`--help` and `--version` exit with `0`.

## Connector grants

These commands use the existing selected factory database without requiring a
running daemon, remote configuration, credentials or network access. All accept
`--json`; explicit invocation is sufficient without confirmation.

| Command | Arguments and result |
| --- | --- |
| `connector grants list` | No arguments. All retained grants in creation-time/ID order; an empty list succeeds. |
| `connector grants inspect` | `<grant-id>`. The same safe summary for one exact ID. |
| `connector grants revoke` | `<grant-id>`. Revoke every family under the exact grant, or report already revoked. |
| `connector grants revoke-all` | No arguments. Atomically revoke every existing grant, including old owner generations; report newly revoked count. |

Output includes factory home, reach, effective status, renewable grant lifetime
and separate unexpired access-token counts/expiries, without credentials. Times
are UTC. Successful revocation refuses the next protected request and renewal,
including connected clients, and survives reconnect and daemon restart. Other
grants remain usable after single-grant revocation.

Empty and repeated operations exit `0`; unknown retained IDs or missing database
exit `3`; syntax errors exit `2`; storage, cancellation and output failures exit
`1`. A failed output write after commit does not undo revocation. Inspect/retry
an unconfirmed operation. See [Connector grants](connector-grants.md) for coverage,
serialized revoke-all boundaries, retention and recovery limits.

## Standards book

All `standards` commands emit JSON and use the initialized local database. See
[Standards book](standards.md) for input examples, exact-review requirements and
storage/recovery limits.

| Command | Arguments and flags |
| --- | --- |
| `standards` | Show the workflow and usage guidance. |
| `standards proposal create` | `<id> --file FILE` (`-` reads stdin). |
| `standards proposal edit` | `<id> --expected-version N --file FILE`. Replaces the full change set and base. |
| `standards proposal list` | List current proposal versions. |
| `standards proposal show` | `<id> [--version N]`. Include exact retained approval and publication. |
| `standards proposal validate` / `review` | `<id> --version N`. Review includes complete before/after changes and digests. |
| `standards proposal approve` / `publish` | `<id> --version N --digest HASH`. Resolve the configured owner locally. |
| `standards category add` / `replace` | `<id> --proposal ID --expected-version N --name NAME [--description TEXT]`. |
| `standards category remove` | `<id> --proposal ID --expected-version N`. |
| `standards category list` | `[--proposal ID --version N]`. Defaults to the published book. |
| `standards book current` / `history` | Inspect complete revisions and provenance. |
| `standards book show` | `<id>`. Inspect a retained revision. |
| `standards book diff` | `<from> <to>`. Zero selects the empty unpublished base. |

Every command accepts `--json` explicitly. Invalid changes, missing records,
unpublished state, stale references, absent exact approval and storage failures
exit with `1`. Missing local setup or configured owner exits with `3`; malformed
input or flags exit with `2`.

## Initialization and repositories

### `init`

```text
litefactory init
```

Bootstraps litefactory on this machine. It creates `$LF_HOME`, writes a default `config.yaml`, creates the database, and finds the owner and bot identities.

- The owner comes from the GitHub CLI (`gh`), which must be logged in to github.com.
- The bot comes from the GitHub API, using the token that `config.yaml` points at. Without a token, `init` prints a warning and still finishes everything else.
- It is safe to run again. It refreshes both identities, adds no duplicate rows and never overwrites an existing `config.yaml`.

No flags. Exit `3` if `gh` is missing or not logged in, if `config.yaml` contains a literal secret, or if the bot token belongs to the same account as the owner (the daemon would read its own comments as instructions). In that last case nothing is stored. Read stderr even after success: an unresolved bot identity is only a warning.

### `repo add`

```text
litefactory repo add <path> [--base branch] [--worktrees dir] [--prefix name]
```

Registers the git clone at `<path>`. Owner, name and default branch are read from the clone. Nothing is fetched and nothing is written into the clone. Running it again on the same path updates the registration. Agents are built in and need no setup. After registering, it also tries to register the repository's GitHub webhook (see [`repo webhook`](#repo-webhook)); a webhook problem is printed as a warning and does not change the exit code.

| Argument or flag | Meaning |
| --- | --- |
| `<path>` | Required. A non-bare git clone with a GitHub `origin`. |
| `--base branch` | Branch to cut task branches from. Overrides `settings.default_base_branch` and the remote's HEAD. |
| `--prefix name` | First part of every task branch name. Overrides `settings.feature_prefix`. Default `lf`. |
| `--worktrees dir` | Folder to create this repository's task worktrees under. Overrides `settings.worktree_root`. Default is `.lf-worktrees/<clone-folder-name>` next to the clone. |

On a repeat registration, a flag you leave out keeps the value stored before, not the global default. Exit `3` for a path that does not exist, is not a git clone, is bare, has no usable GitHub `origin`, an unknown base branch, or a worktree folder already used by another repository. Exit `2` for an invalid base branch or prefix. See [Repositories](repositories.md).

### `repo list`

```text
litefactory repo list
```

Prints a table of every registered repository: `PATH`, `REMOTE` (as `forge:owner/name`), `BASE`, `WORKTREES` and `LAST FETCH` (UTC, or `never`). With nothing registered it says so and exits `0`. No flags.

### `repo webhook`

```text
litefactory repo webhook <owner/name> | --all
```

Makes sure the repository has the GitHub webhook that litefactory's listener expects: the `webhook.public_url` address, JSON payloads, the `issues`, `issue_comment` and `sub_issues` events, and a secret kept in the Keychain. A correct hook is left alone, so running it again changes nothing. Other hooks on the repository are never touched. One line is printed per repository.

| Argument or flag | Meaning |
| --- | --- |
| `<owner/name>` | One registered repository. |
| `--all` | Every registered repository. |

Give exactly one of the two, otherwise exit `2`. Exit `3` if the named repository is not registered. Exit `1` if any requested repository could not be fixed. That includes repositories that were skipped because `webhook.public_url` is not set. A plain `http` public URL works but prints a warning.

## Teams

Teams are versioned packages of agent instructions, kept once per machine under `$LF_HOME/teams`. See [Teams](teams.md) for how they fit together. The `--source` flag names where a team comes from: the exact catalogue URL, or `local` for teams you made with `team clone`. If you leave it out, `team install` and `team update` use `settings.team_source` from `config.yaml`; the other commands find the one installed copy and ask you to add `--source` if there is more than one match.

### `team install`

```text
litefactory team install <name> [<version>] [--source catalogue-url|local]
```

Downloads a team from the catalogue, checks it and installs it. Without `<version>` it installs the highest stable version. Prints `Installed ...` or `Already installed ...`.

### `team clone`

```text
litefactory team clone <name> <version> <new-name> <new-version> [--source catalogue-url|local]
```

Copies an installed team into an editable local team with a new name and version, in the `local` namespace. It prints the editable folder, and the origin it was copied from. Fails if the new name and version already exist.

### `team update`

```text
litefactory team update <name> [--source catalogue-url|local]
```

Installs the newest stable version of `<name>` from the same catalogue if it is newer than the highest stable version already installed. Prints `Installed update: ...` or `No update: ...`. Local teams cannot be updated. It needs an installed stable version to start from.

### `team list`

```text
litefactory team list
```

Prints one line per installed team: source, name and version. It reads local files only and does not use the network. No flags.

### `team inspect`

```text
litefactory team inspect <name> <version> [--source catalogue-url|local]
```

Verifies an installed team and prints its name, version, source, folder, checksum and workflows (editable local teams show their origin instead).

### `team remove`

```text
litefactory team remove <name> <version> [--source catalogue-url|local]
```

Removes one installed team version. It refuses, and lists the repositories, while any registered repository is linked to it. Run `team unlink` first.

### `team link`

```text
litefactory team link <name> <version> [--repo registered-clone-path] [--source catalogue-url|local]
```

Selects an installed team version for a registered clone, replacing any earlier choice. Prints `Linked ...` or `Already linked ...`. Without a team link, a repository uses the built-in workflows.

| Flag | Meaning |
| --- | --- |
| `--repo path` | The registered clone. Default: the current directory's repository. |
| `--source` | The exact installed source, when the name and version exist in more than one. |

Exit `3` if the clone is not registered.

### `team unlink`

```text
litefactory team unlink [--repo registered-clone-path]
```

Removes a clone's team link so it goes back to the built-in workflows. Safe to repeat. `--repo` works as in `team link`.

## Daemon

The daemon polls GitHub and runs the work. See [Running the daemon](../operations/running-the-daemon.md).

### `daemon run`

```text
litefactory daemon run [--level level]
```

Runs the poll loop in this process, in the foreground, until it gets SIGINT or SIGTERM. The launchd agent runs exactly this. No pid file is written. A second copy refuses to start because the first holds a lock on `$LF_HOME/daemon.lock`. `git` and `claude` are looked up before the first tick, so a wrong path fails here and not hours into a run.

| Flag | Meaning |
| --- | --- |
| `--level level` | Log level for this run only: `debug`, `info`, `warn` or `error`. Overrides `settings.log_level`; the config file is not changed. |

`--doctor-repair` and `--doctor-token` also appear in `--help`. The factory uses them when it restarts the daemon during a doctor repair. Do not set them yourself. Exit `3` if there is no database or no bot token.

### `daemon start`

```text
litefactory daemon start [--level level]
```

Writes `~/Library/LaunchAgents/com.github.cloudvane.litefactory.plist` and loads it into your login session, so the daemon runs now and at every login. It is a launch agent (not a system daemon) because `claude` signs in through the Keychain. Your current `PATH` is copied into the plist and printed. `--level` sets the log level for every future login without changing `config.yaml`. Running it again replaces and reloads the agent.

### `daemon stop`

```text
litefactory daemon stop
```

Unloads the launch agent. An agent that was not loaded is reported, not treated as a failure. The plist stays, so `daemon start` loads the same setup again.

### `daemon status`

```text
litefactory daemon status
```

Prints the agent label, whether the plist is installed, whether it is loaded and running (with its pid, which comes from launchd), when the last tick finished, and a health summary. It exits `0` even when the daemon is not running.

## Logs and diagnosis

### `logs`

```text
litefactory logs [<issue>] [--run N] [--only kinds] [--grep regexp] [--raw] [-f]
```

Reads the database and log files directly, so it works while the daemon is stopped. Output is never redacted.

**With an issue** (a number such as `123`, or a full task ref such as `github:acme/api#123`), it shows a run of that issue: a summary and then the run's events, one labelled line each. The label says what kind of line it is: `system`, `text`, `think`, `tool`, `output`, `result`, `user` or `other`. The newest run is shown unless you pick one.

**Without an issue**, it shows litefactory's own log (`daemon.log` in `$LF_HOME/logs`), 200 records by default.

| Flag | Applies to | Meaning |
| --- | --- | --- |
| `--run N` | issue | Show run N (1 or more). Default: the newest run. |
| `--runs` | issue | List the issue's runs, newest last. |
| `--repo owner/name` | issue | Pick the repository when the issue number exists in several. Not allowed with a full task ref. |
| `--only kinds` | issue | Only these kinds, comma-separated: `system`, `text`, `think`, `tool`, `output`, `result`, `user`, `other`, or `agent` or `all`. |
| `--full` | issue | Do not shorten tool arguments or output. |
| `-f` | issue | Follow the run as events are written. |
| `-n N` | app log | Show the last N records. Default 200. |
| `--all` | app log | Show the whole file. Cannot be combined with `-n`. |
| `--level level` | app log | Only records at or above `debug`, `info`, `warn` or `error`. |
| `--grep regexp` | both | Only lines matching the regular expression. |
| `-i` | both | Make `--grep` case-insensitive. |
| `--invert` | both | Show the lines `--grep` does not match. |
| `--raw` | both | Print the raw log lines (the run's JSONL) exactly as written. |

Using a flag from the wrong column exits `2`. Examples:

```sh
litefactory logs                       # litefactory's own log
litefactory logs -n 500                # the last 500 lines of it
litefactory logs 123 -f --only agent   # watch what the agents are doing
litefactory logs 123 --only tool       # every tool call in the run
litefactory logs 123 --grep 'go test'  # only lines that mention it
litefactory logs 123 --raw             # the JSONL, byte for byte
```

### `doctor`

```text
litefactory doctor <repository> <issue> <instructions>
```

Starts a read-only diagnosis of one issue, independent of its workflow. `<repository>` is `owner/name`, `<issue>` is the issue number, and `<instructions>` is what you want checked (quote it). It prints the saved conversation: findings with evidence and uncertainty, any questions, and any proposed repair steps. Needs an initialized database. A provider whose isolation cannot be verified refuses to launch. See [Doctor live diagnosis](../operations/doctor.md).

### `doctor status`

```text
litefactory doctor status <repository> <issue>
```

Shows the saved doctor conversation for the issue without starting anything.

### `doctor stop`

```text
litefactory doctor stop <repository> <issue>
```

Stops the doctor conversation for the issue. No issue comment is posted. If a doctor run is still running, it says the stop is requested and finishes when the daemon settles it.

### `doctor apply`

```text
litefactory doctor apply <repository> <issue>
```

Applies the repair proposal that doctor proposed. It only works after you have seen the current proposal in full (run `doctor status` first); otherwise it refuses. If the repair needs a restart, it asks the daemon to restart. See [Doctor owner repairs](../operations/doctor.md).

`doctor`, `status`, `stop` and `apply` have no flags. A repository that is not `owner/name` or an issue that is not a positive number exits `1`.

## Usage and costs

The meter commands read measured token usage and the prices you set. See [Usage and costs](../meter.md) for what the numbers mean.

### `meter`

```text
litefactory meter [<repository> [<task-id>]] [--mode MODE] [--ca AGENT] [--model MODEL] [--json] [--details]
```

Shows measured tokens, frozen cost estimates and missing coverage. With no arguments it covers everything; `<repository>` limits it to one registered repository (a registered path, `owner/name` or `github:owner/name`), and `<task-id>` to one issue number in it.

| Flag | Meaning |
| --- | --- |
| `--mode MODE` | Only `research`, `plan`, `implement` or `spike`. |
| `--ca AGENT` | Only `claude` or `codex`. |
| `--model MODEL` | Only this actual model. Work whose model is unknown stays visible. |
| `--json` | Print JSON instead of text. Unknown numbers are `null`. |
| `--details` | Add run, attempt and invocation rows. |

An unknown or ambiguous repository, a bad filter value or a `<task-id>` that is not a positive number exits `2`.

### `meter export`

```text
litefactory meter export <project> [--coding_agent AGENT] [--issue NUMBER] [--mode MODE] [--time_from RFC3339] [--time_to RFC3339]
```

Writes one registered GitHub project's measurements as CSV on standard output. `<project>` must be written `github:owner/name`. Filters combine with AND. The columns are in [Usage and costs](../meter.md#csv-export).

| Flag | Meaning |
| --- | --- |
| `--coding_agent AGENT` | Only `claude` or `codex`. |
| `--issue NUMBER` | Only this issue number (positive). |
| `--mode MODE` | Only `research`, `plan`, `implement` or `spike`. |
| `--time_from RFC3339` | Only attempts that started at or after this instant. Needs a time zone, for example `2026-09-01T00:00:00Z`. |
| `--time_to RFC3339` | Only attempts that started before this instant. |

Limits: 1,000,000 rows, 256 MiB of CSV, 30 seconds to prepare. Past a limit it fails before printing anything; narrow the filters. Check the exit code before using a file you redirected into: an error part-way can leave a partial file.

### `meter recover`

```text
litefactory meter recover <repository> <task-id> --attempt ID [--apply]
```

Previews, and with `--apply` records, missing Codex sub-agent usage in a finished attempt. Without `--apply` nothing changes. The repository, issue and attempt must match. Find the attempt ID with `meter <repository> <task-id> --details`.

| Flag | Meaning |
| --- | --- |
| `--attempt ID` | Required. An existing finished attempt. |
| `--apply` | Record the missing usage that the evidence supports. |

Missing `<repository>`, a bad `<task-id>` or no `--attempt` exits `2`.

### `meter prices`

```text
litefactory meter prices <list|set>
```

Prints the two forms below. Rates are exact USD per million tokens, and edits apply only to later attempts.

### `meter prices list`

```text
litefactory meter prices list [--json]
```

Shows the current price list. `--json` prints JSON.

### `meter prices set`

```text
litefactory meter prices set <model> --coding_agent AGENT --input RATE --output RATE [--cache_read RATE] [--cache_write RATE] [--cache_write_5m RATE] [--cache_write_1h RATE]
```

Creates a new price list that replaces the entry for one agent and model, and prints the new revision number. Rates you leave out become unknown for that entry.

| Flag | Meaning |
| --- | --- |
| `--coding_agent AGENT` | Required. `claude` or `codex`. |
| `--input RATE`, `--output RATE` | Required. USD per million tokens. |
| `--cache_read RATE`, `--cache_write RATE`, `--cache_write_5m RATE`, `--cache_write_1h RATE` | Optional rates. |

A rate is a plain non-negative decimal with up to 18 digits before and 9 after the point (for example `3` or `0.30`). A model name longer than 256 characters, or a missing required rate, exits `2`.

## Tasks

### `task clean`

```text
litefactory task clean <issue> [--force]
```

Removes the git worktree made for an issue and clears its recorded path. `<issue>` is a number or a task ref such as `github:acme/api#123`. The branch and its commits are left alone. It refuses a worktree with uncommitted changes unless you pass `--force`. The daemon also cleans closed issues once a day (see `settings.worktree_retain_days`). See [Cleanup](../operations/cleanup.md).

### `task retire`

```text
litefactory task retire
```

Shows, without deleting anything, what automatic cleanup would keep or remove for each registered task's worktrees and branches. It fetches fresh remote refs and checks each issue directly, so it needs the bot credential (exit `3` without it). No flags.

## Rights and reviewers

Rights say who may act on a tagged workflow action. Reviewers are people a task is routed to for attention. A reviewer assignment grants no right. All of these need `--repo owner/name` for a registered repository (exit `3` if it is not registered) and the owner identity from `init`. Without `--json` they print a plain table; with `--json`, indented JSON.

### `rights catalogue`

```text
litefactory rights catalogue --repo owner/name [--json]
```

Lists the actions rights can be granted for: one `workflow | stage | action` entry per workflow action that is tagged for rights. Untagged actions are not listed.

### `rights list`

```text
litefactory rights list --repo owner/name [--all] [--json]
```

Lists active grants. `--all` also lists revoked ones.

### `rights grant`

```text
litefactory rights grant <login> --repo owner/name --workflow W --stage S --action A [--task REF] [--expires RFC3339] [--json]
```

Gives a GitHub user the right to take one action. The login is turned into its numeric GitHub ID once, now (this uses the bot credential). `--workflow`, `--stage` and `--action` come from `rights catalogue`; `--action` is `answer`, `request_changes` or `approve`. `--task` limits the grant to one task (a number or a ref such as `github:owner/name#12`). `--expires` is a UTC time in RFC 3339 form. It is refused (exit `3`) for an unknown repository, an action not in the catalogue, an expiry in the past, or a duplicate of an active grant.

### `rights revoke`

```text
litefactory rights revoke <grant-id> --repo owner/name [--json]
```

Revokes a grant. The row and its history stay.

### `rights history`

```text
litefactory rights history --repo owner/name [--json]
```

Shows who changed which rights and reviewers, and when.

### `reviewer assign`

```text
litefactory reviewer assign <login> --repo owner/name --task REF --workflow W --stage S [--replace] [--json]
```

Assigns a reviewer to a task and stage. `--replace` first removes the open reviewers of that task and stage, in one change.

### `reviewer remove`

```text
litefactory reviewer remove <assignment-id> --repo owner/name [--json]
```

Removes an assignment. The row and its history stay.

### `reviewer list`

```text
litefactory reviewer list --repo owner/name [--task REF] [--all] [--json]
```

Lists open assignments. `--task` limits it to one task; `--all` also lists removed ones.

## Story contracts

### `story-contract upgrade`

```text
litefactory story-contract upgrade <parent-ref> [--preview | --apply]
```

Upgrades the unfinished stories of an older plan to the current story contract. `<parent-ref>` is the GitHub parent issue as a task ref, for example `github:acme/api#10`. The default and `--preview` only show the revisions and any conflicts; nothing changes. `--apply` records the upgrade; it refuses active or frozen work and ambiguous conversions, and can resume a half-finished publication. Finished stories and past acceptance are not changed. An upgrade still needs the next published and approved Plan before engineering continues. Give only one of `--preview` and `--apply`. It needs the bot credential, and exits `1` while conflicts remain.

## Factory-internal commands (not for everyday use)

These two groups are run by the factory for a running agent. They need a descriptor file that the factory writes for each attempt, and they do nothing useful on their own. You normally never type them. They are listed here only so every registered command is documented. See [MCP](../guides/mcp.md) for the user-facing side.

### `submission preflight`, `submission submit`, `submission receipt`

```text
litefactory submission preflight --descriptor <absolute-path>
litefactory submission submit    --descriptor <absolute-path>
litefactory submission receipt   --descriptor <absolute-path>
```

Each reads one JSON request from standard input, sends it to the current attempt's local feedback broker, and prints the JSON response. `preflight` checks a contribution without recording it; `submit` records it; `receipt` looks up what happened to an earlier submission. `--descriptor` is the absolute path of the file the factory supplied. Exit `1` if the call fails (the error is also in the JSON).

### `mcp serve`

```text
litefactory mcp serve --descriptor <absolute-path>
```

Serves the current attempt's tools to an agent over standard input and output (the MCP protocol). It needs a live, authorized descriptor and exits `1` otherwise. Coding agents start it themselves.
