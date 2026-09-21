# APP_CONSTRAINTS — litefactory

**Normative rules.** The descriptive lookup companion is [`APP_CONTEXT.md`](APP_CONTEXT.md).

The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**,
**SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **MAY** and **OPTIONAL** are to be
interpreted as in RFC 2119.

Rule identifiers are stable; **[enforced]** links to tests below. Architects read
in full; implementers read relevant sections. Deviations MUST be reasoned and
recorded in `.aiteam/AC_AMENDMENTS.md`.

---

## Sections

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
- **SEC-3** — `--tools` MUST define the Claude tool boundary; additive `--allowedTools` MUST NOT be relied on as one. Empty tool lists MUST be refused by configuration and runner construction because omitting `--tools` allows all tools.

- **SEC-4** — The claude child's environment MUST be an allowlist, never
  `os.Environ`. `internal/harness/env.go` permits `PATH HOME USER SHELL LANG
  LC_ALL TERM TMPDIR` and nothing else, each with a documented reason (`USER`
  is required or the Keychain lookup fails with "Not logged in"). `SSH_AUTH_SOCK`
  and `CLAUDE_CONFIG_DIR` MUST stay absent. `AllowedEnvNames()` is exported so a
  diagnostic can print the boundary.
- **SEC-5** — Report renderers MUST revalidate echoed input locally. `profileNamePattern` and `commandSpan` remain renderer-owned; `commandSpan` MUST stop at the first disallowed rune. Tool-refusal types without label fields MUST retain that boundary.

- **SEC-6** — Redaction MUST remain hygiene, not the subprocess secret boundary. Outbound bodies MUST use `redact.Redactor.Apply` and typed `redact.Body` mutation signatures; callers MUST NOT bypass them. Local `$LF_HOME/logs` copies MUST NOT be redacted.
- **SEC-7 [enforced]** — litefactory MUST NOT mutate a registered clone's
  checkout, index or HEAD. `git.Clone`'s method set is the S-6 allowlist
  (`Fetch WorktreeAdd WorktreeList WorktreeRemove RevParse RemoteURL ConfigGet
  ShowRef` + `Path`). There MUST be no `Commit`, `Checkout`, `Reset`, `Merge`
  or `Pull` on it. The narrow package-level `ObserveRetirement`,
  `GuardedRetirement` and `RetirementAbsent` capabilities MAY perform owned task
  maintenance: fresh fetch/prune, `merge-base --is-ancestor`, `ls-remote`
  (including `--symref origin HEAD` and exact task-ref absence), optional-locks-
  disabled status including ignored/untracked content, and `ls-files --stage -z`;
  unforced exact worktree removal (including stale owned registrations), local
  `update-ref --no-deref -d <task-ref> <expected-head>` and remote deletion with
  explicit `--force-with-lease=<task-ref>:<expected-head>`. The daemon MUST verify
  canonical ownership, closed issue and preservation under task/parent/clone
  coordination before these mutations. Each branch tip MUST be an ancestor of
  the fresh remote base; configured base/default branches and branches used by
  other worktrees MUST remain protected. No general clone mutation is authorized.
- **SEC-8** — Identity MUST be compared numerically, never by renameable handle (`internal/github/doc.go`, D-I).
- **SEC-9 [enforced]** — `gh` MUST be used for identity discovery only, reached
  only by `litefactory init`, with `GITHUB_TOKEN`/`GH_TOKEN` stripped from its
  child environment (`internal/identity`).
- **SEC-10** — `syscall.Umask(0o077)` MUST remain the first statement of
  `main`.
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
- **SEC-17 [enforced]** — Shared-agent `AgentRender.Teams` assembled in `teamsource.RenderTeam` MUST union `team.ExistingTeams(root, harness, agentKey)` before marker composition; replacing the list loses other teams’ attribution.

- **SEC-18** — `internal/team` MUST NOT import `internal/render` or
  `internal/teamsource`, preserving the `cli → teamsource → (team, render)`
  direction.
