# Research Brief — a-failed-run-s-uncommitted-work

## Request

Investigate issue #171, “A failed run's uncommitted work permanently blocks every later run via the base-branch sync,” including the separately reported leftover plan proposal and the conflict-resolution path. The triggering instruction was “check the issue here!” This is Research only: identify present behavior, reconcile the report with this checkout, and provide evidence and decisions for Plan mode.

Provenance: **[V]** verified against repository code or tests during this run; **[O]** reported in the supplied owner's issue, not independently checked against historical execution; **[W]** second-hand evidence; **[R0]** carried forward from earlier research. Recommendations and deductions are explicitly called out as such; they are not implemented behavior. Tests were inspected, not executed. No runtime state or forge history was accessed.

**Assumptions:** The current supplied worktree is the implementation under investigation. The supplied account of #73 is historical evidence, not proof that every behavior still exists unchanged. Headless instructions authorize completing the research without questions or confirmation; no implementation or operational repair is authorized by this mode.

## Codebase Context

### Owners and execution order

- [V] `internal/turn` coordinates one run; `internal/git` owns Git operations; `internal/store` persists sessions and runs; `internal/daemon` handles dispatch and retries; `internal/planbrief` parses the planning transport. The descriptive map records these responsibilities at `.aiteam/APP_CONTEXT.md:64`, `.aiteam/APP_CONTEXT.md:76`, `.aiteam/APP_CONTEXT.md:101`, and `.aiteam/APP_CONTEXT.md:110`.
- [V] Preparation acquires the clone gate, fetches, freezes branch identity, ensures the worktree, then synchronizes the base or applies a confirmed conflict proposal (`internal/turn/prepare.go:526`). An existing registered worktree is deliberately reused without cleaning it (`internal/git/ensure.go:61`).
- [V] Harvest attempts managed-plan publication before saving changes (`internal/turn/harvest.go:113`). Saving returns without committing when the agent did not succeed or publication left an error (`internal/turn/harvest.go:275`, `internal/turn/harvest.go:298`, `internal/turn/plan_handoff.go:78`). Preserving unsuccessful work for a future successful run is required by CONC-8 (`.aiteam/APP_CONSTRAINTS.md:185`).
- [V] Sessions record repository, task, branch, worktree and lock identity, but no ownership manifest for dirty files (`internal/store/sessions.go:26`). Runs record outcome/error, commit SHA and sync outcome, but no failed-work content manifest (`internal/store/runs.go:93`, `internal/store/runs.go:135`). `Run.Succeeded` determines semantic run success, not ownership of the current bytes (`internal/store/runs.go:185`).

### Exact condition for the retry loop

[V] `syncBase` first returns when the task branch is not behind the base (`internal/turn/prepare.go:782`). If behind, it checks whether auto-sync is disabled, checks the distance cap, and previews the merge of committed branch/base state (`internal/turn/prepare.go:787`, `internal/turn/prepare.go:802`, `internal/turn/prepare.go:812`). A clean preview reaches `MergeBase` (`internal/turn/prepare.go:859`).

[V] `MergeBase` reads worktree status and rejects any returned change before starting a merge (`internal/git/merge.go:346`). That error escapes preparation before agent launch (`internal/turn/prepare.go:552`, `internal/turn/prepare.go:137`). Sync preparation failures also occur before a run row exists; the store explicitly documents why `SyncFailed` is not persisted (`internal/store/runs.go:74`).

**Deduction from those verified paths:** A prior unsuccessful run can leave ordinary dirty files. When the base has advanced, synchronization is enabled, and the committed merge preview is clean, the next attempt fails before the agent can reconcile those files. Repeating the attempt with the same state repeats the refusal. The two individually intentional behaviors do compose into the reported failure loop.

### Current-code corrections to the issue

1. **The refusal is conditional, not universal.** [V] An up-to-date branch skips merging. The daemon also recognizes the `Sync: Off` issue label and sets `Request.SyncOverride` (`internal/daemon/tick.go:1675`); disabled sync returns before merge preview and the dirty guard (`internal/turn/prepare.go:787`). **Deduction:** This is an existing conditional escape hatch for an otherwise eligible normal run. It preserves failed work and defers synchronization; it is not automatic reconciliation and does not bypass unrelated admission rules or the confirmed-proposal apply route.
2. **A lone ordinary untracked plan proposal no longer trips this guard.** [V] Current status code explicitly excludes `.litefactory-plan-proposal.json`, `.litefactory-story-events`, and children of the latter (`internal/git/status.go:93`). The issue's description of the proposal as sufficient to make `len(changes) > 0` therefore does not describe this checkout.
3. **Historical operational claims remain historical evidence.** [O] Run 8 on #73 reportedly spent 13m16s producing plan artifacts, was misclassified, and left subsequent attempts wedged; manual recovery reportedly required both committing artifacts and deleting the proposal. This research does not establish which exact historical revision ran or independently verify #155, #126, logs, timestamps or the current condition of #73.

