# Implementation Notes — US-8 (Config-governed defaults and size cap)

## Scope of this run

This run implemented **US-8 only**, per explicit instruction. US-1 through
US-7 and US-9 through US-12 are untouched. The uncommitted, never-reviewed
partial US-1/US-2 work already present in the worktree (`internal/git/merge.go`,
`internal/turn/prepare.go`, `internal/git/{errors,exec,worktree}.go`, from a
prior stopped run) was left exactly as found — not read for guidance, not
built upon, not touched.

## Assumptions

- The "config docs update" task (T-8.3) was satisfied by adding the three new
  keys, with their documentation, to `internal/config/template.go`'s generated
  `config.yaml` template (`DefaultFileContents()`), since the repo has no
  separate `docs/` markdown file enumerating individual config keys and this
  template is what the config package's own convention (CFG-3 anti-drift
  check) treats as the source of operator-facing key documentation.
- No upper bound was added to `AutoSyncMaxBehind` — the story only specifies
  "negative is an error"; adding an unrequested ceiling would have been an
  unrequested scope addition.
- The three new fields were placed flat on `Settings` (not a new nested
  stanza), matching the YAML key names given in the story (`auto_sync_base`,
  `auto_sync_max_behind`, `block_run_on_conflict`) and the existing flat style
  of other `settings.*` keys.

## Code Review Advisories

- **CR-A-001** (advisory, non-blocking): the wording of the negative
  `auto_sync_max_behind` config-load error message is slightly awkward,
  though functionally correct and correctly identifies the offending field.
  Left as-is; does not affect AC-8.2 or any test. See `CODE_REVIEW.md` for
  detail.

## Phase 2 — Environment setup

US-8 touches only `internal/config` (in-memory struct defaulting/validation
against YAML input) — no database, no external HTTP call, no message queue.
No story in this run's scope touches a real dependency. Environment setup and
the integration test suite were both skipped per the orchestrator's Phase 2
rule ("no story touches real dependencies -> skip to Phase 3 immediately").

---

# Implementation Notes — US-7 (`lf:sync=` label opt-out)

## Scope of this run

This run implemented **US-7 only**, per explicit instruction ("Implement
US-7"). US-1 through US-6 and US-9 through US-12 are untouched. US-8 (already
implemented and committed in a prior run) was not touched. The uncommitted,
never-reviewed partial US-1/US-2 work already present in the worktree
(`internal/git/merge.go`, `internal/turn/prepare.go`,
`internal/git/{errors,exec,worktree}.go`, from an earlier interrupted run) was
left exactly as found — not read for guidance, not built upon, not touched.
Confirmed via `git status` after implementation: only `internal/daemon/sync.go`
(new), `internal/daemon/tick.go`, `internal/turn/execute.go`, and the two new
test files (`internal/daemon/sync_internal_test.go`,
`internal/daemon/sync_test.go`) changed.

## What was built

- `internal/daemon/sync.go` — `syncLabelPrefix`, `syncRefusal` enum
  (`syncRefusalNone`/`syncRefusalAmbiguous`/`syncRefusalMalformed`),
  `syncDecision` struct (`Resolved bool; Value bool; Refusal syncRefusal;
  MiscasedPrefix bool`, plus a `refused()` method), and
  `resolveSync(labels []string) syncDecision` — copied rung-for-rung from
  `internal/daemon/agent.go`'s `agentLabelPrefix`/`resolveAgent` ladder, not
  from the dead `lf:tools=`/`legacyToolsLabel` pattern, per the story's
  explicit instruction.
- `internal/turn/execute.go` — `turn.Request` gained `SyncOverride *bool`
  (`nil` = no override, config default applies; non-nil = the label's
  resolved on/off). No `validate()` changes needed — nil is a legitimate
  value.
- `internal/daemon/tick.go` — computes `syncOverride := resolveSync(t.issue.Labels)`
  immediately before the existing `turn.Request{...}` literal and passes
  `SyncOverride` through when `.Resolved` is true; no other field in that
  literal was touched.

This story stops at producing the resolved decision and threading it into
`turn.Request` — it does not implement `syncBase` (US-2, not yet built),
config-default reading (US-8, already done separately), or disclosure of the
refusal reason (US-3, not yet built). `syncDecision.Refusal`/`MiscasedPrefix`
are structured so a later disclosure story can build on them without needing
any raw label text, which was never captured or stored anywhere (SEC-5/S-9).

## Assumptions

- None required beyond the story's own architecture notes — AC-7.1 and the
  ladder description in `US-7.md` were unambiguous and directly implementable
  against the existing `agentLabelPrefix`/`modeLabelPrefix` precedent in the
  same package.

## Code Review Advisories

- None. `go-code-reviewer` returned **CLEAN** with zero BLOCKING or ADVISORY
  findings — verified the ladder's precedence order against `agent.go`
  side-by-side, verified no field on `syncDecision` or `turn.Request` can
  carry raw label text (SEC-5/S-9), verified `tick.go`'s integration point
  left every other `turn.Request{...}` field untouched, and confirmed scope
  discipline via `git status` (only the three implementation files changed).
  See `CODE_REVIEW.md`'s second dated section for the full record; the file's
  first section (US-8, from the prior run) was left unchanged.

## Phase 2 — Environment setup

US-7 touches only `internal/daemon` (in-memory label-string parsing) and a
struct field addition in `internal/turn` — no database, no external HTTP
call, no message queue. No story in this run's scope touches a real
dependency. Environment setup and the integration test suite were both
skipped per the orchestrator's Phase 2 rule.

## Testing

`go-unit-test-engineer` added two test files covering every rung of the
ladder (absent, `on`, `off`, miscased prefix ×3 shapes, duplicated/ambiguous
label ×3 shapes, invalid/empty value), sibling-label independence, a
structural SEC-5/S-9 check via `reflect` confirming no field on
`syncDecision` can carry a string, and an end-to-end check that
`turn.Request.SyncOverride` is populated correctly (or left `nil`) at the
`Daemon.run`/`tick` dispatch point for every label shape. 7 test functions
(~21 subtests total), 0 failures.

## Final gate (orchestrator-run, not delegated)

`go build ./...`, `go vet ./...`, `gofmt -l .` (no output), and
`go test -race ./...` were all run directly by this orchestrator across the
whole module (not just the touched packages) and are all green — every
package reports `ok`, no race detector findings, no vet or fmt issues.

## Bug loop

Not entered — zero test failures reported by any phase.
