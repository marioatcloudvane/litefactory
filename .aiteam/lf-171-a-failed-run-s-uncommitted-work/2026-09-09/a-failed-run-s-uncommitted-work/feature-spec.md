# Feature Spec: a-failed-run-s-uncommitted-work
# Status: APPROVED
# Date: 2026-09-09

## Goal
- Let an otherwise eligible owner-triggered retry run with retained work after a failed run, without requiring machine access, losing edits, or committing failed output as successful work.

## User Stories
| ID | Story |
|---|---|
| US-1 | As the task owner, I want synchronization to defer when my task worktree contains retained edits, so a retry can inspect and finish that work. |
| US-2 | As the task owner, I want a dirty-worktree conflict confirmation to explain a usable recovery sequence, so the session can recover without overwriting edits or applying a stale proposal. |
| US-3 | As the task owner, I want planning transport and failed publication covered by recovery tests, so an internal proposal file cannot recreate the retry blockage. |

## Data Model
| Entity | Field | Type | Required | Constraints |
|---|---|---|---|---|
| Existing session | Task identity, branch, worktree | Existing values | Yes | Reuse the supplied session worktree; do not infer ownership of individual dirty bytes. |
| Existing synchronization result | Outcome, reason, branch/base divergence | Existing result with explicit deferred-dirty meaning | Yes when sync defers | Persist and report deferral without claiming successful synchronization. |
| Existing conflict proposal | Branch/base revision, pending status | Existing values | Yes for confirmation | Dirty refusal leaves proposal pending; revision mismatch keeps existing supersession behavior. |
| Existing plan transport | `.litefactory-plan-proposal.json` | Existing JSON file | No | Factory transport remains excluded from task commits and ordinary dirty-work detection. |

### Relationships
- Retry → same task session and worktree → original retained tracked, staged, unstaged, and untracked work.
- Confirmation → existing conflict proposal → original branch/base revision validation.
- Synchronization outcome → existing run/report context → clear explanation of the older base used by the agent.

## API / Interface Changes
| Method | Endpoint / Action | Auth | Notes |
|---|---|---|---|
| Existing owner comment | Start normal retry | Existing task-owner authorization | No new command, label, configuration, permission, or forge capability. |
| Existing conflict confirmation | Apply confirmed resolution | Existing confirmation authorization | Dirty work receives specific recovery guidance; it must not start an unrequested agent run. |
| Existing run report and agent context | Explain deferred synchronization | Existing delivery | Say retained changes were preserved and the base was not merged; distinguish deferral from disabled, up-to-date, conflicted, and successful synchronization. |

## Business Rules
- Choose nonmutating synchronization deferral rather than automatic parking/restoration for this change.
- For a behind-base branch within the existing synchronization distance cap, with synchronization enabled and ordinary dirty changes present, defer before committed-state merge preview and launch the otherwise eligible normal run using the current task branch and retained work.
- Preserve existing up-to-date, explicit sync-disabled, and distance-cap behavior; retained changes do not bypass admission, authorization, or the distance cap.
- Apply deferral to all ordinary dirty changes in the session worktree, including legacy sessions without manifests; this neither asserts factory ownership nor authorizes overwriting them.
- Preserve index contents, working files, untracked contents, file modes, and task HEAD across the deferral step; no parking, cleanup, commit, or reset occurs to make preparation pass.
- A successful agent run may save reconciled work using existing harvest semantics; failed, timed-out, abandoned, or publication-failed output remains uncommitted under existing failure semantics.
- A clean subsequent run resumes normal synchronization and conflict preview automatically.
- Confirmed resolution retains its dirty guard and revision checks; explain that work is preserved, a normal owner comment can launch a recovery run, and a proposal must be regenerated/reconfirmed if that successful run changes its recorded revision.
- A normal recovery run with dirty work can launch even when committed branch/base state would conflict, subject to the existing distance cap; the deferred outcome does not claim that conflict has been resolved.
- Ordinary untracked factory transport remains a nonblocking existing behavior, not a new ownership exemption for arbitrary user files.
- Existing staged, tracked, or incoming-path transport collisions must never silently delete preserved transport or include it in a task commit; unsafe raw-Git failures retain explicit diagnostics rather than being misreported as successful synchronization.

