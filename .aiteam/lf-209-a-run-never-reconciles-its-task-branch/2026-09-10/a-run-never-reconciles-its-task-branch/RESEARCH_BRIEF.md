# Research Brief — a-run-never-reconciles-its-task-branch

## Request

Research issue `github:marioatcloudvane/litefactory#209`, “A run never reconciles its task branch with origin, so operator pushes are invisible to it.” The original instruction was “research this!” This brief is updated for run 0004, whose supplied mode remains Research, on `lf/209-a-run-never-reconciles-its-task-branch`.

[O] The owner reports that externally pushed task-branch commits were absent from the factory worktree on #170 and #195. For #195, the supplied account identifies remote commits `b05b456` and `cdbb6b3`, local ancestor `e939922`, worktree merge `1d5186d`, an ahead-3/behind-2 status, and runs 6–10 repeatedly reporting an already remotely fixed migration collision. These incident details and commit histories were not independently inspected in this run.

[O] Run 0004 settles the previously discussed policy questions with the owner's instruction:

> when automatically can be done, always do it
>
> for everything else, we do it the same way as with the checkout we already have: we suggest a solution and the user must accept it.
>
> plan this now out, but don't over-complicate it!

This is a controlling policy decision for the next Plan, not wholesale adoption of the assistant's run-0003 recommendations. The seven groups of policy questions are closed under that direction; they must not become another owner questionnaire. The factory-supplied run header still selects Research. This run records the decision and the bounded Plan handoff; it does not create formal Plan artifacts or story proposals.

Provenance: `[R0]` marks repository findings carried forward from the run-0001 delegated codebase tour, not freshly verified in run 0004. `[O]` marks supplied owner evidence or decisions; `[V]` would mark new repository findings verified this run; `[W]` would mark second-hand unverified information. Existing path/line citations are retained as prior-run provenance and may need relocation against the implementation checkout. Run 0004 updates policy only; no fresh code investigation, test execution or incident reproduction is claimed.

### Settled owner policy and bounded interpretation

- **Automate ordinary reconciliation.** Automatically fast-forward clean behind-only state and merge compatible divergence when existing safeguards permit it. Equal and ahead-only state require no merge. Do not add routine approval prompts or a configurable stale-tree escape hatch.
- **Reuse the existing proposed-resolution and acceptance experience for everything that cannot complete automatically.** Preserve local work, explain the concrete obstruction, propose a specific recovery, and require owner acceptance before executing that proposal. A blocked task reconciliation must not silently become ordinary execution against stale inputs. Dirty work, conflicting merges and exceptional histories belong to this policy, rather than newly invented independent recovery workflows.
- **“Automatically can be done” means safely within existing work-preservation and approval constraints.** A technically possible destructive reset, force-push, discard, or application of an approval to changed inputs is not implicitly authorized. Mechanical merge success also does not replace required verification of the resulting tree.
- **Keep the change small.** Reuse existing Git operations, conflict advisor/proposal machinery, reporting terms and ownership boundaries. Extend them only where necessary to identify task versus base reconciliation, preserve freshness bindings and publish successful local preparation results.
- **Keep existing mode and evidence obligations.** Validate's retained-evidence review remains read-only; it must not silently assess a different snapshot. Proposed recovery remains bound to the inputs accepted by the owner. Preserve failed-run work and truthful publication status.


## Codebase Context

### Architecture and ownership

[R0] This is a single Go module. `internal/turn` owns preparation/execution policy, `internal/git` owns Git subprocesses and mutation safeguards, `internal/store` owns persistence, and `internal/report` renders outcomes without importing `turn`. `prepareClone` coordinates clone access through `CloneGate` (`internal/turn/prepare.go:523`). No additional package boundary is evidently required for this concern.

