# Code Review
# Feature: US-8 — Config-governed defaults and size cap
# Reviewed by: go-code-reviewer
# Date: 2026-09-07
# Status: CLEAN

---

## Scope

Single-story review. Files read (all in `internal/config`, as reported):

- `config.go` — `Settings.AutoSyncBase`, `Settings.AutoSyncMaxBehind`,
  `Settings.BlockRunOnConflict` fields, their defaults in `Defaults()`, and
  the `DefaultAutoSyncMaxBehind` constant.
- `validate.go` — `MinAutoSyncMaxBehind` bound constant and the
  `AutoSyncMaxBehind < MinAutoSyncMaxBehind` rejection in `Config.Validate()`.
- `template.go` — the three new keys rendered into the generated
  `config.yaml`, with operator-facing documentation.
- `config_test.go` — five new unit tests covering AC-8.1 and AC-8.2.

`git status`/`git diff` confirm the change is scoped to exactly these four
files; no other tree state (the unrelated `internal/git`/`internal/turn` work
mentioned in the brief) was touched or reviewed. `go build ./...`,
`go vet ./internal/config/...`, `gofmt -l internal/config`, and
`go test ./internal/config/...` all pass as reported.

No cross-story consistency section applies (single-story run, per the task
brief).

---

## BLOCKING findings

None.

- **CFG-6 (negative value is a load error, never clamped)**: correctly
  implemented. `Validate()` rejects `AutoSyncMaxBehind < 0` via
  `MinAutoSyncMaxBehind = 0`; nothing in `Decode`, `normalize`, or `Load`
  mutates the field before or after that check, so a negative value is
  neither clamped to 0 nor silently replaced by
  `DefaultAutoSyncMaxBehind`. Confirmed both unit-test-side
  (`TestAutoSyncMaxBehindNegativeIsConfigError` asserts the field is
  unchanged after a failed `Validate()`) and end-to-end
  (`TestAutoSyncMaxBehindNegativeFailsConfigLoad` goes through
  `Decode`+`Validate`).
- **AC-8.1 (zero-config defaults)**: `Defaults()` sets `AutoSyncBase: true`,
  `AutoSyncMaxBehind: DefaultAutoSyncMaxBehind` (100), `BlockRunOnConflict:
  true`; YAML decoding merges onto this struct, so an omitted key keeps the
  default. Verified structurally and by
  `TestAutoSyncSettingsDefaultWhenOmitted`.
- **AC-8.2 (zero means no cap, distinct from omission)**: `0 <
  MinAutoSyncMaxBehind` is false, so an explicit `auto_sync_max_behind: 0`
  passes validation and is preserved as a real `0`, not reinterpreted as
  "use default". Verified by `TestAutoSyncMaxBehindZeroMeansNoCap` and the
  boolean-override test confirms explicit YAML values aren't swallowed by
  the defaults-merge decode pattern (CFG-2).
- **No new third-party dependency**: `validate.go` and `template.go` import
  only stdlib; `config.go` remains the sole importer of `gopkg.in/yaml.v3`,
  which `purity_test.go`'s `TestValidationHasNoThirdPartyImports` already
  enforces and which still passes.
- **Doc comments**: `AutoSyncBase`, `AutoSyncMaxBehind`, `BlockRunOnConflict`,
  `DefaultAutoSyncMaxBehind`, and `MinAutoSyncMaxBehind` each carry a doc
  comment starting with the identifier's name, and the "100 is a judgment
  call, not a measurement" language required by the story appears in both
  the Go doc comment and the generated `config.yaml` template — satisfying
  the "documented in code and in operator-facing docs" requirement.
- **Naming/idiom**: `MixedCaps`, no stuttering (`Settings.AutoSyncMaxBehind`,
  not `Settings.SettingsAutoSyncMaxBehind`), YAML tags match the
  architect-specified keys exactly (`auto_sync_base`,
  `auto_sync_max_behind`, `block_run_on_conflict`).
- **Package placement**: everything lands in `internal/config`, on the
  existing `Settings` struct and existing `Defaults()`/`Validate()`
  functions — no new package, no `syncBase` or `lf:sync=` logic added,
  matching the story's explicit "no building of syncBase" constraint.
- **`Fprintf` argument wiring in `template.go`**: the format string has two
  `%d` verbs relating to `auto_sync_max_behind` (one inside a comment line,
  one for the value itself) and the argument list correctly supplies
  `d.Settings.AutoSyncMaxBehind` twice, in order, alongside `AutoSyncBase`
  and `BlockRunOnConflict` at their correct positions. `go vet` (which
  checks `Printf` argument count/type against verbs) passes, and manual
  cross-check against the format string confirms alignment.

---

## ADVISORY findings

### CR-A-001: Validation error message wording is slightly awkward
**File:** `internal/config/validate.go:110-113`
**Story:** US-8
**Category:** Idiom / Standards
**Finding:** The error text `"...must not be negative; 0 means no cap and is never applied automatically for a negative value"` is a little hard to parse on first read — "is never applied automatically for a negative value" doesn't clearly say what "is" refers to (the default? the cap?).
**Suggestion:** Something closer to the sibling attachment-bounds messages' plain "is out of range" phrasing, e.g. `"...must not be negative; 0 means no cap, and a negative value is never clamped to 0 or to the default"` would read more directly to an operator debugging a startup failure. Not required — the message already names the field and the rule correctly.

---

## Cross-story summary

Not applicable — this is a single-story run. No other story's files were in
scope, and this change does not touch or depend on `internal/git`,
`internal/turn`, or any other package; it only adds fields to
`internal/config.Settings` that a later, out-of-scope story
(auto-sync itself, US-2/US-7) will read. The doc comments correctly flag
that forward dependency without building any of that logic here.

---

## Verdict

CLEAN — No blocking findings. One advisory (message wording) noted for
`notes.md`. Proceeding to environment setup.
