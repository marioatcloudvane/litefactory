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

---
---

# Code Review
# Feature: US-7 — `lf:sync=` label opt-out
# Reviewed by: go-code-reviewer
# Date: 2026-09-07
# Status: CLEAN

---

## Scope

Single-story review, scoped to exactly US-7 as instructed. Files read (all
reported as created/modified in this run):

- `internal/daemon/sync.go` (created) — `syncLabelPrefix`, `syncRefusal`
  (`syncRefusalNone`/`syncRefusalAmbiguous`/`syncRefusalMalformed`),
  `syncDecision` (with `refused()`), and `resolveSync(labels []string)
  syncDecision`.
- `internal/turn/execute.go` (modified) — added `SyncOverride *bool` to
  `turn.Request`.
- `internal/daemon/tick.go` (modified) — computes `syncOverride :=
  resolveSync(t.issue.Labels)` inside `Daemon.run`, threads it into the
  `turn.Request{...}` literal as `SyncOverride: syncOverride`.

Read for comparison, per the implementer's claim and the architecture notes:
`internal/daemon/agent.go` (`agentLabelPrefix`/`Daemon.resolveAgent`),
`internal/daemon/mode.go` (`modeLabelPrefix`/`resolveModeLabel`), and
`internal/daemon/tools.go` (`toolLabelPrefix`/`legacyToolsLabel`, the
dead-code pattern the story says NOT to copy).

`git status` confirms the change is scoped to exactly these three files
(`sync.go` untracked, `tick.go` and `execute.go` modified); the pre-existing
uncommitted US-1/US-2 files (`internal/git/merge.go`, `internal/turn/prepare.go`,
`internal/git/{errors,exec,worktree}.go`) show no changes and were not
touched or reviewed, per instructions. `go build ./...`, `go vet
./internal/daemon/... ./internal/turn/...`, and `gofmt -l` on all three
touched files all pass clean.

---

## Verification against AC-7.1 and the architecture notes

**Ladder shape and precedence** — `resolveSync` in `sync.go` reproduces
`Daemon.resolveAgent`'s structure rung for rung, confirmed by side-by-side
read of both functions:

1. Scan every label; a label shorter than the prefix is skipped; the prefix
   is compared with `strings.EqualFold` (case-insensitive detection), and
   each match is recorded as a `seen{exactPrefix, value}` — never leaked
   outside the function.
2. `len(found) == 0` → zero-value `syncDecision{}` (silence, not a refusal;
   config default applies). Matches AC-7.1's "no label" path.
3. `len(found) > 1` → `syncRefusalAmbiguous`, unconditionally, before any of
   the matched labels is otherwise inspected — so a mis-cased label still
   counts toward the total and the refusal never depends on which of two
   competing labels the forge happened to return first. This mirrors
   `resolveAgent`'s documented rationale for the same ordering choice
   verbatim.
4. Exactly one match: `!exactPrefix` → `syncRefusalMalformed` with
   `MiscasedPrefix: true`, and — critically — no other field set, so nothing
   of the label's actual text (prefix or value) survives into the returned
   struct.
5. `value` not exactly `"on"` or `"off"` (including empty) →
   `syncRefusalMalformed`, `MiscasedPrefix` left false to distinguish the
   two malformed causes, again with no raw text carried.
6. Otherwise: `Resolved: true`, `Value` set to the boolean the label named.

