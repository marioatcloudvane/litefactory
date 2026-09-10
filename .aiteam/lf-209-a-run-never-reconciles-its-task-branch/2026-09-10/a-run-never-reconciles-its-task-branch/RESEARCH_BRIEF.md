# Research Brief — a-run-never-reconciles-its-task-branch

## Request

Research issue `github:marioatcloudvane/litefactory#209`, “A run never reconciles its task branch with origin, so operator pushes are invisible to it.” The triggering instruction was “research this!” This is run 0001, Research only, on `lf/209-a-run-never-reconciles-its-task-branch`.

[O] The owner reports that externally pushed task-branch commits were absent from the factory worktree on #170 and #195. For #195, the supplied account identifies remote commits `b05b456` and `cdbb6b3`, local ancestor `e939922`, worktree merge `1d5186d`, an ahead-3/behind-2 status, and runs 6–10 repeatedly reporting an already remotely fixed migration collision. These incident details and commit histories were not independently inspected in this run.

Provenance: `[V]` means verified in the repository during this run through the delegated codebase tour; `[O]` means reported by the owner in supplied issue material; `[W]` would mean second-hand and unverified; `[R0]` would mean retained from an earlier run. Recommendations below are research judgments, not approved implementation requirements. Citations refer to repository paths and line numbers at the inspected checkout.

## Codebase Context

### Architecture and ownership

[V] This is a single Go module. `internal/turn` owns preparation/execution policy, `internal/git` owns Git subprocesses and mutation safeguards, `internal/store` owns persistence, and `internal/report` renders outcomes without importing `turn`. `prepareClone` coordinates clone access through `CloneGate` (`internal/turn/prepare.go:523`). No additional package boundary is evidently required for this concern.

[V] The ordinary execution flow opens the registered clone, resolves author identity, fetches, freezes or reuses task identity, ensures a worktree, synchronizes the base, captures HEAD, allocates a run, prepares prompt/resume context, invokes the runner, and conditionally saves and pushes (`internal/turn/prepare.go:102`, `internal/turn/prepare.go:178`, `internal/turn/prepare.go:266`, `internal/turn/harvest.go:365`). Expected operational failures become reportable results; database failures remain errors. Git/store public accessors take context first (`.aiteam/APP_CONSTRAINTS.md:139`, `.aiteam/APP_CONSTRAINTS.md:175`).

### Confirmed task-branch reconciliation gap

[V] After fetching and ensuring the worktree, ordinary preparation calls `syncBase`, comparing the task branch with `origin/<repo.BaseBranch>`. It does not compare the existing local task branch with its own remote-tracking ref (`internal/turn/prepare.go:545`, `internal/turn/prepare.go:560`, `internal/turn/prepare.go:782`).

[V] `EnsureWorktree` returns existing registered worktrees without reconciling their branch. When creating a worktree, an existing local branch takes precedence over the remote task branch (`internal/git/ensure.go:122`, `internal/git/ensure.go:166`). Therefore the gap includes both reused worktrees and newly created worktrees based on an existing stale local task branch.

[V] Remote-only recovery already works: if the local branch is absent and the remote task branch exists, the worktree starts from that remote branch. An entirely new branch starts from the remote base (`internal/git/ensure.go:173`, `internal/git/ensure.go:194`). “Never consults the task remote” is consequently too broad; the defect concerns reconciliation after local task state exists.

[V] `Clone.Fetch` invokes `fetch --prune -- origin`, without an explicit refspec, and successful fetch time is recorded (`internal/git/clone.go:127`, `internal/turn/prepare.go:1309`). The owner's claim that every remote-tracking branch is current depends on the registered clone's configured fetch refspec. The code does not itself guarantee that every task branch is covered.

### Existing primitives and missing behavior

