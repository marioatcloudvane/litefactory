# Feature Spec: Pre-run branch drift detection and safe auto-sync
# Status: APPROVED
# Date: 2026-09-05
# Amended: 2026-09-06 — US-7 reconsidered and kept (note below); US-11/US-12 added; stale `lf:tools=` and migration-number facts corrected

## Goal
Stop a litefactory task branch silently diverging from its base branch by detecting drift before every run and, when a dry run proves it is safe, merging the base into the task branch automatically — so the non-technical owner is never asked to resolve a conflict in the GitHub web editor, and is told plainly when one exists.

## User Stories

| ID | Story |
|----|-------|
| US-1 | As a repo owner, I want litefactory to check before each run how far the task branch is behind the base branch, so that work never starts on a stale tree without anyone noticing. |
| US-2 | As a repo owner, I want litefactory to merge the base branch into the task branch by itself when that merge is provably conflict-free, so that my branches stay current without me running any git commands. |
| US-3 | As a repo owner, I want the run's issue comment to say in plain words that the branch was N commits behind and was synced, so that an automatic change to my branch is never invisible. |
| US-4 | As a repo owner, I want litefactory to refuse to start the run when the base cannot be merged cleanly, and to name the conflicting files in the issue comment, so that an agent never builds on a tree that is about to be thrown away. |
| US-5 | As a repo owner, I want a conflict report that tells me exactly what to do next in one comment, so that I do not have to know what "rebase", "merge base" or "diverged" mean to unblock the task. |
| US-6 | As a repo owner running several issues at once, I want each task branch synced independently at its own run, so that the number of issues in flight does not decide how often I hit conflicts. |
| US-7 | As a repo owner, I want to switch auto-sync off for one issue with a label, so that a task that deliberately needs an old base is not disturbed. |
| US-8 | As an operator, I want auto-sync governed by config with safe defaults, so that I can disable it repo-wide or cap how large an automatic merge may be. |
| US-9 | As an operator, I want every new git operation to live behind the existing `internal/git` boundary, so that the "litefactory never writes to the registered clone" guarantee stays a compile-time property. |
| US-10 | As an operator, I want a failed or refused sync to leave the branch byte-identical to what it was, so that a partial merge state can never be handed to an agent or pushed. |
| US-11 | As a repo owner, I want litefactory to ask the default reasoning model for a concrete way to resolve the conflict and to post that suggestion in the blocked run's comment, so that a refusal hands me a proposal instead of only a list of file names. |
| US-12 | As a repo owner, I want a posted suggestion applied to my branch only after I explicitly confirm it in a comment, so that no model-authored content ever reaches my branch without my say-so. |

### US-7 — reconsidered and kept (2026-09-06)

