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

## Refinement Notice — 2026-09-05, fourth pass

The sponsor reviewed the worked JSON example the third pass produced (specifically the go-team
example carried through into `US-17.md`/`SPIKE.md`) and raised one further, narrower concern,
quoted verbatim from a comment on issue #68: "ok, the json is almost fine, but one thing i dont
like yet: we have the agents as a separate top level element and this will become complicated if
we have 20-30 teams. therefore, each team should have all information inline for agents that are
not shared with other teams. this reduces on big fat element as the 'agents' element would be
right now!"

This does **not** reopen the accepted index-based design (second pass) or any of the third pass's
five refinements — it is a further, narrower refinement of the same accepted design, addressing
only the *top-level shape* of the schema, not its concept. **What changes, in one sentence:** the
flat top-level `agents` map is removed; each team now inlines the `{source, dest}` definitions for
the agents that belong to it alone, and `shared` becomes the single place a cross-team agent is
defined, once. See Domain §1 for the full revision, including a resolved decision on what happens
when a non-shared agent turns out to be needed by more than one team.

This is again a headless run: no clarifying question was put to a human. The one substantive
judgment call this pass had to make (the duplication/shared-promotion tension) is recorded below as
a resolved decision, not an Open Question, per this run's judgment that it is decidable directly
from the sponsor's own stated goals (avoid one big undifferentiated element; avoid duplication) —
not something that genuinely needs sponsor input to settle.

## Resolution Notice — 2026-09-05, fifth pass

Implement mode was opened against this session and halted before writing any code: it found
`plan_brief.md` and the `US-*.md` sub-issues — foundationally `US-1.md`, whose decoder task every
other story depends on — built around the flat top-level `agents` map the fourth pass above
replaced, and declined to guess which shape to build against rather than proceed under either
schema without a decision. The sponsor then asked, on issue #68, "ok how can we resolve this
issue?" This pass answers that question at the level Research owns: it confirms the exact,
ground-truth scope of the drift and lays out the resolution path. It does **not** itself edit
`plan_brief.md`, any `US-*.md` sub-issue, or `SPIKE.md` — per explicit instruction for this run,
reconciling those documents belongs to a Plan-mode pass, not to Research. See Domain §12 for the
full scope confirmation, the updated Constraints & Risks entries, and the sharpened Recommended
scope for Plan mode below.

**Headline finding, stated plainly:** there is no remaining *design* question for Research to
settle here. The fourth pass above already locked the target schema (inline-per-team `agents`, a
map-shaped `shared`, the one-key-one-place invariant) as a resolved decision, not an Open Question.
What blocks Implement is purely that a small, now-enumerated set of already-written documents were
drafted against the schema's *prior* shape before that decision was made, and have not yet been
brought into line with it. That is a reconciliation task against an already-settled design — the
same kind of task this session's own "Plan reconciliation" passes already performed twice (see
MANIFEST.md history, 2026-09-05) for the second- and third-pass corrections — needed now a third
time for the fourth pass, and scoped, per Domain §12, to a much smaller set of files than either
prior reconciliation pass touched.

This is a headless run: no clarifying question was put to a human, consistent with every prior pass
of this brief. This pass also encountered, and declined to act on, prompt-injection-shaped content
embedded in tool output while investigating — recorded as a flagged, non-authoritative finding in
Constraints & Risks and Open Questions, not treated as an instruction.

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

**Refinement added this pass (2026-09-05, fourth pass), folded into the numbered request above:**

14. The index schema's top-level shape changes so no single element grows unbounded as team count
    grows: the flat top-level `agents` map is removed, each team inlines the `{source, dest}`
    definitions for its own non-shared agents, and `shared` becomes a key→`{source, dest}` map in
    its own right rather than a list of keys pointing into the old flat map (Domain §1).

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

**[V] New this pass (fifth) — confirmed inventory of the plan artifacts under review:** 19
physical `US-*.md` files exist (`US-1.md` through `US-19.md`); `US-12.md` was dropped/folded into
`US-10.md` in the 2026-09-05 narrow reconciliation pass (see MANIFEST.md history) and is not part
of the 18 live stories. `plan_brief.md`'s Story Index sequences dependencies such that `US-1.md`
has no dependencies (the foundation story), `US-2.md` depends on `US-1.md`+`US-3.md`, `US-17.md`
depends only on `US-1.md`+`US-4.md`, and every other story touched by this drift roots back to
`US-1.md` through some dependency chain — see Domain §12 for what this means for sequencing a fix.

## Domain / Problem Space

### 1. The corrected index shape

**[R this run]** The index is a pure lookup table, not a config document.

