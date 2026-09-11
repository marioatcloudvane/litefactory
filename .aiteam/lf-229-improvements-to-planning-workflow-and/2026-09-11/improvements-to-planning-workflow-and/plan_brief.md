# Plan Brief — Slimmer run-completion and story comments
# Branch: lf/229-improvements-to-planning-workflow-and
# Date: 2026-09-11
# Parent issue: github:marioatcloudvane/litefactory#229
# Citation base: bf5e8a7
# Research brief: RESEARCH_BRIEF.md (this session)

## Shared Decisions

- **CSD-1** — The live sub-issue creation path is `turn.PublishPlan` →
  `RenderCompleteStory`, for both content contracts (verified by tracing
  `internal/daemon/setup.go`'s wiring, which always satisfies the `PlanForge`
  interface gate at `turn/subissues.go`). The legacy `RenderSubIssue` renderer
  and its whole file-discovery phase are unreachable in production. S-3
  therefore touches only `RenderStoryProgress`; the legacy path is left alone
  and annotated, not changed or deleted.
- **CSD-2** — The partial-status mode gate compares `result.Run.Mode ==
  string(agent.ModeImplement)` — the durable, already-available per-run mode
  string — not `Result.ToolProfile` and not a new `Result.Mode` field.
- **CSD-3** — A managed story counts as "done" when `EngineeringComplete` is
  true, or its state is `verified` or `completed`. This union covers both the
  separated and legacy workflow versions; counting only `verified`/`completed`
  would misreport every successful separated-workflow Implement run as
  "0 of N complete".
- **CSD-4 / CSD-5** — The partial substitution applies only on the success arm,
  after the existing failure fold — a hard failure always wins over partial,
  structurally, with no new state. Nothing but the two integers (`Done`,
  `Total`) may ever enter the partial callout: no story key, title, issue
  number or parent ref.
- **CSD-6** — golangci-lint's `unused` check runs uncapped; any function a
  story's trimming orphans must be deleted in the same commit.
- **CSD-7** — Metering collection (`turn.meterRunSummary`) itself is untouched;
  only its comment-rendering call (`meterMetadata`) is removed. Dropping the
  collection call is a separate, out-of-scope question.
- **CSD-8** — S-4 and S-5 are hand-edits only, independent of each other and of
  S-1..S-3: no generator (`litefactory team add`/`team update`) may run in
  either story, and each story's scoped check reads only its own protected
  tree.
- **DEV-1 (resolved, standard enforced)** — The non-success default `Commit`
  arm survives S-1's removal: it is the only place a failed run with no commit
  states where the uncommitted work is (enforced by `STYLE-11` and
  `TestNonSuccessReportNamesTheWorktree`). All other `Commit` bullet arms are
  removed.
- **DEV-2 (resolved, standard enforced)** — `syncSection` (the "Base branch
  sync" disclosure) survives unchanged; only `taskSyncSection` ("Task branch
  sync") is removed. `syncSection` is the `CONC-16`-mandated deferred-dirty
  disclosure; `taskSyncSection` is the unconditional, SHA-laden bullet the
  owner's complaint actually targets.
- **DEV-3 (accepted, recorded)** — Removing the `Tools:` bullet removes the
  thread's only visible detection story for a label-driven tool-profile
  escalation (no `store.Run` column holds it, and the `Log:` bullet naming
  `litefactory logs` goes in the same change). Proceeding per the owner's
  explicit instruction; the consequence is recorded in S-1, not treated as a
  blocker.

## Workflow

- Shipping order: **S-1** first — it establishes the shrunk section ladder and
  heading construction every later story builds on. **S-2** depends on S-1
  (same `describeOutcome`/section-list code; sequencing keeps the diff
  readable). **S-3** is independent (different files —
  `story_progress.go`, comment-only edits in `subissues.go`) and may ship
  alongside S-1. **S-4** and **S-5** are independent of everything, including
  each other — disjoint protected trees, no shared file.
- Dependency graph: `S-2 → S-1`. No other story depends on anything in this
  plan.

## Cross-Story Constraints

- Platform: `internal/report` stays mode-blind — no `agent.Mode` or
  `store.*` type may enter it; new inputs are plain values only. No
  goroutines or channels are introduced anywhere in this plan. Every new store
  read takes `ctx` first. golangci-lint's `unused` check is uncapped —
  orphaned functions are deleted in the commit that orphans them.
- Security: nothing but the two-integer story tally may enter the partial
  callout — no forge-authored text. The hidden `StatusMarker`/`StoryMarker`/
  `StoryProgressNotice.Marker()` anti-spoofing mechanism and the
  `HasSuffix(marker)` post-render check are untouched by every story in this
  plan. Every rendered body continues to pass through
  `redact.Redactor.Apply` unchanged.
- Tests: `make check` passes with zero new lint findings across the plan.
  `internal/turn/report_test.go`, `subissues_test.go`,
  `plan_publication_test.go`, `deferred_dirty_test.go` and
  `syncrefusal_test.go` all stay green under the DEV-1/DEV-2 resolutions above;
  any assertion deliberately deleted (not merely regenerated) is named in its
  story's own notes.
- Ring-fenced: deleting the dormant legacy sub-issue renderer
  (`RenderSubIssue`/`fileStoryIssues`) is out of reach for this plan — S-3
  records the reachability finding only. `report/story_scope.go` is out of
  scope. `teams/**` (the distributable team catalog) is out of scope for S-4
  and S-5; neither may run a generator that would touch it.

## Story Index

| ID | Title | Sub-issue |
| --- | --- | --- |
| S-1 | Slim the run-completion comment to outcome, artifact link and result | [#247](https://github.com/marioatcloudvane/litefactory/issues/247) |
| S-2 | Label an Implement run that finished some but not all stories as partial | [#248](https://github.com/marioatcloudvane/litefactory/issues/248) |
| S-3 | Trim factory identifiers out of the story-progress comment; confirm the live sub-issue creation path already needs no change | [#249](https://github.com/marioatcloudvane/litefactory/issues/249) |
| S-4 | State the plain-language completion-message rules in the Claude orchestrator instructions (.claude/**) | [#250](https://github.com/marioatcloudvane/litefactory/issues/250) |
| S-5 | State the same plain-language completion-message rules in the Codex orchestrator skills (.codex/**) | [#251](https://github.com/marioatcloudvane/litefactory/issues/251) |

## Out of Scope

- Deleting the dormant legacy `RenderSubIssue` renderer / `fileStoryIssues`
  file-discovery phase — a coherent, self-contained cleanup, but not what the
  owner asked for.
- `report/story_scope.go`'s identifier-heavy comment — same class of trim as
  `RenderStoryProgress`, but not in the owner's scope table.
- `RenderReply` / command-reply comments (`reply.go`) — a different path, not
  part of the complaint.
- The redaction pass (`internal/redact`), metering collection/pricing and
  `litefactory meter`, and the marker mechanism itself — all untouched.
- `teams/**` (the distributable team catalog) — a separate release artifact.
- Any user-facing documentation of the comment format — none exists today and
  none is required.

## Provenance

- Citation base: `bf5e8a7` (branch HEAD at research/plan time).
- Known-stale areas: the typed Plan contribution was submitted and accepted as
  valid input (`InputState: valid`, no diagnostics), but its receipt
  (`request_id f7b37dd4b49190277867aa3af6ee19da`) still showed `PlanState:
  pending` — not yet a terminal publication state — as of this brief. The
  Story Index links above are placeholders; the factory fills them in once
  publication completes.
- Re-pinning requirements: Implement must use the factory-supplied approved
  bundle and issue links once publication finishes, not the placeholder table
  above.
