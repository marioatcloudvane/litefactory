# Implement Mode — Notes
# Branch: main | Session: 2026-08-23 | Feature: lf-setup

## Wave plan

The plan's 38 tasks have a real dependency graph, so Phase 1 runs as dependency-ordered
waves rather than one-agent-per-story in a single parallel burst.

| Wave | Contents |
|---|---|
| 1 | Story 0 (T-BOOT.1/2/3) ‖ T-LF3.0 de-risking spike |
| 2 | LF-0 (config/store/secrets/identity/github-core/init) ‖ T-LF2.1 `naming` ‖ T-LF1.1+T-LF1.2 `git` runner/Clone + URL parser |
| 3 | LF-1 rest ‖ github issues/comments + store accessors ‖ harness/prompt/allowed_tools ‖ redact ‖ command parser |
| 4 | worktree, turn prepare+harvest, report+post, daemon loop, launchd, task clean, logs |

`.aiteam/APP_CONTEXT.md` did not exist at the start of this run — created at Exit via
`app-context-updater` in **init** mode.

## Blockers

- **SSH to GitHub unavailable on this machine** (no keys, empty agent, `gh` unauthenticated,
  `origin` is HTTPS). Resolved by user decision SD-9: build the mechanism, defer the real
  push test. Not blocking implementation.

## Decisions taken during implementation

- **T-LF3.0 spike produced 9 binding amendments** — see `SPIKE_LF3.0_DECISIONS.md`.
  They supersede `IMPLEMENTATION_PLAN.md` where they conflict. Passed verbatim to every
  engineer touching `harness`, `git`, `launchd` and `config`.
- Three of them invalidate plan text rather than merely refining it: AC-3.19's
  Setpgid-based kill mechanism (SD-1), AC-3.14/3.15's treatment of `--allowedTools` as the
  tool boundary (SD-2), and the claude env allowlist, which was self-cancelling (SD-4).
  These are flagged for the drift report.
- **Spec deviation found and fixed mid-flight:** Story 0's placeholder `daemonLogPath()`
  pointed at `$LF_HOME/daemon.log` instead of `$LF_HOME/logs/daemon.log` per S-1. Caught by
  the config engineer, fixed in `ed5c18c`. Worth noting that a *later* engineer caught an
  *earlier* engineer's spec deviation — the per-package briefs carrying S-1 verbatim are
  what made that possible.

## Wave 3 attempt 1 — lost to machine sleep (2026-08-24)

All six Wave 3 agents died mid-run when the machine slept. **No commits**, but ~6,500 lines
of uncommitted work survived on disk. Snapshotted to
`scratchpad/wave3-salvage/{modified.patch,untracked.tgz}` before touching anything.

Triage at the time of the respawn decision:

| Package | Lines | Tests | Verdict |
|---|---|---|---|
| `internal/git` (+commit/push/status) | 2494 | **passing** | salvage |
| `internal/redact` | 1416 | 1 failure (`t.Setenv` in a `t.Parallel` test) | salvage |
| `internal/github` | 1355 | none | salvage, needs tests |
| `internal/harness` | 885 | none | salvage, prompt assembly missing |
| `internal/cli/cmd_logs.go` | 758 | **broke** `TestLogsCommandReachesItsHandler` | salvage, needs repair |
| `internal/repos` | 349 | none | thin — mostly redo |

**Decision: salvage rather than discard.** Agents respawned with instructions to treat the
existing files as an *untrusted draft from an interrupted colleague* — verify against the
brief, rewrite what does not meet it, finish and test. Explicitly not "trust and continue".

Orchestrator made one direct edit to unblock the respawn: `internal/git/errors.go` was
missing the `ErrWorktreeUncommittedChanges` sentinel that `clone.go` already referenced,
breaking `go build` **tree-wide** and therefore blocking all six agents in packages they do
not own. Declared it in the established style. Flagged to the `internal/git` agent as
provisional and subject to its review.

## MANDATORY pre-review gate checks

These must be verified green **before** Phase 1.5 opens. Each is a global invariant with an
executable test, so none can escape a full-suite run — but each was observed violated at
least once, so they are listed explicitly rather than trusted.

