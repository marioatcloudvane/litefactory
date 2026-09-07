# Feature Spec: Failed runs are not committed, and the next run does not claim their work
# Status: APPROVED
# Date: 2026-09-06

## Goal
Stop litefactory committing a non-success run's worktree under the run's raw failure text, and stop the next run inheriting that failed conversation as work it may report as done.

## Verified Facts
Established by reading the code in this worktree, so architecture review does not re-derive them.

| # | Question | Finding | Evidence |
|---|----------|---------|----------|
| VF-1 | Can any agent commit independently of litefactory's commit step? | No, on the Claude path. `DefaultSettings` denies `Bash(git add:*)`, `Bash(git commit:*)`, `Bash(git push:*)`, `Bash(git stash:*)`; `defaultAllowedTools()` admits read-only git verbs only (`status`, `diff`, `log`, `show`, `rev-parse`, `ls-tree`, `merge-base`, `rev-list`). The CLAUDE.md claim holds. | `internal/harness/settings.go:89-105`, `internal/config/policy.go` (`defaultTools`, `defaultAllowedTools`) |
| VF-2 | How strong is that ban? | Defence in depth, self-documented as able to fail open. `--settings` is *additive*, not a replacement; `--allowedTools` is additive to ambient operator settings (measured: 250 ambient allow-rules leaked, Bash executed while absent from `--allowedTools`); `--tools` is the only real boundary. `EnsureSettings` writes the file only when missing, so a hand-edited `claude.settings_path` is never repaired by a run. | `internal/harness/settings.go:12-34, 155-176` |
| VF-3 | Does the same ban exist on the Codex path? | No. `codex exec --json --ignore-user-config --ignore-rules --sandbox <read-only\|workspace-write>` carries no git-verb deny list; `workspace-write` does not distinguish a git write from a file write. | `internal/codex/runner.go` |
| VF-4 | Is missing cost on failed runs a storage gap or a display gap? | Display only. `runOutcome` stores `*float64` guarded by `HasCost \|\| CostUSD > 0`, so unknown stays NULL (SD-3); `internal/store/store_test.go:1132` asserts nil for a failed run; `internal/harness/meta.go` does the same for `NNNN.meta.json`. The defect is `res.CostUSD > 0` in the renderer, which makes "unknown" and "zero" both render as no Cost line, unlike Exit status and Duration which render "unknown". | `internal/turn/harvest.go:204-222`, `internal/report/run.go:582-585` |
| VF-5 | Are token counts stored anywhere? | No. No token-count field exists in `agent`, `harness`, `store`, `report` or `cli`. A "token accounting" fix would be a new feature, not a bug fix. | `internal/agent/result.go:95-165` |
| VF-6 | Is a run's success durable after it is reported? | Not via `status` alone: `CompleteRunReport` moves both succeeded and failed runs to `reported`. It is durable via `runs.error`, which `FinishRun` writes from `harness.RunResult.Reason`, and `classify` returns `""` for `OutcomeSucceeded` and a non-empty reason for all four failure outcomes. `CompleteRunReport` never clears it. | `internal/store/runs.go:262-372`, `internal/harness/runner.go:844-883`, `internal/turn/harvest.go:204-222` |
| VF-7 | Does anything else act on an uncommitted failed worktree? | Yes. `fileStoryIssues` skips only abandoned runs and `result.Err != nil`; its write-back commit stages with `git add --all`. Once US-1 stops committing failed runs, this phase would sweep the failed run's uncommitted work into a commit no runs row names, and pin Detail permalinks to an unpushed SHA. | `internal/turn/subissues.go:106-142`, `internal/git/commit.go:119-124` |

