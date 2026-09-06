# APP_CONTEXT — litefactory

**This file is a lookup reference, not a design document.** It answers one
question: *does this already exist, and where?* Read it to find what is already
built so you do not reinvent it.

It deliberately does **not** hold the rules you must follow — those are in
[`.aiteam/APP_CONSTRAINTS.md`](APP_CONSTRAINTS.md), and they are normative
(MUST / SHOULD / MAY). It also deliberately holds no history: what was built
last week is in git log and under `.aiteam/<branch>/<date>/`, and reading it
back as context is how a run ends up loading a hundred kilobytes to answer a
question the package map answers in three lines.

`litefactory` (module `github.com/cloudvane/litefactory`, **Go 1.26**) is a macOS
daemon that polls GitHub issues and, when the owner comments, runs a headless
`claude` process inside a git worktree and reports back on the thread.

Single Go module. Not a monorepo. Everything is under `internal/`, with
`cmd/litefactory/main.go` as wiring only (umask, signal context, logger,
dispatch, exit code).

In a hurry? **[Do not reinvent these](#do-not-reinvent-these)** is the index.

---

## Before you run anything

- **A live daemon is running from `dist/litefactory`.** Do not `make build`, do
  not write into `dist/`, do not kill or restart it. `dist/` is gitignored.
- Safe: `go build ./...`, `go test ./...`, `go vet ./...`, `gofmt -l .`.
- `make check` = `fmt vet lint test` (this is what CI enforces). `make test`
  runs with `-race`. `make lint` runs a pinned golangci-lint v2 (`.golangci.yml`
  is a v2-format config).
- There are **no build tags and no integration tests** in the tree today. A
  `PreToolUse` hook (`.claude/hooks/check-test-env.sh`) blocks
  `go test -tags integration` / `-run …Integration` unless `TEST_BASE_URL` and
  `TEST_DATABASE_URL` are set — you will only trip it if you invent one.
- Runtime state lives in `$LF_HOME` (default `~/.litefactory`). Do not edit
  `~/.litefactory/config.yaml`.

---

## Package map and dependency direction

Arrows point at what a package imports (litefactory packages only).

```
config        →  (nothing)          leaf. everything may depend on it.
naming        →  (nothing)          leaf. pure functions, zero third-party imports.
store         →  (nothing)          leaf. SQLite + every SQL statement in the tree.
git           →  (nothing)          leaf. the only gateway to the git CLI.
secrets       →  (nothing)          leaf. keychain / env token resolution.
identity      →  (nothing)          leaf. the only package that execs `gh`.
launchd       →  (nothing)          leaf. the macOS LaunchAgent.
logging       →  (nothing)          leaf. slog construction + file sink.
render        →  (nothing)          leaf. per-harness renderer registry (claude/codex/opencode).
gittest       →  (nothing)          real package (not _test.go) — git fixtures for tests.

redact        →  secrets
team          →  config             `team add` write engine + team index decode/validate; no CLI wiring yet
github        →  naming, redact, secrets
harness       →  config, naming
report        →  git, harness, naming, redact
repos         →  git, naming, store
turn          →  config, git, github, harness, naming, redact, report, store
daemon        →  config, git, github, harness, naming, redact, report, repos, secrets, store, turn
cli           →  config, daemon, git, github, identity, launchd, naming, repos, secrets, store
cmd/litefactory → cli, config, logging
```

The load-bearing consequences:

- **`internal/config` imports nothing from litefactory and is stdlib-only apart
  from the YAML decode.** `internal/config/purity_test.go` →
  `TestValidationHasNoThirdPartyImports` parses every non-test file and fails
  on any import whose first path segment contains a dot; `config.go` is the
  single exception, for `gopkg.in/yaml.v3`. `internal/naming` states and holds
  the same invariant.
- **`internal/report` cannot import `internal/turn`** — `turn` depends on
  `report`, so the arrow cannot point back. `report.ReplyKind`
  (`internal/report/reply.go`) is its own enum rather than reusing
  `turn.CommandKind`; the mapping is `replyKind()` in
  `internal/turn/report.go` (~line 600). A new reply shape needs a
  `ReplyKind` **and** a case there. `report` is *not* a leaf — it imports
  `git`, `harness`, `naming`, `redact`.
- **`internal/harness` is the only place that executes `claude`** — subprocess,
  argv, child environment allowlist, kill sweep, `NNNN.jsonl` event stream.
  Path derivation is not its job: `naming.RunLogPath`/`RunMetaPath` derive the
  paths, `config.Paths` owns the `$LF_HOME` layout, and the `NNNN.meta.json`
  sidecar is written by `internal/turn` (`harvest.go`, via `harness.WriteMeta`).
- **`internal/turn` orchestrates one turn** and has no clock, no ticker, no
  signals. **`internal/daemon` is litefactory's sense of time** — tick loop,
  singleton lock, backoff, cursor, shutdown — and owns no business logic
  (`internal/daemon/doc.go`, `internal/turn/doc.go`). This split is what makes
  the business path testable without waiting for anything.
- **`internal/daemon` importing `internal/repos`** reuses the pure
  worktree-collision decision function (`internal/repos/collision.go`,
  AC-PAR-4.8) rather than reimplementing it; `repos` imports nothing back.
- **`internal/render` is a leaf**, stdlib plus `gopkg.in/yaml.v3` only. It
  defines the `Renderer` contract (one pure function per harness) and a
  plain-map registry; a harness gains a renderer by calling `render.Register`
  from its own package's `init`.
- **`internal/team` may import only `internal/config`** (`doc.go`'s
  Invariants), never `internal/render` — a future `internal/teamsource`
  renders first and converts `render.RenderedFile` into `team.RenderedFile`.
  It owns a bookkeeping directory at the *target* repo's root, `.litefactory/`
  (distinct from this repo's own `.aiteam/`): `team.lock.json` (provenance)
  and `.team.lock` (flock target).

### Read the `doc.go` files

Thirteen packages carry a `doc.go` whose prose is the real specification for
that package — `daemon`, `git`, `github`, `harness`, `identity`, `launchd`,
`naming`, `redact`, `render`, `repos`, `report`, `team`, `turn` — and `config`
and `store` carry theirs at the top of `config.go` / `store.go`. **Read the
one for the package you are about to touch before you touch it.**

---

## Do not reinvent these

| I need to… | Use | Notes |
|---|---|---|
| resolve `$LF_HOME` or any path under it | `config.Locate()` → `config.Paths` | `ConfigPath DBPath LogsDir RunsDir DaemonLogPath LockPath SettingsPath` |
| derive a slug, branch, worktree path, run log path, claude session id | `internal/naming` | `Ref`, `ParseRef`, `Slug`, `Branch`, `ClaudeSessionID` (UUIDv5, pinned namespace), `WorktreePath`, `RunLogPath`, `RunMetaPath`, `DefaultWorktreeRoot`. All pure; never reads `LF_HOME` |
| run any SQL | `internal/store` | `Open(ctx, path)` then **`Migrate(ctx)` explicitly** — `Open` does not migrate. Accessors are one file per entity |
| run any git command | `internal/git` | `Resolve(ctx)` → `Binary.Open(ctx, dir)` → `Clone` (reads) / `Worktree` (mutates). `EnsureWorktree`, `ParseRemoteURL`, `*CommandError` |
| serialise git ops across concurrent turns on one registered clone | `internal/daemon`'s `cloneGate`, via `turn.Options.CloneGate` | Unexported in `daemon`; `internal/turn` only sees the `turn.CloneGate` interface it declares (`internal/turn/execute.go`). `nil` is refused, never treated as a no-op |
| refuse two registered repos from sharing one worktree directory | `internal/repos` | `ValidateWorktreeRoots(existing)` at daemon startup, `ValidateNewWorktreeRoot(reg, existing)` at `repo add`, both before any row is written (AC-PAR-4.8) |
| mark/clear/list a session that still owes the daemon a look | `internal/store` | `SetSessionNeedsPoll`, `ClearSessionNeedsPoll`, `ListBacklogSessions` (`internal/store/sessions.go`) over `sessions.needs_poll`. Cleared only on a fully drained turn — never from a `defer` (T-PAR-12) |
| build a git fixture in a test | `internal/gittest` | `New(tb)` gives a clone + local bare origin + tracking, offline. `AssertUnchanged` / `Capture`+`Snapshot.Diff` prove read-only behaviour |
| call the GitHub API | `internal/github` | Concrete `*Client`, seven endpoints, `net/http` only. `RateLimitError.RetryAfter` is what the daemon's backoff reads |
| resolve the bot token | `internal/secrets` | `Resolver{Keychain, LookupEnv}` → `Token`. Resolved **once at process start**, never per request |
| strip credential shapes from outbound text | `internal/redact` | `Redactor.Apply(ctx, text, limit) → Body` |
| build a comment body | `internal/report` | Renders only; reaches no network and no database |
| turn an agent's frontmatter+body into a harness's native output file | `internal/render` | `Renderer` + `Register`/`Lookup` registry. `claude`/`opencode` each emit **two** `RenderedFile`s: the standalone file (`.claude/agents/<agent>.md` tools comma-joined, or `.opencode/agents/<agent>.md` tools as YAML list) plus a root-file section (`CLAUDE.md`/`AGENTS.md`) for `internal/team` to marker-compose; `codex` → `.codex/skills/<agent>/SKILL.md` only (no `model`/`tools`, fails loudly). Pure; empty `Name` → `render.ErrMissingAgentName` |
| install rendered agent artifacts into a target repo, atomically, with provenance | `internal/team` | `Add(root string, renders []AgentRender) (*AddResult, error)` — batch-validate-then-write-all-or-nothing, per-repo flock, no-op short-circuit when source+rendered hashes already match. **Not wired to any CLI command yet** |
| decode/validate the agent team index (teams, shared agents, source/dest) | `internal/team` | `Decode([]byte) (*Index, error)` — strict two-pass JSON, `KeyPattern` for keys/names, faults aggregated |
| compose one agent's section into a shared root file instead of a standalone file | `internal/team`'s `marker.go` | upsert located by agent key alone; an agent shared by multiple teams gets one deduplicated section listing every contributing team |
| run `claude` | `internal/harness` | The only place |
| do one comment's worth of work | `internal/turn` | `ParseCommand` is the front door; `Execute` is everything after |
| decide *when* work happens | `internal/daemon` | `Tick` is an ordinary method, callable straight from a test. It returns synchronously but does **not** wait for the turns it dispatched — use `f.settle()` (one tick plus `Daemon.Quiesce`) in a test that needs to observe what a dispatched turn did |
| add a CLI command | `internal/cli` | One constructor in a `cmd_<group>.go` + one line in `Commands()`. No switch, no mutable registry |
| install/remove the LaunchAgent | `internal/launchd` | LaunchAgent in `gui/$UID` only — never a LaunchDaemon (SD-8) |
| build a logger | `internal/logging` | No package-level logger and no `slog.SetDefault`; it is a struct field passed around |

Before you touch the store:

- **SQLite via `modernc.org/sqlite`** (pure Go, cgo-free — the binary is static),
  driver name `"sqlite"`, not `"sqlite3"`. Pool capped at **one** connection.
  DSN pragmas are correctness, not tuning: `foreign_keys(ON)` (off by default per
  connection), `journal_mode(WAL)` so `litefactory logs` can read while the
  daemon writes, `busy_timeout`, `synchronous(NORMAL)`.
- **The one-connection cap is load-bearing, not incidental**: concurrent
  turn workers plus the daemon's tick goroutine queue for the store rather than
  contend. Raising it is not free throughput — a read-then-write transaction
  upgrading its lock mid-transaction gets `SQLITE_BUSY` immediately rather than
  waiting (`busy_timeout` does not cover a lock upgrade). The cap, `BEGIN
  IMMEDIATE` on every read-then-write transaction, and the DSN pragmas move as
  one decision.
- **Migrations are hand-rolled and embedded** —
  `internal/store/migrations/NNNN_*.sql` via `//go:embed`, version from the
  filename prefix, one transaction each, recorded in `schema_migrations`. A new
  schema change is a new `.sql` file and nothing else — a migration library is
  an explicitly rejected dependency.

NULL mapping in the store is uniform and worth copying: timestamps are
`time.Time` with the zero time meaning NULL (stored as UTC RFC 3339 ms text);
text whose empty value carries no meaning is a plain `string`; forge ids are
`int64` with 0 meaning NULL; and numbers whose zero is a *real* value (an exit
code, a cost) are **pointers**.
