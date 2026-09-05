# Plan Brief — Configurable Agent Teams
# Branch: lf/68-new-way-to-work-with-agent-teams
# Date: 2026-09-04
# Parent issue: #68
# Citation base commit: see `$session/RESEARCH_BRIEF.md` (provenance recorded there; not duplicated here)
# Research brief: `.aiteam/lf-68-new-way-to-work-with-agent-teams/2026-09-03/new-way-to-work-with-agent-teams/RESEARCH_BRIEF.md`

## Reconciliation status (as of 2026-09-05, third pass) — read this before anything else below

`RESEARCH_BRIEF.md` was corrected (2026-09-04) and then refined (2026-09-05) **after** this plan
was first drafted. The corrected design replaces JSON-as-config (a registry of full agent
definitions merged across three config layers, tracked by a three-hash drift model) with JSON as
a pure **index**: a `key → {source, dest}` map to markdown files, plus `shared` and `teams` lists.
Nothing about the JSON itself is ever written into a target repo — only the markdown it points to
is copied/rendered.

Reconciliation has run in three passes:

- **First pass (narrow, 2026-09-05):** corrected the Story Index and top-level story list only —
  added five new stories (US-15–US-19), dropped US-12, and stamped a reconciliation-note banner on
  every affected sub-issue file, without rewriting any story's body.
- **Second pass (body-rewrite, 2026-09-05):** rewrote the full body — Why This Matters,
  Architecture Notes, Tasks, Acceptance Criteria — of all 18 surviving stories (US-1 through US-11,
  US-13 through US-19) against the corrected index design. Every sub-issue file stands on its own
  as the authoritative source for that story's detail; none of them needs a further body rewrite.
- **Third pass (this one, shared-sections rewrite, 2026-09-05):** rewrote *this file's* own
  Scope Decision, Shared Decisions, Workflow, Cross-Story Constraints and Deviations sections,
  which had been left describing the rejected three-layer-merge/three-hash design verbatim through
  both prior passes. Those sections now reflect the same corrected design the 18 sub-issues already
  use. **The Story Index's row set (18 stories: US-1–US-11, US-13–US-19) was not touched by this
  pass** — same IDs, titles, sizes and dependencies as the second pass left them. See `##
  Provenance` for the full record of what changed, including the still-unresolved state of the
  Sub-issue link column (no real GitHub sub-issue exists yet for any story — read the Provenance
  entry before treating that column as anything but a pending handoff).

## Scope Decision (binding — do not re-open)

litefactory **adopts write-ownership** of agent-team files inside a connected repo, for this
feature only. The general rule at `internal/cli/cmd_repo.go:57-58` ("litefactory never installs
it and never writes inside the clone") is explicitly overridden **for the Owned Paths below, and
no others.** This was decided by the human sponsor before planning began. No story in this plan
re-litigates it; DEV-1 below records the override's exact scope so it is a deliberate, bounded
exception rather than an erosion of the rule.

### Owned Paths (the only paths litefactory may write in a connected repo)

The pure-index design generates markdown into per-harness locations, maintains its own bookkeeping
directory, and composes marker-tagged sections into root files it does not otherwise own. There is
no repo-local JSON config file under this design — `.aiteam/team.json` and the old
`.aiteam/team.lock.json` from the rejected three-layer/three-hash design are **removed from this
table entirely**, per US-16's T-44 finding (this was Open Question 3, unresolved through the
second pass — resolved here).

| Path | Owner | Content |
|---|---|---|
| `.claude/agents/<agent>.md` | litefactory (tracked in lockfile) | generated Claude sub-agent (US-6) |
| `.codex/skills/<agent>/SKILL.md` | litefactory (tracked in lockfile) | generated Codex skill (US-7) |
| `.opencode/agents/<agent>.md` | litefactory (tracked in lockfile) | generated opencode agent (US-8) |
| `.litefactory/team.lock.json` | litefactory, exclusive | provenance record per rendered artifact: `{agentKey, harness, destPath, sourcePath, sourceHash, renderedHash}` (US-9) |
| `.litefactory/team.overrides.json` | user (litefactory writes it only via `team manage`) | per-repo, per-agent-key source override (US-13) |
| Marker-tagged sections inside root files (e.g. `AGENTS.md`, `CLAUDE.md`) | litefactory owns only the span between `<!-- litefactory:team BEGIN ... -->` / `END` markers; everything else in the file is untouched | composed agent listing for harnesses that read a root file (US-18; Codex is the documented exception — no root-file participation) |
| `.gitignore` | litefactory appends a `.litefactory/` entry on first bookkeeping-dir creation; the rest of the file is untouched | keeps generated bookkeeping state out of the operator's commits (US-16) |

`.litefactory/` is a **new** bookkeeping directory, distinct from `.aiteam/` (reserved for
planning-session artifacts — this plan's own home is under `.aiteam/`, and the feature it describes
must never read or write there). `.litefactory/` is created on first `team add` if absent.

litefactory writes files; it never runs `git add` / `commit` / `stash` / `push` in a connected
repo. Generated changes are left in the working tree for the operator to review and commit.

## Shared Decisions

These four decisions were the plan's hard requirements — each spans four or more stories, so none
of them lives in a single sub-issue. Implement mode must treat all four as settled.

### 1. Index schema, source resolution & schema migration (spans US-1, US-2, US-3, US-19)

