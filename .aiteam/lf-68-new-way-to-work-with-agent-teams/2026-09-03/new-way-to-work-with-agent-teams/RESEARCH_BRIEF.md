# Research Brief — lf-68-new-way-to-work-with-agent-teams

## Correction Notice — 2026-09-04, second pass

The sponsor reviewed the body of this brief plus its first (2026-09-04) addendum and **rejected
the core design as wrong.** This is not a third addendum layered on top — the sections below are
rewritten outright wherever they depended on the rejected design. Sections that remain accurate
are kept, cited as before.

**What was wrong, in one sentence:** the brief designed the JSON document as a *config carrier*
(per-harness tool/model/sandbox settings living inside the JSON, rendered out to target repos) —
the sponsor's model is a JSON *index* (a key → markdown-filename map, nothing else), with all real
content staying in markdown, exactly as it already is today.

The four corrections, taken as settled fact for this rewrite (not re-litigated):

1. The JSON is an index of links to markdown files, not a config document. A team is a named list
   of agent keys plus a shared list of keys. Nothing about the JSON itself is ever copied into a
   target repo — only the markdown files it points to are copied there.
2. The index lives in git, and its source is a configurable setting — default is litefactory's own
   public repo; a user/org can point it at a different repo or a static path instead.
3. Repo identification for `team add` needs a real resolution mechanism, not a raw path — a bare
   positional word matched fuzzily against connected repos' remote-derived names, erroring on
   ambiguity rather than silently guessing.
4. `team add` must produce a real, working per-harness file structure in the target repo —
   per-agent files in per-harness subfolders, plus two root-level singleton files (`AGENTS.md`,
   `CLAUDE.md`) that are *composed* from multiple contributing markdown sources (this team's own
   agents plus the shared pool) with enough tagging to know which section came from which
   agent/team, and to support later re-diffing on `team update`.

**Explicitly out of scope for this run:** `plan_brief.md` and `US-1.md`–`US-14.md` are now stale
relative to this corrected brief and are **not touched here** — a human will open a separate Plan
run to reconcile them once this brief is accepted. Everywhere the original body cited a decision
from `plan_brief.md` as "resolved," that citation is retained below only as a paper trail of what
is now stale, never as something still binding.

This is a headless run: no clarifying question was put to a human. Ambiguities are resolved with
the most reasonable reading and flagged explicitly as assumptions.

## Refinement Notice — 2026-09-05, third pass

The sponsor confirmed the corrected index-based design above is right ("looks much better
already") and requested five specific refinements on top of it — this is refinement of an accepted
design, not a further rejection, so this pass **extends** the sections below rather than rewriting
them. Nothing from the second-pass correction is reopened.

The five refinements, taken as settled fact for this pass:

1. The default index source is concretely `https://github.com/marioatcloudvane/litefactory/teams`
   — litefactory's own repo, a `teams/` path, with one subfolder per team plus a `shared/`
   subfolder for cross-team agents. See Domain §2.
2. Each index entry needs an explicit source-path → destination-filename split (a source markdown
   file can live at an arbitrary path/name; it lands under a specific, possibly different,
   filename once imported). See Domain §1 (schema revision).
3. `team update`'s effect must reach every worktree of a repo, not just the one it was invoked
   from — litefactory's existing worktree-tracking substrate was researched fresh this run to
   determine what exists to build this on. See Domain §9 (new).
4. Once (3) is solved, the generated per-harness/root files no longer need to be git-tracked in
   the target repo and should be gitignored — except litefactory's own bookkeeping location, which
   must be explicitly protected. See Domain §10 (new).
5. The first team to actually ship must be a real "go-team" built from this repo's own existing
   Go-specific agents, as a concrete, dogfood-able deliverable — recorded as a scope requirement
   for Plan mode, not written as a story here. See Domain §11 (new) and Recommended scope.

This is again a headless run: no clarifying question was put to a human. New ambiguities are
resolved with the most reasonable reading and flagged explicitly below.

## Request

litefactory's agent teams (the specialist sub-agents — go-architect, requirements-engineer,
codebase-tour, web-researcher, etc. — and the orchestrators that invoke them) are currently
"built in" to this one repo. The corrected request:

1. A small JSON **index** file maps a short agent key to a markdown filename, e.g.
   `{"requirements-engineer": "req-engineer.md"}`. It carries no config, no per-harness settings,
   no rendering data — it is a lookup table and nothing else.
2. A **team** is a named list of such keys, plus a **shared** list of keys any team may draw on.
   Agent content itself stays as plain markdown, exactly as `.claude/agents/*.md` and
   `.codex/skills/*/SKILL.md` already are in this repo.
3. The index is **git-hosted**, and its source is **configurable** — default: litefactory's own
   public repo; overridable to a different repo, or to a static filesystem path. `team list` (the
   sponsor also said `repo list` — read here as the same command, see Open Questions) resolves
   against whichever source is configured.
4. `litefactory team add <repo-identifier> <team-name>` installs a team into a connected repo.
   `<repo-identifier>` is a bare word (not a flag), fuzzily matched against connected repos by a
   name derived from each repo's git remote; an unambiguous match resolves, an ambiguous one
   errors and asks for a more specific identifier.
5. `team add` copies/renders the resolved team's markdown (its own agents + the shared pool) into
   the target repo in the layout each harness needs: per-agent files in per-harness subfolders
   (e.g. `.claude/agents/<key>.md`, `.codex/skills/<key>/SKILL.md`), **and** contributes sections
   into two root-level singleton files, `AGENTS.md` and `CLAUDE.md`, whose content is team-specific
   and partly shared — composed, tagged, and traceable, not blindly overwritten.
6. Some agents are shared across teams (e.g. a requirements engineer); others are team-specific
   (go-implementer, python-implementer, ...).
7. Agents must work across multiple harnesses (Claude Code, Codex CLI, opencode, ...).
8. `litefactory team update` re-syncs an installed team against the current index, and `team
   manage` was originally specified as `litefactory team manage <agentname> set <param>=<value>` —
   **this item is directly undercut by Correction 1** and needs re-scoping; see Domain §7.

**Refinements added this pass (2026-09-05), folded into the numbered request above:**

