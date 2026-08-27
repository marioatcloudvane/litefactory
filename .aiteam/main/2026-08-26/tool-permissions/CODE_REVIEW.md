# Code Review
# Feature: tool-permissions
# Reviewed by: go-code-reviewer
# Date: 2026-08-27
# Status: CLEAN

---

## Review method

Read the full non-test diff `2c2a874..HEAD` (`internal/config/{config,policy,validate,template,secretscan}.go`,
`internal/github/issues.go`, `internal/harness/{runner,meta}.go`, `internal/turn/{execute,prepare,report,result}.go`,
`internal/daemon/{daemon,setup,tick,tools}.go`, `internal/report/{reply,run}.go`, `internal/cli/cmd_logs.go`),
cross-referenced every hunk against `IMPLEMENTATION_PLAN.md`'s invariants (C1, C2, C3, C9), the eight planning
decisions (D-1..D-8), and all four ACs tables. Ran `go build ./...`, `go vet ./...`, `gofmt -l .`, and
`go test -race ./internal/{config,daemon,harness,turn,report,github,cli}/...` — all clean/green. Did not touch
`dist/`, did not build, did not restart the daemon.

## BLOCKING findings

None.

## ADVISORY findings

None. No taste-level nit rose to a level worth recording — the diff is unusually disciplined for a
six-engineer parallel change, and every deliberate judgement call in the plan (D-1 through D-8, and the
"known judgement calls" list in the task prompt) is implemented exactly as decided, not as an oversight.

## Verification against the review priorities

1. **C2 — label is a lookup key, never a value.** Traced the full path:
   `github.issuePayload.labels()` (`internal/github/issues.go:269-286`) → `Issue.Labels` → single mapper
   `(*issuePayload).issue()` (`issues.go:241`, used by both `ListIssues` and `GetIssue`) → `turn.Issue.Labels`
   (`internal/turn/execute.go:110-118`, doc-commented as a lookup key) → `internal/daemon/tools.go:resolveTools`,
   which reads label text only to detect the `lf:tools=` prefix and pull the value out for a **map lookup**
   against `d.toolProfiles`; the resolved `toolDecision.Tools`/`.AllowedTools` are freshly copied out of the
   *profile*, never out of the label. From there `turn.Request.Tools/AllowedTools/ToolProfile` →
   `harness.Request` (`internal/turn/prepare.go:220-227`) → `Runner.args`/`Runner.surface`
   (`internal/harness/runner.go:472-546`) → `exec.CommandContext`. Verified structurally, not by comment: the
   engineers added `internal/turn/tools_test.go:TestArgvCarriesNoStringOriginatingFromGitHub`, which spawns a
   **real** `*harness.Runner` against a fake `claude` script that dumps its own argv, injects a sentinel label
   value and a mis-cased prefix into `req.Issue.Labels`, and asserts none of them — nor the profile name itself —
   appear anywhere in the captured argv, while confirming the operator-authored `--tools`/`--allowedTools`
   values do. This is exactly the kind of test that would catch a regression a comment-only guarantee would
   miss. No leak path found anywhere in the traced chain.

2. **C9 — fail-closed.** `resolveTools` (`internal/daemon/tools.go:150-186`) has exactly the four documented
   outcomes (no label → zero value/no-op; ≥2 labels → `ReplyAmbiguousToolLabels`; mis-cased or malformed prefix
   / bad value shape → `ReplyMalformedToolLabel`; unmatched or absent map → `ReplyUnknownToolProfile`; matched →
   resolved). D-5 (label present, `tool_profiles` absent ⇒ refuse) falls naturally out of the same
   `d.toolProfiles[name]` lookup returning `!ok` on a nil map — no special-cased branch needed, and no path
   was found that falls through to the harness's default surface once a label is detected. `refuseTools`
   (`internal/daemon/tick.go:743-820`) runs before any worktree/runs-row/lock is touched (D-1), confirmed by
   its placement at the head of `d.run`. Guarded end-to-end by tests: `TestToolsRefusal_SpawnsNothingAndAdvancesTheWatermark`,
   `TestMalformedLabelReplyNeverEchoesInjectedText`, `TestAmbiguousLabelsNameNeitherLabel`,
   `TestFailedReplyPostLeavesTheCommentUnconsumed`, `TestIssueBodyToolsRefusalPostsNothing` (all in
   `internal/daemon/tools_test.go`).

3. **C1 — `--tools` is the boundary.** `Runner.args`/`Runner.surface` (`internal/harness/runner.go:472-546`)
   emit `--tools` unconditionally in both branches, and `--allowedTools` only inside `if len(allowedTools) > 0`.
   `TestRunOmitsAllowedToolsWhenTheProfileHasNone` and `TestRunUsesTheProfileSurfaceExclusively`
   (`internal/harness/runner_test.go:1226-1288`) pin exactly this distinction.