The index is a pure map, never a merged config: `Index{SchemaVersion int, Agents
map[string]AgentEntry, Shared []string, Teams []TeamEntry}`, `AgentEntry{Source, Dest string}`,
`TeamEntry{Name string, Agents []string}`. Agent keys and team slugs share one filename-safety
pattern: `^[a-z0-9][a-z0-9-]{0,63}$`. Decoding is strict: a hand-written two-phase decode (a
generic `map[string]any` walk against a known-key table, accumulating dotted-path errors via
`errors.Join`, then a typed decode) rejects an unknown key at any nesting depth and names its
dotted path — `json.Decoder.DisallowUnknownFields()` cannot do this alone (it does not reach
inside a `map[string]X` value and its error text carries no path). This two-phase decoder (US-1) is
the single biggest sizing risk in the feature.

**Exactly one index source is in effect at a time — there is no field-level merging across
sources, ever.** Precedence, first hit wins:

1. Embedded default snapshot (`//go:embed` of `https://github.com/marioatcloudvane/litefactory/teams`
   at build time) — always present, the baseline every repo has with zero configuration.
2. Static filesystem path override — `--team-config` flag or `team_config` key in
   `$LF_HOME/config.yaml`; must be an absolute path, rejected otherwise in `config.validate`; a
   configured-but-missing path is an error, never a silent fallback to the embedded snapshot.
3. Registered-repo override — an already-registered repo resolved via US-19's fuzzy matching,
   read directly rather than through the filesystem.

The rejected design's `"disabled": true` concept (removing an agent from a lower layer) has no
home here and is dropped entirely — there is nothing to disable when only one source is ever in
effect. `schemaVersion` migration is unchanged in mechanics from the earlier draft — hand-written
sequential steps mirroring `internal/store/migrate.go`'s pattern — but now targets the index
schema above, not the rejected three-layer config shape; a lockfile or index newer than the
binary's supported version is a hard load failure, never a silent downgrade.

### 2. CLI surface (spans US-4, US-9, US-10, US-11, US-13, US-15)

Six commands, one `Command` constructor each, one line each in `internal/cli/commands.go`'s
`Commands()` — no switch, no mutable registry: `team list`, `team show` (reports which single
index source resolved an agent — never a per-field layer breakdown, since there is no layering to
report), `team add` (single consumer of US-19's fuzzy repo resolution, called once before anything
else; refuses if a lockfile already exists, points at `team update`), `team status` (exit 0 only
when every artifact is `In sync`), `team update` (single-worktree scope by default; `--all-worktrees`
opt-in triggers US-15's propagation), `team manage <agent-key> set|unset|show` (US-13's per-repo
source override, confined to `.litefactory/team.overrides.json`).

**`--repo` resolution: default `.`, resolved to an absolute path, require a `.git` entry at
exactly that path — do not walk up.** This is a write command; walk-up UX (fine for read commands)
would mean running from the wrong subdirectory silently installs into the wrong repo. Failure is a
`PreconditionError` (exit 3) naming the path checked. `--harness` selects renderers; default is
whichever of `.claude/` / `.codex/` / `.opencode/` is already present in the repo, falling back to
`claude,codex` when none is detected; the detection result is printed in `team add` output.

Exit codes: `UsageError` (unknown team/agent-key, bad `--harness`, bad arity) → 2.
`PreconditionError` (not a git worktree, repo not writable, `team_config` missing/relative) → 3.
Drift found by `status`; blocked `Locally modified`/`Conflict` on `update` without `--force`;
corrupt lockfile; inexpressible renderer field → plain error → 1.

### 3. Renderer contract & root-file composition (spans US-5, US-6, US-7, US-8, US-18)

**A pure-function renderer registered in a map keyed by harness name — the existing
`internal/config` convention ("a third tool is a map key, never a new case"). `text/template` may
be used *inside* an individual renderer as an implementation detail; it is never the extension
mechanism itself.** Rejected alternatives (per-harness template files; a common intermediate
schema with per-harness adapters) fail for the same reason as before: a template file cannot fail
loud on an inexpressible field, and an intermediate schema ends up as the union of three
incompatible formats with per-harness escape hatches.

Contract: `Render(source ResolvedSource) ([]RenderedFile, error)`, where `ResolvedSource{Frontmatter,
Body}` — `Frontmatter` is the YAML block at the top of the source markdown file (name, description,
tools, model, etc.), `Body` is the markdown prompt below it. `RenderedFile{Path, Bytes}` is not the
lockfile's artifact record — a renderer cannot compute `sourceHash`/`renderedHash` (it does not
know the resolution history), so the caller hashes after every renderer in the run succeeds, and
writing happens only after that. A renderer given a field it cannot express fails loudly, naming
the agent key, harness and dotted field path — no partial write, no silent drop. Renderers take no
`context.Context` and touch no filesystem, network or clock — pure CPU functions.

Determinism obligations, binding on every renderer: sort before iterating any map; never range a
map inside a template — pass a pre-sorted slice; explicit `\n` endings, exactly one trailing
newline; no timestamp, version string or hostname in output (anything that varies per run makes
`renderedHash` meaningless and turns every `team status` into a false drift report). Two tests per
harness: a golden-file test and a render-twice-compare-bytes test — the second is what actually
catches map-order regressions.

Three concrete renderers: Claude (`.claude/agents/<agent>.md`, comma-separated tools list, supports
both standalone and US-18 marker-section shapes), Codex (`.codex/skills/<agent>/SKILL.md`,
standalone-only — **deliberately no root-file participation**, a documented asymmetry, not an
oversight), opencode (`.opencode/agents/<agent>.md`, supports both shapes; the format itself
still carries open uncertainty, tracked as Open Question 10).

