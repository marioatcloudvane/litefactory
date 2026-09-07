# Plan Brief — Pre-run branch drift detection and safe auto-sync
# Branch: lf/70-how-to-merge-pull-manage-conflicts
# Date: 2026-09-05
# Amended: 2026-09-06 — added US-11/US-12 (conflict-resolution suggestion and
#   confirmed apply); corrected the migration numbers and a step-order wording
#   nit; reconsidered and kept US-7 unchanged. See `ARCH_REVIEW.md`'s
#   2026-09-06 "Amendment Review — US-11 / US-12" section for the full review
#   this amendment synthesises.
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
  Re-verified first-hand on 2026-09-06 against current
  `internal/turn/prepare.go:376-410`: the real order inside the gate is
  `fetch → freezeSlug → ensureWorktree`, and DEV-1's placement — last, after
  `ensureWorktree` — stands unchanged. Affects US-2, US-9, US-10.
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
  is in `internal/daemon`). **Reconsidered on 2026-09-06 after the issue owner
  questioned its value, and kept unchanged, without dissent from the
  architect** — dropping it would leave a repo-wide flip of
  `block_run_on_conflict` as the only escape from a per-issue false-positive
  block, and the label is now also the sole way to keep a repository's file
  content out of a model prompt for an issue that opts out of syncing
  altogether (an issue labelled `lf:sync=off` never reaches the `blocked`
  outcome US-11/US-12 depend on). Affects US-7, and any story that reads the
  label's effect (US-2, US-4, US-11, US-12).
- A sync merge is always a true merge commit — `git merge --no-ff --no-edit` —
  carrying the identity prelude `-c user.name= -c user.email=
  -c commit.gpgsign=false` (the daemon's git child inherits no identity or
  signing config). Never a rebase, never a force-push: rebase would rewrite the
  `Litefactory-Task`/`Mode`/`Run` trailers already on the branch. Affects US-2,
  US-10, and (for the application commit) US-12.
- US-6 is a **verification** story, not a construction story (architect DEV-3).
  No new locking primitive, no per-branch keyed gate, no second `CloneGate`
  acquisition, no mutex in `internal/git`. Measured contention in this codebase
  is per-clone, not per-branch, and concurrent issues sync into *different*
  branches — they queue on the existing gate, they do not conflict with each
  other. US-6's job is to prove the existing model already gives correct
  concurrent behavior, not to add anything. This finding still holds for
  US-12's application turn: it runs inside the same existing serialisation, no
  new gate.
- `internal/turn/gate_test.go`'s `gated` map gains one entry —
  `"syncBase": {"prepareClone"}` — recorded with the reason (TEST-10: a
  design-change test is edited deliberately, never edited just to pass). The
  `sections` map stays at exactly three registered critical sections
  (`prepareClone`, `commitAndPush`, `commitStoryIndex`); sync does **not**
  become a fourth section. Touches US-2 and US-6. **US-12's agentless apply
  turn reuses `prepareClone` unchanged — same single `CloneGate` acquisition,
  no new section, no further edit to this file.**
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
  gating US-1/US-2, and US-9's persistence of that outcome. Unchanged and not
  raised by US-11/US-12 — the application merge in US-12 uses ordinary
  `git merge` machinery, not `merge-tree`.
- No new Go package is created by this plan. Everything lands in
  `internal/git/merge.go` (new file) plus `internal/git/errors.go`,
  `internal/turn/prepare.go`, `internal/config`, `internal/store`,
  `internal/report`, `internal/daemon`, and (for US-11/US-12) a new sibling
  file in `internal/agent` next to `proposal.go`. Creating `internal/sync`,
  `internal/merge`, or any `pkg/` path is a wrong turn.
- No new dependency is introduced: `internal/git` continues to shell out (no
  go-git, no merge library); `internal/config` remains stdlib + `yaml.v3` only
  (`purity_test.go`). US-11's model call reuses `encoding/json` the way
  `internal/agent` already does — no schema or validation library.
