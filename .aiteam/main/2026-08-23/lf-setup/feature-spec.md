# Feature Spec: litefactory M0 — Walking Skeleton
# Status: APPROVED
# Date: 2026-08-23
# Scope: M0 only (LF-0 … LF-5). M1/M2/M3 explicitly excluded.

## Problem

There is no way to hand a GitHub issue to aiteam without sitting at a terminal. Work that is fully specified in an issue still requires a human to open a shell, create a branch, run `claude`, read the output and paste a summary back.

M0 proves one loop closes, end to end, on real infrastructure: **a comment on a real GitHub issue causes a real aiteam run on this machine and a real comment back.** Nothing more. No gates, no artifacts, no permalinks, no multi-turn.

## M0 Success Condition

On a private GitHub repo registered with litefactory, Mario comments `/spike bump the golang.org/x/net dependency` on issue #123. Within one poll tick the daemon reacts 👀. It creates worktree + branch `lf/123-bump-the-golangorgxnet-dependency`, runs `claude -p` with an aiteam `/spike` prompt, and posts a comment carrying the `lf:` marker containing the run summary, files touched, exit status and a collapsible redacted log tail. The branch exists and can be inspected. `litefactory logs 123` shows the run locally with full, unredacted detail. The registered clone's branch and working tree are bit-identical to what they were before.

---

## Settled Decisions

| # | Decision | Consequence for M0 |
|---|---|---|
| D-1 | **Language: Go.** Single static binary, cross-compilable. | No cgo anywhere in the dependency tree. |
| D-2 | **Branch → artifact path (brief Q1) RESOLVED.** aiteam derives the path component with `git rev-parse --abbrev-ref HEAD \| tr '/' '-' \| tr '[:upper:]' '[:lower:]'`. Branch `lf/123-slug` → flat directory component `lf-123-slug`. One level, slash→hyphen, lowercased. | Informational only at M0 (no artifact discovery). Load-bearing at LF-7. |
| D-3 | **Feature slug ownership (brief Q2) RESOLVED.** The slug is consumed by aiteam's *top-level dispatcher*, not the mode orchestrators. In headless `claude -p`, litefactory states the slug in the prompt text and top-level Claude uses it instead of prompting. | No change to aiteam required. M0 states the slug in the prompt even though `/spike` needs no session directory. |
| D-4 | **Slug derivation (brief Q6): always derived, never prompted.** `<issue-number>-<slugified-title>`, truncated to 40 chars on a word boundary. `--feature` overrides. Deterministic — recomputable after DB loss. | See "Slug derivation rules" below. |
| D-5 | **Worktree lifecycle (brief Q5): no auto-removal.** Ship `litefactory task clean <id>` and the `worktree_retain_days` config key. The automatic sweep is later. | M0 = command + config key only. No daemon sweep. |
| D-6 | **PR review comments as input (brief Q4): out of scope.** | Not in v1 M0..M3 planning. |
| D-7 | **SQLite driver: `modernc.org/sqlite`** — pure Go, cgo-free. | Preserves single-binary cross-compilation (D-1). |
| D-8 | **Daemon process model: plain foreground binary supervised by launchd.** `litefactory daemon start` writes/loads a launchd plist rather than self-daemonizing with a PID file. systemd on Linux is later. | No fork/setsid/PID-file logic. See LF-2. |

---

## Shared M0 Mechanics

These are referenced by the story acceptance criteria below and are themselves testable.

### S-1 Disk layout

| Path | Contents |
|---|---|
| `$LF_HOME` (default `~/.litefactory`, overridable by `LF_HOME`) | Everything litefactory owns |
| `$LF_HOME/litefactory.db` | SQLite database (S-2) |
| `$LF_HOME/config.yaml` | Global defaults. **Never secrets** (S-4) |
| `$LF_HOME/logs/daemon.log` | Daemon log, line-oriented, unredacted |
| `$LF_HOME/logs/runs/<forge>/<owner>/<repo>/<issue>/NNNN.jsonl` | Raw Claude Code event stream for run NNNN |
| `$LF_HOME/logs/runs/<forge>/<owner>/<repo>/<issue>/NNNN.meta.json` | Run metadata (S-3) |

- `NNNN` is the run ordinal **per task**, zero-padded to 4 digits, starting at `0001`.
- The ordinal is allocated by inserting the `runs` row **before** the subprocess starts, so a crashed run still owns its number and never collides.
- `$LF_HOME` is created with mode `0700`; `litefactory.db` and `config.yaml` with `0600`.
- Deleting `$LF_HOME` loses history and execution state and never loses code.

