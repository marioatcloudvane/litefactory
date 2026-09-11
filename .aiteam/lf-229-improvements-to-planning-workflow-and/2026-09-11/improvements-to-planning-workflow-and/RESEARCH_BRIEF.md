# Research Brief — improvements-to-planning-workflow-and

## Request

From issue #229 (verbatim complaint, lightly trimmed): litefactory's run-completion
comments carry "a lot of useless metadata" and "look like debug output" — the
example quoted is a Research-run comment with a header line, Branch/Commit/
Publication lines, an artifact link, Exit status/Duration/Cost/Model/Tools/Log/
Meter-coverage lines, a "Replying?" guidance paragraph, a "Base branch sync"
section, a mode-specific "Result" section, a "Files touched" list and a "Run
output" section.

The user's stated design intent (goal, not literal spec — Plan owns the final
wording):
- Keep only the key facts per run; drop internal run metadata (branch, commit,
  exit status, duration, cost, model, tools, log path, meter coverage) — it
  already lives in the logs.
- Drop the "Replying?"/comment-guidance section.
- Drop "Base branch sync".
- A **failed** run gets a clear failure banner at the top.
- A **partial** run (e.g. not all user stories implemented) gets a **partial**
  label — explicitly *not* worded as failure.
- A **succeeded** run needs no banner at all — success is the silent default.
- Drop "Run output" and "Files touched".
- Keep only the artifact link (Research brief / Plan link / etc.) plus whatever
  the mode-specific "Result" content genuinely is.
- Also simplify how comments are posted on the per-story GitHub **sub-issues**
  Plan mode creates — "should also be straightforward and not so complicated."

This is Research only: describe current behavior and hand Plan precise file/line
references, the shared-vs-mode-specific boundary, what a "partial" status would
require, and how sub-issue comments work today. No new format is designed here.

## Codebase Context

All findings below are `[V]` — verified this run by the `codebase-tour`
specialist reading the actual Go source, tests and skill files (two passes: an
initial full sweep, then a targeted follow-up on gaps). File:line references are
as reported by that specialist; none were independently re-read by this
orchestrator (per this team's rule that orchestrators read structure, not
content).

### The rendering package: `internal/report`

`internal/report.Reporter` (`internal/report/reporter.go:31`, constructed via
`report.New` at `reporter.go:51`) is the **one** renderer type for all
GitHub-comment bodies in the system. It is a pure function of plain-data input
structs — no DB, no git, no forge client — documented as deliberate in the
package's own doc comments. Every body passes through the same S-7
redaction/size-limit pass (`internal/redact`) before it is posted.

Render entry points relevant to this brief:
- `RenderRun` (`reporter.go:87`) — the run-completion comment (the one the user
  quoted).
- `RenderReply` (`reporter.go:117`) — command-reply comments (e.g. invalid
  `Mode:` label replies); a different path, not implicated by the user's
  complaint.
- `RenderSubIssue` (`internal/report/subissue.go:153`) — legacy per-story
  sub-issue body.
- `RenderCompleteStory` (`internal/report/complete_story.go:11`) and
  `RenderTypedStory`/`RenderTypedPlan` (`internal/report/typed_story.go:14`,
  `:65`) — the newer typed/managed sub-issue path.
- `RenderStoryProgress` (`internal/report/story_progress.go:26`) — a **third**,
  distinct comment type, not named in the user's request (see below).

### 1–4, 8–9: the run-completion comment — one shared assembler, one shared status enum, no mode branching

`(*Reporter).runBody` (`internal/report/run.go:290-364`) is the single top-level
assembler for the comment the user quoted. It builds five ordered sections —
`secFixed, secSync, secResult, secFiles, secOutput` (`run.go:266-277`, joined
with a blank-line separator, `run.go:390`) — and, if the body exceeds the byte
budget, trims in a documented fixed priority (log tail → result → files,
`run.go:282-289`).

`fixedSection` (`run.go:576-596`) is itself a concatenation, in order, of:
marker + story source, the `### litefactory · run N · <word>` heading, an
optional alert callout, `metadataList`, `meterMetadata`, `ReplyPlainlyNotice`,
and an optional recovery section.

Section-by-section:

- **Header/status** (`### litefactory · run N · succeeded`) — `run.go:581`.
  `view.word` comes from `describeOutcome`/`describeRunOutcome`
  (`run.go:407-423`, `502-565`), switching on `agent.RunResult.Outcome`, an
  enum at `internal/agent/result.go:16-42`:
  `OutcomeSucceeded, OutcomeFailed, OutcomeTimedOut, OutcomeAbandoned,
  OutcomeNoResult, OutcomeInterrupted`. `withFailure`
  (`run.go:432-463`) overrides wording to "did not run"/"not completed" when
  `Run.Failure != nil`. **No "partial" outcome exists anywhere in this enum or
  in the run/harness model.**
- **Branch/Commit/Publication** — bullets inside `metadataList`
  (`run.go:628-732`): Branch at `run.go:634-636`, Commit at `run.go:637-653`
  (four cases), Publication at `run.go:655-657`. `Run.Publication` is populated
  upstream at `internal/turn/report.go:791`.
- **Artifact link** ("Research brief: …") — `run.go:667-669`, calling
  `run.briefLink()` (`internal/report/link.go:170-181` → `blobURL`,
  `link.go:43-63`). It is deliberately kept inside `metadataList`/`secFixed`
  specifically so it survives truncation (comment at `run.go:659-666`). It is a
  small, independently testable method (`link_test.go`) taking only
  `Run.Pushed/SHAAfter/BriefPath` — reasonably decoupled from the rest of the
  body, and shares its `blobURL` helper with the commit-link renderer in
  `reply.go:983`.
- **Exit status/Duration/Cost/Model/Tools/Log** — all in `metadataList`: Exit
  status `run.go:671-686`, Duration `run.go:688-692` (via `Run.duration()`,
  `run.go:239-251`), Cost/Model `run.go:694-710`, Tools `run.go:712-714`
  (delegated to `toolsEntry`, `run.go:754-797`, itself non-trivial: resolved vs.
  requested vs. profile vs. not-granted), Log `run.go:716-725`.
- **Meter coverage** — **not** part of `metadataList`; a separate function,
  `meterMetadata` (`internal/report/meter.go:149-166`), string-concatenated
  onto the body right after `metadataList`'s output (`run.go:587-588`). So the
  metadata block is mostly one shared function, but the meter lines are a
  second, separately maintained function whose output is just appended — a
  future removal touches two call sites, not one.
- **"Replying?" guidance** — the constant `ReplyPlainlyNotice`
  (`internal/report/marker.go:42-44`), reused verbatim across `fixedSection`
  (`run.go:590`), `Reply.body()` (`reply.go:1001`), and sub-issue bodies
  (`subissue.go:231`). **Caution for Plan**: this constant is the
  human-readable half of a load-bearing anti-spoofing mechanism — the HTML
  comment marker it sits beside (`internal/report/marker.go`) is what lets
  litefactory tell its own comments apart from a reply that pastes the marker
  back (`StatusMarker`, referenced at `fixedSection`, `run.go:581`). Dropping
  the advisory *paragraph* is safe; the underlying marker itself is not part of
  what the user asked to remove and must stay.
- **"Base branch sync"** — `syncSection` (`run.go:918-982`), heading constant
  `syncHeading` (`run.go:908`). Conditional: renders nothing when
  `Run.Sync == nil` or `Outcome == SyncUpToDate` (`run.go:920-922`); every other
  `SyncOutcome` (`Synced/Blocked/Skipped/DeferredDirty/Failed`, enum
  `run.go:831-865`) renders a sentence, deliberately never silently (an `AC-3.2`
  comment enforces this). A **second**, independent "Task branch sync" section
  (`taskSyncSection`, `run.go:1227-1244`) is prepended ahead of it when
  `Run.TaskSync != nil` (`run.go:298-303`) — two separate sync disclosures
  exist today, not one; the user's complaint likely covers both, but only named
  one.
- **"Result"** — `resultSection` (`run.go:1054-1075`), heading constant
  `resultHeading` (`run.go:1038`). This function is **mode-blind**: it wraps,
  sanitizes, closes fences on, and truncates `run.Result.ResultText` — the
  coding agent's own final-message text — without inspecting what produced it.
  Confirmed by grep: no `"Research"`/`"Plan"`/`"Implement"` string literal or
  mode check exists anywhere in `run.go`, `reporter.go`, or `runBody`'s call
  graph. All three modes' final-comment rendering is the **identical** code
  path (`RenderRun` → `runBody`), and the only per-mode difference is the text
  the mode's agent itself wrote into `ResultText`
  (`internal/turn/report.go:740`, `runInput`, line 780: `Result:
  result.Harness`, copied through untouched).