9. The default index source's concrete location and layout: `https://github.com/marioatcloudvane
   /litefactory/teams`, with per-team subfolders and a `shared/` subfolder underneath it (Domain
   §2).
10. Each index entry must carry both a source path and a destination filename, not assume they
    match (Domain §1).
11. `team update` (and by extension `team add`) must propagate its effect to every worktree of the
    target repo, not only the one it was invoked against (Domain §9).
12. Once (11) is reliable, the generated per-harness/root files should be gitignored in the target
    repo, except for litefactory's own bookkeeping location, which must stay tracked/protected
    (Domain §10).
13. The first shipped team must be a real "go-team" sourced from this repo's own existing
    Go-specific agents (Domain §11).

## Codebase Context

**[V]** litefactory is a single Go module/binary (`cmd/litefactory/main.go` → `internal/cli`)
with ~15 `internal/*` packages. No package named `team`, `index`, `roster`, or `manifest` exists
today, and no `team` command, `team.json`, or index concept exists anywhere in `internal/`
— this is greenfield.

**[V] Agent teams are not Go data today — they are two hand-authored Markdown trees, owned by an
external tool, one file per agent, 13 agents each, one-to-one:**
- `.claude/agents/*.md` — Claude Code's own sub-agent format: YAML frontmatter (`name`, `tools`,
  `description`, `model`, `color`), body = full prose system prompt.
- `.codex/skills/*/SKILL.md` — Codex CLI's Agent Skills format: narrower frontmatter (`name`,
  `description` only), each opening with a hand-written diff preamble against the Claude version
  (spot-checked `go-architect`: `.codex/skills/go-architect/SKILL.md:6-20` states plainly "Codex
  copy of `.claude/agents/go-architect.md`. Both tools read the same instructions; only the
  enforcement differs," then enumerates the deltas — tools not per-agent enforced, delegation via
  `spawn_agent` not `Task`, no separate reply channel, Codex chooses its own model). Body text is
  otherwise identical from line ~22 onward in the pair inspected.
- **[V] No third tree exists.** `.opencode/` is confirmed absent from this repository (glob
  returned nothing this run).
- **[V] No tooling enforces the "Codex is generated from Claude" claim.** `AGENTS.md:61-63` states
  it in prose only ("This parity is deliberately a copy, not a shared source... If you change one,
  change the other, or say plainly in your report that they have drifted"). No generator script,
  build step, or CI check was found anywhere — a design that wants automated composition/
  provenance for team files (Correction 4) has **nothing in this codebase to model itself on; it
  is genuinely greenfield**, not an extension of an existing pattern.
- **[V] `.claude/AGENT_ROSTER.md`** claims at its own header (line 3) to be "injected into
  `CLAUDE.md` below the routing rules" — but no such injected content is present in the current
  root `CLAUDE.md`. This claim looks stale/aspirational relative to the file's actual current
  content, and should not be cited as live precedent for the composition design in Domain §7.

**[V] litefactory explicitly disclaims ownership of files inside a registered repo's clone** —
`internal/cli/cmd_repo.go:54-58`: "aiteam must already be installed in the repository... litefactory
never installs it and never writes inside the clone." Correction 4 (litefactory writing
`AGENTS.md`/`CLAUDE.md`/per-agent files into target repos) runs directly into this disclaimer —
carried into Constraints & Risks below as a live, unresolved tension, not something Plan mode has
already settled (the prior brief's citation of `plan_brief.md` DEV-1 as having settled this is now
stale — see Correction Notice).

**[V] `internal/repos.DetectAiteam`** (`internal/repos/aiteam.go:106-134`) sets a free-text `Team`
string at `repo add` time, persisted but never read back to select or filter an agent list —
descriptive metadata only, unrelated to the feature being designed here.

**[V] `internal/agent.Kind`** (`internal/agent/kind.go`) and **`config.Kind`**
(`internal/config/tier.go:54-79`) are the existing harness-extensibility points; `config.Kind`
deliberately duplicates `agent.Kind` as plain strings because `internal/config` is stdlib-only
(`internal/config/purity_test.go`). Any new config field for an index source must respect this
purity boundary.

**[V] `config.GitHub.APIBase`** is the strongest existing precedent in this codebase for
**"where to fetch X from, overridable, with a shipped default"** — directly relevant to Correction
2 and re-verified this run:
- Field: `internal/config/config.go:210-224` (`GitHub{APIBase string \`yaml:"api_base"\`}`).
- Default: `internal/config/config.go:440-442` (`Defaults()`), constant `DefaultAPIBase =
  "https://api.github.com"` at line 470.
- Validated in `internal/config/validate.go` (~171-181): non-empty, valid URL, http(s) scheme,
  has a host, no query/fragment ("must be a bare base URL"); trailing slash trimmed on
  normalization (~line 323).
- Rendered into the `litefactory init` template (`internal/config/template.go:~73,~304`).
- Consumed at `internal/github/client.go:41-44,94-99` — `internal/github` cannot import
  `internal/config` (it sits below it in the dependency graph, `AGENTS.md:117-135`), so it
  re-declares its own `defaultAPIBase` and receives the configured value via an `Options` struct
  passed in from above.
This exact shape — a field, a `Default...` constant, URL validation, a template line, a plain
`Options`-struct hand-off across the purity boundary — is the pattern a configurable index-source
setting should copy, not invent from scratch.

**[V] No fuzzy/substring/prefix repo-name matching exists anywhere in the codebase**, verified by
direct inspection this run (see Domain §5 for full detail) — every existing repo lookup is exact.

**[V] No capability exists to read or fetch content from a git remote that is not already a
locally-open, registered clone**, verified this run (see Domain §6). This is a real gap Correction
2 runs into and is addressed there.

**[V] Existing per-repo override precedent:** `.aiteam/rulebook.md` is an optional, per-repo file
that scanners fall back from when absent (confirmed absent in this repo). Prose-based, consumed by
prompt-reading sub-agents, not by Go code — a weak precedent for a machine-read index override, but
the only "repo may supply its own file" convention that exists.

**[V] Security guardrails any new surface must route through, not bypass:**
`internal/config.forbiddenFlag()` (blocks dangerous flag-equivalents from configuration),
blanket secret-scanning of every config value regardless of key name (`secretscan.go`), and
`config.Codex.AllowUserConfig=false` as existing precedent that a config toggle can silently
change *which files an agent tool discovers*. A feature that fetches an index from a configurable,
possibly-third-party git source and copies files it finds there into a user's repo is a
supply-chain-shaped surface and should be designed with the same scrutiny as these guardrails,
not less.

**[V] New this run — repo-to-worktree data model, re-verified by direct inspection for
Refinement 3:** `store.Repo` (`internal/store/repos.go:14-44`) is a flat row (`Path`,
`Forge/Owner/Name`, `RemoteURL`, `BaseBranch`, `FeaturePrefix`, `WorktreeRoot`, `AiteamTeam`,
timestamps) with **no field or table linking it to other checkouts of the same origin**, and the
schema has no `parent_repo_id`/origin-set column (`internal/store/migrations/0001_init.sql:30-49`).
`UpsertRepo` keys only on `Path` (`internal/store/repos.go:58-92`) — two checkouts of the same
origin register as two fully independent rows. **`store.Session`**
(`internal/store/sessions.go:26-59`), by contrast, links one `RepoID` to one `WorktreePath` per
task/issue — this is a **session-to-worktree** mapping, not a repo-to-all-its-worktrees mapping,
but it is a real, already-populated historical record of every worktree path ever used for a given
`RepoID`, which is closer to reusable substrate than a from-scratch build (see Domain §9).

**[V] New this run — `WorktreeList`'s only production call site:** `internal/git`'s allowlisted
`Clone.WorktreeList` (`internal/git/clone.go:13-61`) is called at exactly one place today,
`internal/git/ensure.go:112`, purely to check whether one *specific target directory* already
exists as a registered worktree before creating it (`EnsureWorktree`'s dedup/reuse check for one
task) — not to enumerate a repo's full worktree set for any reporting, daemon-housekeeping, or
reconciliation purpose. No daemon job, CLI command, or store query anywhere aggregates "every
worktree of repo X" today (Domain §9).