### S-2 Database tables required by M0

Only columns M0 actually reads or writes are listed. Additional columns are out of scope.

**`identities`** — one row per (forge, role).

| Column | Type | Notes |
|---|---|---|
| `forge` | text | `github` at M0 |
| `role` | text | `owner` \| `bot` |
| `handle` | text | e.g. `marioatcloudvane`, `litefactory[bot]` |
| `source` | text | `gh` for owner, `forge-api` for bot |
| `derived_at` | timestamp | |

Primary key `(forge, role)`. `init` upserts.

**`repos`**

| Column | Type | Notes |
|---|---|---|
| `id` | integer | surrogate key |
| `path` | text, unique | absolute path to the registered clone |
| `forge` / `owner` / `name` | text | parsed from the remote URL |
| `remote_url` | text | verbatim `origin` URL |
| `base_branch` | text | default from `--base`, else config default, else remote HEAD |
| `feature_prefix` | text | default `lf` |
| `worktree_root` | text | resolved absolute path (S-6) |
| `aiteam_team` | text | recorded at add; nullable if undetectable |
| `aiteam_verified_at` | timestamp | |
| `last_fetch_at` | timestamp | updated by LF-3 |
| `created_at` / `updated_at` | timestamp | |

**`sessions`** — one row per task litefactory has ever touched.

| Column | Type | Notes |
|---|---|---|
| `id` | integer | |
| `repo_id` | integer | FK `repos.id` |
| `issue_number` | integer | |
| `task_ref` | text, unique | canonical `github:owner/name#123` |
| `feature_slug` | text | derived per D-4, frozen on first run |
| `branch` | text | `lf/<issue>-<slug>` |
| `worktree_path` | text | absolute, nullable until first run |
| `state` | text | M0 values: `idle` \| `running` \| `failed` |
| `lock_owner` | text | daemon instance id, nullable |
| `lock_acquired_at` | timestamp | nullable |
| `created_at` / `updated_at` | timestamp | |

**`segments`** — M0 always creates exactly one per session; the table exists so the claude session id is recorded and LF-8/LF-17 need no migration.

| Column | Type | Notes |
|---|---|---|
| `id` | integer | |
| `session_id` | integer | FK |
| `ordinal` | integer | `1` at M0 |
| `claude_session_id` | text | the value passed to `claude --session-id` |
| `created_at` | timestamp | |

**`runs`**

| Column | Type | Notes |
|---|---|---|
| `id` | integer | |
| `session_id` / `segment_id` | integer | FK |
| `ordinal` | integer | per-session; matches `NNNN` in the log filename |
| `mode` | text | `spike` at M0 |
| `trigger_comment_id` | integer, unique per session | the forge comment that caused this run |
| `status` | text | `running` \| `succeeded` \| `failed` \| `reported` |
| `exit_code` | integer | nullable while running |
| `started_at` / `finished_at` | timestamp | |
| `duration_ms` | integer | |
| `cost_usd` | real | nullable, from the terminal `result` event |
| `log_path` | text | absolute path to the JSONL |
| `commit_sha` | text | nullable; the commit this run produced |
| `result_comment_id` | integer | nullable until the report is posted |

**`comments`** — required because LF-2 mandates that third-party comments be *recorded*.

| Column | Type | Notes |
|---|---|---|
| `id` | integer | |
| `session_id` | integer | FK |
| `forge_comment_id` | integer, unique per session | |
| `author_handle` | text | |
| `author_is_bot` | boolean | forge-reported |
| `body` | text | verbatim, unredacted |
| `created_at` | timestamp | forge timestamp |
| `classification` | text | `instruction` \| `third_party` \| `self` |
| `tag` | text | `c4`-style, assigned to `third_party` only; unused at M0, consumed by `/include` at M1 |

**`watermarks`**

| Column | Type | Notes |
|---|---|---|
| `session_id` | integer, PK | |
| `last_comment_id` | integer | highest **consumed** comment id |
| `updated_at` | timestamp | |

**`poll_cursor`** — single-row table holding the repo-set-wide `tasks_updated_since` timestamp and `last_tick_at`.

### S-3 Run metadata file (`NNNN.meta.json`)

Written on run completion **and** best-effort on abnormal termination. Required keys: run ordinal, task ref, session id, segment ordinal, claude session id, mode, model (from the `init` event), the full prompt as sent, worktree path, branch, `sha_before`, `sha_after`, exit code, `started_at`/`finished_at` (RFC 3339), `duration_ms`, `cost_usd`, files touched, `result_comment_id`, `error` (nullable string). Unredacted.

