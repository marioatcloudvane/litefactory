# Feature Spec: Slimmer run-completion and story comments
# Status: APPROVED
# Date: 2026-09-11

## Goal

Strip internal run metadata out of litefactory's GitHub comments so an owner reading a
thread sees only the outcome, the artifact link and the mode's own result content — with
a clear failure banner, an explicitly non-failure "partial" label, and no banner at all
on success.

## Source of record

| Input | Use |
|---|---|
| `$session/RESEARCH_BRIEF.md` | Current behaviour, file/line map, test inventory |
| Owner answers Q-01..Q-04 (issue #229) | Settled; not re-opened here |
| Owner directive "keep it simple, no extra or unnecessary work" | Governs every scope call below |

## User Stories

| ID | Story |
|----|-------|
| US-1 | As a repo owner, I want the run-completion comment to carry only the artifact link and the run's result text, so the thread reads as a conversation and not as debug output |
| US-2 | As a repo owner, I want a failed run to be obvious at the top of its comment, so I do not have to read the body to learn it failed |
| US-3 | As a repo owner, I want a run that implemented some but not all stories labelled **partial**, in wording that is plainly not "failed", so I can tell "unfinished" from "broken" |
| US-4 | As a repo owner, I want a successful run to carry no status banner, so success is quiet and only exceptions draw my eye |
| US-5 | As a repo owner, I want per-story sub-issue comments (creation-time and progress-time) to carry the story's substance and not factory identifiers, so a story thread stays readable |
| US-6 | As a repo owner, I want Plan's and Implement's completion messages to follow a short, predictable shape, so every mode's comment answers the same few questions |

## Scope

### In scope — run-completion comment (`RenderRun` → `runBody`)

| Element | Today | Required |
|---|---|---|
| Hidden status marker (`StatusMarker`) | rendered | **keep, unchanged** — anti-spoofing, not clutter |
| Parent-plan / source links (`storySource`) | rendered when story-sourced | keep, unchanged |
| Heading `### litefactory · run N · <word>` | always | keep |
| Status callout (`alertBlock`) | on every non-success outcome | keep for failure/timeout/abandoned/no-result/interrupted/unknown; **none on success**; new NOTE-level callout for partial |
| `- **Branch:**` | rendered | **remove** |
| `- **Commit:**` | rendered | **remove** |
| `- **Publication:**` | rendered whenever non-empty | **conditional** — render only when work was not published (see BR-4) |
| `- **Research brief:**` | rendered, with a trailing "404s until a Research run writes one" caveat | keep the link; **drop the caveat clause** (BR-5) |
| `- **Exit status:**` / `- **Reason:**` | rendered | **remove** |
| `- **Duration:**` | rendered | **remove** |
| `- **Cost:**` / `- **Legacy reported cost:**` / `- **Model:**` | rendered | **remove** |
| `- **Tools:**` (`toolsEntry`) | rendered | **remove** |
| `- **Log:**` | rendered | **remove** |
| `- **Note:** (stream error)` | rendered | **remove** |
| Meter coverage lines (`meterMetadata`) | appended after `metadataList` | **remove** — second call site, `run.go:588` |
| `ReplyPlainlyNotice` advisory paragraph | rendered in `fixedSection` | **remove from the run comment** |
| Base branch sync (`syncSection`) | conditional section | **remove** |
| Task branch sync (`taskSyncSection`) | conditional section | **remove** (Q-04: owner said drop both) |
| `**Result**` (`resultSection`) | rendered | **keep, mechanism unchanged** — still mode-blind, still sanitised/fence-closed/truncated |
| `**Files touched (n)**` (`filesSection`) | rendered | **remove** |
| `<details>Run output</details>` (`outputSection`) | rendered | **remove** |
| `**Session-limit recovery**` (`recoverySection`) | conditional | keep, unchanged — owner-actionable, not metadata |

### In scope — partial status

- New render input on `report.Run`: an optional story tally (`Done`, `Total`), supplied by
  `internal/turn`, `nil` by default.
- `internal/turn` populates it **only** for Implement-mode runs against a managed feature;
  `internal/report` stays mode-blind and renders only what it is handed.
- Source data: existing `store.GetManagedFeature` + `store.ListManagedStories(parent, generation)`.
  No migration, no new store accessor, no new `agent.Outcome` value.

### In scope — sub-issue comments (confirm, do not redesign)

| Renderer | Required change |
|---|---|
| `RenderSubIssue` (legacy, `subissue.go:153`) | Drop `ReplyPlainlyNotice` from the body. No other change. **Touch only if the path is live** (see D-6) |
| `RenderCompleteStory` / `RenderTypedStory` | No change — already marker + spec, no metadata |
| `RenderStoryProgress` (`story_progress.go:26`) | In scope per Q-01: drop the factory identifiers from the visible body (BR-6) |

### Out of scope

- `RenderReply` / command-reply comments (`reply.go`) — a different path, not part of the complaint.
- The HTML-comment marker mechanism (`marker.go`, `StoryProgressNotice.Marker`) and the
  `HasSuffix(marker)` post-render check — safety features, explicitly retained.
- The S-7 redaction pass (`internal/redact`) and `resultSection`'s sanitise/fence-close/truncate behaviour.
- Metering itself: collection, storage, pricing and `litefactory meter` are untouched; only the
  comment lines go.
- Deleting dead code. If a renderer turns out to be unreachable (D-6), leave it alone.
- `teams/**` (the distributable team catalog) — a separate release artifact, not this repo's working agent set.
- New artifact links for Plan/Implement in Go. Plan's links come from Plan's own result text (BR-9).
- Any user-facing documentation of the comment format — none exists today and none is required.

## Data Model

No schema change. No migration. New in-memory render input only.

| Entity | Field | Type | Required | Constraints |
|---|---|---|---|---|
| `report.Run` | `Stories` | `*report.StoryTally` | no | `nil` means "no story tally applies"; renders nothing |
| `report.StoryTally` | `Done` | `int` | yes | `0 <= Done <= Total` |
| `report.StoryTally` | `Total` | `int` | yes | `> 0`; a tally with `Total <= 0` renders nothing |

### Relationships

- `store.ManagedFeature` (by `ParentTask`) → `store.ManagedStory[]` via `(parent_task, generation)`
- `store.ManagedStory[]` → `report.StoryTally` — counted in `internal/turn`, never in `internal/report`
- `report.StoryTally` → the heading word and callout of `RenderRun`

## Interface Changes

No CLI, HTTP or forge-API surface changes. No permission or access-control changes: the comment
is posted by the same bot identity, to the same issue, through the same `Poster.PostComment`.

| Package | Change |
|---|---|
| `internal/report` | `Run` gains `Stories *StoryTally`; `metadataList` reduced to at most two bullets; `meterMetadata`, `syncSection`, `taskSyncSection`, `filesSection`, `outputSection`, `toolsEntry` no longer called from `runBody` |
| `internal/turn` | `runInput` populates `Run.Stories` for Implement-mode runs; needs the resolved mode on `Result` (today only `Result.ToolProfile` carries it indirectly — architect's choice of handle) |
| `internal/store` | read-only use of existing `GetManagedFeature` / `ListManagedStories` |

## Business Rules

- **BR-1 — success is silent.** `agent.OutcomeSucceeded` with no `Run.Failure` and no partial
  tally renders heading word `succeeded` and **no** callout block.
- **BR-2 — failure is loud.** Every non-success outcome keeps its current heading word, alert
  level and lead sentence exactly as `describeRunOutcome` / `withFailure` produce them today.
  Removing metadata must not change any failure wording.
- **BR-3 — partial is not failure.** When the run's own outcome is `Succeeded`, `Run.Failure`
  is nil, and `Stories != nil && Stories.Total > 0 && Stories.Done < Stories.Total`:
  heading word is `partial`, callout level is `NOTE`, and the lead states the count
  (e.g. "2 of 5 stories are complete; the rest are not done yet."). The words "fail",
  "failed" and "error" must not appear in that lead. A non-success outcome always wins over
  partial — the failure banner is never replaced by a partial one.
- **BR-4 — unpublished work still gets said.** The `Publication` line renders only when the
  run produced a commit that did not reach the remote (`Run.CommitSHA != "" && !Run.Pushed`).
  Every success-shaped publication string is dropped.
- **BR-5 — the brief link renders bare.** `briefLink` already returns a link only when the
  brief exists, the run pushed, and `SHAAfter` is known, so the "404s until a Research run
  writes one" caveat is stale; the bullet becomes `- **Research brief:** [<path>](<url>)`.
- **BR-6 — a story-progress comment carries state and evidence, not identifiers.** The visible
  body keeps: the story key, the state sentence, the parent-issue link, and the indented
  evidence block. It drops: run number, generation number, event/sequence number, approved
  revision hash and validated implementation digest. The trailing `Marker()` and the
  `HasSuffix(marker)` check are unchanged, and `RenderStoryProgress` still **validates**
  `ImplementationDigest` for `engineering_complete` and `verified` even though it no longer
  prints it.
- **BR-7 — the marker is never removed.** Dropping `ReplyPlainlyNotice` removes the visible
  paragraph only. `StatusMarker`, `storyMarker` and `StoryProgressNotice.Marker()` stay, and
  every existing marker-based recovery/dedup path keeps working.
- **BR-8 — nothing that survives truncation may start depending on being cut.** `runBody`'s
  section list and trimming ladder shrink to `secFixed` + `secResult`; the budget check and
  `resultSection`'s truncation notice remain. The brief link stays inside the never-trimmed
  fixed section.
- **BR-9 — Plan and Implement completion messages follow a short shape.** Three rules each,
  plus one shared rule. This is the whole of the Q-03 answer; no four-hard-rules apparatus,
  no new question-inventory contract.

  Shared: the completion message is the run's **last** message, written in plain business
  language — no Go identifiers, no `path:line`, no pasted file contents.

  Plan, in order:
  1. Lead with links — the parent plan issue and each published story issue, one line.
  2. Say in plain language what the plan does and how many stories it has.
  3. Name what is unresolved — open questions or assumptions the owner should check; if there
     are none, say so. Then the existing required `/implement <instruction>` recommendation.

  Implement, in order:
  1. Lead with the story links and, for each, whether it is done.
  2. Say in plain language what was built.
  3. Name what was **not** delivered — dropped, deferred or blocked; if nothing, say so.

## Edge Cases & Error Handling

| Case | Expected behaviour |
|---|---|
| Run succeeded, no managed feature (Research, Plan, Spike, ad-hoc) | No tally, no callout, heading word `succeeded` |
| Plan run that just published N stories, none implemented | **No** partial label — the tally is populated for Implement-mode runs only |
| Implement run succeeded, all stories done | No callout; `succeeded` |
| Implement run succeeded, `Done == 0`, `Total > 0` | Partial callout, "0 of N stories are complete" |
| Implement run failed with 3 of 5 stories done | Failure banner wins; partial is not shown (BR-3) |
| Store read for the tally fails | Log and render with `Stories == nil`; a reporting path must never fail to post because a count could not be taken |
| `Run.Result == nil` (outcome unknown) | Unchanged: `outcome unknown` + CAUTION callout |
| Run produced a commit that was not pushed | `Publication` line renders (BR-4); brief link absent, as today |
| Abandoned run with no worktree | No brief link, no publication line, existing NOTE callout |
| Body still over budget after the removals | `resultSection` truncation ladder applies as today; `filesSection`/`outputSection` rungs are removed with the sections |
| Progress notice whose evidence block is at the 20,000-byte cap | Unchanged cap and unchanged over-limit error |

## Acceptance Criteria

- [ ] AC-1 A succeeded run's comment contains no `> [!` callout block.
- [ ] AC-2 A succeeded run's comment contains none of: `**Branch:**`, `**Commit:**`, `**Exit status:**`, `**Reason:**`, `**Duration:**`, `**Cost:**`, `**Model:**`, `**Tools:**`, `**Log:**`, `**Note:**`.
- [ ] AC-3 No run comment contains meter-coverage output, `Base branch sync`, `Task branch sync`, `Files touched` or `Run output`.
- [ ] AC-4 No run comment contains the `Replying?` advisory sentence.
- [ ] AC-5 Every run comment still begins with a valid `StatusMarker` for its ref and ordinal, and existing marker-matching recovery tests pass unchanged.
- [ ] AC-6 A failed run's comment still renders its `CAUTION` callout, heading word `failed`, and its existing lead sentence and diagnostic, byte-for-byte apart from the removed sections.
- [ ] AC-7 An Implement run that succeeded with `Done < Total` renders heading word `partial`, a `NOTE` callout naming `Done` and `Total`, and no occurrence of `fail`/`failed`/`error` in that callout.
- [ ] AC-8 An Implement run with `Done == Total`, and any run with `Stories == nil`, renders no partial callout.
- [ ] AC-9 A Plan-mode run against a managed feature with published-but-unimplemented stories renders no partial callout.
- [ ] AC-10 A non-success outcome with `Done < Total` renders the non-success callout, not the partial one.
- [ ] AC-11 The `Research brief` bullet renders as a bare labelled link with no trailing caveat clause, and only under the existing `Pushed && SHAAfter != "" && BriefPath != ""` conditions.
- [ ] AC-12 The `Publication` bullet renders when `CommitSHA != "" && !Pushed` and is absent otherwise.
- [ ] AC-13 A `RenderStoryProgress` body contains the story key, state sentence, parent-issue link and evidence block, and contains no run number, generation number, sequence number, revision hash or implementation digest.
- [ ] AC-14 `RenderStoryProgress` still errors for `engineering_complete`/`verified` without a valid `ImplementationDigest`, and its output still ends with `Marker()`.
- [ ] AC-15 A sub-issue body rendered by the live creation-time path contains no `Replying?` advisory sentence.
- [ ] AC-16 All `internal/report/testdata/*.golden` fixtures are regenerated and reviewed; `make check` passes with no skipped or deleted assertion in `internal/turn/report_test.go`, `internal/turn/subissues_test.go`, `internal/turn/plan_publication_test.go`.
- [ ] AC-17 No consumer parses a removed line: a repository search confirms nothing outside tests reads `Branch:`, `Exit status`, `Meter coverage`, `Files touched` or the sync headings out of a comment body.
- [ ] AC-18 `.claude/agents/plan-orchestrator.md` and `.claude/agents/implement-orchestrator.md` state the BR-9 rules verbatim for their own mode.
- [ ] AC-19 `.codex/skills/plan-orchestrator/SKILL.md` and `.codex/skills/implement-orchestrator/SKILL.md` state the same BR-9 rules, with wording equivalent to AC-18's.

## Evidence inventory

| Criterion | Proof method | Source availability | Version/context bounds | Missing prerequisite / disposition |
|---|---|---|---|---|
| AC-1..AC-14 | Offline Go assertions — golden fixtures in `internal/report/testdata` plus table tests over `RenderRun`/`RenderStoryProgress` | Available: repository source, `go test ./internal/report` | This branch, Go 1.26, pure renderer with no I/O | None |
| AC-15 | Offline assertion against `internal/report/testdata/subissue.golden`, conditional on D-6's reachability finding | Available: repository source | Same | If the legacy path is dormant, the criterion is satisfied by the D-6 finding plus no change |
| AC-16 | `make check` on the worktree | Available | Same | None |
| AC-17 | Scoped documentation/source review — repository search | Available: repository source | Static reachability only; does not cover an external consumer outside this repo | None for in-repo scope; external consumers are explicitly not claimed |
| AC-18, AC-19 | Scoped documentation review — the instruction files contain the stated rules | Available: repository source | Documentation content only | None for the content claim |
| Conformance: that a Plan or Implement agent's actual completion message follows BR-9 | Would require live nested-agent observation inside a managed run | **Unavailable** under the current sandbox (`docs/decisions/nested-agent-evidence.md`) | Unknown agent version/host | **Unsatisfied obligation, retained.** AC-18/AC-19 claim only that the instructions state the rules. Do not write an acceptance criterion asserting agent conformance, and do not treat the documentation ACs as proof of it |

## Decisions

| ID | Decision | Basis |
|---|---|---|
| D-1 | Both sync sections go | Owner answer to Q-04 |
| D-2 | `StoryProgressNotice` is in scope, as a trim of identifiers rather than a redesign | Owner answer to Q-01; the body is already free of run metadata but carries factory identifiers of exactly the kind complained about |
| D-3 | Q-03 is answered with three rules per mode written into the existing orchestrator instruction files, not with new skill files | Owner answer to Q-03 plus "keep it simple"; fewer files and no new cross-tree pointer to maintain |
| D-4 | Partial is computed in `internal/turn` and handed to `internal/report` as a plain tally | Preserves `internal/report`'s mode-blindness, which the research brief identified as the main simplifying constraint |
| D-5 | Partial is gated on Implement-mode runs | Without the gate, every Plan run that publishes stories would falsely report "0 of N complete" |
| D-6 | **Architecture to confirm:** which sub-issue creation path(s) are live for current-generation features on this branch — legacy `RenderSubIssue` vs typed `RenderCompleteStory`/`RenderTypedStory`. Change only the live path(s); do not delete the other | Owner answer to Q-02 ("yes, research this"); the brief could not settle it statically. Handles: `turn/subissues.go:fileStoryIssues`, `turn/plan_publication.go:PublishPlan`, `store.ManagedFeature.WorkflowVersion`, `.claude/skills/shared/workflow.md` |
| D-7 | `Publication` survives conditionally instead of being removed outright | Silently dropping "your commit never reached the remote" would be a correctness regression, not a simplification |
| D-8 | Metering, redaction and marker mechanisms are untouched | Not part of the complaint; all are load-bearing elsewhere |

## Assumptions

| ID | Assumption | Based on |
|---|---|---|
| A-1 | A story counts as done when its `store.ManagedStory` state is `verified` or `completed`, or `EngineeringComplete` is true | `store/managed_stories.go:29-39` and the workflow-v1/v2 split in `store/story_completion.go`; the architect may narrow this per workflow version |
| A-2 | `storySource`'s parent/source links stay | They are navigation links, not run metadata, and the owner's drop list does not name them |
| A-3 | `recoverySection` stays | It tells the owner about a persisted future action; it is content, not debug output |
| A-4 | `teams/**` is a separately released catalog and is not kept in parity by this change | `teams/source/agents/shared/plan-orchestrator.md` already diverges from `.claude/agents/plan-orchestrator.md` |
| A-5 | No external system parses the removed lines | To be confirmed by AC-17 within this repository; not claimed beyond it |
| A-6 | `Result.ToolProfile` (or a new `Result.Mode`) is an acceptable handle for D-5's mode gate | `turn.Request.Mode` exists at `execute.go:337` and `ToolProfile` is documented as derived from the resolved mode |

## Story split (protected-tree ownership)

| Story | Tree | Owner | Depends on |
|---|---|---|---|
| S-1 Slim the run-completion comment | `internal/report/**`, `internal/turn/**`, `internal/report/testdata/**` | shared source | — |
| S-2 Partial story status | same shared source | shared source | S-1 (same heading construction and section list) |
| S-3 Trim sub-issue and story-progress comments | `internal/report/subissue.go`, `story_progress.go`, `internal/turn/**` | shared source | — (independent files; may run alongside S-1) |
| S-4 BR-9 rules in the Claude instruction set | `.claude/agents/plan-orchestrator.md`, `.claude/agents/implement-orchestrator.md` | **Codex** | — |
| S-5 BR-9 rules in the Codex instruction set | `.codex/skills/plan-orchestrator/SKILL.md`, `.codex/skills/implement-orchestrator/SKILL.md` | **Claude** | — |

S-4 and S-5 must not depend on each other. BR-9 above is the single shared wording both quote,
so neither has to wait for the other. Neither is a dependency of S-1..S-3, and no scoped check
for S-4 or S-5 may require the sibling tree to be installed.