- **SEC-19** — Agent-authored `PromptInput.PreviousReason` MUST NOT enter resumed prompts raw. `writePreviousFailedNote` MUST apply `capText`/`neutraliseNoteTags` and `<note>` framing so prior failures cannot become prompt structure.

---

- **SEC-20 [enforced]** — Current and approved GitHub story text MUST remain untrusted delimited data on every cold/resumed/fallback prompt. Framing tags MUST be neutralized or JSON-escaped without truncating the complete approved contract; story text MUST NOT select paths, forge targets, credentials or permissions. Forge reads and mutations MUST remain factory-owned; runner context MUST NOT carry forge credentials.
- **SEC-21 [enforced]** — Proposal/outcome/index, working planning context and evidence access MUST be rooted in the supplied worktree with symlink-component refusal and stable descriptor/content checks. Plan targets MUST be validated before forge effects. Legacy verification MUST bind every criterion plus passing scoped review/tests to immutable snapshot bytes/digest. Separated engineering MUST bind frozen identity/revision, snapshot and factual executed checks without granting acceptance. Validate MUST bind complete criterion and required review outcomes to retained evidence; ambiguous/empty obligations MUST NOT pass. Validate MUST NOT edit or commission/run fresh checks. Explicit numeric-owner exceptions MAY waive any substantive obligation but MUST preserve failed/unverified facts and exact reasoned scope; agents MUST NOT grant waivers.

- **SEC-22** — Conflict advisors MUST return complete identified-region coverage, with missing/null resolutions refused and explicit empty text distinguished. Factory byte spans MUST preserve outside bytes. Collection MUST refuse excluded siblings, unsupported text/types/modes, over 100 files, over 8192 conflict bytes/file, configured total overflow or originals over 1 MiB. Original reads and aggregate 1 MiB advisor output MUST be bounded before allocation.
- **SEC-23** — Generation and apply MUST validate reconstructed text, Go syntax and raw digests/statistics, refusing results strictly below half baseline bytes OR logical lines; exact-half and empty baselines alone MUST NOT trigger shrinkage refusal. Legacy/unknown formats MUST refuse. Apply MUST match fresh canonical bytes and actual merges except ordinary marker labels, validate all candidates before writes, recheck containment and exact staged bytes/modes, and report failed raw-byte restoration honestly. Postcommit push failure MUST NOT undo the commit.
- **SEC-24** — Stored conflict data MUST contain regions/identities/metrics and at most bounded sanitized preview, not new raw originals. Preview MUST reserve baseline SHA, every raw metric row and omission notice before diffs, refusing generation if metadata cannot fit. Display MUST state sanitization and Go-only parsing limits; display text MUST NOT replace raw apply bytes.

## 2. Configuration (`CFG`) — `internal/config`

- **CFG-1** — Decoding MUST be strict: `yaml.Decoder.KnownFields(true)`, so an
  unknown or misspelled key is a startup error, not a silently ignored line
  (AC-0.10). A second YAML document MUST also be refused. `friendlyYAMLError`
  rewrites yaml.v3's Go-type phrasing into `unknown key "…"`.
- **CFG-2** — Defaults MUST be applied by decoding on top of `Defaults()`, so
  an explicit zero (`poll_interval_seconds: 0`) stays distinguishable from an
  absent key and is rejected, not silently replaced.
- **CFG-3 [enforced]** — The shipped template MUST match `Defaults()`. New keys MUST be represented in both defaults and `internal/config/template.go`; optional `tool_ceiling` and `tool_profiles` remain commented examples absent from a fresh configuration.

- **CFG-4 [enforced]** — `config.yaml` MUST NOT contain a secret, only a
  reference to one (a keychain service/account, or an env var *name*).
  `secretscan.go` reflects over every decoded string against the S-7
  credential shapes; a hit MUST be a hard startup error unwrapping to
  `ErrLiteralSecret` (exit 3). The matched text MUST NOT appear in the error —
  only the key and class.
