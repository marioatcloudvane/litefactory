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

---

# Code Review — T-B2.1, T-A1.11, T-A1.12 (markdown half), T-A1.21
# 2026-08-30 | Docs-only review | Branch: lf/7-headless-mode-should-ask-via-a-comment
# Reviewed by: go-code-reviewer
# Status: ADVISORY_ONLY

## Scope

`git status --porcelain | grep '\.go$'` is empty — confirmed no Go file is touched. `CLAUDE.md` does
not appear in the diff — confirmed unmodified, so the Plan gate (AC-B2.9's second half) is untouched.
Diff reviewed: `.claude/agents/research-orchestrator.md` (165→223 lines) and
`.claude/agents/plan-orchestrator.md` (112→134 lines), against B1 (`ebf8d17`, not part of this diff),
`IMPLEMENTATION_PLAN.md` (Story B2 ~L687-L780, Story A1 ~L809-L966), `notes.md`'s "Implement run 2"
section, and `internal/harness/prompt.go:470-560` (`askingRule`, `inferAskingSeam`, `headlessRule`,
`askingRevokedRule`) plus `internal/turn/ask.go`'s `modeMayAsk`.

## Gated checks from the task brief

1. **AC-B1.5 / no-size-limit — PASS.** Grepped the diff for `short|concise|budget|limit|word count|
   line count|keep it|brevity|byte|kib|truncat` — the only hit is the pre-existing "no round cap ...
   imagined limit" sentence about *questions*, not about summary/brief size. The new `### Ordering —
   a warning, not a budget` heading (already landed in B1, unchanged here) is left alone; T-B2.1's new
   `### Where the summary goes` section adds no size language of its own. No byte/line/word budget is
   introduced anywhere in this diff.
2. **B1's four hard rules / audience split — PASS.** Untouched by this diff (lines 91-133 of the
   current file are outside the diff hunks) and nothing in the new text restates or drifts from them.
3. **AC-B2.9 — PASS.** New `### Where the summary goes` explicitly requires
   `$session/RESEARCH_BRIEF.md` be written "on every run," and `## Exit` step 1 still writes it
   unconditionally. `CLAUDE.md` confirmed absent from the diff.
4. **AC-A1.14 — PASS.** Both files state, in matching language, that there is no round cap/counter/
   timeout and that none is to be added, with the qualifier "not in litefactory's Go code." Verified
   against the actual tree: `grep -rn -i "askround|round.*count|maxask|ask.*limit|ask.*cap\b"` over
   every non-test `.go` file returns nothing. `modeMayAsk` (`internal/turn/ask.go:137`) contains no
   counting logic. The claim is accurate.
5. **AC-A1.15 — PASS.** Both files carry `### The standing "move to the next phase" line`, on every
   asking message, and both correctly frame it as the *One Run, One Mode* human-comment handoff rather
   than self-granted continuation permission — matching `askingRule`'s own framing in
   `prompt.go:508-518`.