[R0] The ordinary execution flow opens the registered clone, resolves author identity, fetches, freezes or reuses task identity, ensures a worktree, synchronizes the base, captures HEAD, allocates a run, prepares prompt/resume context, invokes the runner, and conditionally saves and pushes (`internal/turn/prepare.go:102`, `internal/turn/prepare.go:178`, `internal/turn/prepare.go:266`, `internal/turn/harvest.go:365`). Expected operational failures become reportable results; database failures remain errors. Git/store public accessors take context first (`.aiteam/APP_CONSTRAINTS.md:139`, `.aiteam/APP_CONSTRAINTS.md:175`).

### Confirmed task-branch reconciliation gap

[R0] After fetching and ensuring the worktree, ordinary preparation calls `syncBase`, comparing the task branch with `origin/<repo.BaseBranch>`. It does not compare the existing local task branch with its own remote-tracking ref (`internal/turn/prepare.go:545`, `internal/turn/prepare.go:560`, `internal/turn/prepare.go:782`).

[R0] `EnsureWorktree` returns existing registered worktrees without reconciling their branch. When creating a worktree, an existing local branch takes precedence over the remote task branch (`internal/git/ensure.go:122`, `internal/git/ensure.go:166`). Therefore the gap includes both reused worktrees and newly created worktrees based on an existing stale local task branch.

[R0] Remote-only recovery already works: if the local branch is absent and the remote task branch exists, the worktree starts from that remote branch. An entirely new branch starts from the remote base (`internal/git/ensure.go:173`, `internal/git/ensure.go:194`). “Never consults the task remote” is consequently too broad; the defect concerns reconciliation after local task state exists.

[R0] `Clone.Fetch` invokes `fetch --prune -- origin`, without an explicit refspec, and successful fetch time is recorded (`internal/git/clone.go:127`, `internal/turn/prepare.go:1309`). The owner's claim that every remote-tracking branch is current depends on the registered clone's configured fetch refspec. The code does not itself guarantee that every task branch is covered.

### Existing primitives and missing behavior

| Concern | Verified capability or gap | Evidence |
| --- | --- | --- |
| Exact remote task-ref lookup | `Clone.ShowRef` can look up `refs/remotes/origin/<branch>` and distinguish `ErrRefNotFound`; already used in remote-only recovery. | `internal/git/clone.go:433`; `internal/git/ensure.go:166` |
| Measure divergence | `Worktree.Divergence` accepts an arbitrary supplied ref; its base-oriented naming does not restrict its input. | `internal/git/merge.go:44` |
| Preview merge | `MergePreview` likewise accepts a supplied ref. | `internal/git/merge.go:140` |
| Perform merge | `MergeBase` always uses `--no-ff --no-edit`; the pinned worktree surface has no dedicated fast-forward operation. | `internal/git/merge.go:332`; `internal/git/merge.go:390`; `internal/git/surface_test.go:84` |
| Record/render synchronization | One synchronization result exists per run, explicitly describing base synchronization. This is not a two-phase representation. | `internal/store/runs.go:148`; `internal/store/runs.go:406`; `internal/report/run.go:858`; `internal/report/run.go:892` |
| Push protection | Save uses a non-force explicit task-branch refspec; a failed push preserves local commits. | `internal/git/push.go:50`; `internal/git/push.go:78` |

All table findings are [R0].

### Execution paths that affect scope

[R0] Cold and resumed ordinary runs share preparation. A refused resume falls back to a cold prompt without repeating fetch/preparation (`internal/turn/harvest.go:198`). Reconciliation placement must therefore distinguish a run-start invariant from an attempt-start invariant.

[R0] Validate deliberately skips base synchronization because it reviews retained evidence (`internal/turn/prepare.go:777`). Adding a generic task reconciliation before this branch could introduce an additional mutation into that mode. Plan must preserve read-only retained-evidence review under the settled policy; historical acceptance must remain bound to its assessed snapshot.

[R0] Confirmed conflict resolution shares fetching and worktree creation but deliberately bypasses ordinary base synchronization. It compares current local HEAD and remote base SHA with the approved proposal, supersedes proposals whose bindings changed, then applies and pushes. Remote task tip is not part of that freshness comparison (`internal/turn/apply.go:299`, `internal/turn/prepare.go:567`, `internal/turn/prepare.go:612`). A fix limited to the ordinary `syncBase` call would leave this distinct path to be evaluated separately. Any change must preserve approved-SHA bindings instead of silently applying an approval to a different tree.