- **CFG-5** — `Config.Validate()` MUST aggregate all independent validation faults using `errors.Join`; new validation MUST preserve this behavior.

- **CFG-6** — An out-of-range value MUST error, MUST NOT be clamped
  (`poll_interval_seconds` 10–60, `claude.timeout_seconds` 60–86400,
  `worktree_retain_days` 1–3650, `max_concurrent_runs` 1–16).
- **CFG-7** — `exec.LookPath` MUST NOT be used: launchd hands a LaunchAgent
  `PATH=/usr/bin:/bin:/usr/sbin:/sbin`, with no Homebrew. `config.ResolveExecutable`
  searches an explicitly passed PATH; its error MUST name every directory checked.
- **CFG-8** — Nothing outside `internal/config` MAY join a path against
  `$LF_HOME`; a new path MUST become a method on `config.Paths` (`ConfigPath
  DBPath LogsDir RunsDir DaemonLogPath LockPath SettingsPath`).

- **CFG-9** — `settings.auto_resume_on_session_limit` MUST default to false and use the loaded configuration; enabling it MUST NOT change manual scheduling or introduce hot reload.

---

## 3. Testing (`TEST`)

- **TEST-1** — Tests SHOULD be table-driven, MUST use stdlib `testing`, and
  SHOULD call `t.Parallel()` where safe. `go-cmp` is the only assertion
  helper, used sparingly.
- **TEST-2** — A mocking framework or a new module MUST NOT be added — no
  testify, no gomock, no mockery. Fakes MUST be hand-written structs. `go.mod`
  has exactly four direct requires (`go-cmp`, `go-github`, `yaml.v3`,
  `modernc.org/sqlite`) and SHOULD keep that shape.
- **TEST-3** — Shared fakes SHOULD extend existing fixture files: turn uses real git, migrated SQLite and a fake runner; daemon uses httptest GitHub, a real store and fake executor.

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
  SQLite, never a fake store (comment on `turn.ReporterOptions.DB`).
- **TEST-7** — Map-derived validation and output MUST be sorted or follow stable input order. Assertions and user diagnostics MUST NOT depend on Go map iteration.

- **TEST-8** — In `internal/daemon`/`internal/turn` concurrency tests,
  `time.Sleep`, an `Eventually` helper, or wall-clock polling MUST NOT be
  used. Rendezvous on channels, wait with `Daemon.Quiesce`, and bound every
  rendezvous with a timeout naming what was waited for.
- **TEST-9** — A worker-slot-release observable MUST NOT be invented for tests. Forge posts, watermark writes and completion logs occur before deferred pool release; tests needing a free slot MUST use real subsequent ticks.

- **TEST-10** — Failures in surface, purity, policy, clock and gate tests MUST be treated as design changes. Exported methods, interfaces, imports and critical sections MUST be updated deliberately, not by changing tests merely to pass.

---

## 4. Concurrency and resource discipline (`CONC`)

- **CONC-1** — Mutable slices MUST be copied at struct boundaries using slice/map cloning as appropriate, including nested context collections shared across concurrent turns.

- **CONC-2** — Goroutine budgets MUST be documented and MUST stay tiny:
  two in `harness/runner.go` (one per pipe), the loop in `daemon/daemon.go`,
  the second-signal watcher in `cmd/litefactory/main.go`, plus at most
  `max_concurrent_runs` (refused, never clamped, outside 1..16 — ceiling
  twenty) turn workers from `Daemon.offer`/`pool.try`. A new goroutine MUST
  say in a comment what bounds its lifetime. The pool's one termination
  condition is `Daemon.Quiesce` — not a shutdown `select`, not a sleep.
- **CONC-3** — An attempted operation that failed MUST return a result describing its outcome. Errors are reserved for operations that could not be conducted or recorded; reportable failures MUST NOT become silence.

