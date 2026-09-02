# ST-1 — Foundations, bind and auth

> As the operator, I want the UI bound to loopback and gated by a token file,
> so no other machine and no unauthenticated local process can read my
> unredacted logs.

Stands up the HTTP listener, the two new config keys, the token file, and the
security-header and auth middleware that every other page in this feature
sits behind.

## Why This Matters

Right now the only way to see what litefactory is doing is to SSH into the
machine and run CLI commands — this story is what makes "open a browser tab"
possible at all, and it is also where the safety guarantees for that browser
tab live. Nothing else in this feature is buildable, or safe to expose, until
this piece exists. The hard part isn't standing up a listener; it's making
sure a browser on this machine — and only this machine — can ever see the
data, when there's no username or password in the usual sense, only a secret
printed to a terminal, and when the logs behind that secret are deliberately
shown in full, unredacted.

## Architecture Notes

- New package `internal/webui`: `Options` + `New(opts) (*Server, error)`
  validates every dependency at construction time; a `ServeMux` with all
  routes wired (later stories add handlers, this story stubs them); a `Now
  func() time.Time` clock seam so time-dependent behaviour (liveness dot,
  relative times) is testable without sleeping.
- `internal/webui` MUST NOT import `internal/cli`, `internal/daemon` or
  `internal/redact` — this story adds the boundary test that enforces it
  (`go/parser` over non-test files, modelled on
  `internal/identity/policy_test.go`). Logs render unredacted by design; a
  second, divergent redaction path here would be the exact failure this
  boundary prevents.
- Auth middleware wraps the whole mux once, not per-route — a route added by
  a later story is protected by default. Order: security headers → Host
  allowlist (DNS-rebinding defence, see plan_brief's Host/Origin allowlist
  decision) → auth (cookie or `?token=` bootstrap).
- `401` responses for "no token" and "wrong token" must be byte-identical —
  do not let a wrong-token path leak information a no-token path doesn't.
- Platform: `gosec` will fail the build on an `http.Server` without
  `ReadHeaderTimeout`/`ReadTimeout`/`WriteTimeout`/`IdleTimeout` (G112) or on
  `http.ListenAndServe` (G114). Construct the server explicitly and call
  `srv.Serve(ln)` on a `net.Listen`-created listener, so a bind failure
  (port in use) is a distinguishable fatal startup error rather than a panic
  inside a convenience function.
- Risk: the UI listener adds one long-lived `srv.Serve` goroutine plus one
  `net/http`-spawned goroutine per accepted connection — outside
  litefactory's direct control and in tension with `APP_CONSTRAINTS.md`
  CONC-2's stated twenty-goroutine ceiling. This story must amend that
  bullet in `APP_CONSTRAINTS.md` (naming the listener goroutine, its
  shutdown-triggered termination, and the per-connection goroutines) and
  record the amendment in `.aiteam/AC_AMENDMENTS.md` — not break the
  constraint silently. A small stdlib concurrency-limiting listener wrapper
  (cap ~32) is recommended to keep the inventory a real number; do not pull
  in `golang.org/x/net/netutil.LimitListener` — new module.
- Risk: `runDaemonRun` must keep working with `ui.enabled: false` at every
  intermediate commit — no listener, no token file, and the existing
  daemon-only behaviour is unchanged.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-1.1 | `Config.UI{Enabled, Addr}` + `Defaults()` + `configHeader` template entry | infra | — | S |
| T-1.2 | `validateUI` in `validate.go`: loopback host allowlist, port 1–65535, aggregated via `errors.Join` | infra | T-1.1 | S |
| T-1.3 | `Paths.UITokenPath()` + temp-file path helper (reused by ST-8) | data | — | XS |
| T-1.4 | `internal/webui` skeleton: `doc.go`, `Options`/`New`, `ServeMux` with stubbed routes, `Now` clock seam | api | T-1.1 | M |
| T-1.5 | Token file: create-if-absent 32 random bytes base64url unpadded, `0600` explicit, chmod-back-and-warn on wrong mode, read | logic | T-1.3 | S |
| T-1.6 | Security-headers middleware (CSP, X-Frame-Options, Referrer-Policy, X-Content-Type-Options, Cache-Control, no CORS) applied to every response including 401/403/404/static | api | T-1.4 | S |
| T-1.7 | Auth middleware: Host allowlist, `lf_ui` cookie, `?token=` bootstrap → 303, constant-time compare, identical 401 body for wrong-token and no-token | api | T-1.4, T-1.5 | M |
| T-1.8 | `http.Server` construction (explicit timeouts) + explicit `net.Listen` + `Shutdown` with 5s grace on root-context cancellation | api | T-1.4 | S |
| T-1.9 | Wire into `runDaemonRun` alongside `loop.Run(ctx)`; `ui.enabled: false` creates no listener and no token file | infra | T-1.8 | S |
| T-1.10 | Amend CONC-2's goroutine inventory in `APP_CONSTRAINTS.md`, record the amendment in `.aiteam/AC_AMENDMENTS.md`; optional stdlib limit-listener wrapper | infra | T-1.8 | XS |
| T-1.11 | Tests: `config.Validate` table for `ui.addr` (bad and good forms) + existing `template_test.go` stays green | test | T-1.2 | S |
| T-1.12 | Tests: auth + headers via `httptest` — 401 no cookie, 401 wrong token byte-identical, `?token=` → 303 + cookie attributes, cookie → 200, headers present on every status | test | T-1.6, T-1.7 | M |
| T-1.13 | Test: `internal/webui` imports neither `internal/redact` nor `internal/cli` nor `internal/daemon` (non-test files) | test | T-1.4 | S |
| T-1.14 | Test: root-context cancellation shuts the listener down within 5s and `daemon run` exits 0 — channel rendezvous, no `time.Sleep` | test | T-1.9 | S |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-1.1 | With `ui.enabled: true`, starting the daemon binds `127.0.0.1:8787` and creates `$LF_HOME/ui.token` with mode `0600`; `lsof`/`netstat` shows no non-loopback binding. |
| AC-1.2 | `config.Validate` rejects `ui.addr` values `0.0.0.0:8787`, `:8787`, `192.168.1.5:8787` and `127.0.0.1:0`, naming the field. |
| AC-1.3 | With `ui.enabled: false`, no listener is created and no token file is written. |
| AC-1.4 | A request to any route with no cookie and no token returns `401` and does not set a cookie. |
| AC-1.5 | A request to `/projects?token=<valid>` returns `303` to `/projects` and sets `lf_ui` with `HttpOnly`, `SameSite=Strict`, `Path=/`; the follow-up request with the cookie returns `200`. |
| AC-1.6 | A request with a wrong token returns `401` with a body byte-identical to the no-token case. |
| AC-1.7 | Every response — including `401`, `403`, `404` and `/static/app.css` — carries the specified security headers and no `Access-Control-Allow-*` header. |
| AC-1.8 | `go test ./...` includes a test asserting `internal/webui` imports neither `internal/redact` nor `internal/cli` nor `internal/daemon`. |
| AC-1.9 | Cancelling the daemon's context shuts the listener down within 5s and the process exits; `daemon run` with the UI enabled still exits 0 on SIGTERM. |
