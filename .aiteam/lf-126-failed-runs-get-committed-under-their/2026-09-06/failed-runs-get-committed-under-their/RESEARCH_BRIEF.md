# Research Brief — lf-126-failed-runs-get-committed-under-their

## Request

From `github:marioatcloudvane/litefactory#126`, "Failed runs get committed under their failure
text, and the next run claims their work."

Reported symptom (as stated in the issue; verified against the codebase below):
- litefactory commits a run's worktree even when the run failed or timed out, using the run's
  failure/result text as the commit message.
- A later run then finds the failed run's code already committed and reports success for work it
  did not itself do.
- Related: failed/timed-out runs record `cost_usd: 0` and zero token usage because no `result`
  event is emitted, understating aggregate spend.
- Evidence cited: issue #100 ("/schedule" feature) — commit `c6fb438` (failed run 100/0006, 2590
  insertions, commit message literally a ChatGPT usage-limit error string) and commit `9f6bd43`
  (succeeded run 100/0007, falsely claimed "Implemented the scheduling feature end to end" while
  only adding two markdown files). Run metadata under
  `~/.litefactory/logs/runs/github/marioatcloudvane/litefactory/100/`.

## Codebase Context

litefactory is a Go monorepo. `cmd/litefactory` is the entry point; business logic lives under
`internal/` (`turn`, `harness`, `agent`, `report`, `git`, `store`, `codex`, `daemon`, …).
`internal/turn` is the orchestration core that turns a GitHub comment into a run: `turn.Execute`
(`internal/turn/execute.go:383`) → `prepare` (`internal/turn/prepare.go:88`) → `runAndHarvest` /
`save` / `commitAndPush` / `finish` (`internal/turn/harvest.go`). `internal/report` is a pure
rendering package for the GitHub comment body (`internal/report/run.go:53-56`) — no I/O of its own.
`internal/git` and `internal/store` are leaf packages for git plumbing and SQLite persistence
respectively. `internal/harness` drives the Claude CLI subprocess and populates a vendor-neutral
`agent.RunResult` from the terminal `result` JSONL event on the subprocess's stdout stream, when
one arrives.

Two outcome vocabularies exist side by side: `agent.Outcome` (5 values, used in-memory during a
run) and `store.RunStatus` (4 values, persisted: `RunRunning`, `RunSucceeded`, `RunFailed`,
`RunReported`). [V]

## Domain / Problem Space

Tracing "where does litefactory decide to commit, where does the commit message come from, where
is outcome decided, and where does the false-success reporting come from" surfaces **two
independent bugs that compound**, not one:

### Bug A — commit is unconditional on outcome

- `save()` (`internal/turn/harvest.go:236-250`) gates the commit+push step on
  `harness.OutcomeAbandoned` only — every other outcome, including `OutcomeFailed` and a
  timeout/crash, proceeds to commit. [V]
- `commitAndPush()` (`internal/turn/harvest.go:282-361`) builds `git.CommitOptions{Summary:
  runResult.ResultText}` unconditionally — the failure text (or, absent a `result` event entirely,
  a generic empty-string fallback resolved by `sanitizeSummary`) becomes the commit message
  regardless of whether the run succeeded. [V]
- `git.Worktree.Commit` (`internal/git/commit.go:133-183`) has no outcome/status parameter in its
  signature at all — by design it cannot refuse to commit based on run outcome; that decision has
  to be made by its caller, and today the caller doesn't make it. [V]
- `finish()` (`internal/turn/harvest.go:369-434`) calls `DB.SetRunCommit` whenever
  `result.CommitSHA != ""`, again with no status check. [V]
- `DB.SetRunCommit` (`internal/store/runs.go:306-315`) is an unconditional `UPDATE ...
  commit_sha` by run id — it has no status parameter to consult even if the caller wanted one. [V]
- `DB.FinishRun` (`internal/store/runs.go:261-300`) writes `commit_sha = COALESCE(?,
  commit_sha)` — so a commit SHA attached before the run's terminal status is written survives
  being marked `RunFailed`. Nothing clears or flags a commit that belongs to a failed run. [V]
- Net effect: a failed or timed-out run that produced a diff gets that diff committed to the
  worktree under a commit message that is the raw failure text (or, when no result text exists at
  all, a generic placeholder message) — this is exactly the `c6fb438` evidence in the issue. [V]

### Bug B — the next run resumes the failed conversation without regard to its outcome

- `previousRun()` (`internal/turn/prepare.go:412-474`) selects the most recent run of the segment
  satisfying `terminalRunStatus(run.Status)` as "the conversation to continue." [V]
