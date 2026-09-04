# Architecture Review — Agent Teams Configurability

# Architect: Go Architect
# Created: 2026-09-04
# Input: `feature-spec.md` (APPROVED, 2026-09-04)
# Verdict: **PROCEED WITH CORRECTIONS** — the shape is right; five corrections below are load-bearing and must land in the plan before Implement.

---

## System Overview

### How This Feature Fits Into the Existing System

litefactory today is a daemon that *reads* registered clones and *executes* agents inside
worktrees. This feature adds the first capability that **writes into a connected repo**, and
it does so from the CLI rather than from the daemon's run loop. That placement matters: the
whole feature is synchronous, operator-initiated, and never touches the store, the daemon,
the turn loop, or `internal/harness`. It is a new leaf.

The new package `internal/team` sits beside the existing domain packages and is consumed by
exactly one caller: a new `internal/cli/cmd_team.go`. Its own dependency budget is deliberately
tiny — stdlib, plus `internal/config` for two things it must not reimplement
(`config.ClassifySecret` for CFG-4 screening, and a newly exported forbidden-flag predicate for
SEC-1 screening). It must **not** import `internal/cli` (that inverts the sanctioned
`cli -> domain` direction stated in `internal/cli/errors.go`), and it has no business importing
`internal/store`, `internal/git`, `internal/repos`, `internal/daemon`, `internal/turn` or
`internal/harness`.

The data flow is a pure pipeline with filesystem I/O pinned to the two ends:

```
embedded team.json (//go:embed)  ─┐
org team.json (--team-config)    ─┼─► resolve/merge ─► validate ─► ResolvedAgent
repo .aiteam/team.json           ─┘                                     │
                                                                        ▼
                              lockfile ◄── hash ◄── [] RenderedFile ◄── renderers[harness]
                                  │                       │
                                  └──── compare ──────────┴──► plan ──► atomic writes
```

Every step between "read the three layers" and "we have a list of bytes to write" is a pure
function. That is what makes the feature testable without a repo, and it is the single most
important structural property to preserve.

### New Packages / Components Introduced

| Component | Responsibility |
|---|---|
| `internal/team` (new package) | Everything below. One package, several files; do not split into sub-packages yet. |
| `team/embedded.go` + `team/assets/` | The shipped fallback `team.json` and prompt bodies, via `//go:embed`. |
| `team/config.go` | `TeamConfig`/`AgentDefinition` types, the two-phase loader, the dotted-path unknown-key walker. |
| `team/merge.go` | Three-layer resolution into `ResolvedAgent`. Pure. |
| `team/migrate.go` | Hand-written sequential `schemaVersion` migrations, mirroring `internal/store/migrate.go`'s shape. |
| `team/render.go` | The renderer map and the `Renderer` interface (declared here, at the point of consumption — TEST-5). |
| `team/render_claude.go` / `_codex.go` / `_opencode.go` | One concrete renderer each. Pure: `ResolvedAgent -> []RenderedFile`. |
| `team/lock.go` | Lockfile types, load/validate/marshal, path-safety checks. |
| `team/drift.go` | Classification of on-disk state against the lockfile. Pure given a filesystem read. |
| `team/install.go` | The only file that writes. Plans, then applies atomically. |
| `internal/cli/cmd_team.go` | Six `Command` constructors + error translation to `UsageError`/`PreconditionError`. |
| `internal/config` (amended) | New `team_config` key; new exported `ContainsForbiddenFlag`. |

### Key Boundaries

- **cli → team.** Handlers pass primitives and options structs in, get values and typed errors
  out. `internal/team` exports its own sentinels (`ErrNoLockfile`, `ErrLockfileCorrupt`,
  `ErrDrifted`, `ErrUnknownTeam`, `ErrUnknownParam`, `ErrInexpressible`, `ErrForeignFile`);
  `cmd_team.go` maps them onto exit codes. Nothing in `internal/team` may reference
  `PreconditionError`.
- **team → config.** One-way, and only for the two screening functions. `internal/config`
  imports nothing from litefactory, so there is no cycle risk in either direction.
- **team → filesystem.** Confined to `install.go` (writes) and thin read helpers. Renderers
  never touch disk; they return bytes. This is the boundary that makes the "all-or-nothing"
  guarantee mechanically achievable rather than aspirational.
- **The renderer map is the harness extension seam.** Adding a harness is one map entry plus
  one file. This satisfies the spec's AC that a fake harness can be exercised without changing
  any other package — but only if the caller never switches on harness name anywhere else.
  Audit for that in review; it is the easiest AC in the spec to violate accidentally.

### Data Flow

1. `cmd_team.go` resolves `--repo` to an absolute path and asserts it is a git worktree root.
2. `team.Resolve(opts)` reads the embedded layer, then the org layer (if located), then
   `.aiteam/team.json`, migrating each to the current `schemaVersion` in memory, merging
   per-field, and validating the result.
