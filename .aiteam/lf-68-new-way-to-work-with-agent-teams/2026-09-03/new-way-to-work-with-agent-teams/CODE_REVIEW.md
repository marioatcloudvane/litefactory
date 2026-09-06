# Code Review
# Feature: Configurable Agent Teams — US-10 (`team status` drift detection)
# Reviewed by: go-code-reviewer
# Date: 2026-09-06 (re-review, cycle 2)
# Status: ADVISORY_ONLY

---

## Scope (cycle 2 — re-review)

This is a scoped re-review verifying the engineer's fix for the sole cycle-1 BLOCKING
finding, CR-B-001, in `internal/cli/cmd_team.go`'s `runTeamStatus`. Per the
re-review contract, only this file (plus its immediate callers/callees needed to
confirm the fix didn't break a cross-story contract) was re-read.
`internal/team/status.go`, `internal/team/status_test.go`, and
`internal/teamsource/render.go` were not re-reviewed — cycle 1 found no BLOCKING
issues in them and the fix did not touch them.

Independently re-verified (not just trusting the engineer's self-report):
```
go build ./...                                                                    # OK
go vet ./...                                                                      # OK, no findings
gofmt -l internal/cli/cmd_team.go                                                 # no output — clean
go test -count=1 ./internal/cli/... ./internal/team/... ./internal/teamsource/... # all pass
```

---

## CR-B-001 — RESOLVED

**File:** `internal/cli/cmd_team.go:686-805` (`runTeamStatus`)
**Story:** US-10 / T-28

**Original finding:** The loop over `lf.Records` returned immediately from inside
the loop on the first `renderErr` or `classifyErr`, aborting the entire command —
no report printed, every other lockfile record silently dropped. This undermined
AC-1 (every tracked file classified) and AC-3 (a record whose file is missing must
be classified Missing, not silently skipped), and deviated from this codebase's
aggregate-via-`errors.Join` convention.

**Verification of fix:**

1. **The loop no longer returns early.** Both failure sites (`teamsource.RenderCurrent`
   returning `renderErr`, and `team.Classify` returning `classifyErr`) now wrap the
   error with context, append it to a new `recordErrs []error` slice, set
   `inSync = false`, increment `drifted`, append a `teamStatusRecordJSON` entry with
   `Status: "error"` and `Note` set to the wrapped error's text, and `continue` to the
   next record. Confirmed by direct reading of lines 719-763.

2. **No record is silently dropped.** `records` is built with exactly one `append`
   call per code path (render-error, classify-error, success), and every one of
   those three paths is reached exactly once per loop iteration before the loop
   advances (via `continue` or falling through to the bottom). `records` therefore
   always ends up with `len(lf.Records)` entries, for both the JSON and table
   renderers, which both iterate `v.Records` unconditionally. Confirmed AC-1 (every
   tracked file gets exactly one classification/outcome) and AC-3 (a record backed
   by a missing/unrenderable file still gets its own report entry rather than being
   dropped) now hold per-record, independent of any other record's outcome.

3. **Exit-code contract preserved.** The function still returns a plain `error`
   (via `errors.Join(append(recordErrs, summaryErr)...)`) when `!inSync`, and `nil`
   only when every record classified as `team.StatusInSync` with no render/classify
   errors. Never wraps the result in `UsageError` or `PreconditionError` — confirmed
   by reading the full function body; the only other early returns
   (`resolveTeamRepoPath`, `openTeamRuntime`, `resolveTeamIndex`, `LoadLockfile`
   with the `team.ErrNoLockfile` exception) are unchanged from cycle 1 and were
   already reviewed clean.

4. **`errors.Join` usage matches the codebase convention.** This mirrors
   `internal/teamsource/render.go`'s `RenderTeam`, which collects per-roster-entry
   errors into a slice and joins them at the end rather than failing fast — the
   same pattern CR-B-001 asked for. The per-record errors are joined together with
   the pre-existing summary error (`"%d of %d tracked file(s) are not in sync"`),
   so no error information is lost or swallowed; it's aggregation, not silencing.

5. **Nothing else in the file was touched beyond what the fix required.** Diffed
   the current `runTeamStatus`, `writeTeamStatusJSON`, `writeTeamStatusTable`,
   `teamStatusRecordJSON`, `teamStatusJSON`, `teamStatusNote`, and
   `newTeamStatusCommand` against what cycle 1 reviewed clean: JSON/table output
   shape, sorting (`sort.Slice` by `DestPath`/`AgentKey`/`Harness`), the
   hand-edit-risk note logic (scoped to Locally modified/Conflict only), and the
   `team.ErrNoLockfile` tolerance are all unchanged.

6. **Build/vet/gofmt/test independently re-run** — all clean, as shown above.

**Verdict: CR-B-001 is resolved.** No re-opening required.

---

## New findings from this fix

No new BLOCKING issues were introduced by the fix.

### CR-A-002: `team status --json`/table can now emit a sixth "error" outcome not mentioned in the command's documented five-state model
**File:** `internal/cli/cmd_team.go:730, 760` (inside `runTeamStatus`); `internal/cli/cmd_team.go:887-893` (`newTeamStatusCommand`'s `Long` text)
**Story:** US-10 / T-28
**Category:** Standards / Documentation consistency
**Finding:** The fix's per-record failure entries use a literal `Status: "error"` string that is distinct from all five `team.Status.String()` values (`"in sync"`, `"locally modified"`, `"upstream updated"`, `"conflict"`, `"missing"`) — correctly avoiding collision, which is good — but `newTeamStatusCommand`'s `Long` help text still says the command classifies files "into one of five states — in sync, locally modified, upstream updated, conflict, or missing", and `runTeamStatus`'s own doc comment doesn't mention this sixth possible outcome either. A user hitting a render/classify failure (e.g. a deleted source file with a still-tracked lockfile record for a different, unrelated issue than "Missing") will see a status value the command's own `--help` text doesn't describe.
**Suggestion:** Either fold this failure into `team.StatusMissing` semantics at the CLI layer where appropriate, or update the `Long` text and `runTeamStatus`'s doc comment to mention that a record whose current source can no longer be re-rendered or classified is reported as `"error"` with a note explaining why, alongside the five drift states. Not required before proceeding — this is a documentation-completeness gap, not a functional defect — but worth folding in.

---

## Preserved ADVISORY findings from cycle 1

### CR-A-001: `RenderCurrent` is called once per lockfile record with no de-duplication across records sharing the same `(harness, sourcePath)`
**File:** `internal/cli/cmd_team.go:720` (inside `runTeamStatus`'s loop; line shifted from cycle 1's `:712` due to the fix's added comment block, content unchanged)
**Story:** US-10 / T-28
**Category:** Performance
**Finding:** A shared agent installed across multiple teams produces multiple `LockRecord`s with the
same `(rec.Harness, rec.SourcePath)` pair but different `Teams`/`DestPath`. `runTeamStatus` calls
`teamsource.RenderCurrent` independently for each such record, re-reading and re-rendering the same
source markdown redundantly.
**Suggestion:** Cache `RenderCurrent`'s result keyed by `(harness, sourcePath)` within one
`runTeamStatus` invocation and reuse it across lockfile records that share the pair. Not required
before proceeding — `team status` is an interactive CLI command against a small lockfile, not a hot
service path — but worth folding in if the lockfile is expected to grow large.

---

## Cross-story summary

The fix isolates each lockfile record's outcome to its own report entry and no longer allows one
problematic record to suppress the report for every other tracked record, closing the gap CR-B-001
identified between `runTeamStatus`'s per-record loop and `RenderTeam`'s established
aggregate-then-report convention in the same call graph. AC-1 and AC-3's per-record guarantees now
hold even when a record's current source has become unrenderable or unclassifiable. The exit-code
contract, `--json`/table output shape, sorting, hand-edit-risk note scoping, and `ErrNoLockfile`
tolerance are all unchanged and remain correct. The only residual items are two ADVISORY-level notes
(a minor documentation gap around the new "error" outcome, and the pre-existing per-record
`RenderCurrent` de-duplication opportunity) — neither blocks proceeding to environment setup.

---

## Verdict

ADVISORY_ONLY — CR-B-001 resolved; 2 advisory suggestions noted (CR-A-001 carried over, CR-A-002 new).
Proceeding to environment setup.
