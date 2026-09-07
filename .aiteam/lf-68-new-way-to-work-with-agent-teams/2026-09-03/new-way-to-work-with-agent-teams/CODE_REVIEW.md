# Code Review
# Feature: Configurable Agent Teams — US-16 (gitignore + write guard) & US-17 (go-team bootstrap)
# Reviewed by: go-code-reviewer
# Date: 2026-09-07
# Status: ADVISORY_ONLY

---

## Scope and method

Reviewed the actual current, uncommitted working-tree state (not the task reports) for both
stories, against `plan_brief.md`'s cross-story constraints and each story's own `US-16.md`/
`US-17.md` acceptance criteria. All claims in the "what the engineer reports having built" section
of the routing prompt were independently verified against source rather than trusted:

- Read every file `git status` reports changed/added for both stories.
- Ran `go build ./...`, `go vet ./...`, `gofmt -l .` — all clean, zero output.
- Ran `go test -race ./...` across all 26 packages (not just the two stories' own tests) — all
  green, no failures, no data races reported.
- Confirmed `app-context-updater.md` does not exist in `.claude/agents/` (only 12 files present),
  corroborating the engineer's deviation note and `MANIFEST.md`'s run-30 record that the agent was
  retired.
- Byte-diffed all 12 copied markdown files under `internal/team/embedded/teams/{go-team,shared}/`
  against their `.claude/agents/` originals — identical, confirming AC-3's "copied from" claim.
- Traced `internal/team/embedded.go`'s `EmbeddedFS()` root (`embedded/teams`) against
  `internal/teamsource/resolve.go`'s tier-2/tier-3 root (`<root>/teams`) to confirm the `source`
  field's no-`teams/`-prefix convention is consistent across all three index tiers, not a
  US-17-only quirk — confirmed correct via `TestEmbeddedSourcesAllResolve`, which reads every
  `Source` path through the same `fs.FS` production code uses.
- Traced the `--all-worktrees` propagation path (`cmd_team_worktrees.go` →
  `teamUpdateWorktree` → `team.Add`) to confirm US-16's gitignore hook, wired into
  `acquireRepoLock` (called by every `Add`/`SetOverride`/`UnsetOverride`), fires per worktree
  without needing its own dedicated worktree-discovery call.
- Confirmed no file outside the two stories' declared scope was touched (`git diff --stat` against
  `internal/team/write_types.go`, `hash.go`, `marker.go`, `validate.go`, `decode.go`, `types.go`,
  `status.go`, `migrate.go` — none touched).

---

## BLOCKING findings

None.

---

## ADVISORY findings

### CR-A-001: `doc.go` not updated for US-16's new write-guard and gitignore mechanics
**File:** `internal/team/doc.go`
**Story:** US-16 / T-46
**Category:** Standards
**Finding:** `plan_brief.md`'s Cross-Story Constraints require `internal/team`'s `doc.go` to be
"the real spec" for the package, but it documents only the index schema, `Add`'s validation/write
flow, the lockfile, and marker-span hashing. It says nothing about `isOwnedWriteTarget` (the
runtime write guard added by T-46) or `ensureGitignoreEntry`/the `.gitignore` management added by
T-45, even though both are now permanent, load-bearing package behavior invoked from every
`Add`/`SetOverride`/`UnsetOverride` call.
**Suggestion:** Add a short section to `doc.go` describing the Owned Paths write guard
(`isOwnedWriteTarget`, checked inside `writeFileAtomic`) and the `.gitignore` auto-append
(`ensureGitignoreEntry`, hooked into `acquireRepoLock`), mirroring the level of detail already
given to the lockfile and marker-span sections.