## Domain / Problem Space

### Why the observed failure is plausible

[R0] The ordinary preparation path can fetch operator commits while continuing from existing local task state and synchronizing only against the base. The agent can consequently describe its worktree accurately while the owner's expected remote fix remains absent. This confirms the mechanism in the issue without independently establishing the historical incidents (`internal/turn/prepare.go:545`, `internal/turn/prepare.go:782`, `internal/git/ensure.go:122`).

[O] The owner identifies conflict recovery, rescuing work from a failed classification, and completing work through a different harness as reasons operators push externally. These reports motivate treating task-branch authorship as shared rather than assuming exclusive daemon ownership. The exact cited incidents were not reproduced.

The research supports making task-branch freshness an explicit preparation concern. A start-of-run check can detect or incorporate fetched external work, but is not a guarantee that the branch will remain current throughout a long-running agent execution.

### Policy is more nuanced than “reuse base sync”

[R0] Current base synchronization evaluates up-to-date state, disabled override, distance cap, dirty deferral, preview, and merge in that order (`internal/turn/prepare.go:795`, `internal/turn/prepare.go:892`). Base conflicts block only when `BlockRunOnConflict` is enabled; otherwise the run proceeds with disclosure. Excess distance blocks unconditionally (`internal/turn/prepare.go:152`, `internal/turn/prepare.go:169`). Thus the issue's description of universal existing conflict refusal needs qualification.

[R0] Dirty-worktree recovery intentionally preserves edits and can let the runner inspect them; this is also a normative constraint (`internal/turn/prepare.go:795`, `.aiteam/APP_CONSTRAINTS.md:185`). Blindly treating any inability to reconcile as an unconditional refusal could remove that recovery route. Conversely, proceeding with an annotation could preserve the reported stale-work problem. Under the run-0004 policy, Plan must route an obstruction through proposed recovery and owner acceptance while preserving unfinished edits; it must not silently continue stale normal execution.

Run-0004 owner decision supersedes the former merge-versus-refusal question: retain a genuinely absent remote branch as an ordinary path, automatically fast-forward clean behind-only state, and automatically merge compatible divergence when safeguards allow it. Otherwise propose a recovery for owner acceptance using the existing flow.

### Reporting and save semantics are part of correctness

[R0] Prelaunch refusal happens before segment/run allocation; ordinary synchronization metadata is persisted afterward (`internal/turn/prepare.go:145`, `internal/turn/prepare.go:266`). A run-column vocabulary alone cannot persist a refusal for a run that does not exist. Reporting design must account for this lifecycle, rather than merely introducing another outcome string.

[R0] The save baseline `shaBefore` is captured after synchronization. Push happens only if post-run HEAD differs from that baseline (`internal/turn/prepare.go:178`, `internal/turn/harvest.go:415`, `internal/turn/harvest.go:425`). A preparation-created merge followed by an otherwise unchanged successful run can remain local; existing base-sync coverage explicitly exercises this shape (`internal/turn/syncpersist_test.go:26`). If task reconciliation may merge divergent histories, merely creating the correct local merge does not settle when that merge reaches the remote.

## Constraints & Risks