- **"Files touched"/"Run output"** — `filesSection` (`run.go:1084-1114`,
  heading `**Files touched (%d)**`) and `outputSection`/`outputSummary`
  (`run.go:1116-1148`, wrapped in a `<details>` block). Both single, shared
  functions called once each from `runBody`.

**Poster-side plumbing** (`internal/turn/report.go`): `Reporter.ReportRun`
(`report.go:534`) builds the `report.Run{}`, calls the renderer, then posts via
`Poster.PostComment`; `post` (`report.go:686`) and `runInput`
(`report.go:740`) are the two supporting functions. The forge post itself is
`github.Client.PostComment`.

### The Research vs. Plan vs. Implement "Result" content is a prompt-level contract, not a code-level one

Because `resultSection` is mode-blind, the Research-specific structure the user
quoted ("Problems and risks / What was found / Open questions", the standing
"move to the next phase" invitation) is **entirely prompt-driven**, enforced by
two skill files: `.claude/skills/shared/research-report-entry.md` (four hard
rules; fixed section order: link-to-brief, then What was found, Problems and
risks, What to work on next) and `.claude/skills/shared/research-open-questions.md`
(a second hard contract for the full question inventory).

`.claude/agents/plan-orchestrator.md` has **no equivalent skill file and no
comparable structural template** for its normal-completion message — only loose
prose: "Finish each subprocess with one final report" (line 97), "Return the
questions in the final report and stop successfully" for a discussion iteration
with no fixed presentation format (line 116), and one required sentence
recommending `/implement <instruction>` on completion (line 389).

`.claude/agents/implement-orchestrator.md` similarly has no Research-equivalent
contract for a *normal-completion* comment. It has a hard 5-part structure, but
only for an **escalation** report (lines 330-336: Where / What triggered it /
What is already done / The decision needed / The options), and a
`DRIFT_REPORT.md` file template (lines 529-548: Delivered as planned / Added
scope / Dropped or deferred / Architectural deviations / Verdict) that is
carried into the final result but is an internal artifact template, not a
business-register comment contract. `.claude/skills/shared/workflow.md` was
also checked and imposes no cross-mode content contract for the comment text
itself.

**Implication for Plan**: shortening the Research comment is a scoped,
low-risk edit confined to `internal/report/run.go`'s section functions (plus
the two skill files above). Achieving an equally disciplined *shape* for
Plan's and Implement's "Result" content is a separate piece of work — no
existing skill file constrains it today, so either their current freeform
content already satisfies the desired end state, or a new skill file
equivalent to `research-report-entry.md` needs to be authored for each. This
brief does not resolve which.

### 5. Sub-issue comments — two independent, already-thin code paths

Two parallel mechanisms exist for posting to Plan's per-story sub-issues, and
**neither reuses `runBody`, `metadataList`, or any run-report section
function** — both are already structurally simple and already free of the
"useless metadata" the user complained about (confirmed: no branch, commit,
exit status, cost, model, tools, log path, or meter data appears in either).

