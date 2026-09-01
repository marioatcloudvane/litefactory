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
gittest       →  (nothing)          real package (not _test.go) — git fixtures for tests.

redact        →  secrets
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
  `TestValidationHasNoThirdPartyImports` parses every non-test file in the
  package and fails on any import whose first path segment contains a dot.
  `config.go` is the single exception, and only for `gopkg.in/yaml.v3`.
  `internal/naming` states the same invariant in its `doc.go` and holds it.
- **`internal/report` cannot import `internal/turn`** — `turn` depends on
  `report` to render its replies, so the arrow cannot point back. That is why
  `report.ReplyKind` (`internal/report/reply.go`) is its own enum rather than
  reusing `turn.CommandKind`, and why the mapping is a small switch at the call
  site: `replyKind()` in `internal/turn/report.go` (~line 600). If you add a
  reply shape, you add a `ReplyKind` **and** a case in that switch.
  Note `report` is *not* a leaf — it imports `git`, `harness`, `naming`, `redact`.
- **`internal/harness` is the only place that executes `claude`.** It owns the
  subprocess, the argv, the child environment allowlist, the kill sweep, and it
  writes the run's `NNNN.jsonl` event stream.
  *Path derivation is not.* `naming.RunLogPath` / `naming.RunMetaPath`
  derive the paths and `config.Paths` owns the `$LF_HOME` layout. The sidecar
  `NNNN.meta.json` is written by **`internal/turn`** (`harvest.go`, via
  `harness.WriteMeta`) once the shas and touched files are known.
- **`internal/turn` orchestrates one turn** and has no clock, no ticker, no
  signals. **`internal/daemon` is litefactory's sense of time** — tick loop,
  singleton lock, backoff, cursor, shutdown — and owns no business logic. This
  split is a named invariant (`internal/daemon/doc.go`,
  `internal/turn/doc.go`); it is what makes the whole business path testable
  without waiting for anything.

- **`internal/daemon` importing `internal/repos` is new and deliberately
  cycle-free** — added when the startup worktree-collision check
  (`internal/repos/collision.go`, AC-PAR-4.8) reused the pure decision
  function rather than reimplementing it. `repos` still imports nothing back
  from `daemon`.

- **The issue-body decision is durable, not a single first-sight moment** — a
  refusal of exactly "not assigned to the owner" on a non-empty, comment-free
  body whose parsed command spawns something sets `sessions.body_pending`;
  `Daemon.discover` re-admits that session on every later tick until the flag
  clears, admission-policed against starving comment-driven work
  (`internal/daemon/tick.go`).

### Read the `doc.go` files

Eleven packages carry a `doc.go` whose prose is the real specification for that
package — `daemon`, `git`, `github`, `harness`, `identity`, `launchd`, `naming`,
`redact`, `repos`, `report`, `turn` — and `config` and `store` carry theirs at
the top of `config.go` / `store.go`. **Read the one for the package you are
about to touch before you touch it.** They record what was measured, what was
tried and rejected, and what breaks if you do the obvious thing.

---

## Do not reinvent these

