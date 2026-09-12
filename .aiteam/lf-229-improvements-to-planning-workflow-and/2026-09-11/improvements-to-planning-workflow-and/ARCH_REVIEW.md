# Architect: Go Architect
# Created: 2026-09-11
# Feature: Slimmer run-completion and story comments (issue #229)
# Verdict: APPROVE WITH CHANGES — two spec items must change before freeze (DEV-1, DEV-2)

Inputs read in full: `.aiteam/APP_CONTEXT.md`, `.aiteam/APP_CONSTRAINTS.md`,
`$session/feature-spec.md`, `.claude/skills/go/platform-constraints.md`,
`.claude/skills/shared/security-scanner.md`, `.claude/skills/shared/task-decomposer.md`,
`.claude/skills/shared/workflow.md` (§ Structured content and submission contract).
Source traced on this branch: `internal/report/{run,subissue,complete_story,typed_story,story_progress,meter}.go`,
`internal/turn/{report,subissues,plan_publication,harvest,execute,result,meter_report}.go`,
`internal/store/{managed_stories,engineering,story_progress,runs}.go`, `internal/daemon/setup.go`.

---

## 0. The open item the RE routed to me: D-6 / Q-02 — answered

**It is a false binary, and the live/dormant line is not where the spec drew it.**

The split is not "legacy contract 1 renderer vs typed contract 2 renderer". It is
**pre-managed file-discovery sub-issue phase (dormant) vs managed Plan publication
(live for *both* content contracts)**.

Traced call graph on this branch:

| Path | Entry | Renderer | Live? |
|---|---|---|---|
| Managed Plan publication | `turn/plan_handoff.go:232` → `turn.PublishPlan` (`plan_publication.go:52`) | `RenderCompleteStory` (`complete_story.go:11`) for **every** story, contract 1 and contract 2 alike | **Live.** The only production creation path |
| Typed body assembly | `PublishPlan` (`plan_publication.go:88`), `typed_plan.go:234/249`, `story_contract_upgrade.go:181` | `RenderTypedStory` (`typed_story.go:14`) builds the *body bytes*; `RenderCompleteStory` then wraps marker + redaction | **Live, but only for contract 2 generations.** It is not a second sub-issue renderer — it is the body producer whose output `RenderCompleteStory` publishes |
| Legacy file-discovery phase | `turn/harvest.go:181` → `fileStoryIssues` (`subissues.go:106`) → `subissues.go:412` | `RenderSubIssue` (`subissue.go:153`) | **Dormant in production.** |

Why `RenderSubIssue` is unreachable in a real daemon, deterministically:

- `fileStoryIssues` returns at `subissues.go:119` whenever `opts.SubIssueCreator`
  satisfies `turn.PlanForge` — *"The managed forge uses only the explicit Plan
  handoff. Legacy file discovery must never publish an old plan during another mode."*
- The only non-test construction of `turn.Options` is `internal/daemon/setup.go:278`,
  which passes `client` (`*github.Client`).
- `*github.Client` implements all seven `PlanForge` methods: `GetIssue`
  (`issues.go:217`), `GetParentIssue` (`managed_issues.go:14`), `ListIssues`
  (`issues.go:144`), `ListSubIssues` (`subissues.go:180`), `CreateIssue`
  (`issues.go:376`), `AddSubIssue` (`subissues.go:234`), `UpdateIssue`
  (`managed_issues.go:36`).
- Therefore the type assertion always succeeds in production and the phase always
  returns before it renders anything. `RenderSubIssue`'s only remaining callers are
  `report/subissue_test.go`, `turn/subissues_test.go` and `github/subissues_test.go`.

