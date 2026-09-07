# Code Review
# Feature: US-11 — `team update` (single-worktree scope)
# Reviewed by: go-code-reviewer
# Date: 2026-09-07
# Status: ADVISORY_ONLY

---

## BLOCKING findings

None.

Every item called out for explicit verification came back clean:

1. **`errors.Join` isolate-and-continue aggregation is real.** `runTeamUpdate`'s
   per-record loop (`internal/cli/cmd_team.go:921-1210`) uses `continue` on
   every failure branch (`renderErr`, `classifyErr`, the
   `include && !freshFound` anomaly guard) and never `return`s early out of
   the loop. All accumulated `recordErrs` are joined with the summary line at
   the very end via `errors.Join(append(recordErrs, summary)...)`. This is the
   exact pattern whose absence caused two prior BLOCKING findings on this
   issue (run 20's `Add()` validation loop, run 25's `runTeamStatus` loop) —
   confirmed fixed/present here.
2. **`--force` behavior matches AC-2.** `teamUpdateNote` produces the exact
   text `"was %s; overwritten because --force was passed"` for the
   `Overwritten` action, and the switch in `runTeamUpdate` only sets
   `action = teamUpdateActionOverwritten` for `StatusLocallyModified` /
   `StatusConflict` when `force` is true; otherwise it refuses
   (`teamUpdateActionRefused`) and leaves the file untouched. Verified
   end-to-end by `TestTeamUpdate_ForceOverwritesAndNamesWhatItDiscarded`,
   which asserts the literal note string and that hand-edited content is
   actually gone afterward.
3. **AC-3 (single-worktree scope) is genuinely enforced and tested.**
   `TestTeamUpdate_SingleWorktreeScope` creates a real second git worktree via
   `repo.Git("worktree", "add", ...)`, captures its full state before and
   after running `team update --force` against the primary worktree, and
   asserts the diff is empty. This is a real integration check, not an
   assertion resting on code reading alone.
4. **No `--migrate-config` or `--all-worktrees` flag exists anywhere.**
   Confirmed via grep across `internal/cli/*.go` and by reading
   `newTeamUpdateCommand()` directly (`internal/cli/cmd_team.go:1317-1344`):
   only `--repo`, `--force`, `--json` are registered.
5. **The `write.go` dedup fix is sound and does not regress `team add`'s
   idempotency guarantee.** Read `internal/team/write.go` in full and traced
   both the standalone-file and root-file-section paths. The old code treated
   a lockfile-hash match (`existing.matches(c.record)`) alone as sufficient to
   skip a write. Because a "Locally modified" record has unchanged
   `SourceHash`/`RenderedHash` (only the on-disk bytes were hand-edited), the
   old check would wrongly no-op it even under `--force` — this was the root
   cause the fix addresses. The new code adds a genuine `os.ReadFile` +
   `bytes.Equal` comparison against the actual on-disk bytes (or, for root
   files, `locateMarkerSection` + exact span comparison) before deciding to
   skip. For a true no-op (in-sync file), the comparison still passes and the
   skip/idempotency behavior other callers (`team add` re-runs) depend on is
   preserved. For Missing files, `os.ReadFile` fails and correctly falls
   through to a real write (restoration). This was verified by reading the
   code and reasoning through all three cases, not by trusting the report.
6. **Exit code mapping matches Shared Decision 2.** `runTeamUpdate`'s
   refusal path returns a plain `error` via `errors.Join`, never a
   `PreconditionError`/`UsageError`. `internal/cli/errors.go`'s `ExitCode`
   dispatcher maps a plain error to `ExitFailure` (1). Confirmed against
   `TestTeamUpdate_RefusesLocalModificationsByDefault`'s own assertion
   (`code == ExitFailure`).
7. **Package-boundary contract (`cli -> teamsource -> team`) is respected.**
   `internal/team`'s non-test files import only
   `github.com/cloudvane/litefactory/internal/config` from litefactory's
   internal packages (grep-confirmed). Rendering/hashing logic for the update
   path lives in `internal/teamsource.RenderCurrent`
   (`internal/teamsource/render.go:147-169`), not in `internal/cli`.
