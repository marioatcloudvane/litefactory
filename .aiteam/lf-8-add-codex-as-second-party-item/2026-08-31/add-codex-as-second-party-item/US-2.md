# US-2 — Per-turn label routing

> As an issue owner, I want an `lf:agent=<tool>` label re-read every tick to
> route the run to Claude or Codex, so that I can switch tools mid-thread
> without editing config or restarting the daemon.

Adds a second per-tick label decision, `resolveAgent`, built on the exact
control-flow ladder the existing `lf:tools=` mechanism already uses, and wires
it into both places a run gets dispatched.

## Why This Matters

The whole point of putting Codex behind a label is that switching tools should
feel as easy as changing a GitHub label — no config edit, no daemon restart, no
waiting for a deploy. That mechanism — read a label fresh every tick, refuse
cleanly on anything ambiguous or malformed rather than guessing — already exists
in this codebase for a different purpose (choosing a permission profile), almost
exactly shaped the way this needs to work. The job here is mostly disciplined
reuse: routing which *tool* runs uses the same refusal vocabulary as routing
which *permissions* apply, so an operator who already understands one
understands the other, and a two-line label typo produces a clear refusal
instead of a surprising run.

## Architecture Notes

- Reuse `resolveTools`'s ladder byte-for-byte in `resolveAgent`: no matching
  `lf:agent=` label → zero-value decision, configured default agent runs; more
  than one matching label (any casing) → refusal; a label whose prefix isn't
  lowercase `lf:agent=` → refusal; a value that doesn't match the shape regex
  (including empty) → refusal; a well-formed value that isn't a registered
  `Kind` → refusal, may name the value; otherwise resolved.
- The four-stage refusal precedence from `plan_brief.md`'s Shared Decisions
  applies here for stages 1 and 2: the agent-label ladder runs first; if it
  refuses, the tools-profile ladder is never evaluated in that tick. (Stage 3 —
  per-agent stanza narrowing — belongs to US-7; stage 4 — segment ownership —
  belongs to US-3. Do not implement either of those here.)
- Resolution happens once per tick, at the same call sites `resolveTools`
  already uses (`handleComment`, `runIssueBody`). Two comments drained in one
  tick share one label snapshot, matching `lf:tools=`'s existing documented
  granularity.
- No new GitHub API call is introduced — labels come from the tick's existing
  fresh snapshot, exactly as `lf:tools=` already reads them.
- This story needs `internal/agent`'s `Kind` type and the `RunnerSet` from US-5
  to route to; it cannot start before US-5's core lands.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-2.1 | `resolveAgent()` mirroring `resolveTools`'s exact ladder | Code | US-5 (`ReplyKind`s, `RunnerSet`) | M |
| T-2.2 | Wire both call sites (`handleComment`, `runIssueBody`); enforce agent-ladder-before-tools-ladder precedence | Code | T-2.1 | M |
| T-2.3 | Table tests per ladder rung: absent, two labels, mis-cased prefix, mis-cased value, empty value, unknown value, resolved — asserting exact `ReplyKind` and that `Agent` is empty on refusal rungs | Test | T-2.1 | M |
| T-2.4 | Golden-argv test (byte-identical Claude argv when no `lf:agent=` label is present) + no-new-GitHub-API-call test against the fake forge's request log | Test | T-2.2 | S |
| T-2.5 | Refusal-ordering test: an agent refusal and a tools refusal in the same tick report only the agent refusal | Test | T-2.2 | S |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-2.1 | `resolveAgent` returns the zero decision for a label set containing no `lf:agent=` label, and the resulting argv for a Claude run is byte-identical to the pre-feature argv |
| AC-2.2 | Table test covers every ladder rung — absent, two labels, mis-cased prefix, mis-cased value, empty value, unknown value, resolved — asserting the exact `ReplyKind` and that `Agent` is empty on the ambiguous and malformed rungs |
| AC-2.3 | A test asserts no refusal path can carry raw label text into a rendered reply |
| AC-2.4 | Labels are read from the tick's existing snapshot: no new GitHub API call is made per tick, asserted against the fake forge's request log |
| AC-2.5 | Both call sites (`handleComment`, `runIssueBody`) resolve the agent |
| AC-2.6 | A test asserts that when both an agent refusal and a tools refusal apply in the same tick, only the agent refusal is reported |