**[V] New this run — no litefactory-owned bookkeeping location exists inside a target repo's
clone, and no `.gitignore`-writing capability exists anywhere in `internal/`**, both verified by
direct inspection for Refinement 4: `internal/repos/aiteam.go` treats `.aiteam/` purely as a
read-only detection marker (`internal/repos/aiteam.go:20-22,85-91,121-134` — stats the directory,
reads for a `team` file, never writes) belonging to a *different, external* tool
(`internal/repos/registration.go:140`, "Nothing is written inside the clone and nothing goes over
the network"). The only `.gitignore` hits anywhere in `internal/` are this tooling repo's own root
`.gitignore` (irrelevant) and a test fixture (`internal/git/worktree_test.go:568`) asserting `git
status` behavior — no production code reads, writes, or manages a *target* repo's `.gitignore`
today. `cmd_repo.go:57-58`'s disclaimer reads identically to how it was cited in the prior pass,
with no narrower carve-out written anywhere for "litefactory may write to path X inside the clone."
This is carried into Domain §10 and Open Questions below as new, unresolved design surface.

**[V] New this run — confirmed full agent roster, for Refinement 5's bootstrap requirement:**
13 files under `.claude/agents/*.md`. Five are Go/backend-specific: `go-architect.md`,
`go-implementation-engineer.md`, `go-code-reviewer.md`, `go-unit-test-engineer.md`,
`go-integration-test-engineer.md`. Eight are generic/cross-language/orchestration:
`codebase-tour.md`, `web-researcher.md`, `requirements-engineer.md`, `proxy-product-owner.md`,
`app-context-updater.md`, `research-orchestrator.md`, `plan-orchestrator.md`,
`implement-orchestrator.md`. See Domain §11.

## Domain / Problem Space

### 1. The corrected index shape

**[R this run]** The index is a pure lookup table, not a config document:

**[R 2026-09-05, revised for Refinement 2]** Each `agents` entry is now an object carrying both a
**source path** (where the markdown actually lives, relative to the index's own git repo root) and
a **destination filename** (what it is written as once imported), rather than a bare filename
string — a source file can have an arbitrary, historically-named path, and land under a different,
specific name in the target repo:

```json
{
  "agents": {
    "requirements-engineer": {
      "source": "teams/shared/agents-mmh.md",
      "dest": "agents.md"
    },
    "go-architect": {
      "source": "teams/go-team/go-architect.md",
      "dest": "go-architect.md"
    },
    "go-implementation-engineer": {
      "source": "teams/go-team/go-impl.md",
      "dest": "go-implementation-engineer.md"
    }
  },
  "shared": ["requirements-engineer"],
  "teams": [
    { "name": "go-team", "agents": ["go-architect", "go-implementation-engineer"] }
  ]
}
```

- `agents` maps a short key to a `{source, dest}` pair. `source` is resolved relative to the index
  source's own git repo root (Domain §2) — this **replaces** the prior pass's unresolved "sibling
  `agents/` directory" guess and directly answers former Open Question 8: paths are explicit per
  entry, not implied by a fixed convention.
- `dest` is the filename the per-harness renderer uses as the canonical, harness-agnostic body name
  for that key before wrapping it in each harness's own frontmatter (Domain §4). **[R this run]
  flagged assumption:** under Domain §4's already-settled design, per-agent output files are named
  by their own agent *key* (`.claude/agents/<key>.md`, `.codex/skills/<key>/SKILL.md`), not by
  `dest` — so in the common case `dest` is redundant with `<key>.md` and mainly documents/stages the
  canonical body name pre-rendering. Whether `dest` should ever be allowed to diverge from
  `<key>.md` in the shipped output (as opposed to just the staging step) is not settled — carried to
  Open Questions.
- `shared` is a list of keys usable by *any* team — not a separate parallel definition, the same
  key as it appears in `agents`.
- `teams` is a list of `{name, agents: [key, ...]}`; a team's effective member list is its own
  `agents` plus the whole `shared` list (**[R this run] assumption, flagged**: shared agents are
  implicitly included in every team rather than requiring each team to list them explicitly —
  the alternative reading, where a team must opt into each shared agent by name, is also
  consistent with the sponsor's wording and should be confirmed before Plan mode locks the schema).
- **[V] Nothing about this JSON is ever written into a target repo.** `team add`/`team update`
  read it, resolve markdown paths from it, and copy/render only the markdown — the index file
  itself never appears inside `.aiteam/`, `.claude/`, `.codex/`, or anywhere else in the target
  repo. This directly reverses the prior brief's `.aiteam/team.json` design, which proposed writing
  a (then config-carrying) JSON document into the target repo as a repo-local override; that design
  is dropped, not adapted.

**[R this run] Open sub-question this run could not settle from the sponsor's one-line example:**
is the index's mapping **one markdown file per agent key, harness-agnostic**, or does the source
repo maintain per-harness variants that the index also needs to address (mirroring this repo's own
current dual-tree reality)? The sponsor's own example (`{"requirements-engineer":
"req-engineer.md"}`) shows exactly one filename per key, with no harness qualifier. Taking that
literally at face value (**[R this run] assumption, most-reasonable reading, flagged**): the index
points to **one harness-agnostic markdown body per agent** (persona + instructions, no frontmatter
or a minimal universal frontmatter), and it is `team add`'s job — not the index's — to render that
single source into each harness's native shape (see Domain §4). This is a materially different
job for `team add` than "copy a file verbatim": it becomes a small per-harness renderer, wrapping
one canonical body in Claude's YAML frontmatter for `.claude/agents/<key>.md` and in Codex's
narrower frontmatter for `.codex/skills/<key>/SKILL.md`. This is carried into Open Questions below
because it changes the shape of `team add`'s implementation non-trivially, and the sponsor's brief
example does not disambiguate it. **Refinement 2's source/dest split does not resolve this
sub-question either way** — it is orthogonal (it fixes *where* a source lives and *what it's called*
once imported, not *how many* variants exist per key).

### 2. The index source is configurable — what already exists to build this on, and what does not

**[R 2026-09-05, revised for Refinement 1] Concrete default source and layout, replacing the prior
pass's generic placeholder:** the default index source is `https://github.com/marioatcloudvane
/litefactory/teams` — litefactory's own public repo, under a `teams/` path. Layout under that path:

```
teams/
  index.json          # [R this run, assumption] the JSON index file itself — exact filename/
                       # path not specified by the sponsor; "index.json" directly under teams/
                       # is the most reasonable reading and is flagged as an assumption (Open
                       # Questions).
  go-team/
    go-architect.md
    go-impl.md
    ...
  python-team/
    ...
  shared/
    agents-mmh.md      # example from Refinement 2 — an arbitrarily-named source file
    ...
```

- Each team gets its own subfolder under `teams/` (e.g. `teams/go-team/`); `teams/shared/` holds
  markdown for agents shared across teams. Source paths in the index (Domain §1) are relative to
  the repo root, so entries read `teams/go-team/go-architect.md`, `teams/shared/agents-mmh.md`,
  etc. — consistent with the worked example above.
- **[R this run] Reconciling this with the prior pass's delivery recommendation, not discarding
  it:** the prior pass recommended shipping a `//go:embed` snapshot as the *default delivery
  mechanism* for v1, specifically because no fetch capability exists yet for a live git source
  (Domain §6). That recommendation and this refinement are not in tension: `https://github.com
  /marioatcloudvane/litefactory/teams` is now the **canonical** default source — the place the
  index conceptually lives and is maintained — while an embedded snapshot of that same `teams/`
  tree, taken at build/release time, remains the pragmatic **v1 delivery mechanism** given the gap
  described below. Live-fetching directly from that URL is the natural fast-follow once Domain §6's
  gap is closed, not a contradiction to resolve now.

**[V] Closest existing precedent:** `config.GitHub.APIBase` (see Codebase Context above) — field +
default constant + URL validation + template line + purity-respecting hand-off. A new setting
(e.g. `team_index: { source: "embedded" | "repo" | "path", ... }`) should mirror this shape rather
than invent a new config idiom.

**[V] What does not exist, verified this run:**
- `internal/git`'s allowed operations (the documented S-6 allowlist, `internal/git/clone.go:13-61`)
  are `Fetch, WorktreeAdd, WorktreeList, WorktreeRemove, RevParse, RemoteURL, ConfigGet, ShowRef` —
  all operating on an **already-open local clone**. There is no clone-from-URL, and no read of a
  single file's content at a ref (no `git show <ref>:<path>` equivalent, no cat-file, no sparse
  checkout) even for an already-registered repo.
- `internal/github`'s client wraps only issue/comment/subissue/user endpoints — no Contents API,
  no raw-file fetch. It also requires a bot token unconditionally (`internal/github/client.go:109-
  111`, "no anonymous mode"), so it could not read an arbitrary public repo's file today without
  authenticating as the bot.
- litefactory has **no existing self-reference** to its own public repo (no hardcoded owner/name,
  no update-check mechanism) — `var version = "dev"` (`cmd/litefactory/main.go:28`) is a build-time
  string only. "Default source = litefactory's own public repo" was a new notion to introduce
  before this run; it is now a concrete URL (`.../marioatcloudvane/litefactory/teams`) but the
  fetch capability to reach it live still needs to be built (see below), whether hardcoded as a
  string constant or exposed as the pre-filled default of the new config field.

**[R this run] Recommendation, given the above — scope the default and the override differently
rather than pretending one mechanism covers both:**
- **Default source: keep it `//go:embed`.** Ship a snapshot of litefactory's own index + shared/
  common markdown baked into the binary at release time. This satisfies "index lives in git" in
  the sense that the embedded snapshot *is* a point-in-time copy of litefactory's own git repo
  (now concretely `.../marioatcloudvane/litefactory/teams`, per Refinement 1), requires zero new
  network capability, and matches the one migration/embedding idiom this codebase already trusts
  (`internal/store/migrate.go`'s `//go:embed migrations/*.sql`).
- **Override to a static filesystem path: cheap, build this first.** No new capability needed
  beyond an `os.ReadFile`+`filepath.Join` — the index and its markdown files are just read from a
  configured local directory instead of the embedded one.
- **Override to "a different repo": split this into two sub-cases, because they cost very
  differently:**
  1. *A repo already registered via `repo add`.* Reuses existing local-clone plumbing, but even
     this needs one small, safe addition to the `internal/git` S-6 allowlist — a read-only
     "file content at ref" operation does not exist today and would have to be added (e.g. `git
     show <ref>:<path>`, or a worktree-based read) before this case works at all.
  2. *An arbitrary, unregistered remote git URL.* This is **new surface with no foundation in the
     codebase today** — either a raw-file HTTP fetch (GitHub Contents API / `raw.githubusercontent
     .com`) or a genuine clone-from-URL capability, neither of which exists. It also opens a real
     supply-chain question (fetching and copying files from a configurable, possibly-untrusted
     remote into a user's working repo) that the existing guardrails (Codebase Context, last
     paragraph) were built to police for other input classes — this needs its own security design,
     not a quick addition.
  **[R this run] recommendation:** ship default (embedded) + static-path override + "already-
  registered-repo" override as the initial scope; treat "arbitrary unregistered remote URL" as an
  explicitly flagged follow-on, not silently absorbed into the same milestone. This satisfies
  Correction 2's letter (configurable, not hardcoded) without pretending a capability exists that
  does not. **Note litefactory's own public repo is itself an "arbitrary remote" from the fetch
  layer's point of view** unless/until it is special-cased — the embedded-snapshot recommendation
  above is precisely what avoids needing that fetch path for v1's default case.

### 3. Per-harness agent definition formats today (external, as of Sep 2026) — carried forward, still relevant to the rendering question in §1/§4

**[W]** No shared schema across harnesses:
- **Claude Code** — Markdown + YAML frontmatter under `.claude/agents/` (`name`, `description`
  required; optional `tools`, `model`, `color`, etc.). A JSON form also exists (`claude --agents`
  flag), but per §1's corrected reading, the index itself carries no such config — this JSON form
  is no longer a design target, only background.
- **Codex CLI** — native format is standalone TOML (`.codex/agents/*.toml`), **not used by this
  repo today**; this repo instead uses the cross-tool Agent Skills convention (`SKILL.md` under
  `.codex/skills/`), which is what `team add` would need to render into. **[W] confidence:
  medium**, this surface moved quickly through 2025–2026.
- **opencode** — Markdown + YAML frontmatter under `.opencode/agents/`, or an embedded JSON
  `"agent"` object in `opencode.json`. Not present in this repo at all today (confirmed absent).

**Implication, restated for the corrected model:** because agent content is staying in markdown
(not a shared JSON schema), the "one shape for every harness" problem from the prior brief is
narrower than previously framed — it is now specifically "how does `team add` turn one
harness-agnostic markdown body into Claude's frontmatter shape and Codex's frontmatter shape,"
not "how does one JSON schema express three harnesses' worth of config fields." This is a smaller,
more mechanical problem, but it is not zero-effort — see Domain §1's flagged open sub-question.

### 4. Team add produces a real per-harness file structure — this is Correction 4's core ask

**[R this run]** For each key in the resolved team (own agents ∪ shared), `team add`:
1. Resolves the key to its markdown source via the index.
2. Writes/renders a per-agent file into each harness's own subfolder that this repo already has a
   convention for: `.claude/agents/<key>.md`, `.codex/skills/<key>/SKILL.md` (and
   `.opencode/agents/<key>.md` if opencode is in scope — see Open Questions). **[V]** Per-agent
   files have **no collision problem** — each is uniquely named by its own agent key, so two
   different teams that both include the same shared key produce byte-identical content in that
   slot; no composition or provenance tagging is needed at the per-agent-file level, only at the
   root-singleton level below.
3. **Contributes a tagged section into `AGENTS.md` and `CLAUDE.md` at the target repo's root** —
   these are singleton files, so two different teams (or a team plus the shared pool) writing into
   the same filename must not blindly overwrite each other.

**[R this run] Recommended composition mechanism for the root singleton files, based on external
precedent (see below) since none exists in this codebase to extend:**

Wrap each contributed section in a named, machine-findable BEGIN/END comment pair, e.g.:

```markdown
<!-- litefactory:team BEGIN team=go-team agent=go-architect -->
... this agent's contributed section ...
<!-- litefactory:team END team=go-team agent=go-architect -->
```

- `team update` finds every BEGIN/END pair, replaces only the spans it owns, and leaves everything
  outside a marked span (hand-written repo content) untouched — this is the same mechanism
  terraform-docs and doctoc use for exactly this problem (see Domain §7, external precedent).
- **[R this run] flagged design decision, not yet settled:** shared agents referenced by more than
  one *installed* team should almost certainly be **deduplicated** in the composed file rather
  than repeated once per installed team — repeating a shared agent's full prompt content once per
  team that includes it would bloat the root file and make later editing ambiguous about which
  copy is "live." Recommendation: one block per shared agent, with the marker listing every
  contributing team (`teams=go-team,py-team agent=requirements-engineer`) rather than one block
  per (team, agent) pair for shared keys specifically. Team-specific (non-shared) agents keep the
  simple one-block-per-(team,agent) form shown above.
- **[R this run] this directly reopens and reverses a prior finding.** The earlier addendum
  (superseded) assumed a single team per repo as "the more conservative reading" because nothing in
  the original request implied otherwise. **Correction 4 explicitly assumes the opposite** — "if
  two different teams both write to `AGENTS.md`/`CLAUDE.md`, content differs per team" only makes
  sense as a real problem if **more than one team can be installed in a repo at once**, or if a
  *second* `team add` for a *different* team is expected to add to, not replace, the first team's
  contributions. This should be stated plainly in Plan mode's scope: **multi-team-per-repo
  installation is now the working assumption**, not a corner case to defer.

### 5. Repo identification for `team add <repo-identifier> <team>` — reconciling two mechanisms

**[V] Re-verified this run, no change from the prior addendum's core facts:** a registered repo is
`store.Repo{ID, Path, Forge, Owner, Name}` (`internal/store/repos.go`), derived from the git
`origin` remote via `git.RepoRef{Forge, Owner, Name}` at `repo add` time
(`internal/repos/registration.go:144-194`, `internal/git/remote.go:24-38`). `Owner` and `Name` are
always derived together from one URL and never split apart (`splitRepoPath`, `internal/git/
remote.go:143-156`, cuts on exactly one `/`). `Repo.Slug()` renders `owner/name`. Only `path` is a
DB-unique column (`internal/store/migrations/0001_init.sql:32`); `(forge, owner, name)` has only a
non-unique index — **two local clones of the same GitHub repo can legitimately collide on
`owner/name`** — this is the same fact Domain §9 leans on below as a possible propagation signal.

**[V] New this run: `Name` is never used alone anywhere in the codebase.** Every existing lookup is
exact and requires the full pair: `GetRepoByPath` (path), `GetRepoBySlug` (exact
`forge,owner,name` triple — and this function, despite being fully implemented, **has zero call
sites anywhere in the codebase today**; it is currently dead code). The one existing "user types a
repo identifier" surface, `--repo owner/name` on `logs` (`internal/cli/cmd_logs.go`, `parseRepoFlag`
~851-862), requires the exact `owner/name` shape and errors on anything else. **No fuzzy, substring,
or prefix matching exists anywhere in this codebase.**

**[V] `ListRepos()`** (`internal/store/repos.go:130-150`, unfiltered, `ORDER BY owner, name, path`)
is the established idiom for "list everything, filter in Go" — already used in `internal/daemon/
tick.go:138`, `internal/daemon/setup.go:223`, `internal/cli/cmd_repo.go:125,193`. Implementing a
substring resolver on top of this needs no new SQL, just a Go-side filter, which is appropriate
given the expected small (single- to low-hundreds) repo count per install.

**[R this run] Reconciliation — these are two different mechanisms serving two different jobs, not
one mechanism the sponsor is redescribing:**
- `logs --repo owner/name` is a **disambiguation flag** for a command whose primary addressing is
  by issue reference; it demands the full, always-unique slug and never guesses.
- `team add <repo-identifier> <team>`'s bare positional word is a **convenience-first fuzzy
  lookup** — the sponsor's own example (`litefactory team add litefactory go-team`) uses just the
  bare repo name ("litefactory"), not `owner/name`. **[R this run] recommendation:** implement this
  as a new resolver (naturally placed in `internal/repos` or a new `internal/team`, not a
  reactivation-in-place of `GetRepoBySlug`) that:
  1. Lists all registered repos.
  2. First tries an **exact `owner/name` match** if the identifier contains a `/` — this
     legitimately resurrects `GetRepoBySlug` as a real call site, since `owner/name` should always
     be accepted as an always-unique, fully-qualified fallback.
  3. Otherwise does a **case-insensitive substring match against `Name` alone** (the sponsor's
     "identifier derived from the repo's git remote name" — read here as the repo-name component
     specifically, not the owner) across all registered repos.
  4. Exactly one match resolves silently. Zero matches is a plain not-found error. **More than one
     match errors and lists every candidate by its full `owner/name` slug**, inviting the user to
     either narrow the substring or supply the full `owner/name` — reusing the same
     `AmbiguousIssueError`-style presentation `logs` already establishes for this class of error
     (`internal/store/errors.go:24-60`, `cmd_logs.go:802-809`), so the UX is consistent across the
     two commands even though the matching strategy differs.
- **[V] This does not require inventing a new CLI dispatch mechanism.** `<repo-identifier>` is an
  ordinary positional argument to the existing `team add` verb (`team add <repo-identifier>
  <team-name>`), not a variable segment spliced into the command name itself — the prior addendum's
  concern about "no `<noun> <identifier> <verb>` shape exists" applied to a different, since-
  discarded phrasing (`teams <repo> <command>`) and does not apply here; an `ArgSpec` with two
  positional slots is all the router needs.

### 6. Fetching from a configurable git source — the concrete gap this design runs into (see also Domain §2)

**[V] Re-stated for emphasis because it is load-bearing for Correction 2:** neither `internal/git`
nor `internal/github` can read a single file's content from an arbitrary or even an already-
registered git remote without new capability being built. This is not a minor detail — "the index
lives in git, and its source is configurable" cannot be fully delivered against an *arbitrary*
remote today without adding either a narrow git allowlist verb (file-at-ref read on an
already-open clone) or a raw-content HTTP fetch path. Domain §2 above scopes an initial delivery
that avoids the harder (arbitrary unregistered URL) case; Plan mode should treat that scoping
decision as one of the first things to make explicit, not an assumption to inherit silently.

### 7. Composing a root singleton file from multiple markdown sources — external precedent

**[W]** No hash-based or source-tracked provenance convention was found in mainstream tooling for
this exact problem; two related, well-established conventions exist and both encode provenance as
**a name/label in the marker text**, not a content hash:
- **terraform-docs** — `<!-- BEGIN_TF_DOCS -->` / `<!-- END_TF_DOCS -->` (customizable delimiter
  text via `.terraform-docs.yml`'s `template` field); content strictly between the pair is replaced
  on each run, content outside is left alone.
- **doctoc** — `<!-- START doctoc generated TOC please keep comment here to allow auto update -->`
  / matching `END` comment, with an inline instruction telling humans not to hand-edit the span and
  to re-run the tool instead.
- **Go's generated-file convention** is a different shape (whole-file scope, not a section within a
  composite file): a single required line matching `^// Code generated .*DO NOT EDIT\.$` before any
  real content, with the generator's name/version as free text — used to decide whether tooling
  (e.g. linters) should skip the file entirely. Useful precedent for *whole* generated files (a
  per-agent `.claude/agents/<key>.md` could plausibly carry an equivalent header) but not for
  composing sections within one file.

**[W] Explicitly not settled:** no mainstream convention embedding a **source-content hash** inside
a composite-file marker (for drift detection) was found within this run's search budget — the
`renderedHash`/`sourceHash` idea from the prior (now-superseded) addendum's lockfile design has no
direct external precedent to borrow from; it would be original design if kept. **[R this run]
recommendation:** keep marker provenance as plain name/label fields (team, agent — following
terraform-docs/doctoc), and keep any hash-based drift detection in a **separate, litefactory-
exclusive lockfile** (not embedded in the marker text itself) that lists, per installed artifact,
which BEGIN/END span or per-agent file it corresponds to and what its content hash was at install
time — this is closer to what the prior brief's lockfile concept was actually good at (drift
detection) once decoupled from the rejected config-rendering model it was originally built to serve.
**This lockfile now needs a concrete home inside the target repo — see Domain §10, which this run
adds specifically because Refinement 4 depends on it.**