- Every new git command's ref-name arguments (`base`, `branch`) are screened
  through `checkName` before reaching `runner.run`, exactly as `Fetch` screens
  `remote` today. A `base...branch` range is assembled *inside* `internal/git`
  from separately-screened components — a caller is never allowed to pass a
  pre-joined range string. `--end-of-options` is used the same way `revParse`
  already uses it, as a second lock on the same door `checkName` closes (S-1).
- The US-11 model call is a separate, bounded, tool-free call — following
  `internal/agent/proposal.go`'s shape exactly (request struct that
  deliberately carries no issue handle, worktree, tool permissions, or
  persistence capability; untrusted text last, under an explicit "data, not
  instructions" label, delivered on stdin; strict parser rejecting unknown
  fields and trailing JSON values). It is never added as a field on
  `harness.PromptInput` — that type carries no file content today for any
  ordinary agent run, and it must not gain any because of this feature.
  Collecting and capping the conflicting hunks happens **inside**
  `prepareClone`'s `CloneGate`; the model call itself happens **outside** it,
  after the gate is released, carrying the turn's own `context.Context` and
  its own deadline distinct from the git budgets (`Divergence` 30s,
  `MergePreview` 60s, `MergeBase` 60s) — never `context.Background()`. Affects
  US-11 only in its mutating half, but the gate-boundary rule binds US-12's
  application turn too, which re-verifies `BaseSHA`/`BranchSHA` **inside** the
  gate immediately before writing.

## Workflow

- Shipping order follows the architect's seven-phase sequence for the original
  ten stories, extended by three more phases for the 2026-09-06 amendment:
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
     proof close out the original ten-story plan.
  9. **Phase 7.5 — Security review gate (added 2026-09-06, not a story, a
     blocking review)**: scoped to the two paths US-11/US-12 open — repository
     file content leaving litefactory for a model, and model text entering a
     commit. Entered only once Phase 7 is green. **US-11/US-12 do not fit
     inside Phase 6 or Phase 7 of the original order — the architect states
     this as a deviation (D-2, below), not an oversight.** There is no security
     specialist on this team's roster; the nearest instrument is
     `go-code-reviewer` running `.claude/skills/shared/security-scanner.md`,
     and the human owner is the approver.
  10. **Phase 8 — US-11 (added 2026-09-06)**: propose and post a suggested
      resolution. Entered once Phase 7.5 is signed off. Ships with
      `suggest_conflict_resolution` defaulting **false** until that sign-off
      (D-3) — the spec's own A-20 authorises this as a configuration the
      review may flip, not a deviation from it.
  11. **Phase 9 — US-12 (added 2026-09-06)**: confirm and apply. Entered once
      Phase 8 has shipped **and** at least one real blocked run has produced a
      suggestion an owner has actually read — an observation gate, not a date,
      because US-12's entire risk is a function of suggestion quality nobody
      has yet seen. Opens with `internal/git`'s new
      `TestWorktreeMethodSetIsTheAllowlist` (T-12.1), written before any new
      `*Worktree` mutator exists.
- **Hard coupling (from the architect's resolution of A-14, and reaffirmed
  2026-09-06): US-7 must ship no later than US-2/US-4, and no later than
  US-11 either.** `BlockRunOnConflict` defaults to `true`, and the block is
  correct only because `lf:sync=off` exists as an escape hatch for the
  false-positive case — a `MergeBase` that merges the whole base branch and
  blocks a run over an unrelated file both branches happened to touch. It is
  now also the only per-issue way to keep a repository's file content out of a
  model prompt, since an `lf:sync=off` issue never reaches `blocked`. Shipping
  the refusal, or the suggestion feature, without the label already live is an
  un-overridable block or an unwanted exposure in front of real work.
- Dependency chain: US-1 blocks US-2 (need `Divergence` before deciding to
  preview or merge). US-2 blocks US-3, US-4, US-6, US-9's persistence wiring,
  and US-10 (the rollback guarantee is implemented inside `MergeBase` and
  verified against it). US-4 blocks US-5 (sanitization applies to the refusal
  rendering US-4 introduces) **and now also blocks US-11** (a suggestion is
  only ever produced for the `blocked` outcome US-4 defines). US-11 blocks
  US-12 (there is nothing to confirm without a stored, validated proposal).
  US-10 blocks US-12 (the rollback guarantee covers a failed application the
  same way it covers a failed sync).

