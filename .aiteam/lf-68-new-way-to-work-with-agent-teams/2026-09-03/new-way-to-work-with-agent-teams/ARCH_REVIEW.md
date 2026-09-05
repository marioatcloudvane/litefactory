# Architect: Go Architect
# Created: 2026-09-05
# Run type: reconciliation review (supersedes the 2026-09-04 ARCH_REVIEW.md in this directory, which was written against the rejected JSON-as-config design)

---

## 0. Verdict Summary

| Item | Verdict |
|---|---|
| Ownership-boundary decision (OQ3) | **COHERENT — proceed**, with three mandatory artefacts (see §2.1). It is a DEVIATION from `SEC-7` and MUST be written into `.aiteam/AC_AMENDMENTS.md`. |
| Bookkeeping directory (OQ12) | **`.litefactory/` at repo root, one file for v1: `.litefactory/teams.lock.json`.** Sole carve-out alongside OQ3's four generated-file paths. |
| CLI command table (6 commands) | **Confirmed**, table in §2.5. `team remove` inference is **sound and must be kept** — but no story in the proposed 12 owns it. Gap flagged, new US-13 proposed. |
| JSON index worked example | **Structurally sound**; four corrections required (§2.6): `schemaVersion` absent, `dest` semantics ambiguous for the Codex layout, `dest` uniqueness unvalidated, team-name uniqueness unvalidated. |
| 12-story list | **Validated with 5 deviations** (§6). Headline: `feature-spec.md` is APPROVED but STALE and contradicts the corrected brief. |
| `DisallowUnknownFields` claim in US-1 | **Incorrect as stated** — stdlib suffices. Two-phase decode is warranted only for version-first dispatch (§2.3). |

**Headline DEVIATION:** `$session/feature-spec.md` is marked `Status: APPROVED` but encodes the *rejected* design end-to-end (three-layer merged config, `.aiteam/team.json`, `team manage set/get/unset`, an opencode renderer, and `team remove` explicitly in *Out of Scope*). The plan MUST be reconciled against `RESEARCH_BRIEF.md`, and `feature-spec.md` MUST be marked superseded before implement mode reads it as a gate artefact. Leaving an APPROVED spec that contradicts the plan in the session directory is a live trap for `implement-orchestrator`.

---

## Part 1 — System Overview

### How this feature fits into the existing system

litefactory today is a daemon plus a CLI that *observes* registered git clones and runs harnesses inside worktrees. It has never written a byte inside a registered clone (`internal/cli/cmd_repo.go:54-58`, `SEC-7`). This feature is the first that does: it materialises agent-team definitions — markdown bodies pulled from a configurable index — into the harness-specific locations each harness expects (`.claude/agents/`, `.codex/skills/`), plus composed blocks in the repo's root `AGENTS.md` / `CLAUDE.md`.

The feature sits as a **new leaf-ish domain package, `internal/team`**, consumed by `internal/cli`. Its work is: read an index (JSON), resolve a team to an effective member list, render each member per harness, write the results atomically into a target directory, and record what it wrote in a lockfile so a later `status`/`update`/`remove` can tell litefactory's output from a human's edits.

Nothing in the daemon path changes. `internal/turn`, `internal/report`, `internal/github` are untouched. The only existing-package edits are: one narrowing amendment to a doc string in `internal/cli/cmd_repo.go`, one new config field in `internal/config` (mirroring `GitHub.APIBase`), one new exported wrapper in `internal/config` (`ContainsForbiddenFlag`), one resolver added to `internal/repos`, and six lines in `internal/cli/Commands()`.

### New packages / components introduced

| Component | Responsibility |
|---|---|
| `internal/team` | The whole domain: index model + validation, per-harness renderers, root-document composition, lockfile, drift classification, install/update/remove engines. |
| `internal/team/teams/` (embedded) | The `//go:embed`-ed default index snapshot: `index.json` + the go-team and shared markdown bodies. Data, not code. |
| `repos.ResolveIdentifier` | Fuzzy `<repo-identifier>` → `store.Repo` resolution. Lives in `internal/repos` (which already owns `store` + `git`), **not** in `internal/team`. |
| `internal/cli/cmd_team.go` | Six command constructors + flag binding + sentinel→exit-code translation. |
| `config.TeamIndex` | Where the index comes from. Mirrors `GitHub.APIBase` exactly (field + `DefaultTeamIndex` + validation + `template.go` line). |

### Key boundaries

**The single most important boundary call in this review: `internal/team` MUST NOT import `internal/store`, `internal/github`, or `internal/daemon`.**

The `cli` layer resolves `<repo-identifier>` to a filesystem path (via `repos.ResolveIdentifier` → `store.Repo.Path`) and hands `internal/team` a **target directory path** plus, for US-9, a **pre-computed `[]string` of candidate worktree paths**. `internal/team` is then testable against a bare `t.TempDir()` with no store, no fixture daemon, and no git at all in its unit tests. This keeps the dependency graph shallow (platform-constraints: "keep the `internal/` core dependency surface shallow") and keeps the DAG acyclic without effort.

Resulting import edges:

```
cmd/litefactory ──> cli
cli ──> team, repos, config, store, git, …(existing)
repos ──> git, naming, store            (existing; gains ResolveIdentifier)
team ──> config (types only), stdlib    (encoding/json, os, io/fs, crypto/sha256, path/filepath)
```

`internal/team` does **not** import `internal/git`. US-9's `git worktree list` ground-truth call happens in `cli`/`repos` using the existing `git.Clone.WorktreeList` method; the filtered path list is passed down. This also keeps `SEC-7`'s "the `git.Clone` method set *is* the allowlist" chokepoint intact and untouched.

**The write boundary.** `internal/team` writes to exactly five path shapes under the target directory, and refuses anything else:

```
<target>/AGENTS.md                     (marker-delimited spans only)
<target>/CLAUDE.md                     (marker-delimited spans only)
<target>/.claude/agents/<dest>.md      (whole file, litefactory-owned)
<target>/.codex/skills/<dest>/SKILL.md (whole file, litefactory-owned)
<target>/.litefactory/teams.lock.json  (whole file, litefactory-exclusive)
```

This list is the carve-out. It is enumerable, it lives in one place in `internal/team/doc.go`, and it should be pinned by a test in the spirit of `git/surface_test.go` (see §3, `TEST-10`).

### Data flow

```
team add <repo-id> <team>
  cli: parse flags
   -> repos.ResolveIdentifier(ctx, store, "<repo-id>")  -> store.Repo{Path}
   -> team.LoadIndex(ctx, source)                        -> Index  (embedded | fs path | registered repo)
   -> team.Screen(index bodies)                          -> blocking error on secret / forbidden flag
   -> team.Install(ctx, Options{Target, Team, DryRun, Adopt})
        effective members = team.agents ∪ index.shared   (deduped, sorted)
        for each member: renderers[harness].Render(body)  -> []RenderedFile
        compose(AGENTS.md), compose(CLAUDE.md)            -> marker spans
        atomic write all files (temp + rename, Lstat guard)
        write .litefactory/teams.lock.json
   -> cli: fmt.Fprintf(env.Stdout, …) the summary
```