3. For each selected agent × selected harness, the renderer produces `[]RenderedFile`.
4. The caller hashes each file's bytes (`renderedHash`) and each agent's canonicalised source
   (`sourceHash`, `baseSourceHash`) — see Decision 1.
5. `drift.go` compares against the existing lockfile plus on-disk bytes and produces a plan.
6. `install.go` executes the plan: temp file + `rename` per artifact, then the lockfile last.
7. Nothing is published, queued, or persisted to SQLite. There are no domain events.

### Concurrency & Context Notes

There is **no concurrency in this feature and none should be introduced.** The work is a
handful of small files; a worker pool would add goroutine-lifetime obligations for no
measurable gain, and it would make byte-determinism harder to reason about.

`context.Context` still threads through the handler per convention, and the honest
cancellation contract is: **check `ctx.Err()` once, immediately before the write loop begins,
and never inside it.** Once the first `rename` lands, the command finishes the loop. Checking
inside the loop would manufacture exactly the partial-install state the all-or-nothing rule
exists to prevent. State that reasoning in a comment at the check site — otherwise a future
reviewer will "fix" it.

Renderers take no `ctx` at all. They are pure CPU functions; a `ctx` parameter on them would
be decoration. STYLE-2's ban on `context.Background()` outside `main` is trivially satisfied.

### Shared State and Side Effects

- **Writes inside a connected repo** — see DEV-1. Paths: `.aiteam/team.json` (on `team manage`
  only), `.aiteam/team.lock.json`, `.claude/agents/`, `.codex/skills/`, `.opencode/agents/`.
- **`$LF_HOME/config.yaml` gains a key** (`team_config`). Read-only from this feature's side;
  `litefactory init` will render it from the template.
- **No database writes.** `AiteamTeam` and `repos.DetectAiteam` stay untouched, per spec.
- **No daemon interaction.** A running daemon is unaffected by any `team` command, and a
  `team` command must not require the daemon to be running.

---

## Decision 1 — Update & Drift Detection (FIRM)

### Does the two-hash model work?

**Partly. Two hashes answer two questions, but the spec asks three questions of them.** As
written, `sourceHash` conflates three different causes of change:

1. litefactory shipped a new embedded definition (genuine *upstream* change),
2. the org config changed,
3. the operator edited `.aiteam/team.json`, by hand or via `team manage`.

The spec's own acceptance criterion — "changing an agent's definition in the repo-local layer
makes `team status` report that agent's artifacts `upstream-changed`" — labels the operator's
own edit as an upstream change. That is not a wording nit: US-12 explicitly wants to report
*which upstream definitions changed since the installed `contentVersion`*, and a single
`sourceHash` cannot answer that.

### CORRECTION 1 — three hashes, three independent questions

Add a third per-artifact field. Each hash answers exactly one question and nothing else:

| Field | Compared against | Answers |
|---|---|---|
| `renderedHash` | sha256 of the file's current bytes on disk | *Did a human edit the generated file?* |
| `sourceHash` | sha256 of the **fully resolved** agent (all three layers) + prompt body | *Would `team update` change this file?* |
| `baseSourceHash` | sha256 of the **embedded-layer-only** resolution of that agent + prompt body | *Did litefactory's shipped definition change?* |

`baseSourceHash` is cheap: it is a pure function of the binary's embedded assets, recomputable
at any time, and it isolates the upstream axis exactly. Rename the orthogonal flag accordingly:

- `source-changed` — `sourceHash` differs. The artifact is out of date for *some* reason.
- `upstream-changed` — `baseSourceHash` differs. litefactory itself changed this agent.
  Always implies `source-changed` unless a higher layer overrides the changed field.

Edge case to spell out: an agent that exists only in the org or repo-local layer has **no**
embedded definition. Store `baseSourceHash: ""` for it. A transition from `""` to a real hash
is itself reportable and useful ("litefactory now ships an agent you had defined locally") and
must not be treated as corruption.

### CORRECTION 2 — version the hash scheme

Add a top-level `hashVersion` integer to the lockfile. The hash input must be a canonical byte
sequence prefixed with that version, not `encoding/json` over a struct.

Why this is not over-engineering: `sourceHash` is computed over a Go struct. The first time
anyone adds a field to `AgentDefinition`, every stored `sourceHash` in every repo becomes
stale, and every user's next `team status` reports their entire roster as changed. That is a
mass false-positive on a routine refactor.

The rule: **when the binary's `hashVersion` is greater than the lockfile's, source comparison
is `unknown`, not `changed`.** `renderedHash` comparison still works (it hashes literal file
bytes and is scheme-independent), so local-edit detection degrades not at all. `team status`
reports `hash-scheme-changed` and recommends `team update`, which rewrites the lockfile at the
new scheme. Graceful degradation, one int field, one branch.