## Cross-Story Constraints

- **Platform**: git ≥2.38 hard floor. The live daemon runs from
  `dist/litefactory` — implementation work never runs `make build`, writes
  into `dist/`, kills or restarts the running daemon, or edits
  `~/.litefactory/config.yaml`. Safe commands are `go build ./...`,
  `go test ./...`, `go vet ./...`, `gofmt -l .`. No new dependency, anywhere.
  SQLite has one connection; each migration is one embedded `.sql` file,
  plain DDL, `NOT NULL DEFAULT` on every added column, no backfill needed; the
  outcome write is a read-then-write under `BEGIN IMMEDIATE`.
  **Corrected 2026-09-06 (was stale within a day of the 2026-09-05 plan):**
  the migration head is `0006_schedule_block_replies.sql`, not `0004` —
  `0005_schedules.sql` landed from another feature in between. US-9's
  run-sync columns are therefore `0007_run_sync.sql`, and US-11's proposal
  table is `0008_conflict_proposals.sql`. **Re-verify by listing
  `internal/store/migrations/` at implement time — do not trust these numbers
  from this document; the head already moved twice during this plan's own
  lifetime.** macOS launchd's child environment is deliberately minimal
  (`childEnv()` passes only `PATH`, `HOME`, `SSH_AUTH_SOCK`,
  `GIT_TERMINAL_PROMPT=0`, and `GIT_SSH_COMMAND` in BatchMode) — a merge
  commit inherits no identity or signing configuration, which is why the
  identity prelude is mandatory rather than defensive for every merge commit,
  including US-12's application commit.
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
    The same risk, and the same required prelude, applies to US-12's
    application commit.
  - S-5 (High — the rollback guarantee): "byte-identical to before the run"
    is enforced, not assumed: refuse to merge on a dirty worktree, capture the
    branch SHA before merging, `merge --abort` on any failure, then re-verify
    the SHA. A mismatch is a `failed` outcome, reported, never silently
    accepted. Never add `Reset` to self-heal a failed rollback — `Reset` is
    forbidden on `Worktree` by SEC-7, and this matters more once US-12 gives
    `Worktree` a write path.
  - S-6 (High): the merge happens inside `prepareClone`'s single existing
    `CloneGate` acquisition — no second acquisition, no differently-keyed one.
    US-12's agentless apply turn reuses this same acquisition unchanged.
  - S-7 (Medium, accepted and disclosed, not defended against): auto-sync
    widens the prompt-injection window into the coding agent, since
    base-branch content now reaches it on every run, not only the first. This
    is exactly why US-3's disclosure is load-bearing, not cosmetic.
  - S-8 (Medium): a timed-out merge can leave `MERGE_HEAD`/`index.lock`
    behind; `TimedOut` stays distinguishable in the git package's error
    classification.
  - S-9 (Low, **amended 2026-09-06 by S-17**): the conflict-path list is
    capped where it is parsed (inside `internal/git`), not only where it is
    rendered. Its "never persisted to the store" clause is reversed for
    US-11/US-12 only — see S-17.
  - **S-10 (High, added 2026-09-06 — superset of S-7, not a duplicate):**
    US-11 has litefactory itself read conflicting file *content*, assemble
    it, and send it to a model — a strictly larger surface than S-7's
    disclosure-only acceptance. Narrower in one dimension (the tool-free call
    has no tools, no worktree, no session to resume, so an injected
    instruction cannot be acted on there), wider in another (litefactory is
    the courier, and in US-12 the response is trusted enough to become file
    bytes). Mitigated by capping and excluding whole files before assembly,
    never truncating mid-file, and delivering the content last, on stdin,
    under an explicit "data, not instructions" label.
  - **S-11 (High, added 2026-09-06):** the first path where model-produced
    text becomes committed bytes. Required together: reject the whole
    response on any out-of-list path or any remaining conflict marker; write
    only the paths in `ConflictProposal.Files`; re-verify `BaseSHA`/`BranchSHA`
    inside the gate immediately before writing; exactly one merge commit;
    US-10's rollback guarantee covers the failure path.
  - **S-12 (High, added 2026-09-06):** the confirmation gate is the entire
    security model of US-12 and must be structural, not merely unconfigured —
    no config value, label, flag, environment variable, or "trusted repo"
    exemption may bypass it. The applying code path is reachable only through
    an unexported value type that only a successful confirmation can
    construct, so a missing check becomes a compile error rather than a
    missing `if`.
  - **S-13 (High, added 2026-09-06 — sharpest new edge):** model-supplied
    paths reach the filesystem. Containment is enforced by `*git.Worktree`
    itself — the type that owns the root — rejecting absolute paths, `..`
    traversal, symlink components, and any path not present in
    `ConflictProposal.Files`; never by the caller, and never by string prefix
    comparison. Related to S-2 (also untrusted-path handling) but distinct:
    S-2's sink is markdown, S-13's sink is the filesystem.
  - **S-14 (Medium, added 2026-09-06):** the confirmation phrase is a new
    comment grammar any commenter can reach. Parsed like the existing
    `KindSchedule` grammar — separate parser, canonical lowercase token,
    restricted charset — with the ownership check applied *after* parsing.
    A non-owner confirmation writes nothing and never moves the proposal off
    `pending`; refusals never echo raw comment text.
  - **S-15 (Medium, added 2026-09-06 — TOCTOU/replay):** base or branch can
    move between propose and confirm. Stored SHAs are the guard and are
    re-checked inside the `CloneGate` at apply time, not before acquiring it.
    A stale confirmation marks the proposal `superseded` and writes nothing.
    Double confirmation of the same proposal is a no-op reported as already
    applied, never a second commit. Supersede-on-new-proposal is one
    `BEGIN IMMEDIATE` transaction.
  - **S-16 (Medium, added 2026-09-06):** the model name and confirming
    handle land in a commit trailer and in a comment. Both come from
    litefactory-controlled or pattern-screened values, never from the model's
    own output; the handle follows SEC-5's reject-don't-salvage discipline.
  - **S-17 (Low, added 2026-09-06 — amends S-9):** S-9 said the conflict-path
    list is never persisted. US-11 reverses that by necessity: a resolution
    proposed in one run must be readable by a later run to be applied, so
    `ConflictProposal.Files` and `Resolution` are persisted, capped at the
    store boundary as well as at parse time. The *input* hunks (the raw
    conflicting content shown to the model) are still never persisted — only
    the file list, the proposed resolution, and the two SHAs.
