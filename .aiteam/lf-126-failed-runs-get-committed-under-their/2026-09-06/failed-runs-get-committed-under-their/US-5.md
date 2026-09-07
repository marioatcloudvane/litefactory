# US-5 — The prompt states the predecessor failed and disowns its claims

> As a repository owner, I want the resumed run's prompt to state that the
> previous run failed and that its unverified claims are not this run's
> accomplishments, so that the next report describes what this run did rather
> than re-announcing work that never landed.

Emits a corrective note in the resume header, using the fields US-4
populates, telling the resumed run that its predecessor failed and that its
claims should not be repeated as done work.

## Why This Matters

This is the half of the fix a reader actually notices: the sentence that
stops the AI from opening its next report with "as before, I completed X"
when X never actually landed. It matters because a false "done" claim
compounds — it can get repeated into commit messages, sub-issue write-ups, or
a human's mental model of project state, and each repetition makes it more
expensive to unwind. The genuinely hard part isn't the sentence itself but
making sure it can't be hijacked: the previous run's failure reason is text an
AI model wrote, and it has to reach the next run as a quoted fact, not get
misread as a new instruction from litefactory itself.

## Architecture Notes

- Depends on US-4 landing first — the fields this story renders must exist.
  Can be written against the field definitions in parallel with US-4, but its
  end-to-end tests need US-4's plumbing in place. See plan_brief Workflow.
- Add the corrective-note constant beside `askingRule` / `askingRevokedRule`
  in `internal/harness/prompt.go`, in litefactory's own voice.
- Emit it in `writeResumeHeader` after the `- Feature slug:` line and before
  the infer-guidance block, gated on `PreviousFailed`. This is the only
  insertion point that keeps both existing conditional blocks (infer-guidance,
  ask-permission) byte-for-byte unchanged when the predecessor succeeded —
  insert anywhere else and that guarantee breaks.
- Do not touch the `"- Mode: %s\n"` literal — this repository's own
  `CLAUDE.md` `## Classify` step trusts that exact string, and no test in this
  codebase catches it drifting. The note is added below that line, so this is
  a watch-out, not a task; if implementation finds a reason to touch that
  literal, that is an escalation, not a judgement call.
- **Security guardrail, binding, not optional:** `PreviousReason` must not be
  interpolated raw into the header. It is sourced from `runs.error`, which on
  one classification path is agent-authored text — up to 200 bytes,
  newline-stripped and length-capped by the existing `excerpt` helper, but
  `excerpt` explicitly documents itself as bounding length, not meaning. Pick
  one of:
  - **Preferred**: render it inside the same `<note>` framing every other
    quoted body already uses, passed through `neutraliseNoteTags` and
    `capText`.
  - **Acceptable**: keep it in litefactory's own voice but pass it through
    `neutraliseNoteTags` at minimum, plus a test asserting a `PreviousReason`
    containing `</note>`, a forged `- Mode:` line, or a forged
    `# litefactory run` heading cannot forge header structure.
  State in the task notes which option was chosen.
- `runs.error` reaching the prompt is the one genuinely new cross-run data
  flow in this whole feature — data crossing a run boundary that didn't cross
  it before — which is why the guardrail above is binding rather than
  advisory.
- `BuildPrompt` must stay pure (no clock, filesystem, or env access) with the
  new branch added.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-5.1 | Add the corrective-note constant beside `askingRule` / `askingRevokedRule` in `internal/harness/prompt.go`, stating the predecessor failed and its unverified claims are not this run's accomplishments | logic | US-4's T-4.3 | S |
| T-5.2 | Emit the note in `writeResumeHeader` after the `- Feature slug:` line and before the infer-guidance block, gated on `PreviousFailed`, without touching the `"- Mode: %s\n"` literal | logic | T-5.1 | M |
| T-5.3 | Apply the security guardrail to `PreviousReason` — `<note>` framing plus `neutraliseNoteTags`/`capText`, or at minimum `neutraliseNoteTags` — and document which was chosen and why | logic | T-5.2 | M |
| T-5.4 | Byte-for-byte test that a resume prompt with a succeeded predecessor is unchanged from today, across the infer-guidance and ask-permission combinations | test | T-5.2 | M |
| T-5.5 | Byte-for-byte test that the cold-start prompt is unchanged, and that a cold rerun after a resume refusal carries the new fields inertly | test | T-5.2 | S |
| T-5.6 | Injection test: a `PreviousReason` containing `</note>`, a forged `- Mode:` line, and a forged `# litefactory run` heading cannot forge header structure | test | T-5.3 | M |
| T-5.7 | Test that `BuildPrompt` stays pure with the new branch, and that the note appears in the persisted run-metadata prompt | test | T-5.2 | S |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-5.1 | A resumed run whose predecessor failed receives a prompt containing an explicit statement that the previous run failed and that its unverified claims are not this run's accomplishments. |
| AC-5.2 | The corrective note names the predecessor's reason when `runs.error` is non-empty, and omits the clause cleanly when it is empty. |
| AC-5.3 | A resumed run whose predecessor succeeded receives a resume header byte-for-byte identical to the one produced before this change. |
| AC-5.4 | A cold-start run's prompt is byte-for-byte identical to the one produced before this change. |
