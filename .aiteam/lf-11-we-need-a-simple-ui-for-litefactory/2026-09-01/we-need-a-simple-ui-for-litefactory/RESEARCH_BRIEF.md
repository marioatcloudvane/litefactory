# Research Brief — we-need-a-simple-ui-for-litefactory

## Status

**Complete.** This is a re-run of Research mode for this feature. The prior run
in this session could not collect its `codebase-tour`/`web-researcher`
sub-agent output through the normal channel and stopped short, writing an
INCOMPLETE brief rather than fabricate findings (see git history / prior
`RESEARCH_BRIEF.md` revision). This run received the same two sub-agents'
verified findings directly from its invoker as task input (not a mid-run,
same-turn relay from an ambiguous source), each independently re-confirmed
three times when polled — `codebase-tour` (agent `aae617d4739628e2f`) and
`web-researcher` (agent `acb64d152803c628b`). This brief is grounded in that
material `[R0]` throughout (carried forward from an earlier run's verified
output, not re-verified live by this run's own sub-agent calls), with a small
number of items marked `[O]` where they are read directly off the request
text rather than researched.

**Follow-up run (this revision):** the requester asked, in a follow-up
comment, for real clickable HTML mockups in place of the ASCII wireframes
below. No new codebase or web research was needed for this — nothing about
the codebase or external libraries changed — so this revision only adds the
`mockups/` HTML files and the section describing them; the rest of the brief
(codebase context, implementation options, security section, open
questions) is unchanged from the prior revision.

## Request

From github issue #11 (verbatim intent, lightly organized) `[O]`:

> We need a simple UI that is elegant yet slim; no fancy shit stuff; evaluate
> ways how to implement the UI; I think it should only run on the daemon
> interface and only be reachable there.

Deliverables the requester named explicitly:
- 3-4 UI samples of how the UI should look.
- An outline of how the UI would be implemented (frameworks, etc.).
- The UI should use the running litefactory directly — no new REST/API layer
  unless genuinely necessary; keep it very simple.
- Security must be taken seriously — this runs on a personal account/machine.
- Minimum feature set requested:
  - Settings editor for `config.yaml`.
  - Log viewer for litefactory's own logs.
  - A list of "projects", drilling down into runs, drilling down into
    per-run logs.
  - A dashboard / "greeting" landing view.
- Visual direction: simple, modern colors, dark/anthracite or a strong black,
  minimal, no visual noise.

This is a headless run — no human is available to answer clarifying
questions. Ambiguities are resolved below with the most reasonable reading,
noted explicitly, rather than blocking on a question.

## Codebase Context `[R0]`

Grounded in `codebase-tour` agent `aae617d4739628e2f`'s Pass 1 (map) and
Pass 2 (focus on daemon/server, config, logs, store).

### Shape and entrypoint
- Single Go module `github.com/cloudvane/litefactory`, `go 1.26`
  (`go.mod:3`).
- Layout: `cmd/litefactory/` (entrypoint) + `internal/{cli,config,daemon,
  store,logging,secrets,git,github,launchd,repos,report,turn,naming,redact,
  gittest,...}`.
- **No `web/`, `ui/`, `static/`, `templates/`, `assets/`, or `api/` directory
  exists anywhere in the tree today.** This feature is new capability, not
  reuse or extension of an existing surface.
- Entrypoint: `cmd/litefactory/main.go:30` (`main`) → `run()` at line 41 →
  `cli.NewRouter(...).Run(ctx, args)` at line 79. `syscall.Umask(0o077)` is
  the first statement of `main` (`main.go:36`), which is why every file/dir
  under `$LF_HOME` ends up `0600`/`0700` by default — a security-relevant
  ambient convention this feature should preserve, not work around.
- `daemon run` is dispatched from `internal/cli/cmd_daemon.go:62`
  (`runDaemonRun`), which resolves paths/config/db/token, opens
  `daemon.Open` (`cmd_daemon.go:87`), and blocks on `loop.Run(ctx)`
  (line 101) until SIGINT/SIGTERM. Process supervision is via macOS
  `launchd` (`internal/launchd`, `cmd_daemon.go:240-303`), not a socket.

### No existing network surface
- **No HTTP/gRPC server exists anywhere in the repo.** Every
  `net/http`/`ListenAndServe`/`net.Listen`/`127.0.0.1`/`0.0.0.0`/`localhost`
  hit in the codebase is either test-mocking code (`net/http/httptest` used
  only to mock the *outbound* GitHub API client in tests) or the outbound
  GitHub API client itself (`internal/github/client.go:7`). Nothing binds a
  listener.
- No IPC surface beyond the SQLite file and an advisory lock file
  (`internal/daemon/lock.go`, `paths.go:88`).
- `daemon.Startup.APIBase` (`cmd_daemon.go:200-215`) is the *outbound*
  GitHub API base URL (`config.go:236-237,257`) — not a bind address; do not
  confuse this with a UI listener config field.

### Attach point for a UI listener
- The natural attach point is a new listener started alongside
  `loop.Run(ctx)` inside `runDaemonRun` (`cmd_daemon.go:87-101`), sharing the
  already-opened `*store.DB`, the process's `*slog.Logger`, and the root
  `ctx` (`main.go:126`).
- Per the repo's own one-package-one-concern convention (each concern gets
  its own `internal/*` package; `internal/store` explicitly confines all SQL
  to itself, `store.go:2-3`), new UI code belongs in a **new sibling
  package** (e.g. `internal/webui`), wired in from `cmd_daemon.go`, not
  folded into `internal/daemon` itself.