`team status` reads the lockfile plus the on-disk bytes plus the index and classifies. `team update` re-runs the install engine over the classification. `team remove` runs the composition engine in subtractive mode. All four share the same engine; only the planner differs.

### Concurrency & context notes

**There is no concurrency in this feature, and there should not be.** Recommend explicitly:

- US-9 fan-out across worktrees is **sequential**. N is 2–5, each unit is a handful of file writes, and sequential + `errors.Join` gives a deterministic partial-failure report that a goroutine fan-out would only complicate. `CONC-1`/platform-constraints both say a goroutine needs a reason; there isn't one here.
- `ctx` is still the first parameter on every exported `internal/team` function (`CONC-5`), and long walks (`filepath.WalkDir` over the target, index fetch) MUST check `ctx.Err()` — this is what makes Ctrl-C during a multi-worktree update stop rather than finish.
- No `context.Background()` anywhere in the package (`STYLE-2`).

### Shared state and side effects

- **The target repo's working tree becomes dirty.** litefactory writes files; it never stages, commits, or pushes (that half of `SEC-7`'s intent is preserved unchanged). Any operator running `team add` on a clone that the daemon is about to use will see the daemon's dirty-worktree guard (`git.ErrWorktreeUncommittedChanges`) trip. **This interaction MUST be called out in `team add`'s `Long` text** — it is the most likely field surprise in the whole feature.
- `.litefactory/teams.lock.json` is a file a human can edit. Every field is validated on load; a hand-edited lockfile is otherwise an arbitrary-file-write primitive (see §4).
- The embedded index is compiled into the binary. Changing a shipped agent body changes `sourceHash` for every repo on the next `team status`. This is intended, and is exactly what `hashVersion` exists to keep honest.

---

## Part 2 — Lens 1: Engineering Review

### 2.1 Ownership boundary (OQ3) — VERDICT: coherent, proceed

**Is it engineeringly coherent?** Yes, for three reasons:

1. **The write surface stays enumerable.** Five path shapes, listed in one place, every one of them recorded in a litefactory-exclusive lockfile. That is a materially different thing from "litefactory may write in the clone." The rule that replaces `SEC-7`'s prose is not weaker in kind, only narrower in extent.
2. **`SEC-7`'s enforcement mechanism is untouched.** I verified `internal/git/clone.go`: the method set is `Path, Fetch, WorktreeAdd, WorktreeList, WorktreeRemove, RevParse, RemoteURL, ConfigGet, ShowRef` — no `Commit`, `Checkout`, `Reset`, `Merge`, `Pull`. `git/surface_test.go` pins that set. **Writing plain files with `os` is not a `git.Clone` method and would not trip that test.** So the carve-out does not require weakening the enforced test at all; it requires amending the *prose* that the test only partially covers. That is a small, honest, reviewable change.
3. **The dangerous half of `SEC-7` survives verbatim.** "litefactory never stages, never commits, never pushes in a registered clone" remains absolutely true and remains enforced by the same allowlist.

**Three mandatory artefacts before any write lands** (this is the concrete task set — T-1 in §5):

- **(a)** `internal/cli/cmd_repo.go:54-58` `Long` text amended. It currently reads "litefactory never installs it and never writes inside the clone." The replacement must name the exception explicitly and stay narrow, e.g. *"…never installs it. The only files litefactory ever writes inside a clone are the agent-team artefacts listed in `litefactory team --help`; it never stages, commits or pushes."* Do **not** delete the sentence — a blanket reversal loses the reader's trust in the remaining half.
- **(b)** An entry in `.aiteam/AC_AMENDMENTS.md` per the `APP_CONSTRAINTS.md` preamble ("a deviation MUST be *written into* `.aiteam/AC_AMENDMENTS.md`"), amending `SEC-7` to scope it to git-mutating operations plus all paths **outside** the five-path carve-out. Without this entry the whole feature is a silent contradiction of an `[enforced]` rule.
- **(c)** A surface test in `internal/team` in the spirit of `git/surface_test.go` and `config/purity_test.go`: the owned-path list is a package-level value, and the test asserts the writer refuses any path not matching it. This turns the prose carve-out into something that fails the build when it widens. Given `TEST-10`'s culture in this repo, shipping the carve-out *without* this test is the actual risk, not the carve-out itself.

**Risk if not done:** a future contributor reads `SEC-7`, reads `cmd_repo.go`, and concludes the team writer is a bug — or, worse, reads the team writer and concludes `SEC-7` is dead letter and widens something else.

### 2.2 Bookkeeping directory (OQ12) — VERDICT: `.litefactory/`

**Name:** `.litefactory/` at the target repo root. **Contents for v1:** exactly one file, `.litefactory/teams.lock.json`.

Reasoning:

- **Not `.aiteam/`.** That directory is owned by the `aiteam` npm installer, which `cmd_repo.go` requires be run first. Writing litefactory bookkeeping into another tool's directory makes the ownership question unanswerable the first time the two disagree.
- **Not `.litefactory-team/`.** The scoping belongs in the filename, not the directory name. A directory named for one feature guarantees a second directory the first time litefactory needs to record anything else.
- **A directory, not a bare root file.** `.litefactory/teams.lock.json` keeps the repo root clean and gives the gitignore story (US-10) a single line to manage.

**Confirmation requested by the brief:** yes — `.litefactory/` is the sole carve-out alongside OQ3's four generated-file paths (`AGENTS.md`, `CLAUDE.md`, `.claude/agents/`, `.codex/skills/`). Five path shapes total, no more. **Any future file inside `.litefactory/` requires its own amendment entry**, and `internal/team/doc.go` must say so.

### 2.3 US-1's `DisallowUnknownFields` claim — CORRECTION

The proposed US-1 hedges: *"including a hand-written two-phase decode if `encoding/json`'s `DisallowUnknownFields` proves insufficient."* This is incorrect as stated. `json.Decoder.DisallowUnknownFields()` is stdlib, has existed since Go 1.10, and does exactly the required job for unknown-field rejection at every nesting level. There is no scenario in this schema where it "proves insufficient" for that purpose, and the engineer should not build a fallback for a problem that does not exist (platform-constraints flags premature abstraction).

A **two-phase decode is warranted for a different reason**: version-first dispatch. Decode a tiny envelope (`struct{ SchemaVersion int \`json:"schemaVersion"\` }`) with a *permissive* decoder, check the version, then decode the full versioned shape with the *strict* decoder. That is two `json.Unmarshal` calls over the same bytes, not a hand-written parser.

**Error-message quality** should mirror `internal/config`'s `friendlyYAMLError` treatment of `yaml.Decoder.KnownFields(true)`: an unknown key in an index should name the key and the nearest valid one, not surface `json: unknown field "…"` raw. Copy the *ambition*, not the mechanism.

### 2.4 The `schemaVersion` migration — DEVIATION: drop it for v1

