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