- **Legacy/local path**: `internal/turn/subissues.go:fileStoryIssues` (line
  106) reads `plan_brief.md`'s Story Index, calls `buildSubIssueSpec`
  (`subissues.go:375-423`), which builds a `report.Story{}` and calls
  `RenderSubIssue` (`internal/report/subissue.go:153-182`). Body
  (`subIssueBody`, `subissue.go:215-256`): marker + `storySummary` (business
  register, `subissue.go:264-270`) + optional `detailLine` (permalink to the
  story's plan file, `subissue.go:314-326`) + optional `storyExcerpt`
  (collapsed `<details>` verbatim block, `subissue.go:278-296`) +
  `ReplyPlainlyNotice`. The `Story` struct (`subissue.go:62-116`) has no field
  through which run metadata could even be threaded in. Golden fixture
  `internal/report/testdata/subissue.golden` is ~20 lines vs.
  `run_succeeded.golden`'s ~75.
- **Managed/typed path**: `internal/turn/plan_publication.go:PublishPlan`
  (line 52) calls `RenderTypedStory` (`internal/report/typed_story.go:14-60` —
  User Story / Acceptance Criteria / Tasks table / Architecture / Dependencies
  / Story Guidance, all markdown-escaped via `storyText`,
  `typed_story.go:104-119`) to build the content file, then
  `RenderCompleteStory` (`internal/report/complete_story.go:11-35`) to build
  the issue body itself — marker + the full spec excerpt verbatim, sanitized
  and fence-closed, with no summarization and no truncation permitted (it
  errors rather than truncates, `complete_story.go:24-26`). Even sparser than
  the legacy path.

**Open question for Plan** (not resolved here): whether both paths are live
concurrently — `.claude/skills/shared/workflow.md` describes legacy vs.
contract-2/separated-workflow generations coexisting until an explicit owner
CLI upgrade — or whether one supersedes the other for all newly-approved
features. If both are reachable today, "simplify sub-issue posting" may mean
touching two independent renderers rather than one; if only one is live for
current-generation work, the other may not need changes at all.

### A third comment type not named in the user's request: `StoryProgressNotice`

`internal/report/story_progress.go:11-66` defines `StoryProgressNotice` and
`RenderStoryProgress` (line 26) — a fixed-shape body (story key, one-sentence
state description for `started`/`engineering_complete`/`verified`/
`invalidated`/`blocked`, a parent-issue link with run/generation/event numbers,
approved revision hash, validated implementation digest, and an indented
evidence block capped at 20,000 bytes). It is triggered by
`internal/turn/story_progress.go`'s `Poll`/`Finish` (lines 99, 107) reading
pending `store.StoryProgressEffect` rows written by Implement mode's
incremental event protocol, and delivered via `deliverOne`
(`internal/turn/story_progress.go:472-515`), which posts **to the story's own
sub-issue** (`f.IssueNumber`, line 506) after verifying that sub-issue's
claimed parent matches. No golden test exists for `RenderStoryProgress` in
`internal/report` (only a substring assertion in
`internal/turn/story_progress_test.go:47`, against a live render+post path).
Content is already free of run metadata, but this is a **third, independent**
comment-rendering/delivery mechanism the user's request did not name — flagged
so Plan can make an explicit in/out-of-scope call rather than discover it
mid-implementation.

### 3, 6. Status model — no "partial" signal exists anywhere; one would need to be built

Grepped `partial` (case-insensitive) across `internal/`: every hit concerns
metering/usage-collection status (`agent.CollectionPartial`,
`internal/store/meter*.go`, `internal/harness/usage.go`,
`internal/codex/usage*.go`) or unrelated prose (partial writes, partial
redaction, partial indices) — **none** concerns "some but not all stories in a
feature implemented."

The nearest existing signal is boolean only, not a count, and is not surfaced
in any comment renderer today:
- `internal/store/story_completion.go:16-21` (`CompleteManagedFeature`, legacy
  workflow v1) and lines 25-30 (workflow v2 twin,
  `engineering_succeeded`) flip a feature's state to completed only via a
  `NOT EXISTS(... state != 'completed')` check — an all-or-nothing existence
  test, not a done/total count.
- `store.ManagedFeature` (`internal/store/managed_stories.go:19-26`) has
  `WorkflowVersion, EngineeringSucceeded bool, ParentTask, Generation, State
  string, Ready bool` — no `StoriesTotal`/`StoriesDone` field.
- `store.ManagedStory.State` (`internal/store/managed_stories.go:38`) is a
  plain string; its CHECK constraint
  (`internal/store/migrations/0011_managed_stories.sql:16`) enumerates
  `'planning','frozen','running','blocked','verified','completed'` — no
  `'partial'` value.

**Implication for Plan**: a "partial" banner cannot be built by surfacing an
existing field. It needs a new query (most plausibly a
`COUNT(*) ... GROUP BY state` over `managed_stories` for the current
generation) and a place to plug that signal into `describeRunOutcome`/
`outcomeView` (`run.go:407-565`) alongside the existing
`Succeeded/Failed/TimedOut/Abandoned/NoResult/Interrupted` outcomes — which
today is driven purely by `agent.RunResult.Outcome`
(`internal/agent/result.go:16-42`), a value that knows nothing about
story-count state. Whether "partial" becomes a new `agent.Outcome` enum value,
or a separate concept layered onto the header word choice only when the
generation has multiple stories, is a design decision for Plan, not
established by anything in the current code.

### 7. Documentation

No user-facing doc mandates or describes the exact comment format.
`docs/plan-outcome.md`, `docs/report-recovery.md`, `docs/meter.md`,
`docs/meter-provider-sources.md`, `docs/story-contract*.md`,
`docs/workflow/*.md` were checked; a targeted grep for the field labels
("Branch:", "Exit status", "Meter coverage", etc.) found zero matches. The
format is defined only in `internal/report`'s code and its golden tests, plus
(for Research's `Result`-content shape only) the two skill files named above.
A future Implement change touches no user-facing doc for the format itself,
beyond whatever Plan decides to newly document.

### Prior intent to simplify

None found. Grepped `TODO|FIXME|simplify|shorten|too long|too verbose` across
`internal/report` and `internal/turn` — only false positives (doc comments
describing existing truncation behavior, e.g. `run.go:1151,1173`,
`subissue.go:329,339`; unrelated `TODO` string literals in test fixtures). No
design notes anywhere in the Go source signal a prior plan to shrink this
format.

### Tests that pin the current exact wording

Golden-file tests: `internal/report/report_test.go` drives `checkGolden(...)`
against `internal/report/testdata/*.golden` — `run_succeeded`, `run_failed`,
`outcome_*` (×5), `run_truncated`, `run_push_failed`,
`run_no_result_recorded`, `run_sparse_result`, `reply_*` (×6), `sync_*` (×5).
`internal/report/subissue_test.go` checks `testdata/subissue.golden` plus
several substring assertions. Also present, asserting exact substrings/text
rather than goldens: `internal/report/complete_story_test.go`,
`link_test.go`, `meter_test.go`, `reply_test.go`, `replykind_test.go`,
`followups_test.go`, `task_publication_test.go`, `task_sync_test.go`,
`conflict_preview_test.go`, `conflict_proposal_test.go`,
`confirm_reply_test.go`, `startup_diagnostic_test.go`, `typed_story_test.go`.
Outside the package: `internal/turn/report_test.go`,
`internal/turn/subissues_test.go`, `internal/turn/plan_publication_test.go`,
`internal/github/subissues_test.go`. `go test ./internal/report -update`
regenerates goldens, but every diff needs manual review, and
`internal/turn/report_test.go`/`subissues_test.go` likely need separate
assertion updates since they check behavior, not just golden bytes.

## Domain / Problem Space

litefactory posts one comment per completed run to the triggering GitHub
issue/PR, using it as the primary human-facing surface for run outcome. The
same comment currently mixes three kinds of content with very different
half-lives: (a) durable, load-bearing operational facts already duplicated in
the local run log and the store (branch, commit, exit status, duration, cost,
model, tools, log path, meter coverage) — the user's core complaint is that
none of this belongs in a comment meant for a human deciding what to do next;
(b) a fixed advisory paragraph about how to reply safely, present regardless of
whether it is ever needed; (c) the actual decision-relevant content — outcome,
the artifact link, and the mode's own account of what it found or built. The
separate sub-issue and story-progress comments already skew toward (c) alone,
which is itself informative: the "debug output" problem is concentrated in one
render path (`runBody`/`metadataList`/`meterMetadata`), not systemic across
every comment type in the codebase.

## Constraints & Risks

- **`resultSection` is mode-blind** `[V]` — a major simplifying constraint: the
  header/metadata/sync/files/output sections can be edited or removed without
  touching how Research, Plan or Implement produce their own result text, and
  without threading a "mode" parameter through `internal/report`.
- **The HTML-comment marker (`internal/report/marker.go`) is not the same
  thing as the "Replying?" advisory paragraph** `[V]` — the marker is a
  security-relevant anti-spoofing mechanism (prevents a pasted-back marker
  from making litefactory misread a reply as its own comment); the advisory
  text is what the user wants removed. Plan needs to keep the marker while
  dropping the paragraph, not conflate the two.
- **Meter coverage is appended by a second function (`meterMetadata`), not
  part of `metadataList`** `[V]` — a "drop the metadata block" change has two
  call sites, not one (`run.go:587-588`).
- **Two independent sync sections exist** (`syncSection` for base-branch sync,
  `taskSyncSection` for task-branch sync) `[V]` — the user named only "Base
  branch sync"; Plan should decide explicitly whether task-branch sync is
  in scope too, since it renders through a separate function with separate
  conditions.
- **No "partial" status exists anywhere in the codebase** `[V]` — introducing
  one is new design work (a story-count query plus a place to plug it into
  `describeRunOutcome`), not a rendering-only change. Scope and query cost
  (per-run `COUNT ... GROUP BY` against `managed_stories`) need Plan-level
  sizing.
- **Two independent sub-issue-creation renderers may or may not both be live**
  `[V, with an unresolved branch]` — legacy (`RenderSubIssue`) vs. typed
  (`RenderCompleteStory`/`RenderTypedStory`) paths exist in code; whether both
  are currently reachable for in-flight and newly-approved features, or only
  one is live going forward, was not established this run (see Open
  Questions).
- **A third comment type (`StoryProgressNotice`) exists with no golden test**
  `[V]` — any blanket "simplify comment rendering" scope statement should
  explicitly say whether this is included; it already carries no "useless
  metadata," so if included the work there is different in kind (confirming
  it stays minimal) rather than removing bulk.
- **Extensive golden-file coverage will need regeneration and manual review**
  `[V]` — listed above; this is mechanical but non-trivial given the number of
  fixtures across `internal/report` and `internal/turn`.
- **No user-facing documentation currently describes the format** `[V]` — this
  lowers doc-update risk for the format itself, but if Plan chooses to author
  a Research-report-entry-equivalent skill file for Plan/Implement's Result
  content, that is new documentation work, not an update to existing docs.

## Open Questions

## Q-01 Should "simplify comment rendering" include Implement's per-story sub-issue progress notices (`StoryProgressNotice`), or only the two comment types the user explicitly named (the run-completion comment and sub-issue creation)?

`RenderStoryProgress` (`internal/report/story_progress.go:26`) posts to the
same sub-issues Plan creates, but at a different time (during Implement's
incremental event protocol, not at story-creation time) and through
independent code with no golden test. The user's request named "how comments
are posted on GitHub sub-issues" without distinguishing creation-time vs.
progress-time comments, and did not mention this mechanism by name.

| Solution | Details | Benefits | Tradeoff |
|---|---|---|---|
| Treat it as in scope — Recommended | Plan reviews `StoryProgressNotice`'s shape alongside `RenderSubIssue`/`RenderCompleteStory` and confirms/adjusts it | One coherent pass over everything that posts to a sub-issue; avoids a follow-up surprise later | Slightly larger Plan scope; this notice is already free of run metadata, so likely low-effort to confirm |
| Treat it as out of scope | Only the two named comment types are touched; `StoryProgressNotice` is left untouched | Matches the literal request narrowly; smaller diff | The user may not know this third mechanism exists and could be surprised it wasn't addressed |

## Q-02 Are both the legacy sub-issue renderer (`RenderSubIssue`) and the typed/managed one (`RenderCompleteStory`/`RenderTypedStory`) currently reachable for live work, or does one supersede the other for current-generation features?

`internal/turn/subissues.go:fileStoryIssues` and
`internal/turn/plan_publication.go:PublishPlan` are two independent code paths
producing structurally different sub-issue bodies. `.claude/skills/shared/workflow.md`
describes legacy and contract-2/separated-workflow generations coexisting
until an explicit owner CLI upgrade, which suggests both paths may still be
exercised depending on a feature's persisted generation — but this run did not
trace which persisted-generation state routes to which path, since that is a
runtime/data question rather than a static-code one.

| Solution | Details | Benefits | Tradeoff |
|---|---|---|---|
| Confirm reachability before Plan scopes the change — Recommended | Plan (or a short follow-up research pass) checks whether any currently-active feature/session is still on the legacy generation before deciding whether to touch `RenderSubIssue` at all | Avoids either wasted work on a dead path or an unnoticed gap on a live one | Small extra step before Plan can finalize scope |
| Assume both are live and update both | Change both renderers symmetrically regardless of current usage | Simplest to reason about, no reachability check needed | Possible wasted effort if the legacy path is already dormant for this branch/environment |

## Q-03 Should Plan/Implement's normal-completion "Result" content get a Research-equivalent structural contract (a skill file analogous to `research-report-entry.md`), or does today's freeform content already meet the desired end state once the surrounding metadata is stripped?

`resultSection` in code is mode-blind and already minimal — it just wraps
whatever text the mode's own agent produced. Research's *content* discipline
(four hard rules, fixed section order) comes entirely from
`.claude/skills/shared/research-report-entry.md`. No equivalent skill file
governs Plan's or Implement's normal-completion message; only Implement's
*escalation* path and its internal `DRIFT_REPORT.md` template have comparable
rigor.

| Solution | Details | Benefits | Tradeoff |
|---|---|---|---|
| Author matching skill files for Plan and Implement — Recommended | Create `.claude/skills/shared/plan-report-entry.md` / `implement-report-entry.md` (or equivalent) mirroring Research's four hard rules, adapted to each mode's content | Consistent, predictable "Result" shape across all three modes once the metadata sections are stripped — directly serves the user's "shorter and simpler" goal | New documentation + orchestrator-prompt work beyond pure Go rendering changes |
| Leave Plan/Implement's Result content as-is | Only strip the shared metadata sections (branch/commit/exit status/etc.); leave each mode's own final-message content exactly as currently produced | Smaller, lower-risk change; matches the user's request narrowly (they complained about metadata, not about Plan/Implement's own wording) | Plan's and Implement's comments could remain inconsistent in shape/length with each other and with Research's newly-tightened one |

