# Plan Brief — Configurable Agent Teams
# Branch: lf/68-new-way-to-work-with-agent-teams
# Date: 2026-09-04
# Parent issue: #68
# Citation base commit: see `$session/RESEARCH_BRIEF.md` (provenance recorded there; not duplicated here)
# Research brief: `.aiteam/lf-68-new-way-to-work-with-agent-teams/2026-09-03/new-way-to-work-with-agent-teams/RESEARCH_BRIEF.md`

## Scope Decision (binding — do not re-open)

litefactory **adopts write-ownership** of agent-team files inside a connected repo, for this
feature only. The general rule at `internal/cli/cmd_repo.go:57-58` ("litefactory never installs
it and never writes inside the clone") is explicitly overridden **for the five Owned Paths
below, and no others.** This was decided by the human sponsor before planning began. No story in
this plan re-litigates it; DEV-1 below records the override's exact scope so it is a deliberate,
bounded exception rather than an erosion of the rule.

### Owned Paths (the only paths litefactory may write in a connected repo)

| Path | Owner | Content |
|---|---|---|
| `.aiteam/team.json` | user (litefactory writes it only via `team manage`) | repo-local override layer |
| `.aiteam/team.lock.json` | litefactory, exclusive | provenance + hashes of every generated artifact |
| `.claude/agents/<agent>.md` | litefactory (tracked in lockfile) | generated Claude sub-agent |
| `.codex/skills/<agent>/SKILL.md` | litefactory (tracked in lockfile) | generated Codex skill |
| `.opencode/agents/<agent>.md` | litefactory (tracked in lockfile) | generated opencode agent |

litefactory writes files; it never runs `git add` / `commit` / `stash` / `push` in a connected
repo. Generated changes are left in the working tree for the operator to review and commit.

## Shared Decisions

These three decisions were the plan's hard requirements — each spans four or more stories, so
none of them lives in a single sub-issue. Implement mode must treat all three as settled.

### 1. Update & drift detection — the three-hash model (spans US-9, US-10, US-11, US-12)

Two hashes are not enough: a single "source" hash cannot distinguish "litefactory shipped a new
definition" from "the operator edited `.aiteam/team.json`." Every lockfile artifact carries
**three** independent hashes, each answering exactly one question:

| Field | Hashed over | Answers |
|---|---|---|
| `renderedHash` | the file's bytes on disk | Did a human edit the generated file? |
| `sourceHash` | the fully resolved agent (all three layers) + prompt body | Would `team update` change this file right now? |
| `baseSourceHash` | the **embedded-layer-only** resolution of that agent + prompt body | Did litefactory's shipped definition change? (`""` if the agent has no embedded definition — a legitimate state, not corruption) |

The lockfile carries a top-level `hashVersion` int. The hash input is a canonical byte sequence
prefixed with that version — never raw `encoding/json` over a struct — so that adding a field to
`AgentDefinition` cannot mass-invalidate every stored `sourceHash` in every repo. **When the
binary's `hashVersion` exceeds the lockfile's, source comparison reports `hash-scheme-changed`,
not a false `modified`/`changed`.** `renderedHash` comparison is scheme-independent and keeps
working regardless.

Orphan/foreign split (the sharpest data-loss risk in the feature): a file at an owned path with
no lockfile entry is `orphan` **only if it carries the litefactory provenance marker** (its first
body line, `<!-- litefactory: agent=<name> team=<slug> contentVersion=<n> ... -->`); otherwise it
is `foreign`. `--prune` (on `team update`) may delete an `orphan`. **It may never delete a
`foreign` file, under any flag.** The marker is advisory for drift, authoritative for deletion
safety — two different jobs, one rule each.

Full decision table (`team status` classification × `team update`/`--force` behaviour):

| On-disk state | `source-changed`? | `team status` | `team update` | `team update --force` |
|---|---|---|---|---|
| bytes match `renderedHash` | no | `clean` | no-op | no-op |
| bytes match `renderedHash` | yes | `stale` | rewrite | rewrite |
| bytes differ | no | `modified` | skip, report | overwrite |
| bytes differ | yes | `conflict` | skip, report | overwrite, reported distinctly as *conflict* |
| file absent | no | `missing` | recreate | recreate |
| file absent | yes | `missing` | recreate at new content | same |
| no lockfile entry, marker present | — | `orphan` | report | `--prune` deletes |
| no lockfile entry, no marker | — | `foreign` | report | **never deleted** |
| lockfile `hashVersion` older than binary's | unknown | `hash-scheme-changed` | rewrite lockfile | same |

`conflict` (`modified` + upstream `source-changed` together) is reported distinctly from a plain
`modified` overwrite — same `--force` action, materially higher-stakes consequence. **Never
merged, now or later** — no three-way merge machinery exists in the tree and none is added.

Lockfile invariants, validated on load (a lockfile is a file a human can edit, so nothing about
it is assumed): `artifacts` sorted by `path`, no duplicate paths; every hash is 64 lowercase hex
chars or `""` (only `baseSourceHash`); every `path` is repo-relative, forward-slash, no `..`
segment, not absolute, and resolves inside one of the five Owned Paths — an unvalidated `path` in
a hand-edited lockfile is otherwise an arbitrary-file-write primitive. Lockfile `schemaVersion`
newer than the binary's is a hard refusal, never a silent downgrade; older is migrated in memory
(same sequential-step pattern as config) and persisted only as part of a successful `team
update` — `team status` never writes.

`team status` exits 0 only when every artifact is `clean`; `foreign` counts as non-clean (it is a
real finding an operator should see).

### 2. CLI surface (spans US-4, US-9, US-10, US-11, US-13)

Six commands, one `Command` constructor each, one line each in `internal/cli/commands.go`'s
`Commands()` — no switch, no mutable registry:

| Command | Usage | Notes |
|---|---|---|
| `team list` | `team list [--repo <path>] [--json]` | Reads resolved config; writes nothing. |
| `team show` | `team show <team> [--repo <path>] [--json]` | Per-agent resolved view, naming which layer supplied each field. |
| `team add` | `team add <team> [--repo <path>] [--harness <list>] [--dry-run] [--adopt]` | Refuses if a lockfile already exists (points at `team update`). |
| `team status` | `team status [--repo <path>] [--json]` | Exit 0 clean, 1 otherwise. |
| `team update` | `team update [--repo <path>] [--dry-run] [--force] [--prune] [--migrate-config]` | See decision table above. |
| `team manage` | `team manage <agent> <set\|get\|unset> [<param>[=<value>]] [--repo <path>] [--harness <k>]` | One `Command`, `ArgSpec{["agent","action","assignment"], 2}`; arity beyond `Min` (set/unset need the 3rd positional, get accepts 2 or 3) is a two-line guard inside the handler. Writes only `.aiteam/team.json`; never re-renders — the hint to run `team update` is printed instead. |

`--team-config <path>` binds on all six commands.

**`--repo` resolution: default `.`, resolved to an absolute path, require a `.git` entry at
exactly that path — do not walk up.** This is a write command; walk-up UX (fine for read
commands) would mean running from the wrong subdirectory silently installs into the wrong repo.
Failure is a `PreconditionError` (exit 3) naming the path checked.

**Two separate precedence chains — do not conflate them:**

- **Chain A — where the org layer comes from** (first hit wins, no merging): (1) `--team-config`
  flag, (2) `team_config:` key in `$LF_HOME/config.yaml`, (3) absent → no org layer.
- **Chain B — how content composes** (later overrides earlier, per-field within an agent):
  (1) embedded fallback (`//go:embed`, always present) → (2) org layer (from Chain A, if any) →
  (3) repo-local `.aiteam/team.json` (if present). List-valued fields (`tools`,
  `disallowedTools`, team `agents`) are replaced wholesale, never element-merged. A higher layer
  may add a new agent or team but may never delete one — removal is expressed as
  `"disabled": true`, which excludes the agent from every team and from generated output.

`team_config` **must be an absolute path** — rejected otherwise in `config.validate`, keeping
CFG-8 untouched and needing no new `config.Paths` method. A configured-but-missing
`--team-config`/`team_config` path is an **error** (exit 3), never a silent fallback to
embedded-only.

`--harness` selects renderers; default is whichever of `.claude/` / `.codex/` / `.opencode/` is
already present in the repo, falling back to `claude,codex` when none is detected. The detection
result is printed in `team add` output — an invisible implicit selection is a support burden.

Exit codes: `UsageError` (unknown team/param, bad `--harness`, bad arity) → 2. `PreconditionError`
(not a git worktree, repo not writable, `team_config` missing/relative) → 3. Drift found by
`status`; blocked `modified`/`conflict` on `update`; corrupt lockfile; inexpressible renderer
field → plain error → 1.

**Config source selection, stated plainly (the question the human asked about directly):** a
`team` command always has the shipped fallback (embedded in the binary) as its base. It layers an
org-supplied JSON on top only if Chain A resolves one — set once, centrally, via
`$LF_HOME/config.yaml`'s `team_config` key (for a whole machine/CI runner) or per-invocation via
`--team-config` (for a one-off run against a different org file). It layers the repo's own
`.aiteam/team.json` on top of that, if present. There is no per-command flag to pick "fallback
only" or "org only" — the layering is always all applicable layers, highest-precedence field
wins.

### 3. Per-harness templating (spans US-5, US-6, US-7, US-8)

**Recommendation: a pure-function renderer registered in a map keyed by harness name — the
existing `internal/config` convention ("a third tool is a map key, never a new case"). `text/
template` may be used *inside* an individual renderer as an implementation detail; it is never
the extension mechanism itself.**

Two options were considered and rejected:

- **Per-harness template files** — rejected because a template file cannot fail loud. Its
  default behaviour for a field it cannot express is to silently render `<no value>` and
  continue, which is exactly the failure mode the spec forbids. A validation pass would then be
  needed in front of the template to catch that — at which point the validation pass is the real
  renderer and the template is decoration. Template files also move field-name mapping out of
  the compiler's reach: renaming a field in `AgentDefinition` breaks a string in a file instead of
  the build.
- **A common intermediate schema with per-harness adapters** — rejected because the three target
  formats (YAML frontmatter + Markdown body; a skill directory with its own file layout; an agent
  file with a `permission` object) share almost nothing. The intermediate schema would end up as
  the union of all three with per-harness escape hatches — `ResolvedAgent` again, with an extra
  hop and a second place to maintain the mapping.

The renderer contract: `Render(agent ResolvedAgent) ([]RenderedFile, error)`, where
`RenderedFile{Path, Bytes}` is **not** the lockfile's `Artifact` type — a renderer cannot compute
`renderedHash`/`sourceHash`/`baseSourceHash` (it does not know the resolution history or the hash
scheme), so returning `Artifact` would reach the lockfile with zeroed hashes, which is precisely
the bug that makes drift detection lie. The caller hashes after every renderer in the run has
succeeded; writing happens only after that. A renderer given a field it cannot express fails
loudly, naming the agent, harness and dotted path — no partial write, no silent drop.

Determinism obligations, binding on every renderer: sort before iterating any map
(`slices.Sorted(maps.Keys(...))`, matching `internal/config/policy.go`'s `sortedProfileNames`);
never range a map inside a template — pass a pre-sorted slice; explicit `\n` endings, exactly one
trailing newline; no timestamp, version string or hostname in output (anything that varies per
run makes `renderedHash` meaningless and turns every `team status` into a false `modified`). Two
tests per harness: a golden-file test and a render-twice-compare-bytes test — the second is what
actually catches map-order regressions.

**Tradeoff, stated honestly:** adding a fourth harness needs a Go build and a release — there is
no way for an operator to add a harness without one. That is acceptable: harness file formats
change on the harness vendor's schedule, not the operator's, and litefactory already ships as a
binary. If "add a harness without a release" becomes a real requirement, that is a different
feature and should be planned separately rather than pre-paid for here.

## Workflow

**Critical path** (from the architect's task decomposition):
US-1 → US-3 → US-2 → US-5 (contract only) → US-9 (hashing/lockfile/atomic-write engine) →
US-10 (drift classification) → US-4 (CLI scaffold) → US-11 (`team update`).

**Shipping order, phased:**

1. **US-1** — package skeleton, config types, the two-phase decoder. Land the DEV-1 amendment
   (`.aiteam/AC_AMENDMENTS.md` entry, then the `cmd_repo.go` help-text fix) **first**, before any
   other code in this feature — it records the sanction everything else is written under.
2. **US-3** (schema migration) and **US-14**'s `ContainsForbiddenFlag` export — both depend only
   on US-1, both independent of each other, run in parallel with each other and with step 1's
   tail.
3. **US-2** (three-layer merge into `ResolvedAgent`) — depends on US-1 and US-3.
4. **US-5**'s renderer contract (depends on US-2) unblocks **US-6, US-7, US-8** (the three
   concrete renderers), which proceed in parallel with each other. **US-5's own determinism/
   fake-harness test task lands last within US-5**, after US-6/7/8 exist — it exercises all three.
   In parallel, **US-9's hashing/lockfile engine** (depends on US-2 and US-5's contract, not on
   the concrete renderers) can start.
5. Once US-9's lockfile type exists, **US-10's drift classification** and **US-9's atomic-write
   layer** proceed in parallel (both depend on the lockfile, not on each other).
6. **US-4's CLI scaffold** (`cmd_team.go`, all six constructors) depends on US-5's contract,
   US-10's classification, and US-9's atomic-write layer — it needs their error sentinels to
   translate into exit codes. It is a **hard coupling**: the scaffold cannot land before those
   three pieces exist, even though `team list` itself (US-4's own command) is otherwise simple.
7. With the scaffold in place, **US-9's `team add` wiring**, **US-10's `team status` wiring**, and
   **US-11's `team update`** proceed in parallel — each depends on the scaffold plus its own
   already-built engine piece. **US-13's `team manage`** can join this same wave (it only needs
   the scaffold plus US-14's exported predicate).
8. **US-12** (upstream-change reporting) has no tasks of its own — it is the `baseSourceHash`/
   `source-changed` behaviour built in US-10 and surfaced in US-11's `team update` output. Its
   acceptance criteria are validated once both are in place, not implemented separately.
9. **US-13's comprehensive CLI test suite** closes the plan out last — it depends on every other
   CLI command (`team list/show/add/status/update/manage`) being complete.

**Hard couplings, stated once:** US-4's CLI scaffold is not a pure "team list" story — its single
`cmd_team.go` file registers all six commands' constructors and centralises error→exit-code
translation, so US-9, US-10, US-11 and US-13's CLI-facing tasks all depend on it existing first,
even though those stories' non-CLI engine work can and should proceed before it.

## Cross-Story Constraints

### Platform
- `internal/team` is a new sibling package under `internal/`, not a sub-package of `config`;
  `cmd/litefactory/main.go` needs no change — commands self-register through `Commands()`.
- Dependency direction is `cli -> domain`: `internal/team` exports its own sentinel errors
  (`ErrNoLockfile`, `ErrLockfileCorrupt`, `ErrDrifted`, `ErrUnknownTeam`, `ErrUnknownParam`,
  `ErrInexpressible`, `ErrForeignFile`); `cmd_team.go` maps them to `UsageError`/
  `PreconditionError`/exit codes. `PreconditionError` must never appear inside `internal/team`.
- `internal/config` is the only package `internal/team` may import from litefactory, and only for
  two things: `config.ClassifySecret` (already exported) and the newly exported
  `config.ContainsForbiddenFlag`. No cycle risk either direction.
- No concurrency in this feature, and none should be introduced — the work is a handful of small
  files; a worker pool buys nothing here and makes byte-determinism harder to reason about.
  `ctx.Err()` is checked exactly once, immediately before the write loop begins, never inside it
  — once the first `rename` lands, the command finishes the loop, because checking mid-loop would
  manufacture the partial-install state the all-or-nothing rule exists to prevent. Comment that
  reasoning at the check site. Renderers take no `context.Context` at all — they are pure CPU
  functions.
- Errors are wrapped with `%w` and unwrap to sentinels (STYLE-5); `errorlint` rejects `==`
  comparisons. Validation aggregates *all* problems via `errors.Join` in one pass (CFG-5
  precedent) — never fail on the first error found. `fmt.Print*`/`log.*` are forbidden (STYLE-2):
  command output goes to `env.Stdout`; diagnostics go through `env.Logger`.
- `gosec` is on: file writes need explicit, justified permission bits (`0o666`-with-umask is the
  established pattern; any `//nolint:gosec` needs a one-line justification).
- Use `internal/gittest`'s `AssertUnchanged` / `Capture`+`Snapshot.Diff` to prove `--dry-run`
  writes nothing and that a failed `team add` leaves no partial install — do not hand-roll
  directory-walk assertions.
- `internal/team` ships a `doc.go` recording the three-hash model and the drift decision table
  (Shared Decision 1 above), matching the project convention that a package's `doc.go` is its
  real spec.
- No third-party JSON-Schema, migration, or templating library — confirmed against `go.mod` (four
  direct requires, unchanged). Migrations are hand-written sequential Go functions mirroring
  `internal/store/migrate.go`; templating is stdlib `text/template`.
- **Sizing flag:** `json.Decoder.DisallowUnknownFields()` does not deliver the "unknown key at any
  nesting depth, naming its dotted path" acceptance criterion — it cannot reject unknown keys
  inside a `map[string]X` (hits `agents`, `harnesses`, `permission`), and its error text carries
  no path. Delivering that AC needs a hand-written two-phase decode (generic `map[string]any`
  walk against a known-key table, accumulating dotted paths, `errors.Join`'d, then typed decode).
  Still stdlib-only, but this is the single biggest sizing risk in the feature (US-1).
- `internal/config`'s `purity_test.go` (`TestValidationHasNoThirdPartyImports`) scans only
  `internal/config`'s own directory and checks imports, not exported surface — confirmed
  untouched by this feature; it adds a stdlib `string` field and one exported predicate to
  `internal/config`, neither of which adds an import.
- `.aiteam/APP_CONTEXT.md` is stale (documents 17 packages; `internal/` already has more, and
  this feature adds `internal/team` as an 18th+). Not blocking; run `app-context-updater` after
  this feature lands.

### Security
- **Lockfile `path` validation is non-negotiable.** A hand-edited `.aiteam/team.lock.json` is an
  unvalidated write instruction; without the path checks in Shared Decision 1, a crafted lockfile
  turns `team update` into an arbitrary-file-write primitive. This is the highest-severity item
  in the feature.
- **Symlink refusal is non-negotiable.** `os.Lstat` before every write (including the temp+rename
  path); refuse anything that is not a regular file or absent. Without this, a symlink planted at
  an owned path (e.g. `.claude/agents/x.md` → `~/.ssh/authorized_keys`) turns a routine `team add`
  into a write outside the repo entirely.
- **`--prune` must never delete a `foreign` file** (Shared Decision 1's orphan/foreign split) —
  this is the feature's sharpest data-loss hazard and is addressed structurally, not by an
  operator being careful.
- `promptPath` is validated as repo-relative with no `..` and no absolute form at load, **and
  again after any `filepath.Join`** — a validated-then-joined path is where traversal bugs
  actually live. Agent registry keys used as filenames need the same slug-style pattern
  discipline as team slugs (`^[a-z0-9][a-z0-9-]{0,63}$`) — specify it in US-1's implementation,
  the spec does not.
- The closed parameter allowlist for `team manage` (US-13/US-14) is a security control, not a
  UX nicety — `team manage` can set `sandboxMode` and `permission` values that shape an agent's
  future runtime posture. It must not be relaxed to "any key" for convenience, now or later.
- SEC-1 (`--dangerously-skip-permissions` must be unconstructible) stays enforceable at a single
  grep site: `internal/team` calls the newly exported `config.ContainsForbiddenFlag` rather than
  re-assembling an equivalent check from fragments in a second location.
- Every string value in every config layer, and every `team manage set` value, passes through the
  existing secret scan (`config.ClassifySecret`) regardless of key name — no allowlist carve-out.
  Matched text never appears in an error message (existing CFG-4 rule).
- Prompt bodies from an **org-supplied** `team.json` are written into the repo verbatim and are
  **not** scanned for secrets — accepting an org file is a trust decision the operator makes
  explicitly via `--team-config`/`team_config`. Document this in the command's `Long` help text
  rather than implying a validation that does not exist.
- Errors name the offending path and dotted key, **never** the offending value (secret leakage
  risk), aggregated via `errors.Join`.
- Atomic writes: temp file + `rename` per artifact; the whole command is all-or-nothing (render
  everything, validate every target path, then write); the lockfile is written **last**, so a
  mid-run process death leaves the lockfile describing the previous true state.

### Tests
- `make check` must pass, including `internal/config`'s purity test, throughout.
- No command introduced by this feature invokes `git add`, `git commit`, `git stash` or `git
  push` — verified against a `gittest` fixture snapshot of the index.
- No command writes to any path outside the five Owned Paths — verified by a full-tree snapshot
  diff around `team add` and `team update`.
- `make test` runs with `-race`; trivially satisfied since this feature introduces no
  concurrency, and that absence should be preserved rather than "improved" later.

### Ring-fenced (explicitly out of reach for this plan)
- `internal/harness`, `internal/codex`, the daemon, the turn loop, and the store — this feature
  is a synchronous CLI leaf that never touches any of them.
- `internal/git`'s `Clone` type — writes go through `os` directly under the clone root; no method
  is added to `git.Clone`, and `internal/git/surface_test.go` (which pins git *verbs*, not
  filesystem writes) is not touched.
- The existing `AiteamTeam` registration column and `repos.DetectAiteam` — documented descriptive
  metadata that nothing reads back; this feature does not read, write, or repurpose either.

## Deviations

### DEV-1 — SEC-7 override, scoped precisely
**Rule:** SEC-7 [enforced] — "litefactory MUST NOT write inside a registered clone."
**Deviation:** this feature writes to the five Owned Paths listed above, inside a connected repo.
**Status:** settled by the human sponsor before this plan was written; recorded here, not
re-litigated by any story.
**Scope:** SEC-7's *mechanism* is `git.Clone`'s method set, pinned by
`internal/git/surface_test.go`, which forbids git verbs (`Commit`, `Checkout`, `Reset`, `Merge`,
`Pull`, ...) — not filesystem writes into the clone directory. Writing files under the clone root
with `os` never touches `git.Clone`, so that surface test stays green and **must not be edited**.
The deviation is against SEC-7's *prose*, and the prose must be amended, not silently outgrown:
US-1 carries the task to add an `.aiteam/AC_AMENDMENTS.md` entry and to correct the
self-contradicting help text at `internal/cli/cmd_repo.go:57-58` ("litefactory never installs it
and never writes inside the clone"). Ship this feature without that fix and the tool contradicts
itself in its own `--help` output.
**Residual risk:** the blast radius is now "any file under an Owned Path in any connected repo."
That is exactly why the three security items above (lockfile path validation, symlink refusal,
foreign-file protection) are structural requirements rather than nice-to-haves — SEC-7 was
carrying that safety for free, and relaxing it makes the controls explicit code.

### DEV-2 (minor) — `internal/config` gains an exported predicate
Exporting `config.ContainsForbiddenFlag` (alongside the already-exported `ClassifySecret`) widens
a security-critical package's surface. It serves SEC-1 rather than bending it — one definition
instead of a second fragment-assembly site defeating the existing single-grep guarantee — adds no
import, and trips no test (`purity_test.go` checks imports only; there is no `policy_test.go`
surface pin). Recorded so the change to a security-critical package is a decision on the record,
not a drive-by.

## Story Index

| ID | Title | Size | Depends On | Sub-issue |
|----|-------|------|-----------|-----------|
| US-1 | Agent registry + teams-by-reference JSON schema | L | — | https://github.com/marioatcloudvane/litefactory/issues/74 |
| US-2 | Three-layer config resolution (embedded → org → repo-local) | M | US-1, US-3 | https://github.com/marioatcloudvane/litefactory/issues/75 |
| US-3 | `schemaVersion` migrate-on-load, hand-written sequential steps | M | US-1 | https://github.com/marioatcloudvane/litefactory/issues/76 |
| US-4 | CLI scaffold + `litefactory team list` | L | US-2, US-5, US-9, US-10 (interleaved — see Workflow) | https://github.com/marioatcloudvane/litefactory/issues/77 |
| US-5 | Pluggable per-harness renderer contract | L | US-2 (T-17 also needs US-6/7/8 — see Workflow) | https://github.com/marioatcloudvane/litefactory/issues/78 |
| US-6 | Claude renderer → `.claude/agents/<agent>.md` | M | US-5 | https://github.com/marioatcloudvane/litefactory/issues/79 |
| US-7 | Codex renderer → `.codex/skills/<agent>/SKILL.md` | M | US-5 | https://github.com/marioatcloudvane/litefactory/issues/80 |
| US-8 | opencode renderer → `.opencode/agents/<agent>.md` | M | US-5 | https://github.com/marioatcloudvane/litefactory/issues/81 |
| US-9 | `team add` — hashing, lockfile, atomic writes, install command | L | US-2, US-5 (engine); US-4 (CLI wiring — see Workflow) | https://github.com/marioatcloudvane/litefactory/issues/82 |
| US-10 | `team status` — drift detection against the version generated from | L | US-9, US-4 | https://github.com/marioatcloudvane/litefactory/issues/83 |
| US-11 | `team update` — safe-to-overwrite vs. must-flag | L | US-4, US-9, US-10 | https://github.com/marioatcloudvane/litefactory/issues/84 |
| US-12 | Upstream-change reporting against installed `contentVersion` | S | US-10, US-11 (no new tasks — see Workflow) | https://github.com/marioatcloudvane/litefactory/issues/85 |
| US-13 | `team manage <agent> set/get/unset <param>=<value>` | L | US-4, US-14 (final test task also needs US-9/10/11) | https://github.com/marioatcloudvane/litefactory/issues/86 |
| US-14 | Allowlist + forbidden-flag + secret-scan validation of every settable parameter | S | — | https://github.com/marioatcloudvane/litefactory/issues/87 |

## Out of Scope

- Building, modifying, invoking or replacing the external `aiteam` npm tool. litefactory
  generates the Owned Paths itself; coexistence with a prior `aiteam` install is handled only by
  `team add --adopt`.
- Three-way merge or auto-reconciliation of a `modified` + `source-changed` (`conflict`)
  artifact. Report only, in every story that touches drift.
- Any runtime hook that makes litefactory's Go code choose which sub-agent a harness invokes. The
  generated files remain the only mechanism; `internal/harness`/`internal/codex` prompt
  construction is unchanged by this feature.
- Adding `opencode` to `agent.Kind` / `turn.RunnerSet` as a runnable harness. This feature only
  *generates files for* opencode; litefactory still runs Claude and Codex only.
- Reading, writing or repurposing the existing `AiteamTeam` registration column
  (`internal/repos/registration.go:123`) or `repos.DetectAiteam`. That value stays descriptive
  metadata.
- Regenerating or replacing `.claude/AGENT_ROSTER.md`.
- Adopting an external cross-tool agent-definition standard (Agent Manifest, A2A Agent Card) as
  the schema.
- Any JSON-Schema validation library, any config-migration library, any templating library beyond
  stdlib `text/template`.
- Distributing the fallback config outside the binary (registry, network fetch). `//go:embed`
  only.
- A `team remove`/uninstall command.
- Per-user (`~/.claude/agents/`) or machine-global agent generation. Repo-scoped only.

## Provenance

- Citation base: see `$session/RESEARCH_BRIEF.md` — not duplicated here.
- Known-stale areas: `.aiteam/APP_CONTEXT.md` documents 17 `internal/` packages; the tree already
  has more (`agent`, `attach`, `codex`, `planbrief`), and this feature adds `internal/team` as a
  further new package. Re-run `app-context-updater` after this feature merges.
- Re-pinning requirements: none identified beyond the `APP_CONTEXT.md` refresh above — the
  architect's review found no other drift between the research brief's citations and the current
  tree.