## Domain / Problem Space

### Preservation and ownership are different problems

[V] The failure-preservation contract is deliberate and tested: failed, timed-out, abandoned and no-result work stays uncommitted (`internal/turn/harvest.go:275`; `internal/turn/harvest_test.go:26`). The failure path skips status inspection, and tests require empty `Changes` and `SHAAfter` (`internal/turn/harvest_test.go:109`).

**Deduction:** “The branch belongs to this session and its last run failed” is weaker than “these exact dirty bytes came from that run.” The inspected state cannot distinguish pre-existing edits, edits made between runs, and the failed agent's edits. Automatically classifying all dirt as failed-run work would introduce a new ownership assumption. Legacy sessions already affected by the issue lack any future ownership metadata a fix might add.

### Planning transport has a separate lifecycle

[V] Publication only proceeds for a successful Plan result; failed publication leaves the proposal available for inspection. Removal occurs after publication and local index replacement (`internal/turn/plan_handoff.go:72`, `internal/turn/plan_handoff.go:128`, `internal/turn/plan_handoff.go:135`). Commit handling separately unstages factory transport before inspecting and after staging (`internal/git/commit.go:160`, `internal/git/commit.go:173`). Merge has no corresponding normalization in its inspected path.

**Deduction / unresolved edge cases:** Filtering status resolves the ordinary untracked proposal-only guard condition, but does not prove raw Git merge is unaffected by already-staged or tracked transport, or by an incoming tracked path colliding with a local transport file. Commit exclusion coverage is not merge/apply coverage. A recovery design must avoid committing transport or destroying an inspectable failed proposal while resolving those cases.

### Conflict resolution reaches a second dirty guard

[V] `MergePreview` uses `merge-tree` against committed branch/base state without calling worktree status (`internal/git/merge.go:137`). Consequently dirty work does not itself prevent a conflicted preview or proposal generation. `BlockRunOnConflict` controls the refusal following a conflict result (`internal/turn/prepare.go:159`).

[V] On confirmation, preparation bypasses ordinary `syncBase`, validates stored branch/base SHAs and calls `WriteResolvedFiles` (`internal/turn/prepare.go:560`, `internal/turn/prepare.go:575`, `internal/turn/prepare.go:591`). This operation independently refuses dirty work before beginning (`internal/git/worktree_apply.go:97`). Tests explicitly preserve the dirty files and leave the proposal pending after failed application (`internal/turn/apply_test.go:423`, `internal/turn/apply_test.go:435`).

**Deduction:** Reconfirming alone cannot clear this condition. A fix restricted to ordinary base merging would leave the confirmed-resolution route blocked. Conversely, suggesting disabled auto-sync as a universal recovery path would overlook that confirmation takes another route.

### Recovery options and tradeoffs for planning

These are research evaluations, not a selected implementation plan.

| Option | Potential benefit | Limitation / decision needed |
| --- | --- | --- |
| Report and deliberately defer sync while retained work is present | Allows an eligible retry to use existing work; resembles the current disabled-sync escape hatch; preserves the no-failed-commit contract | The agent may run against an older base. Automatic deferral needs an explicit policy, clear reporting, and a separate answer for confirmed conflict application. |
| Tolerate attributable dirty work during synchronization | Could preserve the visible worktree experience | Current state does not establish byte-level attribution. Removing the guard alone is not a demonstrated preservation mechanism; overlapping edits, index state and merge failures need defined handling. |
| Park work in a factory-owned durable snapshot/ref, synchronize, then restore | Separates preserved work from the clean tree required by synchronization, without presenting it as a successful task commit | Needs narrowly scoped Git capabilities, recovery metadata, crash-safe ordering and restoration-conflict handling. Scratch commits or temporary absence from the worktree need an explicit interpretation of CONC-8. |
| Synchronize against an isolated clean state | Could isolate base-merge computation from retained files | The current preview already operates on committed state; safely installing the merged state into the dirty task worktree still requires preservation/reconciliation. Moving the step alone does not solve that requirement. |
| Always commit failed output | Clears the dirty guard | Conflicts with CONC-8 and the issue's requirement to preserve the fix for failed work being claimed as successful. Exclude from recommended scope. |

[V] There is no existing exported parking/snapshot/restore facility in the inspected Git surface. Broad `Stash`, `Reset`, `Clean`, `Checkout`, `Apply` and `Restore` methods are explicitly forbidden (`internal/git/surface_test.go:121`); existing narrow mutators include `MergeBase`, `WriteResolvedFiles`, `StageResolvedFiles` and `CompleteMerge` (`internal/git/surface_test.go:76`). Existing merge rollback aborts its own failed merge and verifies original HEAD, but starts from the clean-tree precondition (`internal/git/merge.go:397`). **Deduction:** That rollback is not already a solution for retaining arbitrary pre-existing modifications.

