# Plan Brief — litefactory web UI
# Branch: lf/11-we-need-a-simple-ui-for-litefactory
# Date: 2026-09-02
# Parent issue: github:marioatcloudvane/litefactory#11
# Citation base: 4271416
# Research brief: RESEARCH_BRIEF.md (this session)
# Feature spec: feature-spec.md (this session, status APPROVED)

Headless run (run 0005). No human was available mid-plan; every judgment call
below is recorded as a decision rather than left implicit, per the plan being
carried into Implement mode without further clarification.

## Shared Decisions

Decisions binding on two or more stories. Anything true of exactly one story
lives in that story's sub-issue instead.

- **Terminology.** "Project" in the UI is the existing `store.Repo`; no new
  grouping entity is introduced. The UI label stays "Projects"; the data and
  every accessor is repo-shaped.
- **Config additions.** `Config.UI{Enabled bool, Addr string}` (`ui:` in
  YAML); `Enabled` defaults `true`, `Addr` defaults `127.0.0.1:8787`.
  `Paths.UITokenPath()` is `$LF_HOME/ui.token`. Both must be documented in
  `configHeader` and match `Defaults()` (existing `template_test.go` enforces
  this).
- **Auth mechanics.** Cookie name `lf_ui`; `HttpOnly`, `SameSite=Strict`,
  `Path=/`, no `Secure` (plain loopback HTTP), session cookie (no
  `Max-Age`). Bootstrap is `?token=<value>` on any route, verified with
  `crypto/subtle.ConstantTimeCompare`, then `303` to the same path with the
  query stripped. CSRF field value is `HMAC-SHA256(token, "lf-ui-csrf")`,
  hex-encoded in full, compared with `subtle.ConstantTimeCompare`.
- **Host/Origin allowlist (resolves conflict C-4).** `ui.addr` validation
  permits `127.0.0.1`, `::1` and `localhost` as host, but the spec's literal
  `Origin == http://{ui.addr}` check would 403 an operator who browses
  `localhost` while `ui.addr` is `127.0.0.1`. One derived allowlist —
  `{127.0.0.1:port, localhost:port, [::1]:port}` — is shared by both the new
  Host-header check (DNS-rebinding defence, ST-1) and the `POST /settings`
  Origin check (ST-8). Cookies remain host-scoped regardless, so the operator
  must stay on whichever host `litefactory ui` printed; ST-9's help text
  should say so in one sentence.
- **Security headers, uniformly.** Every response — including `401`, `403`,
  `404` and `/static/app.css` — carries `X-Frame-Options: DENY`,
  `Content-Security-Policy: default-src 'none'; style-src 'self'; img-src
  'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'`,
  `Referrer-Policy: no-referrer`, `X-Content-Type-Options: nosniff`,
  `Cache-Control: no-store`. No `Access-Control-Allow-*` header is ever set.
  This is one middleware (ST-1), applied before routing, not a per-handler
  concern.
- **Query budget (resolves deviation D-3).** The spec's "no page issues more
  than 4 DB queries" is arithmetically impossible for `GET /`, which needs
  six whole-result reads (`GetPollCursor`, `ListRepos`,
  `ListRepoSessionCounts`, `ListBacklogSessions`, `CountRunsSince`,
  `ListRecentRuns`). Restated: **≤6 queries on `/`, ≤4 on every other page;
  every query is a single short whole-result read, and no connection,
  transaction or cursor is held across template rendering or a file read.**
  `/runs/{id}` is exactly 4 (`GetRun`, `GetSession`, `GetRepo`,
  `GetPollCursor`).
- **Per-agent tabs render as links, not the mockup's CSS-radio pattern
  (resolves conflict C-1).** The approved mockup (`mockups/run-11.html`)
  switches tabs with selectors hardcoded per tab id, but agent tabs are
  dynamic per run — arbitrary count, arbitrary `subagent_type` names — and a
  static `app.css` cannot enumerate ids it has never seen; the CSP forbids a
  per-page `<style>` block as the alternative. Tabs render instead as
  `<a href="?agent={key}&follow=1">` with an `.active` class on the current
  panel; all panels are still server-rendered in the same response, and
  exactly one is visible, satisfying the mockup's acceptance criterion
  verbatim. This also fixes a real bug the radio pattern would ship with: a
  `follow=1` meta-refresh reloads the page and would reset a radio-based tab
  selection to `orchestrator` every 5s, defeating US-5 for precisely the
  tabs US-4 exists to provide — a link-based selection survives the refresh
  because the reload carries `?agent=` with it. The visual design is
  unchanged; only the interaction mechanism (click loads vs. toggles) moves
  from the mockup. Affects ST-2 (the CSS must not enumerate tab ids) and
  ST-6 (the handler renders the active tab from the query parameter).
