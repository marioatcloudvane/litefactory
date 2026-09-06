# Code Review
# Feature: Configurable Agent Teams — US-9 (`team add` write engine) + US-18 (root-file composition)
# Reviewed by: go-code-reviewer
# Date: 2026-09-06
# Status: ADVISORY_ONLY (re-review cycle 1: CR-B-001 resolved; see Re-review section below)

---

## Scope

Reviewed all files under `internal/team/` implementing the combined US-9/US-18 delivery:
`write.go`, `write_types.go`, `lockfile.go`, `lock.go`, `paths.go`, `writefile.go`, `marker.go`,
`hash.go`, `errors.go`, `doc.go`, plus their test files (`write_test.go`, `marker_test.go`,
`lockfile_test.go`) read for coverage confirmation only, not for defects (test files are out of
review scope per instructions).

Per explicit scope boundaries given for this review, the following are **not** findings:
- Missing CLI wiring (`cmd_team.go`, `internal/cli/*`) — deferred to US-4.
- No `team update` caller for `removeMarkerSection` — trigger belongs to US-11.
- No `<repo-identifier>` resolution (US-9 T-20) — deferred to US-19's resolver via US-4.

---

## BLOCKING findings

### CR-B-001: `Add`'s validation loop fails fast instead of aggregating errors via `errors.Join`
**File:** `internal/team/write.go:67-111` (specifically the four fail-fast exits at lines 68-70,
71-73, 75-77, 78-80)
**Story:** US-9 / T-23
**Category:** Error handling / Cross-story consistency with plan_brief.md
**Finding:** `plan_brief.md`'s Cross-Story Constraints state this requirement twice, in terms that
name `team add` specifically:

- Platform section: "Validation aggregates *all* problems via `errors.Join` in one pass (CFG-5
  precedent) — never fail on the first error found."
- Security section: "Errors name the offending path and dotted field, never the offending value
  (secret leakage risk), aggregated via `errors.Join`" — in a passage explicitly scoped to "every
  write path (`team add`, `team update`, `team manage` overrides)".

`Add`'s validation loop over `renders`/`Files` does the opposite: on the first invalid harness
(line 68-70), invalid agent key (71-73), unowned path (75-77), or detected secret (78-80), it
returns immediately with `return nil, err`, discarding any other validation problems elsewhere in
the same batch. `doc.go`'s own description of `Add` ("A validation failure anywhere in the batch
aborts the whole call with nothing written") documents this fail-fast behavior as intentional, but
that description contradicts the plan's explicit, twice-stated instruction for this exact code
path, and no rationale is recorded anywhere for the deviation. The existing test
(`TestAddValidationFailureWritesNothing`) only ever puts one invalid render in the batch, so it
cannot and does not distinguish "fails fast" from "aggregates and reports all, but still writes
nothing" — it validates the *atomicity* guarantee (AC-1), not the *error-reporting* guarantee the
plan calls for.

Practical harm: an operator whose team has multiple problems (e.g. two agents both under
unowned paths, or one bad harness and one literal secret) sees only the first, fixes it, reruns,
and only then discovers the second — exactly the fix-and-rerun churn `errors.Join` aggregation
exists to prevent, per the CFG-5 precedent already used by this same package's own index decoder
and validator (as `doc.go` itself describes for those).

**Required fix:** Collect validation errors across the full loop (harness, key pattern, owned
path, secret) into a slice and return `errors.Join(errs...)` once the batch has been fully walked,
rather than returning on the first failure — mirroring the CFG-5 pattern already established
elsewhere in this package for index decode/validate. Update `doc.go`'s "The write engine: Add"
section to describe the corrected behavior, and add a test exercising a batch with two or more
independent validation failures that asserts all of them are present in the joined error (via
repeated `errors.As`/`errors.Is`) and that nothing was written.

---

## ADVISORY findings