### CORRECTION 3 — `--prune` must not delete files litefactory did not write

The spec classifies any file at an owned path with no lockfile entry as `orphan`, and
`--prune` deletes orphans. A hand-written `.claude/agents/my-own-helper.md` is an orphan by
that definition. **This is a data-loss hazard and the sharpest risk in the feature.**

Split the class:

- `orphan` — no lockfile entry, **but** the file carries the litefactory provenance marker as
  its first body line. `--prune` may delete it.
- `foreign` — no lockfile entry and no marker. Reported, **never deleted by any flag, ever.**
  `team add --adopt` may overwrite one (that is what "adopt" explicitly means), and that is the
  only path by which litefactory ever touches a foreign file.

This resolves the tension in the spec's "the marker is advisory, the lockfile is authoritative"
honestly, rather than leaving it ambiguous: the marker is advisory **for drift** and
authoritative **for deletion safety**. One decision, one job.

### The lockfile format

Sound, with the three corrections folded in. Invariants that must be **validated on load**, not
assumed — a lockfile is a file a human can edit:

- `artifacts` sorted by `path`, no duplicate paths, `harnesses` sorted.
- Every hash is 64 lowercase hex characters, or `""` for `baseSourceHash`.
- Every `path` is repo-relative, forward-slash, has no `..` segment, is not absolute, and
  resolves inside one of the three owned directories. A lockfile is a write instruction list;
  an unvalidated path in it is an arbitrary-file-write primitive.
- Lockfile `schemaVersion` greater than the binary's ⇒ `ErrLockfileCorrupt`-class refusal
  ("this lockfile was written by a newer litefactory"), never a silent downgrade.
- Lockfile `schemaVersion` **less** than the binary's — the spec does not cover this. Decision:
  migrate in memory with the same sequential step pattern as the config; persist the migrated
  form only as part of a successful `team update`. `team status` never writes.

### The decision table (firm)

| On-disk state | `source-changed`? | `team status` | `team update` | `team update --force` |
|---|---|---|---|---|
| bytes match `renderedHash` | no | `clean` | no-op | no-op |
| bytes match `renderedHash` | yes | `stale` | rewrite | rewrite |
| bytes differ | no | `modified` | **skip**, report | overwrite |
| bytes differ | yes | **`conflict`** | **skip**, report | overwrite, report as *conflict* |
| file absent | no | `missing` | recreate | recreate |
| file absent | yes | `missing` | recreate at new content | same |
| no entry, marker present | — | `orphan` | report | `--prune` deletes |
| no entry, no marker | — | `foreign` | report | never deleted |
| hash scheme newer | unknown | `hash-scheme-changed` | rewrite lockfile | same |

The `conflict` row is the edge case the sponsor asked about, and it earns its own class: with
`--force` the operator is losing an edit *and* taking new content simultaneously. The output
must name it distinctly from a plain `modified` overwrite — same action, materially different
consequence. **Never merge.** No three-way merge, no hunk application, not now and not later.

`team status` exits 0 when every artifact is `clean`, and 1 otherwise (`foreign` included —
it is a real finding an operator should see).

---

## Decision 2 — CLI Surface (FIRM)

Verified against `internal/cli/parse.go`: `splitPositionals` peels leading positionals *before*
`fs.Parse`, then re-appends `fs.Args()`. So both `team manage arch set model=opus --repo /x`
and `team manage --repo /x arch set model=opus` parse identically. The router's longest-name-
prefix match means `team manage` is **one** `Command` with the verb in a positional slot — no
sub-router, no switch in the dispatch table.

### Commands

| Name | Usage | `ArgSpec{Names, Min}` |
|---|---|---|
| `team list` | `team list [--repo <path>] [--json]` | `{}, 0` |
| `team show` | `team show <team> [--repo <path>] [--json]` | `{["team"], 1}` |
| `team add` | `team add <team> [--repo <path>] [--harness <list>] [--dry-run] [--adopt]` | `{["team"], 1}` |
| `team status` | `team status [--repo <path>] [--json]` | `{}, 0` |
| `team update` | `team update [--repo <path>] [--dry-run] [--force] [--prune] [--migrate-config]` | `{}, 0` |
| `team manage` | `team manage <agent> <action> [<assignment>]` | `{["agent","action","assignment"], 2}` |

`--team-config <path>` binds on **all six**. A bare `litefactory team` prints the group usage
for free via `Router.unresolved` — no work required.

`team manage` arity beyond `Min` is checked inside the handler (`set` and `unset` require the
third positional; `get` accepts 2 or 3), returning `Usagef(cmd, ...)`. This is the one place
`ArgSpec` cannot express the rule, and that is fine — it is a two-line guard.

### `--repo` resolution (firm)

