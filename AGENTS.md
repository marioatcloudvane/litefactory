# litefactory — working rules for coding agents

This is Codex's project-instructions file (Codex reads `AGENTS.md`, not
`CLAUDE.md`). It exists so a Codex-driven run in this repository is not
operating with zero project instructions.

`litefactory` (module `github.com/cloudvane/litefactory`, Go 1.26) is a macOS
daemon that polls GitHub issues and, when the owner comments, runs a headless
`claude` process inside a git worktree and reports back on the thread. Single
Go module, not a monorepo — everything lives under `internal/`, with
`cmd/litefactory/main.go` as wiring only.

## What this file is not

This project is also managed by an AI team defined in `CLAUDE.md` that
operates in four modes (Research, Plan, Implement, Spike) via orchestrator
sub-agents, per-agent tool restrictions, and a `PreToolUse` enforcement hook
(`.claude/hooks/check-test-env.sh`). **That mechanism is specific to how
Claude Code works — subagent delegation, per-agent permission scoping, and a
hook that can intercept and block a tool call — and this file does not
reproduce it.** Whether Codex's own `/agent` subagents and hooks feature
could host an equivalent team is genuinely unmeasured as of this writing: a
probe of the `codex` binary in this repository's environment could not run
it (the binary was unavailable to that probe), so no claim about Codex's
subagent or hook capabilities is asserted here as fact. Treat any such
capability as unknown until it has actually been measured, not as something
this file has already decided one way or the other.

What Codex *can* rely on below is tool-independent: it holds regardless of
which coding agent is driving the session.

### Mode vocabulary

The four names — Research, Plan, Implement, Spike — describe stages of work
in this project (understand the problem, turn a brief into a plan, execute
the plan, or take a time-boxed ad-hoc detour) independent of which tool
executes them. The *mechanism* that runs those modes in Claude Code
(orchestrator agents in `.claude/agents/`, session scaffolding under
`.claude/modes/`) is Claude-Code-specific and not available to a Codex
session. If you are asked to "research", "plan", "implement", or "spike"
something in this repository under Codex, treat that as a request about
scope and rigor, not as a signal to invoke a delegation mechanism that does
not exist here.

## Build, test, lint

- **Build:** `go build ./...` (safe to run any time). The project's own
  release build is `make build`, which writes a static, cgo-free binary to
  `dist/litefactory` — see the prohibition below before using it.
- **Test:** `go test ./...` for a quick pass; `make test` runs the full suite
  with `-race`, which is what CI enforces.
- **Vet:** `go vet ./...`.
- **Format:** `gofmt -l .` must report nothing; `make fmt` fails the build if
  it does.
- **Lint:** `make lint` runs a pinned `golangci-lint` v2 (`.golangci.yml` is a
  v2-format config; do not run an unpinned or v1 linter against it).
- **`make check`** runs `fmt vet lint test` in that order and stops at the
  first failure — this is what CI enforces, and the thing to run before
  calling any change finished.
- There are **no build tags and no integration tests** in this tree today.
  Do not invent a `-tags integration` build or a `TestFoo_Integration` test
  name. In Claude Code this is additionally enforced by a `PreToolUse` hook
  that blocks such a `go test` invocation outright unless `TEST_BASE_URL`
  and `TEST_DATABASE_URL` are set; that hook is Claude-Code-specific
  machinery and will not stop a Codex session from doing this — the rule
  itself (don't add integration tests to this tree) still applies and is not
  optional just because the enforcement mechanism isn't present.

## Prohibitions

- **A live daemon may be running from `dist/litefactory`.** Do not run
  `make build`, do not write into `dist/`, and do not kill or restart it.
  `dist/` is gitignored — nothing under it is ever committed.
- **Runtime state lives under `$LF_HOME`** (default `~/.litefactory`). Do not
  edit `~/.litefactory/config.yaml` or anything else under `$LF_HOME` by
  hand; it belongs to the running daemon, not to a coding session.
- Never construct `--dangerously-skip-permissions` (or any equivalent
  skip-permissions flag) anywhere in this tree, including in a string built
  from fragments. This is enforced for the `claude` invocation
  (`internal/config/policy.go`) and the same posture applies to any external
  agent binary this codebase wires up.

## Store rules

- All SQL lives in `internal/store` and nowhere else. No package outside it
  may open a database connection, hold a `*sql.DB`, or embed a raw query.
- The driver is `modernc.org/sqlite` (pure Go, cgo-free), driver name
  `"sqlite"` — not `"sqlite3"`. The connection pool is capped at **one**
  connection; this is a correctness requirement, not a tuning knob (see
  `internal/store/store.go`'s doc comment before changing it).
- Migrations are hand-rolled and embedded: `internal/store/migrations/NNNN_*.sql`
  via `//go:embed`, one transaction per file, version taken from the filename
  prefix, recorded in `schema_migrations`. A schema change is a new `.sql`
  file and nothing else — do not add a migration library.
- `store.Open(ctx, path)` does not migrate; `store.Migrate(ctx)` must be
  called explicitly afterward.
- Every store accessor takes `ctx` first.

## Package map and dependency direction

Single module, everything under `internal/`. Arrows point at what a package
imports (litefactory packages only); packages marked leaf import nothing else
in this tree:

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

Notable rules that follow from this shape:

- `internal/config` imports nothing from this tree and is stdlib-only aside
  from its YAML decode — an enforced invariant (`internal/config/purity_test.go`).
- `internal/harness` is the only place that executes `claude` (or any coding
  agent binary this codebase invokes as a subprocess).
- `internal/git` is the only gateway to the `git` CLI.
- `internal/identity` is the only package that execs `gh`, and only from
  `litefactory init`.
- `internal/report` cannot import `internal/turn` (the dependency runs the
  other way).
- `internal/daemon` is this project's sense of time (tick loop, backoff,
  cursor); `internal/turn` orchestrates a single turn with no clock of its
  own. Do not blur that split.

Before adding new code, check whether it already exists — do not reimplement
a package that is already the designated owner of a concern (path resolution
under `$LF_HOME` belongs to `config.Paths`; slug/branch/worktree-path
derivation belongs to `internal/naming`; SQL belongs to `internal/store`;
git operations belong to `internal/git`; and so on for the leaves above).

## Where the fuller rules live

This file carries only what does not depend on which coding-agent tool is
running it. The full, normative rule set — security, configuration,
testing, concurrency, style, and external-binary rules, each with a stable
identifier (`SEC-1`, `CFG-4`, `TEST-8`, …) — lives in
`.aiteam/APP_CONSTRAINTS.md`. A descriptive map of what already exists in
the codebase lives in `.aiteam/APP_CONTEXT.md`. Read the sections relevant
to the change you are making; nothing in this file should be read as
overriding either of those.