- **Constraint to carry into implementation:** `store.DB`'s connection pool
  is capped at 1 connection (`store.go:132`), by design (`store.go:24-28`).
  A UI's reads will queue behind the daemon's tick-loop writes on that same
  single connection. This is a real, deliberate constraint, not a bug to
  route around — but its effect on UI read latency under concurrent
  dispatch load is unmeasured.

### config.yaml
- Type: `Config` struct, `internal/config/config.go:46-85`. Loader:
  `config.Load(paths)` (`config.go:282-304`); path via
  `paths.ConfigPath()` (`paths.go:67`).
- Fields include `poll_interval_seconds`, `max_concurrent_runs`,
  `default_base_branch`, `feature_prefix`, `worktree_root`,
  `worktree_retain_days`, `log_level`, a nested `github:` block
  (`api_base`, `token_keychain: {service, account}`, `token_env`), and a
  nested `claude:` block (`binary`, `model`, `tools`, `settings_path`,
  `allowed_tools`, `tool_ceiling`, `tool_profiles`, `timeout_seconds`,
  `forward_anthropic_env`).
- The loader does a **strict decode** — `KnownFields(true)`
  (`config.go:6-9,313`) — and a hard-fail secret scan on load
  (`internal/config/secretscan.go`) that rejects credential-shaped literals.
  **`config.yaml` is designed to never contain secrets**: credentials live
  only in macOS Keychain or as the *name* of an env var
  (`internal/secrets/keychain.go`), never as a value in the file itself.
  This materially lowers the blast radius of a settings editor that can
  read/write this file — there is no credential to leak from it by design,
  provided the editor doesn't create a new way to smuggle one in (see
  Security section).
- **No general round-trip writer exists.** The only writer found is
  `WriteDefault` (used by `litefactory init`), which explicitly refuses to
  overwrite an existing file. The shipped `config.yaml` is heavily
  hand-commented (`configHeader` in `template.go:22-120`) — a naive
  `yaml.Marshal` struct round-trip would silently drop those comments and
  potentially reorder keys. A settings editor needs either a
  comment-preserving write path (see Implementation Options) or must accept
  it will not be used, or must be read-only for v1.

### Logs
- **Daemon log**: JSON via `slog.NewJSONHandler`, written to
  `$LF_HOME/logs/daemon.log`, **no rotation**
  (`internal/logging/filesink.go`, `paths.go:77`).
- **Per-run logs**: one JSONL file per `Run` row, at
  `$LF_HOME/logs/runs/<forge>/<owner>/<repo>/<issue>/NNNN.jsonl`
  (`paths.go:79-83`, `store.Run.LogPath`). The existing `litefactory logs`
  CLI command reads/follows these by polling the file every 250ms
  (`cmd_logs.go:29-32`), not via `fsnotify` — a UI log viewer would
  reasonably follow the same poll-based tail approach rather than adding an
  `fsnotify` dependency, if "live tail" is in scope.
- **Logs are explicitly documented in the codebase as deliberately
  unredacted** ("the local copy is always the full truth", referenced as
  S-7 in the tour). This is a stated design posture, not an oversight — but
  it is a posture decision this brief must surface explicitly for a UI that
  would render those logs in a browser (see Security section), because a
  browser changes who/what can plausibly reach that content compared to a
  local file only the CLI reads.

### Data model — "projects" vs. what exists
- There is **no "Project" type** in the store. The actual domain hierarchy
  is: `Repo` (a registered git clone — `internal/store/repos.go:14-44`) →
  `Session` (one task/issue = one branch + worktree + lock —
  `sessions.go:28-59`) → `Run` (one Claude Code invocation —
  `runs.go:59-102`).
- Existing accessors: `ListRepos` (`repos.go:130`, already used by
  `litefactory repo list`), `GetRepo*` (`repos.go:95,106,118`),
  `ListRuns(ctx, sessionID)` (`runs.go:392`), `GetRun*`
  (`runs.go:349,378,361`).
- **No `ListSessionsByRepo` / `ListSessions`-style accessor was found** in
  the portion of `sessions.go` read (only lookup-by-issue and a
  daemon-internal backlog query) — flagged by the tour as needing
  confirmation before a "projects → runs" drilldown view can be built. This
  is carried forward as Open Question 2 below, not resolved by this run.
- **Practical mapping for this brief:** the request's "projects" most
  plausibly maps onto `Repo` (a registered repo = a "project" in the UI),
  with the drilldown going `Repo → Session(s) → Run(s) → per-run log`. This
  is the most reasonable reading `[O]`, not a confirmed decision — Plan mode
  should treat it as the working assumption and confirm it, since "project"
  could alternatively mean something not yet modeled (e.g. a grouping that
  spans repos).

### Auth / security posture today
- **No token, TLS, or socket-permission mechanism exists anywhere in the
  daemon** — there is currently no network-facing surface to protect, so
  there is no existing pattern to imitate for this feature; it starts from
  zero.
- Inherited security posture that *does* apply and should carry over:
  `umask 0o077` (every new file/dir defaults to owner-only permissions),
  in-memory-only GitHub bot token (never written to disk in the clear
  outside Keychain), and the config secret-scan described above.