## Decisions
| # | Decision | Rationale |
|---|----------|-----------|
| D-1 | A non-success run's worktree is **not committed at all** — no placeholder, no WIP message. | The work is not lost: it stays in the worktree and the next run on the task reuses that worktree, where its commit step publishes it (the exact argument `save`'s existing abandonment gate already makes). A placeholder commit would keep the false-history problem alive in weaker form: a commit on the branch that no successful run authored, that the next run's `Changed` check then republishes anyway. |
| D-2 | Resume after a failed run **stays enabled**, with a corrective note injected into the resumed prompt. | `terminalRunStatus`'s documented rationale holds: claude was invoked either way, and a conversation that went badly is still a conversation the next run should continue rather than repeat. The bug is not that the conversation is resumed; it is that the resumed run has no way to know its predecessor failed, so it narrates the predecessor's claims as its own accomplishments. Disabling resume after failure would discard hours of real context to fix a labelling problem, and would make every transient tool-quota failure restart from cold. |
| D-3 | No schema change. Predecessor success is derived from the columns already stored. | `runs.error` is empty for exactly one outcome (`OutcomeSucceeded`) and survives the move to `reported` (VF-6). A new column would duplicate a fact the row already carries. |
| D-4 | Cost gets a "unknown" rendering; token accounting is not introduced. | VF-4 and VF-5. |

## User Stories
| ID | Story |
|----|-------|
| US-1 | As a repository owner, I want litefactory to make no commit for a run that did not succeed, so that a branch never carries a commit whose message is a tool's error text and whose content nobody reviewed as finished work. |
| US-2 | As a repository owner, I want the sub-issue filing phase to skip a run that did not succeed, so that it does not sweep that run's uncommitted work into a commit of its own and does not file issues whose Detail links point at an unpushed SHA. |
| US-3 | As a repository owner, I want the report of a failed run to say plainly that nothing was committed and where the work is, so that I can tell a failed run from an empty one without opening the log. |
| US-4 | As a repository owner, I want a run that resumes a failed predecessor to know that its predecessor failed, so that litefactory can tell it so rather than letting it read the transcript's last claims as settled fact. |
| US-5 | As a repository owner, I want the resumed run's prompt to state that the previous run failed and that its unverified claims are not this run's accomplishments, so that the next report describes what this run did rather than re-announcing work that never landed. |
| US-6 | As a repository owner, I want a run whose cost is unknown to say "unknown" rather than omit the line, so that an absent cost is not silently indistinguishable from a free run. |

## Data Model

No table, column or migration is added or changed. The changes are to in-memory types.

| Entity | Field | Type | Required | Constraints |
|--------|-------|------|----------|-------------|
| `store.Run` (existing row) | `Succeeded() bool` | derived method, no column | yes | Returns true iff `Status == RunSucceeded`, or `Status == RunReported && Error == ""`. False for `RunRunning`, `RunFailed`, and any `RunReported` row carrying a reason. Documented against VF-6. |
| `harness.PromptInput` | `PreviousFailed` | `bool` | yes (zero value valid) | Read only when `Resume` is true, as `PreviousMode` and `PreviousMayAsk` already are. True when the resumed segment's previous run did not succeed. |
| `harness.PromptInput` | `PreviousReason` | `string` | no | The predecessor's `runs.error` verbatim: litefactory's own single-line classification string (`"claude exited with status 1"`, `"timed out after 30m0s; …"`), already length-bounded and newline-stripped by `harness.excerpt` on the `is_error` path. Empty when unknown. Ignored unless `PreviousFailed` is true. |
| `turn.preparation` | (no new field) | — | — | `previousRun` already returns the full `store.Run`; `prepare` already holds it at the point where `resumeDecision` is called. |

### Relationships
- `store.Run` (previous, same segment) → `harness.PromptInput` via `PreviousFailed` / `PreviousReason`, alongside the existing `PreviousMode` / `PreviousMayAsk` plumbing in `internal/turn/prepare.go`.
- `harness.RunResult.Outcome` → `turn.save` / `turn.fileStoryIssues` gate: the run's terminal outcome is already known before either runs (`runAndHarvest` calls `DB.FinishRun` before `save`).