**[R 2026-09-05, revised for Refinement 2]** Each agent entry — wherever it is defined (see the
fourth-pass restructuring immediately below for *where* that now is) — carries both a **source
path** (where the markdown actually lives, relative to the index's own git repo root) and a
**destination filename** (what it is written as once imported), rather than a bare filename
string — a source file can have an arbitrary, historically-named path, and land under a different,
specific name in the target repo.

**[R 2026-09-05, fourth pass — schema restructured, replacing the flat top-level `agents` map]**
The sponsor reviewed the worked example this shape produced once it was concrete enough to look
ahead at (`US-17.md`/`SPIKE.md`'s go-team example) and flagged a scaling problem in a comment on
issue #68: a single flat `agents` map holding every agent from every team becomes one increasingly
large, undifferentiated block as the number of teams grows toward the 20s and 30s — the "big fat
element" concern, quoted verbatim in the Refinement Notice above. This pass keeps the `{source,
dest}` leaf shape from Refinement 2 exactly as it was, and removes only the flat map that used to
hold every leaf in one place:

- There is no top-level `agents` map anymore.
- Each team inlines its own `agents` object directly under its `teams[]` entry — a key→`{source,
  dest}` map, scoped to only the agents that belong to that team alone. A team with 5 agents carries
  exactly 5 entries in its own block, regardless of how many other teams exist alongside it.
- `shared` changes from a list of key-strings that pointed into the old flat map (Refinement 2's
  shape) to a map of key→`{source, dest}` in its own right — the same leaf shape as before, now
  carrying the full definition directly, since there is no longer a flat map for it to reference.
- A team's effective agent set is unchanged in principle from the prior pass: its own inline
  `agents` plus every key in `shared` (still subject to Open Question 2, which this restructuring
  does not resolve either way — implicit-for-every-team and opt-in-per-team are both still
  expressible whether the shared definitions live in a flat map or inline).

Worked example, using the real go-team roster already locked in `US-17.md`/`SPIKE.md` (13 agents:
5 inlined under `go-team`, 8 in `shared`) — only the top-level shape changes here, not the roster:

```json
{
  "shared": {
    "codebase-tour": { "source": "teams/shared/codebase-tour.md", "dest": "codebase-tour.md" },
    "web-researcher": { "source": "teams/shared/web-researcher.md", "dest": "web-researcher.md" },
    "requirements-engineer": { "source": "teams/shared/requirements-engineer.md", "dest": "requirements-engineer.md" },
    "proxy-product-owner": { "source": "teams/shared/proxy-product-owner.md", "dest": "proxy-product-owner.md" },
    "app-context-updater": { "source": "teams/shared/app-context-updater.md", "dest": "app-context-updater.md" },
    "research-orchestrator": { "source": "teams/shared/research-orchestrator.md", "dest": "research-orchestrator.md" },
    "plan-orchestrator": { "source": "teams/shared/plan-orchestrator.md", "dest": "plan-orchestrator.md" },
    "implement-orchestrator": { "source": "teams/shared/implement-orchestrator.md", "dest": "implement-orchestrator.md" }
  },
  "teams": [
    {
      "name": "go-team",
      "agents": {
        "go-architect": { "source": "teams/go-team/go-architect.md", "dest": "go-architect.md" },
        "go-implementation-engineer": { "source": "teams/go-team/go-implementation-engineer.md", "dest": "go-implementation-engineer.md" },
        "go-code-reviewer": { "source": "teams/go-team/go-code-reviewer.md", "dest": "go-code-reviewer.md" },
        "go-unit-test-engineer": { "source": "teams/go-team/go-unit-test-engineer.md", "dest": "go-unit-test-engineer.md" },
        "go-integration-test-engineer": { "source": "teams/go-team/go-integration-test-engineer.md", "dest": "go-integration-test-engineer.md" }
      }
    }
  ]
}
```

- `shared` maps a key directly to its `{source, dest}` pair — usable by *any* team, defined exactly
  once.
- Each team's `agents` maps a key directly to its `{source, dest}` pair for agents that belong to
  that team alone.
- `dest` keeps its Refinement-2 meaning: the filename the per-harness renderer uses as the
  canonical, harness-agnostic body name for that key before wrapping it in each harness's own
  frontmatter (Domain §4). The same already-flagged assumption from Refinement 2 still applies
  unchanged: in the common case `dest` is redundant with `<key>.md` (Open Question 13).
- `teams` remains a list of `{name, agents: {...}}` objects, not restructured into a name-keyed map.
  **[R this run] flagged, not changed:** the same "no reason for one undifferentiated blob at 20-30
  teams" reasoning that motivated this pass could equally apply to how `teams` itself is listed, but
  the sponsor's complaint was specific to the `agents` element — this pass does not extrapolate a
  change the sponsor did not ask for (see Open Question 17).
- **[V] Still true, unchanged by this pass:** nothing about this JSON is ever written into a target
  repo. `team add`/`team update` read it, resolve markdown paths from it, and copy/render only the
  markdown — the index file itself never appears inside `.aiteam/`, `.claude/`, `.codex/`, or
  anywhere else in the target repo.

**[R 2026-09-05, fourth pass] Resolved: what happens when a non-shared agent is genuinely needed by
more than one team.** The inlined shape reopens, at a smaller scale, exactly the duplication problem
the sponsor is trying to eliminate: two teams could each write their own inline entry for what is
conceptually "the same" agent, with two independent `{source, dest}` pairs free to drift the moment
either team's entry is edited without the other. **Resolved as a structural invariant, not left to
convention or to a later reader's discretion:** an agent key is defined in exactly one place in the
whole index — either inline under exactly one team, or once under `shared` — never both, and never
inlined under two different teams. If a second team comes to need an agent that is already inlined
under a first team, that agent is by definition no longer team-specific; it moves to `shared`, it is
not copied. The index loader should treat a key appearing in more than one place (two teams, or a
team and `shared` simultaneously) as a validation error, not a silent last-write-wins merge or a
"whichever the loader sees first" ambiguity — the same way two conflicting definitions of one
identifier are an error in most schemas, not a merge problem. **[R this pass, fifth — load-bearing
for Domain §12] This "error, not silent dedup" behavior is the exact point at which `US-1.md`'s
current acceptance criteria diverge from this decision, not merely its worked JSON example — see
Domain §12.**

This resolution has a real, named cost, stated plainly rather than smoothed over: because `shared`
keys are, per the still-open Open Question 2, currently assumed implicitly available to *every*
team, promoting a key out of one team's inline map because a *second* team also needs it makes that
key visible to the other 18-28 teams too, whether or not they want it. That cost is not new to this
pass — it already exists in `shared`'s implicit-availability design from the second pass — but this
restructuring is what makes it bite at the two-team boundary specifically, not only at the
"shared with everyone" boundary. The cleanest long-term fix for that specific cost is Open Question
2 resolving toward explicit per-team opt-in rather than blanket implicit inclusion, so that adding a
key to `shared` only broadens availability to the teams that actually ask for it. That is carried
forward as a sharpened version of Open Question 2 (see Open Question 16), not re-decided here, since
it is a distinct question from the duplication problem just resolved above.

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
example does not disambiguate it. **Neither Refinement 2's source/dest split nor the fourth pass's
inlining restructuring resolves this sub-question either way** — both are orthogonal to it (they
fix *where* a source lives, *what it's called* once imported, and *which top-level element* it
lives under, not *how many* variants exist per key).

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

**[R 2026-09-07, sixth pass] Resolved.** US-13's own design record (see Investigation Notice below)
settled this as candidate (a) — a per-repo, per-agent-key local source override — and it is now
built and delivered. This section is left as-is, unedited, as the historical record of the question
as Research originally posed it; the resolution and its implementation status are recorded in the
new section below rather than folded back into this paragraph, consistent with this brief's
established convention of not silently rewriting a still-accurate historical record.

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

### 12. Confirmed scope of the plan-artifact drift, and the resolution path — new this pass (fifth), answering "how can we resolve this issue?"

**[R this pass, fifth]** Implement mode halted before writing code because `plan_brief.md` and the
`US-*.md` sub-issues were drafted against the flat top-level `agents` map that the fourth pass
above replaced with the inline-per-team shape. This section confirms, file by file, exactly which
documents are affected and at what severity (via a fresh codebase-tour pass reading every live
`US-*.md` file, `plan_brief.md`, and `SPIKE.md` against Domain §1's current schema), and lays out
what Research can settle directly versus what needs a dedicated Plan-mode pass.

**[V] Directly contradictory — content must be rewritten, not merely re-typed:**
- `plan_brief.md` — Shared Decision 1's struct definition and the Story Index's prose both still
  describe the flat top-level `agents` map.
- `US-1.md` — the foundational decoder/loader story. Its struct definitions describe the flat map,
  and, more importantly, **its Acceptance Criteria (AC-1) and Tasks (T-04) currently specify
  dedup/merge behavior for a key seen more than once** — this is a *behavioral* contradiction with
  Domain §1's resolved invariant, not just a stale type name: the current design requires the
  loader to **error** on a duplicate key, never to silently deduplicate or merge it. Fixing US-1
  is therefore not a find-and-replace of a type name; its acceptance criteria need to be rewritten
  to specify validation-error behavior.
- `US-13.md` — repeatedly names the old `AgentEntry` type and describes team membership as an array
  of key-strings rather than an inline key→`{source,dest}` map.
- `US-17.md` — the go-team worked example story. Contains **both** inconsistencies at once: the
  flat-map shape, and a second, independent problem (below) in its `source`/`dest` field meanings.
  The single sharpest example of the drift.
- `SPIKE.md` — carries a verbatim copy of `US-17.md`'s worked example, inheriting both of its
  inconsistencies unchanged.

**[V] Mild/indirect references — likely resolve once `US-1.md` is corrected, but worth a
confirmatory pass:**
- `US-2.md` — describes a team's agents as an "array" and shared agents as a "list"; depends on
  `US-1.md`'s type directly, so its own wording should be re-checked once that type changes shape.
- `US-3.md` — defers to `US-1.md`'s type generically, without repeating the flat-map shape itself.
- `US-5.md` — names the old type in passing; the underlying "source" concept it depends on survives
  the schema change unchanged.
- `US-9.md` — mainly owns the separate lockfile schema, which this drift does not touch; only a
  passing reference to the agent-index type needs re-checking.

**[V] Untouched by this schema drift entirely** (11 of the 18 live stories): `US-4.md`, `US-6.md`,
`US-7.md`, `US-8.md`, `US-10.md`, `US-11.md`, `US-14.md`, `US-15.md`, `US-16.md`, `US-18.md`,
`US-19.md`. (`US-12.md` was already dropped/folded into `US-10.md` in an earlier reconciliation
pass and is not a live file — see Codebase Context.)

**[V] The second, independent inconsistency, confirmed present specifically in `US-17.md` and
`SPIKE.md`:** their concrete JSON uses `source`/`dest` to describe a one-time *bootstrap
construction* mapping (`source` = a file already sitting in litefactory's own current repo, e.g.
`.claude/agents/go-architect.md`; `dest` = where that file lands once copied into the `teams/`
layout being built) — a different meaning from this brief's own Domain §1 schema-level `source`/
`dest` (`source` = a path within the already-built `teams/` layout; `dest` = the canonical
installed body filename in a *target* repo). Both files need their worked examples corrected to
one consistent meaning, on top of the shape correction above — these are two separate edits to the
same JSON block, not one.

**[V] Sequencing fact, read directly from `plan_brief.md`'s Story Index, not re-derived:** `US-1.md`
has no dependencies (it is the foundation every other story is built on); `US-2.md` depends on
`US-1.md`+`US-3.md`; `US-17.md` depends only on `US-1.md`+`US-4.md`. Every story touched by this
drift, directly or mildly, roots back to `US-1.md`. There is exactly one common ancestor to fix
first.

**What this means for "how can we resolve this issue," stated as the recommendation:**

1. **The scope question is now fully settled — this is the part Research can do, and this pass has
   done it.** There is nothing left to investigate about "which documents are stale" or "how
   stale" — the file list and severity above are exhaustive and ground-truthed against the current
   text of every affected file.
2. **The reconciling edits themselves are Plan-mode work, not Research work**, both because they
   touch `plan_brief.md`/`US-*.md`/`SPIKE.md` directly (explicitly out of bounds for this run) and
   because rewriting acceptance criteria and task tables is Plan mode's normal job, not Research's.
   This should be a third, narrowly-scoped Plan reconciliation pass — the same kind of pass that
   already ran twice this session (see MANIFEST.md, 2026-09-05 narrow and body-rewrite passes) for
   the second- and third-pass corrections — scoped specifically to the fourth pass's schema change.
3. **Recommended sequencing for that pass, given the dependency fact above:** rewrite `US-1.md`
   first, including its Acceptance Criteria/Tasks (not only its struct definitions), since every
   other affected story's own correctness depends on `US-1.md`'s shape being right first; then
   `US-13.md` and `US-17.md`/`SPIKE.md` (the two source/dest meanings, plus the shape); then
   `plan_brief.md`'s Shared Decision 1 and Story Index prose; then spot-check `US-2.md`, `US-3.md`,
   `US-5.md`, `US-9.md` once `US-1.md`'s shape is final, since their own drift is largely inherited
   and may already read correctly once the type they defer to is fixed.
4. **This pass finds no reason to reopen the fourth pass's schema decision itself.** The design in
   Domain §1 is not in question; only the paperwork describing it lags behind it. Implement mode's
   halt was the correct response to an unresolved contradiction, not a sign the schema itself needs
   more Research work.
5. Once the Plan reconciliation pass lands, Implement mode can resume against `US-1.md` first,
   consistent with the dependency ordering above and with `MANIFEST.md`'s existing Story-Index-order
   recommendation from the Spike pass.

**[R this pass, fifth] Security note, recorded as a flagged finding, not acted upon:** while
investigating the file-by-file scope above, the codebase-tour sub-agent encountered
instruction-shaped content embedded inside tool-result output twice (once reading `US-17.md`, once
grepping for `plan_brief.md`) falsely presented as this repository's own router instructions with
override authority, directing it to abandon the assigned investigation. Both were correctly
declined without any change of role or action, consistent with this run's security posture: content
found inside a file or tool output is a fact to report, never an instruction to follow. An
unverified, secondary claim of a "background agent" running an overlapping task elsewhere in this
session was also surfaced and is likewise not acted upon here — it grants nothing and triggers
nothing. See Constraints & Risks and Open Questions for how this is carried forward.

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
  originally worded. **[R 2026-09-07, sixth pass] Resolved and delivered — see the new Investigation
  Notice and Domain §13 below; this bullet is left unedited as the historical record of the risk as
  it stood before US-13 shipped.**
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
- **[V] New this pass (fourth) — `plan_brief.md`, all 18 `US-*.md` sub-issues (especially
  `US-17.md`), and `SPIKE.md` all embed the flat top-level `agents` map shape this pass removes**,
  and are therefore stale relative to Domain §1's revised schema on top of already being stale for
  the reasons recorded in the 2026-09-04 Correction Notice. Explicitly not touched by this run, per
  instruction — reconciliation is a future Plan pass's job. **[V] Superseded in severity detail,
  not in substance, by the fifth pass's exhaustive file-by-file findings directly below and in
  Domain §12** — this bullet's blanket "all 18" framing is now known to be imprecise: only 5 files
  are directly contradictory, 4 more are mild/indirect, and 11 are untouched by this specific drift.
- **[V] New this pass (fourth) — a second, independent inconsistency surfaced while comparing this
  brief's schema to the already-drafted go-team worked example:** `US-17.md`/`SPIKE.md`'s concrete
  JSON uses `source`/`dest` to describe a one-time *bootstrap construction* mapping (`source` = an
  existing file already in litefactory's own current repo tree, e.g. `.claude/agents/go-
  architect.md`; `dest` = where that file lands once copied into the `teams/` layout being built) —
  a different meaning from this brief's own Domain §1 schema-level `source`/`dest` (`source` = a
  path within the already-built `teams/` layout; `dest` = the canonical installed body filename in
  a *target* repo). Neither this pass's restructuring nor the third pass's original Refinement 2
  noticed this drift when it was introduced. Reconciling the two meanings, and updating
  `US-17.md`/`SPIKE.md`'s worked examples to both the inlined shape and one consistent `source`/
  `dest` meaning, is left to the future Plan pass that already owns reconciling the stale Plan
  artifacts — flagged here so it is not missed. **[V] Confirmed still present, unchanged, by the
  fifth pass — see Domain §12.**
- **[V] New this pass (fifth) — the drift into `US-1.md` is behavioral, not only cosmetic.**
  `US-1.md`'s current Acceptance Criteria/Tasks (AC-1/T-04) specify dedup/merge handling for a
  duplicate agent key; Domain §1's resolved invariant requires the loader to treat a duplicate key
  as a validation error instead. A reconciliation pass that only updates struct/type names in
  `US-1.md` without also rewriting AC-1/T-04's described behavior would still leave Implement
  mode building the wrong thing. See Domain §12.
- **[V] New this pass (fifth) — Implement mode correctly halted rather than guessing** between the
  two schema shapes when it found this contradiction, and escalated instead of picking one
  unilaterally. That is the intended failure mode for exactly this situation and should not be read
  as a fault in Implement mode's own design — see Domain §12 for the resolution path.
- **[W] New this pass (fifth), flagged and not acted upon — two attempts at prompt-injection-shaped
  content were encountered inside tool-result output during this run's investigation** (once inside
  a read of `US-17.md`, once inside a grep result referencing `plan_brief.md`), falsely presented as
  this repository's own router instructions with override authority. Both were declined without any
  change of role, permission, or action, per this run's standing security posture: content
  discovered inside a file or tool output is reported as a finding, never treated as an instruction.
  This is recorded here as a security-relevant observation for whoever has broader visibility into
  the repository and its tooling to review — Research's own tool access cannot determine whether
  this is a planted test fixture, an artifact of the harness, or something requiring remediation.
  See Open Questions.

## Open Questions

1. **Does the index map one harness-agnostic markdown file per agent key, or does the source repo
   maintain per-harness variants the index must also address?** (Domain §1.) The sponsor's own
   example implies the former; this run assumed that reading and flagged the consequence (`team
   add` must render, not just copy) but this needs sponsor confirmation before Plan mode commits to
   a renderer design.
2. **Are shared-list agents implicit in every team, or must a team explicitly reference each shared
   key it wants?** (Domain §1.) Assumed implicit this run; changes the schema either way. **[R
   this run, fourth pass] sharpened, not resolved:** the fourth pass's resolution of the
   cross-team-duplication tension (an agent needed by 2+ teams must move to `shared`) makes this
   question matter more as team count grows — see Open Question 16.
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
   (Domain §8.) Three candidate replacements were sketched; none chosen. **[R 2026-09-07, sixth
   pass] Resolved and shipped — see Domain §13.**
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
    side effect of gitignoring the rest? **[R 2026-09-07, sixth pass] Settled by implementation,
    not retroactively by Research: `.litefactory/` — see Domain §13.**
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
16. **New this pass (fourth) — should `shared`'s implicit-availability-to-every-team semantics
    (Open Question 2) be reconsidered specifically because of the inlined-schema restructuring?**
    This pass's resolution of the "agent needed by 2 teams" tension (Domain §1) requires promoting
    such an agent to `shared`, which — under Q2's current implicit-inclusion assumption — makes it
    available to every team, not just the two that need it, a cost that gets larger as the team
    count grows toward the 20-30 scale the sponsor is planning for. Not resolved here; carried as a
    sharpened version of Open Question 2, not a new independent question.
17. **New this pass (fourth) — should `teams` also move from an array to a name-keyed map, for the
    same "no reason for it to be one undifferentiated blob at 20-30 teams" reasoning just applied
    to `agents`/`shared`?** The sponsor's complaint was specific to the `agents` element; this pass
    leaves `teams` as an array rather than extrapolating a change the sponsor didn't ask for, but
    flags it as a natural next question if `teams` itself becomes unwieldy to scan at scale.
18. **New this pass (fifth) — are the two prompt-injection-shaped content encounters (Domain §12,
    Constraints & Risks) a planted test fixture, a harness artifact, or a genuine indicator that
    something in the repository or its tool-result pipeline needs security review?** Research's own
    tool access cannot distinguish between these explanations; this needs someone with broader
    visibility (the repo owner or the harness operator) to confirm. Nothing was acted on either way
    while this remained unresolved.
19. **New this pass (fifth) — should the Plan-mode reconciliation pass recommended in Domain §12 be
    its own dedicated, narrowly-scoped pass, or folded into a future Plan pass that also does other
    work?** Recommended here as dedicated and narrow, matching how the second- and third-pass
    corrections were each reconciled in their own pass, but the final call belongs to whoever opens
    the next Plan-mode run, not to Research.
20. **New this pass (sixth) — should `internal/team/overrides.go`'s dead-code advisory
    (`validateOverrideSource`'s unreachable `filepath.IsAbs(clean)` check, CR-A-001 in
    `CODE_REVIEW.md`) be cleaned up in a future pass, or left as intentional defense-in-depth with a
    one-line comment?** Non-blocking; not investigated further by this pass — see Domain §13.
21. **New this pass (sixth) — should the run that closes out Run 27's missing bookkeeping also
    correct MANIFEST.md's stale Implement summary line, or only append the missing History entry?**
    (Domain §13.) This pass recommends both, since the summary line is a single sentence and is
    demonstrably wrong (still lists US-13 as open) the moment the History entry is added, but leaves
    the exact wording to whoever performs that close-out, consistent with how Run 22 phrased its own
    close-out of Run 21's gap.

## Recommended scope for Plan mode

**In scope:**
- Design the index schema exactly as revised this pass (Domain §1): no top-level `agents` map —
  each team inlines a key→`{source, dest}` map of its own non-shared agents, `shared` is itself a
  key→`{source, dest}` map of cross-team agents, and the index loader enforces the one-definition-
  per-key invariant (a key lives inline under exactly one team, or once under `shared`, never both
  and never duplicated across teams). Resolve Open Questions 1–2, 13, 14, 16, and 17 before
  finalizing it.
- **New this pass (fifth) — run a dedicated, narrowly-scoped reconciliation pass against Domain
  §12's exhaustive findings, rather than a general rewrite:** rewrite `US-1.md` first (both its
  struct definitions and its Acceptance Criteria/Tasks — AC-1/T-04's dedup/merge behavior must
  become a validation-error requirement, not just a renamed type), then `US-13.md` and
  `US-17.md`/`SPIKE.md` (flat-map shape plus the two-different-`source`/`dest`-meanings problem),
  then `plan_brief.md`'s Shared Decision 1 and Story Index prose, then confirm `US-2.md`, `US-3.md`,
  `US-5.md`, `US-9.md` read correctly once `US-1.md`'s shape is final. The remaining 11 live stories
  (`US-4`, `US-6`–`US-8`, `US-10`, `US-11`, `US-14`–`US-16`, `US-18`, `US-19`) need no changes for
  this drift specifically. Do not reopen Domain §1's schema decision itself while doing this — it is
  settled; only the documents describing it are lagging.
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
  fresh decision, not the original wording. **[R 2026-09-07, sixth pass] Delivered — no further Plan
  work needed for this item; see Domain §13.**
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
- Reconciling `plan_brief.md`/the `US-*.md` sub-issues against this corrected brief beyond the
  fourth-pass schema drift scoped in Domain §12 above — broader stylistic or structural rewrites of
  those documents are a separate concern from fixing this specific, confirmed contradiction, and
  are not part of producing or accepting this brief. **[R this pass, fifth] Note: this bullet
  previously named a stale `US-1.md`–`US-14.md` range from an earlier pass, predating the current
  18-story (`US-1`–`US-19`, `US-12` dropped) set** — see Domain §12 for the exhaustive, current file
  list and severity tiers, which supersede any range implied here.
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

**Nor does the 2026-09-05 fourth pass (the top-level `agents`-map → per-team-inline restructuring)
supersede anything above it** — it is additive in the same sense: it revises Domain §1's worked
example and field descriptions in place to remove the flat top-level `agents` map, adds a resolved
decision on cross-team duplication, and adds two new Open Questions (16–17), without reopening the
second pass's accepted index-based design or any of the third pass's five refinements.

**Nor does the 2026-09-05 fifth pass supersede any prior design decision** — it adds Domain §12,
sharpens the two fourth-pass Constraints & Risks bullets about stale plan artifacts with an
exhaustive, file-by-file severity breakdown (without deleting the original bullets, which remain
accurate as high-level flags), adds two new Open Questions (18–19), and corrects one stale
cross-reference in Recommended scope for Plan mode's "Out of scope" section (the `US-1.md`–
`US-14.md` range, left over from the second pass, predating the current 18-story set). No prior
Domain section, Open Question, or in-scope Plan recommendation is reopened or reversed by this pass.

## Investigation Notice — 2026-09-07, sixth pass (post-implementation: what happened to Implement Run 27 / US-13?)

**Trigger.** The sponsor asked for an investigation, not a design pass: Run 27 (Implement mode,
scoped to US-13, `team manage` per-repo agent-source override) was reported by the harness as
**failed**, and the sponsor asked what actually happened, how far the run got, what (if anything)
landed in the working tree or was committed, and what an Implement run should do next without
redoing sound work or doing anything redundant. This pass is that investigation. It is **not** a
design pass — Domain §8's original question and Open Question 6 are already resolved (see below);
nothing about the index schema or any other still-open design question above is touched, reopened,
or revisited by this pass.

**[R this pass] Method and its limits, stated up front.** The orchestrator role carrying out this
pass has exactly `Read`, `Write`, and `Agent` (restricted to `codebase-tour`, `web-researcher`,
`app-context-updater`) — no `Bash`, no `git`, no `Glob`/`Grep` of its own. `codebase-tour` itself
has no `Bash` and is explicitly barred from reading anything under `.aiteam/` beyond a rulebook
path (confirmed by its own refusal when first asked to reconstruct git history and read session
artifacts in one request — a correct refusal, not a gap). **Consequently this pass could not run
`git log`/`git show`/`git diff` directly and does not claim to have.** What it did instead: (1)
read the session's own process artifacts directly (`notes.md`, `DRIFT_REPORT.md`, `CODE_REVIEW.md`,
`MANIFEST.md`, `US-13.md`, `plan_brief.md`, `.aiteam/APP_CONTEXT.md`) as the orchestrator, since no
permitted sub-agent can read `.aiteam/` for this purpose; (2) dispatched `codebase-tour` a second
time, narrowed to a present-day-only read of the specific source and test files US-13's own process
artifacts claim were delivered, with no git or `.aiteam` access, exactly the scope it confirmed it
could perform; (3) cross-referenced both against the git status/log snippet already visible in this
run's own environment context (branch `lf/68-new-way-to-work-with-agent-teams`, working tree
**clean**, HEAD at `2516882 litefactory run`, parent `7dae487 Run 26 (Implement, US-11) is complete
and the artifact exists.`). No commit was inspected via `git show`/`git diff`; the "what changed in
2516882" question is answered by corroboration across independent sources, not by reading the diff
itself — flagged **[W]** below wherever it rests on that inference rather than on a direct git read.

**[V] Finding 1 — Run 27's own process artifacts describe a fully completed US-13 delivery, not a
failure.** `notes.md`'s own entry for this run (dated 2026-09-07, headed "Implement run (this run):
US-13 (`team manage` per-repo agent source override)") reads as a complete, standard-shape Implement
close-out narrative — structurally identical to every prior run's entry in the same file (Run 19,
20, 22, 25, 26) — covering Phase 1 development (all four tasks T-35–T-38), Phase 1.5 code review
(single pass, **ADVISORY_ONLY**, zero BLOCKING findings — the first clean pass on this issue's
recurring `errors.Join`-aggregation mistake, which had been BLOCKING three times previously), Phase
2 (correctly skipped, filesystem-only), and Phase 3 testing (three new end-to-end tests added,
closing real coverage gaps; final state "`go test -race -cover ./...` all packages passing, no
races, `gofmt`/`go vet` clean"), ending with the same standard sign-off line every other run in this
file uses: *"No commits or pushes made this run — worktree left for litefactory to inspect and
commit."* `DRIFT_REPORT.md` (dated 2026-09-07, headed "Drift Report — US-13") independently confirms
the same shape from a different angle: all four tasks and all four Acceptance Criteria delivered and
verified, zero dropped/deferred items, verdict **CLEAN**. `CODE_REVIEW.md` (dated 2026-09-07,
"Status: ADVISORY_ONLY") independently confirms the same review outcome with full findings,
including a specific, successful re-check of the exact class of mistake (fail-fast instead of
`errors.Join` aggregation) that had blocked three prior runs on this issue. All three documents are
mutually consistent, internally detailed (specific function names, file:line citations, coverage
percentages), and read as authored during real execution, not as a template filled in blind.

**[V] Finding 2 — direct, present-day inspection of the source tree corroborates every specific
claim in Finding 1; nothing was found missing, stubbed, or half-built.** A second, narrowly-scoped
`codebase-tour` pass read the eight specific files/symbol sets `notes.md`/`DRIFT_REPORT.md` name as
delivered — `internal/team/overrides.go` (`decodeOverrides`, `validateOverrideSource`,
`SetOverride`, `UnsetOverride`, `ActiveOverride`, `ReadOverrideSource`), `internal/team/overrides_test.go`,
`internal/team/errors.go` (`ErrOverridePath`, `OverridePathError`), `internal/cli/cmd_team.go`
(`newTeamManageCommand`, `runTeamManage`, `teamManageError`), `internal/cli/cmd_team_manage_test.go`,
`internal/teamsource/render.go` (`readAgentSource`, called from both `RenderTeam` and
`RenderCurrent`), `internal/teamsource/render_test.go`, and three cross-command end-to-end tests in
`cmd_team_test.go`/`cmd_team_update_test.go`/`cmd_team_status_test.go` — and reported every file
present, every named symbol present with a matching signature, no `"not implemented yet"` stub
text anywhere, no dangling reference to an undefined symbol, and the `team manage` command actually
registered in `internal/cli/commands.go`'s `Commands()` table (reachable, not orphaned). Its own
verbatim assessment: *"a self-reported 'fully delivered' claim for this feature is corroborated by
what's in the repo today."* The one caveat it raised itself is honest and correctly scoped: it read
source only, did not run `go build`/`go test` itself (it has no `Bash`), and so cannot independently
rule out a compile or runtime-test failure invisible to static reading — that residual uncertainty
is carried forward explicitly below, not silently dropped.

**[V] Finding 3 — `.aiteam/APP_CONTEXT.md` (read directly by this orchestrator, not by
`codebase-tour`, since it sits under `.aiteam/`) already documents US-13 as delivered, in the present
tense, alongside run 26's US-11 documentation.** Its package map states, verbatim: *"`readAgentSource`
checks a per-repo override (`team.ReadOverrideSource`) before falling back to `AgentEntry.Source`...
used by `RenderTeam` and `RenderCurrent`, upstream of hashing — `team.Classify` needs no
override-awareness"* and its "Do not reinvent these" table lists a live row, *"override which local
`.md` file an agent renders from, per repo → `internal/team`... `team manage <key> <set/unset/show>`
is the CLI front door,"* plus a separate row confirming *"all six [`team` subcommands]... are fully
implemented, none a stub."* This is independent, third corroborating evidence, because
`app-context-updater` is a distinct invocation from whatever wrote `notes.md`, and its own stated
job is to describe what is *actually* in the tree, not to echo another agent's self-report.

**[V] Finding 4 — the one piece of Run 27's own close-out that is demonstrably missing is
`$session/MANIFEST.md`.** `MANIFEST.md`, read directly by this orchestrator, has **no History entry
at all for a Run 27**, and its "Implement" summary line (the running parenthetical after the
`[ ] Implement` checkbox) still reads, unmodified since Run 26: *"US-13/14/15/16/17 and US-1's
`manage` subcommand beyond its existing stub still open."* Every prior Implement run in this session
— including Run 25, which itself had a false "failed" terminal status corrected in a later pass (see
below) — eventually got a matching `MANIFEST.md` History entry, either written by the run itself or
retroactively by a subsequent close-out run (the precedent for Run 21, closed out by Run 22). Run 27
has neither. This is the one concrete, verifiable gap between "what Run 27's own artifacts say it
did" and "what the session's bookkeeping record reflects" — everything else checked above is
consistent and corroborated.

**[W] Finding 5 — the most likely explanation, reasoned from precedent and environmental signals,
not confirmed by a direct git read.** Three independent signals point the same direction:
1. The working tree is reported **clean** at HEAD (`2516882`), meaning whatever Run 27 wrote to disk
   is either committed or never existed — and Finding 2 confirms the code exists on disk today.
   Because litefactory (not any agent) performs the commit at the end of a run, and no agent in this
   team is permitted to run `git commit` itself, the only way US-13's code is both present and the
   tree is clean is that it was captured in a commit litefactory made when Run 27's process
   terminated — almost certainly `2516882`.
2. `2516882`'s commit message is the generic fallback *"litefactory run"*, unlike `7dae487`'s
   descriptive, self-authored message for Run 26 (*"Run 26 (Implement, US-11) is complete and the
   artifact exists."*). A descriptive commit message is plausibly generated from a run's own
   terminal report; a generic one is consistent with litefactory committing a worktree's contents
   without having received a clean terminal report to build a message from — i.e., the process ended
   abnormally after doing its work but before (or during) reporting back.
3. This exact shape — real, complete, tested work landing and getting auto-committed, while the
   run's own *reported* terminal status read "failed" for a reason unrelated to the work's
   correctness — already happened once in this same session: Run 25's terminal status "was only a
   post-completion session-usage-limit hit occurring after all work was already committed at
   `f625e3c` — not incomplete or reverted work" (MANIFEST.md, 2026-09-06/07 entry, itself confirmed
   in a later investigation). Run 27 not having its own `MANIFEST.md` entry, but having every other
   completion signal (notes.md close-out narrative, `DRIFT_REPORT.md` CLEAN verdict, `CODE_REVIEW.md`
   ADVISORY_ONLY, `APP_CONTEXT.md` already updated), is consistent with the same failure mode
   recurring: the run substantively finished (through Phase 3 testing and at least one round of
   `app-context-updater`) and was cut off — by a session/usage limit, a duplicate-invocation
   collision (see below), or a comparable harness-level event — at or near the very last step of its
   own close-out checklist, specifically before it could write `$session/MANIFEST.md`.
   **[W] confidence: high, but explicitly not equivalent to having read `git show 2516882` directly**
   — no tool available to this pass can perform that read, and this finding should be treated as a
   well-corroborated inference, not a verified fact, until a future run with `git`/`Bash` access
   confirms it directly (a five-second check: `git show --stat 2516882`).

**[V] Finding 6 — a documented, self-investigated non-issue that is nonetheless worth surfacing.**
`notes.md`'s own Run 27 entry records: *"a system reminder indicated a background agent 'Implement
US-13' was reported running concurrently; the engineer confirmed via `git status`/`git diff --stat`
that only its own edited files had changed, with no unexplained third-party changes, and proceeded
without escalating."* This raises, but does not confirm, the possibility that Run 27 was itself
invoked twice (a duplicate/overlapping harness dispatch) rather than failing on its own merits — a
plausible alternative or contributing explanation for Finding 5's "cut off near the end" hypothesis,
since a harness-level collision between two runs targeting the same worktree is exactly the kind of
event that could produce a real, substantively-complete commit from one process while the harness's
own run-tracking records the encompassing invocation as failed. This pass does not have the tooling
to confirm or rule this out (no access to litefactory's own run-dispatch records) and flags it as an
open question rather than adopting it as the explanation.

**[V] Finding 7 — dependency check, read directly from `plan_brief.md`'s Story Index, not assumed.**
US-13's row states `Depends On: US-1, US-4, US-5, US-9`. All four are independently confirmed landed
and committed prior to Run 27: US-1 (index schema/decoder) and US-5 (renderer contract) shipped in
early Implement runs; US-4 (`team add`/`team list`/`team show` CLI scaffold) shipped in Runs 21-22;
US-9 (write engine) shipped in Run 20 — all four are named as already-committed prerequisites in
`notes.md`'s own Run 27 entry ("Base state respected as given: US-11... landed and committed at
`7dae487`") and corroborated by `APP_CONTEXT.md`'s package map showing `internal/team`, `internal/
teamsource`, and all six `team` CLI subcommands as live. US-13 was not started against a missing or
incomplete foundation.

### 13. `team manage`'s resolution, as actually shipped — closing Domain §8 / Open Question 6

**[R this pass, sixth]** Domain §8 above posed three candidate replacements for the original,
now-invalid `team manage <agent> set <param>=<value>` request item and left the choice as Open
Question 6. `US-13.md`'s own Architecture Notes (read directly by this orchestrator) record that the
choice was made during the fourth Plan reconciliation pass — candidate (a), *"point a key at a
different local markdown file for this repo only"* — and Findings 1-3 above confirm it is now built,
tested, and reviewed clean: `.litefactory/team.overrides.json` (a flat `{agentKey: source}` map, the
same litefactory-owned bookkeeping directory US-9 introduced — directly answering the naming half of
former Open Question 12 by delivered fact, not by further Research recommendation), consumed by
`internal/teamsource/render.go`'s `readAgentSource` upstream of hashing in both `RenderTeam` and
`RenderCurrent`, so that `team.Classify` (US-10's drift classifier) needed **zero** modification —
exactly the "no special case" outcome US-13's own Architecture Notes predicted and `CODE_REVIEW.md`
independently confirmed. The one non-blocking advisory recorded against this delivery
(`CODE_REVIEW.md`'s CR-A-001: an unreachable `filepath.IsAbs(clean)` check in
`validateOverrideSource`, dead code with no behavioral effect) is cosmetic and does not affect any
Acceptance Criterion.

## Recommendation for the next Implement run — do not redo US-13; close out Run 27's bookkeeping and continue the Story Index

**[R this pass, sixth]** This is a narrow, corroborated recommendation for whoever opens the next
Implement run against this session, not a new design question:

1. **Do not re-implement US-13.** All four tasks (T-35–T-38) and all four Acceptance Criteria
   (AC-1–AC-4) are delivered, reviewed `ADVISORY_ONLY` with zero BLOCKING findings, tested (unit
   coverage across `internal/team`, `internal/teamsource`, and `internal/cli`, including three
   dedicated cross-command end-to-end tests), and — per Finding 5 — almost certainly already
   committed at `2516882`, the current tip of the branch, with a clean working tree. Re-running Phase
   1 development for US-13 would duplicate real, sound work and risk producing a second, conflicting
   implementation of the same override mechanism.
2. **Do first confirm, not assume, that `2516882` builds and tests clean**, since this pass had no
   `Bash`/`git` access to verify that directly (Finding 2's own stated limit). A single `go build
   ./... && go vet ./... && gofmt -l . && go test -race ./...` at the start of the next run — cheap,
   and exactly the kind of check every prior Implement run in this session already performs as a
   matter of course before or after its own Phase 1 — closes the one residual gap this investigation
   could not close itself. If that check surfaces any failure, treat it as a new, narrow bug-fix
   task against the already-landed US-13 code, not as grounds to rebuild it from scratch.
3. **Do write the missing `$session/MANIFEST.md` History entry for Run 27**, using `notes.md`'s own
   Run 27 narrative, `DRIFT_REPORT.md`, and `CODE_REVIEW.md` as the source record (the same pattern
   Run 22 used to retroactively document Run 21's gap) — and correct the stale "Implement" summary
   line, which currently still lists US-13 as open. This is bookkeeping, not implementation work, and
   is what actually closes Run 27 out; skipping it leaves the same false "still open" signal in place
   for the next run to trip over.
4. **Optionally, at the same run's discretion, fix `CODE_REVIEW.md`'s one recorded non-blocking
   advisory** (CR-A-001, the dead `filepath.IsAbs(clean)` check in `validateOverrideSource`) — purely
   cosmetic, not required by any Acceptance Criterion, and not worth a dedicated run on its own.
5. **Proceed to new scope from the Story Index**, per `plan_brief.md`'s Workflow step 10: with US-11
   and US-13 both now landed, US-14 (validation wiring, depends on US-5/US-9/US-13 — now fully
   satisfied) is the next story with no unmet dependency, alongside US-15 (multi-worktree
   propagation, depends on US-11 — also now satisfied). US-16 must still not be scheduled until
   US-15 lands (sequence-dependency stated in Domain §9/§10 above, unchanged by this pass). This
   pass does not choose between US-14 and US-15 for the next run — that is a scoping call for
   whoever opens it, per this session's established one-story-per-run discipline (explicitly named
   in Run 27's own `notes.md` entry as the reason bundled-scope runs 20/21/25 either overran or hit a
   usage limit).

This pass does not edit `MANIFEST.md`'s Implement section, `notes.md`, `DRIFT_REPORT.md`, or
`CODE_REVIEW.md` itself — those are Implement-mode's own bookkeeping artifacts and the fix belongs to
the Implement close-out run recommended above, consistent with this session's own established
boundary between Research investigating and Implement/Plan editing their own artifacts (the same
boundary Domain §12's fifth pass observed when it investigated the schema-drift blocker but left the
actual document edits to a dedicated Plan pass).

## Investigation Notice — 2026-09-07, seventh pass (post-implementation: what happened to Implement Run 31 / US-15 & US-16?)

**Trigger.** The sponsor reported that Run 31 (Implement mode, instructed to build US-15 and US-16
together — US-15 first, then US-16 on top of it, per `plan_brief.md`'s Hard-couplings note that
US-16 depends on US-15's live worktree-discovery mechanism *actually existing*, not merely on US-15
being "mostly done") was terminated mid-task by an HTTP 429 session-usage-limit cutoff. The only
recovered partial output was the orchestrator's own statement of that same sequencing plan. This
pass investigates exactly what landed before the cutoff, for both stories separately, and what a
follow-up Implement run should do to finish both without redoing sound work. It is **not** a design
pass — no design question above it is reopened.

**[R this pass] Method and its limits, stated up front, same shape as the sixth pass.** This
orchestrator has `Read`, `Write`, and `Agent` (restricted to `codebase-tour`, `web-researcher`) —
no `Bash`, no `git`, no `Glob`/`Grep` of its own — so it again could not run `git log`/`git show`/
`git diff` directly. What it did instead: (1) read `notes.md`, `DRIFT_REPORT.md`, `CODE_REVIEW.md`,
`MANIFEST.md`, `US-15.md`, `US-16.md` directly, since these sit under `.aiteam/` and no permitted
sub-agent can reach them; (2) read `.aiteam/AC_AMENDMENTS.md` directly, for the same reason,
specifically to check US-16's T-44 (its SEC-7 Owned-Paths documentation amendment); (3) dispatched
`codebase-tour` once, present-day-only, no git/`.aiteam` access, targeted at eight specific,
concrete questions about on-disk US-15/US-16 code (live worktree discovery, the `--all-worktrees`
flag, `.gitignore`-append logic, a runtime Owned-Paths write guard, tests, TODO markers, and the
repo's own root `.gitignore`); (4) cross-referenced all of the above against the git status/log
snippet already visible in this run's own environment context (branch
`lf/68-new-way-to-work-with-agent-teams`, working tree **clean**, HEAD at `f7884f5` — commit message
*"You've hit your session limit · resets 10:40am (Europe/Vienna)"* — parent `d491f76 Run 30
(Implement, US-14) is complete and the artifact exists.`). No commit was inspected via `git show`/
`git diff`; conclusions about what landed rest on corroboration across independent sources, flagged
**[W]** wherever they rest on inference rather than a direct git read.

**[V] Finding 1 — no Implement-mode process artifacts exist for Run 31 at all, for either story.**
`notes.md` ends with Run 30's US-14 entry (and, above it in the file, the US-13 entry retroactively
investigated by the sixth pass) — there is no US-15 or US-16 section anywhere in it. `DRIFT_REPORT.md`
is still headed, verbatim, *"Drift Report — new-way-to-work-with-agent-teams (US-14)"*, dated
2026-09-07 — this file's own established convention in this session is "one current-state report per
Implement run, not cumulative" (explicit in Run 22's MANIFEST.md entry), so a run that actually
reached its own close-out would have overwritten it with a US-15/US-16-scoped report, exactly as
Runs 20, 22, 25, 26, and 30 each did in turn. It was not touched. `CODE_REVIEW.md`'s header likewise
still reads *"Feature: Configurable Agent Teams — US-14..."*, with no trace of any US-15/US-16 review
pass, BLOCKING or otherwise. `MANIFEST.md`'s History (before this pass's edit, below) stopped at Run
30, and its Implement summary line still named "US-15, US-16, and US-17... each needing its own
future run." Unlike Runs 21, 25, and 27's precedent — where an incomplete-looking run's *code* was
real, reviewed, and tested, and only its own paperwork was missing — this run left no Implement-mode
paperwork trail whatsoever, not even a first-pass code-review verdict, for either story.

**[V] Finding 2 — direct, present-day inspection of the source tree shows US-15 is code-complete and
wired end to end; US-16 has zero code anywhere.** A dedicated `codebase-tour` pass, read-only and
present-day-only, reported the following (full citations as returned):
- **US-15 — live worktree discovery (T-41):** found and wired up. `discoverSiblingWorktrees(ctx,
  repoPath)` at `internal/cli/cmd_team_worktrees.go:46` opens the repo via `internal/git` and calls
  `clone.WorktreeList(ctx)` (the pre-existing `git worktree list` wrapper, reused rather than
  duplicated, consistent with the file's own header comment stating this is the only git verb the
  story invokes), excludes bare/prunable entries, resolves symlinks, excludes self, and returns
  sorted absolute paths. Called from `runTeamUpdateAllWorktrees` (`cmd_team_worktrees.go:126`),
  itself called from `runTeamUpdate` in `internal/cli/cmd_team.go:1238` when the new
  `--all-worktrees` flag is set.
- **US-15 — the `--all-worktrees` flag (T-42):** present. Defined at `cmd_team.go:1386`
  (`allWorktrees := fs.Bool("all-worktrees", false, ...)`), threaded into `runTeamUpdate`'s signature
  and branch (`cmd_team.go:1226,1238`), usage string updated at `cmd_team.go:1364,1376`.
- **US-15 — per-worktree aggregation/reporting:** JSON and table writers
  (`teamUpdateAllJSON`/`teamUpdateWorktreeJSON` at `cmd_team_worktrees.go:90-112`,
  `writeTeamUpdateAllJSON`/`writeTeamUpdateAllTable` at `:171,:184`) both present. A single
  worktree's failure is isolated via `errors.Join` (`cmd_team_worktrees.go:167`) rather than
  aborting the loop — matching AC-4's shape as described in `US-15.md`.
- **US-15 — tests:** `internal/cli/cmd_team_worktrees_test.go` — five tests, all built on real
  `git worktree add` fixtures via `gittest.Repo`: `TestTeamUpdateAllWorktrees_UpdatesGitConfirmedSibling`,
  `TestTeamUpdateAllWorktrees_ExcludesStalePathSilently`,
  `TestTeamUpdateAllWorktrees_OneFailureDoesNotBlockOthers`,
  `TestTeamUpdateAllWorktrees_DoesNotForceLocallyModifiedSibling`,
  `TestTeamUpdate_AllWorktreesFlagDefaultsFalse` — one per AC-1 through AC-4 plus the flag's default.
  No `TODO(US-15)` or similar marker was found anywhere in the tree.
- **US-16 — gitignore management (T-45):** **nothing found.** No function resembling
  `EnsureGitignore`/`AppendGitignore` exists anywhere in `internal/`; the only `gitignore`-related
  hit across the whole tree is an unrelated pre-existing test fixture
  (`internal/git/worktree_test.go:568`) writing a literal `.gitignore` as test data for an unrelated
  git-status scenario. `internal/team/lock.go:32`'s `acquireRepoLock` creates `.litefactory/` via
  `os.MkdirAll` but does nothing gitignore-related.
- **US-16 — runtime write guard / Owned Paths (T-46):** **nothing found beyond what already existed
  before this story.** `validateOwnedPath` (`internal/team/paths.go:69`, called from `Add` in
  `internal/team/write.go:85`) is pre-existing US-9/US-18 machinery validating a *renderer's* output
  path against the derived standalone path or a recognized root-file target — it has no awareness of
  `.litefactory/` and was never extended, matching `US-1.md:61`'s own forward note ("US-16 later
  extends this list by one"), which did not happen. The only other write-time guard present is the
  pre-existing symlink refusal (`internal/team/writefile.go:18,68-77`), explicitly distinct from an
  Owned-Paths allow-list check. No `WriteGuard`/`ownedPaths`-shaped new symbol exists.
- **US-16 — tests / TODO markers:** none found (`TestGitignore...`, `TestOwnedPaths...`,
  `TestWriteGuard...`, `TODO(US-16)` — all absent).
- **Root `.gitignore`:** contains a pre-existing `/.litefactory/` line (line 26, under a "Local
  runtime state" comment block) — this is litefactory's **own** repo's own `.gitignore`, predating
  this story, and is explicitly not evidence of T-45's automated per-*target*-repo append logic,
  which is what US-16 actually calls for. No other `.gitignore` file exists in this checkout (no
  sibling worktree present to inspect one in).
- **General sweep:** broad case-insensitive greps for "worktree" and "gitignore" across the whole
  tree turned up nothing beyond the above — no stub functions, no commented-out code, no half-written
  files, and no new error type (e.g. a `GitignoreError`/`WriteNotOwnedError` sibling to the existing
  `PathNotOwnedError`/`ErrRefusedSymlink`) anywhere related to US-16.

**[V] Finding 3 — a third, independent source confirms US-16's documentation task (T-44) was never
reached, corroborating Finding 2's "zero US-16 code" from a completely different artifact.**
`.aiteam/AC_AMENDMENTS.md` (read directly by this orchestrator, since it sits at the repo-root
`.aiteam/` level, the same class of file the sixth pass read `APP_CONTEXT.md` from directly) contains
an entry titled *"Amendment: SEC-7 override for Configurable Agent Teams — Owned Paths (interim),"*
dated 2026-09-06 — i.e. it predates Run 31 and was already the interim state going into it. It lists
exactly the three original harness directories (`.claude/agents/`, `.codex/skills/`,
`.opencode/agents/`) as the Owned Paths override's scope, and its own status line states, verbatim:
*"scope expected to be refined by US-16"* and *"It is expected to be corrected — not merely
extended — once US-16 lands its own bookkeeping directory design... Until then, treat this entry as
the floor of the override's scope, not its ceiling."* No entry beneath it in the ledger amends or
supersedes it. This is T-44 exactly as `US-16.md` describes it (*"Amend `.aiteam/AC_AMENDMENTS.md`'s
SEC-7 entry: replace the stale five-path list with harness dirs + `.litefactory/` + root-file
composition targets"*), unamended — a third, structurally independent confirmation (alongside zero
US-16 code and zero US-16 tests) that US-16 was never started, not merely under-documented.

**[W] Finding 4 — where the cutoff most plausibly landed, reasoned from corroborating signals, not
from a direct git read.** Several signals, read together, point at a specific and fairly narrow
window:
1. **The commit message itself is diagnostic, unlike the sixth pass's `2516882`.** `f7884f5`'s
   message — *"You've hit your session limit · resets 10:40am (Europe/Vienna)"* — is not the generic
   `"litefactory run"` fallback the sixth pass reasoned from; it names the session-limit cutoff
   explicitly, matching the sponsor's own framing of this run's termination cause (HTTP 429) exactly.
   This is stronger, not weaker, evidence for *why* the run stopped than anything available to the
   sixth pass — but it says nothing on its own about *how far* the run got, which is what the rest of
   this finding reasons about.
2. **US-15's own tests (T-43) exist and match its Acceptance Criteria one-for-one.** This team's
   established convention (visible throughout `notes.md`, e.g. Run 19's "all three registered... `go
   build`, `go vet`, and `go test... all passed"` self-check before reporting DONE) is that a
   `go-implementation-engineer`'s own Phase 1 delivery for a story includes that story's own
   task-level tests, written and self-verified before the orchestrator ever dispatches a dedicated
   Phase 1.5 code review. Five tests existing, one per US-15 AC plus the flag default, is consistent
   with a `go-implementation-engineer` invocation for US-15 having run to its own natural completion.
3. **Nothing downstream of that exists.** No `CODE_REVIEW.md` entry, no `notes.md` entry, no
   `DRIFT_REPORT.md` update — none of the steps that, in every one of Runs 19/20/22/25/26/30, follow
   a completed Phase 1 (a Phase 1.5 code-review dispatch, at minimum) are present for US-15, let alone
   for US-16.
4. **Taken together:** the most likely read is that the interruption landed **after** a
   `go-implementation-engineer` had finished building and self-testing US-15 in full, but **before**
   the orchestrator invoked (or received back a report from) `go-code-reviewer` for US-15 — and well
   before any work began on US-16, which shows zero trace of even having been started. This is
   consistent with, and does not contradict, the sponsor's own recovered fragment of the orchestrator's
   stated plan (build US-15 fully first, then US-16 on top of it).
   **[W] confidence: moderate — higher than a guess, lower than a confirmed fact.** Unlike the sixth
   pass's Run 27 (which had three independent *completed*-shaped documents — `notes.md`,
   `DRIFT_REPORT.md`, `CODE_REVIEW.md` — all agreeing, with only the bookkeeping file itself missing),
   this run has **no completed-shaped process document at all** to corroborate against; the inference
   here rests entirely on the on-disk code's own completeness and this team's own general Phase
   1-then-1.5 convention, not on any run-authored narrative. A future run with `Bash`/`git` access
   could sharpen this by running `git log --stat f7884f5` and `git show --stat f7884f5`, neither of
   which this pass could do.