### S-4 Configuration (`config.yaml` at M0)

| Key | Default | Meaning |
|---|---|---|
| `poll_interval_seconds` | `30` | Tick interval. Clamped to 10…300; out-of-range is a startup error, not a silent clamp |
| `default_base_branch` | *(empty)* | Fallback when `repo add --base` is omitted and remote HEAD is undeterminable |
| `feature_prefix` | `lf` | Branch prefix |
| `worktree_root` | *(empty)* | Global override; empty means the per-repo sibling default (S-6) |
| `worktree_retain_days` | `14` | Recorded for the future sweep (D-5). Read and validated at M0; nothing acts on it |
| `github.api_base` | `https://api.github.com` | |
| `github.token_keychain.service` / `.account` | *(empty)* | macOS keychain reference |
| `github.token_env` | *(empty)* | Env-var name indirection |
| `claude.binary` | `claude` | Resolved on `PATH` if not absolute |
| `claude.allowed_tools` | *(explicit list, no default-allow)* | Passed through to `--allowedTools` |
| `claude.timeout_seconds` | `1800` | Hard kill after this; treated as a failed run |
| `log_level` | `info` | |

Unknown top-level keys are a startup error (typo protection), not ignored.

### S-5 Credential handling

- Two credentials, never mixed: the **bot forge token** (API read/write) and **Mario's existing SSH key** (git push). litefactory never uses `gh` as an API transport — `gh` is identity discovery only.
- Resolution order for the bot token: (1) `github.token_keychain` → macOS keychain lookup for that service/account; (2) `github.token_env` → the named environment variable of the daemon process. Exactly one may be configured; both configured is a startup error.
- If the configured reference resolves to empty or missing, the daemon **refuses to start** with a message naming the reference it tried.
- Any config value that looks like a literal credential (matches the redaction patterns in S-7) is a **hard startup error**, regardless of key.
- The bot handle is resolved from the token at `init` and at daemon start; `bot handle == owner handle` is a **hard error** at both points — the daemon refuses to start (this is the M0 subset of `doctor`; the `doctor` command itself is out of scope).
- Under launchd the daemon does not inherit the interactive shell environment; when `token_env` is used, `daemon start` must materialise that variable into the plist's environment from the current shell at install time, and say so.

### S-6 Worktree and branch conventions

- Default `worktree_root` for a repo at `/Users/x/dev/acme-api` is `/Users/x/dev/.lf-worktrees/acme-api`, i.e. a sibling of the clone. Global `worktree_root` config or `repo add --worktrees` overrides.
- Worktree path: `<worktree_root>/task-<issue>`.
- Branch: `<feature_prefix>/<issue>-<slug>`, created from `origin/<base_branch>` after fetch.
- **Slug derivation rules (D-4):** lowercase the issue title; replace any run of non-alphanumeric characters with a single hyphen; strip leading/trailing hyphens; truncate to 40 characters, cutting back to the last hyphen so no partial word survives; strip a trailing hyphen. Empty result (title is all punctuation/non-Latin) → `task`. The full branch name is `lf/<issue>-<slug>` and the issue number is never truncated.
- **Registered-clone commands are an allowlist:** `git fetch --prune`, `git worktree add/list/remove`, `git rev-parse`, `git remote get-url`, `git config --get`, `git show-ref`. `checkout`, `switch`, `stash`, `reset`, `merge`, `pull`, `commit` are never executed against the registered clone. All mutating work happens in the worktree.

### S-7 Outbound redaction

Applied to **every** forge-bound body, as the last transformation before the API call, unconditionally. Never applied to `$LF_HOME` logs — the local copy is always unredacted.

Must cover, at minimum:

| Pattern class | Examples |
|---|---|
| The resolved bot token, literally | exact-string match on the live token value |
| Values of secret-shaped env vars in the daemon environment | any var whose name matches `TOKEN\|SECRET\|PASSWORD\|PASSWD\|KEY\|CREDENTIAL\|API_KEY` with a value of ≥8 chars, matched by value |
| GitHub token formats | `ghp_`, `gho_`, `ghu_`, `ghs_`, `ghr_`, `github_pat_` prefixes |
| Bearer/Authorization material | `Authorization: …` lines, `Bearer <token>` |
| Cloud keys | `AKIA…` / `ASIA…` access key ids |
| Private keys | `-----BEGIN … PRIVATE KEY-----` through `-----END … PRIVATE KEY-----`, whole block |
| Generic URL credentials | `scheme://user:password@host` userinfo |

