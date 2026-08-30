# Implement notes — tool-permissions

## Code Review Advisories

None. `go-code-reviewer` returned **CLEAN** — no BLOCKING and no ADVISORY findings.
See `CODE_REVIEW.md`.

## Decisions taken during Implement

These are decisions the plan did not settle. Each is reversible; the owner may want to.

### I-1 — a tools refusal on the issue-body path posts nothing

D-1 put resolution at the head of `d.run`, which has two callers: `handleComment` (a real comment
id) and `runIssueBody` (`tick.go:437`, `CommentID: 0`). `ReplyRequest.validate` requires
`CommentID > 0` — "a comment id is required to reply to a comment" — so the body path cannot post a
refusal without changing that contract.

**Chosen:** refuse the run, log at INFO naming which refusal it was and that nothing was posted,
post no comment. `ReplyRequest.validate` unchanged.

**Rejected:** letting `ReplyRequest` accept `CommentID == 0` for refusals and skipping the consume
step (the watermark is already planted by `bootstrap`). `RunReport.validate` already treats 0 as
"the issue body", so the precedent exists in the same file.

**Why the chosen option is defensible:** it is consistent with AC-2.16c (a body naming a command
that spawns nothing posts nothing), it is fail-closed because nothing runs, and the silence is *not*
permanent — the next comment on that issue goes through `handleComment`, which has a comment id and
posts the refusal loudly. Implemented as one `if trigger.CommentID == 0` block in `refuseTools`, so
reversing it is local.

### I-2 — `tool_ceiling: []` is treated as absent

An explicitly empty ceiling defaults to `claude.tools` rather than meaning "no tool is permitted",
which would forbid the daemon's own default surface. It narrows, never widens. Pinned by
`TestToolCeilingEmptyListIsTreatedAsAbsent`; that test is the one place to change if reversed.

### I-3 — ceiling entry *names* are policy-checked only when the ceiling differs from `claude.tools`

Once `Load` has defaulted the ceiling, its entries are the same strings already reported under
`claude.tools`; naming them again under a key the operator never wrote is noise. The superset check
and `validateNoForbiddenFlag` always run over the ceiling.

### I-4 — four refusal cases, three `ReplyKind` values

D-5's case (label present, `tool_profiles` absent) renders identically to unknown-profile. An absent
map is simply an unsuccessful exact-key lookup, so it needs no branch, and the sentence is literally
true of it. It does not fall through to defaults.

### I-5 — a mis-cased label still counts toward ambiguity

`lf:tools=research` + `LF:Tools=build` is ambiguous, not malformed. Judging one label before counting
would mean choosing which of two competing grants describes the failure — the exact choice the
ambiguous rule exists to refuse — and would make the outcome depend on the order GitHub returned the
labels. Both branches return an empty `Profile`, so neither can resolve, echo or widen.

### I-6 — `metadataList` renders no `**Tools:**` bullet when a run knows nothing about tools

A prepare failure from before this feature has no requested surface and no profile. `metadataList`'s
documented rule is to omit rather than guess; `**Tools:** unknown` would be a bullet that says
nothing on every historical failure comment. Pinned by test.

## Corrections made during Implement

### C-1 — D-6 was implemented wrongly first, on my instruction

I briefed the T-6 engineer that `LF:Tools=research` should resolve normally, since its *value* is
lowercase. That was wrong. D-6's own rationale says the case-insensitive prefix "turns a typo into a
loud refusal", and AC-3.5's verdict on that exact label is "refuses as malformed". The rule is
**detect leniently, accept strictly**: a label whose prefix case-insensitively equals `lf:tools=` is
seen — so it cannot be silently missed, which is the whole point of D-6 — and is then refused as
malformed unless the prefix is byte-exact. Corrected in `6dd9d9b`.

A useful side effect: `LF:Tools=nosuch` is malformed rather than unknown-profile, because the prefix
is judged before the map lookup. A mis-cased label can never echo an unrecognised name into a
comment.

## Defects found in existing code

- `internal/daemon/fixture_test.go`'s `newDaemon` **silently dropped** `ToolProfiles`/`ToolCeiling`
  overrides, so no daemon test could configure profiles at all. Found and fixed by the T-7/T-9 test
  engineer. Test-only; no production impact.

## Not done

- The plan's **OPTIONAL** section (`--setting-sources user` / `--bare`, `--permission-mode dontAsk`,
  sandbox settings) was skipped wholesale, per D-8 and explicit instruction. **This remains the real
  fix for the ambient-rule leak measured in SD-2** — 250 ambient allow-rules leaked through and Bash
  executed while absent from `--allowedTools`. Carry it into handover.
- Nothing was pushed and nothing was merged to `main`.