## API / Interface Changes

No HTTP, RPC or CLI surface changes. All changes are internal Go.

| Site | Change | Notes |
|------|--------|-------|
| `internal/turn/harvest.go` `save` | Gate widens from `runResult.Outcome == harness.OutcomeAbandoned` to `!runResult.Succeeded()` | Covers Failed, TimedOut, Abandoned and NoResult. `commitAndPush` is not called; `result.Changes`, `SHAAfter`, `CommitSHA`, `Committed`, `Pushed` and `Changed` stay at their zero values. Logs at warn with the worktree path, as the abandoned branch already does. |
| `internal/turn/harvest.go` `save` doc comment | Rewritten | The current comment argues the opposite of the new behaviour and must not be left contradicting the code. |
| `internal/turn/harvest.go` `commitAndPush` | Unchanged | Still the only commit site; still passes `runResult.ResultText` as `Summary`, now reachable only on success. |
| `internal/turn/harvest.go` `finish` | Unchanged | `result.CommitSHA` is empty on the new path, so `SetRunCommit` is already not called. Covered by an acceptance criterion rather than a code change. |
| `internal/turn/subissues.go` `fileStoryIssues` | Guard widens from `runResult == nil \|\| Outcome == OutcomeAbandoned` to `runResult == nil \|\| !runResult.Succeeded()` | VF-7. Same early-return, log-only failure containment as the existing guards. |
| `internal/turn/prepare.go` `resumeDecision` | Unchanged signature and result | It answers "is there a conversation to resume", which stays yes (D-2). |
| `internal/turn/prepare.go` `prepare` | Populates `PreviousFailed` / `PreviousReason` on `promptInput` from the `previous` run it already holds | Set only when `resume` is true; both stay zero on the cold path. |
| `internal/store/runs.go` | Adds `func (r Run) Succeeded() bool` | Read-only derivation, no query and no schema change. |
| `internal/harness/prompt.go` `PromptInput` | Two new fields (above) | Documented with the same "read only on the resume path" wording as `PreviousMode`. |
| `internal/harness/prompt.go` `writeResumeHeader` | Emits the corrective note when `PreviousFailed` is true | Placed after the `- Feature slug:` line and before the infer-guidance and ask-permission blocks, so the two existing conditional blocks keep their byte-for-byte output when the predecessor succeeded. |
| `internal/report/run.go` `metadataList` | Cost branch changes from `res.CostUSD > 0` to a `HasCost`/`HasResult`-driven three-way | Matches how Exit status and Duration already render an explicit "unknown". |
| `internal/report/run.go` `metadataList` / `describeRunOutcome` | The "no commit" statement names the worktree for a non-success run | The `agent.OutcomeTimedOut` callout already says "still in the worktree"; the Failed and NoResult callouts do not, and the metadata line reads `none — this run committed nothing`, which understates a failed run. |

## Business Rules
- Only `internal/turn/commitAndPush` ever creates a commit or a push for a run. No story here grants, implies or requires an agent-initiated commit; the deny list at `internal/harness/settings.go:102-105` and the read-only git allowlist in `internal/config/policy.go` stay exactly as they are.
- A run commits only when `harness.RunResult.Succeeded()` is true. Failed, TimedOut, Abandoned and NoResult all leave the worktree untouched by litefactory.
- `runResult.ResultText` may only ever become a commit message on a success. It is untrusted text either way and keeps its existing `Litefactory-` trailer stripping.
- A run that did not commit records no `commit_sha`: `finish` must not call `SetRunCommit` with an empty SHA, and `FinishRun`'s `COALESCE(?, commit_sha)` must not be passed a SHA from a non-success run.
- Work left uncommitted at the end of a run is the normal, non-lossy state. The next run on the task reuses the same worktree, and its commit step publishes whatever is there.
- The sub-issue phase never runs on a run that did not succeed, because its write-back commit stages with `git add --all`.
- Resume is decided by "is there a resumable conversation"; trust in that conversation's self-report is decided separately, by the predecessor's outcome. The two are never collapsed into one boolean.
- A resumed run whose predecessor failed is told so in its prompt, and told that the predecessor's claims are not its accomplishments. The note is emitted only when the predecessor failed; when it succeeded the resume header is byte-for-byte what it is today.
- A cost that was never reported renders as "unknown". A cost reported as exactly zero renders as `$0.0000`. Neither is omitted.
- No token counts are recorded, derived or displayed.