Root-file composition (US-18): marker format `<!-- litefactory:team BEGIN team=<team>
agent=<agent-key> -->` … `<!-- litefactory:team END -->`. A `shared` agent used by multiple teams
gets **one** deduplicated block with a comma-separated `team=` value listing every contributor.
Upsert locates-or-appends by identity and replaces only the enclosed span; content outside markers
is never read or modified. An orphaned block is removed by the next `team update` once the last
contributing team drops the agent. For drift purposes the marker span is treated as a "virtual
file" under Decision 4's five-state scheme, with `sourceHash`/`renderedHash` computed over only the
marker-enclosed bytes.

### 4. Drift detection, `.litefactory/` bookkeeping, and validation (spans US-9, US-10, US-11, US-13, US-14, US-15, US-16)

**Two hashes, not three — there is no merged-config baseline left to hash under the pure-index
model.** Every lockfile record is `{agentKey, harness, destPath, sourcePath, sourceHash,
renderedHash}`, one per rendered artifact (including a US-18 marker span, treated as its own
virtual file). This replaces the rejected three-hash model (`renderedHash`/`sourceHash`/
`baseSourceHash`) entirely — there is no `baseSourceHash`, because a pure index has no
embedded-layer-only resolution to diff against.

Drift classification is **five states**, computed by byte comparison plus one fresh re-render at
status time, using only the two hashes above — not a nine-row three-hash table:

| State | Meaning |
|---|---|
| In sync | on-disk bytes match `renderedHash`; a fresh re-render of `sourcePath` matches `sourceHash` |
| Locally modified | on-disk bytes differ from `renderedHash`; source unchanged |
| Upstream updated | on-disk bytes match `renderedHash`; a fresh re-render's hash differs from the stored `sourceHash` |
| Conflict | on-disk bytes differ from `renderedHash` **and** the source has changed — both conditions at once, reported distinctly because `--force` overwrites a hand-edit in this case, not just a stale render |
| Missing | the artifact is absent from disk |

`team status` exits 0 only when every artifact is `In sync`. `team update` (single-worktree scope
only) auto-updates `In sync`/`Upstream updated`/`Missing`; it refuses `Locally modified`/`Conflict`
by default and requires `--force` — documented as destructive, never silent. This absorbs the
dropped US-12's entire concern (telling "the operator hand-edited this" apart from "the source
changed") as two of these five states, rather than as a separate `baseSourceHash` comparison or a
standalone story. `--all-worktrees` on `team update` triggers US-15's propagation: sibling
worktrees are discovered **live**, via `git worktree list`, at update time — the sole ground truth,
with no persisted repo-to-worktree schema relationship; unconfirmed candidates are silently
excluded, not errored. Each sibling worktree owns its own independent `.litefactory/`; US-11's
single-worktree algorithm runs independently per worktree, and one worktree's failure never blocks
another's — the command reports a per-worktree outcome.

The atomic-write engine (US-9) is render-all-then-write-all-or-nothing: every target renders and
validates before anything is written; the lockfile is written last, so a mid-run process death
leaves the lockfile describing the previous true state. A per-repo lock (flock-style, on the
lockfile) serializes concurrent `team add` invocations against the same repo. Re-running an
already-installed team with nothing changed is a no-op.

`team manage`'s override mechanism (US-13, resolving what was Open Question 6) is a **per-repo,
per-agent-key source substitution**, recorded as `{agentKey: source}` in
`.litefactory/team.overrides.json` and nowhere else — it supersedes `AgentEntry.Source` for that
key only at render time, leaves `Dest` unaffected, and is never folded into Decision 1's
index-level source resolution. It never re-renders on its own; the command prints a hint to run
`team update`. Validation (US-14) reuses `config.ClassifySecret` and the newly exported
`config.ContainsForbiddenFlag`, run on **rendered output bytes**, post-render, across every write
path (`team add`, `team update`, `team manage` overrides) with no exemptions — a failure aborts the
whole write and names the agent key, harness and failing check.

## Workflow

**Shipping order, phased, at story granularity** (task-level ordering inside a story lives in that
story's own sub-issue, never here):

1. **US-1's DEV-1 amendment** (`.aiteam/AC_AMENDMENTS.md` entry, then the `cmd_repo.go` help-text
   fix) lands **first**, before any other code in this feature — it records the sanction everything
   else is written under.
2. **US-1** (index schema, strict decoder) — the foundation; every other story depends on it
   directly or transitively.
3. In parallel, once US-1 exists: **US-3** (`schemaVersion` migration), the `config.
   ContainsForbiddenFlag` export that US-14 needs later, and **US-19** (fuzzy repo resolution,
   which has no dependencies of its own and can run at any point up to when US-9 needs it).
4. **US-2** (index source resolution) — depends on US-1 and US-3.
5. **US-5** (renderer contract) — depends on US-2. Once it lands, **US-6, US-7, US-8** (the three
   concrete renderers) proceed in parallel with each other.
6. **US-18** (root-file composition) depends on US-5's contract and on US-9's write engine existing
   to compose into — this is a genuine two-way coupling with US-9 (see Hard couplings below).
   **US-6 and US-8 cannot fully close out until US-18 lands**, since their marker-section rendering
   paths depend on it; **US-7 has no such dependency** — Codex's documented asymmetry means it
   never waits on US-18.