### CR-A-001: `lock.go`'s symlink check duplicates a narrower version of `writefile.go`'s `refuseSymlink`
**File:** `internal/team/lock.go:38-44`
**Story:** US-9 / T-23
**Category:** Standards (duplicated logic)
**Finding:** `acquireRepoLock` re-implements its own symlink check inline (`os.Lstat` on the lock
file path plus `!info.Mode().IsRegular()`), rather than reusing `refuseSymlink` from
`writefile.go`, which walks every path component from root rather than just the final target. The
lock file lives under the package-controlled `.litefactory/` directory (lower attacker-influence
than an Owned Path under a harness directory), so this is not a security defect, but it is
independently-maintained logic doing a similar job with a narrower guarantee.
**Suggestion:** Reuse `refuseSymlink(root, lockRelPath)` in `acquireRepoLock` instead of the
inline check, for one canonical symlink-refusal implementation across the package.

### CR-A-002: `Add` takes no `context.Context`, though plan_brief describes a `ctx.Err()` check for write paths
**File:** `internal/team/write.go:49`
**Story:** US-9 / T-23
**Category:** Cross-story consistency
**Finding:** `plan_brief.md`'s Platform Cross-Story Constraints describe `ctx.Err()` being checked
exactly once, immediately before the write loop begins, for write-path operations. `Add`'s actual
signature (`func Add(root string, renders []AgentRender) (*AddResult, error)`) has no context
parameter, so there is nothing to check. Given this story's writes are local, synchronous file
I/O with no external calls, the absence is unlikely to matter functionally, and the eventual CLI
caller (US-4, out of scope here) may be the intended place to thread a context through. Flagging
for the orchestrator's awareness rather than as a defect in these files.
**Suggestion:** When US-4 wires the CLI caller, confirm whether `Add` should accept a
`context.Context` for cancellation, or whether the plan's Platform constraint was written with a
different write path in mind and doesn't apply here.

---

## Checklist walkthrough (summary)

1. **Cross-story consistency (US-9 ⟷ US-18):** Clean. `marker.go`'s `markerSpanHash` populates the
   exact same `LockRecord.RenderedHash` field standalone files populate via `hashBytes` — confirmed
   directly in `write.go:131`. `Add` correctly routes root-file targets (`isRootFileTarget`) through
   marker upsert (`rootCandidates`/`changedSections`) and standalone renders through
   `writeFileAtomic`, both inside the same validate-all-then-write-all batch, both landing before
   `lf.save` at the end. No signature or type mismatches found between the two stories' code.
2. **Security:** Path validation via re-derivation-and-exact-match (`validateOwnedPath`), symlink
   refusal via `os.Lstat` walking every path component (`refuseSymlink`) for both standalone and
   root-file targets, atomic temp-file+rename writes, lockfile written last. `config.ClassifySecret`
   wired into every write path with no exemptions, matching the plan's "no exemptions" language.
   `SecretDetectedError{Path, Class}` carries no raw-value field — CFG-4 compliant. No `.Chmod()`
   calls anywhere — SEC-10 compliant. No secret-scan/allowlist integration beyond `ClassifySecret`
   found, consistent with US-14 being out of scope.
3. **Error handling:** All sentinels wrapped/unwrapped correctly, `errors.Is`/`errors.As` used
   throughout, no `==` comparisons. The one deviation from the plan's `errors.Join` aggregation
   requirement is CR-B-001 above.
4. **Concurrency safety:** Per-repo `flock` (blocking `LOCK_EX`) in `lock.go` correctly serializes
   concurrent `Add` calls; validated by `TestAddSerializesConcurrentCalls` (8 goroutines, exactly 1
   resulting record, no torn/duplicate state). No concurrency introduced beyond this single lock.
5. **Permission bits / gosec:** `writefile.go` creates via `O_EXCL` + explicit mode argument, no
   `.Chmod()` anywhere in the package.