1. **`TestForbiddenFlagIsAbsentFromTheTree`** (`internal/config/purity_test.go`).
   Observed failing during Wave 3: the salvaged `internal/harness/runner.go:295` contained
   the permission-skipping flag **inside a comment asserting it is never constructed** —
   the exact trap the invariant exists to catch. The test fires correctly (an earlier
   `ok` was a stale build cache; `-count=1` reproduces it).
   **Fix pattern is already established** in `internal/config/policy.go`:
   ```go
   func forbiddenFlag() string { return "--dangerously-" + "skip-permissions" }
   ```
   assembled from fragments precisely so the whole string appears nowhere as a literal and
   the guard does not defeat its own test. Any harness reference must do the same.
   *Could not be pushed to the running harness agent — no `SendMessage` tool is provisioned
   in this orchestrator session. Enforced at the sweep instead.*
2. `TestValidationHasNoThirdPartyImports` — `internal/config` validation stays stdlib-only.
3. `internal/redact` and `internal/naming` zero third-party imports.
4. Exactly one `recover` in the tree, in `internal/redact`.
5. `gh` invoked only from `internal/identity`.
6. No `context.Background()` in any request path.
7. Goroutine inventory is exactly four.
8. `internal/gittest` imported only by tests.

## Pre-review sweep results (2026-08-24, after Wave 3)

Verified independently rather than taken from agent reports.

**Green:** `gofmt` clean · `go vet` clean · `go build` clean · `go test -count=1 ./...`
**14/14 packages** · `go test -race -count=1 ./...` **14/14 packages**. Cache disabled
deliberately — an earlier `ok` on `internal/config` was a stale cache masking a real failure.

**Grep-able invariants — all confirmed:**

| Invariant | Result |
|---|---|
| Permission-skipping flag absent from tree | **clean** (harness self-fixed; assembled from fragments) |
| Exactly one `recover()`, in `internal/redact` | **confirmed** — `redact.go:190`; the `store.go:152` hit is a comment saying rollback is *not* recover-driven |
| No `context.Background()` in a request path | **confirmed** — only `main.go:42` creating the root context (correct) and one comment |
| `gh` invoked only from `internal/identity` | **confirmed** — the `identities.go` hit is the S-2 schema constant `SourceGH = "gh"`, not an invocation |
| `pkg/` absent | confirmed |
| No `forge.go` / `Capabilities` struct | confirmed |
| `internal/gittest` imported only by tests | confirmed |

*Advisory for the reviewer:* `internal/gittest` does exec `git` (fixture builder), which is a
carve-out from "internal/git is the only package that may exec git". It is test-only and
`internal/git/doc.go:24` acknowledges it explicitly. Judged legitimate; flagging so the
reviewer rules on it rather than discovering it.

### golangci-lint v2.13.1 — 60 findings, triaged

Now pinned in the Makefile and run via `go run` (commit `2f5eb1c`), so AC-B.1 is verifiable
on any machine. Findings feed Phase 1.5.

**Substantive (fix):**
- **`nilerr` ×2** — non-nil error returned as nil. `cli/cmd_logs.go:601`, `gittest/policy_test.go:50`. Possibly deliberate (tailer treating a read error as "no more data"), but must be verified and annotated, not left ambiguous.
- **`gosec` G104 ×2** — unhandled `tmp.Close()` in `harness/settings.go:177,181`, on what looks like an error path.
- **`gosec` G204 `harness/runner.go:335`** — subprocess with tainted args. This is *by design* and it is **the** security boundary; it needs an explicit `#nosec` with reasoning so a reviewer sees a deliberate decision rather than silence.
- **`gosec` G304 ×3** — `cmd_logs.go` reads files by variable path (paths come from the DB litefactory itself wrote). Annotate with justification.
- **`predeclared` ×3** — parameter named `max` shadows the Go 1.21+ builtin. `cli/parse.go:30`, `redact/truncate.go:65,106`.

**Noise / low value:**
- **`govet shadow` ×40** — nearly all `err` shadowing in `store_test.go`. Notoriously noisy linter.
- **`gosec` G101 `redact/class.go:55`** — **false positive**: `classNames` is the array of redaction *class* names, so it necessarily contains credential-ish words. Annotate.
- **`staticcheck` ×6** — `parser.ParseDir` deprecated (used by the invariant-checking tests) plus three style suggestions.
- **`gocritic` ×1** — `strings.Cut` suggestion in `git/remote.go:120`.

## Known design gap — files-touched when the agent self-commits

Raised by the `internal/turn` engineer. **Not blocking M0; flagged for a future architect call.**

`Result.Changes` (the files-touched list, AC-4.4) is derived from `git status` **before**
staging. If a run commits *for itself* via the Bash tool, then:

- branch-moved detection still works (`SHAAfter != SHABefore`), so the report is not wrong
  about *whether* something happened;
- but the **files-touched list will be empty**, because by the time `Status` runs there is
  nothing unstaged left to see.

`internal/git` has no diff-between-two-SHAs helper, which is what a correct fix needs
(`git diff --name-status <sha_before>..<sha_after>` plus untracked). Adding one is small,
but it changes what AC-4.4 means and so is an architect call, not an engineer's.

M0 impact is limited: the plan's own framing of AC-4.4 is a plain git-derived list between
`sha_before` and the run's commit, and `/spike` runs are not expected to self-commit.
Carried to the drift report so it is a recorded decision rather than a silent gap.

## Process note — concurrent edits to `internal/cli`

`internal/cli/cmd_daemon_agent_test.go` briefly vanished mid-session: the `turn` engineer
moved it aside to unblock its own `go test ./internal/cli/...` run (it did not compile —
`undefined: itoa`), while the `launchd` engineer was still writing it. The launchd engineer
rewrote it and committed (`f41aabf`); verified green afterwards.

No work was lost, but it is the second time parallel agents have collided in `internal/cli`.
**Mitigation already in force:** each engineer is told to edit only its own `cmd_<group>.go`
and never `commands.go`. That held — the collision was in a *test* file, which the file-
ownership rule did not explicitly cover. Worth extending the rule if there is another wave.

## Flaky test found (2026-08-24) — `internal/github`

`TestContextCancellationStopsARequest` failed once in a full `-race -count=1 ./...` run,
having passed the previous sweep. Diagnosed rather than retried-until-green:

- Failure duration **5.07s** pinpoints the `time.After(5 * time.Second)` branch — the
  server never observed the cancellation.
- Cause: the test `time.Sleep(20 * time.Millisecond)` then cancels, **assuming the request
  has reached the handler within 20 ms**. Under parallel `-race` load across 18 packages
  that does not hold; the context is cancelled before the request is dispatched, the
  handler never runs, `released` is never closed, and the test times out.
- Isolated re-run: `-count=5` passes. **Test defect, not a production defect** — the
  `errors.Is(err, context.Canceled)` assertion is fine; the synchronisation is not.

Fix is to replace the sleep with a signal from inside the handler (close an `entered`
channel, cancel on receipt), removing the timing assumption entirely. Assigned to the
quality pass.

Recorded rather than re-run-until-green: an intermittent failure that only appears under
load is exactly the kind that gets dismissed as "flaky, ignore it" and then fails CI weekly.

## Watermark constraint on the daemon tick loop — verified, load-bearing

Raised by the post-and-advance engineer; **verified directly**: `AdvanceWatermark` is
`last_comment_id = MAX(watermarks.last_comment_id, excluded.last_comment_id)`
(`internal/store/watermarks.go:121`). It **monotonically advances and never rewinds**.

The consequence is a real correctness hole in the *daemon*, not in the store:

> If a task's comment queue keeps being processed after an earlier comment failed to post,
> a later successful `Consume` advances the watermark **past the unreported comment**, and
> that comment is **never retried**.

That silently breaks AC-4.10's retry guarantee — the failure is invisible, because the
watermark looks healthy and the missing report is simply never noticed.

**Required daemon behaviour:** within a single task's comment queue, stop at the **first**
comment whose report failed to post; leave the rest for a later tick. `Posted.Skipped` and
the error returns from `ReportRun` / `ReportReply` / `Consume` are the per-comment signal to
check **before** moving to the next comment of the same task. Other tasks in the same tick
are unaffected — the constraint is per-task, not per-tick.

Note also `Posted.CommentID` is non-zero **as soon as the post returns**, so a non-zero id
*alongside* an error means the comment reached GitHub but the recording transaction did not
commit. The daemon must not treat that as "nothing happened".

Carried into the Wave 4c brief as a first-class requirement with its own test.

## DoD coverage reductions — both deliberate user decisions

Two halves of the Definition of Done are **deferred to manual verification**, not met by
automated gates. Both are intentional and both belong in the drift report as **real
reductions in M0 coverage, not oversights**.

| # | Deferred | Reason | What IS covered automatically |
|---|---|---|---|
| SD-9 | `AC-D.2` real `git push` to GitHub over SSH | Machine has no SSH credential for GitHub: empty agent, no `~/.ssh` keys, `gh` unauthenticated, `origin` is HTTPS | Push mechanism verified end-to-end against a **local sshd / bare repo fixture** |
| — | `AC-D.2` real-repo 👀 reaction + report comment; `T-DOD.1` end-to-end on a real private repo | User elected not to provision a throwaway repo + bot credential for M0 | Reaction/comment mechanism verified against the **`httptest` mock GitHub server** throughout Waves 3–4 |