This is the correct precedence order per AC-7.1 ("malformed, miscased, or
duplicated label is refused and the config default applies, never a guessed
value") and matches the two live label-ladder patterns' shape, not the dead
`lf:tools=`/`legacyToolsLabel` pattern (which only detects-and-refuses and
never reads a value). The implementer's claim to have copied
`resolveAgent` rather than `legacyToolsLabel` checks out on direct
comparison.

**SEC-5/S-9 (no raw label text travels)** — Verified by reading every field
`syncDecision` exposes: `Resolved bool`, `Value bool`, `Refusal syncRefusal`,
`MiscasedPrefix bool`. None of these can carry label text; `Value` is a
bool, not a copy of the string. This is stricter than `agentDecision`,
which does carry an `Agent string` field for its "unknown agent" refusal
case (justified there by a documented safety check, `agentKindName`,
before it is set) — `syncDecision` has no equivalent path and needs none,
since its only well-formed values are `"on"`/`"off"`, which collapse to a
bool. Confirmed no other file in the repository references
`resolveSync`/`syncDecision`/`syncRefusal`/`syncLabelPrefix` except
`sync.go` itself, `tick.go`, and this session's own planning docs — so
nothing downstream had a chance to introduce a leak either. `SyncOverride`
on `turn.Request` is a `*bool`, which by construction cannot carry text.

**Turn.Request integration (T-7.2)** — `SyncOverride *bool` is added
alongside the existing `Agent`/`AttachmentsAllowed` fields, with a doc
comment that correctly states: nil means "no label, or one that could not
be resolved — config default applies," non-nil means the label's value
beats the default, and this story does not act on the value (US-2's
`syncBase` does). Read the full `turn.Request` struct and `Request.validate()`
in `execute.go`: no other field was touched, removed, or reordered by this
change, and `validate()` was correctly left alone (nil `SyncOverride` is a
valid, common case and needs no validation gate).

**tick.go integration** — Read the full `turn.Request{...}` literal in
`Daemon.run` before and around the change: every pre-existing field
(`Mode`, `SessionID`, `Issue`, `Trigger`, `Command`, `Tools`,
`AllowedTools`, `ToolProfile`, `Agent`, `CodexSandbox`,
`AttachmentsAllowed`) is present, unchanged, and in the same relative
position; `SyncOverride: syncOverride` is appended as the last field. The
`resolveSync` call reads `t.issue.Labels`, which is a `[]string` on a
per-task value (`task.issue github.Issue`, built once on the Tick goroutine
in `materialise` and handed to exactly one worker) — confirmed no
production code path mutates `t.issue.Labels` after that point (only a test
fixture does, out of scope). `resolveSync` itself takes the slice by value
and only reads it (slicing into `seen` copies, never touching the original
backing array), so there is no race and no risk of the ladder observing a
different label set than the rest of `Daemon.run` (`resolveAgent`,
`resolveModeLabel`, `legacyToolsLabel` all read the identical
`t.issue.Labels` earlier in the same function, on the same goroutine).

**Concurrency** — `resolveSync` is a pure function with no shared state; it
is called once per turn on the worker goroutine dispatched in
`Daemon.offer`'s `go func`, operating only on that goroutine's own `*task`.
No goroutine, channel, mutex, or WaitGroup is introduced by this change, so
none of the concurrency-safety checklist items apply beyond confirming the
absence of a race on `t.issue.Labels`, which is confirmed above.

**Resource management, error handling, performance** — Not applicable in
any material way: no I/O, no allocation beyond one small slice
(`found []seen`, bounded by the number of labels on one issue — never
large), no error return from `resolveSync` (correctly a struct return,
matching the sibling ladders), nothing to defer or close.

**Package placement and dependencies** — `sync.go` lands in
`internal/daemon`, next to `agent.go` and `mode.go`, exactly as specified.
No new package. `sync.go` imports only `strings`, already an
`internal/daemon` dependency elsewhere (`agent.go`, `mode.go`, `tools.go`)
— no new third-party dependency.

**Idiom** — `gofmt -l` clean. Naming is `MixedCaps`, consistent with the
sibling ladders (`syncLabelPrefix`, `syncDecision`, `syncRefusal`), no
stuttering. `resolveSync` is correctly a plain function rather than a
`*Daemon` method, unlike `resolveAgent` — the right call, since unlike
`resolveAgent` (which needs `d.agents` to validate the resolved Kind against
a registry), `resolveSync` needs no daemon state at all; making it a method
anyway would have been the unnecessary-abstraction anti-pattern in reverse.
Doc comments are thorough (including a "why", not just a "what") on every
declaration, matching the surrounding file's style, even though all the new
identifiers are unexported and doc comments are not strictly required for
them. No `TODO`/`FIXME`, no dead code, no stray `fmt.Println`.

---

## BLOCKING findings

None.

## ADVISORY findings

None.

---

## Cross-story summary

This run touches exactly the boundary the story specifies: `sync.go` is a
pure, additive ladder in `internal/daemon`, and its only integration point
is one new field on `turn.Request`, appended without disturbing any
existing field, consumer, or validation rule. `Request.validate()` was
correctly left unchanged (a nil `SyncOverride` needs no gate). The field's
doc comment is explicit that no code in this run reads or acts on the
resolved value — that is deferred to US-2's not-yet-built `syncBase` — and
grep confirms `SyncOverride`/`resolveSync` appear nowhere else in the
codebase, so the story has not reached ahead of its own scope. The
pre-existing uncommitted US-1/US-2 partial files were left untouched, as
instructed, and this story's code does not depend on or interact with them.
No risky boundary was found.

---

## Verdict

CLEAN — No findings. Proceeding to environment setup.
