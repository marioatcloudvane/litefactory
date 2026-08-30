# Code Review
# Feature: headless-mode-should-ask-via-a-comment — User Story A1
# Reviewed by: go-code-reviewer
# Date: 2026-08-30
# Status: CLEAN

---

## Scope

Reviewed all files listed for Story A1:

Modified: `internal/harness/prompt.go`, `internal/harness/prompt_test.go`,
`internal/turn/command.go`, `internal/turn/prepare.go`,
`internal/turn/history_test.go`, `internal/daemon/tick.go`.

Created: `internal/turn/ask.go`, `internal/turn/ask_test.go`,
`internal/turn/ask_turn_test.go`, `internal/harness/prompt_ask_test.go`.

Cross-checked against `IMPLEMENTATION_PLAN.md` (User Story A1, tasks
T-A1.1–T-A1.21, AC-A1.1–AC-A1.17) and `ARCHITECT_REVIEW.md` (F-7, F-9, F-10,
F-11, F-15, F-16, F-22, and the `PreviousAsked` section).

## Verification performed

- `go build ./...` — clean.
- `gofmt -l .` — no output, clean.
- `go vet ./...` — clean.
- `go test -race -cover ./...` — all 18 packages pass, no race reported.
  `internal/turn` 87.0%, `internal/harness` 93.3%, `internal/daemon` 87.9%.
- `make lint` (golangci-lint v2.13.1) — `0 issues.`
- `git diff -- go.mod go.sum` — empty. Three direct requires unchanged
  (`go-cmp`, `yaml.v3`, `modernc.org/sqlite`).
- `git diff -- internal/harness/prompt_test.go` around `TestSpikePromptIsUnchanged`
  (lines 371–408 in the current file) — zero lines touched; the full-head
  literal is byte-identical to HEAD. AC-A1.1 holds.

## BLOCKING findings

None.

## ADVISORY findings

### CR-A-001: `internal/daemon/tick.go`'s two edits are not purely comments
**File:** `internal/daemon/tick.go:600` (and the parallel branch at `:471`)
**Story:** A1 / T-A1.13 (KindNone handling)
**Category:** Standards / accuracy of engineer's own characterization
**Finding:** The task brief for this review states the file's diff is "two doc
comments only, no behaviour change (the engineer flagged this)". The diff is
almost entirely doc-comment prose, but one line in each of the two edited
branches also changes a `log.DebugContext` message string: `"comment is
empty; recorded and ignored"` → `"comment asks for nothing; recorded and
ignored"`, and the sibling issue-body branch's log line was reworded too.
This is not a behaviour change in any functional sense — control flow,
return values and the reachable branch are identical — and no test asserts
the exact log string (checked: no match anywhere in `internal/daemon`). It
is a good change on its own merits, since the old wording ("empty") would
now be misleading for the newly-reachable tag-only-comment case. Flagging
only because the log text is a small, factual gap between what was claimed
and what the diff contains — worth a one-line note in the engineer's DONE
report so a future re-review of "comment-only" claims isn't misled.
**Suggestion:** No code change needed. If the DONE report is amended, note
that the log message text was also updated for accuracy, not just the doc
comments above it.

---

## Cross-story summary

A1 is the only story in this session that can change the behaviour of every
existing untagged run, and the implementation treats that risk accordingly.
The two-way branch in `harness.writeHeader` is composed exactly as F-9
required: `headlessRule`/`askingRule` and the now-unconditional `worktreeRule`
are joined with a single space so the untagged byte stream is unchanged,
and `TestSpikePromptIsUnchanged` — confirmed untouched via `git diff` — pins
it. The slug paragraph (F-10) survives on both paths with only its
justification clause changing, and `TestBuildPromptStatesTheFeatureSlug`
(the T-A1.16 split of the old combined test) asserts it under both
`MayAsk` values.