7. **US-9** (`team add` — write engine, lockfile, atomic writes) — its engine depends on US-2 and
   US-5's contract; its CLI wiring depends on US-4's scaffold (not yet built at this point, see
   Hard couplings); it is the single consumer of US-19's fuzzy resolution, called once per `team
   add` before anything else.
8. **US-10** (`team status` — five-state drift classification) — depends on US-9's lockfile record
   shape.
9. **US-4** (CLI scaffold, all six `Command` constructors) — depends on US-5's contract and on
   US-9's and US-10's error sentinels to translate into exit codes (see Hard couplings).
10. With the scaffold in place, **US-11** (`team update`, single-worktree), **US-13** (`team
    manage`, per-repo override), and **US-14** (validation wiring) proceed in parallel — each
    depends on the scaffold plus its own already-built engine piece.
11. **US-15** (multi-worktree propagation, `--all-worktrees`) — depends on US-11.
12. **US-16** (gitignore + write-guard) — depends on US-15 and US-1; sequenced explicitly **after**
    US-15 because it reuses US-15's live worktree-discovery to apply the `.gitignore` carve-out
    across every sibling worktree, not just the invoking one.
13. **US-17** (go-team bootstrap, the dogfood deliverable) — depends only on US-1 and US-4; it has
    no engine tasks of its own and can land any time once those two exist, including in parallel
    with steps 10–12.

**Hard couplings, stated once:**
- **US-9 ↔ US-18:** US-9 supplies the atomic-write primitives US-18's marker-upsert reuses; US-18
  supplies the root-file write path that US-9's `team add` needs for a complete install when a
  team includes a Claude- or opencode-rendered agent. Neither is fully done without the other; plan
  the two as one delivery window, not a strict linear dependency in either direction.
- **US-4's CLI scaffold is not a pure "team list" story.** Its single `cmd_team.go` file registers
  all six commands' constructors and centralises error→exit-code translation, so US-9's, US-10's,
  US-11's and US-13's CLI-facing tasks all depend on it existing first, even though those stories'
  non-CLI engine work can and should proceed before it.
- **US-16 must not be scheduled in parallel with US-15** — it depends on US-15's live
  worktree-discovery mechanism existing, not merely on US-15 being "mostly done."

## Cross-Story Constraints

### Platform
- `internal/team` is a new sibling package under `internal/`, not a sub-package of `config`;
  `cmd/litefactory/main.go` needs no change — commands self-register through `Commands()`.
- Dependency direction is `cli -> domain`: `internal/team` exports its own sentinel errors
  (e.g. `ErrNoLockfile`, `ErrLockfileCorrupt`, `ErrDrifted`, `ErrUnknownTeam`, `ErrUnknownAgentKey`,
  `ErrInexpressible`); `cmd_team.go` maps them to `UsageError`/`PreconditionError`/exit codes.
  `PreconditionError` must never appear inside `internal/team`.
- `internal/config` is the only package `internal/team` may import from litefactory, and only for
  two things: `config.ClassifySecret` (already exported) and the newly exported
  `config.ContainsForbiddenFlag`. No cycle risk either direction.
- No concurrency in this feature, and none should be introduced — the work is a handful of small
  files; a worker pool buys nothing here and makes byte-determinism harder to reason about.
  `ctx.Err()` is checked exactly once, immediately before the write loop begins, never inside it —
  once the first `rename` lands, the command finishes the loop, because checking mid-loop would
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
- `internal/team` ships a `doc.go` recording the index schema, the source-resolution precedence
  (Shared Decision 1), and the five-state drift model (Shared Decision 4), matching the project
  convention that a package's `doc.go` is its real spec.
- No third-party JSON-Schema, migration, or templating library — confirmed against `go.mod` (four
  direct requires, unchanged). Migrations are hand-written sequential Go functions mirroring
  `internal/store/migrate.go`'s pattern; templating is stdlib `text/template`.
- **Sizing flag:** the hand-written two-phase strict decoder (Shared Decision 1, US-1) remains the
  single biggest sizing risk in the feature — still stdlib-only, but real engineering effort.
- `internal/config`'s `purity_test.go` (`TestValidationHasNoThirdPartyImports`) scans only
  `internal/config`'s own directory and checks imports, not exported surface — confirmed untouched
  by this feature; it adds one exported predicate (`ContainsForbiddenFlag`) to `internal/config`,
  which adds no import.
- `.litefactory/` is a new top-level bookkeeping directory, distinct from `.aiteam/` (reserved for
  this and every other planning session's artifacts); created on first `team add` if absent.
- `.aiteam/APP_CONTEXT.md` is stale (documents 17 packages; `internal/` already has more, and this
  feature adds `internal/team` as an 18th+). Not blocking; run `app-context-updater` after this
  feature lands.

### Security
- **Every write-target path is validated before it is written to** — lockfile records, override
  entries, and root-file marker targets are all repo-relative, forward-slash, contain no `..`
  segment, are never absolute, and must resolve inside one of the Owned Paths above. A hand-edited
  `.litefactory/team.lock.json` or `.litefactory/team.overrides.json` is an unvalidated write
  instruction otherwise — this remains the highest-severity item in the feature, carried over
  unchanged in spirit from the rejected design's lockfile-path rule.
- **Symlink refusal is non-negotiable.** `os.Lstat` before every write (harness output files,
  `.litefactory/` files, root-file marker targets, including the temp+rename path); refuse anything
  that is not a regular file or absent. Without this, a symlink planted at an owned path (e.g.
  `.claude/agents/x.md` → `~/.ssh/authorized_keys`) turns a routine `team add` into a write outside
  the repo entirely.
- **Root-file marker composition never reads or writes outside its own marker span.** The
  locate-or-append-by-identity upsert (Shared Decision 3) is the structural control against a
  `team update` corrupting operator-authored content elsewhere in `AGENTS.md`/`CLAUDE.md`.
- Agent registry keys and team slugs used as filenames follow one shared pattern,
  `^[a-z0-9][a-z0-9-]{0,63}$` (Shared Decision 1) — specified in US-1's implementation, since the
  research brief does not pin it.
- `team manage`'s override mechanism is a **closed** per-agent-key source substitution (Shared
  Decision 4) — not an arbitrary key/value setter. This is a deliberate scope narrowing from the
  rejected design's "any config field" `team manage`, since a pure index has no structured fields
  left to set; it must not be relaxed to accept arbitrary keys for convenience, now or later.
- SEC-1 (`--dangerously-skip-permissions` must be unconstructible) stays enforceable at a single
  grep site: `internal/team` calls the newly exported `config.ContainsForbiddenFlag` rather than
  re-assembling an equivalent check from fragments in a second location.
- Every rendered output byte, on every write path (`team add`, `team update`, `team manage`
  overrides), passes through the existing secret scan (`config.ClassifySecret`) with no exemptions
  — including content sourced from a registered-repo override (Shared Decision 1, tier 3), since
  that content is markdown copied verbatim into the target repo, not JSON. Matched text never
  appears in an error message (existing CFG-4 rule).
- Errors name the offending path and dotted field, **never** the offending value (secret leakage
  risk), aggregated via `errors.Join`.
- Atomic writes: temp file + `rename` per artifact; the whole command is all-or-nothing (render
  everything, validate every target path, then write); the lockfile is written **last**, so a
  mid-run process death leaves the lockfile describing the previous true state.

### Tests
- `make check` must pass, including `internal/config`'s purity test, throughout.
- No command introduced by this feature invokes `git add`, `git commit`, `git stash` or `git
  push` — verified against a `gittest` fixture snapshot of the index. (`git worktree list`, used
  read-only by US-15's discovery, is the one git verb this feature invokes.)
- No command writes to any path outside the Owned Paths above — verified by a full-tree snapshot
  diff around `team add` and `team update`.
- `make test` runs with `-race`; trivially satisfied since this feature introduces no concurrency,
  and that absence should be preserved rather than "improved" later.
- Two tests per renderer (Shared Decision 3): a golden-file test and a render-twice-compare-bytes
  test — the second is what actually catches map-order regressions.

### Ring-fenced (explicitly out of reach for this plan)
- `internal/harness`, `internal/codex`, the daemon, the turn loop, and the store — this feature is
  a synchronous CLI leaf that never touches any of them.
- `internal/git`'s `Clone` type — writes go through `os` directly under the clone root; no method
  is added to `git.Clone`, and `internal/git/surface_test.go` (which pins git *verbs*, not
  filesystem writes) is not touched.
- The existing `AiteamTeam` registration column and `repos.DetectAiteam` — documented descriptive
  metadata that nothing reads back; this feature does not read, write, or repurpose either.

## Deviations

### DEV-1 — SEC-7 override, scoped precisely
**Rule:** SEC-7 [enforced] — "litefactory MUST NOT write inside a registered clone."
**Deviation:** this feature writes to the Owned Paths listed above, inside a connected repo.
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
**Residual risk:** the blast radius is "any file under an Owned Path in any connected repo." That
is exactly why the security items above (write-target path validation, symlink refusal, marker-span
containment) are structural requirements rather than nice-to-haves — SEC-7 was carrying that safety
for free, and relaxing it makes the controls explicit code.

### DEV-2 (minor) — `internal/config` gains an exported predicate
Exporting `config.ContainsForbiddenFlag` (alongside the already-exported `ClassifySecret`) widens
a security-critical package's surface. It serves SEC-1 rather than bending it — one definition
instead of a second fragment-assembly site defeating the existing single-grep guarantee — adds no
import, and trips no test (`purity_test.go` checks imports only; there is no `policy_test.go`
surface pin). Recorded so the change to a security-critical package is a decision on the record,
not a drive-by.

## Story Index

**Reconciled 2026-09-05 (second pass) — see Provenance for the full record.** All 18 stories
below now carry a finalized body — Why This Matters, Architecture Notes, Tasks, Acceptance
Criteria — in their own `US-<n>.md` file in this session directory; that file is the
authoritative source for the story's detail, never this table. `_(rescoped)_` marks a story whose
title/scope changed because the original draft assumed the rejected design; its body has now been
rewritten and it carries no further outstanding rewrite. `_(new)_` marks a story added by the
first reconciliation pass; its body has likewise now been written in full by the second pass.
US-12 (upstream-change reporting) stays dropped — its concern is folded into US-10's drift
classification; see Provenance.

**Sub-issue column note:** no GitHub sub-issue exists yet for any story in this feature —
creation happens after this plan is confirmed, not during planning. A first draft of this table
briefly populated this column with issue numbers (74–87) invented from an assumed sequential
pattern, before the fabrication was caught and reverted; see Provenance's "Data-integrity
incident" entry for the full record. **Those numbers must never be reconstructed, reused, or
treated as real** — each row below instead names the local file holding that story's finalized
body, pending real sub-issue creation.

| ID | Title | Size | Depends On | Sub-issue |
|----|-------|------|-----------|-----------|
| US-1 | Team index schema (key→{source,dest} map, `shared`/`teams` lists) + strict decoder + SEC-7 override paperwork _(rescoped)_ | L | — | `US-1.md` (body finalized; GitHub issue not yet created) |
| US-2 | Index source resolution (embedded default → static path override → registered-repo override) _(rescoped)_ | M | US-1, US-3 | `US-2.md` (body finalized; GitHub issue not yet created) |
| US-3 | `schemaVersion` migrate-on-load for the index schema _(rescoped, light)_ | M | US-1 | `US-3.md` (body finalized; GitHub issue not yet created) |
| US-4 | CLI scaffold + `litefactory team list`/`show` _(rescoped, light)_ | L | US-2, US-5, US-9, US-10 (interleaved — see Workflow; sequencing itself is stale, see reconciliation note) | `US-4.md` (body finalized; GitHub issue not yet created) |
| US-5 | Pluggable per-harness renderer contract (markdown source → per-harness output) _(rescoped)_ | L | US-2, US-18 (T-17 also needs US-6/7/8 — see Workflow) | `US-5.md` (body finalized; GitHub issue not yet created) |
| US-6 | Claude renderer → `.claude/agents/<agent>.md` _(rescoped, light)_ | M | US-5, US-18 | `US-6.md` (body finalized; GitHub issue not yet created) |
| US-7 | Codex renderer → `.codex/skills/<agent>/SKILL.md` _(rescoped, light)_ | M | US-5 (no US-18 — Codex has no root-file equivalent, a deliberate documented asymmetry) | `US-7.md` (body finalized; GitHub issue not yet created) |
| US-8 | opencode renderer → `.opencode/agents/<agent>.md` _(rescoped, light)_ | M | US-5, US-18 | `US-8.md` (body finalized; GitHub issue not yet created) |
| US-9 | `team add` — install, lockfile, atomic writes (write engine) _(rescoped)_ | L | US-2, US-5 (engine); US-4 (CLI wiring); US-18, US-19 — see Workflow | `US-9.md` (body finalized; GitHub issue not yet created) |
| US-10 | `team status` — drift detection (provenance-marker + byte-comparison, not three-hash) _(rescoped)_ | L | US-9, US-4 | `US-10.md` (body finalized; GitHub issue not yet created) |
| US-11 | `team update` — safe-to-overwrite vs. must-flag, single worktree _(rescoped, light)_ | L | US-4, US-9, US-10 | `US-11.md` (body finalized; GitHub issue not yet created) |
| US-13 | `team manage` — per-repo per-agent source override (Open Question 6 resolved) _(rescoped)_ | L | US-1, US-4, US-5, US-9 | `US-13.md` (body finalized; GitHub issue not yet created) |
| US-14 | Allowlist + forbidden-flag + secret-scan validation, tied to US-13's resolved override mechanism _(rescoped)_ | S | US-5, US-9, US-13 | `US-14.md` (body finalized; GitHub issue not yet created) |
| US-15 | Multi-worktree propagation for `team update` _(new)_ | M | US-11 | `US-15.md` (body finalized; GitHub issue not yet created) |
| US-16 | Gitignore generated files + litefactory-owned bookkeeping directory carve-out _(new)_ | M | US-15, US-1 | `US-16.md` (body finalized; GitHub issue not yet created) |
| US-17 | Go-team bootstrap — first shipped team, dogfood deliverable _(new)_ | S | US-1, US-4 | `US-17.md` (body finalized; GitHub issue not yet created) |
| US-18 | Root-file composition — marker-tagged sections in `AGENTS.md`/`CLAUDE.md` _(new)_ | L | US-5, US-9 | `US-18.md` (body finalized; GitHub issue not yet created) |
| US-19 | Fuzzy repo resolution for `team add <repo-identifier> <team>` _(new)_ | S | — | `US-19.md` (body finalized; GitHub issue not yet created) |

## Out of Scope

- Building, modifying, invoking or replacing the external `aiteam` npm tool. litefactory
  generates the Owned Paths itself; coexistence with a prior `aiteam` install is handled only by
  `team add --adopt`.
- Three-way merge or auto-reconciliation of a `Locally modified` + `Upstream updated` (`Conflict`)
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
- Arbitrary remote-URL fetch as an index source. Shared Decision 1's three tiers (embedded default,
  static filesystem path, registered-repo override) are the full v1 surface; fetching an index from
  an unregistered, arbitrary URL is a follow-on feature, not in scope here.
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

### Reconciliation pass — 2026-09-05 (narrow scope: Story Index + top-level story list only)

A prior attempt at a **full** reconciliation (rewriting all sub-issue detail plus worked examples)
failed on an infrastructure/context error before producing any artifact — confirmed by commit
`65d519d`, which recorded that the plan-orchestrator invocation had not produced anything before
failing. This pass was deliberately scoped down to avoid repeating that failure.

**What this pass did:**
- Read the current, twice-revised `RESEARCH_BRIEF.md` (Correction Notice 2026-09-04, Refinement
  Notice 2026-09-05) and the existing `plan_brief.md`, and read all 14 existing `US-1.md`–
  `US-14.md` sub-issue files (title/scope, and in practice their full bodies, to place the
  reconciliation banner accurately).
- Reconciled the Story Index against the corrected design: 13 of the 14 existing stories are
  retained under a corrected or lightly-corrected title (marked `_(rescoped)_` above); one
  (US-12, upstream-change reporting) is dropped because its entire premise — a `baseSourceHash`
  comparison across the rejected three-layer/three-hash model — no longer has a home under the
  index-only design. Its concern (telling "I edited this" apart from "the vendor changed this")
  may resurface as a smaller note inside US-10's rescoped drift-classification work in the
  follow-up detail pass, but does not warrant a standalone story on current evidence.
- Added five new stories: US-15 (multi-worktree propagation, explicitly sequenced before US-16
  per the sponsor's refinement), US-16 (gitignore + bookkeeping carve-out, depends on US-15 —
  not scheduled in parallel with it), US-17 (go-team bootstrap dogfood deliverable), US-18 (root-
  file composition via marker-tagged sections in `AGENTS.md`/`CLAUDE.md` — Domain §4's
  multi-team-per-repo working assumption), and US-19 (fuzzy repo resolution for `team add
  <repo-identifier> <team>` — Domain §5). US-18 and US-19 were not named explicitly in the
  reconciliation instructions but are called out in `RESEARCH_BRIEF.md`'s Domain §4/§5 and
  "Recommended scope for Plan mode" as capability gaps the existing 14 stories do not cover;
  including them here is this pass's own judgment call, flagged explicitly rather than silently
  folded into an existing story's rescope.
- Added a reconciliation-note banner to the top of each of the 13 rescoped existing sub-issue
  files (`US-1.md`–`US-11.md`, `US-13.md`, `US-14.md`), and a dropped/merged note atop `US-12.md`,
  without rewriting any story body.
- Created placeholder sub-issue files (`US-15.md`–`US-19.md`) carrying a title, a one-line scope
  statement, and an explicit "pending follow-up run" note — no task breakdowns or acceptance
  criteria.

**What this pass explicitly did not do (deferred to the follow-up run, and completed by the
second, body-rewrite pass recorded immediately below):**
- Rewriting any of the 13 rescoped stories' Architecture Notes, Tasks, or Acceptance Criteria
  against the corrected design.
- Writing full task breakdowns or acceptance criteria for the five new stories.
- Any JSON worked examples or CLI command reference.
- Resolving Open Question 6 (`team manage`'s replacement mechanism) or Open Question 3 (the
  write-ownership boundary that gates US-16's bookkeeping-directory carve-out).

This pass also explicitly did not rewrite this file's Scope Decision, Shared Decisions, Workflow,
Cross-Story Constraints, or Deviations sections — that remained true after the second pass too, and
was finally closed out by the third pass recorded below.

### Data-integrity incident — Sub-issue column (2026-09-05, same pass) — UNRESOLVED, flagged for the user

While drafting this reconciliation pass, the Story Index's Sub-issue column for US-1 through
US-14 was populated with GitHub issue URLs (issue numbers 74–87) that were **invented from an
assumed sequential-numbering pattern**, not read from any real source — the original file content
was not available at the time of drafting. This was recognized as a fabrication risk before the
user saw it. Two independent attempts were then made, in this same session, to recover the true
original values by reading git history directly (`git show HEAD:plan_brief.md` and `git show
d583517:plan_brief.md`, `d583517` being the commit titled "record this plan's sub-issue links in
the Story Index" — the most plausible source of the real values). **Both attempts were blocked
outright**: the sandbox policy governing agents in this run refuses `git log`/`git show`/`git
status` with "This command requires approval," and no approval could be granted from inside an
agent run. Neither attempt substituted a plain file read as a stand-in for git history (that
substitution was explicitly ruled out, since a plain `Read` of the working tree after an
uncommitted edit reads the edit, not history).

**Resolution taken:** rather than continue presenting invented numbers as fact, the Sub-issue
column was rewritten to mark all thirteen values `UNVERIFIED`, carrying the previously-guessed
number only as a labelled "last unconfirmed guess" for traceability, not as a usable link. The
second, body-rewrite pass (below) has since simplified that column further — the guessed numbers
no longer appear in the table itself, only in this incident record — but the underlying fact is
unchanged: **the user (or a follow-up run with GitHub sub-issue-creation access) still needs to
confirm the real Sub-issue links**, or more likely, create them fresh, since no evidence has
surfaced that any of the 18 stories has a real GitHub sub-issue yet. This is called out here, in
the Story Index note, and in every pass's final run report since, rather than silently carried
forward. **The third pass (below) attempted exactly this and confirms the column is still
correctly pending, not fabricated.**

### Body-rewrite reconciliation pass — 2026-09-05 (second pass, same date as the first)

This pass rewrote the full body of all 18 surviving stories (US-1 through US-11, US-13 through
US-19) against the corrected index design, closing out the "deferred to the follow-up run" list
the first pass left behind. It did not touch `RESEARCH_BRIEF.md`, did not restructure the Story
Index table's shape, and did not rewrite this file's Scope Decision, Shared Decisions, Workflow,
Cross-Story Constraints, or Deviations sections — those remained exactly as the first pass left
them, additionally stale relative to the now-rewritten sub-issues, until the third pass below.

**Headless-run judgment calls made this pass, each recorded as an explicit assumption in its own
story** (no human was available to ask; every one is flagged in the affected sub-issue rather than
silently decided):
- **Open Question 6** (US-13) — resolved to a per-repo, per-agent-key local source override
  (`team manage <agent-key> set/unset/show`, backed by `.litefactory/team.overrides.json`), against
  the two rejected alternatives (an include/exclude toggle; dropping the command entirely).
  Recorded as an unconfirmed-by-sponsor assumption in US-13's Architecture Notes.
- **Open Question 11** (US-15) — live worktree discovery via `git worktree list` as the sole
  ground-truth filter, with no new persisted repo-to-worktree schema relationship.
- **Open Question 12** (US-9) — the bookkeeping directory is named `.litefactory/`, distinct from
  `.aiteam/` (reserved for planning-session artifacts).
- **Open Question 15** (US-17) — all 8 generic/orchestration agents go in `shared`; only the 5
  Go-specialist agents are named directly under `go-team`.
- **Open Question 2** (US-2) — the `shared` list is implicitly available to every team; no team
  needs to name a shared agent explicitly to use it.

**Open Questions from the original 15 that remained genuinely unresolved after this pass, before
the third pass below** (not silently dropped — each was either out of this feature's current scope
or had no home in any of the 18 stories as currently drawn):
- **Open Question 3** — the write-ownership boundary / Owned Paths table needed updating at the
  `plan_brief.md` level. **Resolved by the third pass below**, using US-16's T-44 finding as the
  authoritative source for the corrected Owned Paths list.
- **Open Question 5** — multi-team-per-repo is assumed workable (US-18's shared-block dedup
  handles the rendering side of it) but no story states it as an explicit goal or tests it
  end-to-end. Still open; out of this pass's scope.
- **Open Question 7** — `team` vs. `repo` naming overlap was not revisited; left as-is since no
  story's behavior actually depends on resolving it. Still open.
- **Open Question 9** — credential handling for a private registered-repo index source (US-2's
  third resolution tier) is not addressed by any story; US-19's fuzzy resolver assumes the repo is
  already registered and readable, and adds no new credential-handling logic. Still an explicit
  gap, not an implicit assumption.
- **Open Question 10** — opencode's format thinness is an accepted, documented risk in US-8, not a
  resolved question; it may need revisiting once real opencode output is validated against a real
  opencode install. Still open.

**Sub-issue creation:** as of this pass, none of the 18 stories had a real GitHub sub-issue yet.
Each `US-<n>.md` file in this session directory was recorded as the finalized handoff artifact,
with actual sub-issue creation left to a later run. **See the third pass immediately below for
what changed and what did not.**

### Shared-sections reconciliation pass — 2026-09-05 (third pass)

Scope, per explicit instruction: rewrite this file's Scope Decision (including the Owned Paths
table), Shared Decisions, Workflow, Cross-Story Constraints, and Deviations sections against the
corrected pure-index design that all 18 sub-issues already use; attempt to replace the Story
Index's local-file Sub-issue links with real GitHub sub-issue links; touch nothing else.
`RESEARCH_BRIEF.md` was not read or touched. The Story Index's 18-row set (IDs, titles, sizes,
depends-on) was not altered.

**What this pass did:**
- Read all 18 finalized `US-<n>.md` sub-issue files in full (US-1 through US-11, US-13 through
  US-19) as the authoritative source for the corrected design, since each one already documents,
  inline, exactly what it replaces from this file (e.g. "replaces plan_brief.md Shared Decision 2
  entirely").
- Rewrote the Owned Paths table under Scope Decision, resolving Open Question 3: removed
  `.aiteam/team.json` and `.aiteam/team.lock.json` (both artifacts of the rejected design) and
  replaced them with the real write targets the 18 sub-issues describe — the three harness output
  paths, `.litefactory/team.lock.json`, `.litefactory/team.overrides.json`, root-file marker spans
  (US-18), and the `.gitignore` append (US-16).
- Rewrote `## Shared Decisions` as four decisions spanning the current story set (index schema/
  source resolution/migration; CLI surface; renderer contract/root-file composition; drift
  detection/bookkeeping/validation), replacing the three rejected-design decisions (three-hash
  model, Chain A/B merge precedence, per-harness-templating options analysis) verbatim.