- **CONC-4** — Reporting MUST render and post before acknowledging delivery or advancing watermarks. Execution outcome and recovery intent MUST be durable before announcement; delivery failure MUST NOT erase them.
- **CONC-5** — Every store accessor and every git method MUST take `ctx` first.
- **CONC-6** — Retries and sleeps MUST NOT appear below the daemon:
  `internal/github` retries nothing and sleeps nowhere; backoff is
  `internal/daemon`'s job.
- **CONC-7 [enforced]** — In turn preparation, `SetSessionState(ctx, session.ID, store.SessionRunning)` MUST remain the last statement before success and MUST NOT use `context.WithoutCancel`. Every earlier failure MUST leave session state untouched. Compensating state logic MUST NOT replace `ReconcileOrphans`.

- **CONC-8 [enforced]** — A run MUST be committed and pushed only when it
  genuinely succeeded. `save` (`internal/turn/harvest.go`) and `fileStoryIssues`
  (`internal/turn/subissues.go`) both gate on `runResult.Succeeded()` before
  touching git; any other outcome (failed, timed out, abandoned, no result) MUST
  be left uncommitted in its worktree for the next run on the task to pick up
  and commit once it does succeed.

---

- **CONC-9 [enforced]** — Managed child admission, approval and effects MUST preserve canonical parent identity and independent source consumption. Child discussion MUST refine only unfinished Planning scope and MUST receive a visible lock refusal after promotion; only explicit quiescent parent `replan` or `/plan` MAY reopen unfinished scope. Approval MUST atomically select all unfinished revisions and exclude pending refinements. Required freeze notices MUST be delivered before launch; labels, child comments and external reopening MUST NOT unfreeze scope.
- **CONC-10** — Progress ingestion and external effects MUST hold cross-process parent ownership without a SQL transaction across I/O. Incomplete ordered ingestion MUST defer effects. Intent MUST precede delivery and confirmed delivery MUST precede consumption. Uncertain responses MUST reconcile verified bot provenance/remote identity or block. Legacy newer invalidation MUST supersede stale closes; separated historical acceptance MUST NOT automatically invalidate or reopen after later code/main changes. Decision, report/body projection, closure and delivery intents MUST recover independently without duplicate or premature effects.
- **CONC-11** — Persisted workflow versions MUST preserve active features, replans and pending legacy effects. Legacy scoped acceptance/review/tests MUST close children immediately; invalidation MUST reopen them. Separated Implement MUST release dependencies on engineering completion with children/checkboxes open and finish only after coding, environment/fixes, passing executed tests and aggregate/save gates. Read-only Validate MUST own acceptance/code/architecture/plan review and close each satisfied or explicitly owner-waived story after ordered report/body projection, independently of siblings. Historical acceptance MUST retain its assessed snapshot without automatic expiry/reopening. Normal readiness MUST require every attached child closed and managed acceptance independently of remote state; reasoned owner exceptions MAY cover any substantive blocker. Follow-ups MUST require concrete-content approval and separate attachment approval. Merge MUST require explicit concrete PR/current-head/main approval, renewed after head changes; parent closure MUST follow confirmed integration into intended main. Exceptions MUST NOT fabricate facts or authorize merge. Mode suggestions MUST require complete engineering/save evidence or a concrete blocking finding; automatic mode chaining and parent-recovery reimplementation of completed stories MUST NOT occur.

- **CONC-12 [enforced]** — GitHub title/body MUST be the sole editable managed story source. Plan publication MUST preserve the complete contract, validate current bases, and persist a link-only index before readiness. Implement MUST use the frozen generation on cold, resumed and fallback attempts; live edits, history and local story files MUST NOT replace it. New managed plans MUST NOT create per-story Markdown files; legacy conversion is outside this workflow.