## Constraints & Risks

- [V] Preserve unsuccessful work and prevent failed-run commits from being mistaken for successful output: CONC-8 (`.aiteam/APP_CONSTRAINTS.md:185`). Merely committing everything would reinstate the behavior rejected by the issue.
- [V] Keep retries in the daemon and preparation ordering in the turn coordinator: CONC-6 / CONC-7 (`.aiteam/APP_CONSTRAINTS.md:180`). Git mutations remain in the designated Git package, with a deliberately constrained exported surface (`internal/git/surface_test.go:76`); clone mutations are prohibited by SEC-7 (`.aiteam/APP_CONSTRAINTS.md:52`).
- **Design risk:** A preservation mechanism must define staged/unstaged differences, untracked content, renames and file modes. “Files still exist” is not necessarily equivalent to preserving all state or avoiding unrelated-file changes.
- **Design risk:** A crash after parking or after merging but before restoration can create a new persistent wedge unless durable recovery state identifies both the saved work and completed phase. Shared clone references also require concurrency design.
- **Design risk:** Restoring retained work onto a newer base can conflict even if the committed branch/base preview was clean. This must be a recoverable, visible state, not silent overwriting or a success claim.
- **Design risk:** Legacy affected sessions have no failed-work manifests, so a design relying exclusively on newly captured metadata would not address the reported population.
- **Reporting risk:** Preparation failures precede run-row allocation (`internal/store/runs.go:74`); recovery diagnostics and evidence cannot assume the usual completed-run record exists.

### Existing verification and gaps

[V] Existing tests establish failure preservation (`internal/turn/harvest_test.go:26`), dirty-merge rejection and preservation (`internal/git/merge_test.go:531`), pending confirmed proposals after dirty rejection (`internal/turn/apply_test.go:435`), and transport exclusion during commit including staged and transport-only variants (`internal/git/worktree_test.go:406`).

[V] Inspected synchronization tests cover up-to-date branches, disabled sync, distance cap, overrides, successful merge and committed conflicts (`internal/turn/sync_test.go:139` onward), but the tour found no composed unsuccessful-run → base advancement → retry coverage. Commit exclusion tests do not establish transport behavior during sync or confirmed application. Existing fake-runner/local-resource fixtures provide a suitable verification pattern (`internal/turn/fixture_test.go:71`). TEST-10 constrains exported surface expectations (`.aiteam/APP_CONSTRAINTS.md:161`).

**Recommended verification scope:** Cover the composed failure and publication-failure scenarios; tracked and untracked retained work; ordinary transport-only status plus staged/incoming-path transport cases; confirmed conflict application; repeated retries; preservation when synchronization or restoration fails; and crash boundaries if parking is chosen. Assert file/index/ref outcomes and absence of failed-work success claims, not only error strings. This run executed no tests and changed no production code.

## Open Questions

Plan mode and the architect should resolve these without assuming the historical report matches current code in every detail:

1. Must recovery bring the base current before agent launch, or may an explicitly reported deferred sync allow the retry to repair retained work first?
2. What evidence is sufficient to attribute dirty content, and what conservative fallback supports already-wedged sessions without manifests?
3. If work is parked, what exact state is preserved, where is it recorded durably, and how is each interrupted phase resumed without loss or duplicate application?
4. How are restoration conflicts represented so the session remains actionable and no incomplete recovery is reported as successful?
5. Should confirmed conflict application use the same preservation/recovery policy, and how does that interact with its branch/base SHA validation?
6. How should factory transport be normalized across sync and apply while retaining failed proposals for inspection and excluding them from task commits?
7. What user-facing diagnostic identifies the preserved work and a safe next action when automatic reconciliation cannot proceed, including failures before run allocation?

No external API/version question was needed to establish the existing-code findings, so web research was not invoked. Exact Git snapshot/restoration mechanics remain a design validation task if that option is selected; this brief does not assert unverified command-level guarantees.

## Recommended scope for Plan mode

Address reconciliation between intentionally retained unsuccessful work and the clean-tree requirements of both ordinary base synchronization and confirmed conflict application. Preserve the no-failed-run-commit rule, support legacy affected sessions, make every blocked or partially recovered state actionable, and include composition tests that cross the current package-local guarantees.

Choose explicitly between controlled sync deferral and a durable preservation/reconciliation mechanism; the repository evidence does not justify silently treating all current dirty files as factory-owned. Treat ordinary untracked proposal filtering as existing behavior to protect, with remaining transport lifecycle and raw-merge edge cases to verify. Exclude unconditional failed-output commits, unrelated agent outcome classification changes, manual mutation of the running daemon's state, and implementation during this Research run.