- `terminalRunStatus()` (`internal/turn/prepare.go:769-782`) returns true for `RunSucceeded`,
  `RunFailed`, **and** `RunReported` alike. The doc comment states the intent explicitly (verbatim,
  lines 769-772): *"Failed counts as much as succeeded: claude was invoked either way, and a
  conversation that went badly is still a conversation the next run should be continuing rather
  than repeating."* This is a deliberate design choice, not an oversight. [V]
- `resumeDecision()` (`internal/turn/prepare.go:476-503`) then decides whether to actually resume
  Claude's own CLI session. For the Claude path, resume is `true` whenever a previous run was
  `found` and `segment.Agent == resolvedKind` — **the previous run's outcome/status is never
  consulted** on this path. (Non-Claude tools additionally require a non-empty
  `segment.ResumeToken`, but still no outcome check.) [V]
- This flows into `promptInput.Resume` (`prepare.go:248`) and `agent.PathResume`
  (`prepare.go:260-263`) — Claude is invoked with its own `--resume`/session id, so it continues
  the same conversation it had during the failed run, complete with whatever it already believed
  it had accomplished and already sees committed in the worktree from Bug A. [V]
- The next run's own outcome is then determined independently and correctly by `Result.Succeeded()`
  (`internal/turn/result.go:236-239`: `DispositionRan && OutcomeSucceeded && Err == nil`), and
  `report.describeRunOutcome()` (`internal/report/run.go:422-476`) renders that outcome faithfully.
  **The reporting layer is not buggy** — it accurately reports that *this* run succeeded; the
  falseness is that Claude, resuming a session that already believes prior (failed-run) work is
  done, narrates the *cumulative* conversation as its `ResultText`, and that cumulative narrative
  is what gets surfaced as "what this run did." [V]

### Why the two bugs compound

Bug A alone would put a failed run's diff into the tree with an ugly commit message but a
correctly-recorded `RunFailed` status — recoverable by a human reading the commit log. Bug B alone
(resuming failed conversations) is a reasonable design choice on its own terms, exactly as the doc
comment argues. **Combined**, they produce the issue's exact symptom: run 100/0006 fails but
commits 2590 insertions under an error string; run 100/0007 resumes that same Claude session,
Claude narrates the full (mostly prior) accomplishment, and litefactory reports it as a clean
success with almost no actual new diff of its own. [V]

## Constraints & Risks

- **The resume-failed-conversations design is intentional and documented in-code** (`prepare.go:
  769-772`). A fix must not simply "never resume a failed run" without weighing that against the
  stated rationale — doing so may reintroduce needless repetition of legitimate partial progress.
  The two concerns (should we resume the conversation vs. should we trust its self-reported
  completion) are currently conflated behind a single boolean and likely need to be separated. [V]
- **`store.RunStatus` is a DB lifecycle concept, not a semantic "was the underlying work good"
  concept.** `RunReported` — a fully-reported but still-failed run — is included in
  `terminalRunStatus`'s eligible-predecessor set on the same footing as `RunSucceeded`. Any fix
  touching this enum needs to preserve the lifecycle meaning while adding (or exposing) the outcome
  distinction Plan mode will need. [V]
- **Claude can commit for itself independently of litefactory's own commit path.** The
  `report.Run` struct's `SHAAfter` field (seen distinct from `CommitSHA`) implies Claude's Bash
  tool can commit directly during a run, outside `git.Worktree.Commit()`. This was **not traced to
  its call site** this pass — if true, a fix that only gates litefactory's own `commitAndPush()`
  call may not fully close the gap, since Claude could still commit failed work itself mid-run,
  before litefactory's own outcome-gating logic ever runs. This needs verification before Plan mode
  commits to a fix shape. [W]
- **Cost/token accounting gap** (related, per the issue, but a distinct code path): the terminal
  `result` event from the Claude CLI populates `agent.RunResult`'s cost/token fields
  (`internal/harness/runner.go:551-729`); when no `result` event arrives (crash, timeout, usage-limit
  refusal before completion), those fields stay at their zero value. Two sub-findings, at different
  confidence:
  - The report layer's Cost bullet checks `res.CostUSD > 0` rather than an explicit
    "was cost known" flag — this is a display-layer bug distinct from the underlying persistence,
    and understates displayed cost whenever `CostUSD` is legitimately a small-but-nonzero value that
    happens not to satisfy some other check, or when it's genuinely unknown vs. genuinely zero. [V,
    display layer only]
  - Whether the DB layer (`store`) already distinguishes "unknown" from "zero" via a nullable
    `CostUSD *float64` column, or whether the zero written for a failed/timed-out run is
    indistinguishable from a legitimately free run at the persistence layer too, was **not fully
    verified** this pass and needs confirmation. [W]
- **Generic-fallback commit messages are a second manifestation of Bug A.** A timed-out or crashed
  run with no result text at all still commits — under a fixed fallback string produced by
  `sanitizeSummary`'s empty-string handling — rather than the issue's literal-error-string case.
  Same root cause (unconditional commit), different surface symptom; a fix should cover both. [V]

