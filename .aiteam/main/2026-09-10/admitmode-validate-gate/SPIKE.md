# Spike — admitmode-validate-gate

2026-09-10. Issue #207 could not reach Validate: the owner set `Mode: Validate`,
litefactory overwrote the label and ran Implement instead. Fixing that exposed
three more defects stacked behind it, each only visible once the one in front
was gone. Four fixes, all verified failing-then-passing.

## What was done

**1. `internal/daemon/mode.go` — an explicit owner command lost to the managed pin.**
`case commanded:` moved above `case featureErr == nil && feature.State != "planning":`.
Go takes the first matching case, so on any managed feature outside planning every
slash mode command was discarded and `confirmModeSelection` then rewrote the label
to match. `agent.ModeValidate` was reachable only on unmanaged sessions — that is,
never on a feature with engineering to validate. The turn layer was already
complete: `prepareManagedScope` admits managed Validate for an explicit `/validate`
on a `WorkflowSeparated` feature in state `implementing`, and a read-only `validate`
tool profile exists. Only the switch order was missing.
Test: `TestManagedValidateCommandOutranksImplementPin`.

**2. `internal/harness/validation.go` — the Validate prompt inlined every file body.**
`StorySnapshotFile.Content []byte` is base64 under `encoding/json`, so the whole
retained snapshot went into the prompt. Run 17: 12 stories, 180 files, 2.69MB of
base64 in a 2.81MB prompt — 96% — and a 400 `prompt_too_long` before any review.
0.78MB of that was duplication; only 126 files were unique by sha256.
`withholdSnapshotContent` drops the bodies and keeps path, sha256 and the deletion
flag. Nothing is lost: the reviewer holds Read/Glob/Grep on a worktree at the
reviewed commit, and `SnapshotDigest` is computed over exactly those three fields,
never over content. Prompt 2,811,116 → 125,625 bytes.
Test: `TestValidationPromptWithholdsSnapshotBodies`.

**3. `internal/harness/runner.go` — an unusable resumed conversation was not recoverable.**
`resumeRefused` requires `Model == ""` (no init event), so it did not cover a
conversation that loads and is then declined for size. `resumeUnusable` returns the
same `ErrSessionNotResumable`, which the existing cold-rerun path in `runClaude`
already answers. It is decided structurally — at most one turn and a reported cost
of exactly zero — never by matching "Prompt is too long", which is prose from
another project's binary.
Test: `TestResumeUnusableRerunsColdOnlyWhenNothingHappened`.

**4. `internal/agent/validation.go` + `internal/turn/validation.go` — a completed assessment was discarded.**
Run 20 reviewed all twelve stories, 24 turns, $1.32, and litefactory rejected the
result on `invalid character 'B' looking for beginning of value`: the reviewer led
with prose and fenced the object. `validationEnvelope` takes the first `{` to the
last `}` before parsing, which spans a fence and a preamble alike; every check that
matters is applied to the object and is unchanged. Separately, the rejection path
overwrote `ResultText` with its own error, so the report read "No output was
captured for this run" — it now keeps the reviewer's text and puts the error in
`Diagnostic`. Verified against run 20's real 37,324-byte message: 37,128 bytes
extract clean, all 12 stories.
Test: `TestValidationAcceptsAWrappedObjectButNotASecondOne`.

## What was found

- The four defects are independent and each masked the next. Nothing short of
  running Validate end to end would have found them in one pass.
- Cost of the misroutes and failures on #207: runs 15-20 spent about $2.5 for one
  usable assessment. The earlier Implement history is worse — see below.
- Validate's assessment from run 20: **53 criteria satisfied, 0 unmet, 2 unverified;
  35 reviews satisfied, 1 unmet.** The only real finding is US-012's AC-003
  (authentic dual-harness external evidence), which `docs/agent-evidence-capture.md`
  itself records as factually unverified, and the US-012 plan review that follows
  from it. US-011's AC-004 "unverified" is a note that AC-004 is not among that
  story's frozen criteria, not a defect.
- Token profile of the whole feature, from the meter: $237 across runs 1-14, of
  which cache_read is 75%. `gpt-6-astra` is 5x `claude-sonnet-5` on every
  dimension; codex ran $0.69/min against claude's $0.12/min for the same work.
  Codex's orchestrator spent 102 `wait` calls and 100 narration messages on run 12
  producing four commands. 85% of the prompt's comment history is litefactory's own
  status reports.

## Decisions worth remembering

- The managed pin answers "what does a *plain* comment continue in", and nothing
  more. Persistence across ordinary replies never meant outranking an explicit
  owner transition.
- Strictness belongs to the assessment object, not to the envelope a model wraps
  it in. Rejecting the wrapper cost a complete review and bought nothing that the
  identity checks do not already guarantee.
- A failed run must not have its evidence overwritten by the reason it failed.

## Not done

- **Segment rotation.** `PathColdAfterResumeRefused` reuses the session id from
  `naming.SessionRef(ref, ordinal)`, so a "cold" start fails with `Session ID … is
  already in use` once a conversation file exists; it only worked on run 14 because
  no file existed yet. Session 165 has used segment 1 since 2026-09-09. The rotation
  mechanism exists (`internal/turn/delivery.go:353`) and is used for bundle changes.
  Unblocked here by archiving the 7.1MB conversation to
  `8280ac66-….jsonl.oversized-20260910`; the code fix touches segment identity
  mid-run and wants a design.
- **`Diagnostic` from the claude adapter.** Fix 4 populates it on one path. The
  adapter still never sets it, so a failed run's headline is the exit status.
- **Resuming across a mode change.** A Validate run inherits the Implement
  conversation. It should not.
- **A prompt-size pre-flight guard.** Deliberately skipped: a wrong threshold
  breaks runs that work today.
- **The label-edit half of the mode transition.** A canonical label edit with no
  slash command still loses to the pin. Telling an owner edit from the label
  litefactory applied at promotion needs the last applied label, and
  `internal/store/modes.go` has no accessor for it.

The owner declined issues for all of these: the validation flow is to be reworked
whole.

## Recommendation

Comment `/validate` once more so the factory retains the assessment and projects
acceptance — run 20's review was never persisted, so no acceptance decision exists
for any story. **US-012 cannot be accepted** on that assessment: one blocking
criterion unverified and its plan review unmet. It needs either the missing
evidence or an explicit owner waiver. The other eleven stories are clean.