US-1 proposes "a small sequential `schemaVersion` migration mirroring `internal/store/migrate.go`, scoped down." **Recommend dropping the migration machinery.** `store/migrate.go` exists because a SQLite database is stateful and cannot be re-fetched; an index is a *document that is re-read from source on every command*. There is exactly one schema version in existence. A migration framework for a single-version, re-fetchable document is the textbook premature abstraction.

**Replace with:** record `schemaVersion`, validate it equals the one supported constant, and return a typed error (`UnsupportedIndexVersionError`, unwrapping to a sentinel per `STYLE-5`) that tells the operator to upgrade litefactory when the version is higher, and that the index is from a future/past litefactory when lower. When a v2 schema genuinely arrives, add the second decode path then — the envelope decode from §2.3 already makes that a one-file change.

The **lockfile** is a different matter: it *is* stateful, it *is* on someone's disk, and it does need a `schemaVersion` + a forward-compat story. Keep it there (US-5, T-23).

### 2.5 CLI command reference — VERDICT: confirmed, with a story gap

Exit codes are fixed by `internal/cli/errors.go`: `ExitOK=0`, `ExitFailure=1`, `ExitUsage=2`, `ExitPrecondition=3`. `ExitCode(err)` switches on `errors.As` for `*UsageError` / `*PreconditionError`. Per `STYLE-6`, handlers never read the flag set back and never call `os.Exit` — they return an error kind.

| Command | Usage | Flags | Exit codes | Story |
|---|---|---|---|---|
| `team list` | `team list [--json]` | `--json` | 0 ok; 2 bad flags/args; 3 index unreadable or invalid; 1 other | US-6 |
| `team show <team>` | `team show <team> [--json]` | `--json` | 0 ok; 2 bad args; 3 index unreadable/invalid **or unknown team**; 1 other | US-6 |
| `team add <repo-id> <team>` | `team add <repo-id> <team> [--dry-run] [--adopt] [--json]` | `--dry-run`, `--adopt`, `--json` | 0 ok (incl. `--dry-run`); 2 bad args; 3 repo unresolvable/ambiguous, unknown team, **already installed**, screening blocked, symlink/path refusal; 1 partial write failure | US-5 |
| `team status <repo-id>` | `team status <repo-id> [--team <name>] [--json]` | `--team`, `--json` | 0 ok **regardless of drift** (drift is a value, `CONC-3`); 2 bad args; 3 repo unresolvable, no lockfile, corrupt lockfile; 1 other | US-7 |
| `team update <repo-id>` | `team update <repo-id> [--team <name>] [--dry-run] [--force] [--prune] [--all-worktrees] [--json]` | `--team`, `--dry-run`, `--force`, `--prune`, `--all-worktrees`, `--json` | 0 ok (incl. no-op and `--dry-run`); 2 bad args; 3 repo unresolvable, no lockfile, screening blocked, **blocked `modified` without `--force`**; 1 partial failure across worktrees | US-8 / US-9 |
| `team remove <repo-id> <team>` | `team remove <repo-id> <team> [--dry-run] [--json]` | `--dry-run`, `--json` | 0 ok; 2 bad args; 3 repo unresolvable, team not installed; 1 partial failure | **US-13 (new)** |

Notes binding on every row:

- `--json` on all six for scriptability; the table renderer and the JSON renderer read the same value type. This is one flag, not a story.
- **Exit 3 for "blocked drift without `--force`"** is deliberate: it is a precondition, not a failure. It makes `team update` safe in a script that checks `$?`.
- `--all-worktrees` is US-9's flag and is listed on `team update` only. **Do not add it to `team add`.** Rationale: `add` is a first-time install and the operator should choose the target explicitly; silently installing into four worktrees on a first run is a surprise. US-9's fan-out is an `update`-only behaviour for v1. *(This narrows the "and `team add`?" in the proposed US-9 — my answer is no.)*

**`team remove` — sanity check verdict: KEEP IT.** The inference is sound and, more than sound, it is *load-bearing*. OQ5 makes multi-team-per-repo the working assumption; US-4 makes shared agents produce one marker span whose `team=` attribute lists every contributing team. Without a removal verb there is no code path that ever narrows that list, which means the dedup design has a write path and no unwrite path — the model does not close. Additionally the lockfile accumulates entries no command can ever retire, which makes `--prune`'s orphan detection meaningless over time.

**But: no story in the proposed 12 owns it.** US-8 is `team update`; folding removal into it would make US-8 the largest story in the plan and mix two different planners. See DEVIATION D-4 (§6) — recommend a 13th story.

**Contradiction to flag:** `feature-spec.md`'s Out of Scope section explicitly lists *"A `team remove`/uninstall command."* That spec is stale (see D-1), but it is marked APPROVED, and this is the sharpest single contradiction between it and the corrected brief.

### 2.6 JSON index worked example — VERDICT: structurally sound, four corrections

The brief's Domain §1 shape (`agents` map of key→`{source,dest}`, `shared` list, `teams` list) is correct and I endorse it. OQ13's decision to keep `dest` as an object field rather than collapsing to a bare string is right — it leaves room for a per-entry attribute later without a schema break.

**Correction 1 — `schemaVersion` is missing from the example.** The story list requires it; the worked example must show it, at top level, as an int.

**Correction 2 — `dest` semantics are ambiguous and will break the Codex renderer.** The example gives `"dest": "agents.md"` — a *filename with an extension*. The Claude renderer wants `.claude/agents/<X>.md`; the Codex renderer wants `.codex/skills/<X>/SKILL.md`, where `<X>` is a **directory name**. If `dest` carries `.md`, the Codex path becomes `.codex/skills/agents.md/SKILL.md`, which is legal but absurd.