8. **`removeMarkerSection` non-invocation claim is structurally correct.**
   `teamsource.RenderCurrent`'s doc comment and implementation confirm it
   re-renders only the exact recorded `sourcePath`, with no roster/
   team-membership re-resolution — it has no way to discover that a team has
   stopped contributing a shared agent. Since US-11's Architecture Notes
   scope `team update` to reusing US-10's five-state classification model
   (In sync / Upstream updated / Missing / Locally modified / Conflict), none
   of which represents "orphaned by team membership," this is an inherent
   scope limitation correctly inherited from US-10, not a gap introduced by
   this story. See CR-A-001 below for a related but non-blocking documentation
   issue this surfaced.
9. **Build/vet/format/race-test verification, run directly (not trusted from
   the report):**
   - `gofmt -l .` — no output (clean)
   - `go vet ./...` — no output (clean)
   - `go build ./...` — clean
   - `go test -race ./internal/team/... ./internal/cli/...` — both `ok`
   - `go test -race ./...` (full repo) — all packages `ok`, no failures
   - No `fmt.Println`/`fmt.Printf`/stray `log.*` calls in `cmd_team.go`; all
     output goes through `env.Stdout` via `fmt.Fprintf`/`tabwriter`.
   - No `go func(`/goroutines introduced anywhere in the touched files —
     satisfies the "no concurrency introduced" constraint.
   - `writeFileAtomic` (`internal/team/writefile.go:63-117`, unmodified by
     this story) passes `mode` straight to `os.OpenFile`'s `O_CREATE|
     O_WRONLY|O_EXCL` rather than calling `Chmod`, satisfying the SEC-10/
     gosec permission-bit convention; this story's diff never touches file
     permissions.
   - Checked whether the dedup fix's new pre-write `os.ReadFile` calls (used
     only to decide skip-vs-write) create any symlink exposure ahead of the
     existing `refuseSymlink` guard on the actual write. They do not: the
     read result is used solely for a boolean comparison, never written back,
     and every actual `writeFileAtomic` call is still preceded by
     `refuseSymlink` (`internal/team/write.go:186-189`, `206-209`). If an
     Owned Path were somehow already a symlink, the read either matches
     `c.body` (skip, no write — the symlink was already present before this
     call ran, not created by it) or doesn't match (falls through to the
     write path, where `refuseSymlink` correctly blocks it). No write-bypass
     is possible through this path.

---

## ADVISORY findings

### CR-A-001: Stale doc comment names this story as `removeMarkerSection`'s caller
**File:** `internal/team/marker.go:163-168`
**Story:** US-11 (comment originates from US-18's marker.go)
**Category:** Standards / documentation accuracy
**Finding:** `removeMarkerSection`'s doc comment reads: "removes agentKey's
marker section from content, for a future removal story (US-11) to call." US-11
has now been implemented (this review) and, correctly per its actual
Architecture Notes and acceptance criteria, does not call
`removeMarkerSection` — `team update`'s scope is limited to US-10's per-file
five-state classification, which has no state for "a team stopped
contributing this shared agent." The comment's forward reference to "US-11"
was evidently a placeholder guess made when `marker.go` was written (US-18)
that no longer matches which story actually consumes the primitive, if any
story yet does.
**Suggestion:** Update the comment to drop the specific `(US-11)` reference
(e.g., "for a future team-removal/prune story to call") so a future engineer
searching for "US-11" as the integration point for this primitive isn't sent
looking for a call site that was never in scope.

---

## Cross-story summary

This story integrates cleanly at every boundary it touches. It adds no new
write path — `runTeamUpdate` composes `teamsource.RenderCurrent`,
`team.Classify` (both pre-existing, from US-10/prior work), and `team.Add`
(the same write engine `team add` uses), exactly as its own package doc
comment claims. The one shared-engine change this story required —
`internal/team/write.go`'s dedup-check fix — was necessary because `team.Add`
previously could not distinguish a genuine no-op from a Locally-modified or
Missing record, which would have silently defeated `--force` for `team
update`'s core use case; the fix is scoped tightly (byte comparison added
only at the skip-decision point) and verified not to regress `team add`'s own
idempotent-rerun behavior. The `HashBytes` export (unexported `hashBytes` ->
`HashBytes`) needed for `teamsource` to call it is applied consistently
project-wide with no leftover unexported references. Exit-code, error-
aggregation, and package-boundary conventions established by earlier stories
in this feature are all followed without deviation. The single ADVISORY
finding is documentation-only and does not affect behavior or test coverage.

---

## Verdict

ADVISORY_ONLY — 1 suggestion noted (CR-A-001, documentation only). No BLOCKING
issues. Proceeding to environment setup.