**[V] Finding 5 — US-15's stated dependency is independently confirmed already landed, unrelated to
this cutoff.** `US-15.md`'s own Architecture Notes name `US-11 T-30` as the dependency for T-41; the
sixth pass's own investigation and this session's `MANIFEST.md` History both independently confirm
US-11 (`team update`, single-worktree) landed in Run 26, well before Run 31 started. US-15 was not
begun against a missing or incomplete foundation.

**[W] Finding 6 — the one thing this pass could not close, and the single most important item for
the next run.** Whether US-15's code, exactly as it stands on disk today, actually builds, vets, and
passes its own five tests (`go build ./... && go vet ./... && gofmt -l . && go test -race ./...`) was
**not verified this pass** — it has no `Bash`/`git` access (the same limit the sixth pass had), and
`codebase-tour`'s read of the five test functions was a static read only, confirming they exist and
target the right ACs, not that they pass. This matters more here than it did for the sixth pass's
US-13, precisely because Finding 1 found **no** code-review or test-run artifact at all for US-15 —
unlike US-13, which had three independent documents all separately attesting a real, run, all-green
`go test -race ./...`. Nothing here contradicts that US-15 is sound; nothing here confirms it either.

**Historical note on this brief's own prior recommendation, for continuity:** the sixth pass's own
"Recommendation for the next Implement run" (immediately above) named US-14 and US-15 as the two
stories newly dependency-clear at that point, and explicitly left the choice of which to schedule
next to whoever opened that run. Run 30 (US-14) and Run 31 (US-15 + US-16) both followed directly
from that list — this pass's findings are a continuation of that same thread, not a deviation from it.