**Decision: `dest` is a bare stem — no extension, no slashes.** Each renderer owns its own layout and extension. `"dest": "agents"` → `.claude/agents/agents.md` and `.codex/skills/agents/SKILL.md`. Validation rejects any `dest` containing `.`, `/`, `\`, `..`, or a path separator. Pin this in `internal/team/doc.go` and in US-1's validation task.

**Correction 3 — `dest` uniqueness is unvalidated.** Two agent keys may legitimately have different keys and the same `dest`; that is a silent file-clobber. Validation MUST reject duplicate `dest` values **within any team's effective member list** (own ∪ shared), which is stricter than global uniqueness would be worth and is the actual failure surface.

**Correction 4 — team-name uniqueness and dangling references are unvalidated.** `teams` is a list, so duplicate `name`s are expressible; every entry in `teams[].agents` and in `shared` must resolve to a key in `agents`. Both are load-time validation errors.

Also worth pinning, though not errors in the example: an agent appearing in both a team's own list and in `shared` MUST be deduped (not rendered twice), and the effective member list MUST be sorted before iteration (`TEST-7`) so output is byte-deterministic.

**Corrected 13-agent worked example** (go-team, 5 Go + 8 shared, per OQ15):

```json
{
  "schemaVersion": 1,
  "agents": {
    "app-context-updater":          { "source": "teams/shared/app-context-updater.md",          "dest": "app-context-updater" },
    "codebase-tour":                { "source": "teams/shared/codebase-tour.md",                "dest": "codebase-tour" },
    "go-architect":                 { "source": "teams/go-team/go-architect.md",                "dest": "go-architect" },
    "go-code-reviewer":             { "source": "teams/go-team/go-code-reviewer.md",            "dest": "go-code-reviewer" },
    "go-implementation-engineer":   { "source": "teams/go-team/go-implementation-engineer.md",  "dest": "go-implementation-engineer" },
    "go-integration-test-engineer": { "source": "teams/go-team/go-integration-test-engineer.md","dest": "go-integration-test-engineer" },
    "go-unit-test-engineer":        { "source": "teams/go-team/go-unit-test-engineer.md",       "dest": "go-unit-test-engineer" },
    "implement-orchestrator":       { "source": "teams/shared/implement-orchestrator.md",       "dest": "implement-orchestrator" },
    "plan-orchestrator":            { "source": "teams/shared/plan-orchestrator.md",            "dest": "plan-orchestrator" },
    "proxy-product-owner":          { "source": "teams/shared/proxy-product-owner.md",          "dest": "proxy-product-owner" },
    "requirements-engineer":        { "source": "teams/shared/requirements-engineer.md",        "dest": "requirements-engineer" },
    "research-orchestrator":        { "source": "teams/shared/research-orchestrator.md",        "dest": "research-orchestrator" },
    "web-researcher":               { "source": "teams/shared/web-researcher.md",               "dest": "web-researcher" }
  },
  "shared": [
    "app-context-updater", "codebase-tour", "implement-orchestrator", "plan-orchestrator",
    "proxy-product-owner", "requirements-engineer", "research-orchestrator", "web-researcher"
  ],
  "teams": [
    {
      "name": "go-team",
      "agents": [
        "go-architect", "go-code-reviewer", "go-implementation-engineer",
        "go-integration-test-engineer", "go-unit-test-engineer"
      ]
    }
  ]
}
```

Effective member list for `go-team` = 5 + 8 = 13, sorted by key. This is the dogfood target.

### 2.7 Boundary call: where the fuzzy resolver lives (US-2)

**Put `ResolveIdentifier` in `internal/repos`, not `internal/team`.** `internal/repos` already imports `store` and `git`; `internal/team` should import neither (§Key boundaries). The resolver resurrects the currently-unused `store.GetRepoBySlug` (`internal/store/repos.go:118`) for the exact `owner/name` path and falls back to a case-insensitive substring match over `ListRepos()` (`:130`). The ambiguity error is a typed error unwrapping to a sentinel (`STYLE-5`), presented in `AmbiguousIssueError`'s style, and translated to `ExitPrecondition` by the `cli` wrapper — the domain package must not import `cli.PreconditionError` (`errors.go` says so explicitly).

### 2.8 Reusable patterns confirmed from the stale plan

These four survive the design change intact and should be carried forward:

- **Atomic write + `os.Lstat` symlink refusal + path-containment check.** Carry forward unchanged. This is the single highest-value defensive control in the feature (§4).
- **Drift classification table** (`clean` / `modified` / `missing` / `orphan` / `foreign`, plus the orthogonal upstream axis, plus `hash-scheme-changed`). Carry forward. The **`foreign` never deleted under any flag, ever** rule is non-negotiable and must appear as an acceptance criterion, not a hint.
- **Byte-determinism requirement** (sorted keys, fixed field order, LF endings, single trailing newline). Carry forward and strengthen: a render-twice-compare-bytes test, because non-determinism silently turns every `team status` into a false `modified`.
- **`internal/gittest` snapshot helpers** (`Capture`, `Snapshot.Diff`, `AssertUnchanged`). These are exactly the right proof instrument for "we wrote precisely these five path shapes and nothing else." Note `gittest/policy_test.go`'s `TestOnlyTestsImportGittest` — it stays a test-only import.

**Reduce the three-hash model to two.** The stale plan's `baseSourceHash` existed to answer "did the *embedded layer* change, separately from the org/repo layers" — a question that only exists in the rejected three-layer design. With a pure index there is exactly one source of truth per agent, so two hashes suffice: `renderedHash` (bytes on disk → *did a human edit this?*) and `sourceHash` (the index-resolved body → *would update change this?*). Keep `hashVersion` — the reasoning for it (a hash-scheme change must report `hash-scheme-changed`, not a false `modified` in every repo on earth) is design-independent and still correct.

---

## Part 3 — Lens 2: Platform Constraints

Read against `.claude/skills/go/platform-constraints.md` and `.aiteam/APP_CONSTRAINTS.md`.

| # | Constraint | Status | Implication for this plan |
|---|---|---|---|
| P-1 | Project layout — `internal/` by default, `pkg/` only for real external consumers | applies | `internal/team`. Nothing here goes in `pkg/`; there is no external consumer. |
| P-2 | `SEC-7` — never writes inside a registered clone | **DEVIATION** | See §2.1. Requires the `AC_AMENDMENTS.md` entry; not optional. |
| P-3 | `SEC-1` — the permission-skipping flag is never constructed in the tree | applies, subtle | `config.ContainsForbiddenFlag` may be exported **only if the literal is never reconstructed outside `policy.go`'s fragment assembly**. `purity_test.go:90-124` greps every `.go` file in the tree. The wrapper must call `forbiddenFlag()`, never inline it. |
| P-4 | `CFG-3` — a new config key lands in `Defaults()` **and** `internal/config/template.go` | applies | `config.TeamIndex` + `DefaultTeamIndex` const + `validate.go` URL/path check + `template.go` line, mirroring `GitHub.APIBase` (`config.go:213`, `:441`, `:470`; `validate.go:171-181`; `template.go:304`). `template_test.go` pins it. |
| P-5 | `internal/config` purity — stdlib only except `gopkg.in/yaml.v3` in `config.go` | applies | The `team_index` field and its validation add no import. `ContainsForbiddenFlag` adds no import. Verified against `purity_test.go:37-73`. |
| P-6 | `CFG-8` — only `internal/config` joins paths against `$LF_HOME` | applies **if** any cache lands under `$LF_HOME` | An fs-path index source is an operator-supplied absolute path, not an `$LF_HOME` join, so no new `config.Paths` method is needed **for v1**. If US-1's index fetch ever caches under `$LF_HOME`, a `Paths` method is mandatory. Flag in US-1's notes. |
| P-7 | `CFG-5` — aggregate with `errors.Join` | applies | Index validation reports *all* problems at once, not the first. Same for US-9's per-worktree aggregation. |
| P-8 | `TEST-2` — no mocking framework, no new module; `go.mod` has exactly four direct requires | applies | Everything here is stdlib: `encoding/json`, `crypto/sha256`, `os`, `path/filepath`, `io/fs`, `embed`. **No YAML library for harness frontmatter** — emit the handful of frontmatter lines directly; `gopkg.in/yaml.v3` is a `internal/config`-only import and pulling it into `internal/team` widens the dependency surface for three keys. |
| P-9 | `TEST-4` — `internal/cli` tested in-package | applies | `cmd_team_test.go`, package `cli`. No `*_internal_test.go`. |
| P-10 | `TEST-5` — interfaces declared at the point of consumption | applies | The renderer interface is declared where the install engine consumes it, not exported from a `renderer` sub-package. Concrete `claudeRenderer` / `codexRenderer` structs are returned, interfaces accepted. |
| P-11 | `TEST-7` — sort before iterating a map | applies, **load-bearing** | `agents` is a map. Every render, every marker, every lockfile write iterates it. Unsorted iteration = non-deterministic bytes = every `team status` reports false drift. This is the single most likely correctness bug in the feature. |
| P-12 | `TEST-10` — `surface_test.go` / `policy_test.go` / `purity_test.go` fail on design change; make the change deliberately, never edit the test | applies | The `cmd_repo.go` amendment and any `SEC-7` prose change are deliberate design changes with an `AC_AMENDMENTS.md` paper trail. Additionally, add the new owned-path surface test (§2.1c). |
| P-13 | `CONC-3` — a bad outcome is a value, not an error | applies | Drift classification returns a `[]FileStatus` with a class per file. `team status` exits 0 with drift reported. Only *inability to classify* (corrupt lockfile, unreadable index) is an error. |
| P-14 | `CONC-5` — `ctx` first | applies | Every exported `internal/team` function. |
| P-15 | `STYLE-2` — no `context.Background()` outside main, no `fmt.Print*` / `log.*` | applies | Output goes to `env.Stdout` via `fmt.Fprintf` — the one sanctioned exception, documented at `command.go:54-57`. Diagnostics via `env.Logger` (`log/slog`). |
| P-16 | `STYLE-3` — `Options` struct + `New(opts)` | applies | `team.New(Options{…})` for the install engine; index source, renderers, and clock (if any) are Options fields, not globals. |
| P-17 | `STYLE-5` — typed errors unwrapping to a sentinel | applies | `AmbiguousRepoError`, `UnknownTeamError`, `AlreadyInstalledError`, `UnsupportedIndexVersionError`, `ScreeningError`, `DriftBlockedError` — each `Unwrap()`s to its package sentinel so `cli` can `errors.Is` without importing the concrete type. |
| P-18 | `STYLE-6` — CLI handlers never read the flag set back; exit code derives from error kind | applies | `New(fs *flag.FlagSet) Handler` closes over flag pointers (`command.go:24-27`). Six constructors in `cmd_team.go`, six lines in `Commands()` (`commands.go:8-20`). |
| P-19 | Error handling — `%w` wrapping, no `panic`, every error checked | applies | Standard. No `panic` in `internal/team`, including on malformed markers — a malformed marker is a classified drift state, not a crash. |
| P-20 | Concurrency — every goroutine needs an exit path | **not applicable** (none) | No goroutines introduced; see §Concurrency. This is a deliberate call, not an omission. |
| P-21 | Database access | **not applicable** (read-only) | Only `store.ListRepos`/`GetRepoBySlug`, both existing reads. No schema change, no migration, no `BEGIN IMMEDIATE`. |
| P-22 | HTTP/API design | **not applicable** (no server) | OQ4 keeps arbitrary remote fetch out of scope; there is no HTTP client in v1. |
| P-23 | Logging — `log/slog`, never `fmt.Println` | applies | See P-15. |
| P-24 | Testing — table-driven default, hand-written fakes | applies | Index validation, drift classification, and the resolver are all natural table tests. The renderer tests are golden-file. `gittest` supplies the fixture repos. |

**Cross-story constraints from the stale plan — which still hold verbatim:** P-4 (CFG-3), P-7 (errors.Join), P-8 (no new module), P-11 (sort before iterating), P-15 (env.Stdout), P-17 (typed errors), P-18 (CLI shape), and the byte-determinism rule all carry forward **unchanged**. **Needs adjustment:** the stale plan's `internal/config` three-layer merge constraints, its `ResolvedAgent` type contract, the `team manage` parameter allowlist, the opencode renderer row, and the three-hash model (→ two hashes, §2.8) are all void.

**APP_CONTEXT drift note (for `app-context-updater`, not this plan):** `.aiteam/APP_CONTEXT.md`'s package map omits `internal/agent`, `internal/attach`, `internal/codex`, and `internal/planbrief`, all of which exist. Worth a refresh pass after this feature lands.

---

## Part 4 — Lens 3: Security

Executed inline per `.claude/skills/shared/security-scanner.md`, forward-looking mode.

### Security Findings

**Scope:** the planned agent-team feature — index fetch from a configurable source, markdown rendering into a target repo, the ownership carve-out, and the lockfile.
**Rulebook:** used — `.aiteam/APP_CONSTRAINTS.md` (SEC-1..11, CFG-4) plus `.claude/skills/shared/security-scanner.md`.

| # | Area | Status | Finding |
|---|---|---|---|
| S-1 | Dependencies & supply chain | ❌ **violation risk — headline** | The index source is operator-configurable and its markdown is written into a repo where agents will *execute against it*. This is a prompt-injection / instruction-injection supply chain, not merely a file copy. US-11's screening is the only control and MUST be blocking, not advisory. |
| S-2 | Dependencies & supply chain | ⚠️ concern | `purity_test.go:94-107` skips `.claude` and `.aiteam` directories and only reads `.go` files. **Embedded team markdown under `internal/team/teams/*.md` is therefore invisible to `TestForbiddenFlagIsAbsentFromTheTree`.** The build-time grep does not cover the shipped bodies. US-11's runtime screen is genuinely non-redundant — and US-12 should additionally screen the embedded snapshot in a test. |
| S-3 | Input validation | ⚠️ concern | Agent keys, `dest` stems, and team names become filesystem paths. Path traversal via `"dest": "../../.ssh/authorized_keys"` is the obvious attack. Validation MUST be an allowlist (`[a-z0-9-]+`, no dot, no separator), not a denylist of `..`, and the writer MUST independently re-check containment before every write. Two layers, deliberately. |
| S-4 | Data handling | ⚠️ concern | The install engine follows paths that may be symlinks planted in the target repo. `os.Lstat` refusal before every write (carried forward from the stale plan) is required, on the **final** path and on each parent directory it creates. |
| S-5 | Secrets & configuration | ✅ compliant, conditional | `config.ClassifySecret` + `ErrLiteralSecret` (exit 3) already exist and are the right instrument. **`CFG-4`'s rule that the matched text MUST NOT appear in the error applies verbatim** to the screening error — report file, line, and class, never the value. |
| S-6 | Secrets & configuration | ✅ compliant | `config.TeamIndex` holds a URL or a path, never a credential. OQ9 keeps private/credentialed sources out of scope, which removes the entire token-handling surface from v1. Good scoping. |
| S-7 | Auth / authz | ✅ compliant — not applicable | Single-user local CLI; no multi-tenancy, no roles, no session. |
| S-8 | The ownership carve-out itself | ⚠️ concern, **mitigated** | Re-examined as a risk surface per the brief's request. The carve-out turns litefactory into a program that writes into user repos. Mitigations that make it acceptable: (a) the five-path allowlist is enumerable and test-pinned (§2.1c); (b) S-3's two-layer containment check; (c) S-4's symlink refusal; (d) `git.Clone`'s method set unchanged, so nothing is ever staged, committed, or pushed; (e) the lockfile records every write, making the blast radius auditable and reversible via `team remove`. **Without (a) and (c) I would rate this ❌.** |
| S-9 | Data handling | ⚠️ concern | The lockfile is human-editable and drives deletions under `--prune`. A hand-edited (or malicious) lockfile listing `path: "../../etc/hosts"` is an arbitrary-file-**delete** primitive. Every `path` in the lockfile MUST be re-validated as repo-relative, forward-slash, no `..`, on load — deletion paths are validated at *use*, never trusted from the file. |
| S-10 | Error handling | ✅ compliant | Typed errors unwrapping to sentinels; `cli` translates to exit codes; no stack traces or absolute host paths leaked into user-facing output beyond the target path the operator already named. |
| S-11 | Data handling | ⚠️ concern | `--prune` deletes. The `foreign` classification (no lockfile entry, no provenance marker) MUST be exempt from deletion **under every flag combination including `--force --prune`**. This needs an explicit negative test, not just a code comment. |

**Summary:** the feature's security posture rests on four controls, all of which are already named in the plan but none of which can be treated as optional: (1) **blocking** supply-chain screening of fetched markdown before any write; (2) allowlist validation of every index-derived path component *plus* independent containment re-check at write time; (3) `os.Lstat` symlink refusal; (4) `foreign` files exempt from deletion under all flags. The ownership carve-out is acceptable **only** with the test-pinned owned-path surface from §2.1c. Recommend all four be written as acceptance criteria on their stories rather than as architectural hints — they are the difference between a file-writer and a foot-gun.

---

## Part 5 — Lens 4: Task Decomposition

Per `.claude/skills/shared/task-decomposer.md`. Single-layer team — **every task goes to `go-implementation-engineer`.** Test tasks are separate entries. Numbering is fresh (`T-1`…`T-60`); the stale plan's `T-01`…`T-34` are void. Sizes: `XS` <1h, `S` 1–2h, `M` half day, `L` full day. No task exceeds `L`.

**These tables are per-story content. They belong in the sub-issues, not in `plan_brief.md`** (whose Story Index carries identity and location only).

### US-1 — Ownership boundary + index foundations

| ID | Title | Type | Depends On | Story | Est |
|----|-------|------|-----------|-------|-----|
| T-1 | `cmd_repo.go:54-58` disclaimer amendment + `SEC-7` amendment entry in `.aiteam/AC_AMENDMENTS.md` | infra | — | US-1 | XS |
| T-2 | `internal/team` package skeleton + `doc.go` recording the five owned path shapes, the carve-out, and `dest`-is-a-stem | infra | T-1 | S |
| T-3 | Index types + `schemaVersion` envelope decode + strict `DisallowUnknownFields` full decode | data | T-2 | M |
| T-4 | Index validation: key/`dest`/team-name allowlist, `dest` uniqueness per effective member set, dangling refs, shared∪team dedup, `errors.Join` aggregation | logic | T-3 | M |
| T-5 | Index source resolution: `//go:embed` default + `config.TeamIndex` (`Defaults()` + `template.go` + `validate.go`, mirroring `GitHub.APIBase`) + fs-path and registered-repo overrides | infra | T-3 | M |
| T-6 | `cmd_team.go` scaffold: six commands registered with `notImplemented` handlers, `Commands()` lines, usage text | api | T-2 | S |
| T-7 | Owned-path surface test: writer refuses any path outside the five shapes (`git/surface_test.go` idiom) | test | T-2 | M |
| T-8 | Tests: index parse + validate table (valid, unknown field, wrong version, unsafe key, dup `dest`, dangling ref, dup team) | test | T-4 | M |
| T-9 | Tests: `team_index` default/template/validation parity (`CFG-3`) | test | T-5 | S |
| T-10 | Tests: CLI scaffold registration, usage text, `ErrNotImplemented` exit code | test | T-6 | S |

### US-2 — Fuzzy repo resolver

| ID | Title | Type | Depends On | Story | Est |
|----|-------|------|-----------|-------|-----|
| T-11 | `repos.ResolveIdentifier`: exact `owner/name` via `store.GetRepoBySlug`, else case-insensitive substring on `Name` over `ListRepos()` | logic | T-2 | M |
| T-12 | `AmbiguousRepoError` + `UnknownRepoError` typed errors unwrapping to sentinels, candidates listed in `AmbiguousIssueError` presentation style | logic | T-11 | S |
| T-13 | Tests: resolver table (exact slug, unique substring, ambiguous, no match, case-insensitivity, empty registry) | test | T-12 | M |

### US-3 — Renderer contract + Claude + Codex renderers

| ID | Title | Type | Depends On | Story | Est |
|----|-------|------|-----------|-------|-----|
| T-14 | Renderer interface declared at point of consumption (`TEST-5`) + `RenderedFile{Path, Bytes}` value type | logic | T-4 | S |
| T-15 | Claude renderer → `.claude/agents/<dest>.md`, minimal YAML frontmatter emitted directly (no YAML dep, `TEST-2`) | logic | T-14 | M |
| T-16 | Codex renderer → `.codex/skills/<dest>/SKILL.md`, Agent Skills frontmatter | logic | T-14 | M |
| T-17 | Byte-determinism: sorted map iteration (`TEST-7`), fixed field order, LF endings, single trailing newline | logic | T-15, T-16 | S |
| T-18 | Tests: golden-file render per harness | test | T-17 | M |
| T-19 | Tests: render-twice-compare-bytes determinism, across both harnesses and a 13-member set | test | T-17 | S |

### US-4 — Root singleton composition (`AGENTS.md` / `CLAUDE.md`)

| ID | Title | Type | Depends On | Story | Est |
|----|-------|------|-----------|-------|-----|
| T-20 | Marker grammar + span parser for `<!-- litefactory:team BEGIN team=… agent=… -->` / END; unterminated span is a classified state, never a panic | logic | T-14 | M |
| T-21 | Compose/replace: replace an existing span in place, append new spans at end, leave all unmarked content byte-identical | logic | T-20 | M |
| T-22 | Shared-agent dedup: one span per agent, `team=` attribute lists every contributing team, sorted | logic | T-21 | M |
| T-23 | Subtractive mode: remove a span leaving surrounding bytes identical (consumed by US-13) | logic | T-21 | S |
| T-24 | Tests: idempotence (compose twice = same bytes), hand-written content preserved, malformed/unterminated markers, missing root file created | test | T-22 | L |
| T-25 | Tests: multi-team dedup — install two teams sharing an agent, one span, both names in `team=` | test | T-22 | M |

### US-5 — Install engine + `team add`

| ID | Title | Type | Depends On | Story | Est |
|----|-------|------|-----------|-------|-----|
| T-26 | Lockfile schema at `.litefactory/teams.lock.json` + `schemaVersion` + `hashVersion` + load-time invariant validation (sorted, no dupes, hex hashes, repo-relative paths with no `..`) | data | T-4 | M |
| T-27 | Two-hash model: `renderedHash` (bytes on disk) and `sourceHash` (index-resolved body), canonical hash input | logic | T-26 | M |
| T-28 | Atomic write helper: temp-file + rename, `os.Lstat` symlink refusal on final path and each created parent, independent containment re-check | infra | T-2 | M |
| T-29 | Install engine: effective member list → renderers → composition → files → lockfile, all-or-nothing semantics | logic | T-27, T-28, T-22, T-17 | L |
| T-30 | `team add` wiring: `--dry-run`, `--adopt`, `--json`, refuse-if-installed pointing at `team update`; `Long` text warns the worktree becomes dirty | api | T-29, T-6, T-11 | M |
| T-31 | Tests: install into a `gittest` fixture; `Snapshot.Diff` proves exactly the five owned path shapes changed | test | T-30 | L |
| T-32 | Tests: symlink refusal, path-escape refusal, partial-failure leaves the target unchanged | test | T-28, T-29 | M |

### US-6 — `team list` / `team show`

| ID | Title | Type | Depends On | Story | Est |
|----|-------|------|-----------|-------|-----|
| T-33 | `team list` handler: table + `--json`, both from one value type, written to `env.Stdout` | api | T-5, T-6 | S |
| T-34 | `team show <team>` handler: effective member list incl. shared, marked as own/shared | api | T-33 | S |
| T-35 | Tests: both commands, both output modes, unknown team → exit 3 | test | T-34 | M |

### US-7 — `team status` (drift classification)

| ID | Title | Type | Depends On | Story | Est |
|----|-------|------|-----------|-------|-----|
| T-36 | Drift classification on two orthogonal axes (on-disk vs last-installed; index source vs last-synced) returning a value, not an error (`CONC-3`) | logic | T-27 | M |
| T-37 | `orphan` vs `foreign` discrimination via provenance marker; `hash-scheme-changed` when lockfile `hashVersion` < binary's | logic | T-36 | S |
| T-38 | `team status` handler + report rendering (table + `--json`), exit 0 regardless of drift | api | T-36, T-6, T-11 | M |
| T-39 | Tests: one fixture case per class — `clean`, `modified`, `stale`, `missing`, `orphan`, `foreign`, `hash-scheme-changed` | test | T-38 | L |

### US-8 — `team update`

| ID | Title | Type | Depends On | Story | Est |
|----|-------|------|-----------|-------|-----|
| T-40 | Update planner: per-class safe-to-overwrite vs must-flag, returns a plan value | logic | T-36 | M |
| T-41 | Update applier reusing the install engine and atomic writes; lockfile rewritten last | logic | T-40, T-29 | M |
| T-42 | `--prune`: deletes `orphan` only; `foreign` exempt under every flag combination | logic | T-41 | M |
| T-43 | `team update` wiring: `--team`, `--dry-run`, `--force`, `--prune`, `--json`; blocked `modified` without `--force` → exit 3 | api | T-41, T-42 | S |
| T-44 | Tests: each drift class × update and × `--force` | test | T-43 | L |
| T-45 | Tests (negative, required): a `foreign` file survives `--prune --force`; a hand-edited lockfile path containing `..` is rejected rather than deleted | test | T-42 | M |

### US-9 — Multi-worktree propagation

| ID | Title | Type | Depends On | Story | Est |
|----|-------|------|-----------|-------|-----|
| T-46 | Candidate-set computation in `repos`: `store.Session.WorktreePath` history ∪ `(forge,owner,name)` collision set from `ListRepos()` | logic | T-11 | M |
| T-47 | Live ground-truth filter via existing `git.Clone.WorktreeList`; a stale candidate is dropped, not an error | logic | T-46 | M |
| T-48 | Sequential fan-out in `team update` over the filtered path list, `errors.Join` aggregation, per-worktree partial-success report; `--all-worktrees` flag (update only, not add) | logic | T-47, T-43 | M |
| T-49 | Tests: multi-worktree `gittest` fixture; stale candidate dropped; one worktree fails and the others still report | test | T-48 | L |

### US-10 — Gitignore + bookkeeping carve-out — **depends on US-9; NOT parallelizable**

> **Scheduling constraint, stated plainly:** US-10 MUST be scheduled strictly after US-9 completes. It MUST NOT be run as an independent or parallel story. Its per-worktree vs. shared-root semantics for `.litefactory/` are only decidable once US-9's propagation model exists; scheduling it earlier produces a decision that US-9 then invalidates.

| ID | Title | Type | Depends On | Story | Est |
|----|-------|------|-----------|-------|-----|
| T-50 | Decide and implement `.litefactory/` per-worktree vs shared-root semantics under US-9's propagation model | infra | T-48 | M |
| T-51 | `.gitignore` handling: marker-delimited block for the litefactory-owned entry, hand-written lines untouched, idempotent | logic | T-50 | M |
| T-52 | Tests: gitignore idempotence, hand-written lines preserved, `.litefactory/` semantics across two worktrees | test | T-51 | M |

### US-11 — Supply-chain content screening

| ID | Title | Type | Depends On | Story | Est |
|----|-------|------|-----------|-------|-----|
| T-53 | Export `config.ContainsForbiddenFlag` wrapping the unexported `forbiddenFlag()`; the literal is never reconstructed (`SEC-1`, `purity_test.go`) | logic | T-1 | S |
| T-54 | Blocking screening pass over every fetched markdown body: `config.ClassifySecret` + forbidden-flag check, before any write; matched text never echoed (`CFG-4`) | logic | T-53, T-29 | M |
| T-55 | Tests: screening blocks `team add` and `team update` at exit 3; error names file/line/class but contains no secret text; `TestForbiddenFlagIsAbsentFromTheTree` still green | test | T-54 | M |

### US-12 — Go-team bootstrap content (dogfood)

| ID | Title | Type | Depends On | Story | Est |
|----|-------|------|-----------|-------|-----|
| T-56 | Harness-agnostic body convention: strip harness frontmatter from the 13 `.claude/agents/*.md` sources, document what a body may and may not assume | data | T-15, T-16 | M |
| T-57 | Author `internal/team/teams/go-team/*.md` — the 5 Go agents | data | T-56 | L |
| T-58 | Author `internal/team/teams/shared/*.md` — the 8 generic/orchestration agents | data | T-56 | L |
| T-59 | `internal/team/teams/index.json` (the §2.6 corrected shape) + `//go:embed` wiring | data | T-57, T-58, T-5 | S |
| T-60 | Tests: embedded index parses and validates; every `source` resolves; embedded bodies pass US-11 screening; round-trip install into a fixture yields 13 agents | test | T-59, T-54 | M |

### US-13 — `team remove` **(NEW — see DEVIATION D-4)**

| ID | Title | Type | Depends On | Story | Est |
|----|-------|------|-----------|-------|-----|
| T-61 | Removal planner: which owned paths and which marker spans belong to the removed team **only** | logic | T-23, T-36 | M |
| T-62 | Shared-agent retention: an agent still claimed by another installed team is kept and its span's `team=` list narrowed; lockfile entries retired | logic | T-61, T-22 | M |
| T-63 | `team remove` wiring: `--dry-run`, `--json`; `foreign` files never touched; team-not-installed → exit 3 | api | T-62 | S |
| T-64 | Tests: two-team fixture, remove one, prove shared agents survive, `team=` narrowed, hand-written content byte-identical, `foreign` untouched | test | T-63 | L |

### Build order

```
US-1 ─┬─> US-2 ─────────────────────────────┐
      ├─> US-3 ──> US-4 ──┐                 │
      └─> US-11(T-53) ────┤                 │
                          v                 v
                        US-5 (install engine + team add)
                          │
              ┌───────────┼───────────┬──────────────┐
              v           v           v              v
            US-6        US-7 ──> US-8 ──> US-9 ──> US-10
                          │        │
                          │        └──> US-11 (T-54/T-55)
                          └──> US-13 (team remove)
                                       US-12 (content) — parallel from US-3 onward,
                                         gated only by T-59's dependency on T-5
```

US-12 is the one story that can run in parallel with almost everything from US-3 onward — it is content authoring, not engine work. Recommend starting it early; T-57/T-58 are the two `L` tasks in the plan and they gate the dogfood proof.

---

## Part 6 — Deviations

### D-1 — `feature-spec.md` is APPROVED but stale (**headline**)

**Conflict.** `$session/feature-spec.md` (dated 2026-09-04, `Status: APPROVED`) encodes the rejected design: three config layers (embedded → org → repo-local `.aiteam/team.json`), `AgentDefinition` with `promptPath`/`model`/`tools`/`harnesses` stanzas, `contentVersion`, `team manage set/get/unset` with a closed parameter allowlist, an **opencode renderer** writing `.opencode/agents/<agent>.md`, a `--team-config` flag and `team_config` config key, an Owned Paths table that **omits root `AGENTS.md`/`CLAUDE.md`**, and `team remove` explicitly in **Out of Scope**. Every one of these is contradicted by the corrected `RESEARCH_BRIEF.md` and by OQ1–OQ15.

**Impact.** `implement-orchestrator`'s gate reads `plan_brief.md`, but engineers reading the session directory will find an APPROVED spec that tells them to build the opposite feature — including one (`team remove`) the plan requires and the spec forbids. This is a live trap.

**Options.** (a) Mark `feature-spec.md` superseded with a header pointing at `RESEARCH_BRIEF.md` — cheapest, preserves history. (b) Rewrite it against the corrected design — correct but duplicates the brief. (c) Delete it — loses the still-valid drift table and acceptance-criteria language. **Recommendation: (a).** The reconciliation run should add a Correction Notice header, exactly as `RESEARCH_BRIEF.md` itself carries.

*This decision is the sponsor's, not mine; I am not editing that file in this run.*

### D-2 — `SEC-7` amendment required in `.aiteam/AC_AMENDMENTS.md`

**Rule:** `SEC-7 [enforced]` — "litefactory MUST NOT write inside a registered clone."
**Deviation:** narrow `SEC-7` to git-mutating operations plus all paths outside the five-path carve-out (§2.1). "Never stages, never commits, never pushes" is retained verbatim and remains enforced by `git.Clone`'s unchanged method set.
**Reasoning:** the feature is unbuildable otherwise, and OQ3 has already settled the scope decision. What has *not* been done is the paperwork the `APP_CONSTRAINTS.md` preamble mandates.
**Risk:** an unamended constraint file makes every future reviewer either block the code or discount the constraint file. Mitigated by §2.1's three artefacts, especially the test-pinned owned-path surface (T-7).
**Decision needed from:** the owner. **Until then:** T-1 is the first task in the plan and no write task may land before it.

### D-3 — Drop the index `schemaVersion` migration framework (US-1)

Replace the proposed `store/migrate.go`-style sequential migration with a version check and a typed error. Reasoning in §2.4: an index is a re-fetchable document, not stateful data; there is one version; a migration framework here is premature abstraction that platform-constraints explicitly flags. The **lockfile** keeps its version and forward-compat story. **Impact:** US-1 loses roughly one `M` task.

### D-4 — Add US-13 `team remove`; do not fold it into US-8

The six-command CLI surface includes `team remove`, and §2.5 shows it is load-bearing (nothing else ever narrows a shared agent's `team=` list or retires a lockfile entry). No proposed story owns it. Folding it into US-8 would give US-8 two different planners, `--prune` semantics tangled with removal semantics, and roughly 10 tasks. **Recommend a 13th story** with its own planner, depending on US-4's subtractive mode (T-23) and US-7's classifier (T-36). **Impact:** 12 stories → 13; four tasks; no change to any other story's scope.

### D-5 — `--all-worktrees` is `team update`-only, not `team add`

The proposed US-9 asks "and `team add`?" — **no.** A first-time install into four worktrees the operator did not name is a surprise, and `add` already refuses when a lockfile exists, which makes the fan-out semantics ambiguous (install everywhere, or everywhere-not-yet-installed?). Keep `add` single-target; `update` propagates. **Impact:** narrows US-9 by one code path and one test case.

### Smaller corrections folded into the review, not raised as deviations

- Three-hash model → **two hashes** + `hashVersion` (§2.8). `baseSourceHash` answered a three-layer question that no longer exists.
- `dest` is a **bare stem**, not a filename (§2.6, Correction 2). The example's `"agents.md"` would produce `.codex/skills/agents.md/SKILL.md`.
- `DisallowUnknownFields` **is** sufficient; the two-phase decode is for version dispatch only (§2.3).
- The fuzzy resolver lives in `internal/repos`, not `internal/team` (§2.7).
- `internal/team` imports neither `store` nor `git`; `cli` passes it paths (§Key boundaries).
- `--json` on all six commands, treated as one flag rather than a story.
- `team add`'s `Long` text must warn that the target worktree becomes dirty and may trip the daemon's `ErrWorktreeUncommittedChanges` guard.

---

## Part 7 — Scope Gaps Noted, Not Filled

- **No architect is rostered for release/packaging concerns.** The embedded snapshot means a shipped agent body change is a binary release; how that is versioned and communicated to operators is a release-process question, not a Go architecture one. Engineer should follow `.aiteam/APP_CONSTRAINTS.md` and flag assumptions in task notes.
- **`.gitignore` policy for a target repo is a product decision**, not an architectural one: whether litefactory *should* gitignore `.litefactory/` (operators may reasonably want the lockfile committed, so the team install is reproducible for their colleagues) is US-10's open product question. I have given it the mechanism, not the default. Recommend `proxy-product-owner` settle the default before US-10 starts.
- **No story covers `litefactory team --help`'s top-level group text**, which §2.1a's `cmd_repo.go` amendment points at. Small — fold into US-1's T-6 rather than adding a story.
