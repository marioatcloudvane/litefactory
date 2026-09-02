# Feature Spec: litefactory web UI
# Status: APPROVED
# Date: 2026-09-02

Approved inputs: `RESEARCH_BRIEF.md` (4 research passes) and `mockups/*.html`
(8 files, human-approved: "the UI looks good, plan it out!"). Visual design —
palette, sidebar nav, Projects-only primary nav, gear Settings, secondary
daemon-log link, per-agent run tabs — is settled and is **not** re-opened here.

Headless run: no human available. Every gap is resolved with the most
reasonable reading and recorded in [Assumptions](#assumptions).

## Goal

Give the single local operator a read-first, loopback-only web UI served by the
running daemon, so daemon status, repos, sessions, runs, per-agent run logs, the
daemon log and `config.yaml` are reachable in a browser without a CLI or a new
API layer.

## User Stories

| ID | Story |
|----|-------|
| US-1 | As the operator, I want a landing page showing daemon liveness, today's run tallies and recent activity, so I can see at a glance whether litefactory is working |
| US-2 | As the operator, I want a list of registered repos ("Projects") with per-repo session counts, so I can pick the one I care about |
| US-3 | As the operator, I want a repo's sessions (issues) listed with state and recency, so I can drill into one |
| US-4 | As the operator, I want a run's detail with its log split into per-agent tabs, so I can see how each sub-agent is progressing instead of one interleaved stream |
| US-5 | As the operator, I want a periodically self-refreshing "follow" mode on run and daemon logs, so I can watch a live run without reloading by hand |
| US-6 | As the operator, I want the daemon's own operational log, filterable by level, so I can diagnose poll/dispatch behaviour |
| US-7 | As the operator, I want to view and edit the everyday `config.yaml` fields in a form, with my hand-written comments preserved, so I do not have to hand-edit YAML for routine changes |
| US-8 | As the operator, I want the UI bound to loopback and gated by a token file, so no other machine and no unauthenticated local process can read my unredacted logs |
| US-9 | As the operator, I want a `litefactory ui` command that prints the URL including the token, so I can open the UI without hunting for the secret |

## Data Model

No schema change. No migration file. All views read existing tables plus
`$LF_HOME` files. Two new config keys and one new path are the only additions.

### New config keys (`internal/config`)

| Entity | Field | Type | Required | Constraints |
|--------|-------|------|----------|-------------|
| `Config.UI` (`ui:`) | `enabled` | bool | no | default `true` |
| `Config.UI` (`ui:`) | `addr` | string | no | default `127.0.0.1:8787`; `host:port`; host MUST be `127.0.0.1`, `::1` or `localhost`; port 1–65535; `Validate` rejects any other host, including `0.0.0.0` and empty host |

Both keys MUST be documented in `configHeader` (`internal/config/template.go`)
and MUST match `Defaults()` (enforced by existing `template_test.go`).

### New path (`config.Paths`)

| Entity | Field | Type | Required | Constraints |
|--------|-------|------|----------|-------------|
| `Paths` | `UITokenPath()` | string | yes | `$LF_HOME/ui.token` |
| `ui.token` file | contents | string | yes | 32 random bytes, base64url, no padding; file mode `0600`; created on first daemon start with `ui.enabled`; reused thereafter; never logged in full |

### New store accessors (`internal/store`, read-only SELECTs)

| Accessor | Signature | Used by |
|----------|-----------|---------|
| `ListSessionsByRepo` | `(ctx, repoID int64) ([]Session, error)` — newest `updated_at` first | US-3 |
| `ListRepoSessionCounts` | `(ctx) ([]RepoSessionCounts, error)` — one row per repo, grouped in one query | US-1, US-2 |
| `ListRecentRuns` | `(ctx, limit int) ([]RunSummary, error)` — newest `started_at` first, joined to session + repo | US-1 |
| `CountRunsSince` | `(ctx, since time.Time) (RunTotals, error)` | US-1 |

| New store type | Field | Type | Notes |
|----------------|-------|------|-------|
| `RepoSessionCounts` | `RepoID` | int64 | |
| `RepoSessionCounts` | `Total`, `Idle`, `Running`, `Failed` | int | one counter per `SessionState` |
| `RunSummary` | `Run` | `Run` | embedded |
| `RunSummary` | `TaskRef` | string | from `sessions` |
| `RunSummary` | `IssueNumber` | int | from `sessions` |
| `RunSummary` | `RepoID` | int64 | from `sessions` |
| `RunSummary` | `RepoName` | string | from `repos.name` |
| `RunTotals` | `Running`, `Succeeded`, `Failed`, `Reported` | int | |

### Existing entities read (unchanged)

| Entity | Read via | Fields used |
|--------|----------|-------------|
| `Repo` | `ListRepos`, `GetRepo` | `ID Forge Owner Name RemoteURL BaseBranch LastFetchAt` |
| `Session` | new accessors, `GetSession` | `ID RepoID IssueNumber TaskRef Branch State WorktreePath UpdatedAt` |
| `Run` | `ListRuns`, `GetRun`, new accessors | `ID SessionID Ordinal Mode Status ExitCode Error StartedAt FinishedAt DurationMS CostUSD LogPath CommitSHA` |
| `PollCursor` | `GetPollCursor` | `LastTickAt` (drives the liveness dot) |
| backlog | `ListBacklogSessions` | count only ("N queued") |
| run meta | `$LF_HOME/logs/runs/.../NNNN.meta.json` | `Model ToolProfile ClaudeSessionID Branch SHABefore SHAAfter FilesTouched` |
| run events | `$LF_HOME/logs/runs/.../NNNN.jsonl` | `type subtype parent_tool_use_id message.content[]` |
| daemon log | `$LF_HOME/logs/daemon.log` | `time level msg` + remaining attrs |

### Relationships

- `Repo` → `Session` via `sessions.repo_id`
- `Session` → `Run` via `runs.session_id`
- `Run` → run log file via `runs.log_path`, else `naming.RunLogPath(home, ref, ordinal)`
- `Run` → run meta file via `naming.RunMetaPath(home, ref, ordinal)`
- run event → agent tab via `parent_tool_use_id`; tab label via the `Task` `tool_use` block whose `id` equals that `parent_tool_use_id`, read from `input.subagent_type`
- `Repo`/`Session`/`Run` → UI identity via numeric DB id in the URL

## API / Interface Changes

All routes are HTML (`text/html; charset=utf-8`). No JSON, no REST layer.
All are `GET` except one `POST`. All sit behind the auth middleware.

| Method | Endpoint / Action | Auth | Notes |
|--------|------------------|------|-------|
| GET | `/` | cookie or `?token=` | Dashboard (US-1). Greeting by local hour, 3 tiles, daemon-log link, recent activity (20 rows) |
| GET | `/projects` | cookie | Repo list (US-2). Row: name, remote URL, `N sessions · M running` |
| GET | `/projects/{repoID}` | cookie | Sessions for one repo (US-3), newest first. Row: `#issue title-less ref`, state badge, relative time |
| GET | `/sessions/{sessionID}` | cookie | 303 → `/runs/{newest run id}`; renders an empty-state page when the session has no runs |
| GET | `/runs/{runID}` | cookie | Run detail (US-4). Breadcrumb, run meta strip, per-agent tabs, `follow` link |
| GET | `/runs/{runID}?follow=1` | cookie | Same page + `<meta http-equiv="refresh" content="5">` (US-5) |
| GET | `/runs/{runID}?agent={tabKey}` | cookie | Preselects a tab; all panels are still server-rendered |
| GET | `/logs` | cookie | Daemon log (US-6), newest first, last 500 entries |
| GET | `/logs?level={all\|debug\|info\|warn\|error}&follow={0\|1}` | cookie | Level filter is `>=` the chosen level; `all` = no filter |
| GET | `/settings` | cookie | Settings form (US-7) + read-only raw YAML panel |
| POST | `/settings` | cookie + CSRF + Origin | Applies allowlisted field edits; 303 → `/settings?saved=1` on success, re-renders form with inline errors on failure |
| GET | `/static/app.css` | cookie | The mockups' stylesheet, extracted and `embed.FS`-served; `Cache-Control: no-store` |
| GET | any path with `?token={token}` | token file | Sets cookie, 303 → same path without the query |

### Auth surface

| Element | Value |
|---------|-------|
| Cookie name | `lf_ui` |
| Cookie value | the `ui.token` contents |
| Cookie attributes | `HttpOnly`, `SameSite=Strict`, `Path=/`, no `Secure` (plain HTTP loopback), session cookie (no `Max-Age`) |
| Comparison | `crypto/subtle.ConstantTimeCompare` for token and CSRF |
| CSRF field | hidden `csrf` input on the settings form; value = `HMAC-SHA256(token, "lf-ui-csrf")` hex |
| Failure | `401` + plain page: "Run `litefactory ui` to get the URL with your token." No token echoed |

### CLI change

| Command | Behaviour |
|---------|-----------|
| `litefactory ui` | Prints `http://<ui.addr>/?token=<token>` and nothing else. Non-zero with a precondition error when `ui.enabled: false`, when `$LF_HOME` is not initialised, or when `ui.token` does not exist yet (message: start the daemon once) |

## Business Rules

### Binding and lifecycle
- The UI listener lives in a new package `internal/webui` and is started from `runDaemonRun` (`internal/cli/cmd_daemon.go`) alongside `loop.Run(ctx)`, sharing the already-open `*store.DB`, the process `*slog.Logger`, `config.Paths` and `*config.Config`.
- The listener binds `ui.addr` only. Any non-loopback host is refused at config validation, before the listener is created.
- The listener's context is the daemon's root context; cancellation triggers `http.Server.Shutdown` with a 5s grace, and daemon shutdown does not block on in-flight UI requests beyond that grace.
- A failure to bind (port in use) is a fatal daemon startup error, not a silent degradation.
- `ui.enabled: false` means no listener is created and no token file is written.
- The UI process is the daemon process; there is no second binary, no separate service, no `litefactory ui serve`.

### Dependencies and assets
- Zero JavaScript. Zero third-party CSS/JS. No htmx, no Pico.css, no build step.
- Templates: stdlib `html/template`. Routing: stdlib `net/http` `ServeMux` method+wildcard patterns. Assets: stdlib `embed.FS`.
- The only permitted new third-party dependency in this feature is a comment-preserving YAML editor for US-7 (`goccy/go-yaml`), and only if the comment-preservation acceptance criterion passes.
- "Follow" is `<meta http-equiv="refresh" content="5">`, never a websocket, SSE or JS poll.
- Per-agent tabs use the mockup's pure-CSS radio/label/sibling pattern; every panel is rendered server-side in the same response.

### Reads and the single SQLite connection
- Every DB read is a short, whole-result read; no connection, transaction or cursor is held across template rendering or across a log file read.
- Log files are read from disk directly and never through the DB.
- No page issues more than 4 DB queries.
- Self-refreshing pages refresh no faster than every 5s.

### Data derivation
- Daemon liveness dot: green when `now - PollCursor.LastTickAt <= 2 × poll_interval_seconds`; amber otherwise, with the last tick time as the dot's `title`.
- "N repos watched" = `len(ListRepos)`. "M run in progress" = sessions in state `running`. "K queued" = `len(ListBacklogSessions)`.
- "Today" tile = `CountRunsSince(local midnight)`, rendered as `X runs · Y ok · Z fail` where ok = succeeded+reported and fail = failed.
- "Poll" tile = `every {poll_interval_seconds}s` and `next in {LastTickAt + interval - now}s`, clamped at 0 and shown as `due` when negative.
- Times render as relative ("2m ago") with the absolute RFC 3339 value in the element's `title`, in the daemon's local timezone.
- Run status badge: `running` teal dot, `succeeded`/`reported` green check, `failed` red cross.

### Run log rendering
- At most the last 4 MiB of a run's `NNNN.jsonl` is read per request; a leading partial line is discarded.
- Events are grouped into tabs by `parent_tool_use_id`: the empty value is the first tab, labelled `orchestrator`; each distinct non-empty value is a tab labelled with its `Task` block's `input.subagent_type`, falling back to `agent <first 4 chars of the id>` when the naming block is outside the read window.
- Tab order: `orchestrator` first, then sub-agents in first-appearance order. Tab dot: teal when the agent has no closing `tool_result` for its `Task` block, green when it does.
- Each tab renders at most its last 500 events; when truncated the panel shows a first line: `showing the last 500 events — run `litefactory logs <issue> --run <N>` for the full log`.
- Event rendering follows the CLI's kinds (`system text think tool output result user other`); `think` is hidden by default in the UI as it is in the CLI.
- Run log content is rendered **unredacted** (see Security below).

### Daemon log rendering
- Only the last 256 KiB of `daemon.log` is read per request; a leading partial line is discarded.
- Lines are parsed as slog JSON; a line that does not parse is rendered verbatim as its own row rather than dropped.
- Newest first, at most 500 rows after filtering.

### Settings editor
- Editable allowlist, and nothing else: `poll_interval_seconds`, `max_concurrent_runs`, `default_base_branch`, `feature_prefix`, `worktree_retain_days`, `log_level`, `github.api_base`, `claude.binary`, `claude.model`, `claude.timeout_seconds`.
- `log_level` is a `<select>` over `debug info warn error`; the rest are text inputs; numeric fields are `<input type="number">`.
- Display-only, never editable, never rendered as a value: `github.token_keychain` (shown as `keychain: {service}/{account} (not shown)`), `github.token_env` (shown as the variable *name*), `claude.tool_ceiling`, `claude.tool_profiles`, `claude.allowed_tools`, `claude.tools`, `claude.settings_path`, `claude.forward_anthropic_env`, `worktree_root`, `ui.*`.
- The UI never offers a field whose value would be a credential. There is no "paste your token here" input anywhere.
- Save writes **only** the allowlisted scalar values into the existing document, leaving comments, key order, formatting and every non-allowlisted key byte-identical.
- Before the file is replaced, the candidate document MUST pass the same strict decode, secret scan and validation that `config.Load` applies at daemon startup. A candidate that fails is never written.
- The write is atomic: temp file inside `$LF_HOME` (same filesystem), mode `0600`, `fsync`, then `os.Rename` over `config.yaml`.
- The raw-YAML panel is **read-only in v1** — a `<pre>` inside the mockup's `<details>`, not a `<textarea>`.
- A successful save renders a persistent banner: `Saved. Restart the daemon to apply.` The running daemon does not hot-reload config.
- Concurrent-edit guard: the form carries the config file's `mtime`+size; a save whose recorded stamp no longer matches re-renders the form with `config.yaml changed on disk since this page was loaded — reload and re-apply.` and writes nothing.

### Security
- Loopback TCP only. Unix socket is explicitly rejected for this feature: the client is a browser and browsers cannot address one; a proxy/helper to bridge it is the "fancy shit" the request forbids.
- No TLS. Traffic never leaves the loopback interface.
- Every response carries: `X-Frame-Options: DENY`, `Content-Security-Policy: default-src 'none'; style-src 'self'; img-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'`, `Referrer-Policy: no-referrer`, `X-Content-Type-Options: nosniff`, `Cache-Control: no-store`.
- No CORS headers are set, ever.
- `POST /settings` requires an `Origin` header exactly equal to `http://{ui.addr}` **and** a valid CSRF field. A missing or mismatched `Origin` is a `403`.
- Logs render unredacted, consistent with S-7 ("the local copy is always the full truth"). `internal/webui` MUST NOT import `internal/redact`; the CLI already forbids that import for the same reason and the UI must not create a second, divergent version of the truth. The compensating controls are the loopback bind, the `0600` token file, the `SameSite=Strict` cookie, the CSP and the absence of CORS.
- The token is never rendered into a page body, never written to the daemon log in full (log the fact and the path only), and never placed in a `Referer`-leakable link once the cookie is set (the bootstrap redirect strips it).
- `umask 0o077` is inherited; the token file and the temp config file are created `0600` explicitly regardless.

### Package boundaries
- `internal/webui` may import `config`, `store`, `naming` and stdlib. It MUST NOT import `internal/cli` (`cli` wires it, so the arrow cannot point back), `internal/daemon`, or `internal/redact`.
- The run-event decoding the UI needs already exists unexported in `internal/cli/cmd_logs_render.go`. Whether it is extracted to a shared package or reimplemented in `webui` is the architect's call; either way the two MUST agree on kinds and on `parent_tool_use_id` handling.

## Edge Cases & Error Handling

| Case | Expected behaviour |
|------|--------------------|
| No repos registered | Projects list renders empty state: "No repos registered. Run `litefactory repo add`." |
| Repo has no sessions | Repo page renders the `side-project` mockup's empty state |
| Session has no runs | `/sessions/{id}` renders "No runs yet" in-layout; no redirect, no 404 |
| Run allocated but log file not written yet | Tab area shows `no event log at <path>`; the run meta strip still renders |
| Run log file missing entirely | Same message; page is `200`, not `404` |
| `NNNN.meta.json` missing or malformed | Meta strip falls back to the DB row's fields; missing fields are omitted, never rendered as `<nil>` |
| Run log contains no `parent_tool_use_id` at all | Single `orchestrator` tab; the tab bar still renders |
| Run log line is not JSON | Rendered verbatim as an `other` row in the current tab |
| Unknown `{repoID}`/`{sessionID}`/`{runID}`, or a non-numeric one | `404` rendered inside the normal layout with a link back to `/projects` |
| Unknown route | `404`, same layout |
| `daemon.log` missing | Logs page renders "The daemon has not written a log yet." |
| `daemon.log` larger than 256 KiB | Only the tail is read; a header note says `showing the tail of daemon.log` |
| Level filter matches nothing | "No entries at this level." with the filter still selected |
| `config.yaml` missing | Settings page renders "litefactory is not initialised. Run `litefactory init`." and no form |
| Candidate config fails decode/validate/secret-scan | `200` with the form re-rendered, the user's submitted values retained, and the loader's own error message shown inline. File untouched |
| Config file changed on disk since page load | Save refused with the mtime/size mismatch message; file untouched |
| Comment preservation cannot be achieved by the chosen writer | The story degrades to a read-only settings page plus "edit `~/.litefactory/config.yaml` by hand". Falling back to a struct round-trip that destroys comments is forbidden |
| Port already in use at startup | Daemon fails to start with `ui.addr 127.0.0.1:8787 is already in use` |
| `ui.addr` host is non-loopback | `config.Validate` fails at load; the daemon never starts |
| `ui.token` exists with mode other than `0600` | Chmod back to `0600` at startup and log a warning |
| Request without cookie and without `?token=` | `401` + instruction page. No redirect loop, no auto-issued cookie |
| Request with a wrong `?token=` | `401`, identical response to the no-token case, constant-time compare |
| `POST /settings` without cookie | `401`; without valid CSRF or Origin: `403` |
| DB read blocked behind a daemon write | Request waits on the store's single connection as any other caller does; no separate pool, no timeout shortcut |
| Browser requests a stale `follow=1` page after the run finished | Page still renders; the `follow` control renders as `follow: off` once the run is not `running`, so refreshing stops on the next load |

## Out of Scope

- Any mutating action other than saving allowlisted `config.yaml` fields: no starting/cancelling runs, no repo add/remove, no daemon start/stop/restart, no worktree cleanup, no comment posting.
- Editable raw-YAML textarea (mockup shows one; v1 renders it read-only).
- JSON/REST/gRPC API, websockets, SSE, and any client-side JavaScript.
- htmx, Pico.css, Water.css, simple.css, or any other third-party front-end asset.
- Unix domain socket binding and any TCP↔socket proxy or helper process.
- TLS, multi-user auth, user accounts, roles, login forms, token rotation UI.
- Remote or LAN access, reverse-proxy support, `0.0.0.0` binding.
- Log redaction in the UI, log rotation, log search/grep, log download/export.
- Dark-mode toggle, theme switching, charts, metrics, graphs, keyboard shortcuts, mobile-specific layout.
- Replacing `gopkg.in/yaml.v3` elsewhere in the project (a real finding, but its own issue).
- Raising the store's single-connection cap, adding a second DB handle, or any schema migration.
- Pagination and full-text search across runs (fixed row caps only).
- `litefactory ui --open` / launching a browser.

## Acceptance Criteria

- [ ] With `ui.enabled: true`, starting the daemon binds `127.0.0.1:8787` and creates `$LF_HOME/ui.token` with mode `0600`; `lsof`/`netstat` shows no non-loopback binding.
- [ ] `config.Validate` rejects `ui.addr` values `0.0.0.0:8787`, `:8787`, `192.168.1.5:8787` and `127.0.0.1:0`, naming the field.
- [ ] With `ui.enabled: false`, no listener is created and no token file is written.
- [ ] `litefactory ui` prints exactly one line, `http://127.0.0.1:8787/?token=<token>`, matching the token file.
- [ ] A request to any route with no cookie and no token returns `401` and does not set a cookie.
- [ ] A request to `/projects?token=<valid>` returns `303` to `/projects` and sets `lf_ui` with `HttpOnly`, `SameSite=Strict`, `Path=/`; the follow-up request with the cookie returns `200`.
- [ ] A request with a wrong token returns `401` with a body byte-identical to the no-token case.
- [ ] Every response — including `401`, `403`, `404` and `/static/app.css` — carries `X-Frame-Options: DENY`, the specified CSP, `Referrer-Policy: no-referrer`, `X-Content-Type-Options: nosniff` and `Cache-Control: no-store`, and no `Access-Control-Allow-*` header.
- [ ] `GET /` renders the greeting, the 3 tiles, the daemon-log link and up to 20 activity rows; each row links to `/runs/{id}`.
- [ ] With `PollCursor.LastTickAt` older than 2× `poll_interval_seconds`, the sidebar dot renders amber; within it, green.
- [ ] `GET /projects` lists every `ListRepos` row with its remote URL and `N sessions · M running` from one grouped query.
- [ ] `GET /projects/{repoID}` lists that repo's sessions newest-first with state badge and relative time; a repo with none renders the empty state.
- [ ] `GET /sessions/{id}` with runs returns `303` to the newest run; with no runs returns `200` and the "No runs yet" page.
- [ ] `GET /runs/{id}` for a fixture log containing two `parent_tool_use_id` values renders three tabs — `orchestrator` plus both `subagent_type` names — with all three panels present in the HTML and only one visible with JS disabled.
- [ ] A run log whose `Task` naming block falls outside the read window renders that tab as `agent <4 hex>` rather than failing.
- [ ] A run log with more than 500 events per agent renders the last 500 and the truncation notice naming the `litefactory logs` command.
- [ ] Rendering with JavaScript fully disabled: navigation, tab switching, level filtering and settings save all work; the served HTML contains no `<script>` tag and no `on*=` attribute.
- [ ] `?follow=1` adds `<meta http-equiv="refresh" content="5">`; without it, no refresh meta is present.
- [ ] `GET /logs?level=warn` shows only `warn` and `error` rows; a malformed line renders verbatim rather than being dropped.
- [ ] `GET /logs` against a 10 MiB `daemon.log` reads at most 256 KiB and renders the tail note.
- [ ] `GET /settings` renders the allowlisted fields with current values, renders `github.token_keychain` as `keychain: <service>/<account> (not shown)`, and the response body contains no credential value.
- [ ] `POST /settings` changing `poll_interval_seconds` from 30 to 45 leaves `config.yaml` byte-identical apart from that one scalar — every comment line, key order and untouched key preserved (asserted against the shipped `DefaultFileContents()` document).
- [ ] `POST /settings` with `log_level: bogus` writes nothing and re-renders the form with the loader's error and the submitted values retained.
- [ ] `POST /settings` with a credential-shaped literal in an allowlisted field is rejected by the secret scan and writes nothing.
- [ ] `POST /settings` with no `Origin`, a foreign `Origin`, or a bad CSRF value returns `403` and writes nothing.
- [ ] `POST /settings` after the file's mtime/size changed on disk returns the mismatch message and writes nothing.
- [ ] A successful save leaves `config.yaml` mode `0600`, leaves no temp file behind, and renders the "Restart the daemon to apply." banner.
- [ ] `go test ./...` includes a test asserting `internal/webui` imports neither `internal/redact` nor `internal/cli` nor `internal/daemon`.
- [ ] A run log containing a token-shaped literal renders it verbatim in the UI (unredacted posture is deliberate and tested, mirroring the CLI's `AC-5.7`).
- [ ] Cancelling the daemon's context shuts the listener down within 5s and the process exits; `daemon run` with the UI enabled still exits 0 on SIGTERM.
- [ ] Under `max_concurrent_runs` concurrently dispatching turns, `GET /` completes in under 2s and no page issues more than 4 DB queries.
- [ ] `make check` passes: `gofmt`, `go vet`, `golangci-lint v2`, `go test -race ./...`.

## Assumptions

Recorded because this is a headless run; each resolves an open question or a
gap rather than blocking on it.

| # | Assumption | Basis |
|---|------------|-------|
| A-1 | "Project" in the request means the existing `store.Repo`; no new grouping entity is introduced. UI label stays "Projects", data is `Repo` | Brief OQ3; the store has no Project type and the mockups show repos |
| A-2 | The mockups' "3 open" per-repo count is rendered as `N sessions · M running`. `SessionState` is only `idle\|running\|failed` — there is no "open"/"closed" state to count truthfully | Verified `internal/store/sessions.go:12-24` |
| A-3 | `ListSessionsByRepo` does not exist and is added by this feature, along with three further read accessors | Brief OQ2, verified against all of `internal/store` |
| A-4 | The browser leg is loopback TCP only; the Unix-socket recommendation does not survive contact with the actual client | Brief OQ7 |
| A-5 | Auth is a `SameSite=Strict` `HttpOnly` cookie bootstrapped from a `?token=` URL, not a request header. A header-only scheme is unimplementable for JS-free top-level navigation; `SameSite=Strict` + `Origin` check + CSRF field is the equivalent guarantee | Brief's header-auth recommendation reconciled with the no-JS constraint |
| A-6 | Logs render unredacted; the binding, token, cookie and CSP are the mitigation | Brief OQ5, decided rather than deferred |
| A-7 | `internal/redact` is outbound-only (`Redactor.Apply` on comment bodies) and is not applicable here; the CLI already forbids the import for logs | Brief OQ8, verified in `internal/redact/redact.go` and `internal/cli/cmd_logs.go:149-153` |
| A-8 | The listener runs in-process in the daemon, started from `runDaemonRun`, in a new `internal/webui` package; there is no separate serve command | Brief OQ6 and the request's "only run on the daemon interface" |
| A-9 | The settings editor edits an allowlist of scalars with comments preserved; the raw-YAML panel is read-only in v1; a comment-destroying round-trip is forbidden even as a fallback | Brief OQ1, choosing option 2 with option 1 as the only permitted degradation |
| A-10 | The single-connection cap stands; the UI adapts to it (few short queries, no held connections) rather than the store adapting to the UI. It is validated by an explicit criterion instead of assumed away | Brief OQ4 and `APP_CONTEXT` ("the cap stays at one") |
| A-11 | The daemon-log view stays out of the primary nav, reachable from the sidebar footer, the dashboard link and the Settings diagnostics line, exactly as the mockups show | Mockup revision-2 item 4, which asked Plan to confirm the placement |
| A-12 | `ui.enabled` defaults to `true`. The feature is the point of the issue, and the surface it opens is loopback-only and token-gated | Most reasonable reading of the request |
| A-13 | Default port is `8787`, configurable via `ui.addr` | No port was specified anywhere |
| A-14 | Per-agent tab labels come from the `Task` block's `input.subagent_type`; the roster is per-run, not fixed | Mockup revision-2 item 5, verified feasible via `parent_tool_use_id` in `internal/cli/cmd_logs_render.go:242` |
| A-15 | Config changes take effect on the next daemon start; no hot reload is added | Config is loaded once at startup today |
| A-16 | Row/entry caps (20 activity, 500 log entries, 500 events per tab, 256 KiB / 4 MiB read windows) are fixed, not user-configurable | "Elegant yet slim"; the CLI remains the full-truth path |
