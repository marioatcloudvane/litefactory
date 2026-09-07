# Code Review
# Feature: Configurable Agent Teams — US-13 (`team manage` per-repo agent source override)
# Reviewed by: go-code-reviewer
# Date: 2026-09-07
# Status: ADVISORY_ONLY

---

## Scope

This review covers only the files listed in the Phase 1 implementation report for
US-13 (T-35, T-36, T-37). Prior stories (US-1–US-11, US-19) are already landed and
were not re-reviewed, except where used as reference to confirm this story stayed
consistent with their established patterns.

Files reviewed:
- `internal/team/overrides.go` (new)
- `internal/team/errors.go` (modified: `ErrOverridePath`, `OverridePathError`)
- `internal/cli/cmd_team.go` (modified: `newTeamManageCommand`, `runTeamManage`, `teamManageError`)
- `internal/teamsource/render.go` (modified: `readAgentSource` call site in `RenderTeam`/`RenderCurrent`)

Test files (`overrides_test.go`, `cmd_team_manage_test.go`, `render_test.go`) were
not reviewed, per scope.

Reference files read only to confirm consistency (not modified by this story):
`internal/team/decode.go` (`joinSorted`), `internal/team/writefile.go`
(`refuseSymlink`, `writeFileAtomic`), `internal/team/lock.go` (`acquireRepoLock`),
`internal/team/write.go` (`Add`'s `config.ClassifySecret` call site),
`internal/cli/parse.go`, `internal/cli/router_test.go`, `internal/cli/commands.go`.

---

## BLOCKING findings

None.

---

## ADVISORY findings

### CR-A-001: Unreachable absolute-path check in `validateOverrideSource`
**File:** `internal/team/overrides.go:132`
**Story:** US-13 / T-35
**Category:** Idiom
**Finding:** `validateOverrideSource` first rejects `localPath` if `filepath.IsAbs(localPath)` (line 128). It then computes `clean := filepath.ToSlash(filepath.Clean(localPath))` and checks `... || filepath.IsAbs(clean) ` (line 132) as one of the `".."`-segment rejection conditions. Given `localPath` is already confirmed relative by line 128, and `filepath.Clean` never turns a relative path into an absolute one, the `filepath.IsAbs(clean)` disjunct at line 132 can never be true — it is dead code.
**Suggestion:** Drop the redundant `filepath.IsAbs(clean)` disjunct, or add a one-line comment noting it is deliberate defense-in-depth if that's the intent (e.g. guarding against a future change in ordering). Not required before testing; no behavioral effect.

---

## Cross-story summary

This story integrates cleanly with the established architecture and, notably,
resolves the recurring fail-fast-error-aggregation concern that has been raised
BLOCKING three times in prior reviews: `decodeOverrides` (`internal/team/overrides.go`)
performs the same two-phase generic-walk-then-typed-decode pattern as
`internal/team/decode.go`'s index decoding, collecting every key/value fault into
`errs` and aggregating via the shared `joinSorted` helper rather than returning on
the first bad entry — this is a genuine reference-pattern reuse, not a
reimplementation. `validateOverrideSource`'s checks are sequential fail-fast
checks against a single scalar input (one path, not a collection), which is a
different case from the aggregation rule and is not a violation; AC-4's own
acceptance criteria test each precondition failure mode individually, confirming
this is the intended shape. `internal/teamsource/render.go`'s `RenderTeam` (a
pre-existing loop, only its `readAgentSource` call site changed) continues to
aggregate every read/parse/render/lockfile failure across the whole roster via
`errors.Join`, unaffected by this story's addition.

Dependency direction holds: `cmd_team.go`'s new `runTeamManage`/`teamManageError`
call only into `internal/team` (`KeyPattern`, `KeyError`, `SetOverride`,
`UnsetOverride`, `ActiveOverride`) and do no rendering/hashing/source-resolution
themselves — a thin CLI wrapper, consistent with the `cli -> teamsource -> team`
direction. `PreconditionError` is constructed only inside `internal/cli`
(`teamManageError` maps `*team.OverridePathError` via `errors.As` to
`Precondition(err)`); `internal/team` itself only ever returns the plain
`*OverridePathError`/`*KeyError` sentinels, never a `PreconditionError`.
`internal/team/overrides.go` imports only stdlib packages — no new cross-package
import was introduced, so the "internal/config is the only external litefactory
import internal/team may take" constraint is untouched by this story.

Symlink refusal, path validation, and permission bits all match the established
pattern: `validateOverrideSource` calls `refuseSymlink` before `os.Lstat` on the
candidate write-adjacent path; `ReadOverrideSource` calls `refuseSymlink` before
`os.ReadFile` on the override-referenced markdown; `saveOverrides` writes via the
shared `writeFileAtomic(path, data, 0o666)` helper, matching the `gosec`-justified
umask-relative permission pattern used by `acquireRepoLock` and the lockfile
writer elsewhere in the package. `SetOverride`/`UnsetOverride` serialize on the
same per-repo blocking `flock` (`acquireRepoLock`/`lock.release()`) that `Add`
already uses — reuse, not new concurrency; no goroutines, channels, or new
mutexes were introduced anywhere in the reviewed files.

Confirmed by reading `internal/team/write.go`'s `Add`: every `team.RenderedFile`
byte, regardless of whether it originated from an override (substituted upstream
in `readAgentSource`) or an index-resolved source, flows through the same
`config.ClassifySecret(string(f.Bytes))` check in `Add`'s per-file validation
loop before any write — `Add` has no visibility into which files, if any, came
from an override, so there is no code path by which override-sourced content
could bypass secret scanning. AC-3's requirement that drift classification need
no special case for overridden agents is satisfied structurally: `readAgentSource`
substitutes bytes before `team.HashBytes`/`parseAgentSource`/`renderer.Render` run,
so `team.Classify` (untouched by this story) continues to react only to
`SourceHash`, unaware of which file produced it, exactly as the architecture
notes specify. The `team manage` action switch (`set`/`unset`/`show`, default →
`Usagef`) keeps the override mechanism closed, matching the "not a general
setter" constraint. No `context.Context` parameter was added to `RenderTeam`,
`RenderCurrent`, `readAgentSource`, or `parseAgentSource` — renderer signatures
remain pure/CPU-only. The `team manage` command is registered in
`internal/cli/commands.go`'s `Commands()` table, so it is reachable, and its
`Usage` string literally contains `<agent-key>`, `<action>`, and `<source>`,
satisfying `TestCommandsAreWellFormed`; the router's positional-argument capping
in `parse.go` guarantees the handler's `len(args) > 2` / `args[2]` access is safe
and that `args[0]`/`args[1]` are always populated given `Args.Min: 2`.

The one boundary worth calling out even though it produced no BLOCKING finding:
a symlink refusal surfaced through `SetOverride`/`ReadOverrideSource` is not
mapped to `PreconditionError` in `teamManageError` — it falls through to the
plain-error branch (exit 1) rather than exit 3, even though it is conceptually
a "the path you gave is bad" failure similar to AC-4's other precondition modes.
This was confirmed to be consistent with the rest of `cmd_team.go`: no existing
command (add/status/update) maps `SymlinkRefusedError`/`ErrRefusedSymlink` to
`PreconditionError` either, so this is established codebase behavior, not an
inconsistency introduced by this story, and AC-4 does not enumerate symlinks as
one of its precondition-error modes.

---

## Verdict

ADVISORY_ONLY — 1 suggestion noted (CR-A-001, cosmetic/dead-code only). No
BLOCKING issues found. Proceeding to environment setup.
