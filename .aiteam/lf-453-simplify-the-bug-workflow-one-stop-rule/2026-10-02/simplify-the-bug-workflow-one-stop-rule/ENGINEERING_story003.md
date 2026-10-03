# Story 003 engineering handoff

Frozen scope: story-003 / #459 / issue ID 5682568242 / revision `37976faa2c76939e7ee08711a2123b55569e71480b0cc5c9f860acad8eaa6545`; dependency story-002 engineering recorded. No acceptance, architecture or plan assessment was performed, and no forge effect, checkpoint submission or final application result was sent by this worker.

## Delivered source seams

- `git.Worktree.ObserveSource(ctx, SourceOptions)` identifies complete dirty/saved deliverable bytes, deletions, new files, executable modes and explicitly identified ignored inputs without staging or changing the index. HEAD is observed separately from content identity. Double complete observations detect drift; bounds and unsupported source types fail closed. Factory transport is excluded.
- `git.ObserveEvidenceLog(ctx, canonicalRoot, relativePath)` checks stable regular contained bytes with component-symlink and opened-root identity checks. Relevant external source inputs unsupported by this contract refuse certification.
- `git.Worktree.CheckpointSource` reconciles retained original HEAD/content/options and exact operation message before retry. It reuses a completed local checkpoint after lost acknowledgment, refuses unsupported partial index or staged/tracked factory transport before mutation, and never pushes or grants validation/delivery authority.
- `PrepareGraphBugSource` checks the exact active Bug run/allocation, takes one short clone gate, retains checkpoint intent before mutation, reconciles pending preservation after restart, runs existing `syncTask` then `syncBase`, and retains source/effective-policy/task/base facts in shared graph outputs. Dirty checkpoint content binds all preserved deliverable artifacts, including declared validation non-inputs. Base disabled/up-to-date/distance decisions retain existing policy. Conflicts and unsafe work return their cause before dispatch.
- `bugrepair.Validation` requires exact factory commands, explicit observed zero exit codes, passing outcomes and complete approved conditions with stable log references on one source/main/relevant-input generation. Missing/null exit observation, missing/unknown/duplicate/failed/skipped obligations refuse. `ValidationInputsDigest` binds the required command contract to a sorted immutable relevant tool/configuration/input manifest.
- `GraphBugFixerActivity` checks current immutable approval, prepares source once before binding initial source/main, retains complete approved scope/guidance on warm/cold paths, uses existing fixer rights and shared results, and independently checks source/main/input/log freshness during shared preflight/submission and after successful execution. Stale final observations return blocked at the fixer origin. Only successful accepted evidenced work writes `repair_eligibility` to shared graph history.
- `CurrentGraphBugRepairEligibility` requires the latest source-node attempt, succeeded graph execution, successful settled factory run, current approval, matching accepted output identity/revision and coherent complete evidence. It returns immutable data; finalization independently reobserves freshness before saving/publication.

No new migration or Bug-specific ledger was required. The existing shared outputs express checkpoint, synchronization and repair-eligibility history. The narrow owner-authorized CONC-8/CONC-16 checkpoint amendment is recorded in `.aiteam/AC_AMENDMENTS.md`. Legacy authored result contracts remain unchanged and reject the new validation authority.

## Host composition required from story 004

This story deliberately does not activate a legacy runtime or implement remote delivery. The replacement host must provide real callbacks before admitting the complete new package.

For each allocated analyst/fixer invocation and owner continuation: fetch refs, freeze the slug and ensure the task worktree under normal task/clone ownership, release that clone gate, then call `PrepareGraphBugSource`. Do not call ordinary `prepareClone` or dirty-refusing `syncTask` before preservation. The helper performs checkpoint/reconciliation -> existing task synchronization -> existing base synchronization. It does not fetch, select a route, invoke an agent or run a check recipe. Prompt paths must include retained source/policy facts, including disclosed disabled synchronization.

`GraphBugFixerActivity` callback contract:

- `PrepareSource(ctx, opts, activity) error`: finish the host sequence above once before source-bound result context is built.
- `Observe(ctx, opts, activity) (GraphBugRepairInputs, error)`: read-only canonical actual source, configured base revision, applicable command obligations and complete relevant input manifest. Compute `InputsDigest` with `bugrepair.ValidationInputsDigest`. Preserve the identical contract for submission, postexecution and finalization observation.
- `Prepare(ctx, opts, activity, scope, retained)`: build native warm/cold requests and result service from the settled prepared host context without further source mutation. Keep the exact scope and complete framed retained context.
- `LogRoot(activity)`: a canonical absolute stable evidence directory, with agent-authored symlink paths refused.

Settle `store.FinishRun` separately from shared input acceptance before any runless conversation or delivery consumes success. Finalization uses the existing DOT owner-released revalidation edge only after it has reconciled effects. Changed source/main/inputs require a new fixer generation; unchanged diagnosis approval remains separate. No automatic fixer retry or factory recipe was added.

## Executed engineering checks

Passing command:

`go test -race ./internal/git ./internal/bugrepair ./internal/agentresult ./internal/turn ./internal/teamsource -run 'TestSourceIdentity|TestSourceCheckpoint|TestValidationGeneration|TestValidationInputs|TestFixerResult|TestGraphFixerAcceptedSuccess|TestGraphSourcePreparation|TestCriticalSectionsAreSingleAcquisitions|TestBugFixerMarkdown|TestCaptureBugFixer'`

Latest complete output at that snapshot:

```
ok github.com/cloudvane/litefactory/internal/git (cached)
ok github.com/cloudvane/litefactory/internal/bugrepair (cached)
ok github.com/cloudvane/litefactory/internal/agentresult 1.253s
ok github.com/cloudvane/litefactory/internal/turn 13.760s
ok github.com/cloudvane/litefactory/internal/teamsource 1.534s
```

This final rerun includes legacy validation-authority refusal and retained continuation stop explanations. Fixer context includes original immutable question origin/cause/reason beside each consumed authenticated reply; stale reasons are retained without duplicating historical diagnosis bodies. Final formatting check over every owned Go file reports no output. Scoped `go vet` over the six affected packages reports no output.

Engineering scenarios use real temporary repositories and SQLite, including reopening persistent storage after checkpoint commit but before acknowledgment. Controlled agent/provider cases establish offline orchestration and result/freshness boundaries; they make no claim about an authentic model run or live forge delivery. Full repository `make check` and full race suite belong to the orchestrator's aggregate gate.

Initial test failures were corrected before this handoff: temporary worktree fixtures had initially referenced stale origin baseline; failed-process fixtures initially expected nil error instead of its retained failure cause; stable evidence roots initially used an OS symlink alias instead of canonical paths. None remains outstanding.