## Q-04 Should the "Task branch sync" section (`taskSyncSection`, `run.go:1227-1244`) be dropped alongside "Base branch sync" (`syncSection`), given the user named only the latter?

Two independent, similarly-conditional sync-disclosure sections exist. The
quoted example in the user's complaint only showed "Base branch sync" firing;
it's unclear whether the user is aware a second, separate section exists, or
would want it kept, dropped, or folded into a single line.

| Solution | Details | Benefits | Tradeoff |
|---|---|---|---|
| Drop both — Recommended | Treat "no more sync exposition in the comment, it's in the logs" as the underlying intent and remove `taskSyncSection` too | Matches the spirit of the user's complaint (this section is exactly as much "internal state, already in logs" as the one they named) | Slightly exceeds the literal request; needs explicit owner sign-off since it wasn't named |
| Drop only "Base branch sync" as literally requested | Leave `taskSyncSection` untouched | Matches the literal ask exactly | Comment could still show sync-related "debug output" the user would likely also want gone, just not phrased that way |

## Recommended scope for Plan mode

- **In scope, low ambiguity**: remove/condense `metadataList`'s
  Exit-status/Duration/Cost/Model/Tools/Log lines and the separate
  `meterMetadata` bullets (`run.go:628-732`, `meter.go:149-166`); remove the
  `ReplyPlainlyNotice` advisory paragraph from the run-completion comment only
  (keep the underlying marker mechanism); remove "Files touched"
  (`filesSection`) and "Run output" (`outputSection`) from `runBody`; make the
  success case render no outcome banner (adjust `describeRunOutcome`/
  `outcomeView`, `run.go:407-565`, and `fixedSection`'s heading construction,
  `run.go:581`) while keeping a clear failure banner; keep the artifact-link
  line (`briefLink`, `link.go:170-181`) and `resultSection` untouched in
  mechanism (only their surrounding sections change).
- **In scope, needs a design decision recorded in the plan**: whether
  "Base branch sync" removal also covers "Task branch sync" (Q-04); what
  exactly a "partial" status means, where its signal comes from (new
  story-count query, most plausibly against `managed_stories`), and how it
  plugs into the existing outcome-word machinery without becoming a new
  `agent.Outcome` value if that's judged too invasive (open design question,
  not resolved by this brief); whether Plan/Implement should get a
  Research-equivalent Result-content skill file (Q-03).
- **Needs a scoping decision before work starts**: whether
  `StoryProgressNotice` is in scope (Q-01), and whether both sub-issue
  rendering paths are currently live and need parallel changes or whether
  only one is (Q-02).
- **Out of scope for this change, flagged only as a dependency to preserve**:
  the S-7 redaction pass, the marker/anti-spoofing mechanism, and
  `resultSection`'s sanitize/fence-close/truncate behavior — none of these
  were named as problems and all are shared by every comment type touched.
- **Any code change here must be accompanied by**: updating the golden
  fixtures under `internal/report/testdata/` (`go test ./internal/report
  -update`, then manual diff review) and the assertions in
  `internal/turn/report_test.go` and `internal/turn/subissues_test.go` that
  check behavior beyond golden bytes.
