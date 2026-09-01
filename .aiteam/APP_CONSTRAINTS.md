# APP_CONSTRAINTS — litefactory

**This is litefactory's Book of Standards.** It holds the rules that bind a
change to this codebase. It is normative; [`APP_CONTEXT.md`](APP_CONTEXT.md) is
the descriptive companion that tells you what already exists and where.

The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**,
**SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **MAY** and **OPTIONAL** are to be
interpreted as described in [RFC 2119](https://www.rfc-editor.org/rfc/rfc2119).

Rules carry stable identifiers (`SEC-1`, `CFG-4`, …). Cite them: an architecture
hint, a review finding or a code comment that names the rule it is applying is
worth more than one that restates it. A rule marked **[enforced]** has a test
that fails when you break it — the enforcement table at the bottom says which.

**Who reads this.** `go-architect` reads it in full, every time: its guidance
MUST stay inside these rails. `go-implementation-engineer` reads the section it
needs, when it needs it — not the whole file up front. Nobody else needs it.

**Deviating.** These are not preferences. If you believe a rule is wrong for
the change in front of you, you MUST flag it explicitly as a deviation with the
reasoning, and it MUST be *written into* `.aiteam/AC_AMENDMENTS.md`. (That is a
write, not a read — do not open the ledger to look something up unless you are
recording a deviation in it.) You MUST NOT
silently contradict a rule here.

---

## Sections

Read the sections your change touches. You are not expected to read this file
end to end unless you are the architect.

| § | Prefix | Read it when |
|---|---|---|
| [1. Security](#1-security-sec) | `SEC` | anything near the claude subprocess, the child environment, the token, git writes, identity, or text that leaves the process |
| [2. Configuration](#2-configuration-cfg--internalconfig) | `CFG` | touching `internal/config`, adding a config key, or resolving a path under `$LF_HOME` |
| [3. Testing](#3-testing-test) | `TEST` | writing or changing any test |
| [4. Concurrency and resource discipline](#4-concurrency-and-resource-discipline-conc) | `CONC` | goroutines, shared state across turns, slices crossing a struct boundary, ordering of post/commit |
| [5. Style and code shape](#5-style-and-code-shape-style) | `STYLE` | always worth a skim; it is short |
| [6. External binaries](#6-external-binaries-ext) | `EXT` | changing how litefactory invokes `claude` or `git` |

---

## 1. Security (`SEC`)

These are not style rules. Each has a test or a type that fails if you break it.

- **SEC-1 [enforced]** — `--dangerously-skip-permissions` MUST NOT be
  constructed anywhere in the tree. `config.forbiddenFlag()`
  (`internal/config/policy.go`) assembles it from two fragments precisely so the
  whole string is not a literal anywhere, which keeps a repository-wide grep a
  reliable review invariant. `validateNoForbiddenFlag` additionally refuses any
  *configured* value that smuggles it in through `claude.binary`,
  `settings_path`, or any tool list.
- **SEC-2** — Untrusted input MUST be passed to the child process on stdin and
  MUST NOT be passed in argv (AC-3.12). `harness.Request.Prompt` is piped in.
  This removes the ~1 MB macOS `ARG_MAX` ceiling and keeps third-party text out
  of every `ps` listing.
- **SEC-3** — `--tools` MUST be treated as the real tool boundary;
  `--allowedTools` MUST NOT be relied on as one. Measured: `--allowedTools` is
  *additive* to the operator's own `~/.claude/settings.json` — 250 ambient
  allow-rules leaked through and `Bash` executed while absent from the list.
  `--tools` is what the init event's `.tools` array reflects. An omitted
  `--tools` means allow-all, so an empty list MUST be a validation error rather
  than "no tools" (`validateTools` in `internal/config/policy.go`, and the
  `New()` guard in `internal/harness/runner.go`).
- **SEC-4** — The claude child's environment MUST be an allowlist and MUST NOT
  be `os.Environ`. `internal/harness/env.go` permits
  `PATH HOME USER SHELL LANG LC_ALL TERM TMPDIR` and nothing else. Every entry
  MUST carry a documented reason (`USER` is required or the Keychain lookup
  silently fails with "Not logged in"). `SSH_AUTH_SOCK` and `CLAUDE_CONFIG_DIR`
  are deliberately absent and MUST stay absent. `AllowedEnvNames()` is exported
  so a diagnostic can print the boundary.
- **SEC-5** — `internal/report` MUST re-apply input restrictions locally rather
  than trusting upstream validation. `internal/report/reply.go` re-declares
  `profileNamePattern` (rather than importing it) and `commandSpan` re-restricts
  the command token, because this is the one place attacker-controlled text is
  echoed into a comment litefactory signs its own name to. `commandSpan` MUST
  *stop* at the first disallowed rune rather than skipping it, so unexpected
  input can only ever produce a shorter, plainer token. Two of the three tool
  refusals carry no field at all for label text to travel in — that is the
  mechanism, not a habit, and it MUST NOT be softened into one.
- **SEC-6** — Redaction MUST be treated as hygiene, not as a boundary
  (`internal/redact/doc.go`). The boundary is not giving the subprocess the
  secret. The ordering is enforced by the type system: renderers return
  `redact.Body`, which only `redact.Redactor.Apply` can construct and
  `github.PostComment` is the only consumer of. There MUST be no exported way to
  get the text back out. Outbound bodies MUST be redacted; the local
  `$LF_HOME/logs` copy deliberately MUST NOT be.
- **SEC-7 [enforced]** — litefactory MUST NOT write inside a registered clone.
  `git.Clone`'s method set *is* the S-6 allowlist (`Fetch WorktreeAdd
  WorktreeList WorktreeRemove RevParse RemoteURL ConfigGet ShowRef` + `Path`).
  There MUST be no `Commit`, `Checkout`, `Reset`, `Merge` or `Pull` on it, so
  mutating a registered clone stays a compile error.
- **SEC-8** — Identity MUST be compared numerically and MUST NOT be compared by
  handle (`internal/github/doc.go`, D-I). Logins are renameable and a
  fine-grained PAT on a machine user reports type `"User"`.
- **SEC-9 [enforced]** — `gh` MUST be used for identity discovery only, MUST be
  reached only by `litefactory init`, and its child environment MUST strip
  `GITHUB_TOKEN`/`GH_TOKEN` (`internal/identity`). Otherwise `init` records the
  bot as the owner and the daemon reads its own comments as instructions.
- **SEC-10** — `syscall.Umask(0o077)` MUST remain the first statement of `main`.
  Nothing in litefactory calls `Chmod`; that one line is why it does not have to.
- **SEC-11** — `redact.Redactor.Apply` (`internal/redact/redact.go`) holds the
  only `recover()` in the tree: a panic mid-redaction becomes an error so the
  post aborts and the watermark does not advance (AC-4.9). A second `recover()`
  MUST NOT be added.

---

## 2. Configuration (`CFG`) — `internal/config`

- **CFG-1** — Decoding MUST be strict. `yaml.Decoder.KnownFields(true)`: an
  unknown or misspelled key MUST be a startup error, not a silently ignored line
  (AC-0.10). A second YAML document MUST also be refused. `friendlyYAMLError`
  rewrites yaml.v3's Go-type phrasing into `unknown key "…"`.
- **CFG-2** — Defaults MUST be applied by decoding on top of `Defaults()`, so an
  explicitly written zero (`poll_interval_seconds: 0`) stays distinguishable
  from an absent key and is rejected rather than silently replaced.
- **CFG-3 [enforced]** — The shipped template MUST NOT drift from `Defaults()`.
  `internal/config/template.go` holds a commented `config.yaml` as a format
  string and `DefaultFileContents()` interpolates the values *from* `Defaults()`.
  A new config key MUST be added to `Defaults()` **and** to the template.
  (`tool_ceiling` and `tool_profiles` are written as commented examples only —
  both are optional and absent on a fresh install.)
- **CFG-4 [enforced]** — `config.yaml` MUST NOT contain a secret, only a
  reference to one (a keychain service/account, or the *name* of an env var).
  `secretscan.go` reflects over every string in the decoded struct against the
  S-7 credential shapes; a hit MUST be a hard startup error unwrapping to
  `ErrLiteralSecret`, mapped to exit 3. The matched text MUST NOT appear in the
  error — only the key and the class.
- **CFG-5** — Validation MUST aggregate. `Config.Validate()` collects every
  problem and returns `errors.Join(errs...)`. Reporting one problem per run
  would mean fixing four typos in four restarts. New validation MUST follow this.
- **CFG-6** — An out-of-range value MUST be an error and MUST NOT be silently
  clamped (`poll_interval_seconds` 10–60, `claude.timeout_seconds` 60–86400,
  `worktree_retain_days` 1–3650, `max_concurrent_runs` 1–16).
- **CFG-7** — `exec.LookPath` MUST NOT be used. launchd hands a LaunchAgent
  `PATH=/usr/bin:/bin:/usr/sbin:/sbin`, which has no Homebrew.
  `config.ResolveExecutable` searches an explicitly passed PATH, and its error
  MUST name every directory it looked in.
- **CFG-8** — Nothing outside `internal/config` MAY join a path against
  `$LF_HOME`. A new path MUST become a method on `config.Paths` (`ConfigPath
  DBPath LogsDir RunsDir DaemonLogPath LockPath SettingsPath`).

---

## 3. Testing (`TEST`)

- **TEST-1** — Tests SHOULD be table-driven, MUST use stdlib `testing`, and
  SHOULD call `t.Parallel()` where safe. `go-cmp` is the only assertion helper
  and SHOULD be used sparingly (7 test files: `config`, `redact`, `launchd`,
  `repos`). Everything else compares by hand with a good failure message.
- **TEST-2** — A mocking framework MUST NOT be added, and neither MUST a new
  module. There is no testify, no gomock, no mockery, and adding one is not on
  the table. Fakes MUST be hand-written structs. `go.mod` has exactly four
  direct requires (`go-cmp`, `go-github`, `yaml.v3`, `modernc.org/sqlite`) and
  SHOULD keep that shape.
- **TEST-3** — A package-wide fake cast SHOULD extend an existing
  `fixture_test.go` rather than standing up a third way of faking a turn.
  `internal/turn/fixture_test.go` (in-package) builds a real git clone with a
  real origin, a migrated database and a configurable fake claude;
  `internal/daemon/fixture_test.go` (`package daemon_test`) builds an `httptest`
  GitHub, a real store and a fake executor.
- **TEST-4** — `*_internal_test.go` MUST mean white-box. In packages otherwise
  black-box tested (`daemon`, `git`, `github`), a file with that suffix is
  `package X` and everything else is `package X_test`. Packages tested in-package
  throughout (`turn`, `harness`, `config`, `cli`, `naming`, `logging`,
  `secrets`) MUST NOT use the suffix.
- **TEST-5** — Interfaces MUST be declared at the point of consumption. `harness`
  exports a concrete `*Runner` and `turn` declares the one-method `turn.Runner`
  it needs; `report` exports a concrete `*Reporter` and `turn` declares
  `Renderer` and `Poster`. An interface MUST NOT be added next to its
  implementation.
- **TEST-6** — A promise about a transaction MUST be tested against real SQLite,
  never a fake store (see the comment on `turn.ReporterOptions.DB`). A fake would
  let a test assert a guarantee the implementation does not keep.
- **TEST-7** — Determinism is required; Go map ordering has bitten this codebase.
  You MUST sort before iterating a map or before emitting a list an assertion
  will read. Existing sites: `sortedProfileNames` (`internal/config/policy.go`),
  the `reflect.Map` branch in `secretscan.go`, `harness/env.go`,
  `github/comments.go`, `launchd/plist.go`, `store/migrate.go`,
  `redact/redact.go`, `cli/cmd_logs_render.go`.
- **TEST-8** — In the concurrency tests of `internal/daemon` and `internal/turn`
  you MUST NOT use `time.Sleep`, an `Eventually` helper, or wall-clock polling.
  Rendezvous on channels, wait with `Daemon.Quiesce`, and bound every rendezvous
  with a timeout that fails with a message naming what was waited for.
- **TEST-9** — There is no observable in `internal/daemon` meaning "a worker has
  released its slot", and one MUST NOT be invented for a test. The forge post,
  the watermark write and the "turn complete" log line all happen strictly
  *before* the worker's deferred `inflight.leave` and `pool.done()`. A test that
  needs a freed slot MUST retry over real ticks — which is also faithful to the
  design, since dispatch has no queue. (This rule cost a flaky test to learn.)
- **TEST-10** — Some tests exist to fail on a *design change*, not on a bug — the
  `surface_test.go` / `policy_test.go` / `purity_test.go` family, plus
  `daemon/clock_test.go` and `turn/gate_test.go`. When one fails you MUST make
  the design change deliberately; you MUST NOT edit the test to pass. Adding an
  exported method, an interface, an import, a new call site or a new critical
  section can fail the build. That is the feature.

---

## 4. Concurrency and resource discipline (`CONC`)

- **CONC-1** — Slices MUST be copied at struct boundaries, with
  `append([]string(nil), …)` (11 existing sites across `harness/runner.go`,
  `harness/meta.go`, `turn/execute.go`, `turn/report.go`, `daemon/daemon.go`,
  `daemon/tools.go`, `daemon/tick.go`) or `slices.Clone` / `maps.Clone`.
  Originally this defended a sequential bug: a request struct is mutated in place
  on the cold-rerun path, and one `append` into spare capacity would rewrite the
  operator's configuration for every later run in the process. **Under concurrent
  turns it is load-bearing for isolation between live sibling turns** — two turn
  workers legitimately hold requests built from the same configuration at the
  same instant, so a missed copy is a data race, not a stale value. One of these
  MUST NOT be removed on the grounds that nothing looked like it was reading the
  slice again.
- **CONC-2** — Goroutine budgets MUST be documented and MUST stay tiny. The
  inventory is four fixed goroutines — `harness/runner.go` (exactly two, one per
  pipe, neither able to outlive `Run`), `daemon/daemon.go` (the loop), and
  `cmd/litefactory/main.go` (the second-signal watcher) — plus at most
  `max_concurrent_runs` turn workers dispatched from `Daemon.offer`
  (`internal/daemon/tick.go`) via `pool.try` (`internal/daemon/pool.go`). With
  `max_concurrent_runs` refused (never clamped) outside 1..16, the hard ceiling
  on the process is twenty goroutines. A new goroutine MUST say in a comment what
  bounds its lifetime. The one named termination condition for the worker pool is
  `Daemon.Quiesce` — not a `select` on shutdown, and not a sleep. **This bullet is
  the canonical statement of the inventory**; older plans under `.aiteam/` state
  it differently and carry pointers saying so. Do not go and read them — one of
  them names a `logs -f` tailer goroutine that does not exist (`litefactory logs
  -f` re-reads the run's JSONL synchronously on the command's own goroutine).
- **CONC-3** — A bad outcome MUST be a value, not an error. Across `turn`,
  `harness` and `daemon`, an operation that *happened and went badly* returns a
  result whose field says how; the `error` return is reserved for "could not be
  conducted or recorded at all". Silence on the issue is the one outcome that is
  forbidden (AC-4.1).
- **CONC-4** — Post first, then commit. Every reporting path MUST render, post,
  and only then write to the database in a single transaction. Advancing a
  watermark before a successful post turns a transient 502 into permanent silence.
- **CONC-5** — Every store accessor and every git method MUST take `ctx` first.
- **CONC-6** — Retries and sleeps MUST NOT appear below the daemon.
  `internal/github` retries nothing and sleeps nowhere; backoff is
  `internal/daemon`'s job.

---

## 5. Style and code shape (`STYLE`)

- **STYLE-1** — Comments MUST explain *why*, and MUST be true. This codebase
  holds an unusually high standard: comments cite decision and
  acceptance-criterion identifiers (`D-6`, `AC-3.12`, `SD-2`, `S-7`, `C9`,
  `P-3`), record what was *measured* rather than assumed, and say what would
  break if the code were changed the obvious way. **When your change makes a
  comment untrue, updating it is part of the change.** A one-line restatement of
  the code is worse than nothing here.
- **STYLE-2** — Lint rules that are not negotiable (`.golangci.yml`):
  `context.Background()` MUST NOT appear outside `main` (take the context from
  the caller); `fmt.Print*` and `log.*` MUST NOT be used (use the injected
  `*slog.Logger`, or the command's output writer). `errcheck` checks type
  assertions; `gosec`, `errorlint`, `nilerr`, `noctx`, `unparam` are on. `govet`
  is `enable-all` minus `fieldalignment` and `shadow`, and the config explains
  why for both.
- **STYLE-3** — New wiring SHOULD use an `Options` struct plus
  `New(opts) (*T, error)`, validating every dependency at wiring time rather
  than at first use. The stated reason recurs verbatim across the codebase: the
  first render happens when a run has just finished, which is the worst possible
  moment to discover the reporter cannot produce a body.
- **STYLE-4** — Where a constructor would be overkill, a function field SHOULD be
  the injection seam: `secrets.Resolver{Keychain, LookupEnv}` has no
  `NewResolver`; `launchd.Manager` takes a `Runner`. Tests substitute the field.
- **STYLE-5** — Errors SHOULD be typed, carry fields, and unwrap to a sentinel.
  `*git.CommandError` exposes command and stderr as fields so a caller can print
  them verbatim; `*github.RateLimitError.RetryAfter` is read by the daemon's
  backoff; `*config.SecretError` carries the key and class but never the value.
  Formatting a string a caller has to re-parse SHOULD be avoided.
- **STYLE-6** — CLI handlers MUST NOT read the flag set back. `Command.New(fs)`
  binds flags and returns a *closure* capturing the values; the handler receives
  only arity-checked positionals plus `*cli.Env`. Exit code comes from the error
  kind: `*UsageError` (bad command line) vs `*PreconditionError` (machine not
  ready) vs plain error.
- **STYLE-7** — Eleven packages carry a `doc.go` whose prose is the real
  specification for that package. You MUST read the one for the package you are
  about to touch, before you touch it.

---

## 6. External binaries (`EXT`)

- **EXT-1** — `claude` and `git` are external binaries this codebase depends on.
  Their behaviour MUST be probed read-only, in a scratch directory outside the
  repo, and written up in a document. A test that spawns a real `claude` MUST
  NOT be committed: it would be slow, quota-consuming and flaky, and the repo has
  no build-tag or env-var gate for it.
- **EXT-2** — A write-up MUST state the exact binary version it was verified
  against and how to re-verify after an upgrade. Two such documents exist and a
  change to how litefactory invokes `claude` SHOULD extend one rather than
  starting a third convention: the `--session-id`/`--resume` contract, and the
  tool-surface probe. Their paths are in the Story Index of the sessions that
  produced them; you do not need to read them unless you are changing that
  invocation.

---

## Enforcement

If one of these fails, you changed a design decision — make it deliberately
rather than editing the test.

| Test | What it pins |
|---|---|
| `config/purity_test.go` | `internal/config` is stdlib-only bar the YAML decode; the skip-permissions flag appears nowhere in the tree (SEC-1) |
| `git/surface_test.go` | `Clone`'s method set *is* the S-6 allowlist; `Worktree` has no exported construction path; zero values refuse to exec git (SEC-7) |
| `git/construction_internal_test.go` | `Worktree` is built in exactly one place |
| `git/exec_internal_test.go` | every git command carries a deadline; the child env is an allowlist; the launchd PATH fallback (SEC-4, CFG-7) |
| `github/surface_test.go` | `PostComment` takes `redact.Body`, never `string`; exactly seven endpoints; no interface type is declared in the package (no premature Forge abstraction); `Token.Reveal()` has exactly one call site, on the `Authorization` line (SEC-6, TEST-5) |
| `identity/policy_test.go` | only `cli/cmd_init.go` may import `internal/identity`; the literal `"gh"` appears nowhere else outside `store/identities.go` (SEC-9) |
| `gittest/policy_test.go` | no non-test file imports `internal/gittest` |
| `repos/network_test.go` | registration makes no network call — `repo add` must work on a plane |
| `config/template_test.go` | the shipped `config.yaml` template decodes back to `Defaults()` (CFG-3) |
| `store/store_test.go` | FK enforcement, migrate idempotency, `AdvanceWatermark` never rewinds, `AllocateRun` refuses a replay, `CompleteRunReport` is one transaction |
| `daemon/clock_test.go` | only `Daemon.Tick` (never a helper, never a function literal) reads the daemon's clock |
| `turn/gate_test.go` | `prepareClone`, `commitAndPush` and `commitStoryIndex` are the only three call sites of `CloneGate.Acquire`, one acquisition each (a registered-set check, not three hardcoded names); the claude run itself is never inside a critical section |
| `daemon/gate_internal_test.go`, `pool_internal_test.go`, `inflight_internal_test.go` | gate/pool/inflight mechanics in isolation — mutual exclusion per path, semaphore refuses over `max_concurrent_runs`, `enter`/`leave` are a true check-and-insert (CONC-2) |
| `repos/collision_test.go` | two registered repos may not resolve the same worktree directory |
| `daemon/record_test.go` (`TestNoStaleConcurrencyStatementSurvives`) | no `.go` file outside `.aiteam` says "locked by another daemon instance" (AC-PAR-8.4) |
| `daemon/record_test.go` (`TestIntroducedCriteriaCarryTheParPrefix`) | every acceptance criterion this feature's plan and spec introduce carries the `AC-PAR-` prefix (AC-PAR-8.7) |