Replacement is the fixed literal `[redacted]`. Each redaction increments a counter written to `daemon.log` with the pattern class that fired (never the matched text). Redaction failure (panic in the pass) means **the comment is not posted** — failing closed is correct here.

### S-8 Comment marker and self-filter

- Every outbound comment body begins with an HTML comment marker carrying the task ref and run ordinal, in the shape shown in brief §6.
- The self-filter drops a comment if **any** of: author handle equals the stored bot handle; the forge reports the author as a bot; the body contains the literal `<!-- lf:` anywhere. This is one code path — LF-14's owner-handle check is the same filter, not a second one.

### S-9 Failure surfacing (non-zero `claude` exit)

A run is failed if the process exits non-zero, is killed by the `claude.timeout_seconds` timeout, or terminates without emitting a terminal `result` event.

In every failed case: exit code (or `-1` plus a reason string for signal/timeout) is stored on the `runs` row and in `NNNN.meta.json`; the run status becomes `failed`; the session state becomes `failed`; and a **failure comment** is posted (never silence) containing the exit code or kill reason, the elapsed duration, the absolute local log path, and a collapsible `<details>` block with the last 40 lines of the run's stderr and stream text, redacted per S-7. Partial JSONL is retained exactly as captured — never truncated or deleted on failure.

The failure comment is a report; posting it **does** advance the watermark (see LF-4). Automatic retry is not attempted at M0.

---

## LF-0 — Derive identity once

> As Mario, `litefactory init` works out who I am from the git tooling already logged in on this machine.

**Scope.** `litefactory init` only: create `$LF_HOME`, write a default `config.yaml` if absent, create/migrate the database, derive and store the owner handle, and — if a bot credential is configured — resolve and store the bot handle. GitHub / `gh` only.

**Acceptance criteria**

- [ ] `litefactory init` on a clean machine creates `$LF_HOME` (`0700`), `config.yaml` (`0600`, defaults per S-4, no secrets), and `litefactory.db` (`0600`) with the S-2 schema.
- [ ] Owner identity is derived by invoking `gh` (auth check, then the authenticated-user API call) and reading the login; it is stored as `identities(forge='github', role='owner')` with `source='gh'`.
- [ ] The derived handle is echoed to the operator on success in the form `github: <handle>`.
- [ ] `gh` absent from `PATH`, or present but unauthenticated, or returning a non-zero exit: `init` **fails loudly** with a non-zero exit, printing the exact command attempted and its stderr. No partial identity row is written.
- [ ] A bot credential configured per S-5 is resolved and its handle stored as `identities(role='bot')`. No bot credential configured is **not** an `init` failure — `init` prints a warning naming what still has to be configured before the daemon can run.
- [ ] If both handles are known and `bot handle == owner handle`, `init` exits non-zero with an explicit self-instruction-loop explanation and stores neither.
- [ ] Re-running `init` is idempotent: it refreshes both handles, never duplicates rows, and never overwrites an existing `config.yaml`.
- [ ] The `gh` credential is used for identity discovery only. No code path outside `init` invokes `gh`.
- [ ] Only `github.com` is supported; a `gh` configuration whose active host is not `github.com` fails with a clear message.

---

## LF-1 — Register an existing clone

> As Mario, `litefactory repo add /Users/mariomh/dev/acme-api` tracks a repo that lives where I already keep it — no owner flag, no identity to type.

**Scope.** `litefactory repo add <path> [--base <branch>] [--worktrees <path>] [--feature-prefix <p>]` and `litefactory repo list`. `repo rm` and `repo pause` are out of scope.

**Acceptance criteria**