### CR-A-002: US-16's "reuse US-15 worktree discovery" architecture note is satisfied implicitly, not explicitly
**File:** `internal/team/lock.go`, `internal/cli/cmd_team_worktrees.go`
**Story:** US-16 / T-45
**Category:** Consistency
**Finding:** `US-16.md`'s Architecture Notes state this story "reuses US-15's live
worktree-discovery logic (T-41) rather than inventing a second way to enumerate worktrees, and
confirms the rule is applied consistently in every worktree it runs against." The shipped
implementation does not call `discoverSiblingWorktrees` from anywhere in `internal/team`; instead
it hooks `ensureGitignoreEntry` into `acquireRepoLock`, which every `Add` call (including each
per-worktree `Add` inside `runTeamUpdateAllWorktrees`'s loop) already passes through. This is
verified correct by trace and by `TestGitignoreEntryAppliesUniformlyAcrossMultipleWorktrees` — it
achieves AC-1 without duplicating discovery logic, arguably better than the note's literal
suggestion — but it is a different mechanism than the one the story doc describes, and nothing in
the code or tests calls this out as a deliberate deviation from the architecture note's wording.
**Suggestion:** Add a one-line comment (in `lock.go`'s `acquireRepoLock` doc comment, which already
explains the choke-point reasoning) noting explicitly that this achieves the "every worktree"
requirement by being downstream of every `Add` call rather than by calling
`discoverSiblingWorktrees` directly, so a future reader doesn't go looking for a call that isn't
there.

### CR-A-003: `US-17.md`'s AC-1/AC-2 wording ("exactly 8 shared agents") is stale relative to the shipped index
**File:** `.aiteam/lf-68-new-way-to-work-with-agent-teams/2026-09-03/new-way-to-work-with-agent-teams/US-17.md`
**Story:** US-17
**Category:** Consistency
**Finding:** `US-17.md`'s Acceptance Criteria table (AC-1) and worked example still say "the 8
generic/orchestration agents," but `app-context-updater` has no surviving `.claude/agents/` source
and the shipped index correctly ships 7 shared agents (12 total, not 13). The routing prompt for
this review already carries the corrected AC-1 wording ("exactly the generic/orchestration agents
that actually still exist as agent definitions in this repo"), and the implementation, tests
(`embedded_test.go`, `cmd_team_embedded_test.go`), and `embedded.go`'s doc comment are all
internally consistent with the corrected count — this is not a code defect. It is a planning-doc
staleness gap: `US-17.md` itself was never updated to record the deviation, unlike
`plan_brief.md`'s AC_AMENDMENTS.md convention for exactly this kind of "the plan said X, reality is
Y" correction.
**Suggestion:** Not this run's job to fix (out of Implement mode's remit to edit `$session` plan
artifacts), but worth flagging back to Plan/Research bookkeeping so a future reader of `US-17.md`
alone isn't misled into thinking the shipped index under-delivers.

---

## Cross-story summary

US-16 and US-17 both touch `internal/team` but do not collide: US-16 changed the write path
(`writeFileAtomic`'s `(root, relPath, ...)` signature, the new `isOwnedWriteTarget` guard,
`.gitignore` management) and its own new files; US-17 only replaced embedded snapshot data
(`index.json`, markdown files) and rewrote a doc comment in `embedded.go` — genuinely read-only
index construction, confirmed by direct trace that `embedded.go`'s `Embedded()`/`EmbeddedFS()` path
never touches `writeFileAtomic` or any write primitive. No interface mismatch, no duplicate logic,
no silent overwrite between the two engineers' work. All call sites of `writeFileAtomic`
(`gitignore.go`, `write.go`, `lockfile.go`, `overrides.go`) were updated consistently to the new
two-parameter-prefix signature; `go build`/`go vet`/`go test -race` confirm this across the whole
tree, not just the two stories' own packages.

The `errors.Join` aggregation pattern that has been BLOCKING three times previously on this issue
was checked in every new/modified validation and multi-item loop touched by these two stories
(`decodeOverrides`'s two-pass decode — pre-existing, unmodified by this run but re-verified clean;
`Add`'s per-render validation loop — pre-existing, unmodified by this run) and in the new code
added (`gitignoreHasEntry`, `isOwnedWriteTarget`, `ensureGitignoreEntry`) — none of the new code
introduces a multi-item validation loop that fails fast; the two new "fault-injected" test cases
(`write_guard_test.go`) each loop over multiple bad inputs and check every one individually rather
than stopping at the first failure, which is the right shape for a test asserting this property.

Security review of the new/changed write paths: `isOwnedWriteTarget` normalizes via
`filepath.Clean`/`filepath.ToSlash` before prefix-matching, which correctly rejects `..`-escaping
attempts (verified by manual trace of a multi-`..` bypass attempt) and any absolute path (no
prefix in `harnessOwnedDirPrefixes` starts with `/`). Symlink refusal via `os.Lstat` is present at
every write choke point (`refuseSymlink` walks every path component from root down;
`writeFileAtomic` additionally `Lstat`s the final target and the temp-file path before writing).
`ensureGitignoreEntry` calls `refuseSymlink` before reading `.gitignore`. File permission bits are
justified (`0o666`-with-umask via direct `OpenFile` mode, matching the project's documented
SEC-10 no-`Chmod` convention). No `fmt.Print*`/`log.*` calls were introduced in either story's new
code (all output continues to route through `env.Stdout`/`env.Logger` at the CLI layer, unmodified
by these two stories). No import beyond `internal/config` was added to `internal/team`
(`go list -deps` confirms).

---

## Verdict

ADVISORY_ONLY — 3 non-blocking advisories noted (doc.go completeness gap, an architecture-note
wording mismatch resolved better in code than the note describes, and a stale AC count in
US-17.md's own planning doc, not the code). Proceeding to environment setup / close-out.