- **Mockup CSS becomes one static, class-based stylesheet (resolves conflict
  C-2).** The approved mockups carry 10 inline `style=` attributes and 8
  per-file `<style>` blocks, both blocked by `style-src 'self'`; all become
  classes in one `internal/webui/static/app.css`, extracted (not redesigned)
  from the mockups. Because `/static/app.css` sits behind the auth
  middleware like every other route, the `401` page cannot load it — the
  `401` page is deliberately left unstyled rather than exempting the
  stylesheet from auth. Affects ST-1 (the 401 page) and ST-2 (the
  extraction).
- **Settings writes stay stdlib-only; no new dependency (resolves deviation
  D-2, informs conflict C-3).** `internal/config/purity_test.go`
  (`TestValidationHasNoThirdPartyImports`, enforced) fails on any import
  whose first path segment contains a dot, with `gopkg.in/yaml.v3` the one
  hardcoded exception — so `goccy/go-yaml`, the dependency the research
  brief and feature spec left conditionally open, cannot live in
  `internal/config` at all, and putting it in `webui` instead would split
  ownership of `config.yaml` across two packages reading it with two
  different YAML implementations. ST-8 opens with a decision probe
  (comparing a stdlib in-place scalar editor against a `yaml.v3`
  `yaml.Node` round-trip for byte-identity against `DefaultFileContents()`)
  but the stdlib path is the one this plan expects to win, and no story may
  add a module to `go.mod` without a recorded amendment (see Cross-Story
  Constraints). `go.mod` keeps its four direct requires.
- **`internal/runlog` is a new shared leaf package (informs conflict C-5).**
  The claude stream-json wire model (`Event`/`Message`/`Block`, the kind
  constants, the block→kind classification) moves out of
  `internal/cli/cmd_logs_render.go`'s unexported types into a new
  stdlib-only leaf package, imported by both `cli` and `webui`. This is a
  new package-boundary decision that outlives ST-6 (its owning story), so it
  is recorded here; the extraction task itself, and the requirement that
  `cmd_logs_render_test.go` pass unmodified, are ST-6's.

## Workflow

- **Shipping order:** ST-3 (store accessors) and ST-1 (foundations) start in
  parallel — ST-3 is pure `internal/store` work independent of the HTTP
  surface. ST-2 (shell) follows ST-1 immediately — every page story
  duplicates layout/asset/helper work if it lands first. Once ST-2 (and
  ST-3, for the two that read it) are in, ST-4, ST-5, ST-6 and ST-7 can
  proceed in any order or in parallel. ST-9 (`litefactory ui`) is small and
  can follow ST-1 at any point. **ST-8 (settings) ships last** — it is the
  only mutating story, the only one with a dependency risk (the decision
  probe), and the only one with a sanctioned degradation path.
- **Dependency graph:**

  ```
  ST-3 (store accessors) ─────────────────┐
                                          ├──→ ST-4 (dashboard)
  ST-1 (foundations) ──→ ST-2 (shell) ────┼──→ ST-5 (projects/sessions)
     │                       │            ├──→ ST-6 (run detail + tabs + follow)
     │                       │            ├──→ ST-7 (daemon log)
     │                       └────────────┴──→ ST-8 (settings)
     └──→ ST-9 (litefactory ui)
  ```
- **Hard coupling:** nothing in this feature ships partially observable —
  `ui.enabled: false` (the existing default-safe path) must keep working
  throughout, so ST-1's config/listener wiring must not regress the no-UI
  case at any intermediate commit.

## Cross-Story Constraints