### 8. `team manage <agentname> set <param>=<value>` no longer has a clear target — a direct consequence of Correction 1

**[R this run] — the most important second-order finding of this run.** The original request's
item 7 assumed the index carried structured per-harness fields (tools, model, sandbox mode) that a
`set <param>=<value>` command could address and mutate. Correction 1 removes that substrate
entirely — the index maps a key to a filename, nothing more; there is no `param` inside it to set.
This is not a small wording gap; it invalidates the mechanism, not just the syntax, of one of the
request's eight original items. Plausible replacements, none yet chosen:
- Point a key at a **different local markdown file** for this repo only (a per-repo content
  override, e.g. "use my own `go-architect.md` instead of the index's"), which would need its own
  small piece of repo-local state — but per Correction 1's "nothing about JSON is copied," this
  state cannot itself be a copy of the index; it would need to be a new, narrower mechanism.
- An include/exclude toggle for a specific key within an installed team, independent of content.
- Drop the command entirely for this phase and treat editing an agent's markdown as "edit the file
  litefactory already installed, directly" (consistent with per-agent files having no ongoing
  ownership conflict once installed — see Constraints & Risks).
This is carried into Open Questions below rather than resolved here, since choosing among these
changes the shape of `internal/cli/cmd_team.go`'s command surface materially.

