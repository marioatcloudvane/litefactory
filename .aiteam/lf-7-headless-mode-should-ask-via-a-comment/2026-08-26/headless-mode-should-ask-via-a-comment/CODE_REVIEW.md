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