| I need to… | Use | Notes |
|---|---|---|
| resolve `$LF_HOME` or any path under it | `config.Locate()` → `config.Paths` | `ConfigPath DBPath LogsDir RunsDir DaemonLogPath LockPath SettingsPath` |
| derive a slug, branch, worktree path, run log path, claude session id | `internal/naming` | `Ref`, `ParseRef`, `Slug`, `Branch`, `ClaudeSessionID` (UUIDv5, pinned namespace), `WorktreePath`, `RunLogPath`, `RunMetaPath`, `DefaultWorktreeRoot`. All pure; never reads `LF_HOME` |
| run any SQL | `internal/store` | `Open(ctx, path)` then **`Migrate(ctx)` explicitly** — `Open` does not migrate. Accessors are one file per entity |
| run any git command | `internal/git` | `Resolve(ctx)` → `Binary.Open(ctx, dir)` → `Clone` (reads) / `Worktree` (mutates). `EnsureWorktree`, `ParseRemoteURL`, `*CommandError` |
| serialise git ops across concurrent turns on one registered clone | `internal/daemon`'s `cloneGate`, via `turn.Options.CloneGate` | Unexported in `daemon`; `internal/turn` only sees the `turn.CloneGate` interface it declares (`internal/turn/execute.go`). `nil` is refused, never treated as a no-op. The dispatch shape is specified in `internal/daemon/doc.go` |
| refuse two registered repos from sharing one worktree directory | `internal/repos` | `ValidateWorktreeRoots(existing)` at daemon startup, `ValidateNewWorktreeRoot(reg, existing)` at `repo add`, both before any row is written (AC-PAR-4.8) |
| mark/clear/list a session that still owes the daemon a look | `internal/store` | `SetSessionNeedsPoll`, `ClearSessionNeedsPoll`, `ListBacklogSessions` (`internal/store/sessions.go`) over `sessions.needs_poll`. Written at dispatch, cleared only on a fully drained turn — never from a `defer` (T-PAR-12) |
| mark/clear/list a session still owed a re-ask of its issue body | `internal/store` | `SetSessionBodyPending`, `ClearSessionBodyPending`, `ListBodyPendingSessions` (`internal/store/sessions.go`) over `sessions.body_pending`; cleared on a run, a permanent refusal, or issue close — never from inside the gate itself (SD-H) |
| build a git fixture in a test | `internal/gittest` | `New(tb)` gives a clone + local bare origin + tracking, offline. `AssertUnchanged` / `Capture`+`Snapshot.Diff` prove read-only behaviour |
| call the GitHub API | `internal/github` | Concrete `*Client`, seven endpoints, `net/http` only. `RateLimitError.RetryAfter` is what the daemon's backoff reads |
| resolve the bot token | `internal/secrets` | `Resolver{Keychain, LookupEnv}` → `Token`. Resolved **once at process start**, never per request |
| strip credential shapes from outbound text | `internal/redact` | `Redactor.Apply(ctx, text, limit) → Body` |
| build a comment body | `internal/report` | Renders only; reaches no network and no database |
| run `claude` | `internal/harness` | The only place |
| do one comment's worth of work | `internal/turn` | `ParseCommand` is the front door; `Execute` is everything after |
| decide *when* work happens | `internal/daemon` | `Tick` is an ordinary method, callable straight from a test. It returns synchronously but does **not** wait for the turns it dispatched — use `f.settle()` (one tick plus `Daemon.Quiesce`) in a test that needs to observe what a dispatched turn did |
| add a CLI command | `internal/cli` | One constructor in a `cmd_<group>.go` + one line in `Commands()`. No switch, no mutable registry |
| install/remove the LaunchAgent | `internal/launchd` | LaunchAgent in `gui/$UID` only — never a LaunchDaemon (SD-8) |
| build a logger | `internal/logging` | No package-level logger and no `slog.SetDefault`; it is a struct field passed around |

Before you touch the store:

- **SQLite via `modernc.org/sqlite`** (pure Go, cgo-free — the binary is static),
  driver name `"sqlite"`, not `"sqlite3"`. Pool capped at **one** connection.
  DSN pragmas are correctness, not tuning: `foreign_keys(ON)` (SQLite defaults it
  *off*, per connection — without it every `REFERENCES` is decorative),
  `journal_mode(WAL)` so `litefactory logs` can read while the daemon writes,
  `busy_timeout`, `synchronous(NORMAL)`.
- **The cap stays at one, and is load-bearing now rather than incidental**: up
  to `max_concurrent_runs` turn workers plus the daemon's tick goroutine reach
  the store at the same instant in production, so concurrent callers queue
  rather than contend (`store.go`'s doc comment). Raising it is out of scope
  and is not the free throughput it looks like — `database/sql` opens a
  transaction with a deferred `BEGIN`, so a transaction that reads and then
  writes has to upgrade to a write lock partway through, and SQLite refuses
  that upgrade with `SQLITE_BUSY` immediately rather than waiting
  (`busy_timeout` does not apply to an upgrade, since waiting there could
  deadlock). **The single-connection cap, `BEGIN IMMEDIATE` on every
  read-then-write transaction, and per-connection verification of the DSN
  pragmas move as one decision** — any one of the three alone leaves the other
  two wrong.
- **Migrations are hand-rolled and embedded** —
  `internal/store/migrations/NNNN_*.sql` via `//go:embed`, version from the
  filename prefix, one transaction each, recorded in `schema_migrations`. A new
  schema change is a new `.sql` file and nothing else. Adding a migration library
  is an explicitly rejected dependency.

NULL mapping in the store is uniform and worth copying: timestamps are
`time.Time` with the zero time meaning NULL (stored as UTC RFC 3339 ms text, so
lexicographic order is chronological order); text whose empty value carries no
meaning is a plain `string`; forge ids are `int64` with 0 meaning NULL; and
numbers whose zero is a *real* value (an exit code, a cost) are **pointers**.