### 9. Multi-worktree propagation — new capability, researched fresh this run for Refinement 3

**[R 2026-09-05, new]** litefactory's own repos are not always a single checkout: this very
research run executed inside `/Users/mariomh/.lf-worktrees/litefactory/task-68`, a worktree of a
`litefactory` main checkout elsewhere — the exact shape Refinement 3 is concerned about. `team
update` (and, by extension, a re-run of `team add`) needs its file-level effect to reach every
worktree of the target repo, not only the one the command happened to be invoked from.

**[V] What exists to build this on, verified this run by direct inspection:**
- **No repo-to-worktree data-model link.** `store.Repo` has no field or table representing "this
  is a worktree of that other registered repo," and the schema has no worktree-set column
  (`internal/store/migrations/0001_init.sql:30-49`). Two checkouts of the same origin, if both
  registered, are two fully independent rows.
- **`store.Session.WorktreePath`, however, is a real, already-populated historical record** —
  every session/task links one `RepoID` to the specific `WorktreePath` it ran in
  (`internal/store/sessions.go:26-59`). Querying `WorktreePath` for a given `RepoID` across all
  sessions yields a *candidate set* of worktrees litefactory has itself created for that repo —
  not a from-scratch build, but not a ready-made "current worktrees" answer either, since some of
  those paths may since have been removed (`WorktreeRemove` is a real allowlisted op,
  `internal/cli/cmd_task.go:122`).