The contract-1/contract-2 distinction from `workflow.md` ("existing legacy
generations stay legacy until explicit owner CLI upgrade… on every cold, resumed or
fallback attempt use the supplied generation contract") is real and does keep legacy
generations alive — but a *legacy generation still publishes through `PublishPlan`
and `RenderCompleteStory`*. `PublishPlan` branches on
`GetGenerationContract(...).ContentVersion == 2` only to decide whether typed
content must be supplied and matched (`plan_publication.go:73-105`); both branches
converge on `RenderCompleteStory` at line 117. Legacy generations therefore do **not**
resurrect `RenderSubIssue`.

**Decision (binding, cross-story): S-3 touches exactly one renderer —
`RenderStoryProgress`.** `RenderCompleteStory` already emits marker + spec with no
`ReplyPlainlyNotice` and no metadata, so AC-15 is satisfied today by the live path
with no code change. `RenderSubIssue` is left exactly as it is (spec "Out of scope —
deleting dead code"), and S-3 records the reachability finding as a comment at
`turn/subissues.go`'s gate and on `RenderSubIssue` so the next reader does not
re-derive it (STYLE-1).

---

## 1. System Overview

### How this feature fits into the existing system

Nothing moves between packages. This is a change of *what two pure renderers emit*
and one new fact travelling from `internal/turn` into `internal/report`.

`internal/report` is the render-only leaf of the reporting pair: it holds no DB
handle, does no git and no forge work, and "takes none of these types wholesale"
(`report/run.go:51-59`). `internal/turn` owns delivery: `turn.Reporter.runInput`
(`turn/report.go:740`) is the single translation from a finished turn into a
`report.Run`, and `Reporter.ReportTurn` renders then posts. That direction is fixed
(`turn → report`, APP_CONTEXT package map) and this feature must not invert it: the
partial-status decision is *computed in turn and handed down as two integers*,
exactly as D-4 says.

The three comment surfaces in scope are independent code paths:

1. **Run-completion comment** — `report.Reporter.RenderRun` → `runBody`
   (`report/run.go:290`) → `fixedSection`/`metadataList` + the section ladder.
   Fed by `turn/report.go:runInput`.
2. **Sub-issue body at creation** — `report.Reporter.RenderCompleteStory`, fed by
   `turn.PublishPlan`. **Unchanged by this feature** (see §0).
3. **Story-progress comment** — `report.Reporter.RenderStoryProgress`
   (`report/story_progress.go:26`), fed by `turn.StoryProgress` effects.

### New packages / components introduced

None. Two new exported names inside `internal/report`:

- `report.StoryTally{Done, Total int}` — a plain value describing how many managed
  stories of the run's feature are finished. It carries no mode, no parent ref, no
  story keys and no text.
- `report.Run.Stories *StoryTally` — optional render input; `nil` renders nothing.

One new unexported helper inside `internal/turn`, beside `briefPath`
(`turn/report.go:817`), which is the same shape: best-effort, log-and-degrade,
returns a value rather than an error.

### Key boundaries

- **`report` stays mode-blind.** No `agent.Mode`, no `store.*` type and no parent ref
  may enter `internal/report` for this feature. The renderer sees two integers and
  decides presentation only. (D-4; also what keeps `report` a leaf.)
- **`turn` owns the gate and the counting.** The Implement-mode test, the store reads
  and the done-predicate all live in `internal/turn`.
- **`store` is read-only here.** `GetManagedFeature` and `ListManagedStories`
  (`store/managed_stories.go:55,117`) already exist with `ctx` first (CONC-5). No
  migration, no new accessor, no schema change.
- **The marker mechanism is untouched.** `StatusMarker`, `StoryMarker`,
  `StoryProgressNotice.Marker()` and the `HasSuffix(marker)` post-render check are
  load-bearing identity, not clutter (BR-7). Nothing in this feature may move a
  marker off byte zero or off the last line.

### Data flow

Run comment (after this change):

```
turn.Execute → Result{Run: store.Run, Harness, CommitSHA, Pushed, Publication, …}
  → turn.Reporter.runInput(ctx, result)
       ├─ result.Run.Mode == "implement" ?  ── no ─→ Stories = nil
       └─ yes → store.GetManagedFeature(ctx, result.Ref.String())
                store.ListManagedStories(ctx, parent, feature.Generation)
                count done → &report.StoryTally{Done, Total}      (any error → nil + log)
  → report.Reporter.RenderRun(ctx, report.Run{…, Stories})
       → describeOutcome → (failure fold) → (partial arm, success only)
       → fixedSection[marker, storySource, heading, callout?, ≤3 bullets, recovery?]
         + syncSection (conditional, see DEV-2) + resultSection
  → redact.Redactor.Apply → Poster.PostComment
```

Story-progress comment: unchanged in shape — `turn.StoryProgress` builds a
`StoryProgressNotice` from durable effect rows, `RenderStoryProgress` validates every
identity it is given and now prints a strict subset of them.

### Concurrency & context notes

- No goroutine is introduced and none may be (CONC-2). The tally is two synchronous
  queries on the reporting path.
- `ctx` is the caller's throughout; `runInput` already receives it. No
  `context.Background()` (STYLE-2 / forbidigo).
- **The tally read must not be able to fail the report.** CONC-4 requires render and
  post to happen before delivery is acknowledged; a decorative count that returns an
  error would turn a postable report into an unpostable one and strand the watermark.
  This is the one place the new code deliberately departs from its neighbour
  `meterRunSummary` (`turn/meter_report.go:12`), which *does* propagate.
- No SQL transaction spans I/O; two plain reads, no lock, no `LockStoryProgress`.

### Shared state and side effects

- Read-only against `managed_features` / `managed_stories`. The tally is a *snapshot
  at report time*; a concurrent Validate run may complete a story a second later. That
  is acceptable and must be worded as such ("N of M stories are complete"), never as a
  durable claim about the feature's final state.
- The removals delete four *classes* of thread-visible evidence: metering lines, the
  files list, the log-tail block and the `Log:`/`Tools:`/`Exit status:` bullets.
  Nothing in the repository parses any of them (AC-17 verified below), but §5 and §6
  record what the owner is agreeing to lose.

---

## 2. Cross-story decisions (binding)

**CSD-1 — The live sub-issue renderer is `RenderCompleteStory`; S-3 trims only
`RenderStoryProgress`.** Full reasoning in §0. `RenderSubIssue` and the whole
`fileStoryIssues` phase are dormant and stay untouched.

**CSD-2 — The mode gate uses `result.Run.Mode`, not `Result.ToolProfile` and not a
new `Result.Mode`.** `store.Run.Mode` (`store/runs.go:109-110`) is the durable,
per-run canonical mode string written at run creation from `Request.mode()`
(`turn/execute.go:438`), which returns `string(agent.ModeImplement)` = `"implement"`.
It is already in `runInput`'s hand as `result.Run.Mode` — no plumbing, no new field,
no `Request` reaching the reporter. It survives resume/continuation (CONC-21 keeps
mode on admitted retries) and is `""` for `DispositionLocked`/`DispositionPrepareFailed`,
which correctly yields no tally. Compare `result.Run.Mode == string(agent.ModeImplement)`;
do **not** string-literal `"implement"`. This supersedes assumption A-6 — the RE's
D-5 mechanism is right, and this is the cleaner existing signal it asked for.

**CSD-3 — "Done" means `EngineeringComplete || State ∈ {"verified","completed"}`,
for both workflow versions.** A-1 asked me to narrow this. Separated workflow (v2)
records Implement's own deliverable as `engineering_complete=1, state='frozen'`
(`store/story_progress.go:170`); `verified`/`completed` are Validate's to set
(`store/acceptance.go:180`, `story_progress.go:246`). Counting only
`verified`/`completed` would report every *successful* separated Implement run as
"0 of N complete" — the same false-partial bug D-5 exists to prevent, one workflow
version over. Legacy workflow (v1) moves stories to `verified` then `completed`
without ever setting `engineering_complete`, so both must be in the set. The union is
the exact inverse of `store.PendingEngineering`'s own predicate
(`store/engineering.go:13`: `state!='completed' AND engineering_complete=0`), which is
this codebase's existing notion of an unfinished story — cite it in the comment.
`state='blocked'` is not done.

**CSD-4 — Partial is applied only on the success arm, after the failure fold.** In
`describeOutcome` (`report/run.go:417`): compute `describeRunOutcome`, then if
`run.Failure != nil` return `view.withFailure(run)` unchanged; only on the remaining
path, and only when `view.succeed`, substitute the partial view. BR-3's "a hard
failure always wins" then holds structurally, with **no new state at all** — this
answers the RE's fourth confirmation question: yes, implementable with nothing beyond
D-5's tally. Keep `succeed: true` on the partial view so `resultSection`'s
empty-message wording (`run.go:1061`) does not change behaviour.

**CSD-5 — Nothing but two integers may enter the partial callout.** No story key, no
story title, no issue number, no parent ref. Forge-authored text in that callout would
(a) make AC-7's `fail`/`failed`/`error` ban unsatisfiable — a story titled "Fix error
handling" breaks it — and (b) put untrusted GitHub text into the one block the reader
treats as litefactory speaking (SEC-5's "report renderers MUST revalidate echoed
input"; SEC-20's untrusted-story-text rule). Two ints are structurally safe.

**CSD-6 — Delete what *this* change orphans; keep what was already dormant.** The
spec's "do not delete dead code" is scoped to the D-6 renderers. Unexported functions
this change strands (`filesSection`, `outputSection`, `outputOverhead`, `fitOutput`,
`elisionNotice`, `droppedOutputNotice`, `toolsEntry`, `notGranted`, `codeList`,
`taskSyncSection`, `meterMetadata`, and the `cuts` fields that go with them) **must**
be deleted: golangci-lint's `unused` is on with `default: standard` and
`max-issues-per-linter: 0`, and `make check` fails on any finding, so leaving them
with only test callers-turned-deleted breaks AC-16. Exported names on exported types
are never reported, so `report.MeterSummary`, `report.Sync`, `report.TailLines` and
every `report.Run` input field survive without lint cost.

**CSD-7 — Keep `Run.Meter` populated; delete only `meterMetadata`.** "Metering itself
is untouched" is a scope boundary, and `turn.meterRunSummary` is metering. Removing
its call would also require editing `turn/meter_test.go`, an enforcement-table test
(CONC-18–20). Delete the renderer (`report/meter.go:149`) and its rendering
assertions only. Accepted consequence: `report.MeterSummary.Add` keeps one production
caller with no visible output; a `STYLE-1` comment must say so, and the whole
question of whether the summary should still be prepared is a follow-up, not this
plan.

**CSD-8 — S-4 and S-5 are independent of each other, of S-1..S-3, and of any
generator.** Neither story may run `litefactory team add`/`team update` or any other
generator that writes both installed trees (workflow.md: "never run a generator that
writes both installed trees"). Each story's scoped check reads only its own tree's two
files. No Go test may assert the content of both trees in one assertion — that would
couple them and break scoped independence. `teams/source/agents/shared/*.md` stays out
of scope per A-4; §8 records the drift risk.

---

## 3. Deviations

### ⚠️ DEV-1 — MAJOR — Dropping the `Commit:` bullet outright deletes the only worktree disclosure a failed run has (STYLE-11)

**The conflict.** The spec's scope table says `- **Commit:**` is removed
unconditionally. `.aiteam/APP_CONSTRAINTS.md` **STYLE-11** says: *"A non-success run
report MUST name the uncommitted worktree in exactly one place: `metadataList`'s
Commit-line default arm (`internal/report/run.go`, when `CommitSHA==""`) or
`outcomeView.withFailure` (when `CommitSHA!=""`)."* For the common failure shape —
outcome failed/timed out/no result, `CommitSHA == ""` because CONC-8 forbids
committing a non-success run, `Failure == nil` because nothing went wrong *around* the
run — the default arm (`run.go:651`) is the *only* place the owner is told where the
work still is. `withFailure` appends `worktreeNote` only when `CommitSHA != ""`
(`run.go:459`), and `describeRunOutcome`'s own arms never name a path.

**The impact.** Removing it takes the count from 1 to 0 and fails the enforced test
`TestNonSuccessReportNamesTheWorktree` (`report/report_test.go:1421`, asserting
`strings.Count(body, testWorktreePath) == 1` for failed/timed-out/no-result).
Per TEST-10 that failure is a design change, not a test to edit green. Substantively:
a failed run would tell the owner nothing about where the uncommitted work is, on the
same feature whose D-7 already refuses to silently drop "your commit never reached the
remote".

**Resolution applied (standard stands).** S-1 keeps `metadataList`'s **default arm
only**, verbatim, and deletes the other three arms (`CommitSHA && Pushed`,
`CommitSHA`, `view.succeed`). AC-2 is unaffected — it is scoped to *succeeded* runs,
and a succeeded run never reaches the default arm. AC-3 does not name `Commit`. AC-6's
byte-for-byte promise is preserved because the surviving string is unchanged.

**Owner decision requested (one comment answers it):** confirm this reading, or state
that STYLE-11 should be retired — in which case the retirement must be recorded in
`.aiteam/AC_AMENDMENTS.md` and AC-6 reworded. Until answered, S-1 is planned as
resolved above and no engineer is blocked.

### ⚠️ DEV-2 — MAJOR — Dropping `syncSection` violates CONC-16, an enforced MUST

**The conflict.** D-1 (owner answer to Q-04) says both sync sections go.
`.aiteam/APP_CONSTRAINTS.md` **CONC-16** says: *"Durable outcomes, all prompt paths and
**owner reports** MUST disclose `deferred_dirty`, divergence and no sync SHA without
claiming a merge or prior success."* The owner report's disclosure *is*
`syncSection`'s `SyncDeferredDirty` arm (`report/run.go:951`). Removing the section
removes the disclosure, and fails
`turn/deferred_dirty_test.go:226 TestReporterDisclosesDeferredDirtyWithoutSuccessClaim`,
which asserts the *posted body* contains "deferred syncing", "preserve uncommitted
changes", "2 commit(s) behind, 0 ahead" and "The base was not merged". That test is in
the enforcement table. It also retires AC-3.2 of the earlier auto-sync feature ("a skip
is never silent"), asserted from `turn/syncrefusal_test.go:275`.

**The impact.** `deferred_dirty` means *litefactory did not merge the base because your
uncommitted work was in the way, and the run proceeded against a stale base*. Blocked
and failed sync mean *this run ran against a branch known not to be current*. These are
owner-actionable facts about correctness of the work, not debug output about the run.

**The asymmetry that resolves it.** The two sections are not the same kind of thing:

| | `syncSection` (`run.go:918`) | `taskSyncSection` (`run.go:1227`) |
|---|---|---|
| Renders when | `Sync != nil && Outcome != up_to_date` — i.e. only when sync did something abnormal, which is *not* the common run | `TaskSync != nil` — unconditionally, on nearly every run (`runInput` populates it for any task-phase observation that is not "absent") |
| Content | a prose sentence about what happened to the work | `origin/task: synced; 2 behind, 1 ahead. Compared local <sha> and remote <sha>.` — raw outcome token plus two SHAs |
| Constraint | CONC-16 (MUST, enforced) + prior AC-3.2 | none found; only `report/task_sync_test.go` asserts it |

The owner's complaint — debug metadata on every comment — lands squarely on
`taskSyncSection` and not on a section that is already silent on the ordinary path.

**Resolution applied (standard stands).** S-1 deletes `taskSyncSection` and the
`Run.TaskSync` rendering (and `report/task_sync_test.go` with it), and **keeps
`syncSection` exactly as it is**, including its `secSync` rung in the ladder. BR-8 is
amended to `secFixed + secSync + secResult`; AC-3's "no run comment contains … `Base
branch sync`" must be narrowed to `Task branch sync` only.

**Owner decision requested:** confirm keeping the base-sync disclosure, or amend
CONC-16 in `.aiteam/AC_AMENDMENTS.md` and accept that a deferred-dirty or
conflict-blocked sync becomes invisible on the thread. A middle option exists if the
owner wants it narrower still: keep only the `deferred_dirty`, `blocked` and `failed`
arms and drop `skipped`, which retires AC-3.2 but keeps CONC-16. I do not recommend it
— it is more code than keeping the section whole, for one fewer rare sentence.

### ℹ️ DEV-3 — NOTICE — Removing the `Tools:` bullet removes the thread's only detection story for label-driven tool escalation

Not a constraint violation; the owner named `Tools` for removal explicitly. But
`toolsEntry`'s own doc comment (`report/run.go:740-753`) is normative in tone and
says the opposite, with a security rationale: *"Labels are authenticated by nothing
(C3). Anyone with triage permission can widen an issue's profile and wait for the next
comment. The ceiling bounds what that can reach; this line is the only thing that makes
it visible afterwards, so it is the whole detection story for a relabel."*

I confirmed the residual: `store.Run` has **no** tools column, so after this change the
resolved tool surface survives only in the unredacted local JSONL under `$LF_HOME/logs`
— and the `Log:` bullet that told the owner the `litefactory logs <issue> --run <n>`
command is being removed in the same change. Net effect: a reader of the thread can no
longer see, or learn how to see, which tool profile a run actually ran under.

Proceeding as instructed. The engineer must **delete** `toolsEntry` along with its
rationale comment rather than leave a comment that contradicts the code (STYLE-1), and
S-1 must state this consequence in its own notes so it is not rediscovered as a bug.
If the owner wants it back cheaply later, the natural home is a `tools` column on
`runs` plus `litefactory logs`, not a comment bullet — a follow-up, not this plan.

---

## 4. Story split — confirmed, with three adjustments

| Story | Right-sized? | Finding |
|---|---|---|
| S-1 slim the run comment | ✅ keep separate | Purely subtractive plus two conditional predicates. Keeping it free of the new tally is what makes AC-6's "byte-for-byte apart from the removed sections" checkable at all. Largest diff of the five, almost all golden churn |
| S-2 partial status | ✅ keep separate, depends on S-1 | Genuinely additive: new type, new store reads, new gate. Correctly sequenced after S-1 — both touch `describeOutcome`/`fixedSection`, and merging them would make the golden diff unreadable |
| S-3 sub-issue + progress trim | ✅ keep separate, independent | **Shrinks** per CSD-1 to `RenderStoryProgress` + a recorded finding. Still a real story on files S-1 never opens (`story_progress.go`), so it may run alongside S-1 |
| S-4 `.claude/**` (Codex-owned) | ✅ | Independent; see CSD-8 |
| S-5 `.codex/**` (Claude-owned) | ✅ | Independent; see CSD-8 |

Not gold-plated, not under-split. Three adjustments:

1. **S-1 gains a criterion** (DEV-1): *a non-success run with no commit still names its
   worktree path exactly once.*
2. **AC-3 narrows** (DEV-2): `Task branch sync` only; `Base branch sync` survives.
3. **AC-15 is satisfied by finding, not by change** (CSD-1): the live creation path is
   `RenderCompleteStory`, which already carries no advisory sentence. S-3's evidence is
   the reachability trace in §0 plus the recorded comment, per the spec's own evidence
   inventory row for AC-15.

### S-4 / S-5 task roles — decided

Both are doc-only edits to instruction Markdown, and Plan's typed task schema requires
a roster role. **Use exactly one task per story, `owner_role: go-implementation-engineer`**
— it is the only roster role whose job is writing files. Do **not** manufacture a test
task: the evidence for AC-18/AC-19 is a scoped documentation review (the spec's own
evidence inventory says so), and a Go test asserting Markdown prose would either be
worthless or, written naively across both trees, would break the protected-tree
independence CSD-8 requires. `go-unit-test-engineer` and `go-integration-test-engineer`
have nothing to do in S-4/S-5 and should not be listed.

One placement note for whoever writes them: `implement-orchestrator.md` already has an
"escalation report carries five things" shape (`:330-340`) and `plan-orchestrator.md`
already requires the `/implement <instruction>` recommendation (`:133`, `:389`) and
returning unresolved questions (`:115`). BR-9 governs the **normal completion**
message; the escalation shape stays and wins when a run stops early. Say that
explicitly in both files, or the two shapes will be read as competing.

---

## 5. Per-task architectural hints

### S-1 — Slim the run-completion comment

#### T-1.1 — Reduce `fixedSection` and `metadataList` to the surviving bullets
**Agent:** go-implementation-engineer · **Type:** logic · **Est:** M · **Depends on:** —

- **Pattern to follow:** `metadataList`'s standing rule is already written at
  `run.go:622` — *"Every entry is omitted rather than guessed when it is not known."*
  This task applies it harder; it does not introduce a new idea.
- **Package placement:** `internal/report/run.go` only. Nothing in `internal/turn`
  changes in this task.
- **Key interfaces:** `metadataList(run Run, view outcomeView) string` keeps its
  signature — `view` is still needed for nothing? It is: the surviving Commit arm is
  the non-success arm, so `view.succeed` is still read. Do not drop the parameter.
- **What survives, exactly — at most three bullets:**
  1. the **non-success Commit default arm** verbatim (`run.go:651-652`) — DEV-1;
  2. **`Publication`** under the full predicate `run.CommitSHA != "" && !run.Pushed &&
     run.Publication != ""` (BR-4; the third conjunct stops an empty bullet, and the
     non-success path's own "Not published by this run…" string from
     `turn/harvest.go:363` correctly falls out because that path has `CommitSHA == ""`);
  3. **`Research brief`** as a bare labelled link (BR-5) — drop the *whole* trailing
     clause including "pinned to this run's commit", and update the doc comment at
     `run.go:659-666` that exists to justify the caveat (STYLE-1).
- **Also in `fixedSection`:** drop the `meterMetadata(run.Meter)` call (`run.go:588`)
  and the `ReplyPlainlyNotice` write (`run.go:590`); keep `StatusMarker`, `storySource`,
  the heading, the callout and `recoverySection` (A-2, A-3). Rewrite the function's doc
  comment — it currently names the notice and explains why it lives there.
- **Watch out for:** `ReplyPlainlyNotice` is still used by `reply.go:1001`; it is a
  const, not dead, and must not be deleted. `alertBlock` must remain unreachable for
  `OutcomeSucceeded` — `describeRunOutcome` already returns an empty `alert` there
  (`run.go:518`), so BR-1 needs **no code change**; confirm rather than implement it.
- **NOT in scope for this task:** the section ladder, the tally, any `internal/turn`
  file, goldens.

#### T-1.2 — Shrink the section ladder and delete what it orphans
**Agent:** go-implementation-engineer · **Type:** logic · **Est:** M · **Depends on:** T-1.1

- **Pattern to follow:** the ladder's ordering comment (`run.go:266-277`) is the
  contract — reading order *is* survival order. Preserve that property with fewer rungs.
- **Final shape:** `secFixed`, `secSync`, `secResult` (DEV-2), `numSections = 3`. Step 1
  (log tail) and step 3 (files) of the trimming ladder go; step 2 (result, cut from the
  end, with `resultCutNotice`/`droppedResultNotice`) stays, and so does the
  diagnostic-rebudget recursion at `run.go:351-362` — it is the guard that keeps the
  mandatory worktree line out of `Apply`'s hard truncate, which DEV-1 just made more
  important, not less.
- **Delete (CSD-6):** `taskSyncSection`, `filesSection`, `filesHeading`,
  `outputSection`, `outputSummary`, `outputOverhead`, `fitOutput`, `elisionNotice`,
  `droppedOutputNotice`, `toolsEntry`, `notGranted`, `codeList`, `meterMetadata`
  (`report/meter.go:149`), `maxFileEntries`, `minTailBytes`, and the `cuts` fields
  `output`, `outputDropped`, `filesDropped`.
- **Integration point:** `Reporter.RenderRun` logs the cut flags at
  `report/reporter.go:103-106`. Reduce those attrs to `result_truncated`. Nothing
  parses that log line; `slog` attrs only.
- **Keep:** `TailLines` (exported, and `internal/turn` captures output against it),
  `Run.Files`, `Run.Output`, `Run.TaskSync`, `Run.Meter`, `Run.ToolsRequested`,
  `Run.ToolProfile` as *input* fields. They are exported fields on an exported type, so
  `unused` never fires, and removing them would ripple into `internal/turn` and its
  tests for no gain (CSD-6/CSD-7). Add one comment saying they are retained inputs the
  run report no longer renders.
- **Watch out for:** `lastLines`, `fenceFor`, `elideMiddle`, `closeOpenFence`,
  `sanitize`, `truncateEnd` are shared with `reply.go`/`subissue.go` — check callers
  before deleting anything in `text.go`. `conflictsBlock` is used by `syncSection`,
  which survives.
- **NOT in scope:** deleting `RenderSubIssue`, `fileStoryIssues`, `github.CreateSubIssues`
  or any metering collection.

#### T-1.3 — Regenerate and prune the `internal/report` fixtures and tests
**Agent:** go-unit-test-engineer · **Type:** test · **Est:** M · **Depends on:** T-1.2

- **Pattern to follow:** `go test ./internal/report -update` rewrites
  `internal/report/testdata/*.golden` (`report_test.go`'s `update` flag). Regenerate,
  then **read every diff** — AC-16 says reviewed, not merely regenerated. A golden that
  silently lost its marker or its callout is exactly what this pass exists to catch.
- **Fixtures affected:** `run_*.golden` (6), `outcome_*.golden` (5) regenerate;
  `sync_*.golden` (5) keep their base-sync content and regenerate only where the
  surrounding body changed; delete `report/task_sync_test.go` with its section.
- **Enforced tests to change deliberately (TEST-10):** `report/report_test.go` is in the
  enforcement table for STYLE-11. `TestNonSuccessReportNamesTheWorktree`
  (`:1421`) must still pass **unchanged** for the three non-success outcomes — that is
  DEV-1's regression test and the reason the default arm survives. Its success half
  asserts `"this run committed nothing"` (`:1467`); that assertion is deliberately
  deleted with the success arm, and the deletion must be called out in the story notes.
  Also delete the two `ReplyPlainlyNotice` assertions on the run body (`:279`, `:305`)
  while keeping the sub-issue ones (`subissue_test.go`) and the marker-prefix guard
  (`:290`) intact.
- **New assertions:** AC-1 (no `> [!` in a succeeded body), AC-2 (none of the ten
  labels), AC-3 as narrowed by DEV-2, AC-4, AC-5 (marker still at byte zero and valid
  for ref+ordinal), AC-6, AC-11, AC-12, plus DEV-1's criterion.
- **Conventions:** `package report_test` (this package is black-box tested; TEST-4 does
  **not** list `report` among the in-package ones), table-driven, `t.Parallel()`
  (TEST-1), no new assertion helper (TEST-2).
- **NOT in scope:** turn-side tests, meter collection tests.

#### T-1.4 — Confirm the delivery path and the AC-16 gate
**Agent:** go-unit-test-engineer · **Type:** test · **Est:** S · **Depends on:** T-1.3

- I verified ahead of time that `internal/turn/report_test.go`,
  `subissues_test.go` and `plan_publication_test.go` assert on **none** of the removed
  strings, so AC-16's "no skipped or deleted assertion" in those three files is
  satisfiable as written. Confirm, do not weaken.
- `internal/turn/syncrefusal_test.go:275` and `deferred_dirty_test.go:226` assert the
  posted body's base-sync sentences — under DEV-2's resolution both keep passing
  unchanged. If the owner instead amends CONC-16, those two are the tests that move.
- Scoped check for this story: `go test ./internal/report/... ./internal/turn/...`, plus
  `make check` for the lint consequences of CSD-6.

#### T-1.5 — Record the AC-17 consumer search as evidence
**Agent:** go-implementation-engineer · **Type:** infra · **Est:** XS · **Depends on:** —

- I ran the search while reviewing: outside `internal/report` itself and its tests,
  nothing in the tree reads `Branch:`, `Exit status`, `Meter coverage`, `Files touched`
  or either sync heading out of a comment body. Reproduce it and record the command and
  output in the story's evidence so Validate can assess a retained check rather than
  re-run one (SEC-21, EXT-4). Scope the claim to this repository; A-5 is not claimed
  beyond it.

### S-2 — Partial story status

#### T-2.1 — `report.StoryTally` and `Run.Stories`
**Agent:** go-implementation-engineer · **Type:** data · **Est:** XS · **Depends on:** S-1

- **Package placement:** beside `Run` in `internal/report/run.go`. One concept, one
  file; do not start a `types.go`.
- **Shape:** `type StoryTally struct{ Done, Total int }`, `Run.Stories *StoryTally`,
  `nil` documented as "no tally applies". No mode, no ref, no keys (CSD-5).
- **Validation belongs to the renderer, as omission:** `Total <= 0`, `Done < 0` or
  `Done > Total` render nothing at all rather than a nonsense count. That is
  `metadataList`'s omit-rather-than-guess rule and CONC-3's "a bad outcome is a value";
  it is not an error return — `RenderRun` must not gain a new failure mode.

#### T-2.2 — The partial arm in `describeOutcome`
**Agent:** go-implementation-engineer · **Type:** logic · **Est:** S · **Depends on:** T-2.1

- **Structure (CSD-4):** in `describeOutcome` only. `run.Failure != nil` →
  `withFailure`, untouched. Otherwise, if `view.succeed` and the tally is present, valid
  and `Done < Total`, return `outcomeView{word: "partial", alert: "NOTE", lead: …,
  succeed: true}`. Do not touch `describeRunOutcome` — it describes how the *run* ended,
  and partial is a fact about the feature, not the process.
- **Wording:** two integers and nothing else, e.g. `"2 of 5 stories are complete; the
  rest are not done yet."` Check your own string for the substrings `fail`, `failed`,
  `error` before you commit it (AC-7); "the rest are not done yet" is safe, "no errors
  so far" is not.
- **Watch out for:** `succeed: true` matters — `resultSection` branches on it
  (`run.go:1061`) for an empty final message, and a partial run is still a success.
- **NOT in scope:** any knowledge of what a story is. `internal/report` must not import
  `internal/store` or `internal/agent`'s mode type for this.

#### T-2.3 — Compute the tally in `internal/turn`
**Agent:** go-implementation-engineer · **Type:** logic · **Est:** M · **Depends on:** T-2.1

- **Package placement:** `internal/turn/report.go`, immediately beside `briefPath`
  (`:817`) — same shape, same contract: best-effort, logs, returns a value, never an
  error. Suggested `func (r *Reporter) storyTally(ctx context.Context, result Result)
  *report.StoryTally`, called from `runInput` at the `report.Run` literal.
- **Gate (CSD-2):** return `nil` unless `result.Run.Mode == string(agent.ModeImplement)`.
  This single line is what makes the Plan edge case (AC-9) structurally impossible
  rather than incidentally absent.
- **Reads:** `r.db.GetManagedFeature(ctx, result.Ref.String())` then
  `r.db.ListManagedStories(ctx, feature.ParentTask, feature.Generation)`. `ErrNotFound`
  is the ordinary case for an unmanaged task — log at Debug, return `nil`. Any other
  error: log at Warn and return `nil`. **Never** propagate (CONC-4; and the spec's own
  "a reporting path must never fail to post because a count could not be taken").
- **Done predicate (CSD-3):** `s.EngineeringComplete || s.State == "verified" ||
  s.State == "completed"`. Put the reasoning — v2 vs v1, and the
  `store.PendingEngineering` inverse — in the comment (STYLE-1); it is the single
  least-obvious line in this story.
- **Watch out for:** `result.Run` is the zero `store.Run` for `DispositionLocked` and
  `DispositionPrepareFailed`, so `Mode == ""` and the gate already returns `nil` — do
  not add a second disposition check. `result.Ref` is the *parent* task ref, which is
  the key `managed_features.parent_task` uses; a story sub-issue comment is routed to
  the parent session before it ever becomes a run (`daemon/story_refinements.go`), so
  there is no child-ref case to handle here.
- **NOT in scope:** a new store accessor, a `Result.Mode` field, any change to
  `meterRunSummary`, any write.

#### T-2.4 — Renderer tests for the partial arm
**Agent:** go-unit-test-engineer · **Type:** test · **Est:** S · **Depends on:** T-2.2

- `package report_test`, table-driven over `RenderRun`, not over `describeOutcome` —
  the package is black-box tested and the callout is only observable through the body.
- Cases: `Done<Total` → heading `partial` + `> [!NOTE]` + both integers + no
  `fail`/`failed`/`error` anywhere in the callout block (AC-7); `Done==Total` → no
  callout (AC-8); `Stories==nil` → no callout (AC-8); non-success outcome with
  `Done<Total` → the outcome's own callout, not the partial one (AC-10); invalid tallies
  (`Total<=0`, `Done>Total`) → nothing rendered.
- Add or regenerate one golden for a partial body so the shape is reviewable as prose.

#### T-2.5 — Delivery tests for the gate and the counting
**Agent:** go-unit-test-engineer · **Type:** test · **Est:** M · **Depends on:** T-2.3

- Extend `internal/turn`'s existing fixture (TEST-3: real migrated SQLite, real git,
  fake runner) rather than building a new harness; no fake store (TEST-6).
- Cases: Implement run with 2 of 5 done → tally present; Plan run against the same
  managed feature with 0 done → `Stories == nil` (AC-9); no managed feature →
  `nil`; a store error → `nil` plus a log line and a body that still posts; workflow v2
  stories at `engineering_complete=1, state='frozen'` counted done; workflow v1 stories
  at `verified`/`completed` counted done; `blocked` not counted.
- TEST-8: no sleeps, no polling; these are synchronous reads.

### S-3 — Trim the story-progress comment

#### T-3.1 — Trim `RenderStoryProgress`'s visible body
**Agent:** go-implementation-engineer · **Type:** logic · **Est:** S · **Depends on:** —

- **Package placement:** `internal/report/story_progress.go` only.
- **Keep (BR-6):** the `Story %s — %s` line with the state sentence, the parent-issue
  link, the indented evidence block, and `Marker()` as the last bytes.
- **Drop from the format string (`:57`):** run number, generation, event/sequence,
  approved revision, and the whole `Validated implementation digest:` line — which,
  note, currently prints an empty backtick pair for `started`/`invalidated`/`blocked`,
  so this also removes a latent nonsense line.
- **Keep every validation (BR-6, AC-14):** the `:30` guard (parent, generation ≥ 1, run
  and effect IDs ≥ 1, `scopeKeyPattern`, `scopeDigestPattern` on `Revision`, 20,000-byte
  detail cap) and the per-kind `ImplementationDigest` check for `engineering_complete`
  and `verified`. The notice validates identities it no longer prints — say so in the
  doc comment, or a future reader will "simplify" the guard away (STYLE-1).
- **Watch out for:** the marker is built from `Parent`, `RunID` and `EffectID`
  (`:21-23`) and is what recovery and dedup match on (CONC-10). Those fields stay on the
  struct and stay in the marker even though the body no longer mentions them. The
  indentation defence at `:56` (`\n` → `\n    `) is what keeps arbitrary evidence from
  forging a column-zero marker — do not touch it. The `Truncated() ||
  !HasSuffix(marker)` post-check (`:62`) stays exactly as is.
- **NOT in scope:** `report/story_scope.go`, which posts a *different* comment carrying
  the same class of identifiers (generation, approved revision). It is not in the
  owner's scope table; §8 records it as a follow-up. Do not trim it here.

#### T-3.2 — Story-progress renderer tests
**Agent:** go-unit-test-engineer · **Type:** test · **Est:** S · **Depends on:** T-3.1

- Table over the five kinds: body contains the story key, the state sentence, the parent
  link and the evidence block; contains no run number, generation, sequence, revision
  hash or implementation digest (AC-13); still errors for
  `engineering_complete`/`verified` without a valid digest and still ends with
  `Marker()` (AC-14); the 20,000-byte cap and its over-limit error are unchanged.
- Assert the *absence* of a revision hash by using a distinctive fixture digest and
  checking `!strings.Contains` — not by regex over the whole body.

#### T-3.3 — Record the D-6 reachability finding in the source
**Agent:** go-implementation-engineer · **Type:** logic · **Est:** XS · **Depends on:** —

- Add a short comment at `turn/subissues.go:117-121` (the `PlanForge` gate) and at
  `report/subissue.go:142` stating what §0 establishes: the only production
  `SubIssueCreator` is `*github.Client`, which satisfies `PlanForge`, so this phase and
  `RenderSubIssue` are unreachable outside tests; the live creation path is
  `turn.PublishPlan` → `RenderCompleteStory` for **both** content contracts. Cite
  `daemon/setup.go:278`.
- **Change no behaviour.** This is the durable form of AC-15's evidence and it costs two
  comments; deleting the dormant phase is explicitly out of scope and is a bigger,
  owner-decided question (§8).

### S-4 — BR-9 in the Claude instruction set (Codex-owned, `.claude/**`)

#### T-4.1 — Write the completion-message rules into both orchestrator agents
**Agent:** go-implementation-engineer · **Type:** infra · **Est:** S · **Depends on:** —

- **Files:** `.claude/agents/plan-orchestrator.md`, `.claude/agents/implement-orchestrator.md`.
  Nothing else in the tree, and nothing in `.codex/**`.
- **Content:** BR-9 verbatim for the mode that owns the file — the shared rule plus that
  mode's three ordered rules (AC-18).
- **Placement:** `plan-orchestrator.md` already carries the `/implement <instruction>`
  requirement (`:133`, `:389`) and the "return the questions in the final report" rule
  (`:115`); BR-9's Plan rule 3 must be written as the *same* requirement, not a second,
  competing one. `implement-orchestrator.md` already defines a five-item **escalation**
  report (`:330-340`); state plainly that BR-9 governs the normal completion message and
  that the escalation shape wins when the run stops early.
- **Watch out for:** do not run `litefactory team add`/`team update` or any renderer —
  they write installed trees and would cross the ownership line (CSD-8). Hand-edit.
  `teams/source/agents/shared/*.md` is out of scope (A-4).
- **Scoped check:** the four rules are present in these two files. It must not read
  `.codex/**` and must not require the sibling tree to be installed.

### S-5 — BR-9 in the Codex instruction set (Claude-owned, `.codex/**`)

#### T-5.1 — Write the same rules into both orchestrator skills
**Agent:** go-implementation-engineer · **Type:** infra · **Est:** S · **Depends on:** —

- **Files:** `.codex/skills/plan-orchestrator/SKILL.md`,
  `.codex/skills/implement-orchestrator/SKILL.md`. Nothing in `.claude/**`.
- **Content:** equivalent wording to S-4's (AC-19). These files open with a "Codex copy
  of `.claude/agents/<name>.md`" banner listing the deliberate differences; BR-9 is not
  one of those differences, so the rules should read the same, in the same place in the
  document, adjusted only for the banner's existing voice.
- **Same guardrails as T-4.1:** hand-edit, no generator, scoped check reads only
  `.codex/**`. S-5 must not wait for S-4 and must not assert S-4's text.

---

## 6. Platform constraints (`.claude/skills/go/platform-constraints.md`)

- **Project layout** — applies, satisfied: every change lands in existing `internal/`
  packages; no new top-level package, no `pkg/` promotion, no new `cmd/`.
- **Error handling** — applies: the new store reads must be wrapped with `%w` when
  logged, and `ErrNotFound` distinguished with `errors.Is(err, store.ErrNotFound)`,
  never by string. The tally path returns a *value* on failure, not an error (CONC-3),
  which is the deliberate exception and must be commented.
- **Concurrency** — applies, negatively: no goroutine, no channel, no shared mutable
  state is introduced, and none may be (CONC-2's budget is fixed). Both new reads take
  `ctx` first (CONC-5).
- **Interfaces** — applies: **do not** introduce a `StoryCounter` interface for the
  tally. `turn.Reporter` already holds `*store.DB` concretely and the turn tests use a
  real migrated SQLite (TEST-3/TEST-6), so a seam here would be premature abstraction
  and would contradict TEST-5's consumption-point rule.
- **Dependency management** — not applicable (no new module; `go.mod`'s four direct
  requires are untouched, TEST-2).
- **HTTP/API layer** — not applicable (no listener, no route).
- **Database access** — applies: existing parameterised `QueryContext` accessors are
  reused verbatim; no new SQL, no migration, no schema change.
- **Testing conventions** — applies: table-driven, `t.Parallel()`, stdlib `testing`,
  hand-written fakes only; `report` tests are `package report_test`, `turn` tests are
  in-package (TEST-4). `make check` runs race tests.
- **Logging & observability** — applies: the tally's degradation must log through the
  injected `*slog.Logger` (`r.log`), Debug for "not a managed feature", Warn for a real
  store error. No `fmt.Print*`, no `log.*` (STYLE-2, forbidigo).
- **Linting & formatting** — applies, and is the sharpest edge in S-1: `unused` is on
  with findings uncapped, so every function this change strands must be deleted in the
  same commit (CSD-6) or `make check` fails.

---

## 7. Security findings (forward-looking: risks this *plan* introduces)

**Scope:** the planned change only — the two renderers, the new tally input, and the
two instruction-file edits. Not an audit of existing code.
**Rulebook:** used — `.aiteam/APP_CONSTRAINTS.md` §1 (`SEC`).

| # | Area | Status | Finding |
|---|---|---|---|
| 1 | Input validation | ✅ | The one new input is two `int`s produced by counting rows litefactory wrote itself. No forge text, no path, no identifier enters a renderer that did not already receive it |
| 2 | Injection into rendered markup | ✅ | CSD-5 forbids story keys/titles in the partial callout. Everything still rendered (`resultSection`, `syncSection`, the evidence block) keeps its existing `sanitize`/`closeOpenFence`/fence-sizing defences untouched |
| 3 | Marker integrity (anti-spoofing) | ✅ | `StatusMarker`, `StoryMarker` and `StoryProgressNotice.Marker()` are explicitly retained (BR-7), as is the `HasSuffix(marker)` post-render check and `story_progress.go:56`'s indentation of arbitrary evidence. AC-5/AC-14 pin both |
| 4 | Redaction boundary (SEC-6) | ✅ | Every body still leaves through `r.apply` → `redact.Redactor.Apply`. Removing sections shrinks what passes through it; nothing bypasses it, and no local `$LF_HOME/logs` copy becomes redacted |
| 5 | Secrets & configuration | ✅ | No config key, no token, no keychain path involved. Removing the `Tools:` bullet removes operator-authored profile *names* from the thread, which is a reduction in disclosure, not an increase |
| 6 | Auditability of tool-profile escalation | ⚠️ | **DEV-3.** The `Tools:` bullet is the only thread-visible evidence of which (label-selected, unauthenticated) tool profile a run used; `store.Run` has no tools column, and the `Log:` bullet naming `litefactory logs` goes in the same change. After this, detecting a triage-permission relabel requires shell access to `$LF_HOME/logs`. Owner-directed; recorded, not blocked |
| 7 | Loss of a correctness disclosure | ⚠️ | **DEV-2.** Dropping `syncSection` would remove the CONC-16-mandated `deferred_dirty` disclosure — "your uncommitted work blocked the base merge and this run proceeded against a stale base". Resolved by keeping the section; listed here because a blanket reading of the spec reintroduces it |
| 8 | Loss of a recovery disclosure | ⚠️ | **DEV-1.** Dropping the `Commit:` default arm would leave a failed run with no statement of where its uncommitted work is (STYLE-11). Resolved by keeping that one arm |
| 9 | Error handling / leakage | ✅ | The tally's failure path logs locally and renders nothing; no store error text reaches the comment. The failed-run `Diagnostic` keeps its existing literal, bounded, `codeSpan`-quoted treatment |
| 10 | Dependencies & supply chain | ✅ | No new dependency; `go.mod`/`go.sum` untouched |
| 11 | Instruction-file edits (S-4/S-5) | ✅ | Prose only, in trees each story owns exclusively; no generator runs, so neither story can write the other's tree (CSD-8) |

**Summary:** the change lowers the amount of internal data leaving the process and
introduces no new untrusted input, so its net security posture is an improvement. The
three ⚠️ items are all *information the owner stops seeing*, not new attack surface;
two of them (7, 8) collide with enforced constraints and are resolved in §3, and the
third (6) is an explicit owner instruction whose consequence is now recorded rather
than discovered later.

---

## 8. Risks and follow-ups explicitly **not** in this plan

1. **`report/story_scope.go`** posts a story-scope-lock comment carrying the same
   identifier class BR-6 trims from progress notices (generation, approved revision
   digest). Not in the owner's scope table; leaving it creates a visible inconsistency
   between two comments on the same sub-issue. Candidate follow-up.
2. **The dormant legacy sub-issue phase** (`turn/subissues.go` ≈ 420 lines,
   `report.RenderSubIssue`, and their share of `github.CreateSubIssues`) is
   production-unreachable (§0) and is now the only caller-less path of its size in the
   tree. Deleting it is a coherent, self-contained cleanup — and explicitly out of scope
   here. S-3's recorded comment is what keeps the finding alive for that decision.
3. **`turn.meterRunSummary` keeps running for a summary nothing renders** (CSD-7). It is
   a paged snapshot query per reported run, and it can still fail the whole report. Both
   facts predate this change; whether the preparation should be dropped, or the failure
   made non-fatal, is a follow-up.
4. **`teams/source/agents/shared/{plan,implement}-orchestrator.md` will not carry
   BR-9** (A-4). If this repo ever installs its own catalog with `team update`, the S-4
   and S-5 edits are overwritten. There is no `.litefactory/` lockfile in this worktree
   today, so the risk is latent rather than active — but it is the reason CSD-8 forbids
   running a generator inside either story.
5. **Conformance to BR-9 cannot be proven in-run.** The spec's evidence inventory is
   right: AC-18/AC-19 claim only that the instructions *state* the rules. Do not let a
   later story or a reviewer promote those into a claim that a Plan or Implement agent
   *followed* them (EXT-4, `docs/decisions/nested-agent-evidence.md`).