Default `.`; resolved to an absolute path; **require a `.git` entry at exactly that path. Do
not walk up.** Rationale: this is a *write* command that installs files. Walk-up UX is
conventional for read commands, but here it means running from a subdirectory of the wrong
repo silently writes into it. On failure, the error names the path that was checked and
suggests `--repo`. This is a `PreconditionError` → exit 3.

### Two precedence chains — do not conflate them

The spec blurs these. They are different.

**Chain A — where the org layer comes from** (first hit wins, no merging):

1. `--team-config <path>` flag
2. `team_config:` in `$LF_HOME/config.yaml`
3. absent → no org layer

**Chain B — how content composes** (later layers override earlier, per-field):

1. embedded fallback (`//go:embed`) — always present, always the base
2. org layer (from Chain A, if any)
3. repo-local `.aiteam/team.json` (if present)

Merge semantics, confirming the spec: per-field within an agent; lists (`tools`,
`disallowedTools`) replaced wholesale, never appended; a higher layer may add an agent but
never delete one — it sets `"disabled": true` instead. That last rule is what keeps the
operation associative and the lockfile explicable.

### `team_config` must be an absolute path

CFG-8 forbids anything outside `internal/config` joining a path against `$LF_HOME`. A relative
`team_config` would demand either such a join or a new `config.Paths` method, and would carry
an unanswerable "relative to what?" question. **Decision: reject a relative `team_config` in
`config.validate` with a clear message.** CFG-8 is then untouched and no `Paths` method is
needed.

A `--team-config` or `team_config` pointing at a nonexistent file is an **error** (exit 3), not
a silent fallback to embedded-only. An operator who configured an org file and typo'd the path
must not quietly get a different team.

### Exit codes

| Condition | Error kind | Code |
|---|---|---|
| unknown team, unknown param, bad `--harness` value, bad arity | `UsageError` | 2 |
| not a git worktree, repo not writable, `team_config` missing/relative | `PreconditionError` | 3 |
| drift found by `status`; blocked `modified`/`conflict` on `update`; corrupt lockfile; inexpressible field | plain error | 1 |

`team status` prints its full report to `env.Stdout` and *then* returns a plain sentinel so the
router maps it to 1. The sentinel's message is printed to stderr as `litefactory: <msg>`, so it
must read as a sentence — e.g. `3 artifacts have drifted; run 'litefactory team update'`.

### `--harness` default

Confirming the spec: the harnesses already installed in the repo (detected by the presence of
`.claude/`, `.codex/`, `.opencode/`), falling back to `claude,codex` when none are found. The
detection result must be printed in the `team add` output — an implicit selection that the
operator cannot see is a support burden.

---

## Decision 3 — Per-Harness Templating (ONE RECOMMENDATION)

**Recommendation: keep the pure-function renderer registered in a map, keyed by harness. Use
`text/template` *inside* a renderer as an implementation detail where it helps, never as the
extension mechanism.**

### Why not per-harness template files as the seam

A template file cannot fail loud. The spec requires that a field a harness cannot express is a
hard error rather than a silent drop — and `text/template`'s default behaviour for a missing or
unmappable field is to render `<no value>` and carry on. You would be building the exact
failure mode the requirement exists to prevent, and then bolting a validation pass in front of
the template to catch it — at which point the validation pass is the real renderer and the
template is decoration. Template files also move the mapping logic out of reach of the
compiler: renaming a field in `AgentDefinition` breaks a string in a file rather than the
build.

### Why not a common intermediate schema with adapters

It adds a third representation between `ResolvedAgent` and the output bytes, and pays for it
with a mapping that must be maintained twice. The payoff would be sharing between harnesses —
but these three formats share almost nothing: YAML frontmatter + Markdown body, a skill
directory with its own file layout, and an agent file with a permission object. The
intermediate schema would end up as the union of all three with per-harness escape hatches,
which is `ResolvedAgent` again with an extra hop.

### The recommended contract, with one correction

**CORRECTION 4 — do not reuse `Artifact` as the renderer's return type.** The spec's
`Render(agent) ([]Artifact, error)` returns the *lockfile record* type, which carries
`renderedHash` and `sourceHash`. A renderer cannot compute those (it does not know the source
resolution and should not know the hash scheme), so it would return them zeroed — and a zeroed
hash reaching a lockfile is precisely the bug that makes drift detection lie. Two types:

- `RenderedFile{ Path string; Bytes []byte }` — what a renderer returns.
- `Artifact{ Path, Agent, Harness, RenderedHash, SourceHash, BaseSourceHash }` — what the
  caller builds after hashing, and what the lockfile stores.

The `Renderer` interface is declared in `team/render.go`, at the point of consumption (TEST-5),
never next to a concrete renderer.

### Determinism obligations (TEST-7)

