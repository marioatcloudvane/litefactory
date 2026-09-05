# Plan Brief — Pre-run branch drift detection and safe auto-sync
# Branch: lf/70-how-to-merge-pull-manage-conflicts
# Date: 2026-09-05
# Parent issue: github:marioatcloudvane/litefactory#70 (How to merge / pull / manage conflicts)
# Citation base: not independently determinable in this run — see Provenance
# Research brief: RESEARCH_BRIEF.md (this session)

## Shared Decisions

- All three new git operations — `Divergence`, `MergePreview`, `MergeBase` — live
  on `*git.Worktree`, never on `*git.Clone` (architect DEV-2, overriding the
  spec's original placement). Putting them on `Clone` would require editing
  `surface_test.go`'s `TestCloneMethodSetIsTheAllowlist`, a design-change test,
  plus an S-6/SEC-7 amendment in `.aiteam/AC_AMENDMENTS.md`. `Worktree` already
  carries read methods of this shape; this placement costs nothing and needs no
  amendment. Affects US-1, US-2, US-9, US-10.
- The sync step (`syncBase`) runs as the **last** step inside `prepareClone`,
  after `ensureWorktree`, not between `fetch` and `freezeSlug` as the spec
  originally stated (architect DEV-1) — `MergeBase` is a `*Worktree` method and
  the worktree does not exist until `ensureWorktree` has run using the branch
  name `freezeSlug` produced. `prepareClone`'s single `CloneGate` acquisition is
  unchanged; `syncBase` is the fourth step inside it, not a new acquisition.
  Affects US-2, US-9, US-10.
- `preparation.shaBefore` is captured **after** `syncBase` completes, not
  before — reading it earlier would report a phantom diff on every synced run,
  since the session branch can move without the coding agent doing anything.
  Affects US-2, US-9.
- `lf:sync=on|off` is modeled on the two **live** label levers —
  `agentLabelPrefix` and `modeLabelPrefix` in `internal/daemon` — copied rung
  for rung (case-insensitive prefix match; more than one matching label is
  ambiguous and refused, never unioned or first-wins; a prefix matched
  case-insensitively but not byte-exact is malformed and refused with no field
  for the label text to travel in; a value outside `{on, off}` is malformed and
  refused; label beats config when it resolves cleanly). It is **not** modeled
  on `lf:tools=` (DEV-4): that label is dead — `legacyToolsLabel` in
  `internal/daemon/tools.go` detects it only to refuse it, and its value is
  deliberately never read or logged. The research brief's citation of this
  precedent to `internal/config/policy.go` is also wrong on location (the code
  is in `internal/daemon`). Affects US-7, and any story that reads the label's
  effect (US-2, US-4).
- A sync merge is always a true merge commit — `git merge --no-ff --no-edit` —
  carrying the identity prelude `-c user.name= -c user.email=
  -c commit.gpgsign=false` (the daemon's git child inherits no identity or
  signing config). Never a rebase, never a force-push: rebase would rewrite the
  `Litefactory-Task`/`Mode`/`Run` trailers already on the branch. Affects US-2,
  US-10.
- US-6 is a **verification** story, not a construction story (architect DEV-3).
  No new locking primitive, no per-branch keyed gate, no second `CloneGate`
  acquisition, no mutex in `internal/git`. Measured contention in this codebase
  is per-clone, not per-branch, and concurrent issues sync into *different*
  branches — they queue on the existing gate, they do not conflict with each
  other. US-6's job is to prove the existing model already gives correct
  concurrent behavior, not to add anything.
- `internal/turn/gate_test.go`'s `gated` map gains one entry —
  `"syncBase": {"prepareClone"}` — recorded with the reason (TEST-10: a
  design-change test is edited deliberately, never edited just to pass). The
  `sections` map stays at exactly three registered critical sections
  (`prepareClone`, `commitAndPush`, `commitStoryIndex`); sync does **not**
  become a fourth section. Touches US-2 and US-6.
- Conflicting file paths are untrusted content that originates on the base
  branch. Every story that touches, renders, or persists that list (US-4,
  US-5, US-9) rejects — never sanitizes or truncates-and-keeps — any path
  failing a local pattern check in `internal/report`, applying SEC-5's
  local-revalidation rule rather than trusting upstream git output. A rejected
  path is replaced by a count, never a partially-reconstructed string.
- Config defaults, binding wherever any of these three flags is read:
  `AutoSyncBase` (`auto_sync_base`) default `true`; `AutoSyncMaxBehind`
  (`auto_sync_max_behind`) default `100`, documented as a judgment call and not
  a measurement, `0` meaning no cap, negative rejected as a config error and
  never clamped (CFG-6); `BlockRunOnConflict` (`block_run_on_conflict`)
  default `true`. Used by US-2, US-4, US-7, US-8.
- git ≥2.38 is a hard floor for `merge-tree --write-tree`. The version probe
  happens once — at git binary resolution, or behind a `sync.Once` at first
  sync — never per run. An unsupported git produces a named sentinel and a
  `failed`/`skipped`-shaped outcome with a clear operator sentence, never a
  hard boot failure and never a raw `*CommandError`. Affects the Phase 0 probe
  gating US-1/US-2, and US-9's persistence of that outcome.
- No new Go package is created by this plan. Everything lands in
  `internal/git/merge.go` (new file) plus `internal/git/errors.go`,
  `internal/turn/prepare.go`, `internal/config`, `internal/store`,
  `internal/report`, `internal/daemon`. Creating `internal/sync`,
  `internal/merge`, or any `pkg/` path is a wrong turn.
- No new dependency is introduced: `internal/git` continues to shell out (no
  go-git, no merge library); `internal/config` remains stdlib + `yaml.v3` only
  (`purity_test.go`).
- Every new git command's ref-name arguments (`base`, `branch`) are screened
  through `checkName` before reaching `runner.run`, exactly as `Fetch` screens
  `remote` today. A `base...branch` range is assembled *inside* `internal/git`
  from separately-screened components — a caller is never allowed to pass a
  pre-joined range string. `--end-of-options` is used the same way `revParse`
  already uses it, as a second lock on the same door `checkName` closes (S-1).

## Workflow

- Shipping order follows the architect's seven-phase sequence:
  1. **Phase 0 — Probe** (not a story; a blocking prerequisite check): verify
     `merge-tree --write-tree` behavior and the git ≥2.38 floor empirically —
     exit code for a conflicted merge vs. a broken invocation, exact
     `--name-only` output shape, whether conflicting paths come back C-quoted —
     before any US-1/US-2 code is written.
  2. **Phase 1 — `internal/git` foundation**: US-1, US-2, US-10. Everything
     else in this plan depends on this landing first.
  3. **Phase 2 — `internal/config`**: US-8. Independent of Phase 1; can run in
     parallel.
  4. **Phase 3 — `internal/store`**: US-9's migration and field additions.
     Independent of Phase 1/2 for the schema itself; the persistence-wiring
     task waits on US-2.
  5. **Phase 4 — `internal/report`**: US-3, US-4's structs and rendering.
     Independent of Phase 1-3; can run in parallel.
  6. **Phase 5 — `internal/daemon`**: US-7's label lever. Independent to
     build, but see the hard coupling below on when it must ship.
  7. **Phase 6 — `internal/turn` convergence**: `syncBase` wiring, outcome
     plumbing, `replyKind()` mapping, the `gate_test.go` registry edit. This is
     where US-2, US-4, US-7's label decision, and US-9's persistence all meet.
     US-5's sanitization is exercised here too.
  8. **Phase 7 — Verification**: US-6 (concurrency proof) and US-10's rollback
     proof close out the plan.
- **Hard coupling (from the architect's resolution of A-14): US-7 must ship in
  the same release as US-2/US-4, never after.** `BlockRunOnConflict` defaults
  to `true`, and the block is correct only because `lf:sync=off` exists as an
  escape hatch for the false-positive case — a `MergeBase` that merges the
  whole base branch and blocks a run over an unrelated file both branches
  happened to touch. Shipping the refusal without the label is an
  un-overridable block in front of real work.
- Dependency chain: US-1 blocks US-2 (need `Divergence` before deciding to
  preview or merge). US-2 blocks US-3, US-4, US-6, US-9's persistence wiring,
  and US-10 (the rollback guarantee is implemented inside `MergeBase` and
  verified against it). US-4 blocks US-5 (sanitization applies to the refusal
  rendering US-4 introduces).

## Cross-Story Constraints

- **Platform**: git ≥2.38 hard floor. The live daemon runs from
  `dist/litefactory` — implementation work never runs `make build`, writes
  into `dist/`, kills or restarts the running daemon, or edits
  `~/.litefactory/config.yaml`. Safe commands are `go build ./...`,
  `go test ./...`, `go vet ./...`, `gofmt -l .`. No new dependency, anywhere.
  SQLite has one connection; the new migration is one embedded `.sql` file
  named `0005_run_sync.sql` (0004 is the current head), plain DDL, `NOT NULL
  DEFAULT` on every added column, no backfill needed; the outcome write is a
  read-then-write under `BEGIN IMMEDIATE`. macOS launchd's child environment is
  deliberately minimal (`childEnv()` passes only `PATH`, `HOME`,
  `SSH_AUTH_SOCK`, `GIT_TERMINAL_PROMPT=0`, and `GIT_SSH_COMMAND` in
  BatchMode) — a merge commit inherits no identity or signing configuration,
  which is why the identity prelude is mandatory rather than defensive.
- **Security** (forward-looking; severities and IDs from the architect's
  review):
  - S-1 (High): ref-name arguments reaching `merge`/`merge-tree`/`rev-list`
    argv must be screened by `checkName`; ranges are assembled inside
    `internal/git`, never accepted pre-joined from a caller.
  - S-2 (High — "the one that matters most"): conflicting file paths are
    attacker-influenced content from the base branch; `internal/report` must
    reject any path failing a local pattern rather than sanitize or partially
    print it, and must not rely on git's own quoting for markdown safety.
  - S-3 (Low): `merge-tree --write-tree` writes loose objects into the shared
    object database even on conflict — it is a dry run of the branch, not of
    the repository. Document this wherever "dry run" language is used.
  - S-4 (Medium): `MergeBase` needs the same identity/signing prelude as
    `Commit`, and passes `Author` explicitly — omitting `commit.gpgsign=false`
    risks a hang against `GIT_TERMINAL_PROMPT=0` until the deadline kills it.
  - S-5 (High — the rollback guarantee): "byte-identical to before the run"
    is enforced, not assumed: refuse to merge on a dirty worktree, capture the
    branch SHA before merging, `merge --abort` on any failure, then re-verify
    the SHA. A mismatch is a `failed` outcome, reported, never silently
    accepted. Never add `Reset` to self-heal a failed rollback — `Reset` is
    forbidden on `Worktree` by SEC-7.
  - S-6 (High): the merge happens inside `prepareClone`'s single existing
    `CloneGate` acquisition — no second acquisition, no differently-keyed one.
  - S-7 (Medium, accepted and disclosed, not defended against): auto-sync
    widens the prompt-injection window into the coding agent, since
    base-branch content now reaches it on every run, not only the first. This
    is exactly why US-3's disclosure is load-bearing, not cosmetic.
  - S-8 (Medium): a timed-out merge can leave `MERGE_HEAD`/`index.lock`
    behind; `TimedOut` stays distinguishable in the git package's error
    classification.
  - S-9 (Low): the conflict-path list is capped where it is parsed (inside
    `internal/git`), not only where it is rendered, and is never persisted to
    the store.
- **Tests**: `internal/git/surface_test.go`'s `TestCloneMethodSetIsTheAllowlist`
  passes unchanged (`Clone` gains nothing). `TestCloneCannotMutateTheRegisteredClone`'s
  forbidden-mutator list is untouched. `internal/turn/gate_test.go`'s `sections`
  map stays at exactly three entries. Any design-change test edit is
  deliberate and carries a recorded reason (TEST-10) — never edited just to
  make it pass. The `report.ReplyKind` enum-exhaustiveness test passes with
  `ReplyRefusesSync` added. Migration 0005 applies cleanly on top of 0004 with
  correct defaults for pre-existing rows.
- **Ring-fenced** (see also Out of Scope): the base branch itself, pull-request
  creation/approval/merge, rebase/force-push/history rewrite, and the Claude
  agent's `--allowedTools` Bash loophole are all out of reach for this plan.

## Story Index

| ID | Title | Size | Depends On | Sub-issue |
|----|-------|------|-----------|-----------|
| US-1 | Measure behind/ahead before every run | S | — | https://github.com/marioatcloudvane/litefactory/issues/108 |
| US-2 | Auto-merge the base branch when the merge is clean | L | US-1, US-7, US-8 | https://github.com/marioatcloudvane/litefactory/issues/109 |
| US-3 | Disclose every automatic change | M | US-2 | https://github.com/marioatcloudvane/litefactory/issues/110 |
| US-4 | Refuse the run and name the conflicting files | M | US-2 | https://github.com/marioatcloudvane/litefactory/issues/111 |
| US-5 | Render conflicts in plain language, safely | M | US-4 | https://github.com/marioatcloudvane/litefactory/issues/112 |
| US-6 | Verify per-issue sync is independent under concurrency | M | US-2 | https://github.com/marioatcloudvane/litefactory/issues/113 |
| US-7 | `lf:sync=` label opt-out | M | — | https://github.com/marioatcloudvane/litefactory/issues/114 |
| US-8 | Config-governed defaults and size cap | S | — | https://github.com/marioatcloudvane/litefactory/issues/115 |
| US-9 | Persist sync outcomes behind the internal/git boundary | M | US-1, US-2 | https://github.com/marioatcloudvane/litefactory/issues/116 |
| US-10 | Guarantee a failed or refused sync leaves the branch untouched | L | US-2 | https://github.com/marioatcloudvane/litefactory/issues/117 |

## Out of Scope

- Merging or pushing into the base branch itself.
- Opening, approving, or merging pull requests.
- Rebase, force-push, or any history rewrite.
- Auto-starting an agent to resolve conflicts (Renovate/Dependabot-style
  regeneration) — regeneration discards committed agent work, which
  contradicts litefactory's "litefactory commits; nobody in here does"
  preservation guarantee.
- GitHub merge-queue configuration.
- Time- or idle-based staleness detection (drift is measured in commit counts
  only).
- Per-issue base-branch override (`lf:base=`) or per-project base-branch
  hierarchy — base branch remains repo-level state in `store.Repo`.
- Any new CLI command or flag.
- Closing the Claude agent's `--allowedTools` Bash loophole — tracked
  separately, not part of this plan.
- Conflict-resolution UI or web-editor guidance beyond the refusal-comment
  text itself.
- Cross-task run ordering or serialization beyond what `CloneGate` already
  provides.

## Provenance

- Citation base: not independently determined in this run. Neither
  `feature-spec.md` nor `ARCH_REVIEW.md` recorded a commit SHA, and Plan mode
  does not hold a tool to read current `HEAD` independently of a file it was
  told to read. `RESEARCH_BRIEF.md` (this session) is the source of record for
  citation base and should be treated as such by Implement mode.
- Known-stale areas: the codebase-tour research pass and this plan's synthesis
  were done two days apart (2026-09-03 research, 2026-09-05 plan); two other
  features (lf/50 config hierarchy, lf/69 router-trusts-daemon-mode) merged
  into this branch's history in between. Re-verify `internal/git`,
  `internal/turn/prepare.go`, and `internal/daemon`'s label resolvers against
  current `HEAD` before implementation starts.
- Re-pinning requirements: verify the git version floor (Phase 0 probe)
  against the actual CI/deploy git version before relying on
  `merge-tree --write-tree`. Re-confirm `exec.go`'s exit-code classification
  for `merge-tree`'s conflicted-vs-broken-invocation cases empirically, not
  from documentation, before implementing `MergePreview`.