- **CONC-13** — Recovery MUST atomically persist semantic interruption and optional continuation before reporting, deduplicate by source run, and preserve context and original prerequisite identity. The root budget MUST count at most three automatic allocations; each MUST require fresh verified future-reset evidence. Suspended prerequisites MUST remain nonterminal until semantic settlement; only final success MUST release descendants, using final completion time.
- **CONC-14** — Automatic admission MUST use existing capacity and task locks, explicit resume without cold fallback, and frozen managed scope. New owner work, reset, changed context, closure or loaded disable MUST durably cancel stale pending work, including future work. Observation failure MUST defer without advancing the watermark. Restart MUST requeue only unallocated reservations; allocated abandoned runs MUST remain terminal absent durable quota evidence.

- **CONC-15 [enforced]** — Ordinary `needs_poll` backlog MUST clear after clean drain or confirmed explicit issue closure. Unknown states, lookup errors and failed clears MUST retain retry intent. Closure MUST preserve identity, state, watermarks and owed reports; skipped/error candidates MUST release admission without dispatch. Reopening MUST reuse normal discovery and retained watermarks. Worker capacity MUST NOT be described as a per-tick issue-read budget.
- **CONC-16 [enforced]** — Normal dirty recovery MUST defer base synchronization after up-to-date, disabled and distance-cap decisions, before merge preview. Preparation MUST preserve HEAD, raw index and retained bytes/modes; status errors MUST retain their cause and prevent launch. Durable outcomes, all prompt paths and owner reports MUST disclose `deferred_dirty`, divergence and no sync SHA without claiming a merge or prior success.
- **CONC-17 [enforced]** — Dirty confirmation MUST preserve work and the pending proposal without launching an agent, and MUST explain ordinary-comment recovery and fresh confirmation after revision changes. Merge MUST reject staged/tracked reserved-transport changes or branch/base reserved-path differences before mutation; ordinary untracked transport MAY coexist with safe synchronization.

- **CONC-18** — Every managed subprocess MUST receive a distinct durable attempt with its catalog pinned before launch, including refused resume/fallback. Observations, checkpoints, measurements and matching frozen valuations MUST update atomically; replay identities MUST deduplicate without merging distinct attempts. SQL transactions MUST NOT span subprocess or output I/O.
- **CONC-19** — Meter exports MUST snapshot selected attempts, coverage and matching measurement/valuation revisions before output, retaining the single SQLite connection pool. Preparation MUST enforce 1,000,000 rows, 256 MiB encoded CSV and 30 seconds (or earlier caller deadline), with bounded paging and cleanup on cancellation/error. No transaction or cursor MAY survive an output write.
- **CONC-20** — Price edits MUST publish immutable full catalogs and MUST NOT reprice existing attempts. Valuations MUST use exact decimal arithmetic and half-even rounding to 12 USD places per billable dimension; cache-duration and reasoning subsets MUST NOT be counted twice. Unknown quantities/rates MUST remain distinguishable from measured zero. Public all-in estimates MUST remain absent unless both pricing and collection are complete; observed subtotals MAY remain visible.
- **CONC-21 [enforced]** — Canonical modes MUST persist through runs, reports and journal retirement. Owner commands MAY replace one valid label; ambiguous labels MUST refuse. Transitions MUST record intent before effects, confirm canonical state before execution and preserve conflicting owner edits. Admitted retries MUST retain mode/tools; completion MUST NOT reapply labels. Managed authorization and frozen scope MUST remain authoritative; agents MAY recommend owner transitions.
- **CONC-22 [enforced]** — An authorized managed Plan iteration MUST invalidate prior readiness before fallible setup. Every subprocess attempt MUST receive a fresh outcome binding. Legacy discussion MUST save identity-bound working context, publish no stories and remain unready. Only a factory-admitted graph Plan MAY publish incomplete draft sub-issues through `lf_plan_story` while remaining unready; such publication MUST retain stable identity, immutable candidate/effect correlation, expected revisions and factory-bound parent/story authority, and MUST NOT satisfy complete/frozen implementation contracts. Graph stage promotion MUST require authenticated owner approval of the exact current candidate and upstream/shared-decision references; retirement MUST require exact owner confirmation and not-planned semantics. Completion MUST require the persisted content/submission contract's validation (legacy full-body proposal v1 or typed contribution v2), publication, link-only index replacement and save before readiness. Failed or discussion iterations MUST NOT restore stale readiness. Reserved transport MUST never enter commits.
- **CONC-23** — Research and Plan MUST permit material asynchronous questions by default, with explicit current no-question instructions taking precedence. Legacy infer MUST retain opt-in semantics; other modes MUST NOT gain permission. Current/previous policy MUST use the same derivation, and cold/resumed/fallback prompts MUST preserve effective mode, tools, task context and current question rules. Every subprocess MUST finish one final report without waiting. Identity-bound unfinished context MUST be reusable across dates without completed markers; ambiguous or foreign context MUST NOT be reused.