### Dependencies (go.mod)
- Go 1.26. Dependencies: `go-cmp` (test-only), `go-github/v90`, `yaml.v3`,
  `modernc.org/sqlite` (pure-Go SQLite driver). **No `html/template` usage,
  no `embed` usage, no vendored JS/CSS, no router library (chi/gin/echo/
  gorilla) anywhere in the module.** Everything a UI needs — routing,
  templating, static-asset embedding, YAML — would be either a stdlib
  package not yet used, or a new dependency.

## Domain / Problem Space `[R0][O]`

- The requester wants a local, low-ceremony **operational UI** for a
  personal automation daemon — the same shape of thing as Prometheus's
  expression browser or other single-user local admin UIs: something that
  runs next to the tool it's observing, not a separately deployed service.
- Five views were named or implied by the request: a dashboard/"greeting"
  landing view, a `config.yaml` settings editor, a daemon-log viewer, and a
  projects list with drilldown into runs and per-run logs (four explicit,
  the daemon-log viewer being distinct from the per-run log viewer).
- "No fancy shit" and "only reachable on the daemon interface" are both
  **constraints Plan mode should treat as binding, not preferences to trade
  away for polish**: (1) minimal added dependencies and no JS build step,
  and (2) the UI must not become a new externally reachable attack surface
  on a tool that already handles a live GitHub bot token and unredacted
  operational logs.
- Because there is currently *no* listener in the daemon at all, framing
  this as "extend the existing UI" is wrong — it is **new capability from a
  cold start**, which is exactly why the binding/auth decisions below carry
  real weight: there is no existing precedent in this codebase pulling the
  decision one way or the other.

## Clickable HTML mockups `[O]`

