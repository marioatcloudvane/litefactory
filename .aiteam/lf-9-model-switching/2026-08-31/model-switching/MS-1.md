# MS-1 — Behavioural probe of `--model` and `--effort`

> As a litefactory maintainer, I want the composition and failure behaviour of `--model` and `--effort` confirmed hands-on against the installed `claude`, so that no implementation story is built on an unverified assumption about an external binary.

Delivers a third external-behaviour document beside the two existing probe write-ups, naming `claude 2.1.251`, with a probe/contingency table — and the go/no-go verdict that unblocks every other story.

## Why This Matters

Everything else in this feature depends on one thing being true: that the two switches we want to
turn actually work the way we think they do when litefactory turns them. We have confirmed the
switches exist and what values they accept. We have confirmed nothing about how they behave in the
specific way litefactory calls the tool — and this project has been bitten before by that exact gap,
when the tool quietly dropped a requested capability and reported success anyway.

So this story is a controlled experiment, not paperwork. It is worth doing first because three of its
questions can change the shape of the work: one would stop the feature and send it back to you, one
would mean re-asking you about a decision you already made, and one would force a different design
for how the choice is remembered across a conversation. Finding any of those out after the code is
written costs far more than finding it out now.

What is genuinely unknown here is behaviour we cannot make up: we have to run it and watch. The
answer may be boring. It is cheap to establish and expensive to assume.

## Architecture Notes

- **This is a gate, not a document task.** The final task's verdict is what unblocks MS-2 through
  MS-7. A probe row that invalidates the argv delivery shape stops the feature and goes back to the
  owner — do not design around it.
- **Two rows are stop conditions and one is a redesign.** AC-MS-1.2 failing stops the feature.
  AC-MS-1.5 turning out to be a hard error rather than a silent no-op stops the allow-and-report
  ruling and needs the owner. AC-MS-1.6 turning out to refuse a *changed* model on resume forces the
  model to be pinned per **segment** — recorded when the segment is created, then passed unchanged,
  ignoring later relabels within it — which is an MS-4 redesign and must be known before MS-4 starts.
  Write those three contingencies out in full; they are the reason the table exists.
- **Platform: no committed test may spawn a real `claude`** (`.aiteam/APP_CONTEXT.md:405-424`). Every
  probe runs read-only in a scratch directory outside the repo. Nothing from this story reaches a
  `_test.go` file.
- **Risk:** the two existing probe documents describe 2.1.246 and 2.1.247 and are already
  version-behind the installed 2.1.251. Record that here rather than silently inheriting their
  claims — MS-7 needs it.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-MS1.1 | Create `MODEL_EFFORT_PROBE.md` in this session directory: exact `claude` version header, re-verification checklist, and the probe/contingency table shape copied from `.aiteam/main/2026-08-30/tool-enforcement/IMPLEMENTATION_PLAN.md:58-91`. Match the two existing probe documents' register. | infra | — | S |
| T-MS1.2 | Probe P1 — litefactory's **full** argv (`-p --output-format stream-json --verbose --session-id … --tools … --settings … --strict-mcp-config`) with `--model` added, with `--effort` added, and with both. Record exit code and whether the run completed. | infra | T-MS1.1 | M |
| T-MS1.3 | Probe P2 — does the init event's `model` field reflect the value passed to `--model`, verbatim, for one alias and one full model ID. | infra | T-MS1.2 | S |
| T-MS1.4 | Probe P3 — bogus model value (exit code, stderr, whether an init event arrived, local failure vs API round-trip); bogus effort value; valid effort on a model carrying no `effort` capability. | infra | T-MS1.2 | S |
| T-MS1.5 | Probe P4 — `--model` alongside `--resume`, including whether a resumed conversation honours a *changed* model. | infra | T-MS1.2 | S |
| T-MS1.6 | Probe P5 — `forward_anthropic_env: true` puts `ANTHROPIC_MODEL` in the child environment and `--model` is also passed: record which wins. | infra | T-MS1.2 | S |
| T-MS1.7 | Fill the contingency column for every row, record the gate verdict, and sign off that every probe ran read-only in a scratch directory outside the repo with nothing committed that spawns `claude`. | infra | T-MS1.2–T-MS1.6 | S |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-MS-1.1 | A document exists at `.aiteam/lf-9-model-switching/2026-08-31/model-switching/MODEL_EFFORT_PROBE.md`, states the exact `claude` version probed, and carries a re-verification checklist, matching the two existing probe documents' shape. |
| AC-MS-1.2 | It records the result of running litefactory's **full argv shape** with `--model` added, separately with `--effort` added, and with both: exit code and whether the run completed. |
| AC-MS-1.3 | It records whether the init event's `model` field reflects the value passed to `--model`, verbatim, for at least one alias and one full model ID. |
| AC-MS-1.4 | It records the observed behaviour of a **bogus model value**: exit code, stderr, whether an init event arrived, and whether the failure is local or a round-trip to the API. |
| AC-MS-1.5 | It records the observed behaviour of a **bogus effort value** (expected: warn on stderr and continue) and of a **valid effort on a model with no `effort` capability** (expected: silent no-op). |
| AC-MS-1.6 | It records `--model` alongside `--resume`, including whether a resumed conversation honours a *changed* model. |
| AC-MS-1.7 | It records which wins when `forward_anthropic_env: true` puts `ANTHROPIC_MODEL` in the child environment and `--model` is also passed. |
| AC-MS-1.8 | Every probe was run read-only in a scratch directory outside the repo. **No test that spawns a real `claude` is committed.** |
| AC-MS-1.9 | Each probe row carries a contingency: what the design does if that probe fails. A probe whose result invalidates the argv delivery shape (AC-MS-1.2) stops the feature and goes back to the owner. |