6. **Cross-file consistency — PASS, with one drift noted below (CR-A-004).** Rules and audience-split
   language are adapted per mode without contradicting each other. On the structural question asked
   directly (`## Asking` top-level in research-orchestrator.md vs. `### Asking` nested under
   `## Detour rule` in plan-orchestrator.md): this is **defensible, not drift**. Research mode's
   Detour default is "log and continue, never pause" — a different default that Asking sits *above*
   as "a higher tier," so it earns its own top-level section (and says so explicitly: "Asking is a
   higher tier above this rule"). Plan mode's Detour rule already contained the pausing bullet
   ("Requires a user decision → pause and ask") that Asking modifies in place ("Whether that pause can
   also end the run depends on…"), so nesting it under the rule it amends is the more accurate
   structure for that file. Different relationship to each file's own Detour rule, correctly reflected
   by different placement.
7. **Coherence with `prompt.go` — PASS.** `askingRule`'s bar ("Ask only when the answer would change
   what gets built/planned and cannot be settled by reading this repository") is reproduced accurately
   in both files, with plan-orchestrator.md's extension to also check `.aiteam/APP_CONTEXT.md` and
   `$session/RESEARCH_BRIEF.md` being a reasonable Plan-specific elaboration, not a contradiction.
   `askingRule`'s "batch your questions into that one message" instruction and its "move to the next
   phase" sentence are both reflected. `modeMayAsk`'s doc comment (`ask.go:111-136`) confirms Research
   and Plan are the two permitted *final* modes and that `infer` is included only as the pre-resolution
   carrier (AC-A1.7, handled by `inferAskingSeam`, a separate Prompt-text task T-A1.15 not assigned to
   these two files) — so plan-orchestrator.md's "grants it to Plan mode and Research mode only" is
   accurate and not contradicted by `infer` being `true` in `modeMayAsk`'s switch. No contradiction
   found between either file and the prompt text a permitted run actually receives.
8. **Internal contradiction within each file — one pre-existing tension resurfaces, see CR-A-005; one
   prior advisory from the B1 review appears resolved, noted below.**

## Note: CR-A-003 (from the B1 review above) appears resolved by this diff

CR-A-003 flagged that `research-orchestrator.md`'s `## Exit` checklist didn't mirror the new
Choreography step 5 requiring the business summary. This diff's Exit step 3 now explicitly requires
the business summary as "the run's last word," cross-references *The business summary*, *Ordering*
and *Where the summary goes*, and step 4 explicitly ties routing-back to that summary. Re-reading
`## Exit` alone (lines 216-223) now surfaces the business-summary requirement without needing to
notice Choreography step 5 separately. Treating CR-A-003 as **resolved**, not carried forward.

---

## BLOCKING findings

None.

---

## ADVISORY findings

### CR-A-004: research-orchestrator.md's Asking section doesn't state the batching instruction explicitly
**File:** `.claude/agents/research-orchestrator.md:176-187`
**Story:** A1 / T-A1.11
**Category:** Cross-story / Consistency
**Finding:** `plan-orchestrator.md`'s new `### Asking` explicitly says "Batch everything into one
message: a queue of one-question runs costs the owner more attention than a wrong guess would have" —
mirroring `askingRule`'s own "Batch your questions into that one message rather than taking a run per
question." `research-orchestrator.md`'s `## Asking` implies the same outcome via "Asking is stopping.
There is nothing to wait on — you put the questions into your final message and end the run," but
never states the batching rationale or the "one run per question" failure mode by name. A reader of
research-orchestrator.md alone gets the mechanism (one final message) without the reasoning
plan-orchestrator.md gives for why that mechanism matters.
**Suggestion:** Add a sentence to research-orchestrator.md's `## Asking` naming the batching rationale
explicitly, matching plan-orchestrator.md's phrasing, so the two files carry the same justification
and not just the same behavior.

### CR-A-005: Choreography step 4 sits in tension with the newly-sharpened "nothing follows it" framing
**File:** `.claude/agents/research-orchestrator.md:54-55`
**Story:** B2 / T-B2.1
**Category:** Internal contradiction
**Finding:** Choreography step 4 ("Present the brief to the user for confirmation before exiting") is
unchanged by this diff, but step 5 and the new `### Where the summary goes` section now say, much more
emphatically than before, that the business summary is "the last thing the run does; nothing follows
it," and that it is "not a preamble you write before carrying on with more work." Runs in this system
are single-shot/headless (per `notes.md`'s Q7 answer and `askingRule`'s "there is nothing to wait
on") — there is no separate in-run step where the user can confirm the brief before the final message
is written. Read in isolation, step 4 suggests a distinct wait-for-confirmation step preceding step 5;
read against the new "nothing follows it" language, that's not how the run actually works. This
tension predates this diff (step 4's wording is untouched), but the new language makes it more visible
to a reader relying on the Choreography list as authoritative.
**Suggestion:** Either fold step 4 into step 5 (the business summary itself *is* what's presented for
confirmation, in the same final message) or reword step 4 to make clear that "confirmation" happens
via the owner's reply on the next run, not via an in-run pause.

---

## Cross-story summary

This diff builds on B1 correctly: it does not touch, restate, or weaken the four hard rules, the
audience split, or the no-size-limit guarantee, and it closes the one structural gap the B1 review
(CR-A-003) had flagged in `## Exit`. The A1 asking-permission material is substantively identical
across both files (permission source, judgement, honouring a direct instruction to move on, no
Go-side cap, the standing "move to the next phase" line and its non-license framing), adapted
correctly to each file's own Detour-rule shape rather than copy-pasted. It also agrees with what
`askingRule`/`inferAskingSeam`/`headlessRule`/`askingRevokedRule` actually tell a permitted run in
`internal/harness/prompt.go`, and with `modeMayAsk`'s Research/Plan/infer semantics in
`internal/turn/ask.go`. The two advisory findings are a minor cross-file completeness gap
(batching rationale, CR-A-004) and a pre-existing wording tension made more visible by this diff's
sharper "final message" framing (CR-A-005); neither weakens a gated acceptance criterion and neither
blocks the diff.

## Verdict

ADVISORY_ONLY — 2 suggestions noted (CR-A-004, CR-A-005). All checked acceptance criteria
(AC-B1.5, AC-B2.9, AC-A1.14, AC-A1.15, AC-A1.7, AC-A1.21) pass. No Go file modified; `CLAUDE.md`
unmodified. Proceeding — no re-review cycle needed for this docs change.

---

# Code Review — User Story E1 (`plan_brief.md` replaces `IMPLEMENTATION_PLAN.md`)
# Commit: `0ab746d` | Diff: `5850daa..0ab746d` | Branch: `lf/7-headless-mode-should-ask-via-a-comment`
# Reviewed by: go-code-reviewer
# Date: 2026-08-31
# Status: ADVISORY_ONLY

## Scope

`git diff --name-only 5850daa 0ab746d -- '*.go'` is empty — confirmed no Go file is touched
(AC-E1.10). This is a documents-only review: 13 files changed (2 of them this session's own
`MANIFEST.md`/`notes.md` bookkeeping, not in scope), 11 live `.claude/**` and repo-level files
reviewed against Story E1 in `IMPLEMENTATION_PLAN.md` (L993–1153): `.claude/agents/plan-orchestrator.md`,
`.claude/agents/implement-orchestrator.md`, `.claude/agents/go-architect.md`,
`.claude/agents/go-implementation-engineer.md`, `.claude/agents/go-code-reviewer.md`,
`.claude/agents/go-unit-test-engineer.md`, `.claude/agents/go-integration-test-engineer.md`,
`.claude/agents/proxy-product-owner.md`, `.claude/agents/app-context-updater.md`,
`.claude/AGENT_ROSTER.md`, `.claude/skills/shared/task-decomposer.md`. `CLAUDE.md` and
`initialplan.md` do not appear in this diff — they were edited in an earlier commit (run 0010,
before `5850daa`) and are correctly excluded from E1's own diff. No build, no tests, no lint apply
to Markdown.

## Priority 1 — AC-E1.5, the anti-duplication rule

`plan-orchestrator.md`'s new `plan_brief.md` contract table (lines ~63–71) and its format block
(lines ~73–121) mirror the architect's contract table (`IMPLEMENTATION_PLAN.md` L1107–1115)
essentially verbatim, including every "Never contains" column. The anti-duplication rule is stated
once, prominently, ahead of the contract: *"if it is true of exactly one story, it does not belong
in `plan_brief.md`"* (matches the plan's own L1117–1119 wording almost exactly). The Story Index
format is `| ID | Title | Size | Depends On | Sub-issue |` — no Tasks/AC/Architecture-Notes column,
correctly carrying identity and location only.

The per-story sub-issue format block reinforces this from the other side: *"This is where a story's
tasks, acceptance criteria and architecture notes live — and the *only* place they live"* and closes
with *"not aggregated globally, and not duplicated into `plan_brief.md`."* A competent agent
following this template has both the rule stated in prose and a template shape that structurally
has nowhere to put per-story content except the sub-issue — the `plan_brief.md` format block
contains no per-story-shaped section (no free-text area under Shared Decisions, Workflow, or
Cross-Story Constraints large enough to informally house a story's task list without visibly
breaking the section's own stated purpose). I judge this contract sound: it invites compliance, not
drift. Only a mild suggestion below (CR-A-006).

## Priority 2 — the transitional-fallback judgment call

Traced all 17 occurrences of "transitional" across `implement-orchestrator.md` (8),
`go-code-reviewer.md` (3), `go-implementation-engineer.md` (2), `go-unit-test-engineer.md` (2),
`app-context-updater.md` (1), `go-integration-test-engineer.md` (1), plus a structurally distinct
one in `plan-orchestrator.md`'s Exit step 2 (sub-issue creation deferred to C1, not an
`IMPLEMENTATION_PLAN.md`-read fallback).

**Precedence is unambiguous everywhere.** Every instance reads as "primary artifact, or — in a
transitional session — fallback artifact." None inverts this or suggests checking
`IMPLEMENTATION_PLAN.md` first. The one place that actually branches on file presence
(`implement-orchestrator.md`'s `## Inputs`, first bullet) states it explicitly: *"If `plan_brief.md`
is absent and `IMPLEMENTATION_PLAN.md` is present, treat that single file as the whole plan."*
`plan_brief.md` wins whenever it exists — stated once, in the one place that needs to branch;
every other file's mention is documentation of what it might receive, not independent branching
logic. That centralization is good design: only one file (`implement-orchestrator.md`) has to get
the conditional right.

**Each instance is additive and removable without semantic re-reasoning** — confirmed by reading
every occurrence in context. Most are parenthetical asides — `(or, in a transitional session,
`IMPLEMENTATION_PLAN.md`)` — appended to a sentence that reads correctly with the parenthetical
simply deleted. A few are full standalone sentences (`go-code-reviewer.md:26`,
`implement-orchestrator.md:16`, and the `implement-orchestrator.md` Inputs-bullet paragraph),
equally clean to delete wholesale. Nowhere does removing the fallback require rewriting the
surrounding clause or making a new judgment call about what should replace it — C1 does not have to
re-reason about any of these files, only excise a clause.

**However, the wording is not one uniform template**, which is the one thing I'd push back on in the
notes.md rationale's "one-sentence removal per file" framing. I count three distinct phrasings in
use:
1. `(or, in a transitional session, `X`)` — the majority (11 of 17).
2. `..., or its section of `IMPLEMENTATION_PLAN.md` in a transitional session)` — reordered, embedded
   mid-parenthetical rather than appended (`go-implementation-engineer.md:31`,
   `go-integration-test-engineer.md:7`).
3. Standalone sentences with their own phrasing (`go-code-reviewer.md:26`,
   `implement-orchestrator.md:16` and the `## Inputs` paragraph at `implement-orchestrator.md:12`).

A single find-and-replace regex will not catch all three shapes; C1's `T-C1.13` will need at least
two or three passes (or a manual per-file check) rather than one. This does not block testing —
every instance is still a clean, self-contained deletion once found — so I'm filing it as advisory,
not blocking. See CR-A-007.

## Priority 3 — AC-E1.4, the invocation-content contract

Confirmed both sides of this contract agree, file by file:

- `implement-orchestrator.md`'s `## Inputs` states it fetches each story's sub-issue *up front*, once,
  and that "every downstream agent below receives its story's content **in its invocation**, never a
  forge link it must fetch itself (AC-E1.4)."
- Every downstream agent's "what you receive" language matches: `go-architect.md` ("Your primary
  input"), `go-implementation-engineer.md` ("passed to you in your invocation, not a file you open
  yourself"), `go-code-reviewer.md` ("passed inline by the orchestrator — not files you fetch
  yourself"), `go-unit-test-engineer.md` and `go-integration-test-engineer.md` (content "passed to
  you in your invocation" / "in its invocation"). No downstream file retains language implying it
  reads `plan_brief.md`, a sub-issue, or a forge link itself.

One nuance worth naming, not a mismatch: `implement-orchestrator.md`'s Phase 3 line ("Pass each
agent `plan_brief.md` plus every story's sub-issue content ... and let it select its own stories")
hands **every** story's sub-issue content to both `go-unit-test-engineer` and
`go-integration-test-engineer` uniformly, then relies on the agent to self-filter — appropriate for
`go-unit-test-engineer` (which covers all stories), but for `go-integration-test-engineer` (which
"covers only the stories flagged in Phase 2") this means handing over sub-issue content for stories
it will discard. This exact pattern existed before E1 with the single-file `IMPLEMENTATION_PLAN.md`
(where "pass the full file" was free — one file, no assembly cost); under the new model the
orchestrator explicitly assembles per-story content, so it is now cheap and more precise to filter
to the Phase-2-flagged subset before invoking `go-integration-test-engineer`. Not a contract
violation — AC-E1.4 only requires content to arrive in the invocation, and it does — but an easy
efficiency win. See CR-A-008.

## Priority 4 — cross-file consistency

Grepped every touched file for `sub-issue`, `Story Index`, and `plan_brief.md` (`sub-issue` never
appears as `sub issue` or `story issue` anywhere — terminology is uniform). Counted 11 files using
the vocabulary consistently: `plan_brief.md` is always the shared-context artifact, `Story Index` is
always the discovery table, `sub-issue` is always the per-story content home. No file invents a
fourth artifact name or a competing term for any of these.

One drift found, pre-existing rather than introduced by this diff — see CR-A-009 below.

## Priority 5 — AC-E1.1

`grep -n "IMPLEMENTATION_PLAN" .claude/agents/plan-orchestrator.md` returns exactly one line, `:177`:
*"`.aiteam/` session archives are never rewritten when an artifact format changes ... A session
already written with `IMPLEMENTATION_PLAN.md` keeps that file exactly as it is..."* — this is the
T-E1.10 statement, correct and required. No `IMPLEMENTATION_PLAN.md` format block remains anywhere
in the file; the entire old format block (feature/branch/date header through the per-story sections)
was replaced by the `plan_brief.md` format block and the separate per-story sub-issue format block.
**AC-E1.1 holds.**

## Repo-wide AC-E1.2 re-check

`grep -rln "IMPLEMENTATION_PLAN" --include="*.md" .` (excluding `.aiteam/`) returns exactly:
`CLAUDE.md`, `initialplan.md`, and the 8 `.claude/agents/*.md` files carrying the transitional
fallback reviewed above under Priority 2. No other live file surfaced. This matches the
already-verified state and confirms no new permanent current-artifact claim was introduced.
**AC-E1.2 continues to hold** (scoped, as instructed, to "after E1 + C1" — these transitional
mentions are expected at this point, not violations).

## ADVISORY findings

### CR-A-006: Sub-issue template has no inline anti-duplication reminder
**File:** `.claude/agents/plan-orchestrator.md:~100-121` (per-story sub-issue format block)
**Story:** E1 / T-E1.1
**Category:** Consistency / defense-in-depth
**Finding:** The anti-duplication rule is stated once in prose above the two format blocks, and the
prose framing of each template ("and the *only* place they live" / "Never contains: ... any content
at all") is strong. But neither fenced template itself carries an inline bracketed reminder (e.g.
`[do not restate a plan_brief.md Shared Decision here]`) the way other templates in this codebase
(e.g. go-architect's hint block) use bracketed guidance text throughout.
**Suggestion:** Optional: add a one-line bracketed reminder inside the `## Architecture Notes`
placeholder of the sub-issue template, so an agent skimming only the fenced block (not the prose
above it) still sees the rule. Not required — the current prose is already explicit enough that I
would not block on this.

### CR-A-007: Transitional-fallback wording uses three distinct templates, not one
**File:** `.claude/agents/implement-orchestrator.md`, `go-code-reviewer.md`,
`go-implementation-engineer.md`, `go-unit-test-engineer.md`, `go-integration-test-engineer.md`,
`app-context-updater.md` (17 occurrences total)
**Story:** E1 / T-E1.4, T-E1.5, T-E1.6, T-E1.7
**Category:** Cross-story / maintainability
**Finding:** See Priority 2 analysis above. Three distinct phrasing shapes are used for what is
conceptually the same fallback clause: `(or, in a transitional session, X)`, a reordered variant
embedded mid-parenthetical (`go-implementation-engineer.md:31`, `go-integration-test-engineer.md:7`),
and freestanding sentences (`go-code-reviewer.md:26`, `implement-orchestrator.md:16` and its
`## Inputs` paragraph). Each instance is individually self-contained and mechanically deletable, but
a single regex/script pass will not catch all three shapes — the notes.md rationale's framing
("a one-sentence removal per file") slightly understates the mechanical work: it is closer to "one
clause removal per occurrence, using at least three matcher patterns," across 17 occurrences in 6
files.
**Suggestion:** Not required before testing proceeds — this is a note for whoever executes
`T-C1.13`. Either standardize on the single dominant `(or, in a transitional session, X)` phrasing
now (a small follow-up edit, since 11 of 17 already use it), or leave as-is and have `T-C1.13` budget
for a per-file check rather than a single global find-and-replace.

### CR-A-008: `go-integration-test-engineer` receives all stories' sub-issue content, not just the flagged subset
**File:** `.claude/agents/implement-orchestrator.md:144`
**Story:** E1 / T-E1.4 (Phase 3 invocation)
**Category:** Performance / precision
**Finding:** *"Pass each agent `$session/plan_brief.md` plus every story's sub-issue content ... and
let it select its own stories: `go-unit-test-engineer` covers all stories,
`go-integration-test-engineer` covers only the stories flagged in Phase 2."* Under the new
per-story-sub-issue model the orchestrator explicitly assembles what it hands over (unlike the old
single-file `IMPLEMENTATION_PLAN.md`, where "pass the whole file" was the only option). Handing
`go-integration-test-engineer` every story's content when it will use only a Phase-2-flagged subset
is unnecessary context volume, not a correctness bug — AC-E1.4 is still satisfied.
**Suggestion:** When Phase 2 has already identified which stories touch real dependencies, pass
`go-integration-test-engineer` only those stories' sub-issue content instead of the full set. Small,
non-blocking efficiency improvement.

### CR-A-009: `go-architect.md`'s own invocation description doesn't match how it's actually invoked
**File:** `.claude/agents/go-architect.md:1` (frontmatter trigger conditions), `:57`
(Required Reading Process, item 5)
**Story:** E1 / T-E1.5 — pre-existing, not newly introduced by this diff
**Category:** Cross-story / documentation accuracy
**Finding:** `go-architect.md` item 5 now reads: *"`plan_brief.md` plus the per-story sub-issues -
Your primary input. You will annotate each story's tasks with architectural guidance that folds into
its sub-issue's Architecture Notes..."* — implying the architect reads (and possibly writes into) an
already-existing `plan_brief.md` and its sub-issues. Its frontmatter trigger examples similarly
describe being invoked "After a plan_brief.md and its per-story sub-issues have been drafted."
But per `plan-orchestrator.md`'s actual choreography (`## Choreography`, step 2, "Architect review"),
`go-architect` is invoked with only `$session/feature-spec.md` and the platform-constraints skill
path — **before** `plan_brief.md` or any sub-issue exists — and "returns findings inline — no files
written." The plan-orchestrator itself does the folding into `plan_brief.md` and the sub-issues in
step 3 ("Synthesise"), combining "RE spec + architect findings."
I traced this back through the diff: the same shape of claim existed in the pre-E1 text too
("`IMPLEMENTATION_PLAN.md` - Your primary input. You will annotate tasks in this plan..." and
frontmatter examples describing "Here's the `IMPLEMENTATION_PLAN.md` file with all the user stories
and tasks" as already existing) — so this is **not a regression introduced by this diff**; the
implementer performed a faithful rename of an already-inconsistent description rather than
introducing new drift. Flagging per the review brief's instruction to surface cross-file drift
regardless of origin.
**Suggestion:** Not blocking — no behavioral contract is broken (the actual invocation, in
`plan-orchestrator.md`, is correct and unambiguous, and that's the file every other agent's
"what I receive" language correctly agrees with). Worth a follow-up docs fix to `go-architect.md`'s
own self-description to say it receives `feature-spec.md` and returns findings inline for the
orchestrator to fold into `plan_brief.md`/sub-issues — but that's pre-existing debt, not new debt
from E1, so I'm not routing it back to this story's engineer.

## Cross-story summary

This is a careful, faithful execution of a 13-file, 13-task rename-and-restructure story. The
`plan_brief.md` contract table and format block reproduce the architect's specification field-for-
field; the anti-duplication rule (AC-E1.5) is stated once and reinforced structurally in both
templates; every downstream agent's stated inputs match what `implement-orchestrator.md` promises to
hand it (AC-E1.4); terminology (`plan_brief.md`, `sub-issue`, `Story Index`) is uniform across all 11
touched files; `plan-orchestrator.md` carries no `IMPLEMENTATION_PLAN.md` format block (AC-E1.1); the
repo-wide grep confirms no new permanent current-artifact claim exists outside the expected
transitional set (AC-E1.2). The transitional-fallback judgment call is sound in substance — every
instance is additive, unambiguous about precedence, and removable without re-reasoning — but its
wording is inconsistent enough across three phrasing templates that `T-C1.13` should expect a
per-occurrence pass rather than a single mechanical script. The one real drift found
(`go-architect.md`'s self-description vs. its actual invocation contract) predates this diff and
was carried through by a faithful rename rather than introduced by it.

## Verdict

ADVISORY_ONLY — 4 suggestions noted (CR-A-006 through CR-A-009), none blocking. All checked
acceptance criteria (AC-E1.1, AC-E1.2, AC-E1.4, AC-E1.5, AC-E1.10) pass; AC-E1.6, AC-E1.7, AC-E1.9
reconfirmed as already verified and unchanged. No Go file modified; `CLAUDE.md`'s Implement gate
untouched. Proceeding — no re-review cycle needed for this docs change.