## 5. Style and code shape (`STYLE`)

- **STYLE-1** — Comments MUST explain why and remain true. When behavior changes, affected comments MUST be updated; use relevant decision/acceptance identifiers rather than restating code.

- **STYLE-2** — Lint rules that are not negotiable (`.golangci.yml`):
  `context.Background()` MUST NOT appear outside `main`; `fmt.Print*`/`log.*`
  MUST NOT be used (use the injected `*slog.Logger` or the command's output
  writer). `errcheck` checks type assertions; `gosec`, `errorlint`, `nilerr`,
  `noctx`, `unparam` are on. `govet` is `enable-all` minus `fieldalignment` and
  `shadow`.
- **STYLE-3** — New wiring SHOULD use an Options struct and validating constructor; missing dependencies SHOULD fail at construction rather than during post-run reporting.

- **STYLE-4** — Where a constructor would be overkill, a function field SHOULD be
  the injection seam: `secrets.Resolver{Keychain, LookupEnv}` has no
  `NewResolver`; `launchd.Manager` takes a `Runner`. Tests substitute the field.
- **STYLE-5** — Errors SHOULD expose typed fields and unwrap to a sentinel. Preserve `git.CommandError` command/stderr, `github.RateLimitError.RetryAfter`, and secret errors that disclose key/class but never value.

- **STYLE-6 [enforced]** — CLI handlers MUST capture bound flag values in `Command.New` closures instead of rereading flag sets. They receive positional arguments and `cli.Env`. Usage/precondition errors are reserved for invocation failures; domain findings (including drift) MUST use ordinary errors.

- **STYLE-7** — Before editing a package, agents MUST read its `doc.go` specification.
- **STYLE-8** — `internal/harness/prompt.go`'s `"- Mode: %s\n"` header line
  and `CLAUDE.md`'s opening line, which declares that header authoritative,
  MUST change together, in the same commit. No test catches the `CLAUDE.md`
  side drifting — `prompt_test.go`/`prompt_body_test.go` pin only the Go
  string. (Was `CLAUDE.md`'s `## Classify`; that section is gone, because
  litefactory pins the mode before any agent reads the file.)
- **STYLE-9** — Loops validating or writing independent items MUST aggregate every item’s failure with `errors.Join`/`joinSorted`, as in CFG-5 and SEC-12; one failure MUST NOT hide unrelated records.

- **STYLE-10** — `agent.RunResult.Succeeded()` (live subprocess) and
  `store.Run.Succeeded()` (persisted row, `internal/store/runs.go`) MUST NOT be
  collapsed into one call or converted into each other.
- **STYLE-11** — A non-success run report MUST name the uncommitted worktree in
  exactly one place: `metadataList`'s Commit-line default arm
  (`internal/report/run.go`, when `CommitSHA==""`) or `outcomeView.withFailure`
  (when `CommitSHA!=""`).

---

## 6. External binaries (`EXT`)

- **EXT-1** — `claude` and `git` are external binaries this codebase depends
  on; their behaviour MUST be probed read-only, in a scratch directory
  outside the repo, and written up. A test spawning a real `claude` MUST NOT
  be committed: slow, quota-consuming, flaky, and ungated.
