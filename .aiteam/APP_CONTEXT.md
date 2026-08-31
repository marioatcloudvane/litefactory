# APP_CONTEXT — litefactory

Read this before writing code. It exists so you find what already exists,
follow the conventions this codebase actually holds itself to, and do not
reinvent a utility that is already three lines away.

`litefactory` (module `github.com/cloudvane/litefactory`, **Go 1.26**) is a macOS
daemon that polls GitHub issues and, when the owner comments, runs a headless
`claude` process inside a git worktree and reports back on the thread.

Single Go module. Not a monorepo. Everything is under `internal/`, with
`cmd/litefactory/main.go` as wiring only (umask, signal context, logger,
dispatch, exit code).

In a hurry? Jump to **[Do not reinvent these](#do-not-reinvent-these)** at the
bottom, then come back for the invariants.

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

### Read the `doc.go` files

Eleven packages carry a `doc.go` whose prose is the real specification for that
package — `daemon`, `git`, `github`, `harness`, `identity`, `launchd`, `naming`,
`redact`, `repos`, `report`, `turn` — and `config` and `store` carry theirs at
the top of `config.go` / `store.go`. **Read the one for the package you are
about to touch before you touch it.** They record what was measured, what was
tried and rejected, and what breaks if you do the obvious thing.

---

## The amendment ledger and the archive rule

- **`.aiteam/AC_AMENDMENTS.md` is where a decision goes when a later feature
  changes it.** A reader asking "what happened to AC-3.1?" or "what happened to
  D-X?" has exactly one place to look — not the plan that first stated it, not
  this file, that ledger. It outgrew being acceptance-criteria-only and now
  admits four kinds of entry: **amendments** (a criterion's wording or meaning
  changed), **rulings** (a `D-*` decision or an invariant interpreted before
  work could proceed — the original text stands, this file says what it was
  held to mean and why), **reviews** (examined, deliberately left alone, with
  the reason it survives written down), and **deferrals** (considered, not
  done, not rejected — their own section at the bottom). An entry that
  *settles* a question rather than *amending* a criterion carries an *Applies
  to* line instead of "used to say"/"now says". Several invariants stated
  elsewhere in this file, and in archived plans, are amended there — check the
  ledger before relying on a criterion or decision from an old plan.
- **Archives are never edited into agreement with the present.** An old plan
  keeps its original text even once later work amends, reverses or rules on
  something it says — an edited archive is no longer evidence of what was
  actually decided at the time. The correction goes in the ledger; the archived
  document gets a one-line *pointer* added under the table or statement it
  concerns, and nothing else changes. This is counter-intuitive and easy to
  violate with good intentions ("just fix the stale sentence while I'm here") —
  don't. `lf-setup/IMPLEMENTATION_PLAN.md`'s AC-2 table, AC-3 table, Settled
  Decisions table and its goroutine-inventory statement carry these pointers,
  as does `comment-intent/IMPLEMENTATION_PLAN.superseded.md`.

---

## Security invariants — enforced by tests, not by convention

These are not style rules. Each has a test or a type that fails if you break it.

- **`--dangerously-skip-permissions` is never constructed anywhere in the tree.**
  `config.forbiddenFlag()` in `internal/config/policy.go` assembles it from two
  fragments precisely so the whole string is not a literal anywhere, which keeps
  a repository-wide grep a reliable review invariant.
  `TestForbiddenFlagIsAbsentFromTheTree` (`purity_test.go`) walks the whole repo
  and fails on any `.go` file containing it. Separately,
  `validateNoForbiddenFlag` refuses any *configured* value that smuggles it in
  through `claude.binary`, `settings_path`, or any tool list.
- **Untrusted input goes on the child's stdin, never argv** (AC-3.12).
  `harness.Request.Prompt` is piped in. This removes the ~1 MB macOS `ARG_MAX`
  ceiling and keeps third-party text out of every `ps` listing.
- **`--tools` is the real tool boundary; `--allowedTools` is not.** Measured:
  `--allowedTools` is *additive* to the operator's own `~/.claude/settings.json`
  — 250 ambient allow-rules leaked through and `Bash` executed while absent from
  the list. `--tools` is what the init event's `.tools` array reflects.
  **An omitted `--tools` means allow-all**, which is why an empty list is a
  validation error rather than "no tools": see `validateTools` in
  `internal/config/policy.go` and the `New()` guard in
  `internal/harness/runner.go`.
- **The claude child's environment is an allowlist, never `os.Environ`.**
  `internal/harness/env.go`: `PATH HOME USER SHELL LANG LC_ALL TERM TMPDIR`, and
  nothing else. Every entry has a documented reason (`USER` is required or the
  Keychain lookup silently fails with "Not logged in"). `SSH_AUTH_SOCK` and
  `CLAUDE_CONFIG_DIR` are deliberately absent. `AllowedEnvNames()` is exported
  so a diagnostic can print the boundary.
- **`internal/report` re-applies input restrictions locally rather than trusting
  upstream validation.** `internal/report/reply.go` re-declares
  `profileNamePattern` (rather than importing it) and `commandSpan` re-restricts
  the command token, because this is the one place attacker-controlled text is
  echoed into a comment litefactory signs its own name to. `commandSpan` *stops*
  at the first disallowed rune rather than skipping it, so unexpected input can
  only ever produce a shorter, plainer token. Two of the three tool refusals
  carry no field at all for label text to travel in — that is the mechanism, not
  a habit.
- **Redaction is hygiene, not a boundary** (`internal/redact/doc.go`). The
  boundary is not giving the subprocess the secret. The type system enforces the
  ordering: renderers return `redact.Body`, which only `redact.Redactor.Apply`
  can construct and `github.PostComment` is the only consumer of. There is no
  exported way to get the text back out. Outbound bodies are redacted; the local
  `$LF_HOME/logs` copy deliberately is not.
- **litefactory never writes inside the registered clone.** `git.Clone`'s method
  set *is* the S-6 allowlist (`Fetch WorktreeAdd WorktreeList WorktreeRemove
  RevParse RemoteURL ConfigGet ShowRef` + `Path`). There is no `Commit`,
  `Checkout`, `Reset`, `Merge` or `Pull`, so mutating a registered clone is a
  compile error. `internal/git/surface_test.go` pins the method set both
  positively and negatively.
- **Identity is numeric, never a handle** (`internal/github/doc.go`, D-I).
  Logins are renameable and a fine-grained PAT on a machine user reports type
  `"User"`. Compare ids.
- **`gh` is identity discovery only**, reached only by `litefactory init`, and
  its child environment strips `GITHUB_TOKEN`/`GH_TOKEN` (`internal/identity`) —
  otherwise `init` would record the bot as the owner and the daemon would read
  its own comments as instructions.
- `syscall.Umask(0o077)` is the **first statement of `main`**. Nothing in
  litefactory calls `Chmod`; that one line is why it does not have to.
- **The only `recover()` in the tree** is in `redact.Redactor.Apply`
  (`internal/redact/redact.go`): a panic mid-redaction becomes an error so the
  post aborts and the watermark does not advance (AC-4.9). Do not add a second.

### The tests that enforce the above

If one of these fails, you changed a design decision — go and make it
deliberately rather than editing the test.

| Test | What it pins |
|---|---|
| `config/purity_test.go` | `internal/config` is stdlib-only bar the YAML decode; the skip-permissions flag appears nowhere in the tree |
| `git/surface_test.go` | `Clone`'s method set *is* the S-6 allowlist; `Worktree` has no exported construction path; zero values refuse to exec git |
| `git/construction_internal_test.go` | `Worktree` is built in exactly one place |
| `git/exec_internal_test.go` | every git command carries a deadline; the child env is an allowlist; the launchd PATH fallback |
| `github/surface_test.go` | `PostComment` takes `redact.Body`, never `string`; exactly seven endpoints; **no interface type is declared in the package** (no premature Forge abstraction); `Token.Reveal()` has exactly one call site, on the `Authorization` line |
| `identity/policy_test.go` | only `cli/cmd_init.go` may import `internal/identity`; the literal `"gh"` appears nowhere else outside `store/identities.go` |
| `gittest/policy_test.go` | no non-test file imports `internal/gittest` |
| `repos/network_test.go` | registration makes no network call — `repo add` must work on a plane |
| `config/template_test.go` | the shipped `config.yaml` template decodes back to `Defaults()` |
| `store/store_test.go` | FK enforcement, migrate idempotency, `AdvanceWatermark` never rewinds, `AllocateRun` refuses a replay, `CompleteRunReport` is one transaction |
| `daemon/clock_test.go` | only `Daemon.Tick` (never a helper, never a function literal) reads the daemon's clock |
| `turn/gate_test.go` | `prepareClone`, `commitAndPush` and `commitStoryIndex` are the only three call sites of `CloneGate.Acquire`, one acquisition each (a registered-set check, not three hardcoded names); the claude run itself is never inside a critical section |
| `daemon/gate_internal_test.go`, `daemon/pool_internal_test.go`, `daemon/inflight_internal_test.go` | the gate/pool/inflight mechanics in isolation — mutual exclusion per path, semaphore refuses over `max_concurrent_runs`, `enter`/`leave` are a true check-and-insert |
| `repos/collision_test.go` | two registered repos may not resolve the same worktree directory |
| `daemon/record_test.go` (`TestNoStaleConcurrencyStatementSurvives`) | no `.go` file outside `.aiteam` says "locked by another daemon instance" — a sibling goroutine within one process is the real contender now, not a second daemon (AC-PAR-8.4) |
| `daemon/record_test.go` (`TestIntroducedCriteriaCarryTheParPrefix`) | every acceptance criterion this feature's `IMPLEMENTATION_PLAN.md`/`feature-spec.md` introduce carries the `AC-PAR-` prefix — parsed from the plan's AC tables and the spec's checklist, not grepped for the bare pattern (AC-PAR-8.7) |

---

## Config conventions (`internal/config`)

- **Strict decoding.** `yaml.Decoder.KnownFields(true)` — an unknown or
  misspelled key is a startup error, not a silently ignored line (AC-0.10).
  A second YAML document is also refused. `friendlyYAMLError` rewrites yaml.v3's
  Go-type phrasing into `unknown key "…"`.
- **Defaults are applied by decoding on top of `Defaults()`**, so an explicitly
  written zero (`poll_interval_seconds: 0`) is distinguishable from an absent key
  and is rejected rather than silently replaced.
- **The shipped template cannot drift from `Defaults()`.**
  `internal/config/template.go` holds a commented `config.yaml` as a format
  string; `DefaultFileContents()` interpolates the values *from* `Defaults()`,
  and `TestDefaultFileMatchesDefaults` decodes the generated file and compares it
  against `Defaults()`. If you add a config key, add it to `Defaults()`, to the
  template, and expect that test to tell you if you forgot.
  (The template deliberately writes `tool_ceiling` and `tool_profiles` only as
  commented examples — both are optional and absent on a fresh install.)
- **config.yaml never contains secrets**, only references to them (a keychain
  service/account, or the *name* of an env var). `secretscan.go` reflects over
  every string in the decoded struct and matches the S-7 credential shapes; a hit
  is a hard startup error unwrapping to `ErrLiteralSecret`, which the command
  layer maps to exit 3. Matched text never appears in the error — only the key
  and the class.
- **Validation aggregates.** `Config.Validate()` collects every problem and
  returns `errors.Join(errs...)`. Reporting one problem per run would mean fixing
  four typos in four restarts. Follow this when you add validation.
- **Out of range is an error, never a silent clamp** (`poll_interval_seconds`
  10–60, `claude.timeout_seconds` 60–86400, `worktree_retain_days` 1–3650).
- **Never `exec.LookPath`.** launchd hands a LaunchAgent
  `PATH=/usr/bin:/bin:/usr/sbin:/sbin`, which has no Homebrew.
  `config.ResolveExecutable` searches an explicitly passed PATH and its error
  names every directory it looked in.
- **Nothing outside `internal/config` may join a path against `$LF_HOME`.**
  Add a method to `config.Paths` instead (`ConfigPath DBPath LogsDir RunsDir
  DaemonLogPath LockPath SettingsPath`).

---

## Testing conventions

- **Table-driven, stdlib `testing`, `t.Parallel()` where safe.** `go-cmp` is the
  only assertion helper and is used sparingly — 7 test files (`config`, `redact`,
  `launchd`, `repos`). Everything else compares by hand with a good failure
  message.
- **No mocking framework, no new module.** There is no testify, no gomock, no
  mockery, and adding one is not on the table. Fakes are hand-written structs.
  (`go.mod` has exactly three direct requires: `go-cmp`, `yaml.v3`,
  `modernc.org/sqlite`. Keep it that way.)
- **`fixture_test.go` is the pattern for a package-wide fake cast.**
  `internal/turn/fixture_test.go` (in-package, `package turn`) builds a real git
  clone with a real origin, a migrated database and a configurable fake claude.
  `internal/daemon/fixture_test.go` (`package daemon_test`) builds an
  `httptest` GitHub, a real store and a fake executor. Extend one of these two
  rather than standing up a third way of faking a turn.
- **Naming: `*_internal_test.go` means white-box.** In packages that are
  otherwise black-box tested (`daemon`, `git`, `github`), a file named
  `*_internal_test.go` is `package X` and everything else is `package X_test`.
  Packages tested in-package throughout (`turn`, `harness`, `config`, `cli`,
  `naming`, `logging`, `secrets`) do not use the suffix.
- **Interfaces are declared at the point of consumption.** `harness` exports a
  concrete `*Runner`; `turn` declares the one-method `turn.Runner` interface it
  needs. `report` exports a concrete `*Reporter`; `turn` declares `Renderer` and
  `Poster`. Do not add an interface next to its implementation.
- **A promise about a transaction is tested against real SQLite**, not a fake
  store — see the comment on `turn.ReporterOptions.DB`. A fake would let a test
  assert a guarantee the implementation does not keep.
- **Determinism matters; Go map ordering has bitten this codebase.** Sort before
  iterating a map or before emitting a list an assertion will read:
  `sortedProfileNames` in `internal/config/policy.go`, the `reflect.Map` branch
  in `internal/config/secretscan.go`, and also `harness/env.go`,
  `github/comments.go`, `launchd/plist.go`, `store/migrate.go`,
  `redact/redact.go`, `cli/cmd_logs_render.go`.
- **Some tests exist to fail on a design change**, not on a bug — the
  `surface_test.go` / `policy_test.go` / `purity_test.go` family listed above,
  joined by `internal/daemon/clock_test.go` (parses the AST to confirm only
  `Daemon.Tick` reads the injected clock, and that no function literal reads it
  either) and the critical-section check in `internal/turn/gate_test.go`
  (`TestCriticalSectionsAreSingleAcquisitions`). This one is no longer two
  hardcoded names — it is two registries, `sections` (function name → the file
  it must be declared in: `prepareClone`, `commitAndPush`, `commitStoryIndex`)
  and `gated` (call name → the set of sections it may appear in: `fetch`,
  `freezeSlug`, `ensureWorktree` in `prepareClone`; `Commit`, `Push` in
  `commitAndPush` **or** `commitStoryIndex`), plus a package-wide check that the
  total `Acquire` call count equals the number of registered sections. That
  combination is what still catches a new ungated git write section on its own
  name, which is how `commitStoryIndex` itself arrived on a merge from main.
  Parsing the package's own AST with `go/ast` to pin who is allowed to call what
  is an established shape for this kind of test in this codebase, not a
  one-off. Adding an exported method, an interface, an import, or a new call
  site — or a new critical section — can fail the build. That is the feature.

---

## Style

- **Comments explain *why*, and are expected to be true.** This codebase holds an
  unusually high standard here: comments cite decision and acceptance-criterion
  identifiers (`D-6`, `AC-3.12`, `SD-2`, `S-7`, `C9`, `P-3`), record what was
  *measured* rather than assumed, and say what would break if the code were
  changed the obvious way. **When your change makes a comment untrue, updating it
  is part of the change.** Match the register — a one-line restatement of the
  code is worse than nothing here.
- **Copy slices at struct boundaries**, with `append([]string(nil), …)` (11 uses
  across `harness/runner.go`, `harness/meta.go`, `turn/execute.go`,
  `turn/report.go`, `daemon/daemon.go`, `daemon/tools.go`, `daemon/tick.go`) or
  `slices.Clone` / `maps.Clone`. The reason was originally sequential: a request
  struct is mutated in place on the cold-rerun path, and one `append` into spare
  capacity would rewrite the operator's configuration for every later run in the
  process. **Under concurrent turns this stops being a defence against a
  sequential bug and becomes load-bearing for isolation between live sibling
  turns** — two turn workers sharing one process now legitimately hold requests
  built from the same configuration at the same instant, and a missed copy at
  one of these sites would be a data race, not just a stale value. Do not remove
  one of these on the grounds that nothing looked like it was reading the slice
  again.
- **Goroutine budgets are documented and deliberately tiny.** The inventory is
  four fixed goroutines — `harness/runner.go` (exactly two, one per pipe,
  neither able to outlive `Run`), `daemon/daemon.go` (one, the loop), and
  `cmd/litefactory/main.go` (one, the second-signal watcher) — plus **at most
  `max_concurrent_runs` turn workers**, dispatched from `Daemon.offer`
  (`internal/daemon/tick.go`) via `pool.try` (`internal/daemon/pool.go`). That
  setting is validated to `MinConcurrentRuns..MaxConcurrentRuns` (1..16) and is
  **refused, never clamped**, outside that range, so the hard ceiling on the
  whole process is twenty goroutines. **This is now load-bearing, not aspirational:**
  `Tick` (`internal/daemon/tick.go`) dispatches turns onto real goroutines and
  returns without waiting for them — see "Concurrent turns" below for the shape.
  The one named termination condition for the worker pool is `Daemon.Quiesce`
  (`internal/daemon/pool.go`) — not a `select` on shutdown, and not a sleep.
  **The two documents are reconciled.** The one canonical text is the goroutine
  inventory entry in `.aiteam/AC_AMENDMENTS.md`;
  `lf-setup/IMPLEMENTATION_PLAN.md:60` keeps its original sentence and carries a
  pointer to it. That sentence's `logs -f` tailer was the error — there is no
  such goroutine, `litefactory logs -f` re-reads the run's JSONL synchronously on
  the command's own goroutine — and the fourth fixed one is `main`'s
  second-signal watcher, which this file had right all along. If you add a
  goroutine, say in a comment what bounds its lifetime.
- **A bad outcome is a value, not an error.** Across `turn`, `harness` and
  `daemon`, an operation that *happened and went badly* returns a result whose
  field says how; the `error` return is reserved for "could not be conducted or
  recorded at all". Silence on the issue is the one outcome that is forbidden
  (AC-4.1).
- **Post first, then commit.** Every reporting path renders, posts, and only then
  writes to the database in a single transaction. Advancing a watermark before a
  successful post turns a transient 502 into permanent silence.
- Lint rules worth knowing (`.golangci.yml`): `context.Background()` is forbidden
  outside `main` (take the context from the caller); `fmt.Print*` and `log.*` are
  forbidden (use the injected `*slog.Logger`, or the command's output writer);
  `errcheck` checks type assertions; `gosec`, `errorlint`, `nilerr`, `noctx`,
  `unparam` are on. `govet` is `enable-all` minus `fieldalignment` and `shadow`,
  and the config explains why for both.
- Every store accessor and every git method takes `ctx` first.

### Recurring shapes you should match

- **`Options` struct + `New(opts) (*T, error)`**, validating every dependency at
  wiring time rather than at first use. The stated reason recurs verbatim: the
  first render happens when a run has just finished, which is the worst possible
  moment to discover the reporter cannot produce a body.
- **Function fields as the injection seam**, where a constructor would be
  overkill: `secrets.Resolver{Keychain, LookupEnv}` has no `NewResolver`;
  `launchd.Manager` takes a `Runner`. Tests substitute the field.
- **Typed errors carrying fields, unwrapping to a sentinel.**
  `*git.CommandError` exposes the command and stderr as fields so a caller can
  print them verbatim; `*github.RateLimitError.RetryAfter` is read by the
  daemon's backoff; `*config.SecretError` carries the key and class but never the
  value. Prefer this to formatting a string a caller has to re-parse.
- **No retries, no sleeps, below the daemon.** `internal/github` retries nothing
  and sleeps nowhere — backoff is `internal/daemon`'s job. Keep it that way.
- **CLI handlers never read the flag set back.** `Command.New(fs)` binds flags
  and returns a *closure* capturing the values; the handler receives only
  arity-checked positionals plus `*cli.Env`. Exit code comes from the error
  kind: `*UsageError` (bad command line) vs `*PreconditionError` (machine not
  ready) vs plain error.

---

## Where durable external-behaviour findings live

`claude` and `git` are external binaries whose behaviour this codebase depends
on. The convention is: **probe read-only, in a scratch directory outside the
repo, and write it up in a document — do not commit a test that spawns a real
`claude`.** A test like that would be slow, quota-consuming and flaky, and the
repo has no build-tag or env-var gate for it.

The two documents, both stating the exact `claude` version they were verified
against and how to re-verify after an upgrade:

- `.aiteam/main/2026-08-25/session-resume/CLAUDE_CLI_CONTRACT.md` — the
  `--session-id` / `--resume` contract (verified against `claude 2.1.246`).
  Reusing `--session-id` exits 1 with no stdout; `--resume` fails *closed* while
  `--continue` fails *open*, which is why `--continue` is never constructed; the
  tool boundary survives a resume.
- `.aiteam/main/2026-08-26/tool-permissions/TOOL_SURFACE_PROBE.md` — that an
  operator-authored profile actually widens the *resolved* surface of a real
  `claude` process (verified against `claude 2.1.247`).

If you change how litefactory invokes `claude`, add to these rather than
starting a third convention.

---

## The feature most recently built: tool profiles selected by a label

A GitHub label of the exact form **`lf:tools=<profile>`** on an issue selects an
operator-authored tool profile from `$LF_HOME/config.yaml`, and that profile
becomes the `--tools` / `--allowedTools` surface of the `claude` child for runs
that issue triggers. With no profiles configured the feature is inert and every
run uses `claude.tools` exactly as before.

Key files:

| File | Role |
|---|---|
| `internal/config/policy.go` | profile/ceiling/tool-list validation, `sortedProfileNames`, `forbiddenFlag` |
| `internal/config/config.go` | `Claude.ToolCeiling`, `Claude.ToolProfiles`, `ToolProfile` |
| `internal/daemon/tools.go` | `resolveTools(labels)` → `toolDecision` |
| `internal/daemon/tick.go` | resolution point (`Daemon.run`) and `Daemon.refuseTools` |
| `internal/harness/runner.go` | `Runner.surface`, `Request.validate` ceiling re-check, `args` |
| `internal/report/reply.go` | the three refusal replies |
| `internal/report/run.go` | requested-vs-resolved tool surface in the run report |

Things that are easy to get wrong:

- **`claude.tool_ceiling`, not the label, is the security boundary.** Anyone who
  can label an issue can select any profile, so a profile is only ever as safe as
  the ceiling. The ceiling is resolved **once**, in `config.Load`, before
  validation (`Claude.resolveToolCeiling`), and defaults to a *copy* of
  `claude.tools` — the narrowest default there is. The harness reads it as given
  and never re-derives it. `harness.Request.validate` re-checks `Tools` against
  the ceiling immediately before argv is built.
- **Detected leniently, accepted strictly (D-6).** The prefix is matched
  case-insensitively so `LF:Tools=` is *seen*, then refused because it is not
  byte-exact lowercase. Seeing-and-refusing turns a typo into a loud refusal
  instead of a silently ignored permission request.
- **Three refusal kinds, not four:** `ReplyUnknownToolProfile`,
  `ReplyMalformedToolLabel`, `ReplyAmbiguousToolLabels`. "No profiles configured
  at all" takes the *unknown* path — from the thread's point of view it is the
  same answer.
- **Only the unknown-profile refusal names anything.** The malformed and
  ambiguous refusals leave `Profile` empty; there is deliberately no field for
  label text to travel in.
- **A refusal on the issue-body path posts nothing.** When the trigger comment id
  is `0` (the issue body run as the first instruction) there is no comment to
  answer, so `refuseTools` refuses the run, logs it, and posts no comment. The
  next real comment on the issue is refused loudly on the thread. This is the
  only branch to change if that trade is revisited.
- **Ambiguity is never resolved by picking or unioning.** Two labels are two
  permission requests; the count is taken before any of them is judged.
- **The label is only a lookup key.** Every string that reaches argv is
  operator-authored configuration. `ToolProfile` (a validated name) is used for
  logging and metadata only and is never joined into argv. `turn.Issue.Labels`
  documents that it is one hop from `harness.PromptInput` and that no label text
  may be rendered into a prompt.
- **An unprofiled run's argv must stay byte-identical** to what it was before
  this feature existed (AC-2.3). `Runner.surface` discriminates on
  `len(req.Tools) > 0`, not on `ToolProfile`, because `Tools` is what argv is
  built from.

---

## Concurrent turns

**The daemon runs turns in parallel now.** `Daemon.Tick` (`internal/daemon/tick.go`)
splits into a synchronous **discovery** phase and an asynchronous **dispatch**
phase, both still on the Tick goroutine, followed by a worker goroutine per
dispatched session. `Tick` returns without waiting for the turns it dispatched
— a thirty-minute run on one issue no longer stops another being noticed thirty
seconds later. Production call sites now exist for everything the previous
version of this section warned had none: `Daemon.offer` calls
`d.inflight.enter(...)` then `d.pool.try()` before spawning the worker, and
`Daemon.discover`/`Daemon.backlog` call `store.ListBacklogSessions`. Read
`internal/daemon/doc.go` before touching any of this — it is the specification,
not this summary.

What changed structurally:

- **The poll cursor is a pure monotonic watermark.** The old hold-back to the
  oldest unfinished issue is gone — it used to re-list that issue every tick,
  which rewound discovery for every other repository at the same time.
  Re-discovery of outstanding work now comes from `sessions.needs_poll`, unioned
  into discovery alongside the forge listings. A backlogged session's
  `github.Issue` is fetched lazily, in `Daemon.materialise`, only once the
  dispatcher has already taken a slot for it — which is what bounds the extra
  cost at `max_concurrent_runs` issue reads per tick, however deep the backlog.
- **`TickResult` was reshaped, not synchronised.** `Bootstrapped`, `Comments`,
  `Runs` and `Replies` are gone — they described what the *work* did, and the
  work now happens on goroutines that outlive the tick, so counting it would
  need a mutex guarding a snapshot of an unfinished computation. `Dispatched`
  (a rename of `Runs`) and `InFlight` are new and describe the *decision*,
  which is entirely the Tick goroutine's own. `Deferred` now means "eligible
  sessions that found no free slot **or** were held back by a mid-tick backoff
  arming" — not "no session, this tick did nothing" (that is `Skipped`). No
  `TickResult` field is written by more than one goroutine; that is deliberate
  and is what makes it `-race`-checkable rather than mutex-guarded.
- **The in-flight set, not the store's session lock, is what serialises one
  session's turns within a process.** `d.inflight.enter` is a check-and-insert
  on the Tick goroutine at dispatch (`Daemon.offer`); `d.inflight.leave` runs
  after the report has posted — in the worker's deferred call, which fires
  after `d.work` returns — which is wider than `turn.Execute` alone. The
  watermark for a comment advances inside `CompleteRunReport`, so releasing at
  `Execute`'s return (rather than after the report) would leave the session
  momentarily unlocked and unconsumed, which is exactly the state discovery
  reads as eligible. `sessions.lock_owner` in the store is unchanged and is now
  **defence in depth**: within one process the in-flight set already prevents
  two sibling workers from both reaching `AcquireSessionLock`, so
  `DispositionLocked` is reachable only from a second daemon *process*.
- **Dispatch is a two-level round-robin over a materialised eligible set.**
  Repositories rotate against each other; sessions rotate within a repository.
  Queues are sorted by repository id and the rotation resumes at the first id
  greater than the last dispatched, wrapping — a genuine cyclic successor
  (fixed by CR-A-003; the earlier version fell back to index 0 when the
  last-dispatched repository had dropped out of the eligible set, over-serving
  the lowest id). With S free slots and R repositories holding work: S ≥ R
  gives every repository ⌊S/R⌋ or ⌈S/R⌉ slots in the same tick; S < R serves
  every repository within ⌈R/S⌉ ticks. `internal/daemon/doc.go` states the
  policy by name and the four things it explicitly does **not** promise
  (not proportional, not preemptive, not a priority system, not fair over an
  individual session's queued comments) — read there rather than re-deriving
  it.
- **The rate-limit backoff is checked at two points, never one:** the top of
  `Tick`, and again in `Daemon.offer`, as the last statement before the `go`
  — because a worker's 429 can arm the hold (via `Daemon.rateLimited`) while
  the dispatch loop is still running, and a tick that only checked at the top
  would keep starting runs into a forge that is refusing them. The instant
  compared is `tickAt`, never `d.now()` — nothing below `Daemon.Tick` may read
  the clock (AC-PAR-7.2; `TestClockIsReadOnlyFromTick` fails the build if it
  does). The backoff owns no `CancelFunc` and touches no worker's context: it
  gates the dispatch of *new* runs only, and turns already in flight run to
  their end untouched (AC-PAR-4.3).

What already exists and must not be reinvented:

| File | Role |
|---|---|
| `internal/daemon/gate.go` | `cloneGate` — per-clone git serialisation, keyed on `store.Repo.Path`. A 1-buffered channel per clone, acquired with `select` on `ctx.Done()` — **never** a `sync.Mutex`, because `Lock` is uninterruptible and would blow `Daemon.Quiesce`'s shutdown grace budget. Consumed only through the `turn.CloneGate` interface declared on `turn.Options`; `internal/git` declares nothing and knows nothing about it |
| `internal/daemon/pool.go` | `pool` — a buffered-channel semaphore plus a `sync.WaitGroup`, sized from `config.MaxConcurrentRuns`. `try()` is non-blocking, called only from `Daemon.offer` on the Tick goroutine, and is now load-bearing: every dispatched turn runs on a worker goroutine this claims. Exports `Daemon.Quiesce(ctx)` — the **only** exported API this feature adds. Production shutdown goes through the unconditional `pool.wait()`, never through `quiesce`/`Quiesce` — see the doc comment on `pool.wait` for why a `select` on `ctx.Done()` there would abandon every worker |
| `internal/daemon/inflight.go` | `inflight` — the in-memory per-session mutual-exclusion set for one daemon *process* ("not this session, not now"). Distinct from and wider than `sessions.lock_owner` in the store — see above |
| `internal/store/migrations/0002_session_backlog.sql` | `sessions.needs_poll` (partial index on `= 1`), plus `store.SetSessionNeedsPoll` / `ClearSessionNeedsPoll` / `ListBacklogSessions`. Set for every session dispatch takes off the eligible set — dispatched **or** deferred for want of a slot (`Daemon.markBacklog`, called from both `Daemon.offer` and `Daemon.deferRest`) — cleared only by a fully drained, error-free turn, as a statement, never a `defer` (a `defer` would clear it on error paths too, which is the stale-*false* the design forbids) |
| `config.MaxConcurrentRuns` | `max_concurrent_runs` in `config.yaml`, default `config.DefaultMaxConcurrentRuns` (3 — a conservative placeholder, not a measurement; sizing probe P5 was never run — recorded in `internal/config/config.go`'s struct doc comment and in the shipped template comment in `internal/config/template.go`, the two durable in-tree homes of that provenance), range `config.MinMaxConcurrentRuns`..`config.MaxMaxConcurrentRuns` (1..16), refused outside it rather than clamped. Re-validated and re-floored in `daemon.Options.validate` and `pool.newPool` — refused/floored at three layers total |
| `internal/repos/collision.go` | `ValidateWorktreeRoots` / `ValidateNewWorktreeRoot` — refuses two registered repos from mapping one issue number to one worktree directory, since a worktree path carries the issue number but not the repository. Enforced at daemon startup and at `repo add`, before any row is written (AC-PAR-4.8) |

Things that are easy to get wrong here:

- **The clone gate protects three critical sections, not two.** CS1 is
  `fetch` → `freezeSlug` → `ensureWorktree`, taken as one acquisition by
  `prepareClone` in `internal/turn/prepare.go`. CS2 is `Commit` → `Push`, taken
  by `commitAndPush` in `internal/turn/harvest.go`. **CS3** is `Commit` → `Push`
  for the plan's Story Index rewrite, taken by `commitStoryIndex` in
  `internal/turn/subissues.go` — added on a merge from main that brought in
  `internal/turn/subissues.go` with an ungated third git write section, caught
  by `TestCriticalSectionsAreSingleAcquisitions`. CS3 cannot be folded into
  CS2: the `CreateSubIssues` forge round-trip sits between the run's own push
  (CS2) and the Story Index commit (CS3), and cannot be moved out from between
  them in either direction — sub-issues may only be filed once CS2's commit is
  on the remote for their detail links to resolve against, and the rewrite CS3
  publishes is made of the issue numbers that forge call returns. One
  acquisition spanning both would hold the clone across an unbounded network
  call, closing for every sibling turn exactly the gap AC-PAR-3.2 exists to
  keep open. The `os.WriteFile` of `plan_brief.md` and the `CreateSubIssues`
  call both stay **outside** the gate, for the same reason. The `claude` run
  itself deliberately sits outside all three sections — that gap is the entire
  source of surviving parallelism (AC-PAR-3.2), and
  `internal/turn/gate_test.go` (`TestClaudeRunsOutsideTheCloneGate`) checks it
  directly. Do not widen any of the three "to be safe".
- **`TestCriticalSectionsAreSingleAcquisitions` is a registry, not a hardcoded
  pair.** It is `sections` (function name → the file it must be declared in)
  and `gated` (call name → the set of sections it may appear in), plus a
  package-wide check that total `Acquire` calls equal the number of registered
  sections. A fourth, ungated git write section fails on its own name, the way
  `commitStoryIndex` itself would have.
- **A nil `CloneGate` is a startup error, not a degraded no-op**
  (`turn.Options.validate`). The measured failure rates it prevents, at
  parallelism 4 against one clone — 75% on a contended `fetch`, 0.75% on
  `worktree add`, and `push --set-upstream` leaving 25% of branches with no
  upstream set while exiting 0 (a `could not lock config file` error printed in
  37% of runs, more often than the loss itself — the gap between the two counts
  was never chased; see `internal/git/clone.go`'s doc comment for the full
  writeup) — would look healthy for a long time under a silent no-op and then
  start posting fatal git errors that read like flakes. `internal/git`
  declares no mutex and stays a leaf: `Fetch`, `WorktreeAdd` and `Push` all
  contend against one clone's `.git` state (refs/remotes, the worktrees
  registry, `.git/config`), and serialising them across concurrent turns is
  the caller's job — done here by the per-clone `cloneGate`, never by
  `internal/git` itself.
- **`pool.wait()` takes no context and must not be given one.** It is called
  from `loop`'s defer, at the moment the loop's own context is *already*
  cancelled — that is why `loop` is returning. A `select` on `ctx.Done()` there
  would return instantly and abandon every in-flight worker while still reading
  like a join. `Daemon.Quiesce` (which does take a context) is the caller-facing
  version; production shutdown never goes through it.
- **`pool.try`'s contract:** every `true` return is owed exactly one
  `pool.done()`. The normal discharge is the worker's first deferred call
  (first, so it runs last, after every other defer the worker registers). The
  only other permitted discharge is an immediate undo on the same goroutine
  when it turns out no worker is started at all — `Daemon.offer` does both: it
  takes a slot, then reads the issue and re-checks the backoff, and calls
  `d.pool.done()` immediately on the path where the issue read fails, the
  issue turns out closed, or the backoff armed mid-tick. Do not repeat the
  older claim that a `true` return which never reaches a `go` silently leaks a
  slot forever — that branch is now covered.
- **A standing rate-limit backoff cannot self-clear with zero repos
  registered** (CR-A-002, `tick.go` `poll` / `backoff.go` `clear`) — assessed as
  benign (no repos means no traffic the backoff protects), documented rather
  than fixed. Still open, still accepted.
- **`internal/daemon/clock_test.go`** pins that only `Daemon.Tick` reads the
  injected clock — not a helper, not a function literal — because a `d.now()`
  read from inside a dispatched worker would make the number of clock readings
  depend on dispatch order instead of being deterministic. Everything below
  `Tick`, including `Daemon.offer` and every worker, is handed `tickAt`
  instead.
- **Goroutine inventory is now four fixed plus at most `max_concurrent_runs`
  turn workers** (hard ceiling 20) — see "Style" above. `Daemon.Quiesce` is the
  single named termination condition for the pool.
- **The daemon's mutable state is: the backoff, the in-flight set, the pool,
  and the dispatch rotation cursors** (last-dispatched repository id, and
  last-dispatched session id per repository). The cursors live on `*Daemon`
  because they must survive between ticks, and deliberately have no mutex —
  only `Daemon.dispatch`, on the Tick goroutine, ever touches them.

### Tests in `internal/daemon` and `internal/turn` worth knowing about

New files: `internal/daemon/dispatch_test.go`, `concurrency_test.go`,
`crossrepo_test.go`, `fairness_test.go`, `refill_test.go`, `sequential_test.go`,
and `internal/turn/concurrent_execute_test.go`.

- `f.settle()` (`internal/daemon/fixture_test.go`) is **one tick plus
  `Daemon.Quiesce`** — the primitive a test uses whenever it needs to observe
  what a dispatched turn did, not just that `Tick` returned.
- `f.plantCursor()` is the old "settle" — the first tick of a daemon's life,
  expected to find nothing, that initialises the poll cursor to "now" (AC-2.9)
  so a pre-existing issue is not listed. A future engineer reaching for
  "the cursor-planting helper" by its old name will find the wrong function.
- `f.turns.triggers()` is sorted — a set assertion across sessions, since
  dispatch order across sessions is the scheduler's business.
  `f.turns.triggersFor(sessionID)` preserves call order — within one session,
  order *is* the invariant.
- **House rule: no `time.Sleep`, no `Eventually`, no wall-clock polling** in
  this package's concurrency tests. Rendezvous on channels, wait with
  `Quiesce`, and bound every rendezvous with a timeout that fails with a
  message naming what was waited for.
- **Durable lesson (cost a flaky test): no observable in this package means
  "a worker has released its slot".** The forge post, the watermark write and
  the "turn complete" log line all happen strictly *before* the worker's
  deferred `inflight.leave` and `pool.done()` (see `Daemon.offer`'s dispatch
  goroutine: `defer d.pool.done()` then `defer d.inflight.leave(...)` then
  `d.work(ctx, t)` — the pool release fires last because it is registered
  first). A test that needs a freed slot must retry over real ticks, which is
  also faithful to the design: dispatch has no queue, so a freed slot is taken
  by whichever tick runs next, not by whoever posted first.

---

## Do not reinvent these

| I need to… | Use | Notes |
|---|---|---|
| resolve `$LF_HOME` or any path under it | `config.Locate()` → `config.Paths` | `ConfigPath DBPath LogsDir RunsDir DaemonLogPath LockPath SettingsPath` |
| derive a slug, branch, worktree path, run log path, claude session id | `internal/naming` | `Ref`, `ParseRef`, `Slug`, `Branch`, `ClaudeSessionID` (UUIDv5, pinned namespace), `WorktreePath`, `RunLogPath`, `RunMetaPath`, `DefaultWorktreeRoot`. All pure; never reads `LF_HOME` |
| run any SQL | `internal/store` | `Open(ctx, path)` then **`Migrate(ctx)` explicitly** — `Open` does not migrate. Accessors are one file per entity |
| run any git command | `internal/git` | `Resolve(ctx)` → `Binary.Open(ctx, dir)` → `Clone` (reads) / `Worktree` (mutates). `EnsureWorktree`, `ParseRemoteURL`, `*CommandError` |
| serialise git ops across concurrent turns on one registered clone | `internal/daemon`'s `cloneGate`, via `turn.Options.CloneGate` | Unexported in `daemon`; `internal/turn` only sees the `turn.CloneGate` interface it declares (`internal/turn/execute.go`). `nil` is refused, never treated as a no-op. See "Concurrent turns" below |
| refuse two registered repos from sharing one worktree directory | `internal/repos` | `ValidateWorktreeRoots(existing)` at daemon startup, `ValidateNewWorktreeRoot(reg, existing)` at `repo add`, both before any row is written (AC-PAR-4.8) |
| mark/clear/list a session that still owes the daemon a look | `internal/store` | `SetSessionNeedsPoll`, `ClearSessionNeedsPoll`, `ListBacklogSessions` (`internal/store/sessions.go`) over `sessions.needs_poll`. Written at dispatch, cleared only on a fully drained turn — never from a `defer` (T-PAR-12) |
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