- [ ] The path is resolved to an absolute, symlink-resolved path and must be an existing git repository with an `origin` remote; otherwise the command exits non-zero naming which check failed.
- [ ] `forge:owner/name` is parsed from the `origin` URL for both SSH (`git@github.com:owner/name.git`) and HTTPS (`https://github.com/owner/name.git`) forms, with the `.git` suffix stripped. A non-GitHub host exits non-zero stating that only GitHub is supported at this build.
- [ ] Base branch resolution order: `--base` flag → `default_base_branch` config → remote HEAD. If none resolve, the command exits non-zero asking for `--base`.
- [ ] **aiteam presence is verified** by: a `CLAUDE.md` at the repo root that mentions aiteam, **or** an existing `.aiteam/` directory. Neither present → the repo is **not** registered and the message names `npx aiteam` as the fix. litefactory never installs aiteam itself.
- [ ] The aiteam team is recorded when detectable from `CLAUDE.md` / `.aiteam/`; when not detectable the repo is still registered with a null team and a warning.
- [ ] `worktree_root` is resolved at add time per S-6 and stored absolute.
- [ ] **Nothing is written inside the repo.** After `repo add`, `git status --porcelain`, the current branch, and the index in the registered clone are byte-identical to before. Verified by an automated test that dirties the working tree first and asserts the dirt survives untouched.
- [ ] `repo add` on an already-registered path is idempotent: it updates the mutable fields (base, worktree root, prefix, team) and creates no duplicate row.
- [ ] `repo list` prints, per repo: local path, remote `forge:owner/name`, base branch, aiteam team, worktree root, last fetch time (`never` when unset).
- [ ] Registering a repo performs no network calls beyond what `git remote get-url` needs (i.e. none).

---

## LF-2 — Poll for your comments

> As Mario, when I comment on an issue in a tracked repo, the daemon notices within a tick.

**Scope.** The daemon loop, launchd integration, the single cross-repo poll query, watermarks, the self-filter, comment classification, the 👀 reaction, and `/spike` command parsing. Reacting to the parsed command is LF-3.

**Acceptance criteria — process model (D-8)**

- [ ] `litefactory daemon run` runs the loop in the foreground, logging to stdout and `daemon.log`, and exits cleanly on SIGTERM/SIGINT after finishing or abandoning the current tick. It is directly runnable in a terminal.
- [ ] `litefactory daemon start` writes a launchd user-agent plist under `~/Library/LaunchAgents/` with label `dev.litefactory.daemon`, invoking `litefactory daemon run`, and loads it. Re-running replaces and reloads the plist without error.
- [ ] `litefactory daemon stop` unloads the agent. `litefactory daemon status` reports loaded/not-loaded, running/not-running, PID when running, and the `last_tick_at` value from the database.
- [ ] The daemon never forks, never self-daemonizes, and writes no PID file.
- [ ] Two concurrent daemon instances are prevented by an exclusive lock on the database file; the second exits non-zero with a clear message.

**Acceptance criteria — polling**

- [ ] Tick interval defaults to 30s and is always ≤ 60s in effect.
- [ ] Each tick issues **one** forge query covering all registered repos, asking for tasks updated since the persisted `tasks_updated_since` cursor.
- [ ] The cursor is persisted after each successful tick and survives daemon restart. It is initialised to "now" at first ever tick, so pre-existing history never causes a stampede.
- [ ] **First-sight bootstrap:** when a task is observed for the first time, its watermark is set to the highest comment id existing at that moment, *without* processing those comments. Historical comments never trigger a run.
- [ ] Comments are fetched per touched task, filtered to `id > watermark`, and processed in ascending id order.
- [ ] Forge rate-limit responses (403/429 with rate-limit headers) cause exponential backoff with a logged message; the daemon does not crash, does not lose the cursor, and resumes automatically.
- [ ] Transport errors on a tick leave both cursor and watermarks unchanged, so the next tick redoes the work.

**Acceptance criteria — filtering and classification**

- [ ] The self-filter (S-8) drops bot-authored and `lf:`-marked comments before anything else. A litefactory status comment never causes a run — verified by an automated test that feeds the daemon its own posted comment.
- [ ] A comment authored by the stored owner handle is classified `instruction`.
- [ ] A comment from any other human is stored in `comments` with classification `third_party` and the next `cN` tag for that session, and **triggers nothing** — no run, no reaction, no state change, no reply.
- [ ] A task opened by someone else never auto-starts anything; only comments are ever acted on, never the issue body alone.
- [ ] A 👀 reaction is added to any comment classified `instruction` within the same tick in which it is first seen, before the run starts, as a liveness signal. Reaction failure is logged and does not abort the turn.

**Acceptance criteria — parsing**

- [ ] Only `/spike <text>` is a recognised, actionable command at M0. It must be the first non-whitespace token of the comment body.
- [ ] `/research`, `/plan`, `/implement`, `/approve`, `/include`, `/status`, `/stop`, `/retry`, `/reset` and any unknown `/word` receive a single short reply comment stating the command is not supported in this build, spawn nothing, and advance the watermark. Commands are never forwarded blindly.
- [ ] Free text from the owner with no leading slash command is recorded and ignored (there are no active multi-turn sessions at M0), and the watermark advances.
- [ ] `/spike` with an empty argument is answered with a reply comment asking for text, and spawns nothing.
- [ ] Watermark advance is defined by LF-4: it happens only after the resulting comment (report, failure or unsupported-command reply) has been posted successfully.