## Edge Cases & Error Handling
- Run fails after producing substantial work → no commit, no push; the report names the worktree path; the next run on the task carries it forward and commits it on success.
- Run times out → same as above; the existing "still in the worktree" callout stays and is now literally true rather than approximately true.
- Run is abandoned by daemon shutdown → unchanged behaviour, now reached through the general gate rather than a special case; the ≤3s shutdown budget (D-B) is still never spent on a commit or a push.
- Run exits 0 with no terminal result event (`OutcomeNoResult`) → treated as non-success: no commit. This is the case the old `!= OutcomeAbandoned` gate committed under a generic fallback message.
- Run succeeds but produces no changes → unchanged: `commitAndPush` runs, `Changed` is false, AC-3.23's "no changes is a normal result" report is unaffected.
- Run succeeds, commit succeeds, push fails → unchanged: `result.Err` is set, the report quotes the worktree still holding the commit (AC-3.24).
- Branch HEAD moved without litefactory committing (possible only on the Codex path per VF-3) during a failed run → litefactory does not push it; the next run's `prepare` reads HEAD into `shaBefore` before the run starts, so the next successful run's `Changed` check publishes it. Nothing is lost and nothing is attributed to the failed run.
- Predecessor run is `reported` and had failed → `Run.Succeeded()` returns false because `runs.error` is non-empty (VF-6), so the corrective note is still emitted.
- Predecessor run row predates this change or carries an empty error with a `reported` status → treated as succeeded, no note. Failing open to "no note" is the pre-existing behaviour and cannot make a report worse than it is today.
- Predecessor run is on a different segment, or the segment changed agent → `resume` is false, both new fields stay zero, and the cold prompt is unchanged.
- Predecessor's reason string is empty while `PreviousFailed` is true → the note is emitted without the reason clause; it never renders an empty quotation.
- Resumed session is refused by claude and `internal/turn` rebuilds the prompt with `Resume = false` → `PreviousFailed` and `PreviousReason` are ignored on the cold path exactly as `PreviousMode` and `PreviousMayAsk` already are.
- Report has no `agent.RunResult` at all → Cost is omitted, as Exit status already renders "unknown" from the `Result == nil` branch; no new nil dereference is introduced.

## Out of Scope
- Any form of placeholder, WIP or "failed run" commit for a non-success worktree (decided against in D-1).
- Disabling resume after a failed run, and any redesign of segment/resume semantics beyond making the predecessor's outcome visible (D-2).
- Any change that grants, enables or implies an agent-initiated `git add`, `commit`, `stash` or `push`.
- Hardening the Codex sandbox to match the Claude git deny list. VF-3 records the asymmetry; closing it is separate work.
- Token counting or token accounting of any kind (VF-5).
- Reconciling the two outcome vocabularies (`agent.Outcome` / `harness.Outcome` versus `store.RunStatus`), and adding a durable outcome column (D-3).
- Retrying, auto-resuming or otherwise reacting to a failed run beyond labelling it.
- Changing what `terminalRunStatus` treats as a resumable predecessor.
- Redaction or sanitisation changes to `ResultText` on the success path.