1. **[R0] Preserve existing Git safeguards.** Merge refuses dirty state, staged/tracked reserved transport, and differing reserved paths before mutation. Recovery aborts and verifies HEAD rather than resetting (`internal/git/merge.go:346`, `internal/git/merge.go:440`). Relevant constraints protect the registered clone, retained dirty work, failure-time commit behavior, and approved proposals (`.aiteam/APP_CONSTRAINTS.md:52`, `.aiteam/APP_CONSTRAINTS.md:185`, `.aiteam/APP_CONSTRAINTS.md:204`, `.aiteam/APP_CONSTRAINTS.md:205`).
2. **[R0] Two results cannot be represented faithfully by silent overwrite.** Task sync and base sync may have different outcomes, refs, counts, and conflicts; current storage/rendering is base-specific (`internal/store/runs.go:148`, `internal/report/run.go:858`). Reuse terminology where appropriate, but explicitly settle data representation and historical report compatibility.
3. **[R0] External writers remain outside the clone gate.** Daemon coordination cannot serialize operator pushes. A later remote update can still reject the final push; non-force push preserves the local result (`internal/turn/prepare.go:523`, `internal/git/push.go:50`). There must be an honest recovery report rather than a claim of perpetual synchronization.
4. **[R0] A missing tracking ref is not independently proof that the remote branch is absent.** Fetch coverage is configuration-dependent (`internal/git/clone.go:127`). The design should state its refspec assumption and how missing references are handled.
5. **Exceptional history detection.** Force-rewritten or unrelated remote histories and deleted branches must follow the settled automate-or-propose policy; ahead/behind counts alone do not establish the reason for divergence. Do not create a separate history-tracking subsystem by default. The supplied #195 divergence is owner evidence of a practical case, not a test of all possible histories.
6. **[R0] Separate preparation contracts matter.** Ordinary runs, Validate, and confirmed conflict resolution differ in their mutation and evidence obligations (`internal/turn/prepare.go:567`, `internal/turn/prepare.go:777`). A shared insertion point may have wider effects than its apparent size suggests.

### Verification evidence and likely coverage boundaries

[R0] Existing tests use real local Git repositories, migrated SQLite, and fake runners (`internal/turn/fixture_test.go:74`). Relevant coverage includes base synchronization (`internal/turn/sync_test.go:139`, `internal/turn/sync_test.go:279`, `internal/turn/sync_test.go:317`, `internal/turn/sync_test.go:364`), dirty deferral (`internal/turn/deferred_dirty_test.go:86`, `internal/turn/deferred_dirty_test.go:178`), approved resolution (`internal/turn/apply_test.go:378`), clone coordination (`internal/turn/syncconcurrency_test.go:37`), persistence (`internal/turn/syncpersist_test.go:26`), and worktree behavior (`internal/git/worktree_test.go:105`, `internal/git/worktree_test.go:130`).

No task-upstream reconciliation path exists to exercise today. Future acceptance coverage should distinguish: existing worktree, existing local branch/new worktree, remote-only recovery, no remote task branch, equal/ahead-only/behind-only/diverged state, conflicts, dirty recovery, preparation-only merges, mode-specific behavior, reports/refusal durability, and an external push after the initial fetch. These are research coverage boundaries, not an implementation task breakdown.

[R0] Standard checks are format, vet, pinned lint, and race-enabled tests through `make check` (`Makefile:22`, `Makefile:26`, `Makefile:57`). Run 0001 performed static research only. Run 0004 updates this brief and manifest only; neither run changed production code, executed tests, queried the network/forge, or reproduced historical incidents.

### Adjacent speculation

[R0] The session record holds branch identity/path/state, not a branch-tip SHA; preparation reads HEAD from Git (`internal/store/sessions.go:28`, `internal/store/sessions.go:58`, `internal/turn/prepare.go:178`). The issue's suggested “session recorded branch tip” exposure is therefore not established by this structure.

[R0] Issue materialization uses discovery data or `GetIssue` for unresolved candidates (`internal/daemon/tick.go:984`, `internal/daemon/tick.go:997`). Managed-story revisions are intentionally frozen (`.aiteam/APP_CONSTRAINTS.md:198`). These findings do not establish a general cached-issue-body defect. That subject requires a separate concrete freshness failure and should not be bundled into this branch-reconciliation change.

## Open Questions

No further owner policy answers are required before Plan. Run 0004 resolves the seven previously listed groups through safe automation, otherwise a proposed solution requiring owner acceptance. The remaining items are engineering choices for Plan, not blockers to request from the owner:

1. **Reuse boundary for proposed recovery.** Locate the smallest extension of the existing checkout/base-conflict proposal and acceptance path that can target task-branch reconciliation. Bind the proposal to local HEAD and the relevant remote inputs; changed inputs require a fresh proposal rather than reuse of stale approval. Preserve reserved-path and dirty-work safeguards. Detailed applicability of that machinery to dirty work and exceptional histories has not been freshly verified in this run.
2. **Report both phases without losing evidence.** Keep task and base outcomes distinguishable, including a reason and suggested recovery when preparation stops before an agent starts. Choose the smallest necessary storage/report extension; the prior suggestion of an independent preparation-attempt record is not an owner requirement. Do not overwrite one phase's result with the other.
3. **Publish the actual successful result.** Make a preparation-only merge eligible for the normal successful save even when the agent changes nothing. Preserve failed-run local work and existing save restrictions. If another writer moves the remote before publication, automate recovery only when the resulting tree can still satisfy required checks and approval bindings; otherwise propose the concrete recovery for acceptance. The earlier blanket recommendation to defer all late reconciliation to another run is superseded by the owner's automation-first rule. Do not build an unbounded retry loop or claim a rejected push was successful.
4. **Establish what was fetched.** Use a reliable task-ref observation with the existing fetch where coverage allows it; address restrictive fetch coverage with the minimum necessary Git handling. Absence confirmed for a new branch remains silent. Missing evidence is not proof of deletion or rewrite. A new persistent history tracker and mandatory known-deletion refusal were prior recommendations, not settled requirements; do not add them by default. Detectable exceptional cases use the same proposed-recovery flow when safe automation cannot establish a valid result.
5. **Preserve specialized paths.** Keep ordinary cold/resumed execution, retained-evidence Validate and accepted-resolution application consistent with their existing obligations. Safe reconciliation must precede normal work that depends on the task checkout; Validate must not mutate its assessed snapshot, and accepted proposals must not apply to changed inputs.

The architectural choice of how to represent and implement these details belongs to Plan. No additional product questionnaire, configuration matrix or separate approval mechanism is warranted by the owner's direction.

## Recommended scope for Plan mode

Produce a small plan for automatic task-branch reconciliation after fetch/worktree availability and before ordinary base synchronization. Include automatic clean fast-forward and compatible merge, reuse of the existing proposed-resolution/owner-acceptance flow for blocked cases, separate task/base reporting, and successful publication of preparation-created commits. Include the necessary freshness and local-work safeguards, and cover the existing new-branch and remote-only recovery paths without changing their normal behavior.

Use the existing Git/store/report ownership boundaries and local repository test fixtures. Check the meaningful state combinations already identified above, especially operator commits on an existing task branch, compatible divergence, conflicts, dirty work, changed approved inputs and an unchanged agent run after a preparation merge. Keep detailed story boundaries and acceptance criteria for the actual Plan run.

Exclude migration-collision remediation, reconstruction of #170/#195 history, issue-cache changes, acceptance-workflow redesign, automatic mode transitions, destructive branch replacement, a separate recovery approval system, and speculative history-tracking infrastructure. Earlier run-0003 options are discussion history; they must not inflate the plan or override the owner's simpler rule.

### Run assumptions and limitations

Run 0001 established the code-path findings above through a delegated static tour; `.aiteam/rulebook.md` was absent and the tour used relevant project constraints. Historical incidents remain owner-reported. Run 0004 carries those findings forward honestly and records the new owner decision; it does not claim the current checkout has been reverified. Only this supplied same-task session's brief and manifest were used for the update.

The reasonable interpretation of automation preserves work and existing evidence/approval safeguards. Failure to complete automatically leads to a concrete proposed solution and owner acceptance, not a silent stale run or a new generic questionnaire. The latest owner wants planning to proceed, but the factory supplied Research for this subprocess. Research therefore ends with this updated brief and the explicit handoff command: `/plan Implement automatic task-branch reconciliation; reuse the existing proposed-solution and owner-acceptance flow for anything that cannot complete safely automatically; keep the plan small.` No formal Plan, proposal, code changes, tests, forge operations, commits or pushes were performed.