**Environment prerequisites for the manual pass**, when someone runs it: an SSH key
registered with GitHub, a throwaway private repo, and a bot credential in the keychain
(per D-D, keychain is the only supported source for `daemon start`).

Net effect: every M0 mechanism is tested, but **nothing has yet been exercised against real
GitHub**. That is the single largest residual risk in the milestone and should be stated
plainly rather than buried — the first real run is still the first real run.

> **Superseded 2026-08-25 — the first real run happened.** See *Closed against real
> GitHub* below. The paragraph above is kept as written because it was true when written.

## Closed against real GitHub (2026-08-25)

The full M0 loop ran end to end on `marioatcloudvane/litefactory#1`, against the real
GitHub API, as the real bot account:

```
claude run finished       outcome=succeeded exit_code=0 duration=1m8.5s cost_usd=0.483
run committed and pushed  commit=4eb1445 files=1
posted the run report     comment_id=5409367450 bytes=2248 redactions={total:0}
turn complete             disposition=ran pushed=true reported=true
```

That closes all three deferred items in the table above:

- **SD-9 — real `git push`.** Done, and the SSH prerequisite turned out to be wrong.
  `origin` is HTTPS and git's credential helper supplies the token, so no SSH key is
  needed. The "environment prerequisites" paragraph above overstates what a real run
  requires: a throwaway repo and a keychain bot credential, yes; an SSH key, no.
- **Real-repo 👀 reaction.** Observed on the triggering comment, authored by
  `botatcloudvane`.
- **Real report comment.** Posted as the bot, marker intact, zero redactions.

## Defects and constraints the first real run exposed

- **`Bash(make:*)` is missing from `defaultAllowedTools`.** The agent could not run the
  project's own verification command and honestly reported the verification as
  outstanding. Compounding it: `litefactory init` writes the defaults *into* the user's
  `config.yaml`, so fixing the template never reaches an existing install — the fix needs
  a migration path, not just a new default. Note the counter-argument recorded in
  `comment-intent`: the Makefile is `Edit`-able inside the work area, so a blanket
  `Bash(make:*)` widens the boundary more than it looks. Enumerating the Go toolchain
  commands individually is the safer shape.
- **A fine-grained PAT cannot serve as the bot credential.** It can only reach
  repositories owned by the token's own resource owner, so a bot account's fine-grained
  token can never name a repo owned by a different user, no matter what collaborator
  rights the bot has. Symptom: `404` on every repo and issue read while `GET /user`
  succeeds. A **classic PAT with `repo` scope** is required. Worth stating in the setup
  documentation, because the failure looks like a permissions mistake rather than a
  structural limit.
- **A Bash tool without an Aqua security session cannot read the keychain.**
  `launchctl managername` returns `Background`; metadata reads succeed, secret reads fail
  with `errSecInteractionNotAllowed` (`-25308`, exit 36). This is precisely why the
  LaunchAgent targets `gui/$UID`, and it means the daemon must be started from the user's
  own terminal, not from a tool subprocess.

## Sequencing traps a first-time operator will hit

Each of these cost a cycle during the first live run. None is a bug; all three are
worth documenting.

- A `/spike` posted in the same poll window as the issue's creation is swallowed by
  watermark planting (AC-2.10) — first sight skips every comment that already exists.
- An instruction placed in the issue **body** does nothing. litefactory reads comments,
  never bodies (AC-2.19 / `TestIssueBodyAloneStartsNothing`).
- Since 2026-08-25 the third trap is gone: a comment that did not start with `/spike` was
  consumed silently. It now starts a run in `infer` mode — see
  `.aiteam/main/2026-08-25/comment-intent/`.

## Open risks

- **`golangci-lint` is not installed on this machine.** `make check` includes it, so the
  `check` target cannot currently pass end to end — and `make check` *is* acceptance
  criterion AC-B.1. **User decision: option 2** — pin it as a `go run` invocation (tool
  pin, not a module dependency of the shipped binary) and wire it into `make check` /
  `make lint`. Full `golangci-lint run ./...` sweep to run once Wave 3 lands, with findings
  fed into the Phase 1.5 code review gate.

## Code Review Advisories

_(pending Phase 1.5)_