## Recommendation for the next Implement run — verify and close out US-15 first, then build US-16 fresh on top of it

**[R this pass, seventh]** A narrow, corroborated recommendation for whoever opens the next Implement
run against this session:

1. **Do not re-implement US-15's core mechanism.** Live worktree discovery
   (`internal/cli/cmd_team_worktrees.go`'s `discoverSiblingWorktrees`), the `--all-worktrees` flag,
   per-worktree propagation/aggregation, and a five-test suite covering all four ACs are all present
   and wired together on disk today (Finding 2). Rebuilding any of this from scratch would duplicate
   real, apparently-sound work.
2. **Do first run the build/vet/test check this pass could not run itself** — `go build ./... &&
   go vet ./... && gofmt -l . && go test -race ./...` — before touching anything else. This is the
   single open item Finding 6 leaves unresolved, and unlike US-13's precedent, there is no existing
   `CODE_REVIEW.md`/`notes.md`/`DRIFT_REPORT.md` entry to fall back on if it turns out something is
   broken. If it fails, treat the fix as a narrow bug-fix task against the already-landed US-15 code,
   not as grounds to rebuild the feature.
3. **Then run the Phase 1.5 code review that never happened for US-15**, scoped to the files Finding
   2 names (`internal/cli/cmd_team_worktrees.go`, `internal/cli/cmd_team_worktrees_test.go`, and the
   touched portions of `internal/cli/cmd_team.go`) — treat this exactly as if Run 31 had reached this
   step but the process died first; running it now is completing the one step that never happened,
   not redoing anything. Apply the standard 2-cycle fix/re-review rule if it finds anything BLOCKING —
   this issue's own recurring `errors.Join`-aggregation mistake (BLOCKING in Runs 20, 25, and the
   original Run 21) is the first thing worth checking specifically in `cmd_team_worktrees.go`'s
   per-worktree loop (Finding 2 already confirms an `errors.Join` call exists at
   `cmd_team_worktrees.go:167`, but this pass did not independently trace whether it is a real
   isolate-and-continue or merely present).