| Concern | Verified capability or gap | Evidence |
| --- | --- | --- |
| Exact remote task-ref lookup | `Clone.ShowRef` can look up `refs/remotes/origin/<branch>` and distinguish `ErrRefNotFound`; already used in remote-only recovery. | `internal/git/clone.go:433`; `internal/git/ensure.go:166` |
| Measure divergence | `Worktree.Divergence` accepts an arbitrary supplied ref; its base-oriented naming does not restrict its input. | `internal/git/merge.go:44` |
| Preview merge | `MergePreview` likewise accepts a supplied ref. | `internal/git/merge.go:140` |
| Perform merge | `MergeBase` always uses `--no-ff --no-edit`; the pinned worktree surface has no dedicated fast-forward operation. | `internal/git/merge.go:332`; `internal/git/merge.go:390`; `internal/git/surface_test.go:84` |
| Record/render synchronization | One synchronization result exists per run, explicitly describing base synchronization. This is not a two-phase representation. | `internal/store/runs.go:148`; `internal/store/runs.go:406`; `internal/report/run.go:858`; `internal/report/run.go:892` |
| Push protection | Save uses a non-force explicit task-branch refspec; a failed push preserves local commits. | `internal/git/push.go:50`; `internal/git/push.go:78` |

All table findings are [V].

### Execution paths that affect scope

[V] Cold and resumed ordinary runs share preparation. A refused resume falls back to a cold prompt without repeating fetch/preparation (`internal/turn/harvest.go:198`). Reconciliation placement must therefore distinguish a run-start invariant from an attempt-start invariant.

[V] Validate deliberately skips base synchronization because it reviews retained evidence (`internal/turn/prepare.go:777`). Adding a generic task reconciliation before this branch could introduce an additional mutation into that mode. Plan needs an explicit policy consistent with read-only retained-evidence review; historical acceptance must remain bound to its assessed snapshot.

[V] Confirmed conflict resolution shares fetching and worktree creation but deliberately bypasses ordinary base synchronization. It compares current local HEAD and remote base SHA with the approved proposal, supersedes proposals whose bindings changed, then applies and pushes. Remote task tip is not part of that freshness comparison (`internal/turn/apply.go:299`, `internal/turn/prepare.go:567`, `internal/turn/prepare.go:612`). A fix limited to the ordinary `syncBase` call would leave this distinct path to be evaluated separately. Any change must preserve approved-SHA bindings instead of silently applying an approval to a different tree.

## Domain / Problem Space

### Why the observed failure is plausible

[V] The ordinary preparation path can fetch operator commits while continuing from existing local task state and synchronizing only against the base. The agent can consequently describe its worktree accurately while the owner's expected remote fix remains absent. This confirms the mechanism in the issue without independently establishing the historical incidents (`internal/turn/prepare.go:545`, `internal/turn/prepare.go:782`, `internal/git/ensure.go:122`).

[O] The owner identifies conflict recovery, rescuing work from a failed classification, and completing work through a different harness as reasons operators push externally. These reports motivate treating task-branch authorship as shared rather than assuming exclusive daemon ownership. The exact cited incidents were not reproduced.

The research supports making task-branch freshness an explicit preparation concern. A start-of-run check can detect or incorporate fetched external work, but is not a guarantee that the branch will remain current throughout a long-running agent execution.

### Policy is more nuanced than “reuse base sync”

[V] Current base synchronization evaluates up-to-date state, disabled override, distance cap, dirty deferral, preview, and merge in that order (`internal/turn/prepare.go:795`, `internal/turn/prepare.go:892`). Base conflicts block only when `BlockRunOnConflict` is enabled; otherwise the run proceeds with disclosure. Excess distance blocks unconditionally (`internal/turn/prepare.go:152`, `internal/turn/prepare.go:169`). Thus the issue's description of universal existing conflict refusal needs qualification.

[V] Dirty-worktree recovery intentionally preserves edits and can let the runner inspect them; this is also a normative constraint (`internal/turn/prepare.go:795`, `.aiteam/APP_CONSTRAINTS.md:185`). Blindly treating any inability to reconcile as an unconditional refusal could remove that recovery route. Conversely, proceeding with an annotation could preserve the reported stale-work problem. Plan must choose the user-visible tradeoff explicitly.

