# litefactory — working rules for coding agents

## Routing

`- Mode: X` in the prompt header is authoritative: never classify, never change it.

Use the factory-supplied role definition when present; do not reopen its local
skill or agent file. The routing below is fallback when no role was supplied.
Dispatch-only rules below do not restrict a supplied worker role.
Read supporting contracts only when relevant and not already supplied in full.

| Mode | Invoke |
|---|---|
| Research | `research-orchestrator` |
| Plan | `plan-orchestrator` |
| Implement | `implement-orchestrator` |
| Validate | `go-code-reviewer`, read-only contract |
| Spike | nobody here — the factory runs the graph in `docs/workflow/spike.md`; never execute it in this session |

Skills live at `.codex/skills/<name>/SKILL.md` and are listed to you
automatically. Session setup is in `docs/workflow/session.md`.

Outside Validate, scaffold `$session` per `docs/workflow/session.md` first, then
pass the request, `$session` and any factory-supplied frozen stories inline.

Validate reuses the supplied session and frozen scope read-only. Do not scaffold
a directory or MANIFEST, write artifacts, or require artifact existence to route
or finish Validate. Validate finishes with structured runner findings, not an
artifact; assess the returned result without writing or checking for a result
file.

Invoke once, read the result once, check the artifact once. Never poll for
status. Report a failure or a missing artifact and stop.

Do not read source, search the repo or form a view on the design; that is the
orchestrator's job and its context is cheaper than yours.

Never `git add`, `commit`, `stash` or `push` — litefactory commits the worktree
itself, and uncommitted work at the end of a run is normal.

Research uses `internal/teamsource/research/skills/research-evidence/SKILL.md` only. For other
modes, lifecycle, managed stories, Plan continuation and the story protocol are in
`.codex/skills/workflow/SKILL.md`.

## Prohibitions

- **A live daemon may be running from `dist/litefactory`.** Do not run
  `make build`, write into `dist/`, or kill or restart it. `dist/` is
  gitignored.
- **Runtime state under `$LF_HOME`** (default `~/.litefactory`) belongs to that
  daemon. Do not edit `config.yaml` or anything else there by hand.
- Never construct `--dangerously-skip-permissions`, or any equivalent, anywhere
  in this tree — including from string fragments. Enforced for `claude` in
  `internal/config/policy.go`; the posture applies to every agent binary.
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

## Shared completion

Follow the factory-declared final route: shared result helper, permitted communication
operations or structured terminal output. Use the supplied explicit baseline and
revision; prior prose is not editable state. A story checkpoint or input receipt
alone does not complete the run. Input, engineering, acceptance and remote effects
remain independent. Correct preflight diagnostics; preserve request identity on
reply loss and use a new request for corrected content. The factory revalidates
after exit; missing/malformed output blocks nominal success, and failure or
interruption retains its cause. Validate remains reference-only, without shell,
edits, capture, fresh checks or delegation. Legacy scopes keep their declared
compatibility route until supported upgrade/admission. Adoption and retirement are
separate; offline checks do not satisfy authentic #274/#296 host observations.