---

## LF-3 — Run aiteam in a worktree

> As Mario, `/spike <text>` creates a worktree and runs Claude Code with aiteam installed.

**Scope.** Lock, prepare, run. Per-task lock; fetch; worktree/branch creation; prompt assembly; `claude -p` invocation; event stream capture; commit and push of whatever the run produced; exit capture. Plus `litefactory task clean <id>` per D-5. Explicitly **not** in scope: artifact discovery, permalinks, gate pre-checks, resume, queueing.

**Acceptance criteria — lock**

- [ ] A per-task lock is acquired before preparation. If it is already held, the triggering comment is **left unconsumed** (watermark not advanced) and retried on a later tick. No queue, no coalescing, no "busy" reply at M0.
- [ ] A lock held by a daemon instance id that is not the current one is reclaimed on daemon start, and the reclaim is logged.
- [ ] The lock is released on every exit path including panic and non-zero `claude` exit.
- [ ] A run row already exists for this `(session, trigger_comment_id)` in status `reported` → the turn is a no-op, guaranteeing crash-replay never double-runs the same comment.

**Acceptance criteria — prepare**

- [ ] `git fetch --prune` runs against the **registered clone** first; `last_fetch_at` is updated. Fetch failure aborts the turn with a failure comment (S-9) and no worktree side effects.
- [ ] The feature slug is derived per D-4/S-6 on first run for the task and frozen on the session row; subsequent runs reuse it.
- [ ] The branch is `lf/<issue>-<slug>`, created from `origin/<base_branch>`. If the branch already exists locally or on the remote, it is reused, not recreated.
- [ ] The worktree is created at `<worktree_root>/task-<issue>`, creating parent directories as needed. If a valid worktree for that branch already exists at that path it is reused. If the path exists but is not a worktree for that branch, the turn fails loudly rather than deleting or overwriting anything.
- [ ] After the whole turn, the registered clone's HEAD, current branch and `git status --porcelain` are identical to their pre-turn values. Verified by an automated test with a dirty clone.

**Acceptance criteria — prompt**

- [ ] The prompt is assembled by litefactory and consists of, in order: a header naming the task ref and the feature slug and instructing that no interactive prompt for a feature name is to be issued (D-3); the aiteam `/spike` command with the text following `/spike` in the triggering comment; the issue title and body; and the triggering comment verbatim. Issue text and comment text are fenced/delimited so they read as material.
- [ ] The full prompt as sent is persisted in `NNNN.meta.json` (S-3).

**Acceptance criteria — run**

- [ ] `claude` is invoked in headless mode with the worktree as the working directory, streaming JSON event output with verbose enabled, an explicit `--session-id`, and an explicit `--allowedTools` value taken from `claude.allowed_tools`. There is no implicit allow-all.
- [ ] The `--session-id` is deterministic from task ref plus segment ordinal, and is recorded on the `segments` row.
- [ ] Every event received is written to `NNNN.jsonl` as it arrives (streamed, not buffered to completion), so `-f` can follow a live run and a crash preserves partial output.
- [ ] Model and tool surface are read from the `init` event; final result text and cost are read from the terminal `result` event; both land in `NNNN.meta.json`.
- [ ] The `claude` binary being missing or non-executable fails the turn before any worktree is created, with a message naming the configured binary.
- [ ] A run exceeding `claude.timeout_seconds` is killed and treated as a failed run per S-9.
- [ ] Non-zero exit is captured and surfaced per S-9 — never swallowed, never logged-and-ignored.

**Acceptance criteria — commit and push**

- [ ] After the run, changes in the worktree (tracked and untracked, excluding ignored) are committed on the task branch, authored as Mario with his normal git identity, with a message summarising the spike and the machine-readable trailers `Litefactory-Task`, `Litefactory-Mode: spike`, `Litefactory-Run: <ordinal>`.
- [ ] The branch is pushed to `origin` using the ambient SSH credential — never the bot token.
- [ ] If the run produced no changes, no commit and no push occur, and the report comment says so explicitly.
- [ ] `sha_before`, `sha_after` and the resulting `commit_sha` are recorded. Push failure does not lose the commit: it is reported in the comment with the local worktree path so the branch is still inspectable.

**Acceptance criteria — cleanup command (D-5)**