- **`Clone.WorktreeList` is allowlisted and functional at the `internal/git` layer**
  (`internal/git/clone.go:13-61`) but its only production call site today,
  `internal/git/ensure.go:112`, uses it only to check whether one specific target directory already
  exists before creating a new worktree — never to enumerate the full current set.
- **No code anywhere aggregates "every worktree of repo X"** for any reporting, daemon-housekeeping,
  or update-propagation purpose today. Verdict: **this is new capability, not existing wiring** —
  the primitive git call exists, the discovery/aggregation layer around it does not.
- **A second, independent signal already exists in the schema and is arguably cheaper to build on:**
  Domain §5 confirms `(forge, owner, name)` is a non-unique index specifically *because* "two local
  clones of the same GitHub repo can legitimately collide" is an anticipated case. `team
  update`/`team add` could query `ListRepos()` filtered to matching `(Forge, Owner, Name)` and apply
  the same file write to every matching row's `Path`, entirely within existing schema — this only
  catches checkouts litefactory has itself registered via `repo add`/session creation, so it is
  complementary to, not a full replacement for, live `git worktree list` ground-truth.

**[R this run] Recommendation:** combine the two signals rather than building either alone as the
sole source of truth — use `sessions.WorktreePath` history (or the `(forge,owner,name)` collision
set) as the *candidate* list, and a live `git worktree list` against the canonical clone as the
*ground truth* filter, discarding paths that no longer exist as live worktrees. This avoids
inventing a new schema relationship as a first move; only add a persisted repo-to-worktree
relationship later if the live-computation approach proves insufficient in practice. This is
flagged as a recommendation, not a locked decision — Plan mode should treat it as the starting
point for a real design, not inherit it silently (see Open Questions).

### 10. Gitignoring managed files, and the bookkeeping-directory carve-out — new this run for Refinement 4

**[R 2026-09-05, new]** Refinement 4's premise is explicit and sequenced: *once* Domain §9's
propagation problem is solved reliably, the generated per-harness/root files no longer need to be
git-tracked in the target repo, because litefactory itself becomes the regenerable source of truth.
This section records what gets gitignored, what must not be, and what is missing to make that safe.

**Concrete paths to gitignore in the target repo, per this repo's own current conventions
(Domain, Codebase Context):**
- Root `CLAUDE.md`
- Root `AGENTS.md`
- `.claude/agents/` (the subfolder `CLAUDE.md`'s composition pulls per-agent files from)
- `.codex/skills/` (the subfolder `AGENTS.md`'s composition pulls from, via this repo's Agent
  Skills convention)
- `.opencode/agents/`, conditionally, only once/if opencode rendering ships (Open Question 10)

**What must explicitly not be gitignored:** a new, litefactory-owned bookkeeping directory holding
whatever tracks installed teams/artifacts (the lockfile/index-cache/drift-detection state
recommended in Domain §7). **[V] Verified this run: no such directory exists anywhere in the
codebase today.** `.aiteam/` is not a candidate — it is explicitly a *different, external* tool's
read-only marker (`internal/repos/aiteam.go`, `internal/repos/registration.go:140`, "Nothing is
written inside the clone"), and litefactory's own `cmd_repo.go:57-58` disclaimer ("litefactory
never installs it and never writes inside the clone") still reads unmodified. **[R this run]
recommendation, flagged as a naming choice for Plan mode, not a settled decision:** introduce a
distinct directory (e.g. `.litefactory/` or `.litefactory-team/`) at the target repo's root, scoped
narrowly to team-install bookkeeping, and treat its creation as an explicit, narrow amendment to the
`cmd_repo.go` disclaimer rather than a silent exception to it.

**[R this run] Important clarification this refinement does not itself resolve:** gitignoring the
generated files does **not** answer the still-open ownership-boundary question (Open Question 3,
carried from the second pass). litefactory still literally writes into the target clone either way
— `.gitignore` only changes whether *git* tracks the result, not whether litefactory writes there.
Domain §10 operates **downstream of**, not as a substitute for, that decision. It should also not
be read as resolving Domain §9 for free: gitignoring is explicitly sequenced *after* reliable
multi-worktree propagation exists, per the sponsor's own framing, because untracked generated files
in one worktree are invisible to git-based inspection of any other worktree — without propagation,
gitignoring would silently create per-worktree drift rather than removing a tracking burden.

### 11. Bootstrapping the first real team — new this run for Refinement 5

**[R 2026-09-05, new]** Refinement 5 requires the first team to actually ship to be a real,
dogfood-able "go-team," not a hypothetical example — built from this repo's own existing
Go-specific agents. **[V] Verified this run, full current roster:** 13 files under
`.claude/agents/*.md`. Five are Go/backend-specific — `go-architect`, `go-implementation-engineer`,
`go-code-reviewer`, `go-unit-test-engineer`, `go-integration-test-engineer` — matching exactly the
specialists this repo's own `implement-orchestrator` already invokes for Go work. The remaining
eight are generic/cross-language/orchestration (`codebase-tour`, `web-researcher`,
`requirements-engineer`, `proxy-product-owner`, `app-context-updater`, and the three
`*-orchestrator` agents) and are natural `shared`-pool candidates rather than go-team-specific
members, though this reading is not sponsor-confirmed (see Open Questions).

Per this run's instruction, **this is recorded as a requirement for Plan mode to turn into a story,
not written as a story here** — see Recommended scope for Plan mode.

## Constraints & Risks

- **[V] Ownership-boundary tension is live again, not resolved.** `cmd_repo.go:54-58` disclaims
  litefactory ever writing inside a registered repo's clone; Correction 4 requires exactly that
  (per-agent files, plus composed root singleton files). The prior brief treated this as settled by
  `plan_brief.md` DEV-1 — that citation is now stale (see Correction Notice) since the design DEV-1
  was written against has itself been rejected. **This decision needs to be made again, explicitly,
  in the next Plan run**, not inherited from a plan built on a discarded model. **[R this run]
  Refinement 4's gitignore behavior does not relax this constraint** — see Domain §10.
- **[V] No fetch capability exists for a configurable git-hosted index source beyond the
  already-registered/local-path case** (Domain §2, §6) — shipping "point at any different repo" as
  stated risks overcommitting to unbuilt capability; scope the first delivery explicitly (embedded
  default + static path + already-registered repo) and flag arbitrary-remote-URL support as a
  follow-on. Litefactory's own concrete default repo/path (Refinement 1) is itself subject to this
  same gap unless served via the embedded-snapshot mechanism recommended in Domain §2.
- **[V] `team manage <agent> set <param>=<value>` has no clear target under the corrected model**
  (Domain §8) — treat this as an open design problem, not a carry-over item to implement as
  originally worded.
- **[V] No composition/provenance tooling precedent exists in this codebase** (Domain §4, §7) — the
  root-singleton-file composition mechanism is new design end to end, informed by external
  precedent (terraform-docs/doctoc marker style) but not modeled on anything already working here.
- **[V] Multi-team-per-repo is now the working assumption** (Domain §4) — this is a materially
  larger design surface than "one team, one lockfile, one set of files" and should be scoped as
  such from the start of the next Plan run, not discovered partway through implementation.
- **[V] `internal/config` purity constraint** still applies to any new config field for the index
  source (`purity_test.go`), same as before.
- **[V] Every config value is secret-scanned today regardless of key name** (`secretscan.go`) — a
  new config surface (index source location, possibly credentials for a private index repo) must
  preserve this, not carve out an allowlist.
- **[W] Harness format churn** — Codex's and opencode's config surfaces were both described as
  moving quickly through 2025–2026 in the prior research pass; a per-harness renderer built against
  today's field names carries real drift risk.
- **[V] `plan_brief.md` and `US-1.md`–`US-14.md` are now stale** relative to this corrected brief.
  They are not edited by this run (explicitly out of scope) but should not be treated as valid
  scope for implementation until a human runs Plan mode again against this corrected brief.
- **[V] New this run — multi-worktree propagation is new capability, not existing wiring**
  (Domain §9): no repo-to-worktree data-model link exists, and `WorktreeList`'s only production
  call site today is a single-path existence check, not enumeration. `team update` cannot be
  assumed to "just work" across worktrees without this being designed and built.
- **[V] New this run — no `.gitignore`-writing capability and no litefactory-owned bookkeeping
  directory exist in the target-repo write path today** (Domain §10) — Refinement 4 requires both,
  and the bookkeeping directory's introduction is itself a new, narrow carve-out against the
  ownership-boundary constraint above, not a way of avoiding that constraint.
- **[V] New this run — Refinement 4 is explicitly sequence-dependent on Refinement 3.** Gitignoring
  generated files before multi-worktree propagation is reliable would silently produce per-worktree
  drift (each worktree's generated files becoming invisible, untracked, and independently stale)
  rather than removing a tracking burden — Plan mode should not schedule these as independent,
  parallelizable stories.

## Open Questions

1. **Does the index map one harness-agnostic markdown file per agent key, or does the source repo
   maintain per-harness variants the index must also address?** (Domain §1.) The sponsor's own
   example implies the former; this run assumed that reading and flagged the consequence (`team
   add` must render, not just copy) but this needs sponsor confirmation before Plan mode commits to
   a renderer design.
2. **Are shared-list agents implicit in every team, or must a team explicitly reference each shared
   key it wants?** (Domain §1.) Assumed implicit this run; changes the schema either way.
3. **Does litefactory take over write-ownership of `AGENTS.md`/`CLAUDE.md`/per-agent harness files
   inside a target repo, overriding `cmd_repo.go`'s existing disclaimer, or does some other tool
   still own that boundary and litefactory only prepares content for it to apply?** This was
   treated as settled by the now-stale `plan_brief.md` DEV-1; it needs a fresh decision against the
   corrected model, not an inherited answer. **[R this run] This same decision now also gates
   Domain §10's bookkeeping-directory carve-out** — resolving it once should cover both.
4. **What is the actual delivery scope for "the index source is configurable"?** — embedded default
   + static path + already-registered repo (buildable now) vs. arbitrary unregistered remote URL
   (needs new fetch capability and its own security review). (Domain §2, §6.)
5. **Can a repo have more than one team installed at once, and if so, how does an installed-team
   registry track that** (for `team status`, `team update`, and for knowing which BEGIN/END spans
   in `AGENTS.md`/`CLAUDE.md` belong to which still-installed team when one is later removed)?
   (Domain §4.) Correction 4's own wording implies yes; this needs to be stated as a firm design
   requirement, not inferred.
6. **What does `team manage` become, now that there is no structured per-agent config to `set`?**
   (Domain §8.) Three candidate replacements were sketched; none chosen.
7. **Is "`team list`" and "`repo list`" the same command under two names, or does the sponsor mean
   two distinct things** (one listing teams/agents found in the configured index source, the other
   listing litefactory's already-registered repos)? Read here as the former being the new
   command and the latter being the existing `repo list` (`internal/cli/cmd_repo.go:72-86`,
   unrelated to this feature) — flagged because the request's wording used both terms
   interchangeably.
8. **Where exactly, relative to the index file, do the markdown files it points to live** (sibling
   directory, path field per entry, fixed convention)? **[R this run] Largely resolved by
   Refinement 2** — each entry now carries an explicit `source` path, resolved relative to the
   index's own git repo root (Domain §1, §2). What remains open is only the index *file's own*
   exact path/filename under `teams/` (assumed `teams/index.json`, not sponsor-confirmed — see
   Open Question 14).
9. **Does a private/access-controlled index source need credential handling**, and if so, does that
   route through existing secret-scanning/storage conventions or need new ones? Only relevant once
   Open Question 4's "arbitrary remote" case is in scope, but worth flagging now so it is not
   discovered late.
10. **opencode support** — still unresolved from the prior research pass; `.opencode/` is confirmed
    absent from this repo, and this run did not re-check `.aiteam/APP_CONTEXT.md`/
    `APP_CONSTRAINTS.md` for a prior scoping decision (out of this run's read boundary). Carried
    forward as still open.
11. **New this run — should repo-to-worktree propagation (Domain §9) be computed live** (querying
    `sessions.WorktreePath` history and/or the `(forge,owner,name)` collision set, filtered against
    a fresh `git worktree list`) **or persisted as a first-class relationship in the schema** (e.g.
    a new `parent_repo_id`/origin-set column)? This run recommends starting with live computation
    and only persisting if that proves insufficient, but this is a recommendation, not a decision.
12. **New this run — what should the new litefactory-owned bookkeeping directory (Domain §10) be
    named, and does introducing it require a formal, narrow amendment to the `cmd_repo.go`
    "never writes inside the clone" disclaimer** (Open Question 3), rather than being treated as a
    side effect of gitignoring the rest?
13. **New this run — can the `dest` field in the revised index schema (Domain §1, Refinement 2)
    ever need to diverge from `<key>.md`/`<key>/SKILL.md`** (the per-harness renderer's own
    established output naming, Domain §4), or is it effectively always redundant with the key once
    the renderer exists? If always redundant, Plan mode may choose to simplify the schema back
    toward a bare string in the common case, with the object form reserved for entries that
    genuinely need a divergent name.
14. **New this run — what is the exact filename/path of the JSON index file itself** under the
    default `https://github.com/marioatcloudvane/litefactory/teams` layout (assumed
    `teams/index.json` this run, not stated by the sponsor)?
15. **New this run — should the go-team's `shared` pool include the repo's already-generic agents**
    (`requirements-engineer`, `proxy-product-owner`, `codebase-tour`, `web-researcher`,
    `app-context-updater`), leaving only the five Go-specific agents as go-team-specific members
    (Domain §11)? Assumed yes this run as the most natural reading of "shared across teams" applied
    to this repo's own roster; needs confirmation before Plan mode's bootstrap story is written.

## Recommended scope for Plan mode

**In scope:**
- Design the index schema exactly as a key→`{source, dest}` map plus `shared`/`teams` lists
  (Domain §1, as revised by Refinement 2), resolving Open Questions 1–2, 13, and 14 before
  finalizing it.
- Design `team add <repo-identifier> <team>`'s fuzzy repo resolver (Domain §5) — substring match
  against the registered repo's `Name`, exact `owner/name` as an always-available fallback,
  ambiguity handling reusing the existing `AmbiguousIssueError` presentation style.
- Design the per-harness rendering step for per-agent files (Domain §3–4) and the BEGIN/END
  marker-based composition mechanism for `AGENTS.md`/`CLAUDE.md` (Domain §4, §7), explicitly
  scoped for multiple installed teams per repo (Open Question 5).
- Explicitly re-decide the ownership-boundary question (Open Question 3) as a first planning
  decision, since it is no longer settled by a stale plan — note this same decision now also gates
  Domain §10's bookkeeping-directory carve-out.
- Scope the index-source configurability delivery narrowly and explicitly (Domain §2, §6, Open
  Question 4): embedded default (sourced from the concrete `https://github.com/marioatcloudvane
  /litefactory/teams` layout per Refinement 1), static path override, already-registered-repo
  override; flag arbitrary remote URL support as an explicit follow-on rather than silently
  building toward it.
- Re-scope or drop `team manage ... set <param>=<value>` (Domain §8, Open Question 6) based on a
  fresh decision, not the original wording.
- **New this pass — design multi-worktree propagation for `team update`/`team add`** (Domain §9):
  combine `sessions.WorktreePath` history and/or the `(forge,owner,name)` collision set as a
  candidate list with a live `git worktree list` as ground truth (recommended starting point, Open
  Question 11); state explicitly whether this needs a new persisted repo-to-worktree relationship
  or can remain computed live.
- **New this pass — design the gitignore/protected-bookkeeping-directory behavior** (Domain §10):
  which exact paths get gitignored (`CLAUDE.md`, `AGENTS.md`, `.claude/agents/`, `.codex/skills/`,
  conditionally `.opencode/agents/`) and the name/location/protection mechanism for litefactory's
  own bookkeeping directory (Open Question 12) — explicitly sequenced *after* multi-worktree
  propagation is reliable, not scheduled as an independent or parallel story.
- **New this pass — include a concrete story that authors the first real team definition: a
  "go-team" sourced from this repo's own existing Go-specific agents** (`go-architect`,
  `go-implementation-engineer`, `go-code-reviewer`, `go-unit-test-engineer`,
  `go-integration-test-engineer`, confirmed via direct inspection this run, Domain §11) as a
  concrete, dogfood-able deliverable populating the default index — not left as a hypothetical
  schema example. Plan mode should resolve Open Question 15 (which agents are `shared` vs.
  go-team-specific) when writing this story.
- New `internal/cli/cmd_team.go`, following the existing one-file-per-command-group,
  `Commands()`-registration convention — this part of the prior brief's guidance remains valid.

**Out of scope for this feature (flag if requested later, do not silently absorb):**
- Fetching an index from an arbitrary, unregistered remote git URL (Domain §2's harder case) —
  defer explicitly unless Plan mode decides otherwise with eyes open to the new capability and
  security review it requires.
- opencode as a fully first-class rendering target, pending Open Question 10.
- Reconciling `plan_brief.md`/`US-1.md`–`US-14.md` against this corrected brief — that is a
  separate, future Plan run's job, not part of producing or accepting this brief.
- Persisting a new repo-to-worktree schema relationship as a first move (Domain §9) — start from
  live computation over existing data (session history, `git worktree list`) and only add schema if
  that proves insufficient.

---

## Superseded material

The 2026-09-04 addendum previously appended to this brief (CLI repo-scoping recommendations built
on a `--repo owner/name` flag, and three sample JSON documents — an embedded fallback carrying
per-harness config, a repo-local `.aiteam/team.json` config override, and a three-hash lockfile) is
**superseded by this rewrite** and has been removed from this document rather than kept alongside
it, per this run's instruction to correct rather than layer. Its factual findings about litefactory's
existing repo-identity mechanism (`owner/name`, `GetRepoBySlug`, the `logs --repo` convention) were
independently re-verified this run and are carried forward above (Domain §5) as still accurate;
its JSON samples and its CLI-flag recommendation were built on the rejected config-carrying index
model and are not carried forward.

**Nothing from the 2026-09-05 refinement pass supersedes anything above it** — this pass is
additive: it revises the index schema's shape (Domain §1) and the default source's concreteness
(Domain §2) in place, and adds three new domain sections (§9–§11) alongside everything the second
pass established.