4. **`#nosec G204` justification** (`internal/harness/runner.go:539-561`) was read against the current code:
   it now states the tool lists "arrive on the request rather than only from the runner," that they are
   "copies of operator-authored strings out of config.yaml, selected by a profile name that was validated
   against the operator's own profile map," that `validate` has "just checked every one of them against
   r.ceiling," and that the profile name itself "stays off argv entirely." Every one of those claims is true
   of the code as committed — the copy-at-resolution-boundary (`daemon/tools.go:180-186`), the ceiling
   re-check (`Request.validate`, `runner.go:355-370`), and the fact that `args`/`surface` never reference
   `req.ToolProfile` (only `Tools`/`AllowedTools`) were all independently verified above. The rewrite is
   accurate, not just plausible-sounding.

5. **Aliasing and mutation.** `d.resolveTools` copies both slices with `append([]string(nil), profile.Tools...)`
   / `...AllowedTools...` at the resolution boundary (`daemon/tools.go:180-186`), and
   `TestResolveTools_CopiesProfileSlices` (`internal/daemon/tools_internal_test.go:178-208`) is a genuinely
   adversarial test: it mutates an element by index, appends past the copy's own length eight times, and
   re-resolves the same profile afterward to confirm the map entry and later resolutions are unaffected —
   this would catch a copy that merely aliased with spare capacity. Confirmed `internal/turn/harvest.go` is
   unmodified (`git diff` shows no hunk) and its cold-rerun mutation (`harvest.go:118-128`) only touches
   `prep.promptInput`, `prep.prompt`, `prep.request.Prompt`, and `prep.request.Path` — never `.Tools`/
   `.AllowedTools` — so even the in-place `prep.request` mutation the plan flagged as the hazard cannot reach
   the tool lists. `daemon.New` clones the profile map itself (`maps.Clone(opts.ToolProfiles)`,
   `daemon.go:252`) and the ceiling slice (`append([]string(nil), opts.ToolCeiling...)`), and
   `config.Claude.resolveToolCeiling` (`policy.go:308-317`) explicitly clones rather than aliases `Tools` when
   defaulting the ceiling. No shared-backing-array path back into `config.Claude.ToolProfiles` was found.

6. **Concurrency.** `d.toolProfiles`/`d.toolCeiling` are assigned exactly once, in `daemon.New`
   (`daemon.go:252-253`), and are read-only afterward — confirmed by grep (only one write site, one read
   site in `tools.go:173`) and by the fact `go test -race` is clean across every touched package including
   `internal/daemon` and `internal/harness`.

7. **Idiom, error handling, resource management, consistency.** `gofmt -l .` and `go vet ./...` are both
   silent. No new goroutines are introduced anywhere in the diff (grep for `go func`/`go d.` in the changed
   files returns nothing new), consistent with the plan's explicit note that this feature adds none. Error
   wrapping follows existing package idiom throughout (`fmt.Errorf("%w: ...", ErrInvalidRequest, ...)`,
   `invalidRequest(...)` helpers reused rather than reinvented). The three new `ReplyKind` values follow the
   existing flat-enum precedent (D-4) rather than introducing a new struct. `internal/report`'s house rule —
   re-validate anything echoed into a comment, rather than trusting an upstream check — is honored: both
   `daemon/tools.go`'s `toolProfileName` and `report/reply.go`'s `profileNamePattern` independently define
   the same regex rather than importing one from the other, matching the package's stated precedent at
   `reply.go:87-92` and the plan's S-8 note. Comments are extensive and, on spot-check, accurate to the code
   they describe — including the corrected `#nosec` comment (item 4 above) and the config template's
   comment-only worked example (verified `Defaults()` gains no profile, keeping `AC-1.9` true).

## Cross-story summary

Six engineers, ten commits, and the seams hold. The riskiest boundary — the label crossing from
`internal/github` through `internal/daemon`/`internal/turn` into `internal/harness`'s argv — was verified
structurally (a real subprocess spawn asserting on captured argv) rather than left to trust between stories,
which is the strongest form of cross-story consistency check this feature could have shipped. The
`report.ReplyKind`/`RefusesTools()` split cleanly reused `internal/turn/report.go`'s existing `ReportReply`
guards rather than growing a parallel refusal path, and the two live guards the plan called out
(`ReplyRequest.validate`, `replyKind`) were both updated to branch on `refusing()` first, exactly as directed.
`config.Claude.ToolCeiling`/`ToolProfiles` flow into both `internal/daemon.Options` and `internal/harness.Options`
from the same resolved `s.Config.Claude` in `setup.go`, so the daemon's logged ceiling and the harness's
enforced one cannot drift from two different reads of the config file. No boundary looked risky enough to
flag even short of a finding.

## Verdict

CLEAN — No findings. Proceeding to environment setup.