- **EXT-2** — A probe write-up MUST state the verified binary version and re-verification steps. Claude invocation changes SHOULD extend the existing session/resume or tool-surface probe.

- **EXT-3 [enforced]** — Quota classification MUST use provider-owned terminal refusal evidence, never generated prose or warnings. Every known blocking reset MUST be usable and future; the latest bound wins. Claude Unix seconds MUST use reset+60 elapsed seconds; Codex machine-local displayed minutes MUST use the next-minute boundary+60 seconds without changing the child timezone. Ambiguous dates, DST gaps/folds or inconsistent local context MUST remain report-only.

- **EXT-3 [enforced]** — Codex generic `error` notices MUST remain raw-log diagnostics, separate from final agent content. Success MUST require exit zero, `turn.completed` and no explicit `turn.failed`; explicit failure MUST stay sticky regardless of terminal ordering or malformed optional detail. Zero-exit failure reasons MUST describe the stream evidence, not a failed process exit.

- **EXT-4** — Before criteria freeze, planners MUST apply the [nested-agent evidence decision](../docs/decisions/nested-agent-evidence.md) and inventory proof method, source availability, version/context bounds and missing prerequisites. Unsupported in-run nested Codex observation MUST NOT be required. Missing authentic evidence MUST remain factually unverified and failed checks MUST remain failed, including after any explicit reasoned owner exception. Separated workflow MAY accept waived obligations without fabricating a pass; legacy behavior MUST remain version-gated. Frozen contract edits MUST use factory-mediated replanning. Validate MUST assess retained checks without rerunning or commissioning them.

---

- **EXT-5** — Provider collection MUST preserve process outcomes and diagnose unknown actual models, unverified resume baselines and missing descendants. Configured models and inclusive totals MUST NOT substitute for exclusive source-proven attribution. Authentic versioned captures MAY be deferred by explicit owner instruction; deferred evidence MUST NOT certify complete provider support. Automated tests MUST NOT launch paid providers.

## Enforcement