6. **Acceptance criteria:**
   - US-9 AC-1 (write-all-or-nothing): satisfied — `Add` validates before writing anything and
     aborts with nothing written on failure (`TestAddValidationFailureWritesNothing`); note the
     fail-fast *mechanism* used to satisfy this is exactly what CR-B-001 flags as needing to also
     aggregate errors, not just abort cleanly.
   - US-9 AC-2 (lockfile record with sourceHash/renderedHash): satisfied —
     `TestAddWritesStandaloneFileAndLockRecord`, `TestLockfileSaveAndLoadRoundTrip`.
   - US-9 AC-3 (concurrent calls don't interleave/tear records): satisfied —
     `TestAddSerializesConcurrentCalls`.
   - US-9 AC-4 (idempotent re-add is a no-op): satisfied —
     `TestAddIsIdempotentNoOpForStandaloneFile`, `TestAddUpsertsRootFileSectionAndIsIdempotent`.
   - US-18 AC-1 (first write appends, second write replaces in place byte-identical outside):
     satisfied — `TestUpsertMarkerSectionAppendsToEmptyContent`,
     `TestUpsertMarkerSectionAppendsAfterExistingContent`,
     `TestUpsertMarkerSectionReplacesInPlaceLeavingSurroundingContentUntouched`.
   - US-18 AC-2 (shared agent → one deduplicated block listing all contributing teams):
     satisfied — `TestUpsertMarkerSectionSortsAndDedupsTeamList`, `TestAddDedupsSharedAgentAcrossTeams`.
   - US-18 AC-3 (orphaned block removed on next `team update`): the primitive
     (`removeMarkerSection`) is implemented and tested
     (`TestRemoveMarkerSectionRemovesSpanAndCollapsesBlankLines`,
     `TestRemoveMarkerSectionNotFound`); the `team update` caller is explicitly out of scope
     (US-11) per this review's instructions — no gap within scope.
   - US-18 AC-4 (drift classification computed only over the marker span): the foundation
     (`markerSpanHash` hashing only the BEGIN line + body + END line) is implemented and tested
     (`TestMarkerSpanHashChangesWithTeamListEvenIfBodyUnchanged`,
     `TestMarkerSpanHashStableUnderTeamOrderAndDuplicates`); actual five-state drift classification
     is US-10's concern and correctly not implemented here.
7. **`doc.go`:** Documents the write engine, lockfile shape, marker/virtual-file hashing scheme,
   and the package's import invariant clearly, in line with the "doc.go is the real spec"
   convention — with the one correction needed being the `Add` validation-behavior paragraph per
   CR-B-001.
8. **Idiom/style:** `gofmt -l` and `go vet` report nothing against the package. No stray
   `fmt.Print*`/`log.*` calls. No dead code, `TODO`/`FIXME` markers found in the reviewed files.

---

## Cross-story summary

US-9 and US-18 integrate cleanly at their shared boundary: `marker.go`'s virtual-file hashing
scheme was built to feed the exact `LockRecord` shape `lockfile.go` defines, confirmed directly in
`write.go` rather than inferred, and `Add` treats standalone files and root-file sections
uniformly within one validate-then-write-all batch, which is the core atomicity guarantee both
stories depend on. The package's own import-boundary discipline (only `internal/config`,
documented and justified in both `write_types.go` and `lock.go`) is respected throughout. The one
genuine risk at this boundary is not between the two stories but between the implementation and
the plan itself: `Add`'s fail-fast validation loop (CR-B-001) satisfies the atomicity half of the
plan's requirement (nothing is written on failure) but not the error-completeness half (report
every problem in the batch, not just the first), and this gap is precise and mechanically
actionable to fix without touching either story's public surface.

---

## Verdict

BLOCKING — 1 issue must be resolved before testing. Routing to the engineer responsible for
US-9 T-23 (`internal/team/write.go`, `internal/team/doc.go`).

---

## Re-review (cycle 1)

**Date:** 2026-09-06
**Scope:** `internal/team/write.go`, `internal/team/doc.go`, `internal/team/write_test.go` — the
files touched to fix CR-B-001. The two prior advisory findings (CR-A-001, CR-A-002) were not
required to be fixed and were not re-checked; they stand as originally recorded above.

### CR-B-001: RESOLVED

