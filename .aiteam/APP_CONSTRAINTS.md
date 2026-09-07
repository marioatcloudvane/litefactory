# APP_CONSTRAINTS — litefactory

**This is litefactory's Book of Standards.** It holds the rules that bind a
change to this codebase. It is normative; [`APP_CONTEXT.md`](APP_CONTEXT.md) is
the descriptive companion that tells you what already exists and where.

The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**,
**SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **MAY** and **OPTIONAL** are to be
interpreted as described in [RFC 2119](https://www.rfc-editor.org/rfc/rfc2119).

Rules carry stable identifiers (`SEC-1`, `CFG-4`, …). Cite them: a hint,
review finding, or comment naming the rule it applies is worth more than one
that restates it. **[enforced]** means a test fails when you break it — see
the enforcement table at the bottom.

**Who reads this.** `go-architect` reads it in full, every time. `go-implementation-engineer`
reads the section it needs, when it needs it. Nobody else needs it.

**Deviating.** These are not preferences. A rule may be bent only as an
explicit, reasoned deviation recorded in `.aiteam/AC_AMENDMENTS.md`; you MUST
NOT silently contradict a rule here.

---

## Sections

Read the sections your change touches; only the architect reads end to end.

| § | Prefix | Read it when |
|---|---|---|
| 1. Security | `SEC` | anything near the claude subprocess, the child environment, the token, git writes, identity, or text that leaves the process |
| 2. Configuration | `CFG` | touching `internal/config`, adding a config key, or resolving a path under `$LF_HOME` |
| 3. Testing | `TEST` | writing or changing any test |
| 4. Concurrency | `CONC` | goroutines, shared state across turns, slices crossing a struct boundary, ordering of post/commit |
| 5. Style | `STYLE` | always worth a skim; it is short |
| 6. External binaries | `EXT` | changing how litefactory invokes `claude` or `git` |

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
- **SEC-2** — Untrusted input MUST be passed on stdin, MUST NOT be passed in
  argv (AC-3.12). `harness.Request.Prompt` is piped in — removes the ~1 MB
  macOS `ARG_MAX` ceiling and keeps third-party text out of `ps`.
- **SEC-3** — `--tools` MUST be treated as the real tool boundary;
  `--allowedTools` MUST NOT be relied on as one. Measured: `--allowedTools` is
  *additive* to the operator's own `~/.claude/settings.json` — 250 ambient
  allow-rules leaked through and `Bash` ran while absent from the list.
  `--tools` is what the init event's `.tools` array reflects. An omitted
  `--tools` means allow-all, so an empty list MUST be a validation error, not
  "no tools" (`validateTools`, `internal/config/policy.go`; the `New()` guard,
  `internal/harness/runner.go`).
- **SEC-4** — The claude child's environment MUST be an allowlist, never
  `os.Environ`. `internal/harness/env.go` permits `PATH HOME USER SHELL LANG
  LC_ALL TERM TMPDIR` and nothing else, each with a documented reason (`USER`
  is required or the Keychain lookup fails with "Not logged in"). `SSH_AUTH_SOCK`
  and `CLAUDE_CONFIG_DIR` MUST stay absent. `AllowedEnvNames()` is exported so a
  diagnostic can print the boundary.
- **SEC-5** — `internal/report` MUST re-apply input restrictions locally rather
  than trusting upstream validation. `internal/report/reply.go` re-declares
  `profileNamePattern` (rather than importing it) and `commandSpan` re-restricts
  the command token, because this is the one place attacker-controlled text is
  echoed into a comment litefactory signs its own name to. `commandSpan` MUST
  *stop* at the first disallowed rune rather than skipping it. Two of the three
  tool refusals carry no field for label text to travel in — that is the
  mechanism and MUST NOT be softened.
- **SEC-6** — Redaction MUST be treated as hygiene, not a boundary
  (`internal/redact/doc.go`) — not giving the subprocess the secret is the
  boundary. The ordering is enforced by type: renderers return `redact.Body`,
  which only `redact.Redactor.Apply` constructs and only `github.PostComment`
  consumes; there MUST be no exported way to get the text back out. Outbound
  bodies MUST be redacted; the local `$LF_HOME/logs` copy MUST NOT be.
- **SEC-7 [enforced]** — litefactory MUST NOT write inside a registered clone.
  `git.Clone`'s method set *is* the S-6 allowlist (`Fetch WorktreeAdd
  WorktreeList WorktreeRemove RevParse RemoteURL ConfigGet ShowRef` + `Path`).
  There MUST be no `Commit`, `Checkout`, `Reset`, `Merge` or `Pull` on it, so
  mutating a registered clone stays a compile error.
- **SEC-8** — Identity MUST be compared numerically, never by handle
  (`internal/github/doc.go`, D-I). Logins are renameable and a fine-grained
  PAT on a machine user reports type `"User"`.
- **SEC-9 [enforced]** — `gh` MUST be used for identity discovery only, reached
  only by `litefactory init`, with `GITHUB_TOKEN`/`GH_TOKEN` stripped from its
  child environment (`internal/identity`). Otherwise `init` records the bot as
  the owner and the daemon reads its own comments as instructions.
- **SEC-10** — `syscall.Umask(0o077)` MUST remain the first statement of
  `main`. Nothing here calls `Chmod`; that one line is why it does not have to.
- **SEC-11** — `redact.Redactor.Apply` holds the only `recover()` in the
  tree: a panic mid-redaction becomes an error so the post aborts and the
  watermark does not advance (AC-4.9). A second `recover()` MUST NOT be added.
- **SEC-12 [enforced]** — `internal/team`'s write entry points MUST
  aggregate every validation fault via `errors.Join`/`joinSorted` rather than
  fail-fast (mirrors CFG-5); a batch `Add` call MUST write nothing if any
  render is invalid.
- **SEC-13 [enforced]** — `team.Add` MUST write via temp-file-then-rename,
  MUST NOT call `Chmod` (ties SEC-10), and MUST save its lockfile last.
- **SEC-14 [enforced]** — `team.Add` MUST `Lstat` (never `Stat`) and refuse a
  symlinked path component before each write, so a swap cannot redirect
  writes outside the repo.
- **SEC-15 [enforced]** — A `RenderedFile.Path` MUST match the Owned Path
  `team.Add` re-derives from `(harness, agentKey)`; a renderer's claimed
  path is never trusted.
- **SEC-16 [enforced]** — A root-file record's `RenderedHash` MUST hash only
  the marker span (`markerSpanHash`), never the whole file, so it cannot
  misfire on unrelated edits.
- **SEC-17 [enforced]** — A caller assembling a shared agent's
  `AgentRender.Teams` (`internal/teamsource.RenderTeam`) MUST union the new
  team into `team.ExistingTeams(root, harness, agentKey)`'s result, never
  overwrite it: `upsertMarkerSection` replaces the section wholesale from
  `Teams` alone, so skipping the union drops every other team's attribution.
- **SEC-18** — `internal/team` MUST NOT import `internal/render` or
  `internal/teamsource`, preserving the `cli → teamsource → (team, render)`
  direction. `Classify` (`internal/team/status.go`) takes a caller-computed
  fresh render rather than rendering itself, so importing either here would
  let the leaf depend on what already depends on it.
- **SEC-19** — `PromptInput.PreviousReason` (`internal/harness/prompt.go`), the
  previous run's stored failure reason, MUST NOT reach a resumed run's prompt
  raw — it can be agent-authored text. `writePreviousFailedNote` MUST route it
  through `capText`/`neutraliseNoteTags` and frame it in a `<note>` element
  like any other third-party text, so a forged `</note>` or `- Mode:` line
  cannot reach the header as structure.

---

## 2. Configuration (`CFG`) — `internal/config`

- **CFG-1** — Decoding MUST be strict: `yaml.Decoder.KnownFields(true)`, so an
  unknown or misspelled key is a startup error, not a silently ignored line
  (AC-0.10). A second YAML document MUST also be refused. `friendlyYAMLError`
  rewrites yaml.v3's Go-type phrasing into `unknown key "…"`.
- **CFG-2** — Defaults MUST be applied by decoding on top of `Defaults()`, so
  an explicit zero (`poll_interval_seconds: 0`) stays distinguishable from an
  absent key and is rejected, not silently replaced.
- **CFG-3 [enforced]** — The shipped template MUST NOT drift from `Defaults()`.
  `internal/config/template.go` holds a commented `config.yaml` as a format
  string and `DefaultFileContents()` interpolates the values *from* `Defaults()`.
  A new config key MUST be added to `Defaults()` **and** to the template
  (`tool_ceiling`/`tool_profiles` ship as commented examples only, both
  optional and absent on a fresh install).
- **CFG-4 [enforced]** — `config.yaml` MUST NOT contain a secret, only a
  reference to one (a keychain service/account, or an env var *name*).
  `secretscan.go` reflects over every decoded string against the S-7
  credential shapes; a hit MUST be a hard startup error unwrapping to
  `ErrLiteralSecret` (exit 3). The matched text MUST NOT appear in the error —
  only the key and class.
- **CFG-5** — Validation MUST aggregate. `Config.Validate()` collects every
  problem and returns `errors.Join(errs...)`. Reporting one problem per run
  would mean fixing four typos in four restarts. New validation MUST follow this.
- **CFG-6** — An out-of-range value MUST error, MUST NOT be clamped
  (`poll_interval_seconds` 10–60, `claude.timeout_seconds` 60–86400,
  `worktree_retain_days` 1–3650, `max_concurrent_runs` 1–16).
- **CFG-7** — `exec.LookPath` MUST NOT be used: launchd hands a LaunchAgent
  `PATH=/usr/bin:/bin:/usr/sbin:/sbin`, with no Homebrew. `config.ResolveExecutable`
  searches an explicitly passed PATH; its error MUST name every directory checked.
- **CFG-8** — Nothing outside `internal/config` MAY join a path against
  `$LF_HOME`; a new path MUST become a method on `config.Paths` (`ConfigPath
  DBPath LogsDir RunsDir DaemonLogPath LockPath SettingsPath`).

---

## 3. Testing (`TEST`)

- **TEST-1** — Tests SHOULD be table-driven, MUST use stdlib `testing`, and
  SHOULD call `t.Parallel()` where safe. `go-cmp` is the only assertion
  helper, used sparingly (7 files: `config`, `redact`, `launchd`, `repos`).
  Everything else compares by hand with a good failure message.
- **TEST-2** — A mocking framework or a new module MUST NOT be added — no
  testify, no gomock, no mockery. Fakes MUST be hand-written structs. `go.mod`
  has exactly four direct requires (`go-cmp`, `go-github`, `yaml.v3`,
  `modernc.org/sqlite`) and SHOULD keep that shape.
- **TEST-3** — A package-wide fake cast SHOULD extend an existing
  `fixture_test.go` rather than standing up a third way of faking a turn.
  `internal/turn/fixture_test.go` builds a real git clone with a real origin,
  a migrated database and a configurable fake claude; `internal/daemon/fixture_test.go`
  builds an `httptest` GitHub, a real store and a fake executor.
- **TEST-4** — `*_internal_test.go` MUST mean white-box: in packages
  otherwise black-box tested (`daemon`, `git`, `github`), that suffix is
  `package X`, else `package X_test`. Packages tested in-package throughout
  (`turn`, `harness`, `config`, `cli`, `naming`, `logging`, `secrets`) MUST
  NOT use the suffix.
- **TEST-5** — Interfaces MUST be declared at the point of consumption. `harness`
  exports a concrete `*Runner` and `turn` declares the one-method `turn.Runner`
  it needs; `report` exports a concrete `*Reporter` and `turn` declares
  `Renderer` and `Poster`. An interface MUST NOT be added next to its
  implementation.
- **TEST-6** — A promise about a transaction MUST be tested against real
  SQLite, never a fake store (comment on `turn.ReporterOptions.DB`) — a fake
  would let a test assert a guarantee the implementation does not keep.
- **TEST-7** — Determinism is required; Go map ordering has bitten this
  codebase. You MUST sort before iterating a map or emitting a list an
  assertion will read. Existing sites: `sortedProfileNames`
  (`internal/config/policy.go`), `secretscan.go`'s `reflect.Map` branch,
  `harness/env.go`, plus five more across `github`, `launchd`, `store`,
  `redact`, `cli`.
- **TEST-8** — In `internal/daemon`/`internal/turn` concurrency tests,
  `time.Sleep`, an `Eventually` helper, or wall-clock polling MUST NOT be
  used. Rendezvous on channels, wait with `Daemon.Quiesce`, and bound every
  rendezvous with a timeout naming what was waited for.
- **TEST-9** — There is no observable in `internal/daemon` meaning "a worker
  released its slot", and one MUST NOT be invented for a test: the forge
  post, watermark write, and "turn complete" log line all happen *before*
  the worker's deferred `inflight.leave`/`pool.done()`. A test needing a
  freed slot MUST retry over real ticks.
- **TEST-10** — Some tests exist to fail on a *design change*, not on a bug — the
  `surface_test.go` / `policy_test.go` / `purity_test.go` family, plus
  `daemon/clock_test.go` and `turn/gate_test.go`. When one fails you MUST make
  the design change deliberately; you MUST NOT edit the test to pass. Adding an
  exported method, an interface, an import, a new call site or a new critical
  section can fail the build. That is the feature.

---

## 4. Concurrency and resource discipline (`CONC`)

- **CONC-1** — Slices MUST be copied at struct boundaries, with
  `append([]string(nil), …)` or `slices.Clone`/`maps.Clone` (11 sites across
  `harness`, `turn`, `daemon`). Defends a sequential mutation bug and is now
  load-bearing for isolation between concurrent turn workers holding requests
  built from the same configuration — a missed copy is a data race, not a
  stale value.
- **CONC-2** — Goroutine budgets MUST be documented and MUST stay tiny:
  two in `harness/runner.go` (one per pipe), the loop in `daemon/daemon.go`,
  the second-signal watcher in `cmd/litefactory/main.go`, plus at most
  `max_concurrent_runs` (refused, never clamped, outside 1..16 — ceiling
  twenty) turn workers from `Daemon.offer`/`pool.try`. A new goroutine MUST
  say in a comment what bounds its lifetime. The pool's one termination
  condition is `Daemon.Quiesce` — not a shutdown `select`, not a sleep.
- **CONC-3** — A bad outcome MUST be a value, not an error. Across `turn`,
  `harness` and `daemon`, an operation that *happened and went badly* returns
  a result whose field says how; `error` is reserved for "could not be
  conducted or recorded at all". Silence on the issue is forbidden (AC-4.1).
- **CONC-4** — Post first, then commit. Every reporting path MUST render,
  post, and only then write to the database in one transaction. Advancing a
  watermark before a successful post turns a transient 502 into permanent
  silence.
- **CONC-5** — Every store accessor and every git method MUST take `ctx` first.
- **CONC-6** — Retries and sleeps MUST NOT appear below the daemon:
  `internal/github` retries nothing and sleeps nowhere; backoff is
  `internal/daemon`'s job.
- **CONC-7 [enforced]** — In `internal/turn/prepare`,
  `SetSessionState(ctx, session.ID, store.SessionRunning)` MUST remain the last
  statement before a successful return, and MUST NOT be wrapped in
  `context.WithoutCancel`: every error return above it, the agent-switch
  refusal included (AC-3.1), MUST leave `sessions.state` untouched. A brief
  window where `allocateRun`'s `runs` row reads `running` before its session
  does is accepted, not a bug — `ReconcileOrphans` self-heals it at daemon
  start; no compensating logic MUST be added for it.
- **CONC-8 [enforced]** — A run MUST be committed and pushed only when it
  genuinely succeeded. `save` (`internal/turn/harvest.go`) and `fileStoryIssues`
  (`internal/turn/subissues.go`) both gate on `runResult.Succeeded()` before
  touching git; any other outcome (failed, timed out, abandoned, no result) MUST
  be left uncommitted in its worktree for the next run on the task to pick up
  and commit once it does succeed.

---

## 5. Style and code shape (`STYLE`)

- **STYLE-1** — Comments MUST explain *why*, and MUST be true. This
  codebase holds an unusually high standard: comments cite decision and
  acceptance-criterion identifiers (`D-6`, `AC-3.12`, `S-7`, …), record what
  was *measured*, and say what would break if changed the obvious way.
  **When a change makes a comment untrue, updating it is part of the
  change.** A restatement of the code is worse than nothing here.
- **STYLE-2** — Lint rules that are not negotiable (`.golangci.yml`):
  `context.Background()` MUST NOT appear outside `main`; `fmt.Print*`/`log.*`
  MUST NOT be used (use the injected `*slog.Logger` or the command's output
  writer). `errcheck` checks type assertions; `gosec`, `errorlint`, `nilerr`,
  `noctx`, `unparam` are on. `govet` is `enable-all` minus `fieldalignment` and
  `shadow`.
- **STYLE-3** — New wiring SHOULD use an `Options` struct plus
  `New(opts) (*T, error)`, validating every dependency at wiring time, not
  first use: the first render happens when a run has just finished — the
  worst moment to discover the reporter cannot produce a body.
- **STYLE-4** — Where a constructor would be overkill, a function field SHOULD be
  the injection seam: `secrets.Resolver{Keychain, LookupEnv}` has no
  `NewResolver`; `launchd.Manager` takes a `Runner`. Tests substitute the field.
- **STYLE-5** — Errors SHOULD be typed, carry fields, and unwrap to a
  sentinel. `*git.CommandError` exposes command/stderr for verbatim
  printing; `*github.RateLimitError.RetryAfter` drives the daemon's backoff;
  `*config.SecretError` carries the key and class, never the value.
- **STYLE-6 [enforced]** — CLI handlers MUST NOT read the flag set back.
  `Command.New(fs)` binds flags and returns a *closure* capturing the values;
  the handler receives only arity-checked positionals plus `*cli.Env`. Exit
  code comes from the error kind: `*UsageError` (bad command line) vs
  `*PreconditionError` (machine not ready) vs plain error — a domain-level
  finding (e.g. drift, a per-record failure) MUST exit via a plain error, never
  the other two, which are reserved for flag/precondition problems.
- **STYLE-7** — Read the `doc.go` for the package you are about to touch
  before you touch it — its prose is that package's real specification (18
  packages carry one).
- **STYLE-8** — `internal/harness/prompt.go`'s `"- Mode: %s\n"` header line
  and `CLAUDE.md`'s `## Classify` trust of that literal MUST change together,
  in the same commit. No test catches the `CLAUDE.md` side drifting —
  `prompt_test.go`/`prompt_body_test.go` pin only the Go string.
- **STYLE-9** — A loop that validates or writes several independent items (a
  batch, a repo's set of records) MUST aggregate every item's failure via
  `errors.Join`/`joinSorted` rather than returning on the first — same pattern
  as `CFG-5` and `SEC-12`; `cli`'s `runTeamUpdate` per-record loop follows it
  too. This exact fail-fast regression has reached BLOCKING code review twice,
  in two unrelated stories: the fix is one line, and missing it hides every
  problem after the first from the caller.
- **STYLE-10** — `agent.RunResult.Succeeded()` (live subprocess) and
  `store.Run.Succeeded()` (persisted row, `internal/store/runs.go`) MUST NOT be
  collapsed into one call or converted into each other — different evidence,
  different lifetimes, same English question.
- **STYLE-11** — A non-success run report MUST name the uncommitted worktree in
  exactly one place: `metadataList`'s Commit-line default arm
  (`internal/report/run.go`, when `CommitSHA==""`) or `outcomeView.withFailure`
  (when `CommitSHA!=""`) — never both.

---

## 6. External binaries (`EXT`)

- **EXT-1** — `claude` and `git` are external binaries this codebase depends
  on; their behaviour MUST be probed read-only, in a scratch directory
  outside the repo, and written up. A test spawning a real `claude` MUST NOT
  be committed: slow, quota-consuming, flaky, and ungated.
- **EXT-2** — A write-up MUST state the exact binary version verified
  against and how to re-verify after an upgrade. Two exist; a change to how
  litefactory invokes `claude` SHOULD extend one rather than starting a
  third: the `--session-id`/`--resume` contract, and the tool-surface probe.

---

## Enforcement

If one of these fails, you changed a design decision — make it deliberately
rather than editing the test.

| Test | What it pins |
|---|---|
| `config/purity_test.go` | `internal/config` is stdlib-only bar the YAML decode; skip-permissions flag appears nowhere in the tree (SEC-1) |
| `git/surface_test.go` | `Clone`'s method set *is* the S-6 allowlist; `Worktree` has no exported construction path; zero values refuse to exec git (SEC-7) |
| `git/construction_internal_test.go` | `Worktree` is built in exactly one place |
| `git/exec_internal_test.go` | every git command carries a deadline; the child env is an allowlist; the launchd PATH fallback (SEC-4, CFG-7) |
| `github/surface_test.go` | `PostComment` takes `redact.Body`, never `string`; seven endpoints; no interface declared in the package; `Token.Reveal()` has one call site, on the `Authorization` line (SEC-6, TEST-5) |
| `identity/policy_test.go` | only `cli/cmd_init.go` may import `internal/identity`; the literal `"gh"` appears nowhere else outside `store/identities.go` (SEC-9) |
| `gittest/policy_test.go` | no non-test file imports `internal/gittest` |
| `repos/network_test.go` | registration makes no network call — `repo add` must work on a plane |
| `config/template_test.go` | the shipped `config.yaml` template decodes back to `Defaults()` (CFG-3) |
| `store/store_test.go` | FK enforcement, migrate idempotency, `AdvanceWatermark` never rewinds, `AllocateRun` refuses a replay, `CompleteRunReport` is one txn |
| `store/runs_test.go` | `Run.Succeeded()`'s two-arm predicate: true for `RunSucceeded`, true for `RunReported` only when `Error==""` (STYLE-10) |
| `daemon/clock_test.go` | only `Daemon.Tick` (never a helper, never a function literal) reads the daemon's clock |
| `turn/gate_test.go` | `prepareClone`, `commitAndPush`, `commitStoryIndex` are the only call sites of `CloneGate.Acquire`, one each; the claude run itself is never inside a critical section |
| `daemon/gate_internal_test.go`, `pool_internal_test.go`, `inflight_internal_test.go` | gate/pool/inflight mechanics in isolation — mutual exclusion per path, semaphore refuses over `max_concurrent_runs` (CONC-2) |
| `repos/collision_test.go` | two registered repos may not resolve the same worktree directory |
| `daemon/record_test.go` (`TestNoStaleConcurrencyStatementSurvives`) | no `.go` file outside `.aiteam` says "locked by another daemon instance" (AC-PAR-8.4) |
| `daemon/record_test.go` (`TestIntroducedCriteriaCarryTheParPrefix`) | every acceptance criterion this feature introduces carries the `AC-PAR-` prefix (AC-PAR-8.7) |
| `turn/execute_test.go` (three new tests) | the running write is visible for the whole dispatch window; a refusal touches no session state and clears no failure (CONC-7) |
| `team/write_test.go` | batch validation, atomic write, symlink refusal, owned-path checks (SEC-12–15) |
| `team/marker_test.go` | root-file hash covers only the marker span (SEC-16) |
| `teamsource/render_test.go` | shared-agent `Teams` is unioned into `team.ExistingTeams`, never overwritten (SEC-17) |
| `cli/cmd_team_status_test.go` | `team status` exits `ExitOK` only when every record is in sync, `ExitFailure` on any drift, `ExitPrecondition` reserved for a bad `--repo` (STYLE-6) |
| `cli/cmd_team_update_test.go` (`TestTeamUpdate_MixedOutcomesInSingleRun`) | one refused record in a run does not block or hide a separately-applied record's update in the same run (STYLE-9) |
| `turn/execute_test.go` (three new tests) | the running write is visible for the whole dispatch window; a refusal touches no session state and clears no failure (CONC-7) |
| `turn/harvest_test.go`, `turn/subissues_test.go` (`TestSaveOnlyCommitsASucceededRun` and siblings) | `save` and `fileStoryIssues` commit/push only a genuinely succeeded run (CONC-8) |
| `harness/prompt_previous_failed_test.go` | `PreviousReason` is neutralised and framed inside a `<note>`, never interpolated raw; a cold prompt or a resume whose predecessor succeeded stays byte-unchanged (SEC-19) |
| `report/report_test.go` (`TestNonSuccessReportNamesTheWorktree`, `TestFailureAroundTheRunWithNoCommitNamesTheWorktreeOnce`) | the worktree is named in exactly one place per non-success report (STYLE-11) |