- **No `template.HTML`, `template.JS`, `template.CSS` or `template.URL`
  conversion anywhere in `internal/webui`**, on anything derived from a log,
  a DB row, a config value or a request. Run logs are attacker-influenced
  end to end (issue text, tool output, fetched pages, sub-agent names) and
  render unredacted by design; `html/template`'s contextual escaping is the
  primary defence and the CSP is the second layer. This is the
  highest-severity risk the plan carries and needs an explicit test in every
  story that renders untrusted content (ST-6 especially).
- **`internal/webui` MUST NOT import `internal/redact`, `internal/cli` or
  `internal/daemon`** — enforced by a `go/parser` boundary test over
  non-test files, modelled on `internal/identity/policy_test.go`. Logs
  render unredacted by design (the CLI already forbids the `redact` import
  for the same reason); this feature must not create a second, divergent
  version of the truth. A `_test.go` file may import `internal/harness` to
  pin JSON tag names for the meta-sidecar decode struct — the boundary test
  covers non-test files only.
- **Gather, then render.** No DB rows streamed into a template, no file
  handle open during template execution; output is built into a
  `bytes.Buffer` and copied to the response only on success, so a template
  error mid-render never leaves a half-page behind a committed `200`. The
  store's single-connection cap makes a held connection a daemon-wide stall.
- **`ctx` first, everywhere**, threaded from `r.Context()` into every store
  call and file read. `context.Background()` stays lint-forbidden outside
  `main`; the shutdown grace context derives from the root context via
  `context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)`.
- **Zero JavaScript, zero third-party front-end assets, no build step.** No
  `<script>` tag, no `on*=` attribute, no inline `style=` or `<style>`
  anywhere in rendered output.
- **No file path is ever built from a request parameter.** Log paths come
  from `runs.log_path` or `naming.RunLogPath` after
  `naming.ParseRef(session.TaskRef)`; the stylesheet is one hardcoded
  `embed.FS` read, never `http.FileServer`.
- **Never log the query string or the token.** Request logging records the
  path only; the token file's path may be logged, its contents never — the
  bootstrap URL carries the secret in its query string.
- **Every new goroutine states in a comment what bounds its lifetime.** The
  UI listener adds one `srv.Serve` goroutine (bounded by `srv.Shutdown` on
  root-context cancellation) plus one `net/http`-spawned goroutine per
  accepted connection, outside litefactory's direct control. This
  contradicts `APP_CONSTRAINTS.md`'s CONC-2 as currently written, which
  calls itself "the canonical statement" of a four-goroutine, twenty-ceiling
  inventory — CONC-2 must be amended (naming the listener goroutine, its
  termination condition, and the per-connection goroutines) as part of ST-1,
  recorded as an amendment in `.aiteam/AC_AMENDMENTS.md`, not silently
  broken. A small stdlib concurrency-limiting listener wrapper (cap ~32, no
  new dependency) is recommended so the inventory keeps a real number rather
  than "unbounded"; `golang.org/x/net/netutil.LimitListener` must not be
  used, since it is a new module.
- **No mocking framework, no new module, no fakes for the store.** Real,
  migrated SQLite in `t.TempDir()` for every test touching storage — no
  `Store` interface introduced for testability, matching the codebase's
  existing pattern of testing against concrete `*store.DB`.
- **No migration file, no schema change, no change to the single-connection
  cap.** This feature is a read projection over existing tables plus one
  config write; the cap is load-bearing and explicitly out of scope to
  raise.
- **Every exported identifier in the two new packages (`internal/webui`,
  `internal/runlog`) carries a doc comment, and each package a `doc.go`.**
  `revive:exported` and `revive:package-comments` are enforced lint rules.
- **`internal/webui` is black-box tested** (`package webui_test`), matching
  the existing convention for `daemon`/`git`/`github`; a `_internal_test.go`
  file is the only white-box exception (`package webui`).
- **No integration build tag.** `.claude/hooks/check-test-env.sh` blocks
  `-tags integration` / `-run …Integration` unless `TEST_BASE_URL` and
  `TEST_DATABASE_URL` are set, and this tree has no such tests. The
  "`GET /` under concurrent turns in under 2s" criterion is an ordinary
  in-package test with real SQLite and concurrent writer goroutines.
