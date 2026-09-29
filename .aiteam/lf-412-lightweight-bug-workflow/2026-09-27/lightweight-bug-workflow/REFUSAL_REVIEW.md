# REFUSAL_REVIEW.md — story-002 (412-002: Evidence instead of hard stops)

## The test (d8)

For every place the bug path raises an error or a blocked outcome, one
question: **would removing this refusal let something merge, close, or spend
repair-attempt budget without one of the two owner approvals (analysis or
delivery) already covering it?**

- **Fails the test (nothing unapproved could actually happen without it)** →
  **policy**. The step continues on its normal success edge and records what
  it noticed as a fact (`bug_events` row, kind `fact`) for the owner to see at
  review.
- **Passes the test, and the site rejects malformed input, a changed replay, a
  crossed identity, a path escaping the project, a second worktree owner, or a
  repeat of an uncertain remote action (PR create/merge/close)** → **integrity**.
  Stays a hard error, unchanged.
- **Passes the test for any other reason** (it binds one of the two
  approvals, the three-try repair budget, cancel-blocks-dispatch, or the
  factory's own final check) → **guard**. Stays a hard error, unchanged.

Scope reviewed: `internal/store` bug files (`bug_tasks.go`, `bug_analyses.go`,
`bug_repairs.go`, `bug_checks.go`, `bug_delivery.go`, `bug_events.go`, the new
`bug_facts.go`), `internal/turn` bug files (`bug_*.go`), `internal/bugchecks`,
`internal/buganalysis`, `internal/bugrepair`, `internal/config` bug check
config (`bug_checks.go`, `bug_isolation.go`), and `internal/daemon` bug
permissions/dispatch (`bug_permissions.go`, `bug_analysis.go`,
`bug_test_dispatch.go`). All on the code as it stands after story-001 (the
six-table `bug_tasks`/`bug_analyses`/`bug_repairs`/`bug_checks`/`bug_delivery`/
`bug_events` store).

## Methodology and how to read this table

The bug path raises roughly 445 errors or blocked outcomes across the
reviewed packages. The overwhelming majority are one of a small number of
recurring *shapes* — a stale-revision guard, a replayed/mismatched identity, a
missing required field, a PR-provenance check, a budget/reservation guard —
each shape repeated once per call site that needs it. Listing all ~445
individually would not make the review more checkable; it would bury the
handful of sites that actually change classification under hundreds of
mechanically identical rows. So below:

- Every site whose classification is **policy** (i.e. changed by this story)
  is listed individually, with its exact file, message/condition and the
  one-line reason, because these are the only sites where behavior changes.
- Every site whose classification is **guard** or **integrity** is listed by
  **shape** (the recurring pattern), with the file(s) and representative
  message(s), a count of how many call sites carry that shape, and the reason
  the whole shape is guard/integrity. Every guard/integrity site in the
  reviewed packages falls into one of the shapes below — none were found that
  need an individual carve-out beyond what is already itemized as policy.

## Summary

| Class | Sites |
|---|---|
| Policy (changed by this story) | 5 distinct conditions (2 in `bugrepair`/`buganalysis` validators, 1 already-fixed scope disposition now also surfaced as a fact, 2 five-named-pain-point confirmations already fixed pre-story) |
| Guard | ~230 sites, all in the recurring shapes below |
| Integrity | ~210 sites, all in the recurring shapes below |

(Counts are grep-based over `errors.New(`/`fmt.Errorf(`/`return Err*` in the
reviewed non-test files; a handful of these are plain `ErrNotFound`
propagation for an absent lookup, which is not a refusal at all — a lookup
miss is a data fact, not a decision stop — and is excluded from both totals.)

---

## 1. Policy sites (changed by this story)

### 1.1 `internal/bugrepair/evidence.go` — `Evidence.Validate`

| Site | Class | Reason |
|---|---|---|
| Missing reservation/analysis digest/source commit/tree/summary | Guard/Integrity (unchanged) | Malformed input to the binding the approved repair budget spend rests on. |
| Coverage criterion not in the approved set, or duplicate | Integrity (unchanged) | A fabricated or duplicated criterion would misrepresent what the owner approved. |
| Coverage change/test path escapes the project (`fs.ValidPath` failure, `\`, NUL, CR/LF) | Integrity (unchanged) | Path escaping the project — named integrity condition. |
| `done=true` with a criterion still showing a gap, or no changes/tests | Guard/Integrity (unchanged) | An internally self-contradictory "done" claim (claims completion while recording an unmet gap on the same structure); not one of the two named policy candidates. |
| `done=true` but not every approved criterion has a coverage entry | Guard/Integrity (unchanged) | Same reasoning: the "done" claim would be structurally incomplete. |
| **`done=false` and a criterion has no changed file, test reference, or explicit gap** | **Policy** (`bugrepair.coverage_gap:<criterion>`) | Removing this cannot let anything merge/close/spend budget without approval: the owner still reads the coverage table at delivery review and decides. Named candidate in the architecture. Now a `Notice`; recorded as a fact from `internal/store/bug_repairs.go` (`ApplyBugRepairStop`, `done=false` call). |

### 1.2 `internal/buganalysis/analysis.go` — `BugRepairBoundaries.Validate` / `BugAnalysis.Validate`

| Site | Class | Reason |
|---|---|---|
| Zero allowed paths | Guard/Integrity (unchanged) | A bounded remedy with no boundary at all is not a coherent minimum for review. |
| Allowed/excluded path escapes the project or is malformed | Integrity (unchanged) | Path escaping the project — named integrity condition. |
| **More than 256 allowed/excluded paths** | **Policy** (`buganalysis.boundaries_count`) | Named candidate. The owner still reads and approves the exact remedy at analysis review regardless of list length. |
| **A repeated allowed/excluded path** | **Policy** (`buganalysis.boundaries_duplicate:<path>`) | Same reasoning; a duplicate cannot itself authorize anything extra. |
| All other `BugAnalysis.Validate` faults (blank expected/actual/cause/remedy/environment, missing reproduction/evidence/exclusions/criteria, invalid observation status, blank list entries, invalid/duplicate criterion or question IDs, oversize) | Guard (unchanged) | These protect the *minimum content a review needs to mean anything* — the architecture names only the boundaries-count/duplicate rule as "beyond the minimum a review needs"; the rest is that minimum, and stays a hard stop so the analysis-approval decision is never made over emptiness. |

Both validators now return `(notices []Notice, err error)`. Call sites (all
inside an existing store transaction, so a fact and the step it lets continue
commit together — AC-004, AC-010):

- `internal/store/bug_analyses.go` (`ApplyBugAnalysisResult`, review-ready
  analysis writes): records analysis-stage facts via `recordBugFactTx`, stage
  `analysis`.
- `internal/store/bug_repairs.go` (`CompleteBugRepair`, `done=true`, and
  `ApplyBugRepairStop`, `done=false`): records repair-stage facts, stage
  `repair`.
- `internal/agentresult/bug.go` and `internal/agentresult/bug_repair.go` call
  the same validators for pure submission-consistency diagnostics before any
  store write exists (there is no session/transaction at that point to record
  a fact into, and this pass only judges whether the agent's own submission
  decodes). Mechanically updated to discard notices (`_, err := ...`); the
  authoritative recording stays at the store call sites above.
- `internal/turn/bug_repair_observer.go` (`BugRepairBoundedScope`) also calls
  `BugRepairBoundaries.Validate()` to derive a scope disposition from the
  *already-approved* analysis; its notices duplicate what was already
  recorded once when that analysis was approved, so they are discarded here
  too (mechanically, `_, err := ...`) rather than re-recorded on every repair.

---

## 2. The five named pain points

| Pain point | Where | Class | Status |
|---|---|---|---|
| File-list gate too strict | `internal/store/bug_repairs.go`, `RetainBugRepairScope`; `internal/turn/bug_repair_handoff.go` | **Policy** | Already fixed pre-story (#384, #409): an "outside" or "indeterminate" disposition is retained as evidence, never a hard stop. This story additionally records it as a `fact` (`store.repair_scope:<request>`, stage `repair`) the first time a non-`within` disposition is observed, so it now also appears in the "What the factory noticed" section, not only in the raw evidence blob. |
| Frozen criterion wording (can't fix a typo in an approved requirement) | `internal/buganalysis/analysis.go` | **Policy** | No hard-error site was found in the reviewed code that refuses a corrected criterion wording after approval — a new analysis revision is simply a new revision, and typo fixes are content, not identity, so `BugCriterion.Requirement`/`Measurement` text is never diffed against a prior revision as a block. Already covered: nothing here needed a change. |
| Stale shared-definition pinning | `internal/store/bug_repairs.go`, `CreateBugTestCall` | **Guard**, narrowly | Already fixed pre-story: the check binds the exact launch record's own `graph_definitions` row (`r.ChildDefinition`/`r.DefinitionDigest`), not a shared/reusable definition row that could drift after the check was created. Code comment at the site: "shared definition rows cannot prove it, so only the child's own identity is checked here." The guard stays hard because it binds to the *current* definition; a *stale* comparison would have been the policy case, and none remains. |
| Step left reserved after a refusal | `internal/store/bug_tasks.go` (`releaseBugWorkOwnerTx`), called from `StopBugRepair`, `CompleteBugRepair`, `ApplyBugRepairStop`, merge/close stop paths | N/A — not a refusal to classify | Already fixed pre-story: every stop/terminal path releases the `guard_owner` reservation in the same transaction that records the stop, so a refused step never leaves the worktree permanently claimed. Confirmed present at all four call sites. |
| Fixer blind to check failures | `internal/store/bug_checks.go` (`RecordBugFailure`/`BugFailureReasons`); `internal/turn/bug_fixer.go:137` | N/A — not a refusal to classify | Already fixed pre-story (story-001's failure-reason query): the fixer's prompt reads `db.BugFailureReasons` and includes `PriorFailures` so a new repair attempt can see why prior check attempts failed. Confirmed read site present. |

---

## 3. Guard/integrity shapes (unchanged), by area

### `internal/store/bug_tasks.go` (~56 sites)

| Shape | Count (approx.) | Class | Reason |
|---|---|---|---|
| Identity/definition pinning (`immutable Bug identity conflict`, `bug lifecycle requires distinct pinned parent and child definitions`, `bug lifecycle cannot replace pinned envelope`, `bug report source status or task mismatch`, `bug factory recovery dispatch binding mismatch`) | ~10 | Integrity | Crossed identity between a task/instance/attempt and the record being written. |
| Optimistic-concurrency guard (`ErrBugRevisionConflict`, `bug continuation replay conflict`) | ~4 | Integrity | A changed replay — the caller's view of `bug_tasks.revision` is stale. |
| Lifecycle completion prerequisites (`bug lifecycle requires confirmed issue closure`, `bug lifecycle still owes effects, cleanup or reports`, `cancelled Bug lifecycle cannot complete`, `completed Bug lifecycle cannot be rewritten`) | ~6 | Guard | Close only after confirmed remote closure; nothing unfinished can be marked complete. |
| Retry/restart eligibility (`only a Bug parent that failed in its analyst or fixer, or stopped on a blocked analysis, can be retried`, `bug lifecycle does not admit fresh work`, `active Bug operation must resume its existing envelope`, `bug parent still has active work`) | ~6 | Guard | Cancel/active-work blocks every fresh dispatch; retry is scoped to genuinely terminal, non-active states. |
| Owner/authentication mismatch on continuation (`bug continuation owner mismatch`, `bug continuation reply replay mismatch`) | ~3 | Guard/Integrity | Binds continuation to the exact authenticated owner reply. |
| Malformed input (`invalid Bug identity`, `invalid Bug work owner/kind`, `invalid Bug lifecycle disposition/reference`, `invalid Bug restart report projection`) | ~10 | Integrity | Structural validity of the row being written. |
| Everything else in this file (remaining ~17 sites) | 17 | Guard/Integrity | Same recurring shapes (binding mismatch, replay conflict, malformed input) applied to the parent-continuation and restart-report paths. |

### `internal/store/bug_analyses.go` (~80 sites)

| Shape | Count (approx.) | Class | Reason |
|---|---|---|---|
| Eight-pass budget (`ErrBugAnalysisExhausted`, `ErrBugAnalysisActive`) | 2 | Guard | The analyst's own bounded attempt budget, parallel to the three-try repair budget. |
| Revision/replay conflict (`ErrBugRevisionConflict`, `store: Bug application replay conflict`, `store: Bug review identity conflict`, `store: Bug application evidence conflict`) | ~8 | Integrity | Changed replay of an already-applied analysis result or review. |
| Graph/attempt/result binding mismatch (`store: Bug graph consistency evidence missing`, `store: Bug graph retained-result reference mismatch`, `store: Bug proposal response/outcome mismatch`, `store: Bug review source mismatch`) | ~15 | Integrity | Crossed identity between the retained agent result and the graph attempt/pass it claims to settle. |
| Review-readiness and approval binding (`store: Bug analysis is not review ready`, `store: Bug approval must exactly match active published analysis review`, `store: successful pass has failure reason`) | ~6 | Guard | Binds the analysis approval to the exact revision/digest the owner saw — one of the two named approvals. |
| Malformed input / required-field checks on proposals, questions, answers | ~30 | Integrity | Structural validity of a proposal/question/answer row. |
| Remaining sites (question/answer replay, revision-history checks, wait/notice bookkeeping) | ~19 | Guard/Integrity | Same recurring shapes. |

### `internal/store/bug_repairs.go` (~81 sites, now 85 after this story's fact-recording additions)

| Shape | Count (approx.) | Class | Reason |
|---|---|---|---|
| Three-try budget (`ErrBugRepairExhausted`, `count >= 3`) | 1 | Guard | The named repair-attempt budget. |
| Active-work/worktree-owner guard (`ErrBugWorkActive`, `guarded != 0`, `bug test worktree is occupied`, `nested Bug children forbidden`) | ~6 | Guard | One repair/check at a time per worktree; prevents concurrent, unreviewed writes. |
| Reshape/unauthorized-transition guard (`ErrBugRepairUnauthorized`, `store: initial Bug fix must be exactly the first reservation`, `store: terminal repair cannot change`) | ~5 | Guard | A reshape must follow the exact prior reshape state and a strictly newer analysis revision — protects the repair budget from being spent outside an owner-authorized reshape. |
| Revision/replay conflict (`ErrBugRevisionConflict`, `bug repair completion replay conflict`, `bug test call replay conflict`, `immutable repair scope observation conflict`) | ~12 | Integrity | Changed replay of an already-settled repair/test-call/scope row. |
| Graph/result/run binding mismatch (`store: repair graph did not confirm success`, `store: repair result source mismatch`, `store: repair retained result binding mismatch`, `bug child definition is not pinned to parent package`, `bug test parent binding mismatch`) | ~20 | Integrity | Crossed identity between the retained result, the run, and the graph attempt/definition it claims. |
| Evidence/scope validation delegated to `bugrepair.Evidence.Validate` and `RetainBugRepairScope`'s malformed-input check | 2 (+1 unknown-disposition) | Guard/Integrity/Policy | See §1.1 and §2 (file-list gate). |
| Malformed input (`invalid Bug repair request`, `invalid Bug test call`, `store: malformed Bug repair scope observation`) | ~15 | Integrity | Structural validity. |
| Remaining sites (test-generation sequencing, failure/resolution recording) | ~20 | Guard/Integrity | Same recurring shapes. |

### `internal/store/bug_checks.go` (~34 sites)

| Shape | Count (approx.) | Class | Reason |
|---|---|---|---|
| Recipe/binding pinning (`bug recipe digest mismatch`, phase/binding mismatches) | ~10 | Guard | Protects "the final check running only the pinned recipe" — explicitly named as a keep. |
| Unreliable-check propagation | ~2 | Guard | An unreliable prior check outcome must not silently pass; blocks delivery. |
| Replay/malformed input on failure records and phase rows | ~22 | Integrity | Same recurring shapes. |

### `internal/store/bug_delivery.go` (~83 sites, +2 after this story's digest/facts wiring)

| Shape | Count (approx.) | Class | Reason |
|---|---|---|---|
| Delivery-approval binding (`bug delivery evidence changed`, `bug review replay conflict`, `store: Bug delivery reply does not match published active review`, `bug delivery reply replay conflict`) | ~15 | Guard | Binds delivery approval to the exact review digest the owner saw — this story extends the digest with the fact list precisely so this guard keeps meaning what it says (AC-007). |
| PR provenance/head binding (`bug review PR provenance mismatch`, `bug delivery publication conflict`) | ~10 | Guard | Merge only with the approved review's exact PR/head, per the not-in-scope list. |
| Cancellation (`bug delivery cancelled`) | 1 | Guard | Cancel blocks every dispatch. |
| Unanswered clarification (`unanswered Bug clarification prevents approval`) | 1 | Guard | An unresolved owner question must not be silently approved past. |
| Malformed input / structural validity (`invalid Bug delivery review`, `invalid Bug PR intent`, `missing Bug review invalidation reason`) | ~25 | Integrity | Structural validity. |
| Remaining sites (merge/close reconciliation, replay/binding mismatches) | ~31 | Guard/Integrity | Same recurring shapes. |

### `internal/store/bug_events.go` / `bug_facts.go`

| Shape | Class | Reason |
|---|---|---|
| `store: invalid Bug event` (empty kind/reference), `store: Bug event content exceeds 1 MiB`, `store: Bug event replay conflict` | Integrity | Structural validity and changed-replay protection for the shared append-only log every domain write shares. |
| `store: invalid Bug fact session/stage/reference/text` (new, `bug_facts.go`) | Integrity | Structural validity of a fact row itself — a malformed fact is a programming error in a call site, not something an owner decision ever covers. |
| 32-fact cap / overflow marker | N/A (behavior, not a refusal) | Never returns an error for exceeding the cap; folds excess into one overflow fact instead (AC-008). |

### `internal/turn` bug files (~140 sites across 22 files)

Every site found is one of: incomplete/invalid factory bindings passed into
an `Activity.Execute` (integrity — malformed input from the caller, never
owner content), a graph/attempt/session binding mismatch (integrity — crossed
identity between the dispatched node and the domain row it targets), an
"already registered/already executing" replay guard (integrity), or a direct
pass-through of a `store` guard/integrity error already classified above. No
additional policy candidate was found beyond §1 and §2. Representative files
checked in full: `bug_checks.go`, `bug_merge.go`, `bug_close.go`,
`bug_analysis_policy.go`, `bug_pr.go`, `bug_repair_handoff.go`,
`bug_repair_observer.go`. `bug_pr.go`'s "Bug review publication pending" and
"Bug review retained; publication is pending" outcomes are explicitly a wait,
not a refusal — per the architecture, a "still waiting" outcome takes the
existing pending/park path, never a fact.

### `internal/bugchecks` (executor.go, recovery.go, identity_*.go)

| Shape | Class | Reason |
|---|---|---|
| Sandbox/PATH/writable-root/executable-identity checks (`check path escapes worktree`, `bug checks require the Codex sandbox`, `unsupported writable check root`, `check executable outside the operator PATH and worktree`, `check executable exceeds 128 MiB identity bound`) | Guard | Protects the factory's own final check from running unsandboxed or unpinned code — explicitly named as a keep. |
| Process identity/replay (`process changed during identity observation`, `missing verified check process start identity`) | Integrity | Crossed identity — classic PID-reuse-race protection, matches the named integrity condition exactly. |
| Output/timeout bounds (`check output exceeded 4 MiB`, `check timed out`, `check output capture incomplete`, `check exit status unavailable`) | Guard | An unreliable/incomplete check outcome must not silently pass as evidence for delivery. |

### `internal/config` bug check config (`bug_checks.go`, `bug_isolation.go`)

| Shape | Class | Reason |
|---|---|---|
| Recipe/argv/environment/timeout structural validation | Integrity | Structural validity of the operator-authored pinned recipe. |
| Operator sandbox config validation (`bug_checks.binary must be an absolute path`, writable/deny-read/path entries) | Guard | Protects the sandbox the final check runs in — same "final check integrity" keep as `internal/bugchecks`. |

### `internal/daemon` bug permissions/dispatch (`bug_permissions.go`, `bug_analysis.go`, `bug_test_dispatch.go`)

| Shape | Class | Reason |
|---|---|---|
| Role/tool/sandbox permission checks (`bugRolePermissions`: unknown role, missing profile, wrong sandbox, tool outside role surface or operator ceiling) | Guard | Operator least-privilege config; a misconfigured ceiling could let the read-only analyst or the fixer escape its intended sandbox/tool surface, bypassing the reviewed repair flow entirely. |
| Authenticated-owner binding for analysis approval (`Bug approval requires authenticated owner`, `Bug approval requires current published analysis review`) | Guard | Binds analysis approval to the exact numeric owner identity — one of the two named approvals. |
| `bug_test_dispatch.go` (executor unavailable, session-lock unavailable) | N/A — not a refusal | Both requeue the dispatch with backoff; neither is a hard stop, both are already the existing pending/retry path. |

---

## 4. Design notes for the test-writing phase

- **Fact redaction (AC-008).** `internal/store` never depends on
  `internal/redact` (it would need the resolved bot token, which only
  `internal/turn`/`internal/daemon` hold at process start). Facts are stored
  as plain text. Redaction happens where the rest of the review body already
  gets redacted: `internal/turn/bug_pr.go`'s existing
  `h.Redactor.Apply(ctx, rendered, 60000)` call for the delivery review, and
  the equivalent `h.reporter.renderer.RenderReportDelivery` path for the
  analysis review. Because facts are rendered into the body text before that
  call (via `report.RenderBugFactsSection`/`bugFactLines`), every fact's text
  passes through the same redactor as the rest of the review body without any
  new dependency.
- **Digest binding (AC-007).** `store.BugDeliveryReviewDigest` takes an
  explicit `facts []BugFact` parameter. Every place that recomputes "the
  digest that should currently be active" (`RetainBugDeliveryReview`,
  `bugDeliveryGuardTx`'s callers in `ApplyBugDeliveryReply` and
  `approvedBugDeliveryTx`, and `internal/turn/bug_pr.go`'s `prepareReview`/
  `applyReply`) fetches facts fresh via `db.BugFacts`/`bugFactsTx` rather than
  from a persisted struct field, so a fact recorded between two calls is
  never silently absorbed into an old digest. The analysis review does not
  have a comparable digest-recompute path independent of a new analysis
  revision; its `ContentDigest` is the hash of the fully rendered comment
  body (which now includes the facts section), so a later fact does change
  `ContentDigest`, but nothing today re-dispatches the *same* analysis review
  comment node purely because a fact was added — see the concern in the
  engineering report.
- **No new graph edges (AC-005).** All fact recording added by this story
  happens on the existing success edge inside a store method that already
  runs (`ApplyBugAnalysisResult`, `CompleteBugRepair`, `ApplyBugRepairStop`,
  `RetainBugRepairScope`); no new node, edge or dispatch was added to
  `bug.dot`.
