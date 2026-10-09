# Litefactory development rules

These rules apply only when working on litefactory. Other repositories keep their own project rules. The factory-supplied role, graph and permission ceiling remain authoritative. The literal `- Mode: X` header is authoritative; never classify or change it.

Never stage, commit, stash or push; the factory saves the worktree. Never stop processes you did not start.

## Prohibitions

- **A live daemon may be running from `dist/litefactory`.** Do not run
  `make build`, write into `dist/`, or kill or restart it. `dist/` is
  gitignored.
- **Runtime state under `$LF_HOME`** (default `~/.litefactory`) belongs to that
  daemon. Do not edit `config.yaml` or anything else there by hand.
- Never bypass or weaken runner permission checks. This applies to every agent binary, including when composing arguments from fragments.
- No build tags and no integration tests exist in this tree. Do not invent a
  `-tags integration` build or a `TestFoo_Integration` name.

## Build, test, lint

`go build ./...` and `go vet ./...` are safe any time. `make check` runs
`fmt vet lint test` in order, stops at the first failure, and is what CI and
Validate enforce. `make test` runs every test. It keeps full `-race` coverage outside
`internal/turn`, `internal/store` and `internal/cli`; those SQL-heavy packages
run completely without instrumentation and then repeat the explicit
`RACE_TESTS_*` selections in Makefile with `-race`. Those lists cover existing
goroutine/synchronization fixtures and shared-state owners; extend them when
adding concurrent behavior. `PKGS` selects packages in both phases, and either
phase failing stops the target. SQLite instrumentation dominates the blanket
race cost. A factory story worker or bug fixer does not run `make check` or `make test`:
it builds the repository, runs every package it changed completely (with
`-race`, except `internal/store`, `internal/turn` and `internal/cli`, which run
without it), and lints them; the full suite belongs to
Validate. `gofmt -l .` must report nothing. `make lint` runs a pinned
golangci-lint v2 against a v2 `.golangci.yml`; never an unpinned or v1 linter.
Prefer `make build` only as the operator — see the prohibition above.

## Store rules

- All SQL lives in `internal/store`. No package outside it opens a connection,
  holds a `*sql.DB`, or embeds a query.
- Driver is `modernc.org/sqlite`, driver name `"sqlite"` (not `"sqlite3"`). The
  pool is capped at **one** connection: a correctness requirement, not a tuning
  knob — read `internal/store/store.go`'s doc comment before changing it.
- Migrations are hand-rolled and embedded: `internal/store/migrations/*.sql`
  via `//go:embed`, one transaction per file, version from the filename prefix,
  recorded in `schema_migrations`. A schema change is a new `.sql` file and
  nothing else — no migration library.
- New migrations use a fresh UTC millisecond timestamp per file:
  `YYYYMMDDHHMMSSmmm_description.sql` (for example,
  `20261007130500123_example.sql`), never the local maximum plus one.
  Check the local directory for duplicate numeric prefixes before writing;
  timestamps reduce collisions but do not guarantee uniqueness. Give dependent
  migrations increasing versions in dependency order; timestamps do not validate
  SQL dependencies. Keep historical/applied filenames unchanged. If Implement
  parks a migration conflict, rename only the story's new, unapplied migration
  and recheck against current integration when the owner resumes the story.
- `store.Open` does not migrate; call `store.Migrate` explicitly. Every accessor
  takes `ctx` first.

## Package map and dependency direction

Single module under `internal/`. Arrows point at what a package imports:

```
config, naming, store, git, secrets, identity, launchd, logging, gittest  →  leaves

redact   → secrets
github   → naming, redact, secrets
harness  → config, naming
report   → git, harness, naming, redact
repos    → git, naming, store
turn     → config, git, github, harness, naming, redact, report, store
daemon   → config, git, github, harness, naming, redact, report, repos, secrets, store, turn
cli      → config, daemon, git, github, identity, launchd, naming, repos, secrets, store
cmd/litefactory → cli, config, logging
```

- `internal/config` imports nothing from this tree and is stdlib-only aside from
  its YAML decode — enforced by `internal/config/purity_test.go`.
- `internal/harness` runs Claude, `internal/codex` runs Codex; both implement
  `internal/agent.Runner`.
- `internal/git` is the only gateway to the `git` CLI. `internal/identity` is the
  only package that execs `gh`, and only from `litefactory init`.
- `internal/report` must not import `internal/turn`.
- `internal/daemon` owns time (ticks, backoff, cursor); `internal/turn` runs one
  turn with no clock. Do not blur that split.

Before adding code, check the designated owner: `$LF_HOME` paths belong to
`config.Paths`, slug/branch/worktree derivation to `internal/naming`, SQL to
`internal/store`, git to `internal/git`.

## Where the fuller rules live

The normative rule set with stable identifiers (`SEC-1`, `CFG-4`, `TEST-8`, …)
is `.aiteam/APP_CONSTRAINTS.md`; a map of what already exists is
`.aiteam/APP_CONTEXT.md`. Neither is overridden by this file.