## Edge Cases & Error Handling
- Status inspection fails → fail preparation with the underlying cause; do not interpret unknown state as clean or dirty.
- Retry fails again → preserve work again; another otherwise eligible retry can still launch without manual cleanup.
- Only an ordinary untracked proposal remains → normal clean-tree synchronization proceeds and transport is retained until its existing lifecycle removes it.
- Plan publication fails after successful agent planning → ordinary artifacts remain available to the next retry; publication failure must not force a failed-output commit.
- Both staged and unstaged changes affect one file, with additional untracked files → preparation leaves all layers untouched for the runner.
- Dirty confirmed proposal → leave branch, index, files, and proposal pending; report the normal-retry recovery sequence without falsely marking confirmation applied.
- Dirty recovery completes successfully and changes branch HEAD → old confirmation is superseded under existing checks rather than replayed against new content.
- Explicit sync disabled or branch up to date → retain current outcome semantics; do not relabel as deferred-dirty.
- Behind count exceeds cap → retain refusal and existing explanation; owner can use existing supported sync controls as appropriate.

## Out of Scope
- Durable snapshots, stashes, scratch refs, ownership manifests, crash-recovery state machines, or automatic cleanup.
- Unconditional commits of unsuccessful output or changes to agent success classification.
- Automatic resolution of ordinary retained edits, automatic application of confirmed proposals onto dirty content, or new retry scheduling.
- Manual repairs to #73, live daemon/runtime changes, direct forge access by agents, new public commands or configuration.
- Broad redesign of transport lifecycle or tracked reserved-path migration; preserve current exclusions and provide regression coverage and accurate failure reporting.

## Assumptions
| Source | Decision |
|---|---|
| Research brief identifies deferral as an existing-policy-compatible option; issue seeks recoverability | An explicitly disclosed retry on the current base is acceptable; this avoids moving or claiming retained bytes. |
| Current store has no dirty-byte ownership manifest | Nonmutating deferral covers all session-worktree edits and legacy failures without pretending to attribute them. |
| Existing clean-tree confirmation safeguards | Conflict confirmation remains nonmutating when dirty; recovery uses an ordinary owner-triggered run followed by fresh synchronization/confirmation as needed. |
| Research verifies current proposal status exclusion | Protect the existing untracked transport behavior; do not reintroduce or duplicate the historical filter fix. |
| Headless owner request approves planning | No unresolved owner questions are required to produce this bounded plan. |

## Acceptance Criteria
- [ ] AC-171-1: After a failed run leaves ordinary changes and the base advances within the cap, an otherwise eligible normal retry reaches the agent with task HEAD, index, working-file bytes/modes, and untracked files unchanged by preparation; synchronization is explicitly deferred.
- [ ] AC-171-2: Repeated unsuccessful retries preserve work without task commits, and a later successful retry uses existing save semantics; the next clean eligible run performs normal synchronization.
- [ ] AC-171-3: Deferred synchronization is visible in agent context and the owner report, including branch/base divergence and preserved-work reason, without a successful-sync or successful-failed-work claim.
- [ ] AC-171-4: Up-to-date, sync-disabled, over-cap, status-error, and clean committed-conflict paths retain their existing admission and outcome behavior.
- [ ] AC-171-5: Confirming a valid proposal against a dirty worktree preserves HEAD, index, worktree, and pending proposal and explains the ordinary-comment recovery path; a normal recovery run launches despite committed conflicts, within the cap, and subsequent stale confirmation is superseded.
- [ ] AC-171-6: A failed Plan or failed Plan publication retaining proposal plus ordinary artifacts permits a normal retry; proposal-only untracked state does not cause dirty deferral, and transport never enters a task commit.
- [ ] AC-171-7: Regression coverage exercises staged transport and tracked/incoming reserved-path collisions, proving either safe synchronization with transport excluded from commits or an explicit preserved-state failure, without silent overwrite or false success.