Research recommendation: retain no-remote-branch as an ordinary path, investigate fast-forward for behind-only clean state, and require an explicit policy for true divergence. A clean merge can preserve both writers' commits; refusal avoids automatically joining histories but requires operator recovery. Neither is approved by this research run.

### Reporting and save semantics are part of correctness

[V] Prelaunch refusal happens before segment/run allocation; ordinary synchronization metadata is persisted afterward (`internal/turn/prepare.go:145`, `internal/turn/prepare.go:266`). A run-column vocabulary alone cannot persist a refusal for a run that does not exist. Reporting design must account for this lifecycle, rather than merely introducing another outcome string.

[V] The save baseline `shaBefore` is captured after synchronization. Push happens only if post-run HEAD differs from that baseline (`internal/turn/prepare.go:178`, `internal/turn/harvest.go:415`, `internal/turn/harvest.go:425`). A preparation-created merge followed by an otherwise unchanged successful run can remain local; existing base-sync coverage explicitly exercises this shape (`internal/turn/syncpersist_test.go:26`). If task reconciliation may merge divergent histories, merely creating the correct local merge does not settle when that merge reaches the remote.

## Constraints & Risks

1. **[V] Preserve existing Git safeguards.** Merge refuses dirty state, staged/tracked reserved transport, and differing reserved paths before mutation. Recovery aborts and verifies HEAD rather than resetting (`internal/git/merge.go:346`, `internal/git/merge.go:440`). Relevant constraints protect the registered clone, retained dirty work, failure-time commit behavior, and approved proposals (`.aiteam/APP_CONSTRAINTS.md:52`, `.aiteam/APP_CONSTRAINTS.md:185`, `.aiteam/APP_CONSTRAINTS.md:204`, `.aiteam/APP_CONSTRAINTS.md:205`).
2. **[V] Two results cannot be represented faithfully by silent overwrite.** Task sync and base sync may have different outcomes, refs, counts, and conflicts; current storage/rendering is base-specific (`internal/store/runs.go:148`, `internal/report/run.go:858`). Reuse terminology where appropriate, but explicitly settle data representation and historical report compatibility.
3. **[V] External writers remain outside the clone gate.** Daemon coordination cannot serialize operator pushes. A later remote update can still reject the final push; non-force push preserves the local result (`internal/turn/prepare.go:523`, `internal/git/push.go:50`). There must be an honest recovery report rather than a claim of perpetual synchronization.
4. **[V] A missing tracking ref is not independently proof that the remote branch is absent.** Fetch coverage is configuration-dependent (`internal/git/clone.go:127`). The design should state its refspec assumption and how missing references are handled.
5. **Unsettled history cases.** Force-rewritten or unrelated remote histories, deleted branches, and both-ahead-and-behind state need explicit policy. The supplied #195 divergence is owner evidence of a practical case, not a test of all possible histories.
6. **[V] Separate preparation contracts matter.** Ordinary runs, Validate, and confirmed conflict resolution differ in their mutation and evidence obligations (`internal/turn/prepare.go:567`, `internal/turn/prepare.go:777`). A shared insertion point may have wider effects than its apparent size suggests.

### Verification evidence and likely coverage boundaries

[V] Existing tests use real local Git repositories, migrated SQLite, and fake runners (`internal/turn/fixture_test.go:74`). Relevant coverage includes base synchronization (`internal/turn/sync_test.go:139`, `internal/turn/sync_test.go:279`, `internal/turn/sync_test.go:317`, `internal/turn/sync_test.go:364`), dirty deferral (`internal/turn/deferred_dirty_test.go:86`, `internal/turn/deferred_dirty_test.go:178`), approved resolution (`internal/turn/apply_test.go:378`), clone coordination (`internal/turn/syncconcurrency_test.go:37`), persistence (`internal/turn/syncpersist_test.go:26`), and worktree behavior (`internal/git/worktree_test.go:105`, `internal/git/worktree_test.go:130`).