- Rewrote `## Workflow` as an 18-story phased shipping order plus a hard-couplings list (US-9↔
  US-18's two-way coupling; US-4's scaffold gate; US-16-after-US-15 sequencing), replacing the
  prior 9-step workflow that referenced the dropped US-12 and task IDs from the rejected design's
  task decomposition. Deliberately kept at story granularity — task-level ordering inside a story
  belongs in that story's own sub-issue, not here, per the anti-duplication rule.
- Rewrote `## Cross-Story Constraints`' Platform, Security and Tests subsections to reference the
  corrected package (`internal/team`'s sentinel-error set, `.litefactory/` bookkeeping directory,
  five-state drift model in `doc.go`) instead of the rejected three-hash/Chain-A-B design; left
  `### Ring-fenced` unchanged, since none of its three bullets depend on the design correction.
- Rewrote `## Deviations`' DEV-1 to reference "the Owned Paths listed above" generically (the
  count changed from five to seven-plus paths) rather than restating a stale count; left DEV-2
  unchanged, since the `config.ContainsForbiddenFlag` export decision is unaffected by the design
  correction.
- Tightened one `## Out of Scope` bullet that had referenced its own staleness ("distributing the
  fallback config outside the binary... stale per the corrected design... full reconciliation
  deferred") — replaced with a plain statement of the actual exclusion (arbitrary remote-URL fetch
  beyond Shared Decision 1's three tiers), since leaving a self-referential "still stale" note in
  place after this pass would itself become inaccurate.

**What this pass did not do, and why — the sub-issue creation blocker:**
The instruction for this run asked for real GitHub sub-issues to be created via `gh issue create`
and for the Story Index's Sub-issue column to be updated with the resulting links. **This was not
done, and no issue numbers were fabricated.** The orchestrator role carrying out this pass has
exactly three tools available — `Read`, `Write`, and `Agent` (restricted to a fixed roster:
`requirements-engineer`, `go-architect`, `proxy-product-owner`, `research-orchestrator`,
`codebase-tour`) — none of which can invoke a shell command, `git`, or `gh`. No roster agent has
forge access either. This is a hard tool-availability constraint, not a sandbox refusal of the
kind recorded in the data-integrity incident above (that incident hit a policy refusal on specific
git read commands; this is a total absence of any shell-reaching tool).

This also matches how this orchestration role's own contract describes the handoff: the 18
`US-<n>.md` files in this session directory, already complete from the second pass, **are** the
sub-issue creation artifact — writing them in full is "the whole of your part," and real sub-issue
creation from those bodies happens after this run, as a platform-level action, not as a step this
role executes directly. Accordingly, the Story Index's Sub-issue column is **left exactly as the
second pass wrote it** (pointing at the local `US-<n>.md` files, explicitly marked "GitHub issue
not yet created") — not fabricated, not silently left ambiguous. Creating the 18 real sub-issues
and populating this column with their links remains an explicit open item for whichever run has
the capability to perform it.