- Sort before iterating **any** map — `slices.Sorted(maps.Keys(...))` — following the precedent
  and the comment in `internal/config/policy.go`'s `sortedProfileNames`.
- Never range a map inside a template. Pass a pre-sorted slice of pairs.
- Explicit `\n` endings; exactly one trailing newline; no timestamp, no version string, no
  hostname in rendered output. Anything varying per-run makes `renderedHash` meaningless and
  turns every `team status` into a false `modified`.
- Two tests per harness: a golden-file test, and a render-twice-compare-bytes test. The second
  is what actually catches map-order regressions; the golden file alone will pass by luck.

### Tradeoffs, stated honestly

Adding a harness needs a Go build and a release. That is the real cost, and it is acceptable:
harness file formats change on the harness vendor's release schedule, not the operator's, and
litefactory ships as a binary anyway. If a future requirement is "an operator adds a harness
without a litefactory release," that is a different feature and this decision should be
revisited then — not pre-paid for now.

---

## Library and Package-Boundary Confirmations

### "No third-party JSON-Schema, migration, or templating library" — **CONFIRMED**

`go.mod` stays at four direct requires (go-cmp, go-github, yaml.v3, modernc.org/sqlite), so
TEST-2 holds. Migrations follow the hand-written sequential pattern already established in
`internal/store/migrate.go`, where a migration library is an explicitly rejected dependency.
Templating is `text/template`, stdlib.

**CORRECTION 5 — but the schema validation is not as cheap as the spec implies.** The spec's
AC requires "an unknown key at any nesting depth produces a load error naming its dotted path."
`json.Decoder.DisallowUnknownFields()` does **not** deliver that:

- it does not reject unknown keys inside a `map[string]X` — a map accepts any key by
  definition. That covers `agents`, `harnesses` and `permission`, so the `harnesses` map's
  restriction to `claude|codex|opencode` must be validated explicitly and cannot ride on
  `DisallowUnknownFields`;
- its error text is `json: unknown field "x"` with **no path context**, so the dotted path
  cannot be recovered from it.