| Question | Recorded outcome |
|---|---|
| Raised by | Issue owner: "do we have these kind of opt-out labels? I think it makes no sense" |
| Decision | `lf:sync=on\|off` is **kept exactly as specified**. No mechanism is added, removed or reshaped by this reconsideration. |
| Alternative weighed | Drop the label; ship US-2/US-4 with no per-issue override. |
| Why the alternative was rejected | With no per-issue override, the only escape from a false-positive block is flipping `block_run_on_conflict` for the **entire repo** — which un-blocks every issue's real conflicts too, not just the false-positive one. A repo-wide safety switch is the wrong-sized tool for a one-issue problem, and turning it off to unblock one README conflict silently disarms the guarantee US-4 exists to provide for every other issue. |
| Why the label's cost is acceptable | Its blast radius is one issue; its effect is disclosed in that issue's comment as outcome `skipped` (US-3); it introduces no new vocabulary — it copies the live `agentLabelPrefix` / `modeLabelPrefix` ladder rung for rung. |
| Standing constraint (unchanged) | US-7 must ship no later than US-2/US-4, never after (architect's resolution of A-14). |
| Not reopened by this note | Whether `lf:base=` or any other label should exist (still out of scope); what `syncBase` does with the resolved decision (US-2); the config levers themselves (US-8). |

### US-11 / US-12 — decomposition note (for the architect's final call)

| Point | Detail |
|---|---|
| Written as two stories | US-11 is read-plus-post: it observes a `blocked` outcome, calls a model, posts text. US-12 is the only half that writes to the branch. |
| Why split | Different triggers (a blocked run vs. a later owner comment), different risk surfaces (outbound prompt content vs. index/merge-completion mutators and an ownership check), and different `internal/git` needs (none vs. net-new). US-11 is shippable and useful on its own — a suggestion the owner applies by hand is already better than a file list. |
| Dependency | US-12 depends on US-11; both depend on the US-4 `blocked` outcome. Both are **additive** to US-1..US-10 and replace none of them. |
| If merged into one story | Acceptable, provided the mandatory confirmation gate stays inside it. The gate is the point of the story and is not a phase-2 refinement. |

## Data Model

### New value types (in-memory, `internal/git`)

| Entity | Field | Type | Required | Constraints |
|--------|-------|------|----------|-------------|
| `Divergence` | `Ahead` | `int` | yes | Commits on branch not on base; `>= 0` |
| `Divergence` | `Behind` | `int` | yes | Commits on base not on branch; `>= 0` |
| `Divergence` | `Base` | `string` | yes | Base ref compared against, e.g. `origin/main` |
| `Divergence` | `Branch` | `string` | yes | Task branch name |
| `MergePreview` | `Clean` | `bool` | yes | True when the merge produced a tree with no conflicts |
| `MergePreview` | `Conflicts` | `[]string` | no | Repo-relative paths; empty iff `Clean` |
| `MergePreview` | `TreeSHA` | `string` | no | Written tree from the dry run; empty when not `Clean`; recorded only, never checked out |
| `MergeResult` | `Head` | `string` | yes | Worktree HEAD after the merge commit |
| `MergeResult` | `Merged` | `bool` | yes | False when nothing to merge (already up to date) |

### New persisted columns (`runs` table, migration `0007_run_sync.sql`)

Migration number corrected 2026-09-06: `0005` and `0006` are taken (head is `0006_schedule_block_replies.sql`). Re-check the head before writing the file.

| Entity | Field | Type | Required | Constraints |
|--------|-------|------|----------|-------------|
| `Run` | `SyncOutcome` | `TEXT NOT NULL DEFAULT ''` | yes | One of `""`, `up_to_date`, `synced`, `blocked`, `skipped`, `failed` |
| `Run` | `SyncBehind` | `INTEGER NOT NULL DEFAULT 0` | yes | `Divergence.Behind` observed at prepare time |
| `Run` | `SyncAhead` | `INTEGER NOT NULL DEFAULT 0` | yes | `Divergence.Ahead` observed at prepare time |
| `Run` | `SyncCommitSHA` | `TEXT NOT NULL DEFAULT ''` | yes | Merge commit created; empty unless `SyncOutcome = synced` |

### Conflict-resolution proposal (US-11/US-12)

Persisted. These are the attributes the feature requires; the storage shape, state machine and package placement are architecture decisions (ODQ-24). No comparable "awaiting human confirmation" state exists in `internal/store` today.

| Entity | Field | Type | Required | Constraints |
|--------|-------|------|----------|-------------|
| `ConflictProposal` | `ID` | opaque identifier | yes | Must be short enough for the owner to quote in a comment; unique |
| `ConflictProposal` | `SessionID` | ref | yes | A proposal is confirmable only inside its own session |
| `ConflictProposal` | `RunID` | ref | yes | The run whose `SyncOutcome = blocked` produced it |
| `ConflictProposal` | `Base` | `string` | yes | Base ref the blocked preview ran against |
| `ConflictProposal` | `BaseSHA` | `string` | yes | Base tip the suggestion was computed from; re-verified at apply time |
| `ConflictProposal` | `BranchSHA` | `string` | yes | Branch tip the suggestion was computed from; re-verified at apply time |
| `ConflictProposal` | `Files` | `[]string` | yes | Paths the suggestion covers; must be a subset of `MergePreview.Conflicts` |
| `ConflictProposal` | `Resolution` | resolved content per file | yes | Model output; untrusted; never applied before confirmation |
| `ConflictProposal` | `Excluded` | `[]string` | no | Conflicting paths deliberately left out (over budget, binary, non-UTF-8) |
| `ConflictProposal` | `Status` | enum | yes | One of `pending`, `confirmed`, `rejected`, `expired`, `superseded` |
| `ConflictProposal` | `Model` | `string` | yes | Model/tier that produced the suggestion; disclosed in the comment |
| `ConflictProposal` | `CreatedAt` | timestamp | yes | Start of the TTL window |
| `ConflictProposal` | `DecidedAt` | timestamp | no | Set when status leaves `pending` |
| `ConflictProposal` | `ConfirmedBy` | `string` | no | Login of the commenter whose confirmation was accepted |
| `ConflictProposal` | `AppliedCommitSHA` | `string` | no | Merge commit produced by the confirmed application; empty otherwise |

### New report fields (`internal/report`)

| Entity | Field | Type | Required | Constraints |
|--------|-------|------|----------|-------------|
| `report.Run` | `Sync` | `*report.Sync` | no | Nil renders no sync section at all |
| `report.Sync` | `Outcome` | `SyncOutcome` | yes | Same enum as the persisted column |
| `report.Sync` | `Behind` / `Ahead` | `int` | yes | Rendered as counts, never as raw git output |
| `report.Sync` | `Base` | `string` | yes | Named in the comment so the owner sees which branch was merged in |
| `report.Sync` | `CommitSHA` | `string` | no | Permalinked only when the branch was pushed |
| `report.Sync` | `Conflicts` | `[]string` | no | Rendered as a file list; truncated at 20 with a count of the remainder |
| `report.ReplyKind` | `ReplyRefusesSync` | const | yes | New enum member plus its case in `replyKind()` in `internal/turn/report.go` |
| `report.Run` | `Proposal` | `*report.ConflictProposal` | no | Nil renders no suggestion section |
| `report.ConflictProposal` | `ID` | `string` | yes | Quoted back by the owner to confirm |
| `report.ConflictProposal` | `Files` | `[]string` | yes | Covered paths; truncated at 20 like `Conflicts` |
| `report.ConflictProposal` | `Excluded` | `[]string` | no | Named so the owner knows the suggestion is partial |
| `report.ConflictProposal` | `Resolution` | rendered per-file blocks | yes | Model text; escaped and byte-capped like any untrusted field |
| `report.ConflictProposal` | `Model` | `string` | yes | Disclosed: the owner is told a model wrote this |
| `report.ConflictProposal` | `ConfirmPhrase` | `string` | yes | The exact words the owner must comment to confirm |
| `report.ConflictProposal` | `ExpiresAt` | timestamp | yes | Rendered in plain words |
| `report.ReplyKind` | `ReplySuggestsResolution` | const | yes | New enum member plus its case in `replyKind()` |
| `report.ReplyKind` | `ReplyAppliesResolution` | const | yes | New enum member plus its case in `replyKind()` |

### Config additions (`config.Settings`)

| Entity | Field | Type | Required | Constraints |
|--------|-------|------|----------|-------------|
| `Settings` | `AutoSyncBase` | `bool` (`auto_sync_base`) | no | Default `true` |
| `Settings` | `AutoSyncMaxBehind` | `int` (`auto_sync_max_behind`) | no | Default `100`; `0` means no cap; negative is a config error |
| `Settings` | `BlockRunOnConflict` | `bool` (`block_run_on_conflict`) | no | Default `true` |
| `Settings` | `SuggestConflictResolution` | `bool` (`suggest_conflict_resolution`) | no | Default `true`; `false` makes a blocked run behave exactly as US-4 alone |
| `Settings` | `ConflictProposalTTLHours` | `int` (`conflict_proposal_ttl_hours`) | no | Default `168` (7 days); `0` means no expiry; negative is a config error |
| `Settings` | `ConflictContentMaxBytes` | `int` (`conflict_content_max_bytes`) | no | Default `32768` total across all conflicting files sent to the model; negative is a config error |

### Relationships
- `Repo.BaseBranch` → the remote-tracking ref `origin/<BaseBranch>` used as the comparison base; nothing else defines the base.
- `Session.Branch` → the task branch compared and merged into.
- `Run` → one optional sync outcome per run, written in the same transaction as the run's terminal status.
- `report.Run.Sync` → rendered inside the existing end-of-turn comment; it is not a separate comment.
- `internal/git.Clone` → gains read-only `Divergence` and `MergePreview`; `internal/git.Worktree` → gains the single new mutator `MergeBase`.
- `Run` (outcome `blocked`) → at most one `ConflictProposal`; a session has at most one `pending` proposal at a time.
- `ConflictProposal` (status `confirmed`) → exactly one merge commit on the task branch; never on the base branch.

## API / Interface Changes

| Method | Endpoint / Action | Auth | Notes |
|--------|------------------|------|-------|
| Go | `(*git.Clone) Divergence(ctx, base, branch string) (Divergence, error)` | n/a | Wraps `git rev-list --left-right --count <base>...<branch>`; read-only, belongs on the S-6 allowlist |
| Go | `(*git.Clone) MergePreview(ctx, base, branch string) (MergePreview, error)` | n/a | Wraps `git merge-tree --write-tree --name-only <branch> <base>`; writes only loose objects, never a ref, so it stays a read-only-by-contract clone method |
| Go | `(*git.Worktree) MergeBase(ctx, base string, opts MergeOptions) (MergeResult, error)` | n/a | Third and only new mutator for US-1..US-10; runs `git merge --no-ff --no-edit` in the worktree; `MergeOptions` carries the commit trailers |
| Go | `internal/git` errors | n/a | `ErrMergeConflict` added; conflicts surface as a typed error, not a stderr string match |
| Go | `internal/turn.prepareClone` | n/a | Gains a `syncBase` step between `fetch` and `freezeSlug`; runs inside the single existing `CloneGate` acquisition |
| Config | `settings.auto_sync_base`, `settings.auto_sync_max_behind`, `settings.block_run_on_conflict`, `settings.suggest_conflict_resolution`, `settings.conflict_proposal_ttl_hours`, `settings.conflict_content_max_bytes` | operator (file perms) | Loaded and validated with the existing `Settings` validation path; unknown values are a startup error |
| Label | `lf:sync=off` \| `lf:sync=on` | issue write | Detect-loosely/accept-strictly ladder copied rung for rung from the two **live** label levers in `internal/daemon` — `agentLabelPrefix` (`agent.go`) and `modeLabelPrefix` (`mode.go`). Not modelled on `lf:tools=`, which is dead code kept only so `legacyToolsLabel` can refuse it. Label wins over config for that issue; duplicated, miscased-prefix or out-of-vocabulary values are refused, never guessed, and the raw label text does not travel onward |
| Comment | End-of-turn run report | n/a | Gains a sync section when `report.Run.Sync != nil` |
| Comment | Refusal reply | n/a | `ReplyRefusesSync` renders the conflict report: base, behind count, conflicting files, and the copy-paste command sequence to resolve locally |
| Go | Narrow, tool-free model call producing a conflict-resolution suggestion | n/a | Same call family as the existing `agent.Classifier` and `agent.ProposeSchedule` calls: prompt in, structured output out, no tools, no session resume, no worktree access. Resolves to the default tier (`TierBalanced`); no new `config.Tier` value. Exact interface and whether the existing narrowing adapter is reused is ODQ-23 |
| Go | `internal/git` — mark a conflicted path resolved and complete the merge | n/a | **Net-new mutator surface.** No scoped `git add <path>`, no index-inspection call and no merge-completion primitive exists in `internal/git` today. Must live behind the same boundary and be covered by the S-6 allowlist test. Shape is ODQ-25 |
| Store | Persisted proposal record with a `pending` state and a status transition | n/a | Net-new: `internal/store` has no "awaiting human confirmation" state. Needs its own migration; take the next free number after the run-sync migration |
| Comment | Suggestion reply | n/a | `ReplySuggestsResolution`: names the base, the conflicting files, any excluded files, the model that produced the text, the suggested resolution per file, the proposal ID, the exact confirmation phrase and the expiry |
| Comment | Owner confirmation | issue comment; commenter must pass the same ownership check litefactory already applies to schedule proposals | The **only** trigger that authorizes writing. Must carry the proposal ID and the confirmation phrase; anything else is not a confirmation |
| Comment | Application reply | n/a | `ReplyAppliesResolution`: names the proposal ID, the files written, the resulting merge commit, and the fact that the content came from a model and was confirmed by a named human |
| CLI | none | n/a | No new command, no new flag |

## Business Rules

- Drift is computed against `origin/<Repo.BaseBranch>` after the existing `fetch`, inside `prepareClone`, before `freezeSlug` and before the agent starts.
- The comparison base is the remote-tracking ref only. A local base branch in the registered clone is never read and never written.
- `Behind == 0` → outcome `up_to_date`; no merge, no sync section in the comment, no refusal.
- `Behind > 0` and auto-sync enabled and `Behind <= AutoSyncMaxBehind` (or cap is `0`) → run `MergePreview`.
- `MergePreview.Clean` → run `Worktree.MergeBase`, which produces a merge commit authored by litefactory's existing commit identity and carrying the existing `Litefactory-Task` / `Litefactory-Mode` / `Litefactory-Run` trailers; outcome `synced`; the agent then runs on the synced tree.
- The sync merge commit is pushed by the same end-of-run `Worktree.Push` that pushes the agent's work; there is no separate mid-run push.
- `MergePreview` reports conflicts → the branch is not touched at all; outcome `blocked`; when `BlockRunOnConflict` is true the agent is not started and the run terminates with `ReplyRefusesSync`.
- Merge, never rebase. Rebase would require a force-push that `Worktree.Push` does not offer and would rewrite the trailer history that ties commits to runs.
- `Behind > AutoSyncMaxBehind` → outcome `skipped`; no merge attempted; the run proceeds and the comment states the branch is N commits behind and was left alone because it exceeds the cap.
- Auto-sync disabled by config or by `lf:sync=off` → outcome `skipped`; drift is still measured and still disclosed in the comment; the run proceeds.
- Every automatic change to the branch is disclosed. A `synced` outcome always renders the sync section, naming the base branch, the behind count and the merge commit.
- litefactory never merges a task branch into the base branch, never pushes to the base branch, and never opens, approves or merges a pull request.
- All new git commands go through `internal/git/exec.go`. No other package execs git.
- The whole drift-and-sync step happens under the one `CloneGate` acquisition already taken by `prepareClone`; no second acquisition is introduced.
- Each new git command carries its own deadline in the existing style: `Divergence` 30s, `MergePreview` 60s, `MergeBase` 60s.
- A `blocked` or `failed` outcome leaves the branch at exactly the SHA it had when the run started; no partial merge state, no `MERGE_HEAD`, no index left behind.

### Suggested resolution (US-11)

- A suggestion is produced **only** for a run whose sync outcome is `blocked`. No other path may invoke it: not `skipped`, not `failed`, not a clean merge, not a PR conflict, not an agent-reported build failure.
- The model call is narrow and tool-free — prompt in, structured output out — and resolves to the default tier. It never gets tools, worktree access, a session to resume, or the ability to run git.
- The model call never changes the refusal. If it fails, times out, is disabled, or returns nothing usable, the run still reports the block exactly as US-4 requires, plus one line saying no suggestion could be produced. A blocked run is never made worse by the suggestion attempt.
- Conflicting file **content** — not just file names — is placed in the model prompt. This is a categorically larger untrusted payload than any field the prompt builder carries today (issue title/body, comment body, bounded history) and is a new outbound-content surface. It is subject to the same "untrusted text does not travel raw" discipline, with per-file and total byte caps (`conflict_content_max_bytes`). Sizing this surface, and whether the existing neutralise-and-truncate treatment suffices for structured file content, is ODQ-27/ODQ-28.
- Files that do not fit the byte budget, are binary, or are not valid UTF-8 are excluded from the prompt, recorded in `Excluded`, and named in the comment so the owner knows the suggestion is partial. If nothing fits, no proposal is created.
- Model output is untrusted input on the way back. A suggestion that names a path outside `MergePreview.Conflicts`, or that contains conflict markers, is rejected in full; no proposal is created and the comment says so.
- Creating and posting a proposal writes nothing to the task branch, the index, or any commit. US-11 on its own is read-plus-post.
- A new proposal for a session supersedes any earlier `pending` proposal for that session; a session never has two live proposals.

### Confirmation and application (US-12)

- **The confirmation gate is mandatory and blocking.** Nothing the model produced is written to the branch, the index, or a commit on the strength of the suggestion alone. There is no config value, label, flag, environment variable or "trusted repo" exemption that removes or bypasses the gate. A change that makes application automatic is a change to this spec, not a configuration of it.
- Applying a resolution requires an **explicit, subsequent human confirmation comment** naming the proposal ID and carrying the confirmation phrase. Silence is not consent; a thumbs-up reaction is not consent; a comment that merely mentions the proposal is not consent.
- The confirming commenter must pass the same ownership check litefactory already applies to schedule proposals. A confirmation from anyone else is refused, written nowhere, and reported.
- **Propose and apply are two separate turns.** The blocked run ends after posting the suggestion. litefactory never holds a run open, polls, sleeps or otherwise waits inside one run for a reply. This requires a "wait for a human reply, then act" mechanism that **does not exist anywhere in the codebase today** — the only precedent is the schedule-proposal pattern (persist a pending row, act on a later comment that names it, gated by author ownership), and no mid-run pause/resume state machine exists. This is net-new work and must be sized as such, not treated as plumbing (ODQ-24).
- A confirmation is refused, with nothing written, when: no proposal ID is named; the ID is unknown; the proposal belongs to a different session; its status is not `pending`; or it has passed its TTL (status `expired`).
- Before writing anything, litefactory re-verifies that the base tip and the branch tip still match `BaseSHA` / `BranchSHA`. If either moved, the proposal becomes `superseded`, nothing is written, and the comment says a fresh suggestion is needed.
- Application writes only the files listed in `ConflictProposal.Files`. Any attempt to touch a path outside that list aborts the application, leaves the branch unchanged, and is reported.
- A confirmed application produces exactly one merge commit on the task branch, authored by litefactory's existing commit identity, carrying the existing run trailers plus a trailer identifying the confirmed proposal and the confirming human. The trailer name and format are ODQ-26.
- Marking a conflicted path resolved and completing the merge go through `internal/git` like every other git operation. No package outside `internal/git` execs git for this, and the new mutators sit on `Worktree`, never on `Clone`.
- The application never targets the base branch, never force-pushes, and never rewrites existing commits on the task branch.
- If the application fails at any point, the branch is left byte-identical to its pre-application state — no partial write, no `MERGE_HEAD`, no half-resolved index — and the proposal returns to a terminal reported state rather than being retried inside the run. US-10's guarantee covers this path.
- Every application is disclosed. The comment states that the content originated from a model, names the model, names the confirming human, lists the files written, and links the resulting commit.
- `suggest_conflict_resolution: false` disables US-11 and US-12 together: no proposal is created, so no confirmation is possible.

## Edge Cases & Error Handling

- Base branch missing on the remote (`origin/<base>` does not resolve) → outcome `failed`; run refused via the existing `failuref` path; comment names the missing ref and the configured base branch.
- Worktree does not exist yet (first run of a task) → branch is cut from the freshly fetched base; `Behind == 0` by construction; outcome `up_to_date`.
- Branch has no commits of its own (`Ahead == 0`, `Behind > 0`) → still a merge, not a reset; outcome `synced`; the merge is a fast-forward and `MergeResult.Head` is the base tip.
- `Ahead == 0` and `Behind == 0` → `up_to_date`, silent.
- `merge-tree` unsupported by the installed git (older than 2.38) → outcome `failed` with a `*CommandError`; run refused; message names the required git version. No fallback to an in-worktree trial merge.
- `MergeBase` fails after a clean preview (race: base moved between preview and merge) → abort the merge (`git merge --abort`), outcome `failed`, run refused, branch unchanged. No retry inside the run.
- Merge succeeds but the later end-of-run push fails → existing push-failure reporting applies; the sync section still renders with `Pushed = false` and no permalink.
- Uncommitted changes present in the worktree before the merge (possible after an interrupted previous run) → do not merge; outcome `failed`; run refused; the comment names the dirty files. litefactory does not stash, reset or discard.
- Conflicting file list is very large → render the first 20 paths and "and N more".
- Repeated runs on a still-conflicting branch → each run reports the block once, in that run's own comment. No separate or duplicated comment stream, no escalating notification.
- Both `lf:sync=on` and `lf:sync=off` present on the issue → ambiguous: refuse the label, use the config default, and note the ambiguity in the comment. Never unioned, never first-wins.
- Label prefix matched case-insensitively but not byte-exact, or a value outside `{on, off}` → refused with a note, config default applies, and the raw label text is not carried onward.
- Auto-sync introduces a merge commit that a later squash-merge of the task branch collapses; that is expected and is the reason squash is the recommended base-branch history.
- Agent (claude) has already committed for itself during a prior run → irrelevant to this feature: drift is computed from refs, not from who authored the commits.
- Model call fails, times out, or returns unparsable output → no proposal; the block is reported exactly as US-4 does, plus one line stating no suggestion was produced.
- Model returns a resolution for a path outside the conflict list, or content still containing conflict markers → whole suggestion rejected; no proposal is created; the comment says the suggestion was discarded as unusable.
- All conflicting files exceed the byte budget, or all are binary/non-UTF-8 → no proposal; the comment names why.
- Some conflicting files fit and others do not → a partial proposal covering the ones that fit; the excluded paths are named; confirming it resolves only those files and the merge is not completed if conflicts remain.
- Owner comments "yes" or "looks good" with no proposal ID → not a confirmation; nothing is written; litefactory replies with the exact phrase required.
- A non-owner comments the confirmation phrase → refused; nothing is written; the refusal is reported in the thread.
- Owner confirms a proposal whose base or branch tip has moved → `superseded`; nothing is written; the comment asks for a fresh run to regenerate the suggestion.
- Owner confirms a proposal past its TTL → `expired`; nothing is written; same guidance.
- Owner confirms the same proposal twice → the second confirmation is a no-op and is reported as already applied; no second commit.
- Owner confirms an older proposal after a newer one superseded it → refused; the comment names the current proposal ID.
- A confirmation arrives while another run for that session is in flight → the application waits on the existing session/clone serialization; it never runs concurrently with another turn on the same worktree.
- Application succeeds locally but the push fails → existing push-failure reporting; the merge commit stays on the local branch; the comment renders without a permalink.
- Owner edits the suggestion text in the thread and confirms → the edited text is not applied; only the stored `Resolution` is. Editing a suggestion is out of scope.
- Issue labelled `lf:sync=off` → the run is never `blocked`, so no proposal is ever produced for it.

## Out of Scope

- litefactory merging, fast-forwarding or pushing anything into the base branch.
- litefactory creating, updating, approving or merging pull requests.
- Rebase, force-push, `git pull --rebase`, or any history rewrite of a task branch.
- Automatically starting an agent turn to resolve a conflict (Renovate-style branch regeneration), or resolving a conflict by discarding, rewriting or regenerating already-committed work on the task branch.
- Applying a model-suggested resolution without an explicit human confirmation, under any config value, label, flag or environment. US-11/US-12 are not a route back to auto-resolution.
- Holding a run open, polling, sleeping or otherwise waiting inside a single run for the owner's confirmation.
- Suggesting resolutions for anything other than a US-4 `blocked` sync — no PR-review conflicts, no failing builds, no agent errors.
- Partial or interactive confirmation (accepting some files of a proposal and rejecting others), or applying an owner-edited version of the suggested text.
- A new `config.Tier` value, a dedicated "reasoning" tier, or any per-story model selection beyond the existing default.
- GitHub merge queue configuration or any repo-settings change made by litefactory.
- Time-based or idle-based staleness (drift is measured in commits only).
- A per-issue base-branch override (`lf:base=`); the base branch stays a repo-level property.
- A per-project base-branch hierarchy or multi-base topology.
- Any new CLI command or flag.
- Closing the claude `Bash` + additive `--allowedTools` loophole that lets an agent run git for itself; tracked separately.
- Conflict resolution UI, web editor guidance beyond the text of the refusal comment, or any change to GitHub's own conflict editor behaviour.
- Cross-task coordination, ordering or serialization of runs to avoid conflicts.

## Acceptance Criteria

- [ ] AC-1: With the task branch level with `origin/<base>`, a run produces no sync section in its comment and writes `SyncOutcome = up_to_date`.
- [ ] AC-2: With the base 3 commits ahead and no overlapping files, a run merges the base in, the agent runs on the merged tree, the comment states the branch was 3 commits behind and was synced, and `SyncOutcome = synced` with a non-empty `SyncCommitSHA`.
- [ ] AC-3: With the base and the branch both editing the same lines of the same file, the agent is never started, the run ends with `ReplyRefusesSync`, the comment lists the conflicting file paths, and the branch SHA is unchanged from before the run.
- [ ] AC-4: `git.Clone` still has no `Commit`, `Checkout`, `Switch`, `Stash`, `Reset`, `Merge` or `Pull` method; the only mutators in the module remain `Worktree.Commit`, `Worktree.Push`, `Worktree.MergeBase` and the new resolution mutators from US-12.
- [ ] AC-5: `internal/git/exec.go` is still the only file in the module that execs git for these operations; a test asserting the single exec point passes unchanged.
- [ ] AC-6: `Divergence` returns `Ahead`/`Behind` matching `git rev-list --left-right --count base...branch` on a fixture repo covering all four combinations of ahead/behind zero and non-zero.
- [ ] AC-7: `MergePreview` reports `Clean = false` with the exact conflicting paths and creates no ref, no branch and no worktree change; the worktree status before and after is identical.
- [ ] AC-8: `settings.auto_sync_base: false` makes a behind branch report `skipped` and run normally, with the behind count still disclosed in the comment.
- [ ] AC-9: An issue labelled `lf:sync=off` reports `skipped` even when `auto_sync_base: true`; an issue labelled `lf:sync=on` syncs even when `auto_sync_base: false`; a duplicated, miscased or out-of-vocabulary label is refused and the config default applies.
- [ ] AC-10: With `auto_sync_max_behind: 2` and a branch 5 commits behind, no merge is attempted, the outcome is `skipped`, and the comment names both the count and the cap.
- [ ] AC-11: The drift check and any merge occur within the single `CloneGate` acquisition in `prepareClone`; a test asserts exactly one `Acquire` per prepare.
- [ ] AC-12: A worktree left dirty before the run causes outcome `failed`, a refusal, and no merge; the dirty files are named in the comment.
- [ ] AC-13: A forced `MergeBase` failure leaves no `MERGE_HEAD` in the worktree and the branch at its original SHA.
- [ ] AC-14: The run-sync migration applies to a database created at the preceding head and the four new columns default correctly for pre-existing rows.
- [ ] AC-15: `ReplyRefusesSync` is a valid `ReplyKind` with a `String`, and `replyKind()` in `internal/turn/report.go` maps to it; the enum-exhaustiveness test passes. The same holds for `ReplySuggestsResolution` and `ReplyAppliesResolution`.
- [ ] AC-16: No code path in the module pushes to, or creates a commit on, `Repo.BaseBranch`; a test asserts the push refspec always targets the session branch.
- [ ] AC-17: A blocked run with `suggest_conflict_resolution: true` posts one comment containing the conflicting files, a per-file suggested resolution, the model name, a proposal ID and the confirmation phrase, and the branch SHA is unchanged after the run.
- [ ] AC-18: With the model call stubbed to fail or time out, a blocked run still produces the full US-4 refusal, no proposal row is written, and the comment states that no suggestion could be produced.
- [ ] AC-19: With `suggest_conflict_resolution: false`, a blocked run is byte-identical in behaviour and comment content to the US-4-only path; no model call is made.
- [ ] AC-20: A test asserts that no code path writes to the worktree, the index, or a commit for a proposal whose status is not `confirmed` — the confirmation gate has no bypass.
- [ ] AC-21: A confirmation comment from a non-owning author is refused, writes nothing, changes no proposal status away from `pending`, and is reported in the thread.
- [ ] AC-22: A confirmation naming an unknown, expired, superseded or already-decided proposal writes nothing and is reported with the reason.
- [ ] AC-23: A valid confirmation whose stored `BaseSHA`/`BranchSHA` no longer match the current tips marks the proposal `superseded`, writes nothing, and asks for a fresh suggestion.
- [ ] AC-24: A valid confirmation on an unchanged branch writes only the files named in the proposal, produces exactly one merge commit carrying the run trailers plus the proposal/confirmer trailer, and the comment names the model, the confirmer, the files and the commit.
- [ ] AC-25: A forced failure part-way through application leaves the branch at its pre-application SHA with no `MERGE_HEAD` and no modified tracked files.
- [ ] AC-26: A model response naming a path outside `MergePreview.Conflicts`, or containing conflict markers, produces no proposal; a test covers both cases.
- [ ] AC-27: Conflicting file content placed in the model prompt is byte-capped in total per `conflict_content_max_bytes` and passes the same untrusted-text treatment as existing untrusted prompt fields; a test asserts the cap and the treatment.
- [ ] AC-28: No run is held open waiting for a confirmation; a test asserts the blocked run reaches a terminal state after posting the proposal.

## Open Design Questions (architecture, not requirements)

These are surfaced by the 2026-09-06 research pass and are deliberately left for the architect. None of them changes what the feature must do; each changes how it is built.

| ID | Question | Why it is open |
|----|----------|----------------|
| ODQ-23 | Does the suggestion call reuse the existing narrow classifier/proposal call shape and its narrowing adapter, or does a large untrusted hunk payload need a different interface? | The existing calls carry short forge text; this one carries file content |
| ODQ-24 | Does the propose→confirm→apply flow reuse the schedule-proposal pattern (persisted pending row + a later comment naming it + author-ownership check), and what new store state does it need? | No "awaiting human confirmation" state exists in the store; no mid-run pause/resume exists anywhere |
| ODQ-25 | Is net-new scoped index-resolution and merge-completion surface in `internal/git` in scope for this feature, and should it get its own allowlist-style test? | No scoped `git add <path>`, no index inspection and no merge-completion primitive exists today |
| ODQ-26 | What trailer convention marks a confirmed model-assisted resolution commit? | Today's trailers are task/mode/run only; provenance for model-authored, human-confirmed content is new |
| ODQ-27 | Is the existing neutralise-and-truncate treatment plus a byte budget sufficient for a categorically larger structured payload of file content? | Every existing capped field is short free text, not source code |
| ODQ-28 | Should this story get a dedicated security review pass before implementation? | It is the first outbound path that sends repository file content to a model and the first inbound path where model text can reach a commit |
| ODQ-20 | Confirm the next free migration number at implement time. | Head moved to `0006_schedule_block_replies.sql`; the originally planned `0005` collides |

## Assumptions Recorded (resolved without the owner, headless run)

| # | Open question | Decision taken | Based on |
|---|---|---|---|
| A-1 | Build staleness/drift now or defer | Build now; it is the whole feature | Brief's recommended scope names it as the in-scope core |
| A-2 | Does litefactory open PRs | No; explicitly out of scope | Brief's hard boundary "never merge into the shared base"; PR creation is a distinct capability with its own auth surface |
| A-3 | Base-branch mis-resolution reporting | Missing `origin/<base>` is a run refusal via the existing `failuref` path, not a silent skip | `prepareClone` already turns world-state problems into reportable failures |
| A-4 | Documentation audience | Not a spec deliverable; the refusal comment itself carries the plain-language instructions | Issue #70 is about the owner's experience in the issue thread |
| A-5 | Retry semantics for a failed sync | No in-run retry; the next run re-measures from scratch | Every run already re-fetches; drift is cheap to recompute and a retry loop hides a moved base |
| A-6 | `lf:merge=` / `lf:base=` label lever | `lf:sync=on\|off` only; no `lf:base=` | Single-purpose label levers are the established pattern; base branch is repo-level state in `store.Repo` |
| A-7 | Concrete thresholds | `auto_sync_max_behind: 100`, no time-based threshold | Commit count is directly observable and actionable; an age threshold implies an action nobody defined |
| A-8 | Drift comment placement | Folded into the existing end-of-turn run report, not a separate comment at run start | One run, one comment is the established shape; a second comment doubles thread noise for the non-technical reader |
| A-9 | Codex sandbox git-network behaviour | Not relevant; all git runs in litefactory's own process, never in the agent sandbox | The centralization principle from the brief's third pass |
| A-10 | Closing the claude `Bash`/`--allowedTools` loophole | Out of scope, flagged for separate tracking | It is a sandbox-policy problem, not a merge problem; folding it in would double the blast radius |
| A-11 | Per-project base-branch hierarchy | Out of scope; single repo-level base | No current data model supports more than one base per repo |
| A-12 | Flag-a-human vs regenerate on conflict | Flag a human; refuse the run | Regeneration means discarding committed agent work, which contradicts "litefactory commits; nobody in here does" as a preservation guarantee |
| A-13 | Merge vs rebase for sync | Merge, `--no-ff` | Rebase needs force-push, which `Worktree.Push` deliberately lacks, and would rewrite run trailers |
| A-14 | Whether a conflict blocks the run | Yes by default, `block_run_on_conflict: true`, operator-overridable | An agent building on a tree that will be discarded wastes a run and produces a misleading diff |
| A-15 | Persistence of the sync outcome | Persist on `runs` via the next free migration | Audit and log rendering need it; it is four columns, not a new table |
| A-16 | Conflict-file list length | Truncate at 20 with a remainder count | Matches how the existing files-touched list keeps comments readable |
| A-17 | Keep or drop the `lf:sync=` label after the owner's pushback | Keep, unchanged | The only alternative escape from a false-positive block is flipping `block_run_on_conflict` repo-wide, which un-blocks every issue's real conflicts, not just the false-positive one; the label's blast radius is one issue and is disclosed |
| A-18 | Which model "the reasoning model (default one)" means | Whatever the existing narrow, tool-free model call resolves to by default (the documented default tier) | No "reasoning" tier exists in the tier vocabulary; inventing one would add a config concept the owner did not ask for |
| A-19 | One story or two for suggest-and-apply | Two (US-11 propose-and-post, US-12 confirm-and-apply), with US-12 depending on US-11 | Different triggers, different risk surfaces, and only US-12 writes to the branch; the architect makes the final decomposition call |
| A-20 | Default for `suggest_conflict_resolution` | `true` | The owner asked for the behaviour; the propose half writes nothing, and the config value exists as the off switch. The architect or a security review (ODQ-28) may flip this default |
| A-21 | Confirmation trigger shape | An issue comment naming the proposal ID plus a fixed confirmation phrase | Comments are the only owner-facing channel litefactory already listens on; reactions and edits are not reliably observable as consent |
| A-22 | Who may confirm | The same ownership check already applied to schedule proposals | Reusing the established check avoids inventing a second, divergent notion of "the owner" |
| A-23 | Proposal lifetime | `conflict_proposal_ttl_hours: 168` (7 days), `0` = no expiry | A stale suggestion computed against a long-moved tip is more dangerous than no suggestion; a week matches a normal review cadence |
| A-24 | Conflict content byte budget | `conflict_content_max_bytes: 32768` total | Same order as the existing bounded-history budget; large enough for realistic conflicts, small enough to bound the new outbound surface |
| A-25 | Behaviour when only some conflicting files fit the budget | Produce a partial proposal and name the excluded files | A partial suggestion plus honest disclosure beats no help at all; the merge simply is not completed while conflicts remain |
| A-26 | What happens if the branch moved between proposal and confirmation | `superseded`; nothing written; a fresh run must regenerate | Applying content computed against a different tree is exactly the failure mode the confirmation gate exists to prevent |
| A-27 | Whether the confirmation gate may ever be configurable off | No; it is non-negotiable and has no config, label or flag escape | Without the gate this story becomes the auto-resolve pattern already rejected in A-12 and Out of Scope |
| A-28 | Correction: the `lf:sync=` label pattern reference | Corrected from `lf:tools=` to the live `agentLabelPrefix`/`modeLabelPrefix` levers in `internal/daemon` | `lf:tools=` is dead code kept only so `legacyToolsLabel` can refuse it; copying it would produce a label litefactory silently refuses |
| A-29 | Correction: the run-sync migration number | No longer fixed at `0005`; take the next free number (head is `0006_schedule_block_replies.sql` as of 2026-09-06) | The 2026-09-06 re-validation found the number now collides |