`turn.HasAskTag` is genuinely line-anchored (whole-line, `CutPrefix` + a
trimmed remainder check, never `strings.Contains`), and `TestHasAskTag`'s
table is adversarial in the right way: quote-replies, indentation, fenced
code blocks, backtick-quoted mentions ("the literal is `lf: ask`, as this
plan states it many times") and mis-cased/mis-spaced variants are all
asserted absent. `AskTag` ("lf: ask") and `MarkerPrefix` ("<!-- lf:") are
confirmed structurally disjoint (`AskTag` doesn't start with `<!--`, and no
line beginning with `AskTag` also begins with `MarkerPrefix`), and
`TestTheAskTagAndTheSelfMarkerDoNotMatchEachOther` asserts both directions.

The permission conjunction (`mayAsk := req.Command.Tagged &&
modeMayAsk(req.mode())`) lives once, in `prepare.go`, and `modeMayAsk`
correctly admits `research`, `plan` and `harness.InferMode` while excluding
`implement` and `spike` — matching Q6 as the architect's review restated it.
`previousMayAsk` uses the *previous* run's mode, not this run's — the subtle
bug the architect review called out by name (a tagged spike predecessor must
not read as having had permission) — and guards the zero-`store.Run` case
with the `!resume` early return before ever consulting
`previous.TriggerCommentID`. `TestPreviousMayAsk` and the turn-level
`TestPermissionIsRecoveredFromThePreviousTrigger` /
`TestPermissionIsRecoveredFromTheIssueBody` exercise this at both the unit
and integration level, including the "no previous run at all" and "trigger
comment predates litefactory's watch" cases.

`writeResumeHeader`'s bidirectional emission (AC-A1.11/AC-A1.12) is
implemented as `if in.MayAsk == in.PreviousMayAsk { return }`, with a code
comment that states the fails-open reasoning explicitly, distinguishing it
from the `writeInferGuidance` precedent immediately above it — exactly what
the architect review said someone would otherwise "simplify" away. The F-7
characterization test (`TestResumeHeaderIsPinned`) exists, predates the
permission logic in spirit (it pins the header at `MayAsk ==
PreviousMayAsk == false`, the zero-value case, which is what an unmodified
`writeResumeHeader` produced), and both of its two `PreviousMode` branches
are covered.

The F-15 micro-decision (tag mid-line inside `/spike lf: ask do X`) is
resolved by treating it as not-a-tag rather than special-casing partial
stripping. That reading is sound: it is the direct consequence of
line-anchoring, which is the same mechanism F-22 relies on to stop a
quoting document from granting permission, and relaxing it for this one
shape would reopen exactly that hole. The genuine hazard — a tag on its own
line under a `/spike` argument — is handled (`stripAskTag` operates on the
raw body before `firstToken` runs) and is tested directly
(`TestParseCommandStripsTheAskTag`, "a spike carrying the tag keeps its
argument"). `TestParseCommandNeverFails`'s final assertion is a clean
articulation of why *not* stripping the mid-line case is correct: permission
is read once from the original body and never re-derived from `Text`, so a
`/spike lf: ask` instruction (command word stripped, leaving a tag-shaped
line) is left alone rather than silently edited.

`internal/daemon/tick.go`'s two `KindNone` branches correctly dispose of the
newly-reachable tag-only-comment case: the `runIssueBody` empty-body gate
checks the *raw* body (`strings.TrimSpace(t.issue.Body) == ""`) before
`ParseCommand` ever strips the tag, so a body that is only `lf: ask` passes
the gate as non-empty and then correctly falls through to the `KindNone`
branch after stripping — the previously-unreachable branch the engineer
flagged. `handleComment`'s parallel branch needs no separate gate, since
`ParseCommand` itself classifies both an empty body and a tag-only body as
`KindNone`. Neither branch posts a validation error or a reply, matching
AC-A1.9.

No Go-side ask-round counter, cap or timeout was found anywhere in
`internal/` (swept for `AskRound`, `MaxAsk`, `askCap`, `askTimeout`,
`AskCounter` case-insensitively — no hits), matching AC-A1.14/Q8.

One boundary worth naming even though nothing is wrong there: the doc
comment on `PromptInput.BodyIsInstruction` and the surrounding code in
`BuildPrompt` correctly keep the permission paragraph inside the *header*
(written by `writeResumeHeader` before the early return for
`Resume && BodyIsInstruction`), not inside the `## Context` section that gets
dropped — so a body-triggered resumed run does still see a permission
change if one occurred, even though that combination is documented as
unreachable in production today. Nothing in the code claims otherwise.

## Verdict

CLEAN — No blocking findings. One informational advisory noted above (log
message text, not behaviour) has no action required and does not gate
Implement mode. Proceeding to environment setup.

---
---

# Code Review
# Feature: headless-mode-should-ask-via-a-comment — User Story B1
# Reviewed by: go-code-reviewer
# Date: 2026-08-30
# Status: ADVISORY_ONLY

---

## Scope

This is a **markdown-only docs review**, not a Go code review. No build, no
tests, no lint — none apply. Reviewed against the stated acceptance criteria
for User Story B1 (`IMPLEMENTATION_PLAN.md` lines 604–684) in worktree
`/Users/mariomh/.lf-worktrees/litefactory/task-7` on branch
`lf/7-headless-mode-should-ask-via-a-comment`.

Files reviewed (uncommitted working-tree diff, confirmed via
`git status`/`git diff`):

- `.claude/agents/research-orchestrator.md`
- `.claude/agents/codebase-tour.md`

The rev-6 "OWNER-EXECUTED, NOT AGENT-EXECUTED" banner in the plan for this
story is superseded per the orchestrating agent's instruction — the edits
were made with owner approval in an interactive session and both writes
landed. Not treated as an open issue.

## Acceptance criteria — results

| ID | Result | Notes |
|---|---|---|
| AC-B1.1 | PASS | `research-orchestrator.md:21-27,93-96` — two outputs named (`RESEARCH_BRIEF.md` technical artifact vs. the run's final message), audience table names "business analysts and business users" vs. "Technical readers — Plan mode, engineers, the architect". |
| AC-B1.2 | SKIP | Manual/observational per plan; not gated here. |
| AC-B1.3 | PASS | Required-sections list (`:122-131`) places "The link to the full brief. Early" first; Ordering section (`:133-146`) explicitly instructs "Put the link and the problems near the top." |
| AC-B1.4 | PASS | Required-sections list mandates "Problems and risks — named, not implied, and never omitted," with an explicit no-empty-case ("no problems found" is itself a finding). |
| AC-B1.5 (the trap) | PASS | `:143-146`: "This is a warning about ordering. It is not a size limit and must not be turned into one. There is deliberately no byte limit and no line limit on the summary, and none on the brief. Do not add one. Do not budget against 32 KiB, do not count characters, and do not drop a finding to stay under a number." No numeric cap is imposed anywhere on the brief or the summary in this file. The mechanical "why" (32 KiB cap, cut-from-the-end, tail is where the link/open-questions sit) is stated immediately before this passage. `codebase-tour.md` independently reinforces the same non-inheritance: "The brief and its business summary carry no size limit — that is a deliberate difference of audience, not an inconsistency. Do not carry a cap across to them." No wording in either file reads as a size limit or invites budgeting the brief/summary. |
| AC-B1.6 | PASS | `codebase-tour.md:82-103` — new "Return-size cap" section: "roughly 12 KiB / about 1,500 words," with a per-part breakdown table, and a Rules-section cross-reference (`:111`). |
| AC-B1.7 | PASS | `git status` shows only the two `.claude/agents/*.md` files modified; no Go file touched. |

## Four hard rules (Architecture Notes) — all present, stated as rules

1. Plain language — `research-orchestrator.md:105-108`: "No `path:line` citations. No provenance tags (`[V]`, `[O]`, `[W]`, `[R0]`). No Go identifiers — no package, type, function, method, field or constant names, and no file paths." Present.
2. Honest, does not swallow complexity — `:110-113`. Present, including the "simplifying the language is required; simplifying the picture is not allowed" framing.
3. Names the problems found as its own section, progress-only is failing — `:115-116` and reiterated in Required sections (`:128-130`). Present.
4. Always links the detailed artifact — `:118-120`. Present.

## T-B1.3 — technical brief keeps full detail

Confirmed in two places: `:81-84` ("Write the brief at full technical depth... The business summary does not replace any of that") and the dedicated `:148-152` "What the brief keeps" section (provenance tagging, citations, section depth all retained; summary is "an addition to the brief, never a replacement"). Consistent with each other and with T-B1.3's requirement.

## Citation accuracy check

`research-orchestrator.md:137-138` claims:
- `maxResultBytes` at `internal/report/run.go:25`
- `truncateEnd` called at `internal/report/run.go:713`

Verified directly against the worktree file:

```
25:	maxResultBytes = 32 << 10
...
713:	if cut, ok := truncateEnd(text, "", maxResultBytes-len(resultOverlongNotice)-maxFenceBytes); ok {
```

Both citations are byte-accurate. (Note: the plan's own Architecture Notes cite `report/run.go:24` and `run.go:528` for the same facts — those are the plan's numbers, not the implementation's; they are stale relative to current `run.go`, but the *committed doc text* being reviewed here cites the correct, current line numbers, which is what matters for this AC.)

## BLOCKING findings

None.

## ADVISORY findings

### CR-A-002: "The only file you write" contradicts the MANIFEST.md bullet immediately below it
**File:** `research-orchestrator.md:23-25`
**Story:** B1 / T-B1.1
**Category:** Consistency / precision
**Finding:** The Output list reads:
```
- `$session/RESEARCH_BRIEF.md` — ... The only file you write.
- **The run's final message** — ...
- Updated `$session/MANIFEST.md` with Research marked done.
```
Calling `RESEARCH_BRIEF.md` "the only file you write" in the same list that also requires updating `MANIFEST.md` two lines later is a literal self-contradiction (MANIFEST.md is also written). It doesn't affect any acceptance criterion and a careful reader will resolve it correctly (MANIFEST.md is a stamp, not a content artifact), but it's loose enough to cause confusion on a future edit.
**Suggestion:** Either drop "The only file you write," or qualify it, e.g. "the only *artifact document* you write" to distinguish it from the MANIFEST.md stamp.

### CR-A-003: Exit checklist doesn't mirror the new Choreography step 5
**File:** `research-orchestrator.md:158-164` (unchanged by this diff) vs. `:55` (new)
**Story:** B1 / T-B1.1
**Category:** Consistency / completeness
**Finding:** Choreography step 5 (new in this diff) says: "Write the business summary as the run's final message ... Never skip it, and never substitute the brief for it." The `## Exit` section — the run's end-of-flow checklist — was not touched by this diff and still lists only: write `RESEARCH_BRIEF.md`, stamp `MANIFEST.md`, route back to the dispatcher. It does not mention producing the business summary, even though that summary is now a required output of the run (per the new `## Output` section) and is explicitly flagged "never skip." A reader who treats `## Exit` as the authoritative close-out list rather than reading the whole file could miss the newly-added deliverable.
**Suggestion:** Add a step to `## Exit` (or a forward-reference) that mirrors Choreography step 5, so the business summary is required by the section a reader is most likely to treat as the final checklist.

---

## Cross-story summary

B1's two files are self-consistent on every gated acceptance criterion: the
audience split, the four hard rules, the ordering-not-budget framing for
AC-B1.5 (the trap this story was designed to test), and the brief's
untouched full-detail contract are all present, correctly worded, and cross-
reference each other coherently between `research-orchestrator.md` and
`codebase-tour.md`. The one genuine cross-file design point — that
`codebase-tour.md`'s new cap must not leak onto the brief/summary — is
handled explicitly and correctly in both directions. The two advisory
findings above are wording/completeness gaps in `research-orchestrator.md`
that predate or sit alongside this diff's new content; neither weakens any
of the four hard rules or the no-size-limit guarantee, and neither blocks
downstream stories (T-B1.3's reuse-by-C1 note is unaffected).

## Verdict

ADVISORY_ONLY — 2 suggestions noted (CR-A-002, CR-A-003). All seven gated
acceptance criteria (AC-B1.1, AC-B1.3–AC-B1.7) pass; AC-B1.2 is out of scope
per plan. Both citations verified byte-accurate against the current
`internal/report/run.go`. No Go file modified. Proceeding — no re-review
cycle needed for this story.