`write.go`'s validation loop (lines 66-124) now collects every problem — invalid harness (line
74-77), invalid agent key (78-81), unowned path (83-86), detected secret (87-90) — into a local
`errs []error` slice via `append` + `continue`, rather than returning on the first one. Confirmed
the inner `continue` on a per-file error only skips to the next file in the same render's `Files`
loop (not the whole render or the whole batch), and the outer per-render `continue`s (invalid
harness, invalid agent key) correctly move to the next `AgentRender` rather than aborting the whole
call. After the full walk, `joinSorted(errs)` (the same helper `decode.go` already uses for the
same purpose — sorts by message text, then `errors.Join`) is called once; if non-nil, `Add` returns
immediately at line 122-124, before `result := &AddResult{}` or any of the write-phase code that
follows. No writes, no lockfile save, occur on any validation failure — the write phase is
unreachable when `errs` is non-empty. This matches the plan's twice-stated `errors.Join`
aggregation requirement (Platform and Security sections) for `team add`.

Verified `errors.Is`/`errors.As` resolve correctly against the joined error: `errors.Join`'s
returned value implements `Unwrap() []error`, which `errors.Is`/`errors.As` traverse natively
(Go 1.20+) across every leaf in the tree, independent of `joinSorted`'s sort step (sorting only
reorders `errs` before joining, it doesn't change which sentinels are reachable). Read
`errors.go`'s sentinel definitions directly: `UnsupportedHarnessError.Unwrap() → ErrUnsupportedHarness`
and `PathNotOwnedError.Unwrap() → ErrPathNotOwned`, both distinct chains, both reachable
independently through the joined tree.

`doc.go`'s "The write engine: Add" section (lines 55-75) now reads: "Every problem found across the
whole batch is collected as validation proceeds and returned together via errors.Join, rather than
stopping at the first one, so a caller with several bad renders in one call sees every problem at
once. A validation failure anywhere in the batch still aborts the whole call with nothing written."
The prior fail-fast-as-intentional language is gone; the new text accurately describes the
collect-all/write-nothing-on-any-failure behavior now in `write.go`.

The new test, `TestAddAggregatesAllValidationFailuresInOneBatch` (write_test.go:180-233), batches
two renders with two independent, non-overlapping validation problems: `badHarness` (harness
`"not-a-real-harness"`) and `badPath` (path `.claude/agents/someone-else.md`, not the owned path
for agent `go-tester`/`claude`). It asserts `errors.As` succeeds for both `*UnsupportedHarnessError`
and `*PathNotOwnedError`, `errors.Is(err, ErrPathNotOwned)` is true, and — the part that actually
proves completeness rather than just "some error came back" — that `err.Error()` contains both
`badHarness.Harness` and `badPath.Files[0].Path` as substrings, which only holds if both individual
errors survived into the final joined message rather than one shadowing the other. It also asserts
neither the standalone file for the valid-shaped `go-tester` render nor the lockfile was written.
This is a materially different test from the pre-existing
`TestAddValidationFailureWritesNothing` (one bad render + one good render, asserts only atomicity,
never asserted completeness) — it exercises the exact gap CR-B-001 identified.

Independently ran (not just trusting the engineer's report):

```
go build ./...                                    # OK
go vet ./...                                       # OK
go test -race -count=1 ./internal/team/...         # ok  	github.com/cloudvane/litefactory/internal/team	1.499s
gofmt -l internal/team                             # no output — clean
```

Also ran the new test in isolation (`-run TestAddAggregatesAllValidationFailuresInOneBatch -race
-v`) to confirm it exercises the intended path rather than passing vacuously: PASS.

No new problem introduced by the fix. `standaloneWrite`/`rootSection` candidate-building still runs
inline in the same loop as validation (unchanged from before), but since nothing derived from
`candidates`/`rootCandidates` is read before the `joinSorted` check, this doesn't leak partially-
validated state into the write phase.

---

## Verdict (re-review, cycle 1)

CLEAN — CR-B-001 is resolved. No new BLOCKING findings introduced by the fix. The two prior
ADVISORY findings (CR-A-001, CR-A-002) remain outstanding as advisory-only, unchanged. Proceeding
recommendation: environment setup / Phase 2.