- **Tests**: `internal/git/surface_test.go`'s `TestCloneMethodSetIsTheAllowlist`
  passes unchanged (`Clone` gains nothing — none of US-11/US-12's mutators
  touch it). `TestCloneCannotMutateTheRegisteredClone`'s forbidden-mutator list
  is untouched. `internal/turn/gate_test.go`'s `sections` map stays at exactly
  three entries; **US-12's application turn reuses `prepareClone` unchanged
  and requires no edit to this file.** Any design-change test edit is
  deliberate and carries a recorded reason (TEST-10) — never edited just to
  make it pass. The `report.ReplyKind` enum-exhaustiveness test passes with
  `ReplyRefusesSync` (US-4), then `ReplySuggestsResolution`/
  `ReplyAppliesResolution` (US-11/US-12), added in turn. Migration `0007`
  applies cleanly on top of `0006`, and `0008` cleanly on top of `0007`, with
  correct defaults for pre-existing rows — re-verify the actual head at
  implement time. **`internal/git/surface_test.go` gains a new,
  `Worktree`-scoped counterpart —`TestWorktreeMethodSetIsTheAllowlist` plus a
  forbidden-mutator list covering `Reset`, `Rebase`, `CherryPick`, `Clean`,
  `Stash`, `Checkout`, `Switch` — written before US-12's first mutator exists,
  the same discipline `Clone`'s allowlist test already applies.**
