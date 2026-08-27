# Drift Report — tool-permissions
# Branch: lf/tool-permissions | Date: 2026-08-27

Compares what was delivered against `IMPLEMENTATION_PLAN.md`, from the task reports collected
across Phases 1-4.

## Delivered as planned

- **T-1** — `ToolProfile` type; `tool_profiles`/`tool_ceiling` decode; effective-ceiling resolution
  in `Load`; `TodoWrite` dropped from `defaultTools()`; commented-only example in `template.go`.
- **T-2** — key-parameterised `validateTools`/`validateAllowedTools`; ceiling ⊇ `claude.tools`;
  profile-key regex; per-profile ceiling containment; `allowed_tools` ⊆ profile `tools`;
  `validateNoForbiddenFlag` extended; sorted iteration.
- **T-3** — 22 test functions plus subtests, one per AC-1.1..AC-1.12 plus the fail-open cases.
  `internal/config` at 94.8% statement coverage.
- **T-4** — labels through the single `(*issuePayload).issue()` mapper to `github.Issue.Labels` and
  `turn.Issue.Labels`, both fetch paths, no extra API call. `surface_test.go` needed no change and
  was not weakened (it walks type declarations, not struct fields).
- **T-5** — `Request.{Tools, AllowedTools, ToolProfile}`; `Runner.ceiling`; `args` selection
  preserving the empty-`--allowedTools` guard and unconditional `--tools`; ceiling check in
  `Request.validate`; `#nosec` justification rewritten; `"claude started"` attributes.
- **T-6** — resolver at the head of `d.run`, case-insensitive prefix scan, exact-key lookup, the
  refusal decisions, copy-on-read, resolved lists onto `turn.Request` → `prepare` →
  `harness.Request`. `internal/turn/harvest.go` unmodified, as required by AC-2.12.
- **T-7** — resolver decision table (15 cases); argv contains no label-derived string; ceiling
  rejection; no-label argv byte-identical; resume guard still green; cold-rerun carries the profile.
- **T-8** — three flat `ReplyKind` values and `Reply.Profile`, regex re-applied inside
  `internal/report`; both `ReplyRequest.validate` and `ReportReply` guards branched on the refusal
  first; daemon calls `ReportReply` instead of `d.run`.
- **T-9** — each refusal refuses, replies, spawns nothing, advances the watermark; no raw label text
  in any rendered body; ambiguous names neither label; label-present-profiles-absent refuses.
- **T-10** — the E-1 correction (`turn.Result` and `report.Run` both gain the two fields, rendered
  from `metadataList`); `**Tools:**` bullet after `Model` inside `secFixed`; `harness.Meta` fields;
  `cmd_logs.go` fields.
- **T-11** — report line for granted / requested≠resolved / nil-result runs; `logs` output;
  redaction verified to leave tool lists untouched.

All eight planning decisions D-1..D-8 were implemented as decided, including the two the plan
flagged for possible owner reversal (D-3, keep per-profile `allowed_tools`; D-5,
label-present-but-no-profiles refuses).

## Added scope

Small additions, none of which change the feature's shape. Each has a stated reason.

- **`internal/config/secretscan.go` — sorted map iteration in `walkStrings`.** `tool_profiles` is
  the package's first map; without it, two profiles containing credential-shaped strings would
  report in random order. Same determinism concern as AC-1.11. ~5 lines.
- **`toolDecision.MiscasedPrefix` plus a log attribute.** Keeps the two malformed causes — bad
  prefix case vs bad value — distinguishable in logs. A `bool`, structurally incapable of carrying
  label text.
- **An INFO log line recording the ceiling in force before `Execute`.** Otherwise `Daemon.toolCeiling`
  would be a dead field (D-2 puts enforcement at boot and at `Request.validate`). A run that then
  hangs still leaves a record of the boundary that applied to its per-tick label snapshot.
- **`ReplyKind.RefusesTools()`** — one exported helper so `internal/turn` can tell the two reply
  families apart without owning `internal/report`'s vocabulary.
- **Test fixture repair:** `internal/daemon/fixture_test.go`'s `newDaemon` silently dropped
  `ToolProfiles`/`ToolCeiling` overrides. Test-only, pre-existing.

## Dropped or deferred

- **The OPTIONAL section** (`--setting-sources`/`--bare`/`dontAsk`/sandbox) — skipped wholesale, per
  D-8 and explicit instruction. It remains the real fix for the SD-2 ambient-rule leak and belongs
  in handover.
- **T-12 was delivered as a probe document, not a committed test.** See "Architectural deviations".

## Architectural deviations

- **T-12's form.** The plan says "Integration probe … `CLAUDE_CLI_CONTRACT.md` five-probe style".
  That document's own method is a read-only probe in a scratch directory outside the repo, written
  up as a document — not a Go test. Delivered that way: `TOOL_SURFACE_PROBE.md`, three probes on
  `claude 2.1.247`. A committed test spawning a real `claude` inside `go test ./...` would be slow,
  quota-consuming and flaky, and the repository has no build-tag or env gate for one. AC-2.14 is
  satisfied by observation: `WebFetch`, absent from `claude.tools`, appears in the resolved init
  event when a profile grants it.
- **AC-2.4's test lives in `internal/turn`, not `internal/daemon`.** The daemon fixture fakes
  `turn.Execute` entirely and so never builds real argv — it structurally cannot exercise "no label
  text in argv". The load-bearing security test was placed one layer down, driving a real
  `*harness.Runner` from the `turn.Request` the resolver would have produced.
- **AC-2.7 required no code change.** The existing path already closes it: an `ErrInvalidRequest`
  from `Run` becomes `DispositionRan`/`OutcomeFailed`, is reportable, and consumes the comment. This
  was verified empirically with a stub runner rather than assumed, and the reasoning recorded on
  `turn.Request.Tools`' doc comment because `harvest.go` had to stay unmodified.
- **T-8 was committed before T-6**, inverting the planned order. T-6 references the reply API, so a
  T-6-first commit would not have compiled, and a T-6 that avoided it would have been a commit where
  refusals are computed and discarded.

## Gap the plan did not cover

- **A tools refusal on the issue-body path.** D-1 puts resolution at the head of `d.run`, which is
  also reached from `runIssueBody` with `CommentID: 0`, and `ReplyRequest.validate` requires a
  positive comment id. Decided during Implement (I-1 in `notes.md`): refuse, log, post nothing.
  Raised with the owner; **not** confirmed by them. Reversal is local.

## Verdict

**CLEAN** — delivered matches plan within acceptable micro-decisions.

Two caveats the owner should read rather than skip: the issue-body refusal gap above was decided
without their answer, and the D-6 case-sensitivity rule was implemented wrongly first on the
orchestrator's incorrect instruction and corrected in `6dd9d9b` (C-1 in `notes.md`).
