# US-2 — Refuse legacy `lf:tools=` label loudly

> As an operator who still has an issue carrying the old `lf:tools=<profile>`
> label, I want litefactory to tell me the syntax has moved instead of quietly
> ignoring my label, so that I notice and relabel rather than losing a tool
> profile I thought was still active.

Delivers a loud, explicit refusal reply for the retired `lf:tools=` label
spelling, so the US-1 rename never silently drops a profile someone still relies on.

## Why This Matters

The moment the label vocabulary changes, every issue still wearing the old
`lf:tools=web` label would otherwise just stop working, with no sign of why. That
is the worst kind of breaking change — one nobody notices until the run behaves
differently than expected. This story makes the change loud instead of silent: an
issue with the old label gets a reply that says the syntax moved and shows the new
one, and the run continues with no tool profile applied rather than failing.

## Architecture Notes

- **Hard-coupled to US-1**, per `plan_brief.md`'s Workflow — the two ship in one
  commit. A rename without this refusal is a silent permission drop for every
  currently-labelled issue.
- Needs a new `report.ReplyKind` and its case in `internal/turn/report.go`'s
  `replyKind()` switch, in the same commit as the detection code — `internal/report`
  cannot import `internal/turn`, so the two halves of this change are inseparable.
- The refusal is a reply plus a no-op profile resolution (equivalent to no label
  present), never a turn failure — this is the "rejections are values" principle
  applied to a parsing-level rejection, not just a fetch-level one.
- The reply text goes through the existing redactor before posting, exactly like
  every other reply kind; no special-casing needed here.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-2.1 | Detect the legacy `lf:tools=<value>` label pattern | code | — | S |
| T-2.2 | Add new `report.ReplyKind` + its `replyKind()` case in `internal/turn/report.go` | code | T-2.1 | S |
| T-2.3 | Compose and post the refusal reply naming the new `Tools: <Profile>` notation; resolve to no profile | code | T-2.2 | S |
| T-2.4 | Regression test: legacy label never resolves to a profile, run never fails | test | T-2.3 | S |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-2.1 | An issue/comment carrying `lf:tools=<anything>` produces a reply naming the new `Tools: <Profile>` notation and resolves to no tool profile. |
| AC-2.2 | The refusal never fails the turn. |
| AC-2.3 | The new reply passes through the existing redactor before posting, like every other reply. |