The requester's follow-up on the issue asked for real, clickable HTML
mockups rather than the ASCII wireframes below ("i wanted a real html
output as clickable thing, not an asci art"). Eight static, self-contained
HTML files exist under `mockups/` in this session directory, covering the
same four UI concepts as the wireframes below — the drilldown concept
(sketch 3) is expanded into its three linked states (repo list → sessions
for a repo → run detail with log), matching the three-level hierarchy the
wireframe itself already showed as a single sequence:

- `mockups/dashboard.html` — concept 1, the landing/"greeting" view.
- `mockups/settings.html` — concept 2, the `config.yaml` settings editor
  (structured fields + collapsible raw-YAML fallback).
- `mockups/projects.html` — concept 3, level 1: the repo list.
- `mockups/project-litefactory.html`, `mockups/project-nginx-router.html`,
  `mockups/project-side-project.html` — concept 3, level 2: sessions/issues
  for a given repo (three repos shown, including an empty-state example for
  `side-project`).
- `mockups/run-11.html` — concept 3, level 3: a single run's detail, with
  its log split into per-agent tabs (see below).
- `mockups/logs.html` — concept 4, the daemon's own operational log viewer.

Each file is fully self-contained (inline `<style>`, no external CSS/JS, no
build step) and opens directly via `file://` — double-click or drag into a
browser. Navigation between them is real `<a href="...">` linking to the
sibling file in the same folder — clicking "litefactory" on the dashboard's
activity list or the projects list actually opens `project-litefactory.html`,
and its `#11` row opens `run-11.html` — with no backend behind any of it;
this is a static clickdummy for design review, not a working app, consistent
with Research mode's remit (no Go code, no daemon changes).

**Revision 2 (this run):** the requester left four remarks on the first
HTML-mockup revision (issue comment), all incorporated across all eight
files so the set stays internally consistent:

1. **Palette flipped from dark to light.** The whole set now uses a white
   (`#ffffff`) page background as the base, with graphite (`#2f333a` /
   `#3a3f47`) demoted to an accent — specifically the sidebar's background,
   plus header bars, active-state highlighting, and the settings "Save"
   button. Body text is near-black (`#1b1e23`) on white; panels/tiles use a
   light gray surface (`#f6f7f8`); a muted teal (`#3d8f7f`) remains for
   status dots, links, and the active-tab underline, at the same sparing
   density as the prior dark palette's accent use. This directly reverses
   the earlier dark/anthracite-primary direction — read as a correction of
   the first mockup revision, not an alternative to it.
2. **Top tab-bar navigation replaced with a left sidebar**, present
   identically on all eight pages: a fixed 212px graphite column running the
   full viewport height, holding the brand/status at top, the primary nav
   in the middle, and a footer pinned to the bottom via a flex spacer.
3. **Settings demoted to a gear icon at the sidebar's bottom**, visually
   separated from the primary nav by a border and by sitting in its own
   `sidebar-bottom` block below a flex spacer — no longer a peer tab next to
   "Projects."
4. **Top-level nav trimmed to "Projects" only** — the previous "Runs" and
   "Logs" top-level tabs are gone; drilling into a repo already reaches its
   runs (unchanged three-level `Repo → Session → Run` hierarchy). The
   daemon's own log view (`logs.html`) still exists and is still reachable,
   but per the request it is deliberately **not** a primary-nav item.
   **Working assumption `[O]`, not confirmed by the requester**: it's reachable
   two ways instead — a small secondary link in the sidebar footer (next to,
   but visually lighter than, the Settings gear) and a plain-text link from
   the dashboard ("View the daemon's own operational log") and from Settings
   ("Diagnostics — view the daemon's own operational log"). Plan mode should
   confirm this placement rather than assume it's final.
5. **Per-run log view (`mockups/run-11.html`) now splits into per-agent
   tabs** instead of one interleaved stream, addressing "I want to see how
   it is progressing, not everything in one log but per agent." Three tabs —
   `orchestrator`, `codebase-tour`, `web-researcher` — each with its own
   scrollable log pane and a small status dot (teal = running, green = done
   next to each tab label). Agent names are not invented generically; they
   are litefactory's own aiteam sub-agent roster (the same names this very
   research run's own orchestrator/codebase-tour/web-researcher used),
   chosen because it's the most concrete, grounded example of "subagents and
   the like" available and because a Research-mode run is what issue #11
   itself is. **Tab switching is implemented with a pure-CSS
   radio-input/label/sibling-selector pattern — no JavaScript** — consistent
   with the "no fancy shit" / no-build-step direction; a real implementation
   would presumably drive the tab list from whichever agents actually ran a
   given run, which may be a different roster per run (e.g. Implement mode's
   `go-architect`/`go-implementation-engineer`/`go-unit-test-engineer`
   instead of Research mode's roster) — this mockup shows one concrete
   instance, not a claim that the tab set is fixed.

**These HTML mockups supersede the ASCII wireframes below as the primary
way to review the visual design** — including the wireframes' palette and
navigation description, which the mockups above now deliberately diverge
from following the requester's feedback. The wireframes are left in place,
not deleted, because they still carry useful annotation (the reasoning
behind each layout choice, which store type each view maps to, what a
`[tail]` toggle would need) that the HTML files intentionally don't repeat —
read the HTML files for "what it looks like," read the prose under each
wireframe below for "why it's built that way," and read this section, not
the wireframes' palette line, for the current color/navigation decisions.

## UI Concept Sketches (ASCII wireframes — superseded for visual review, see above)

Four wireframes below, in the requested dark/anthracite palette. All are
plain server-rendered HTML — no client-side app framework, no JS build step
implied by any of them (see Implementation Options for how these bind
together and what "modern" widgets, like a live-tailing log pane, would
actually require).

**Palette** (a concrete anthracite-leaning starting point, not a locked
decision): background `#16181c` (near-black, not pure `#000` — pure black
against white text is harsh at length), panel background `#1f2226`, border
`#2b2f36`, primary text `#e6e6e6`, secondary/muted text `#8b9099`, accent
`#5fb3a3` (a muted teal-green, used sparingly for status/links only), status
colors kept desaturated: success `#5fae6b`, warning `#d1a34c`, error
`#c9614f`. Monospace (`ui-monospace`/`SFMono`/`Menlo`) for logs and config
text; a plain system sans-serif for everything else.

### 1. Dashboard / "greeting"

```
┌────────────────────────────────────────────────────────────────┐
│ litefactory                                    ● daemon running │
│──────────────────────────────────────────────────────────────── │
│  Projects   Runs   Logs   Settings                                │
├────────────────────────────────────────────────────────────────┤
│                                                                    │
│  Good evening.                                                    │
│  3 repos watched · 1 run in progress · 2 queued                  │
│                                                                    │
│  ┌───────────────┐ ┌───────────────┐ ┌───────────────┐          │
│  │ ACTIVE NOW     │ │ TODAY          │ │ POLL          │         │
│  │ litefactory    │ │ 7 runs         │ │ every 30s     │         │
│  │ #11  running…  │ │ 6 ok · 1 fail  │ │ next in 12s   │         │
│  └───────────────┘ └───────────────┘ └───────────────┘          │
│                                                                    │
│  Recent activity                                                  │
│  ─────────────────────────────────────────────                   │
│  ● litefactory #11        running     started 2m ago  →           │
│  ✓ litefactory #9         done        14m ago         →           │
│  ✗ nginx-router #3        failed      1h ago          →           │
│  ✓ litefactory #7         done        3h ago          →           │
│                                                                    │
└────────────────────────────────────────────────────────────────┘
```

Notes: a single glanceable landing page — status dot in the header tied to
whether the daemon poll loop is alive, three small stat tiles (not a full
metrics dashboard — "no fancy shit"), and a flat activity list that is
already effectively `ListRuns` across recently-touched sessions, sorted by
recency. No charts, no auto-refreshing widgets beyond a plain periodic
page/partial reload.

### 2. Settings — config.yaml editor

```
┌────────────────────────────────────────────────────────────────┐
│ litefactory          Projects   Runs   Logs   Settings           │
├────────────────────────────────────────────────────────────────┤
│  Settings                                    [ Save ]  [ Revert ]│
│  ────────────────────────────────────────────────────────────── │
│  ⚠ Read-only fields below are masked or omitted — secrets are    │
│    never stored in config.yaml; see Keychain.                    │
│                                                                    │
│  General                                                          │
│    poll_interval_seconds     [ 30            ]                   │
│    max_concurrent_runs       [ 2             ]                   │
│    default_base_branch       [ main          ]                   │
│    feature_prefix            [ lf/           ]                   │
│    log_level                 [ info      ▾   ]                   │
│                                                                    │
│  GitHub                                                           │
│    api_base                  [ https://api.github.com ]          │
│    token source               keychain: litefactory/gh-bot (n/a) │
│                                                                    │
│  Claude                                                           │
│    binary                    [ claude       ]                    │
│    model                     [ sonnet       ]                    │
│    timeout_seconds           [ 1800         ]                    │
│    tool_ceiling               (advanced — edit raw YAML below)   │
│                                                                    │
│  ▸ Raw config.yaml (advanced, preserves comments)                │
│                                                                    │
└────────────────────────────────────────────────────────────────┘
```

Notes: a structured form for the fields worth exposing casually, plus a
collapsible raw-YAML fallback for the fields that aren't (`tool_profiles`,
etc.) so the editor doesn't need a bespoke widget for every nested map. The
"token source" line is deliberately **display-only** — the UI shows *where*
the token comes from (Keychain service/account name) and never a value,
consistent with the existing secret-scan design. Whether the structured
form and the raw-YAML fallback can coexist safely against the same file
without clobbering comments is exactly the open implementation question in
the section below — this mockup intentionally shows both to make that
trade-off visible rather than picking one silently.

### 3. Projects list → drilldown

```
┌────────────────────────────────────────────────────────────────┐
│ litefactory          Projects   Runs   Logs   Settings           │
├────────────────────────────────────────────────────────────────┤
│  Projects                                                         │
│  ────────────────────────────────────────────────────────────── │
│  litefactory        github.com/cloudvane/litefactory   3 open    │
│  nginx-router        github.com/.../nginx-router        1 open    │
│  side-project         github.com/.../side-project        0 open    │
└────────────────────────────────────────────────────────────────┘

  ↓ click "litefactory"

┌────────────────────────────────────────────────────────────────┐
│ ← Projects / litefactory                                          │
│  ────────────────────────────────────────────────────────────── │
│  #11  simple UI for litefactory      ● running      2m ago    →  │
│  #9   fix worktree cleanup           ✓ done          14m ago  →  │
│  #7   add repo list command          ✓ done          3h ago   →  │
│  #3   nginx router bug                ✗ failed        1d ago   →  │
└────────────────────────────────────────────────────────────────┘

  ↓ click "#11"

┌────────────────────────────────────────────────────────────────┐
│ ← litefactory / #11                                    ● running │
│  ────────────────────────────────────────────────────────────── │
│  branch: lf/11-we-need-a-simple-ui   started: 2m ago              │
│  run 0001                                                         │
│                                                                    │
│  14:02:11  daemon  poll: found issue #11, no open run             │
│  14:02:11  daemon  worktree created at .lf-worktrees/litefactory/…│
│  14:02:12  claude  session start, model=sonnet                    │
│  14:02:14  claude  reading CLAUDE.md                               │
│  14:02:19  claude  tool: Read internal/cli/cmd_daemon.go           │
│  ...                                                                │
│                                                              [tail]│
└────────────────────────────────────────────────────────────────┘
```

Notes: three-level drilldown — `Repo` list → `Session`s (issues/tasks) for
that repo → `Run`s' log for the selected session, matching the actual
`Repo → Session → Run` store hierarchy rather than inventing a fourth
concept. The bottom-right `[tail]` toggle is the one place a small amount of
JS earns its keep (see Implementation Options — htmx polling or SSE for
"follow", off by default).

### 4. Daemon log viewer

```
┌────────────────────────────────────────────────────────────────┐
│ litefactory          Projects   Runs   Logs   Settings           │
├────────────────────────────────────────────────────────────────┤
│  Daemon log                    level: [ all ▾ ]   [ ] follow     │
│  ────────────────────────────────────────────────────────────── │
│  14:05:02 INFO  poll tick: 3 repos checked, 0 new issues          │
│  14:04:32 INFO  run 0001 finished ok  session=litefactory#11      │
│  14:03:58 WARN  github rate limit at 62%                          │
│  14:02:11 INFO  poll tick: 3 repos checked, 1 new issue            │
│  14:00:41 INFO  daemon started  pid=8841 version=0.4.2             │
└────────────────────────────────────────────────────────────────┘
```

Notes: a flat, filterable (by `slog` level) view directly over
`daemon.log`'s JSON lines — same file the daemon already writes, no new log
format. Kept as its own view distinct from per-run logs because it's a
different file with a different lifecycle (unrotated, whole-daemon-lifetime
vs. per-run).

## Implementation Options `[R0]`

Grounded in `web-researcher` agent `acb64d152803c628b`'s findings, applied
against the codebase facts above.

### Server / routing
- **Recommendation: stdlib `net/http` with Go 1.22+ pattern-based
  `ServeMux`** (method + wildcard routing, e.g. `mux.Handle("GET
  /projects/{repo}/{session}", ...)`), not chi/gin/echo/gorilla. For a
  handful of routes (dashboard, settings, projects list, project detail,
  session detail, run log, daemon log) this is now generally considered
  sufficient, and it adds zero new dependencies to a `go.mod` that
  currently has none of these routers. `[Likely, per web-researcher]`
- Templating via stdlib `html/template` (auto-escaping, no dependency),
  assets bundled with `embed.FS` (Go 1.16+, stdlib) so the binary stays
  single-file-deployable, consistent with how the rest of litefactory ships
  (no separate build/deploy pipeline exists today). Neither is used in the
  codebase yet, but both are stdlib — this is the lowest-dependency path
  available and matches "no fancy shit."

### Frontend interactivity — plain templates vs. htmx
- **Recommendation: plain server-rendered `html/template` pages for
  everything, with htmx added narrowly** for the handful of places that
  genuinely need partial updates without a full page reload — the run-log
  "follow" toggle (mockup 3) and the settings-save confirmation, most
  plausibly, and nothing else to start.
- htmx: ~16KB min+gzip, zero dependencies, 0BSD license (public-domain
  equivalent), current stable v2.0.9 as of the research pass, actively
  maintained. It works by letting HTML attributes trigger AJAX/SSE
  requests that swap server-rendered HTML fragments into the DOM — no JS
  build step, no client framework, no separate JSON API surface, which
  directly satisfies the request's "no REST interface unless necessary."
  `[Likely, per web-researcher]`
- This means the settings editor, projects list, and dashboard can all be
  ordinary full-page-load HTML with zero JS, and only the live-tail log
  view needs htmx (or a plain polling `<meta refresh>`/JS `fetch` loop as
  an even lower-dependency fallback, at the cost of a slightly clunkier
  tail experience).

### Styling
- **Recommendation: a classless or near-classless CSS framework**, single
  `<link>` tag, no build step, retargeted toward the anthracite palette
  above via CSS custom properties. Candidates surfaced by research, in
  order of fit:
  - **Pico.css** (~7.7KB gzip) — dedicated classless build, built-in
    light/dark themes, exposes CSS custom properties that make hitting an
    exact anthracite palette straightforward, actively maintained. Best fit
    if precise color control matters, which it does here given the
    explicit palette ask.
  - **Water.css** (~2KB gzip) — auto light/dark via `prefers-color-scheme`,
    smaller, fewer customization hooks.
  - **simple.css** — similar profile to Water.css.
  All three are drop-in, no npm/build tooling, consistent with "no fancy
  shit." `[Medium confidence, per web-researcher]`

### Binding: Unix domain socket vs. loopback TCP
- **Recommendation: Unix domain socket**, not loopback TCP, as the primary
  bind mechanism. A Unix socket (`net.Listen("unix", path)`) restricts
  connection ability to processes that can open that file on disk — an
  OS-filesystem-permission guarantee, not a firewall-dependent one.
  Loopback TCP (`127.0.0.1:PORT`) is reachable by **any local process
  belonging to any user** on a multi-user machine unless separately firewalled;
  it has no inherent per-user access control. On a personal single-user
  machine the practical difference is smaller, but the socket is still the
  stronger default and costs nothing extra to implement, so there's no
  reason to take the weaker option. `[Verified, per web-researcher]`
- **Known caveat, load-bearing for implementation**: Go's
  `net.Listen("unix", path)` does not let you set the socket file's mode at
  creation time (a long-standing stdlib limitation, golang/go#11822) — the
  code must call `os.Chmod(path, 0600)` immediately after `Listen()`
  succeeds, and there is a brief race window before that chmod lands.
  litefactory's existing `umask 0o077` (`main.go:36`) narrows that window
  further (the socket file is created non-world/group-accessible by default
  even before the explicit chmod) but does not eliminate it as a documented
  fact to carry into implementation.
- This socket would live under `$LF_HOME` alongside the existing
  `daemon.lock` file (`internal/daemon/lock.go`), inheriting the same
  `0700`-directory ambient protection.
- Browser access to a Unix socket requires either a thin TCP-to-socket
  proxy or (more simply) the browser talking to a loopback TCP port that
  is itself only reachable because... — this is worth flagging plainly:
  **most browsers cannot speak to a Unix domain socket directly.** The
  practical shape is very likely still loopback TCP for the actual browser
  connection, with the socket-vs-TCP research finding applying most
  cleanly to a *local CLI or secondary process* talking to the daemon, not
  to a browser-facing admin UI. This tension between the security research
  (socket preferred) and the requirement (a *browser* UI) is carried
  forward as an open question below — it was not resolved by either
  sub-agent pass and needs a decision, not an assumption, before Plan mode
  scopes it.

### config.yaml round-tripping for the settings editor
- **`gopkg.in/yaml.v3` (litefactory's current YAML dependency) was archived
  by its own maintainer on 2025-04-01 and is explicitly marked
  UNMAINTAINED in its own README.** This is a fact about litefactory's
  *existing* dependency, not just a research aside — it is relevant to this
  feature regardless of whether Plan mode scopes a settings editor, and
  arguably worth flagging to the maintainer independent of this issue.
  `[Verified, per web-researcher]`
- **`goccy/go-yaml`** (current v1.19.x at research time) is the suggested
  modern replacement: pure Go, and specifically built for reversible
  transformation that preserves comments and anchors via an AST-level
  editing API (rather than struct-based Unmarshal→Marshal), with a higher
  YAML-test-suite pass rate than yaml.v3. `[Verified archival status,
  Medium confidence on feature specifics, per web-researcher]`
- **This directly resolves the "how does a settings editor avoid destroying
  the hand-written comments in config.yaml" question from Codebase
  Context**: a naive struct round-trip (Unmarshal into `Config`, edit
  fields, Marshal back out) will lose comments, potentially reorder keys,
  and drift formatting, because the struct is a lossy intermediate
  representation. A comment-preserving editor needs the AST/node-level
  editing API of a library built for that (`goccy/go-yaml` is the concrete
  candidate surfaced), editing specific nodes' values in place rather than
  round-tripping the whole document through a struct.
- Three real options for the settings editor, in increasing order of
  effort, none picked yet by this brief:
  1. **Read-only display** of the current config (safest, smallest scope,
     satisfies "show me my settings" but not "edit them here").
  2. **Structured-field edits via AST node replacement** (`goccy/go-yaml`,
     editing only the handful of fields exposed in the form in mockup 2,
     leaving comments/unknown fields untouched) — matches the mockup, most
     faithful to "elegant yet slim," but is new integration work.
  3. **Full struct round-trip accepting comment loss** — least work, but
     silently destroys the maintainer's hand-written `config.yaml`
     comments (`template.go:22-120`) on first save, which is a real,
     visible regression a personal-tool maintainer would likely notice and
     dislike.

## Security `[R0]`

- **Bind strategy**: see Binding section above — Unix domain socket is the
  stronger primitive where the client can be anything other than a
  browser; for the browser-facing UI itself the realistic option is
  loopback-TCP-only (`127.0.0.1`, explicitly never `0.0.0.0`), which this
  brief recommends as the fallback specifically *because* browsers cannot
  reach a Unix socket directly — this is a case where the theoretically
  stronger option (socket) doesn't fit the actual client (a browser), and
  Plan mode needs to make that trade-off explicit rather than default to
  "socket is more secure" without checking it's reachable.
- **Auth**: recommend a **shared-secret token, generated at daemon startup
  (or persisted across restarts), written to a file under `$LF_HOME` with
  mode `0600`**, required as a request header (not only a cookie) on every
  UI request. This is a well-established minimal-complexity local-admin-UI
  pattern (Prometheus documents basic-auth for its local expression
  browser/API; Tailscale's local tooling treats local secrets as
  file-delivered rather than inline) and is proportionate — full mTLS was
  considered and judged excessive complexity for a single-user local tool.
  Header-based (not cookie-only) matters specifically because a
  cookie-only token can be silently attached by a foreign page in the
  browser; a header the UI's own JS/htmx has to set explicitly cannot be
  forged that way by a page that isn't the UI itself. `[Medium confidence,
  per web-researcher]`
- **config.yaml exposure risk**: substantially pre-mitigated by
  litefactory's existing design — `config.yaml` is architecturally
  incapable of holding a secret today (the load-time secret-scan hard-fails
  on credential-shaped literals; real credentials live only in Keychain or
  as an env-var *name*). The settings editor therefore is not, by itself, a
  credential-leak vector, **provided** its write path is built to preserve
  that same constraint (i.e., the editor must not add a "paste your token
  here" field that bypasses the secret-scan, and should arguably run the
  same secret-scan on any content it writes back, structured-field or raw).
- **Unredacted-log exposure risk**: `daemon.log` and per-run JSONL logs are
  a deliberate, documented "always the full truth" design choice for the
  CLI. **A browser-facing UI changes the calculus and this brief flags it
  explicitly rather than inheriting the CLI's posture silently**: a file
  only the CLI (running as the local user, in a terminal) can read is a
  different exposure than the same content rendered into a browser tab,
  reachable by anything that can present the auth token, on a machine where
  browser extensions, other tabs, or (per the CSRF note below) even other
  web pages are part of the threat model. Recommend Plan mode make an
  explicit decision here rather than defaulting either way: either (a) the
  loopback+token binding is judged sufficient mitigation and logs render
  unredacted as today, or (b) the UI applies the same kind of redaction
  litefactory's `internal/redact` package presumably already does
  elsewhere (not directly confirmed by this tour pass) before rendering log
  content in a browser. This is Open Question 5 below.
- **Clickjacking / CSRF**: for a loopback- or socket-adjacent HTTP (not
  HTTPS) UI, TLS is not necessary — traffic never leaves the machine, which
  is standard practice for this class of tool (Prometheus and most local
  dev servers run plain HTTP on loopback). But the browser-side attack
  surface still applies regardless of transport encryption:
  - **Clickjacking** is not mitigated by CSRF tokens — mitigate separately
    with `X-Frame-Options: DENY` (or CSP `frame-ancestors 'none'`) on every
    response, so the UI cannot be framed by another page.
  - **CSRF** is a real concern even for a same-origin localhost app,
    because browsers have historically allowed public-internet pages to
    make requests to localhost/local-network services (flagged explicitly
    by MDN's "Local network access" guidance) — a malicious external
    website could attempt to hit the UI's port from a victim's browser.
    Mitigate with `SameSite=Strict` on any cookie, checking `Origin`/
    `Referer` on state-changing requests, and — most robustly, matching the
    auth recommendation above — requiring the shared-secret token as a
    request **header**, which a foreign page cannot attach automatically
    the way it can a cookie.
  - Leave CORS unset (default same-origin) — do not add a permissive CORS
    policy for this UI. `[Medium confidence — synthesis to this specific
    scenario is inference, per web-researcher]`

## Constraints & Risks

- **This is new capability, not reuse.** No listener, router, templating
  dependency, or auth mechanism exists in litefactory today; every
  security decision in this brief starts from zero, with no existing
  in-repo precedent to fall back on or defer to.
- **The single SQLite connection cap** (`store.go:132`) means UI reads
  share one connection with the daemon's tick-loop writes. This is a
  deliberate existing constraint (by design, not a bug), but its effect on
  UI responsiveness under concurrent dispatch load is unmeasured and should
  be treated as a risk to validate early in Plan/Implement, not assumed
  away.
- **litefactory's current YAML dependency (`yaml.v3`) is archived/
  unmaintained** upstream as of 2025-04-01 — independent of this feature's
  settings-editor scope, this is a fact worth the maintainer's attention,
  and directly shapes which library a comment-preserving settings editor
  would use (`goccy/go-yaml`, not `yaml.v3`).
- **Terminology risk**: "projects" in the request most plausibly maps to
  the existing `Repo` type, but this is this brief's working assumption
  `[O]`, not a confirmed mapping — the store has no literal "Project"
  concept, and Plan mode should confirm the mapping (or the alternative,
  that "project" means some new grouping) before scoping the drilldown
  feature.
- **Socket-vs-browser tension**: the strongest-researched binding option
  (Unix domain socket) is not directly reachable by a browser, which is
  the actual client here. Loopback TCP is the practical fallback, but this
  is a real trade-off this brief is surfacing, not resolving — see Open
  Questions.
- **Log redaction posture is currently silent for a browser context.** The
  CLI's "always unredacted" design was a considered choice for a terminal
  reader; whether it should hold for a browser-rendered view is an
  explicit decision this brief flags rather than inherits.

## Open Questions

1. **Config writer/round-trip.** No config round-trip writer exists today
   (only `WriteDefault`, which refuses to overwrite). Does the settings
   editor use `goccy/go-yaml`'s AST-level node editing to preserve
   `config.yaml`'s hand-written comments, accept comment loss via a plain
   struct round-trip, or ship read-only for v1? This gates the scope and
   effort of the settings-editor story directly.
2. **`ListSessionsByRepo` accessor.** `internal/store/sessions.go` (read
   only through line 80 by the tour) was not confirmed to have an accessor
   listing sessions for a given repo — needed for the "projects → runs"
   drilldown (mockup 3). If it doesn't exist, it needs to be added as part
   of this feature's store work.
3. **"Project" terminology mapping.** Does "project" in the request map
   onto the existing `Repo` type (this brief's working assumption), or does
   it need a new concept not currently modeled (e.g. a grouping spanning
   multiple repos)? Confirm before Plan mode scopes the projects list view.
4. **SQLite single-connection cap under UI load.** The store's connection
   pool is capped at 1; UI read latency under concurrent daemon dispatch
   activity is unmeasured. Should be validated early (a quick load check)
   rather than discovered in production.
5. **Log redaction posture for the browser.** Should `daemon.log` and
   per-run JSONL content be redacted before rendering in the UI, or is
   loopback/socket binding plus token auth judged sufficient mitigation to
   keep the CLI's existing unredacted posture? This is a security decision,
   not an implementation detail, and needs an explicit owner call.
6. **Where the UI listener is wired in.** Should the new listener/package
   (`internal/webui` or similar) be started from `runDaemonRun`
   (`cmd_daemon.go:87-101`) directly, and does it get its own lifecycle
   (start/stop with the daemon) or a separate CLI subcommand
   (`litefactory ui`)? The mockups and security section assume "runs
   embedded in the daemon process," per the request's "should only run on
   the daemon interface," but the exact wiring point is Plan/Implement's
   call.
7. **Socket vs. loopback TCP for the actual browser connection.** Research
   recommends a Unix domain socket as the stronger primitive, but browsers
   generally cannot address a Unix socket directly. Is the intended
   resolution loopback-TCP-only for the browser leg (this brief's working
   recommendation), or is there an appetite for a local proxy/helper that
   fronts the socket, which would add complexity the request explicitly
   wants avoided ("no fancy shit")? Needs an explicit decision.
8. **`internal/redact` applicability.** The tour noted `internal/redact`
   exists in the module but did not confirm what it redacts or whether it
   already applies to log content — worth a targeted look before Plan mode
   decides Open Question 5.

## Recommended scope for Plan mode

Plan mode should cover:
- The five views in the UI Concept Sketches section (dashboard, settings,
  projects list, project/session/run drilldown, daemon log viewer), broken
  into stories, using the store's actual `Repo → Session → Run` hierarchy
  (Open Question 3 notwithstanding — confirm the mapping first).
- The server package (`internal/webui` or similar), its wiring into
  `runDaemonRun`, routing via stdlib `ServeMux`, and template/asset
  embedding via `embed.FS` — per Implementation Options.
- The binding and auth decision (Open Questions 5 and 7) as an explicit,
  early story — this gates almost everything else and should not be left
  implicit.
- The settings-editor's read/write strategy (Open Question 1) as its own
  scoped decision, given the three real options laid out above have
  meaningfully different effort and risk.
- Any store-layer additions needed (Open Question 2, e.g.
  `ListSessionsByRepo`) as explicit tasks, not assumed to already exist.

Plan mode should **not** need to re-research: the absence of an existing
HTTP surface, the config/secret-scan design, the log storage locations and
format, the store hierarchy, or the shortlisted libraries (stdlib
`net/http`+`html/template`+`embed.FS`, optional htmx, Pico.css/Water.css/
simple.css, `goccy/go-yaml`) — those are grounded above. It should resolve
the eight open questions above, most of which are product/architecture
decisions rather than further research.

This is Research mode only — no plan, no code, no per-story breakdown was
produced here; that is Plan mode's job once a human opens it.