No task-upstream reconciliation path exists to exercise today. Future acceptance coverage should distinguish: existing worktree, existing local branch/new worktree, remote-only recovery, no remote task branch, equal/ahead-only/behind-only/diverged state, conflicts, dirty recovery, preparation-only merges, mode-specific behavior, reports/refusal durability, and an external push after the initial fetch. These are research coverage boundaries, not an implementation task breakdown.

[V] Standard checks are format, vet, pinned lint, and race-enabled tests through `make check` (`Makefile:22`, `Makefile:26`, `Makefile:57`). This run performed static research only: no code changed, tests executed, network/forge queries, or historical incident reproduction.

### Adjacent speculation

[V] The session record holds branch identity/path/state, not a branch-tip SHA; preparation reads HEAD from Git (`internal/store/sessions.go:28`, `internal/store/sessions.go:58`, `internal/turn/prepare.go:178`). The issue's suggested “session recorded branch tip” exposure is therefore not established by this structure.

[V] Issue materialization uses discovery data or `GetIssue` for unresolved candidates (`internal/daemon/tick.go:984`, `internal/daemon/tick.go:997`). Managed-story revisions are intentionally frozen (`.aiteam/APP_CONSTRAINTS.md:198`). These findings do not establish a general cached-issue-body defect. That subject requires a separate concrete freshness failure and should not be bundled into this branch-reconciliation change.

## Open Questions

Plan mode and the owner must settle these policy choices before implementation; this headless run requests no answer or approval.

1. Should clean behind-only state fast-forward? For both-ahead-and-behind state, should the factory merge automatically when clean, or refuse for operator reconciliation? How should rewritten/unrelated histories behave?
2. Is task freshness mandatory for normal execution, configurable like base conflict blocking, or deferrable when dirty recovery must remain available? What exact action should an operator take after each refusal?
3. Does Validate skip task mutation, perform only a freshness observation, or need some other explicit handling consistent with retained evidence? How should remote task changes supersede confirmed-resolution proposals without violating their approved SHA bindings?
4. How are task and base outcomes, compared refs/SHAs, ahead/behind counts, and prelaunch refusals retained and rendered? Does the current persistence model need extension?
5. When must a preparation-only merge be pushed if the runner makes no additional commit? How does that interact with the prohibition on saving failed-run changes?
6. Is all-task-branch fetch coverage a supported clone invariant, or must reconciliation verify/address restrictive refspecs? How should absent/deleted remote task branches be distinguished?
7. What should reports say when the remote changes after fetch and the final push is rejected? Does retry/recovery belong in this change, or is preserving local work with precise guidance sufficient?

## Recommended scope for Plan mode

Define task-branch reconciliation for existing local task state as a distinct preparation concern, after fetch/worktree availability and with explicit ordering relative to base synchronization. Cover divergence policy, safe mutation, durable/reportable outcomes, operator recovery, preparation-only save behavior, and the deliberate treatment of Validate and confirmed-resolution paths. Preserve existing remote-only and new-branch behavior, retained dirty work, non-force push protection, and approved evidence bindings. Use the existing Git/store/report ownership boundaries and local repository test fixtures.

Exclude migration-collision remediation, reconstruction of #170/#195 history, general issue-cache changes, acceptance-workflow redesign, and automatic mode transitions. Research confirms a code path defect; it does not approve a merge-versus-refusal policy or produce an implementation plan.

### Run assumptions and limitations

The supplied issue and comment are evidence of intent, not additional authority. The reasonable headless reading is to complete existing-code research in this worktree without asking questions, making changes to production code, contacting the forge, or waiting for approval. The supplied feature slug/session are authoritative. No prior-session artifacts were read. `.aiteam/rulebook.md` was absent; the tour used relevant project constraints and its standard architecture/testing review. Repository evidence was sufficient, so no external research was commissioned. Research ends with this brief; Plan was not started.