- **Test infrastructure hygiene.** Tests bind port 0 or use `httptest`,
  never a fixed port — a live daemon runs on this machine from
  `dist/litefactory` during development; no task may run `make build`,
  restart that daemon, or edit `~/.litefactory/config.yaml`.
- **`gosec` G112/G114 and G304 are expected findings, not surprises.** The
  `http.Server` must be constructed explicitly with `ReadHeaderTimeout` etc.
  and serve on a separately created `net.Listener` (so a bind failure is a
  distinguishable startup error, not a panic inside `ListenAndServe`); a log
  path opened from a variable needs a `//nolint:gosec` with a stated reason,
  matching the existing pattern in `config.Load`.
- **`make check` (`gofmt`, `go vet`, `golangci-lint v2`, `go test -race
  ./...`) is the definition of done for every task in every story.**

## Story Index

| ID | Title | Size | Depends On | Sub-issue |
|----|-------|------|-----------|-----------|
| ST-1 | Foundations, bind and auth | L | — | https://github.com/marioatcloudvane/litefactory/issues/23 |
| ST-2 | Shell: layout, assets, shared helpers | M | ST-1 | https://github.com/marioatcloudvane/litefactory/issues/24 |
| ST-3 | Store read accessors | S | — | https://github.com/marioatcloudvane/litefactory/issues/25 |
| ST-4 | Dashboard | M | ST-2, ST-3 | https://github.com/marioatcloudvane/litefactory/issues/26 |
| ST-5 | Projects and sessions | M | ST-2, ST-3 | https://github.com/marioatcloudvane/litefactory/issues/27 |
| ST-6 | Run detail, per-agent tabs, follow | L | ST-2 | https://github.com/marioatcloudvane/litefactory/issues/28 |
| ST-7 | Daemon log | S | ST-2 | https://github.com/marioatcloudvane/litefactory/issues/29 |
| ST-8 | Settings | L | ST-2 | https://github.com/marioatcloudvane/litefactory/issues/30 |
| ST-9 | `litefactory ui` command | S | ST-1 | https://github.com/marioatcloudvane/litefactory/issues/31 |

## Out of Scope

- Any mutating action other than saving allowlisted `config.yaml` fields —
  no starting/cancelling runs, no repo add/remove, no daemon
  start/stop/restart, no worktree cleanup, no comment posting.
- An editable raw-YAML textarea (the mockup shows one; v1 renders it
  read-only).
- Any JSON/REST/gRPC API, websockets, SSE, or client-side JavaScript.
- htmx, Pico.css, Water.css, simple.css, or any other third-party front-end
  asset.
- Unix domain socket binding and any TCP↔socket proxy or helper process.
- TLS, multi-user auth, user accounts, roles, login forms, token rotation UI.
- Remote or LAN access, reverse-proxy support, `0.0.0.0` binding.
- Log redaction in the UI, log rotation, log search/grep, log
  download/export.
- Dark-mode toggle, theme switching, charts, metrics, graphs, keyboard
  shortcuts, mobile-specific layout.
- Replacing `gopkg.in/yaml.v3` elsewhere in the project.
- Raising the store's single-connection cap, adding a second DB handle, or
  any schema migration.
- Pagination and full-text search across runs (fixed row caps only).
- `litefactory ui --open` / launching a browser automatically.

## Provenance

- Citation base: `4271416` (HEAD of `lf/11-we-need-a-simple-ui-for-litefactory`
  at plan time).
- Known-stale areas: the architect review cites specific line numbers
  (`internal/cli/cmd_logs_render.go:242`, `internal/redact/redact.go` /
  `internal/cli/cmd_logs.go:149-153`, `mockups/run-11.html:67-73`) read
  directly from the tree at citation base. Re-verify these against the
  current tree before implementing the tasks that depend on them — commits
  may land between this plan and Implement mode.
- Re-pinning requirements before merge: re-run
  `internal/config/purity_test.go` and the new `internal/webui` boundary
  test to confirm the import restrictions still hold; re-verify the
  `POST /settings` byte-identity criterion against the current
  `DefaultFileContents()` output, since the default config template is a
  plausible target for unrelated future edits.
