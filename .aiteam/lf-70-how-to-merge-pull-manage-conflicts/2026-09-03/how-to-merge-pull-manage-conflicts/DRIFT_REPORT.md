# Drift Report — Config-governed defaults and size cap (US-8)
# Branch: lf/70-how-to-merge-pull-manage-conflicts | Date: 2026-09-07

## Scope note

This run implemented **US-8 only**, by explicit instruction. US-1 through
US-7 and US-9 through US-12 remain unimplemented and are out of scope for
this drift comparison. The uncommitted, never-reviewed partial US-1/US-2 work
already present in the worktree from a previously stopped run was left
untouched throughout.

## Delivered as planned

- T-8.1: `AutoSyncBase bool` (default `true`), `AutoSyncMaxBehind int`
  (default `100`), `BlockRunOnConflict bool` (default `true`) added to
  `internal/config`'s `Settings`, with defaults applied via `Defaults()`.
- T-8.2: `Config.Validate()` rejects `AutoSyncMaxBehind < 0` as a hard config
  load error (CFG-6) — never clamped to `0` or to the default.
- T-8.3: The three keys, plus the "100 is a judgment call, not a measurement"
  note, documented in `internal/config/template.go`'s generated
  `config.yaml` template (the config package's existing convention for
  operator-facing key documentation, per its CFG-3 anti-drift check — this
  repo has no separate `docs/` file enumerating individual keys).
- T-8.4: Seven unit tests across defaults-when-omitted, explicit zero
  (both via direct struct assignment and via full YAML decode), negative
  rejection (both direct and via `Decode`+`Validate`), and per-field boolean
  override with sibling-independence.
- AC-8.1 and AC-8.2 both verified directly by tests; `go test -race ./...`
  green across all 22 packages, `go vet ./...` and `gofmt -l .` clean.
- Package placement (`internal/config`), no new dependency (`purity_test.go`
  still passes), no new package created — all match the plan's Architecture
  Notes and Shared Decisions exactly.

## Added scope

- Two extra unit tests beyond the implementer's initial five, added during
  Phase 3 to close two coverage gaps: explicit-zero surviving the real
  YAML-decode-onto-defaults merge path (not just direct struct assignment),
  and sibling-field independence when only one auto-sync key is overridden.
  Both are within T-8.4's intent ("tests: default values, 0=no-cap,
  negative=error, explicit true/false override") — not new scope, just more
  thorough coverage of the same task.

## Dropped or deferred

None within US-8's own scope. Everything in the story's task table and
acceptance criteria was delivered.

(Not a drop, but worth naming: US-2's and US-7's later consumption of these
three fields — reading them inside `syncBase` and overriding
`AutoSyncBase` via the `lf:sync=` label — is explicitly NOT this story's job
per its own Architecture Notes, and was correctly left undone.)

## Architectural deviations

None. The implementer followed the plan's stated package placement,
dependency constraints, and field semantics exactly, including the
CFG-6 negative-value hard-error rule and the "judgment call, not measurement"
documentation requirement for the `100` default.

## Verdict

CLEAN — delivered matches plan within acceptable micro-decisions (test
coverage additions and the choice of `template.go` as the documentation
surface, both consistent with the story's stated intent and the codebase's
own conventions).