- [ ] `litefactory task clean <issue>` removes the worktree for that task via `git worktree remove` and clears `worktree_path` on the session. It refuses (non-zero, no action) if the worktree has uncommitted changes, unless `--force` is given.
- [ ] `worktree_retain_days` is read and validated from config; no automatic removal happens at M0.
- [ ] No worktree is ever removed automatically.

---

## LF-4 — Report back

> As Mario, when the run finishes I get a comment with what happened.

**Scope.** Composing and posting exactly one report comment per run, redaction, and watermark advance. Not in scope: editing a live status comment, labels, permalinks, PRs.

**Acceptance criteria**

- [ ] Exactly one comment is posted per completed run — success or failure. Silence is never an outcome.
- [ ] The body begins with the `lf:` marker carrying task ref and run ordinal (S-8).
- [ ] A successful report contains: the run's final result text as the summary; the list of files touched; the branch name and, when pushed, the commit SHA as plain text; the exit status; the run duration; and a collapsible `<details>` block with the last ~40 lines of run output.
- [ ] "Files touched" is a plain `git`-derived file list between `sha_before` and the run's commit plus untracked additions. It is **not** aiteam artifact discovery and makes no claim about `.aiteam/` state.
- [ ] A failed run posts a **failure** comment per S-9, clearly distinguishable from success at a glance, including the absolute local log path.
- [ ] The entire body passes through the S-7 redaction pass immediately before posting. An automated test injects a token-shaped string into the run output and asserts it does not reach the outbound body while remaining present verbatim in `$LF_HOME/logs`.
- [ ] Bodies exceeding the forge's comment size limit are truncated from the middle of the log tail (never the summary), with an explicit truncation notice and the local log path.
- [ ] **The watermark advances only after the comment post returns success**, and the run row moves to `reported` in the same transaction. If the post fails, the watermark does not move, the run row keeps its state, and the next tick re-attempts the *post* — it does not re-run `claude` (guaranteed by the `(session, trigger_comment_id)` uniqueness rule in LF-3).
- [ ] Comment posting uses the bot credential (S-5) and never `gh` or Mario's identity.

---

## LF-5 — See it locally

> As Mario, `litefactory logs 123` shows everything the forge comment left out.

**Scope.** The `logs` command over locally stored runs. Not in scope: `litefactory status`, `runs --failed`, `tail`, timeline of modes/segments/transitions beyond what M0 records.

**Acceptance criteria**

- [ ] `litefactory logs <issue>` lists that task's runs, one line each: ordinal, start time, duration, exit code, mode, status, trigger comment id, result comment id, log path. Newest last.
- [ ] When the issue number is ambiguous across registered repos, the command exits non-zero listing the candidate `owner/name` values and requires `--repo owner/name`.
- [ ] An unknown task exits non-zero with a clear message and no stack trace.
- [ ] `--run N` prints that run's metadata summary (S-3) plus the final result text.
- [ ] `--run N --raw` writes the run's JSONL to stdout byte-for-byte, suitable for piping into a JSON tool.
- [ ] `-f` follows the newest run for the task, streaming new events as they are written, terminating when the run's terminal event is written or on SIGINT. Following a run that has already finished prints existing content and exits.
- [ ] All `logs` output is **unredacted**. Redaction is outbound-only (S-7).
- [ ] `logs` works with the daemon stopped — it reads only the database and the log files.

---

## Assumptions

Called out rather than buried. Any of these can be vetoed before implementation starts.