Delivering that AC requires a **two-phase decode**: decode into `map[string]any`, walk it
against a known-key table accumulating a dotted path, aggregate the findings with
`errors.Join` (matching CFG-5's precedent), then decode into the typed structs. Still no
library — but this is an **L**, not an **S**, and it is the single biggest sizing risk in the
feature. Size it honestly in the plan or it will eat a story.

### "`internal/team` leaves `purity_test.go` untouched" — **CONFIRMED, with one caveat**

`TestValidationHasNoThirdPartyImports` scans only `internal/config`'s own directory and asserts
that every file except `config.go` is stdlib-only. `internal/team` is outside its scope
entirely. The only changes this feature makes to `internal/config` are a stdlib `string` field
and one exported predicate — neither adds an import, so the test stays green. There is no
`policy_test.go` surface pin in that package, so adding an exported function trips nothing.

**Caveat / CORRECTION 6:** the spec says `team manage` should apply "a `forbiddenFlag()`-
equivalent check." `config.forbiddenFlag()` is **unexported**, as are `validateTools` and
`validateAllowedTools`. Writing an "equivalent" in `internal/team` would assemble
`--dangerously-skip-permissions` from fragments in a *second* place — which defeats the stated
purpose of SEC-1's fragment trick (that a single repo-wide grep finds every site). Decision:
**export one narrow predicate from `internal/config`**, `func ContainsForbiddenFlag(value
string) bool`, implemented in `policy.go` beside `forbiddenFlag()`, with `validateNoForbiddenFlag`
refactored to call it. One definition, one place, stdlib-only, purity untouched.

`config.ClassifySecret` is **already exported** with a doc comment that anticipates exactly this
reuse ("so `init` can check a value a user is about to have written for them, with the same
patterns startup uses"). `team manage` calls it directly. No new code, no duplication, no cycle.

### Not a trap: the `AC-PAR-` prefix test

`TestIntroducedCriteriaCarryTheParPrefix` in `internal/daemon/record_test.go` is hard-scoped by
`const sessionDir = "main/2026-08-30/parallelization-lf"`. It does not scan this session, so
this spec's unprefixed acceptance criteria are compliant as written. (It *will* fail hard if
anyone moves or deletes those two parallelization-lf documents — unrelated to this feature, but
worth knowing.)

### `.aiteam/APP_CONTEXT.md` is stale

It documents 17 packages; `internal/` now also contains `agent`, `attach`, `codex` and
`planbrief`. Not blocking, but `app-context-updater` should be run after this lands, and this
feature adds an 18th+ package.

---

## Platform Lens

- **Platform: single Go module, everything under `internal/`, `cmd/` is wiring only** — `internal/team`
  is a new sibling package, not a sub-package of `config`; `cmd/litefactory/main.go` needs no change
  at all, because commands self-register through `Commands()`.
- **Platform: `internal/config` is the only package everyone may depend on** — it is the correct
  home for the two screening predicates, and the correct place for `team_config`. Nothing else
  in the tree should learn what a team is.
- **Platform: dependency direction is `cli -> domain`** — `internal/team` exports sentinels;
  `cmd_team.go` translates. `PreconditionError` never appears in the domain package.
- **Platform: adding a command is one constructor plus one line in `Commands()`** — six
  constructors in `cmd_team.go`, six lines. No switch, no registry mutation; the spec's AC on
  this is satisfiable exactly as written.
- **Platform: `parseArgs` handles interspersed flags and positionals** — `team manage <agent>
  <action> <assignment>` works with flags in any position, so no custom argument handling is
  needed anywhere.
- **Platform: errors are wrapped with `%w` and unwrap to sentinels (STYLE-5)** — every
  `internal/team` failure mode gets a typed error or sentinel; `errorlint` is on and will
  reject `==` comparisons.
- **Platform: `errors.Join` is the aggregation precedent (CFG-5)** — validation must report
  *all* problems in one pass, not the first. An operator fixing a 20-agent team one error per
  run is a bad experience the codebase already knows how to avoid.
- **Platform: `fmt.Print*` and `log.*` are forbidden (STYLE-2)** — all command output goes to
  `env.Stdout`, the one sanctioned exception; diagnostics go through `env.Logger`.
- **Platform: `context.Background()` outside `main` is forbidden (STYLE-2)** — renderers take no
  ctx; handlers use the one passed in.
- **Platform: `make test` runs with `-race`** — trivially satisfied given there is no
  concurrency, and that is a reason to keep it that way.
- **Platform: `gosec` is on** — file writes need explicit, justified permission bits.
  `0o666`-with-umask is the established pattern (`Paths.EnsureHome` documents why), and any
  `//nolint:gosec` needs the same style of one-line justification.
- **Platform: `internal/gittest` has `AssertUnchanged` and `Capture`+`Snapshot.Diff`** — this is
  the ready-made tool for proving that `--dry-run` writes nothing and that a failed `team add`
  leaves no partial install. Use it; do not hand-roll directory walks.
- **Platform: eleven packages carry a `doc.go` that is the real spec** — `internal/team` must
  ship one, and it is the right place to record the three-hash model and the drift table so the
  next reader does not have to reconstruct them from this review.
- **Platform: a live daemon runs from `dist/litefactory`** — do not `make build`, do not write
  into `dist/`, do not restart the daemon while implementing.

---

## Security Lens (forward-looking: risks this plan carries)

### Security Findings

| Area | Finding | Status |
|---|---|---|
| Input validation | `promptPath` must be repo-relative with no `..` and no absolute form — spec says so; must be enforced at *load*, and again after any `filepath.Join`, since a validated-then-joined path is where traversal bugs actually live. | ⚠️ concern |
| Input validation | `artifacts[].path` in a **hand-edited lockfile** is an unvalidated write instruction. Without the path checks in Decision 1, a crafted lockfile turns `team update` into arbitrary file write. Highest-severity item in the feature. | ❌ violation if unaddressed |
| Input validation | **Symlinks.** If `.claude/agents/x.md` is a symlink to `~/.ssh/authorized_keys`, a naive write follows it. `os.Lstat` before every write; refuse anything that is not a regular file or absent. Applies to the temp+rename path too. | ❌ violation if unaddressed |
| Input validation | Two-phase decode with dotted paths (Correction 5) is also the input-validation control; a partially-validated config is a partially-trusted one. | ⚠️ concern |
| Input validation | `slug` regex `^[a-z0-9][a-z0-9-]{0,63}$` is sound and is what keeps a team name out of a path. Agent names used as filenames need the *same* treatment — the spec does not state a pattern for agent map keys. **Gap: specify it.** | ⚠️ concern |
| Authz | Not applicable in the usual sense; the trust boundary is the local operator. The real authz question is *which directories litefactory may write to*, and it is answered by the owned-path allowlist. Enforce the allowlist at the write site, not only at plan time. | ✅ compliant |
| Authz | `team manage` writes agent **permission** and **sandboxMode** values — i.e. it edits the security posture of agents that will later run. The closed allowlist with no raw pass-through is the right control and must not be relaxed to "any key" for convenience. | ✅ compliant |
| Secrets | `config.ClassifySecret` reused directly for `team manage` values. Matched text must **not** appear in the error (CFG-4). | ✅ compliant |
| Secrets | The `team_config` string is automatically covered by `walkStrings` in the existing startup scan — free, no work, but worth knowing it is not a gap. | ✅ compliant |
| Secrets | Prompt bodies from an **org-supplied** team.json are written into the repo verbatim. They are not scanned. Accepting an org file is a trust decision the operator makes explicitly via `--team-config`; document it in the command's `Long` rather than pretending to validate it. | ⚠️ concern |
| Secrets | SEC-1: `--dangerously-skip-permissions` must be unconstructible. Correction 6 (export one predicate) keeps that to one site; a second fragment-assembly would silently weaken the grep guarantee. | ⚠️ concern |
| Data handling | `--prune` deleting unmarked files is data loss (Correction 3). `foreign` files are never deleted. | ❌ violation if unaddressed |
| Data handling | `--force` overwriting a `conflict` destroys an operator edit. Acceptable with an explicit flag; the output must name what was overwritten, per file. | ⚠️ concern |
| Data handling | Atomic temp+rename per file, lockfile written last. If the process dies mid-loop, the lockfile still describes the previous state and the next `team status` reports the truth. Good property — preserve the ordering deliberately. | ✅ compliant |
| Dependencies | No new module. Four direct requires unchanged. `//go:embed` only, no network fetch of team definitions. | ✅ compliant |
| Error handling | Errors must name the offending *path* and *key*, never the offending *value* (secret leakage). Aggregate with `errors.Join`. | ⚠️ concern |
| Error handling | Corrupt lockfile is a hard error that never rewrites the file. Refusing to act on state you cannot parse is correct. | ✅ compliant |

**Summary:** three ❌-if-unaddressed items (lockfile path validation, symlink following, `--prune`
data loss), all of them cheap to fix and all of them structural rather than incidental — each is
a consequence of this being the first litefactory feature that writes into a directory a human
also writes into. Address them in the design, not in review. No blocking issue otherwise; the
trust model (local operator, explicit org-file opt-in) is coherent.

---

## Task Decomposition

Single-layer team: every task goes to `go-implementation-engineer`, with test tasks split out
per the decomposer's convention.

| ID | Title | Type | Depends On | Story | Est |
|---|---|---|---|---|---|
| T-01 | `internal/team` package skeleton + `doc.go` recording the three-hash model and drift table | infra | — | US-1 | S |
| T-02 | `TeamConfig`/`AgentDefinition`/per-harness stanza types with field-level constraints | data | T-01 | US-1 | M |
| T-03 | Two-phase decode: `map[string]any` walk against a known-key table producing dotted paths, aggregated via `errors.Join` | logic | T-02 | US-1 | L |
| T-04 | Tests: unknown key at depth, inside a map, and multiple simultaneous errors | test | T-03 | US-1 | M |
| T-05 | Embedded fallback assets + `//go:embed` loader | data | T-02 | US-2 | S |
| T-06 | `schemaVersion` sequential in-memory migration framework, mirroring `store/migrate.go` | logic | T-02 | US-3 | M |
| T-07 | Tests: migration chain, unknown-newer version refusal | test | T-06 | US-3 | S |
| T-08 | Three-layer merge into `ResolvedAgent` (per-field, lists wholesale, add-not-delete, `disabled`) | logic | T-05, T-06 | US-4 | M |
| T-09 | Tests: merge precedence matrix, `disabled` semantics, layer-adds-agent | test | T-08 | US-4 | M |
| T-10 | Org-layer location: `--team-config` > `team_config` > absent; absolute-path rule | logic | T-08 | US-4 | S |
| T-11 | `config.TeamConfig` key: `Defaults()` **and** `internal/config/template.go` (CFG-3), absolute-path validation | data | — | US-4 | S |
| T-12 | Export `config.ContainsForbiddenFlag`; refactor `validateNoForbiddenFlag` onto it | logic | — | US-14 | XS |
| T-13 | `Renderer` interface + harness map + `RenderedFile` type | api | T-08 | US-5 | S |
| T-14 | Claude renderer (`.claude/agents/<agent>.md`) with fail-loud on inexpressible fields | logic | T-13 | US-5 | M |
| T-15 | Codex renderer (`.codex/skills/<agent>/SKILL.md`) | logic | T-13 | US-6 | M |
| T-16 | opencode renderer (`.opencode/agents/<agent>.md`) | logic | T-13 | US-7 | M |
| T-17 | Tests: golden file **and** render-twice-byte-equality, per harness; fake harness via map entry only | test | T-14, T-15, T-16 | US-5–7 | L |
| T-18 | Canonical hash input + `hashVersion`; `renderedHash`/`sourceHash`/`baseSourceHash` computation | logic | T-08, T-13 | US-8 | M |
| T-19 | Lockfile types, marshal, and load-time invariant validation incl. path safety | data | T-18 | US-9 | M |
| T-20 | Tests: lockfile round-trip, sortedness, traversal path rejection, newer-schema refusal | test | T-19 | US-9 | M |
| T-21 | Drift classification incl. `conflict`, `foreign`, `hash-scheme-changed` | logic | T-19 | US-10 | L |
| T-22 | Tests: full decision-table coverage, one case per row | test | T-21 | US-10 | L |
| T-23 | Atomic write layer: `os.Lstat` symlink refusal, owned-path allowlist at write site, temp+rename, lockfile last | logic | T-19 | US-11 | M |
| T-24 | Tests: symlink refusal, no partial install on mid-loop failure, `--dry-run` writes nothing (`gittest.AssertUnchanged`) | test | T-23 | US-11 | L |
| T-25 | `cmd_team.go`: six constructors + `Commands()` registration + error→exit-code translation | api | T-13, T-21, T-23 | US-13 | L |
| T-26 | `--repo` resolution (no walk-up) + `--harness` detection with fallback | logic | T-25 | US-13 | M |
| T-27 | `team list` / `team show` incl. `--json` | api | T-25 | US-13 | M |
| T-28 | `team add` incl. `--dry-run`, `--adopt` | api | T-25, T-23 | US-11 | M |
| T-29 | `team status` incl. `--json` and exit-1-on-drift | api | T-25, T-21 | US-10 | M |
| T-30 | `team update` incl. `--force`, `--prune` (marker-gated), `--migrate-config` | api | T-25, T-21, T-23 | US-12 | L |
| T-31 | `team manage` closed allowlist + `ClassifySecret` + `ContainsForbiddenFlag` screening + `.aiteam/team.json` write-back | api | T-25, T-12 | US-14 | L |
| T-32 | Tests: CLI arity, flag precedence, exit codes per condition | test | T-27–T-31 | US-13 | L |
| T-33 | Amend `cmd_repo.go` `Long` help text (the "never writes inside the clone" sentence) | infra | — | DEV-1 | XS |
| T-34 | Write DEV-1 into `.aiteam/AC_AMENDMENTS.md` | infra | — | DEV-1 | XS |

**Ordering.** T-01→T-04 first: the loader is the foundation and the largest single risk. T-11,
T-12, T-33 and T-34 are independent and can land at any time — T-34 should land **first**, since
it records the sanction under which everything else is written. Renderers (T-13–T-17) can
proceed in parallel with the lockfile chain (T-18–T-20) once T-08 exists. The CLI (T-25+) is
last and is mostly assembly.

**Critical path:** T-01 → T-02 → T-03 → T-08 → T-18 → T-19 → T-21 → T-23 → T-25 → T-30.

---

## Deviations

### ⚠️ DEV-1 — SEC-7: litefactory writes inside a registered clone

**Rule:** SEC-7 [enforced] — "litefactory MUST NOT write inside a registered clone."

**Deviation:** This feature writes `.aiteam/team.json`, `.aiteam/team.lock.json`,
`.claude/agents/`, `.codex/skills/` and `.opencode/agents/` inside a connected repo.

**Status:** Settled by the human sponsor. Recorded, not re-litigated.

**Scope, precisely.** SEC-7's *mechanism* is `git.Clone`'s method set, pinned by
`internal/git/surface_test.go` — it forbids git **verbs** (`Commit`, `Checkout`, `Reset`,
`Merge`, `Pull`, …), not filesystem writes into the clone directory. Writing files under the
clone root with `os` never touches `git.Clone`, so `surface_test.go` stays green and must not
be edited. **The implementation MUST NOT add a method to `git.Clone`** — that would break the
surface test and violate TEST-10 as well. The deviation is against SEC-7's *prose*, and the
prose must be amended rather than quietly outgrown.

**Required actions:** T-34 (write into `.aiteam/AC_AMENDMENTS.md`, per the deviating clause) and
T-33 (amend the `repo add` help text at `internal/cli/cmd_repo.go:57-58`, which currently states
"litefactory never installs it and never writes inside the clone"). Shipping this feature
without T-33 means the tool contradicts itself in its own help output.

**Residual risk:** the blast radius is now "any file under an owned directory in any connected
repo." That is exactly why the three ❌ security items above (lockfile path validation, symlink
refusal, `foreign`-file protection) are non-negotiable rather than nice-to-have. SEC-7 was
carrying that safety for free; once it is relaxed, the controls become explicit code.

### ⚠️ DEV-2 (minor, for the record) — `internal/config` gains an exported predicate

`ContainsForbiddenFlag` widens an enforced package's surface. It *serves* SEC-1 rather than
bending it (one definition instead of two), adds no import, and trips no test — there is no
`policy_test.go` surface pin, and `purity_test.go` checks imports only. Flagged so the change to
a security-critical package is a decision on the record rather than a drive-by.

**No other deviation is required by this feature.**

---

## Out of Architectural Scope

Nothing in this feature falls outside Go backend architecture. There is no frontend contract, no
infrastructure topology and no deployment concern; the CLI *is* the interface and the artifacts
are local files. No gap to note.
