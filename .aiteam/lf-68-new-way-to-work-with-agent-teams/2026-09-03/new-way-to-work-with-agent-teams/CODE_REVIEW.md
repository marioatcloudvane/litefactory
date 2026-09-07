# Code Review
# Feature: Configurable Agent Teams — US-14 (post-render forbidden-flag/secret validation on every write path)
# Reviewed by: go-code-reviewer
# Date: 2026-09-07
# Status: CLEAN

---

## Scope

This review covers only the files listed in the Phase 1 implementation report for
US-14. Prior stories (US-1–US-13) are already landed and were not re-reviewed,
except where used as reference to confirm this story integrates cleanly with
their write paths.

Files reviewed:
- `internal/config/policy.go` (modified: exported `ContainsForbiddenFlag`, refactored `validateNoForbiddenFlag`)
- `internal/team/validate.go` (modified: `validateAgentEntry` now checks Source/Dest, removed `TODO(US-14)`)
- `internal/team/errors.go` (modified: `ErrForbiddenFlag`, `ForbiddenFlagError`, `ForbiddenFlagInIndexError`, extended `SecretDetectedError`)
- `internal/team/write.go` (modified: `Add`'s per-file loop, doc comment)
- `internal/team/doc.go` (modified: Invariants section)

Test files (`internal/team/write_test.go` and others) were not reviewed, per scope
— test engineers own their own suites in Phase 3.

Files read only to trace call graphs and confirm coverage (not modified by this
story): `internal/team/decode.go` (`joinSorted`), `internal/cli/cmd_team.go`
(`runTeamAdd`, `runTeamUpdate`, `runTeamManage`), `internal/config/purity_test.go`,
`internal/team/overrides.go`'s callers.

---

## Findings against the focus points

**1. Write-path coverage (AC-1).** Traced concretely rather than trusting the
report. `internal/cli/cmd_team.go` has exactly two functions that produce
`team.AgentRender` and pass it to a write call: `runTeamAdd` (line ~761-766,
`teamsource.RenderTeam` → `team.Add`) and `runTeamUpdate` (line ~1006-1140,
`teamsource.RenderCurrent` per record, batched into `team.AgentRender`s, then
`team.Add`). `runTeamManage` (`set`/`unset`/`show`) never renders or writes agent
content at all — it only records/reports a per-repo override pointer
(`team.SetOverride`/`UnsetOverride`/`ActiveOverride`), which is read back later at
the same `readAgentSource` call site every render already goes through (per
`cmd_team.go`'s own package doc comment and US-13's established pattern). There is
no third write path and no path that bypasses `team.Add`. AC-1's "regardless of
whether it originated from the index-resolved source or a US-13 override" holds:
an override's content only ever reaches disk via a subsequent `team add`/`team
update` call, both of which route through the single `Add` validation gate. No
gap found.

**2. `errors.Join`/fail-fast correctness.** Confirmed in both touched loops:
- `validateAgentEntry` (validate.go): Source and Dest are each checked
  independently (`if entry.Source == "" {...} else if
  config.ContainsForbiddenFlag(...) {...}`, same shape for Dest), both append to
  the local `errs` and return it; the caller (`Validate`) already appends this
  into its own `errs` slice across every team/agent and returns the full slice
  without early return. `Validate`'s caller aggregates via the existing
  `joinSorted`/`errors.Join` path in `decode.go`.
- `Add`'s per-file loop (write.go): the secret check and the new forbidden-flag
  check each `continue` (proceeding to the next file in `ar.Files`, not
  returning), and the outer `for _, ar := range renders` loop is likewise never
  exited early. `joinSorted(errs)` is called once, after every render and every
  file in every render has been checked. Verified this is a real loop `continue`,
  not a no-op — traced the exact control flow rather than trusting the report.

**3. AC-3 duplication check.** `grep` for `strings.Contains`, the flag literal
fragments, and any secret-pattern regex/string inside `internal/team` turned up
nothing beyond the two call sites into `config.ContainsForbiddenFlag` and
`config.ClassifySecret`. `internal/team/validate.go`'s only new import is
`internal/config`, consistent with the package's documented import restriction.
No duplication found.

**4. Error message leakage.** `ForbiddenFlagError.Error()` and
`ForbiddenFlagInIndexError.Error()` both only state that a forbidden flag was
found, never the matched substring. `SecretDetectedError.Error()` states the
credential `Class` (a category label such as "aws" or "generic-token", not the
matched value) — this was already the pre-existing pattern before this story;
this story only added `AgentKey`/`Harness` fields to the message, which are safe
to print (not secret material). No leakage introduced.

**5. `internal/config` behavior preservation.** The diff to `policy.go` is a
pure extraction: `ContainsForbiddenFlag(value)` wraps exactly the same
`strings.Contains(value, forbiddenFlag())` expression `validateNoForbiddenFlag`'s
`check` closure used inline before. No new import, no change to `forbiddenFlag()`,
no behavioral change to the error message text or the call sites iterated.
`go test ./internal/config/...` passes, including `TestValidationHasNoThirdPartyImports`
and `TestForbiddenFlagIsAbsentFromTheTree` (both unmodified, both still pass).

**6. General idiom/error/concurrency/resource/security/performance.** No
goroutines introduced. No new I/O resources opened in the touched code (the
per-file loop only operates on already-in-memory `f.Bytes`). Errors are typed,
wrapped via sentinel `Unwrap()` per existing package convention, and callers can
`errors.Is(err, team.ErrForbiddenFlag)`. `gofmt -l .`, `go vet ./...`, and
`go build ./...` are all clean module-wide. `go test ./internal/config/...
./internal/team/... ./internal/teamsource/...` all pass.

One minor, non-blocking performance observation is recorded below as an
advisory.

---

## BLOCKING findings

None.

---

## ADVISORY findings

### CR-A-001: Redundant `[]byte`→`string` conversion per rendered file
**File:** `internal/team/write.go:89-94`
**Story:** US-14
**Category:** Performance
**Finding:** `Add`'s per-file loop calls `config.ClassifySecret(string(f.Bytes))`
and then, on a byte-string miss, `config.ContainsForbiddenFlag(string(f.Bytes))`
— two separate `[]byte`→`string` conversions of the same underlying bytes for
every rendered file. Each conversion allocates and copies. Rendered agent
definitions are small (markdown files), so this is not a hot-path concern in
practice, but it is a needless duplicate allocation per file per `Add` call.
**Suggestion:** Convert once — `content := string(f.Bytes)` — and pass `content`
to both `config.ClassifySecret` and `config.ContainsForbiddenFlag`.

---

## Cross-story summary

US-14 integrates cleanly with US-2/US-4 (index resolution), US-11/US-12 (`team
update`'s classify-then-write flow), and US-13 (`team manage` overrides). The
single choke point the plan called for — every rendered byte passing through
`internal/team.Add` before any write lands — is real and was traced concretely
through both `runTeamAdd` and `runTeamUpdate`; there is no second write path for
`runTeamManage` to have missed, since it never renders or writes agent content
itself. The `internal/config` surface widening (`ContainsForbiddenFlag`) is
minimal, well-documented as a deliberate exception (DEV-2), and the refactor of
`validateNoForbiddenFlag` to use it is behavior-preserving. The
`errors.Join`/aggregate-don't-fail-fast pattern — called out as having been
BLOCKING three times before on this issue — was verified by tracing actual
control flow in both touched loops, not merely checked for the presence of an
`errs` slice or a comment.

---

## Verdict

CLEAN — No blocking findings. One advisory noted (CR-A-001, performance,
non-blocking). Proceeding to environment setup.