| Test | What it pins |
|---|---|
| `config/purity_test.go` | Config purity; SEC-1 |
| `git/surface_test.go` | Read-only clone API; SEC-7 |
| `git/construction_internal_test.go` | `Worktree` is built in exactly one place |
| `git/exec_internal_test.go` | Deadlines, allowlisted child environment and PATH fallback (SEC-4, CFG-7) |
| `github/surface_test.go` | Redacted mutations, pinned API inventory, consumer interfaces and token boundary (SEC-6, TEST-5) |
| `identity/policy_test.go` | Identity-only gh execution (SEC-9) |
| `gittest/policy_test.go` | no non-test file imports `internal/gittest` |
| `repos/network_test.go` | Offline repository registration |
| `config/template_test.go` | Template decodes to Defaults (CFG-3) |
| `store/store_test.go` | Migration, replay and report transaction invariants |
| `store/runs_test.go` | Persisted success predicate (STYLE-10) |
| `daemon/clock_test.go` | Only Tick reads the clock |
| `turn/gate_test.go` | CloneGate acquisition sites and exclusion of model calls |
| `daemon/gate_internal_test.go`, `pool_internal_test.go`, `inflight_internal_test.go` | Bounded worker pool and per-clone exclusion (CONC-2) |
| `repos/collision_test.go` | two registered repos may not resolve the same worktree directory |
| `daemon/record_test.go` | Concurrency and acceptance-identifier invariants |
| `turn/execute_test.go` (three new tests) | the running write is visible for the whole dispatch window; a refusal touches no session state and clears no failure (CONC-7) |
| `team/write_test.go` | batch validation, atomic write, symlink refusal, owned-path checks (SEC-12–15) |
| `team/marker_test.go` | root-file hash covers only the marker span (SEC-16) |
| `teamsource/render_test.go` | Shared-agent team membership union (SEC-17) |
| `cli/cmd_team_status_test.go` | Domain versus precondition exits (STYLE-6) |
| `cli/cmd_team_update_test.go` | Independent outcomes aggregate (STYLE-9) |
| `turn/harvest_test.go`, `turn/subissues_test.go`, `turn/plan_retry_transport_test.go` | Success-only save, publication failure and transport exclusion (CONC-8) |
| `harness/prompt_previous_failed_test.go` | Untrusted failure-reason framing (SEC-19) |
| `report/report_test.go` | One worktree location per failure (STYLE-11) |
| `harness/approved_stories_test.go`, `plan_prompt_security_test.go`, `turn/approved_attempts_test.go` | Complete untrusted revision context across cold/resume/fallback (SEC-20) |
| `turn/plan_index_test.go`, `agent/story_progress_contract_test.go`, `validation_test.go`, `planbrief/acceptance_projection_test.go` | Rooted immutable evidence, retained-check assessment and guarded checkbox projection (SEC-21) |
| `store/approved_stories_test.go`, `turn/managed_scope_test.go`, `daemon/story_refinements_test.go` | Atomic approval, child policy and independent consumption (CONC-9) |
| `store/story_progress_lock_test.go`, `turn/story_progress_test.go`, `story_ack_recovery_test.go`, `acceptance_test.go`, `followups_test.go` | Parent ownership and ordered, recoverable legacy/acceptance/publication effects (CONC-10) |
| `turn/story_parent_recovery_test.go`, `managed_lifecycle_test.go`, `engineering_progress_test.go` | Legacy closure, separated engineering and recoverable completion (CONC-11) |
| `turn/plan_publication_test.go`, `plan_index_test.go`, `approved_attempts_test.go` | Complete publication, link-only index and frozen execution source (CONC-12) |
| `codex/recovery_test.go` | Recoverable diagnostics, sticky terminal failure, final content and process/context precedence (EXT-3) |
| `agent/conflict_regions_test.go` | Identified-region coverage and output bounds (SEC-22) |
| `git/worktree_regions_test.go` | Preservation, staged barriers and honest restoration (SEC-23) |
| `report/conflict_preview_test.go` | Complete metadata and safe bounded preview (SEC-24) |
| `codex/quota_test.go`, `harness/quota_test.go` | EXT-3 |
| `daemon/backlog_closure_test.go`, `daemon/backlog_test.go` | Durable closure retirement, retry retention, admission release, report preservation and reopening (CONC-15) |
| `turn/deferred_dirty_test.go`, `store/deferred_sync_test.go`, `harness/deferred_sync_test.go`, `codex/deferred_sync_test.go` | Preserved dirty recovery, policy precedence, persistence and all-path disclosure (CONC-16) |
| `turn/dirty_confirmation_recovery_test.go`, `git/reserved_sync_test.go` | Pending confirmation recovery and read-only transport collision refusal (CONC-17) |
| `daemon/mode_retention_test.go`, `daemon/mode_transition_test.go`, `store/mode_transition_test.go` | Persistent selection, transitions and independent child retirement (CONC-21) |
| `planbrief/outcome_test.go`, `turn/plan_outcome_test.go` | Strict attempt binding, readiness and rooted context/transport safety (CONC-22, SEC-21) |
| `agent/usage_test.go`, `meter/tokens_test.go`, `meter/reconcile_test.go` | Usage ownership, nullable dimensions and replay semantics |
| `store/meter_test.go`, `meter_prices_test.go`, `turn/meter_test.go`, `meter_price_test.go` | Attempt durability, pinned catalogs and frozen valuations (CONC-18–20) |
| `cli/meter_test.go`, `meter_endtoend_test.go`, `report/meter_test.go` | Coherent export, cancellation, uncertainty and exact observed subtotals (CONC-19–20) |
| `harness/usage_test.go`, `codex/usage_test.go` | Conservative synthetic collector behavior, not authentic provider certification (EXT-5) |