## Open Questions

1. Should a failed/timed-out run commit its worktree diff at all? Candidate directions: (a) never
   commit on a non-success outcome, (b) commit but under a clearly-marked, fixed message (not the
   raw failure text) and/or to a non-default ref, (c) commit but flag the commit/run row as
   "committed under failure" so downstream consumers (including the next run's own context-building)
   can treat it differently from a clean success. This is a product decision for Plan mode, not
   something Research should preempt.
2. Should resume-from-failed-run be disabled outright, or should the conversation still resume
   (per the stated design rationale in `prepare.go:769-772`) while the *next* run is prevented from
   inheriting the failed run's self-reported "already done" narrative as if it were its own new
   accomplishment? These read as two separable concerns today implemented as one boolean
   (`terminalRunStatus`/`resumeDecision`), and Plan mode should decide whether to keep them merged
   or split them.
3. If a prior run's commit is retained despite failure, how should the *next* run be told "this
   diff exists but was not validated as complete" so it doesn't narrate it as its own success? Does
   this need a prompt-level signal to Claude, a report-level distinction, or both?
4. Should `DB.FinishRun`'s `COALESCE`-preserve behavior for `commit_sha` change so a failed run's
   commit is explicitly distinguishable from a succeeded run's commit at the data layer, rather than
   relying solely on cross-referencing `commit_sha` against `status`?
5. Does Claude's own Bash-tool commit capability (the `SHAAfter` vs `CommitSHA` split noted above)
   provide an independent path for failed-run work to land in the tree outside litefactory's own
   commit call? Not traced to its call site this pass — needs a direct check (likely in
   `internal/turn` prompt construction / tool policy, or wherever `SHAAfter` is populated) before
   Plan mode finalizes a fix shape, since gating only `git.Worktree.Commit()` may be insufficient.
6. Is the DB-layer `CostUSD` column nullable (distinguishing "unknown" from "zero"), or does the
   accounting gap exist at persistence too, not just in the report-layer `> 0` display check? Needs
   direct confirmation (likely `internal/store` schema/migration files, not read this pass).
7. Behavioral, not just static: does resuming a failed Claude session actually differ from a cold
   run against the same already-populated worktree in producing the false-success narrative, or
   would a cold rerun on that same worktree exhibit the same symptom (because the files are already
   there regardless of resume)? Static code reading settles the *mechanism* (resume ignores prior
   outcome) but not whether resume itself is necessary for the symptom to appear — worth a
   deliberate check before scoping the fix narrowly around resume logic alone.
8. No web-research was needed for this brief — every open point is internal to litefactory's own
   Go code and is resolvable by further codebase reading, not by external research. If this
   assessment is wrong, Plan mode should flag needing `web-researcher`.

## Recommended scope for Plan mode

**In scope:**
- Gating litefactory's own commit step (`internal/turn/harvest.go` `save()`/`commitAndPush()`,
  `internal/git/commit.go` `Worktree.Commit`, and the `DB.SetRunCommit`/`DB.FinishRun` persistence
  path in `internal/store/runs.go`) on run outcome, per whatever direction is chosen for Open
  Question 1.
- Separating "is this a valid conversation to resume" from "should this prior run's self-report be
  trusted as evidence of completed work" in `internal/turn/prepare.go`'s `previousRun` /
  `terminalRunStatus` / `resumeDecision`, per Open Question 2.
- Verifying and, if confirmed, closing the Claude-self-commit gap (Open Question 5) so the fix is
  not bypassed by Claude's own Bash-tool git usage.
- Fixing the cost/token accounting gap for failed/timed-out runs — both the report-layer `> 0`
  check and, if confirmed missing, the persistence-layer distinction between unknown and zero cost
  (Open Questions 6 and the related Constraints & Risks item).
- Deciding and specifying the exact commit-message/status semantics for a run that fails after
  producing a diff (fixed message vs. suppressed commit vs. flagged commit), since the current
  behavior of committing raw failure text under the run's real diff is the issue's most visible
  symptom.

**Out of scope:**
- A broader redesign of the `agent.Outcome`/`store.RunStatus` dual-vocabulary system beyond what's
  needed to express the outcome-of-a-commit / outcome-of-a-resume-predecessor distinction — this
  brief did not find evidence that the dual vocabulary itself is the problem.
- Any change to `internal/report`'s rendering logic beyond the narrow Cost-bullet display fix — the
  report layer was found to faithfully render what it's given; the falseness enters upstream of it.
- Redesigning the Claude resume feature wholesale — only the specific gap (resume ignoring prior
  outcome when deciding what to trust as "already done") is in scope, not resume/session-continuity
  as a feature.