## Acceptance Criteria
- [ ] A turn whose run ends `OutcomeFailed` makes no commit: the branch SHA is unchanged, `git log` shows no new commit, and no commit message anywhere contains the run's result text.
- [ ] The same holds for `OutcomeTimedOut` and `OutcomeNoResult`; the existing abandoned-run behaviour is unchanged.
- [ ] A turn whose run ends `OutcomeSucceeded` still commits and pushes exactly as before, with `runResult.ResultText` as the summary.
- [ ] After a failed run, the worktree still contains the run's changes (a file the run wrote is present and shows as modified/untracked in `git status`).
- [ ] After a failed run, the run's row has an empty `commit_sha`, and `SetRunCommit` was not called.
- [ ] After a failed run, `Result.Changed`, `Committed`, `Pushed`, `CommitSHA`, `SHAAfter` and `Changes` are all at their zero values, and `Result.Err` is nil (the run failed; the turn did not).
- [ ] `fileStoryIssues` returns early for a failed, timed-out or no-result run, files no sub-issues, and makes no write-back commit.
- [ ] A run following a failed run in the same worktree commits both its own changes and the predecessor's leftovers when it succeeds.
- [ ] `store.Run.Succeeded()` returns: true for `succeeded`; true for `reported` with an empty error; false for `reported` with a non-empty error; false for `failed`; false for `running`.
- [ ] A resumed run whose predecessor failed receives a prompt containing an explicit statement that the previous run failed and that its unverified claims are not this run's accomplishments.
- [ ] The corrective note names the predecessor's reason when `runs.error` is non-empty, and omits the clause cleanly when it is empty.
- [ ] A resumed run whose predecessor succeeded receives a resume header byte-for-byte identical to the one produced before this change.
- [ ] A cold-start run's prompt is byte-for-byte identical to the one produced before this change.
- [ ] `resumeDecision` still returns true for a failed Claude predecessor on the same segment: resume is not disabled.
- [ ] A run with no reported cost renders `- **Cost:** unknown`; a run reporting exactly `0` renders `- **Cost:** $0.0000`; a run reporting a positive amount renders as it does today.
- [ ] The report for a run that did not succeed names the worktree path holding the uncommitted work, and does not describe the run as having committed nothing in the same words used for a successful run that changed nothing.
- [ ] `make check` is green, including the existing abandoned-run, no-changes and push-failure tests in `internal/turn` and the five-outcome rendering test in `internal/report`.

## Assumptions
| # | Assumption | Based on |
|---|------------|----------|
| A-1 | "Non-success" means `!harness.RunResult.Succeeded()` — all four failure outcomes, not only `OutcomeFailed`. | `runOutcome`'s own rule that "only success is not a failure" (`internal/turn/harvest.go:199`); the generic-message manifestation in the brief came from `OutcomeNoResult`. |
| A-2 | Not committing is preferable to a placeholder commit. | D-1; `save`'s existing abandonment rationale that the worktree is a safe place and the next run reuses it. |
| A-3 | Resume stays enabled. | D-2; `terminalRunStatus`'s documented design intent (`internal/turn/prepare.go:769-772`). |
| A-4 | `runs.error` is a sound success/failure signal. | VF-6: `classify` returns `""` only for `OutcomeSucceeded`; `CompleteRunReport` does not clear the column. |
| A-5 | The corrective note belongs in the resume header rather than in a new prompt section. | `writeResumeHeader` is already where per-predecessor deltas (mode change, ask-permission change) are emitted, with an established precedent for emitting only on change. |
| A-6 | The exact wording of the corrective note is the implementer's, within the constraint that it states the failure and forbids claiming the predecessor's work. | No wording is specified anywhere upstream; the acceptance criteria pin the semantics, not the string. |
| A-7 | Widening `fileStoryIssues`'s guard is in scope even though the brief did not name it. | VF-7: without it, US-1 relocates the bug rather than fixing it. |
| A-8 | The Cost fix is in scope; a token fix is not. | VF-4 and VF-5, resolved from code as instructed. |