4. **Only once US-15 is confirmed sound (steps 2-3 clean), build US-16 fresh.** Finding 2 and Finding
   3 agree from two independent angles: there is genuinely nothing to build on — no gitignore-append
   code, no extended Owned-Paths write guard, no tests, and the SEC-7 ledger entry (T-44) is still the
   original interim, three-path list. US-15's discovery mechanism now exists and is exactly the thing
   `US-16.md`'s own Architecture Notes say to reuse rather than reinvent
   (`discoverSiblingWorktrees`/T-41) — this is the concrete reason US-16 could not have started
   correctly before US-15 was real, not merely a scheduling preference.
5. **T-44 (the `.aiteam/AC_AMENDMENTS.md` SEC-7 correction) is part of US-16's own scope, not a
   separate bookkeeping chore** — it is explicitly one of US-16's four tasks (`US-16.md`), and the
   ledger is still waiting for exactly this amendment (Finding 3). Do it as part of building US-16,
   not as an afterthought.
6. **Close out both stories' bookkeeping together, once both are actually done** — overwrite
   `DRIFT_REPORT.md` scoped to US-15+US-16 (following this file's established one-report-per-run
   convention), add one `notes.md` entry covering both, and write the `MANIFEST.md` History entry.
   Do not write a partial close-out for US-15 alone if US-16 is being attempted in the same run —
   but if time runs short, closing out US-15 alone (with an honest note that US-16 was not reached)
   is strictly better than repeating Run 31's shape of leaving zero bookkeeping for work that did
   land.
7. **Given this session's own repeated lesson** — Runs 20, 21, 25, and now 31 have each either
   bundled multiple stories into one run and overrun, or been cut off by a usage limit mid-way — this
   pass recommends treating "verify and close out US-15" as the hard requirement for the next run and
   "then build US-16" as a should-if-time addendum within the same run, rather than a second
   forced-together bundle. The two stories remain hard-sequenced (US-16 genuinely cannot start before
   US-15 is confirmed sound, not merely present on disk), but sequencing does not require forcing both
   into one run if the first alone risks running long.

This pass does not edit `MANIFEST.md`'s Implement section, `notes.md`, `DRIFT_REPORT.md`, or
`CODE_REVIEW.md` itself, for the same reason the sixth pass did not: those are Implement-mode's own
bookkeeping artifacts, and the fix belongs to the Implement run recommended above.