- **Ring-fenced** (see also Out of Scope): the base branch itself, pull-request
  creation/approval/merge, rebase/force-push/history rewrite, and the Claude
  agent's `--allowedTools` Bash loophole are all out of reach for this plan.
  **Added 2026-09-06:** no mid-run pause-and-wait-for-a-human-reply mechanism
  exists anywhere in litefactory today, and none is introduced by US-11/US-12
  — propose and apply are two independently terminal runs linked by a stored
  row, never a live wait, and this must not change as a side effect of
  building either story. Whether a suggested resolution is actually *good* is
  a product question answerable only by reading real output, not an
  architecture question this plan resolves — it is why Phase 9 opens on an
  observation gate rather than a date, not a promise about suggestion quality.

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
| US-11 | Suggest a resolution when a run is refused for a conflict | L | US-4, US-5, US-8, US-9 | https://github.com/marioatcloudvane/litefactory/issues/139 |
| US-12 | Apply a confirmed resolution and complete the merge | L | US-11, US-10 | https://github.com/marioatcloudvane/litefactory/issues/140 |

## Out of Scope

- Merging or pushing into the base branch itself.
- Opening, approving, or merging pull requests.
- Rebase, force-push, or any history rewrite.
- Auto-starting an agent to resolve conflicts (Renovate/Dependabot-style
  regeneration) — regeneration discards committed agent work, which
  contradicts litefactory's "litefactory commits; nobody in here does"
  preservation guarantee. (US-11/US-12's model-suggested, human-confirmed
  resolution is a different shape from this exclusion — no regeneration, no
  write without an explicit, separate confirmation — and does not fall into
  it.)
- GitHub merge-queue configuration.
- Time- or idle-based staleness detection (drift is measured in commit counts
  only).
- Per-issue base-branch override (`lf:base=`) or per-project base-branch
  hierarchy — base branch remains repo-level state in `store.Repo`.
- Any new CLI command or flag.
- Closing the Claude agent's `--allowedTools` Bash loophole — tracked
  separately, not part of this plan.
- Conflict-resolution UI or web-editor guidance beyond the refusal-comment
  and suggestion-comment text itself.
- Cross-task run ordering or serialization beyond what `CloneGate` already
  provides.
- **Added 2026-09-06:** a live, mid-run pause that waits for a human reply
  before continuing — propose and apply stay two separate, fully-finished
  runs. Judging or tuning suggestion quality, prompt wording, or model choice
  beyond the structural safety rules US-11/US-12 specify. Any bypass of
  US-12's confirmation gate, in any form (config, label, flag, environment
  variable, or trusted-repo exemption).

## Provenance

- Citation base: not independently determined in this run. Neither
  `feature-spec.md` nor `ARCH_REVIEW.md` recorded a commit SHA, and Plan mode
  does not hold a tool to read current `HEAD` independently of a file it was
  told to read. `RESEARCH_BRIEF.md` (this session) is the source of record for
  citation base and should be treated as such by Implement mode.
- Known-stale areas: the codebase-tour research pass and the 2026-09-05 plan
  synthesis were done two days apart; two other features (lf/50 config
  hierarchy, lf/69 router-trusts-daemon-mode) merged into this branch's
  history in between, and that is what made the original migration-number
  citation (`0005_run_sync.sql` on top of a `0004` head) stale within a day.
  On 2026-09-06, `main` was merged into this branch again (owner-initiated)
  and a fresh research pass plus this architect amendment re-verified the
  migration head (now `0006_schedule_block_replies.sql`) and `prepareClone`'s
  step order directly against current source — both are captured above as
  corrected. Re-verify `internal/git`, `internal/turn/prepare.go`, and
  `internal/daemon`'s label resolvers against current `HEAD` again before
  implementation starts; `main` continues to move and has already changed the
  migration head twice during this plan's own lifetime.
- Re-pinning requirements: verify the git version floor (Phase 0 probe)
  against the actual CI/deploy git version before relying on
  `merge-tree --write-tree`. Re-confirm `exec.go`'s exit-code classification
  for `merge-tree`'s conflicted-vs-broken-invocation cases empirically, not
  from documentation, before implementing `MergePreview`. **Re-list
  `internal/store/migrations/` immediately before creating migration `0007`
  or `0008` — do not trust the numbers recorded in this document.** Phase 7.5's
  security review (ODQ-28) is itself a re-pinning gate for US-11/US-12: it
  must sign off before `suggest_conflict_resolution` may default to `true`
  and before Phase 9 opens.
