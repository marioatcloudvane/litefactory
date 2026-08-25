# Manifest
# Branch: main
# Session: 2026-08-23

## Modes

- [x] **Research** — `RESEARCH_BRIEF.md` *(complete — supplied by user as `initialplan.md`, copied in directly; research mode skipped)*
- [x] **Plan** — `IMPLEMENTATION_PLAN.md` *(complete — scoped to M0 / LF-0..LF-5)*
- [x] **Implement** — code + tests *(complete)*

## History

- 2026-08-23 — session created; research skipped, user-provided `initialplan.md` adopted as RESEARCH_BRIEF.md
- 2026-08-23 — routing to plan-orchestrator
- 2026-08-23 — requirements-engineer wrote `feature-spec.md` (M0 only, LF-0..LF-5)
- 2026-08-23 — go-architect review: package layout, platform + security findings, 37-task decomposition; 7 items escalated for user decision
- 2026-08-23 — user settled all 7: D-A/D-B/D-C/D-D accepted; `identities.user_id` accepted; `<note>` framing pulled forward accepted; run-rate circuit breaker **rejected** for M0
- 2026-08-23 — proxy-product-owner pass: per-story bundles + coverage analysis; found 4 task-table defects (missing store accessors for `segments`/`runs`, missing deps on T-LF2.6 and T-LF4.3, dangling "D-E" reference, `daemon status` missing a DB dependency) — all corrected in the plan
- 2026-08-23 — `IMPLEMENTATION_PLAN.md` written; Plan mode complete
- 2026-08-23 — routing to implement-orchestrator; Phase 1 wave plan recorded in `notes.md`
- 2026-08-23 — Wave 1 dispatched: Story 0 (module bootstrap) ‖ T-LF3.0 de-risking spike
- 2026-08-23 — **T-LF3.0 spike complete — 9 binding design amendments** written to `SPIKE_LF3.0_DECISIONS.md` (SD-1..SD-9). These **supersede** the plan where they conflict. Headlines: `kill(-pgid)` does **not** reap claude's grandchildren (Bash tool `setsid`s into a new session) → subtree pgid sweep required, AC-3.19's mechanism is wrong; `--allowedTools` is **additive to the operator's `~/.claude/settings.json`** and is not a tool boundary → use `--tools` + `--settings`; `.subtype` reports `success` on `is_error:true` runs → branch on `is_error`, and a third "no result event at all" stall outcome needs a hard wall-clock timeout; `USER` is **required** for claude's Keychain auth, `SSH_AUTH_SOCK` is **not** needed by claude, and `ANTHROPIC_*` must **not** be forwarded by default (the plan's allowlist was self-cancelling — `ANTHROPIC_API_KEY` matches its own `KEY` strip pattern); `bufio.Scanner` **aborts** on `ErrTooLong` rather than truncating (real lines to 72,411 bytes) → `bufio.Reader.ReadBytes`; `--verbose` mandatory; claude binary must be an absolute path from config; daemon must be a **LaunchAgent in `gui/$UID`**, not a LaunchDaemon, because Keychain auth needs the GUI security session.
- 2026-08-23 — **User decision (SD-9):** machine has no SSH credential for GitHub (empty agent, no `~/.ssh` keys, `gh` unauthenticated, `origin` is HTTPS). Build the push mechanism per the spike's design (verified against a local sshd); **defer the real GitHub push test**. `T-DOD.1`/`AC-D.2` real-GitHub push reclassified from blocking automated acceptance to a **documented manual verification step with an environment prerequisite**.

- 2026-08-23 — Wave 2 complete, 4 commits: `beedc67` naming + comment parser, `bd40ac7` store/schema/all accessors, `9c2b330` `internal/git` Clone+Worktree+remote parser+gittest fixtures, `ed5c18c` config + secrets. Tree green: `gofmt`/`vet`/`build`/`test -race` clean. Config engineer also fixed a Story 0 spec deviation (`daemon.log` path was missing the `logs/` component per S-1).
- 2026-08-23 — Wave 3 dispatched (6 parallel): harness+prompt, github client, `git.Worktree` mutating half, redact, repos+`repo add`/`repo list`, LF-5 `logs`.

- 2026-08-24 — **Wave 3 attempt 1 lost to a machine sleep.** All six agents died mid-run; **no commits**, but ~6,500 lines of uncommitted work survived. Snapshotted to `scratchpad/wave3-salvage/`. Triage in `notes.md`. **Decision: salvage, not discard** — respawned all six with instructions to treat the on-disk files as an untrusted interrupted draft (verify against brief, rewrite what falls short, test). Orchestrator made one direct unblocking edit: declared the missing `git.ErrWorktreeUncommittedChanges` sentinel that `clone.go` already referenced and which broke `go build` tree-wide, blocking all six.
- 2026-08-24 — **User decision on lint:** option 2 — pin `golangci-lint` as a `go run` invocation wired into `make check`/`make lint`, not a system install and not accepted debt. Deferred to immediately after Wave 3 lands (avoids a 7th concurrent committer), then a full `golangci-lint run ./...` sweep feeding Phase 1.5.

- 2026-08-24 — **Wave 3 complete (respawn), 6 commits:** `c07607f` redact, `2442296` `git.Worktree` mutating ops, `a201603` github client, `e192ed0` LF-5 logs, `8ad6e52` LF-1 repos + `repo add`/`repo list`, `da85c15` harness + prompt. Harness self-caught and fixed the forbidden-flag violation (assembled from fragments, matching config's pattern).
- 2026-08-24 — **Independent pre-review sweep: green.** `gofmt`/`vet`/`build` clean; `go test -count=1` **14/14**; `go test -race -count=1` **14/14**. All 7 grep-able global invariants confirmed by direct grep (full table in `notes.md`). One advisory raised for the reviewer: `internal/gittest` execs `git`, a documented test-only carve-out from the "only `internal/git` may exec git" rule.
- 2026-08-24 — **golangci-lint pinned (`2f5eb1c`)** at v2.13.1 via `go run`, wired into `make lint`/`make check`, so **AC-B.1 is now verifiable on any machine**. First full sweep: **60 findings**, triaged in `notes.md` — substantive: 2 `nilerr` (possible swallowed errors), 2 unhandled `Close` on an error path, 3 missing `#nosec` justifications at deliberate subprocess/file-read sites, 3 `predeclared` (`max`); the remaining ~50 are `govet shadow` noise in tests plus one `gosec` G101 false positive on `redact`'s class-name table.
- 2026-08-24 — Wave 4a dispatched (5 parallel): `turn` prepare/run/harvest + `task clean`, `identity` + `init`, `launchd` + daemon start/stop/status, self-filter + classification, `report`. Wave 4b (post+advance, daemon loop) and a lint-remediation pass follow.

- 2026-08-24 — **Wave 4a complete, 5 commits:** `4a3f4ef` self-filter + classification, `2305a33` LF-0 identity + `init`, `c7aa1d5` LF-4.2 report, `668131a` LF-3.5/3.6/3.7 turn + `task clean`, `f41aabf` LF-2.7 launchd + daemon start/stop/status. 18 packages. Two transient cross-agent collisions in `internal/cli` (a test file moved aside by one engineer while another was writing it) resolved without loss; noted in `notes.md`.
- 2026-08-24 — **Flaky test diagnosed** (not retried-until-green): `internal/github` `TestContextCancellationStopsARequest` assumes a request reaches the handler within a 20 ms sleep; fails under parallel `-race` load. Test defect, not production defect. Assigned to the quality pass with a synchronise-don't-sleep fix.
- 2026-08-24 — **Design gap recorded for a future architect call:** files-touched (AC-4.4) comes from `git status` before staging, so a run that self-commits via the Bash tool reports an empty file list (branch-moved detection still correct). Needs a diff-between-two-SHAs helper in `internal/git`. Not blocking M0.
- 2026-08-24 — Wave 4b dispatched (2 parallel): T-LF4.3 post + advance (the LF-4 transaction), and a quality pass (flaky test + the 60 lint findings). Wave 4c = T-LF2.6 daemon loop, the last implementation task.

- 2026-08-24 — **Wave 4b complete:** `c3568e0` quality pass (flaky test de-slept, **lint backlog cleared — `make lint` now reports 0 issues**, so AC-B.1 is fully satisfied) and `6162237` T-LF4.3 post-and-advance.
- 2026-08-24 — **Watermark constraint verified and escalated into the Wave 4c brief.** `AdvanceWatermark` is `MAX()`-based and never rewinds (`watermarks.go:121`), so a daemon that keeps processing a task's queue after a failed report will advance the watermark **past the unreported comment**, which is then never retried — silently breaking AC-4.10. Required behaviour: stop a task's comment queue at the first failed report. Also noted: `Posted.CommentID` non-zero *with* an error means the comment reached GitHub but the transaction did not commit.
- 2026-08-24 — Wave 4c dispatched: **T-LF2.6 daemon tick loop + `daemon run`** — the last implementation task in M0.

- 2026-08-24 — **Wave 4c complete (`80e532b`): all 38 M0 tasks implemented.** 18 packages. Verified independently: `gofmt`/`vet`/`build` clean, `go test -race -count=1 ./...` **18/18 green**, `make lint` **0 issues**, no `notImplemented` stubs remain. **AC-D.8 confirmed** — `go version -m` reports `CGO_ENABLED=0`; `otool -L` shows only libSystem/libresolv/CoreFoundation/Security, standard for pure-Go darwin (`crypto/x509` roots), not cgo. **AC-B.1 now fully satisfied** — `make check` passes end to end for the first time.
- 2026-08-24 — **User decision: defer the real-repo end-to-end DoD check**, same treatment as SD-9's SSH deferral. `AC-D.2`'s real reaction/comment half and `T-DOD.1` become **manual-verification-pending**; mechanisms stay covered by the `httptest` mock GitHub server. Both deferrals recorded in `notes.md` as **deliberate reductions in M0 coverage, not oversights**. Net residual risk: **nothing has yet been exercised against real GitHub.**
- 2026-08-24 — **Phase 1.5 code review gate opened** — `go-code-reviewer` dispatched with the full advisory set (gittest execs-git carve-out, files-touched gap, the three spike amendments that superseded plan ACs, both DoD deferrals, and the daemon engineer's flagged startup-report-failure gap to characterise independently).
- 2026-08-25 — **Code review gate closed — ADVISORY_ONLY verdict.** `CODE_REVIEW.md` written. All 5 sub-agent checks complete (harness traps: CLEAN; cross-story: 2 advisory; idiom/resources: CLEAN + 1 stylistic; watermark state machine: CLEAN + 1 advisory rate-limit gap in `reportAbandoned`; worktree-escape: CLEAN — mechanically enforced by reflection + AST tests). No blocking findings. **M0 Implement mode complete.** Two advisory fixes recommended before M1: timestamp format alignment in `harness/meta.go`, rate-limit backoff in `reportAbandoned`.

## Notes

**Scope:** M0 walking skeleton only. Done when `/spike` on a real issue returns a real comment and an inspectable branch. LF-6..LF-18 (M1/M2/M3) are explicitly out of scope.

**38 tasks** across Story 0 (bootstrap) + LF-0..LF-5 + DoD. `T-LF3.9` was added by the plan pass — the architect's table had no data-layer task for the `segments` and `runs` accessors.

**Start with `T-LF3.0`**, the de-risking spike, before any other LF-3 work: it verifies `claude -p --output-format stream-json` under a non-tty child with a sanitised env, and `git push` over SSH from a launchd-started process. Both are external unknowns that could reshape T-LF3.1/T-LF3.2.

**Carried forward to M1:** the run-rate circuit breaker (rejected here, belongs with LF-11's blocked state), PR review comments as input, log rotation on `daemon.log`.