| # | Assumption | If wrong |
|---|---|---|
| A-1 | M0 **commits and pushes** the task branch so "a branch you can inspect" is inspectable on GitHub, not just on disk. Report comments name branch and SHA as **plain text** — no blob permalinks, no artifact-derived links. | Drop to commit-only, local branch; report names the worktree path instead. |
| A-2 | A `comments` table is required at M0, beyond the five tables named in the brief, because LF-2 mandates recording third-party comments. `cN` tags are assigned but consumed by nothing until `/include` at M1. | Record third-party comments in `daemon.log` only and defer the table. |
| A-3 | Target platform for M0 is **macOS only** (launchd, keychain). Linux/systemd is a later port; the code should not hard-block it but M0 is not tested there. | Add a portable fallback for the daemon supervisor and credential store. |
| A-4 | GitHub cross-repo polling uses the issue **search** API with repo qualifiers and an updated-since filter to satisfy "one API call covering all tracked repos", accepting its lower rate limit and eventual consistency; per-repo listing is the fallback if search proves unreliable in practice. | Fall back to one issues-list call per repo, still within a 30s tick. |
| A-5 | The `litefactory doctor` command is deferred, but its three hard checks (bot ≠ owner, no literal secret in config, aiteam present per repo) are enforced inline at `init`, daemon start and `repo add`. | Ship `doctor` at M0 as a thin wrapper over the same checks. |
| A-6 | The launchd label is the fixed constant `dev.litefactory.daemon`. | Rename; single constant, no other impact. |
| A-7 | One GitHub identity, one host (`github.com`), one bot credential shared across all registered repos at M0. Per-repo identity overrides (brief §4) are deferred. | Add a per-repo identity override column. |
| A-8 | Since `/spike` bypasses aiteam's pipeline, litefactory does **not** create or read `.aiteam/<branch>/<date>/<feature>/` at M0. The feature slug appears in the prompt only, to keep D-3 exercised and prevent an interactive prompt in headless mode. | State the slug differently or omit it from the M0 prompt. |
| A-9 | Log-tail length in the report comment is 40 lines, matching brief §13, and is a compile-time constant rather than config at M0. | Promote to a config key. |
| A-10 | The forge's own `author_is_bot` signal is trusted as one input to the self-filter, alongside handle match and marker match. Any one of the three is sufficient to drop. | Rely on handle plus marker only. |

---

## Out of Scope

Everything below is deliberately excluded from M0 and must not appear in the implementation plan.

**M1 — the aiteam pipeline (LF-6 … LF-13)**
- Modes and gates: `/research`, `/plan`, `/implement` mapping; gate pre-checks; parking at a gate; `/approve`.
- Artifact discovery: `git status`/`diff` over `.aiteam/`, newest-per-type resolution, state inference from artifacts.
- Permalinks: push-before-comment ordering, SHA-pinned blob URLs, `blob_url` construction.
- Multi-turn sessions: segment resume, free text as next turn, coalescing multiple comments into one turn.
- Live/edited status comments: posted on pickup, edited every ~20s, marker-based recovery of a lost comment id.
- Locking beyond a single-task spike: per-repo parallelism caps, queueing comments during a run, delivery at end of run.
- Loud-failure policy: two-strike `blocked` state, `/retry`, `/reset`, `litefactory status`.
- Permission gate: allowlist-violation comments, 👍 approval, resume on next tick.
- Draft PRs: `open_change`, PR description referencing the plan, `lf:review`.
- The `$LF_TURN_OUT` structured side-channel JSON.
- Labels of any kind, including `lf:brief-ready` / `lf:plan-ready` / `lf:review`.

**M2 — trust (LF-14 … LF-16)**
- `/include` in any form, `<note>` wrapping, injection recorded in run metadata.
- `litefactory doctor` as a command (see A-5).

**M3 — rehydration and second forge (LF-17, LF-18)**
- Rehydration, fresh segments seeded from artifacts, transcript-age and base-drift detection.
- The `Forge` interface extraction, GitLab/Bitbucket, capability degradation. **GitHub only at M0**, called directly.

**Settled as out of scope by decision**
- PR review comments as an input source (D-6).
- Automatic worktree removal / retention sweep (D-5) — command and config key only.

**Not in v1 at all**
- A second harness or any harness registry/YAML; web UI, dashboard, metrics endpoint; multi-user or team ownership; cross-repo tasks; remote workers; natural-language command parsing; auto-merge or auto-deploy.

**Commands not shipped at M0**
- `litefactory repo rm`, `litefactory repo pause`, `litefactory task list`, `litefactory task run`, `litefactory task stop`, `litefactory task reset`, `litefactory tail`, `litefactory runs`, `litefactory status`, `litefactory doctor`.

---

## M0 Definition of Done

- [ ] `litefactory init`, `repo add`, `repo list`, `daemon run|start|stop|status`, `logs`, `task clean` all exist and behave as specified above.
- [ ] `/spike <text>` on a real GitHub issue in a real registered repo produces a real report comment carrying the `lf:` marker.
- [ ] The branch `lf/<issue>-<slug>` exists and is inspectable.
- [ ] The registered clone is provably untouched across the whole loop.
- [ ] A forced non-zero `claude` exit produces a failure comment, not silence.
- [ ] The daemon never reacts to its own comment.
- [ ] `litefactory logs <issue>` shows the run, and `--raw` yields the JSONL.
- [ ] No token appears in `config.yaml` or in any forge comment.
- [ ] The binary builds as a single static, cgo-free artifact.
