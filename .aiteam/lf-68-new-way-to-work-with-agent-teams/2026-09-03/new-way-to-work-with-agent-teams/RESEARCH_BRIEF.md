# Research Brief — lf-68-new-way-to-work-with-agent-teams

## Request

litefactory's agent teams (the specialist sub-agents — go-architect, requirements-engineer,
codebase-tour, web-researcher, etc. — and the orchestrators that invoke them) are currently
"built in." The request is to make agent teams **configurable per repo**, specifically:

1. A new CLI command (or commands) to **add** an agent team to a repo.
2. A new CLI command to **update** an agent team in a repo.
3. Teams are to be defined in a JSON document, roughly:
   ```json
   {
     "teams": [
       { "Go Team": ["agent-a", "agent-b", "..."] }
     ],
     "shared": ["requirements-engineer", "..."]
   }
   ```
   — or a better alternative shape if research surfaces one.
4. The JSON lives in the litefactory repo itself as the standard/fallback config; users may
   supply their own JSON (per corporate/repo) that overrides or extends the fallback.
5. Some agents are shared across teams (e.g. a requirements engineer); others are specialized
   (go-implementer, python-implementer, ...).
6. Agents must work across multiple harnesses (Claude Code, Codex CLI, opencode, ...).
7. Individual agents should be configurable via a CLI command shaped like
   `litefactory team manage <agentname> set <param>=<value>`.
8. Config versioning and a real update/migration story is called out explicitly as needing
   comprehensive research — this is treated as the hardest open problem in this brief.

This is a headless run: no clarifying question was put to a human. Ambiguities below are
resolved with the most reasonable reading and flagged as assumptions or open questions.

## Codebase Context

**[V]** litefactory is a single Go module/binary (`cmd/litefactory/main.go` → `internal/cli`)
with ~15 `internal/*` packages (`config`, `agent`, `harness`, `codex`, `turn`, `daemon`, `repos`,
`store`, `git`, `github`, `identity`, `report`, `naming`, `secrets`, `redact`, `logging`,
`launchd`, `planbrief`). No package named `team`, `roster`, or `manifest` exists today.

**[V] Agent teams are not Go data today — they are two hand-authored Markdown trees, owned by an
external tool.**
- `.claude/agents/*.md` (13 files) — Claude Code's own sub-agent format: YAML frontmatter
  (`name`, `tools` as a comma-separated literal list, `description`, `model`, `color`), body =
  full prose system prompt.
- `.codex/skills/*/SKILL.md` (13 dirs) — Codex CLI's own skill format: narrower frontmatter
  (`name`, `description` only). Each file opens with an explicit hand-written diff preamble
  against the Claude version, e.g. (go-architect's copy): "Codex copy of
  `.claude/agents/go-architect.md`. Both tools read the same instructions; only the enforcement
  differs" — then lists concrete deltas: Codex does not restrict tools per-agent (every agent
  gets the same tool set), delegation uses `spawn_agent` not `Task`, there is no separate reply
  channel (findings are returned as the final message), and Codex chooses its own model rather
  than a literal alias.
- `.claude/AGENT_ROSTER.md` — a human-maintained descriptive index table, cross-referencing the
  13 agent files. Its own text states the current "add an agent" flow in full: "Adding one later
  means writing a new agent file and adding a row here." **[V]** No Go code reads, validates, or
  reasons about either Markdown tree's contents; Claude Code parses its own frontmatter, Codex
  CLI parses its own.

**[V] These files are scaffolded by an external tool, not litefactory, and litefactory
explicitly disclaims ownership of them** — `internal/cli/cmd_repo.go:54-58`: `repo add`
documents that "aiteam must already be installed in the repository — run `npx aiteam` there
first. litefactory never installs it and never writes inside the clone." This is a structural
fact the requested feature runs directly into — see Constraints & Risks and Open Questions.

**[V] The only place litefactory's Go code touches "team" at all** is bookkeeping, not behavior:
`internal/repos.DetectAiteam` (`internal/repos/aiteam.go:106-134`) scans a registered repo once
at `repo add` time and sets a free-text `Team` string (e.g. `"go-backend"`), persisted as
`AiteamTeam` on the registration row (`internal/repos/registration.go:123`). This value is never
read back to select, load, or filter an agent list — it is descriptive metadata only.

**[V] How an agent actually gets invoked today, end to end:** litefactory's `internal/harness`
builds a pure, harness-agnostic text prompt (task, feature slug, framing, live instruction) that
names no specialist agent at all (`internal/harness/prompt.go`). That prompt goes to whichever
`agent.Runner` the run's `Kind` resolves to — Claude via `internal/harness`, Codex via
`internal/codex` — both implementing the same `Runner` contract (`AGENTS.md:117-135`). The
coding-agent tool's own top-level session then reads the target repo's checked-in `CLAUDE.md`,
which routes (in prose, interpreted by the LLM) to an orchestrator, which in turn invokes named
specialist sub-agents — entirely inside that tool's own native sub-agent/skill mechanism,
invisible to litefactory's Go code. **Implication:** litefactory today has no runtime hook at the
point where "which agents exist" is decided; that decision currently happens inside the coding
tool itself, driven by which files exist in `.claude/agents/`/`.codex/skills/`.

**[V] `internal/agent.Kind`** (`internal/agent/kind.go:1-39`) is an open-vocabulary string — the
real harness-extensibility point in Go code; a value is valid only if registered in
`internal/turn.RunnerSet`. **[V] `config.Kind`** (`internal/config/tier.go:54-79`) deliberately
duplicates `agent.Kind` as plain strings (`"claude"`, `"codex"`) rather than importing it,
because `internal/config` is enforced stdlib-only (`internal/config/purity_test.go`); its doc
comment states explicitly: "carries no closed vocabulary of its own — a third tool is an
additional map key here, never a new case to add." This is the pattern a harness-aware team
config would need to follow if it lives in or near `internal/config`.

**[V] `config.ToolProfile{Claude *ToolProfileClaude; Codex *ToolProfileCodex}`** is the nearest
existing precedent for "one logical thing, per-harness stanzas" — a named, operator-authored
tool-permission profile split by harness, narrowed via `HasStanzaFor` (string switch on
`"claude"`/`"codex"`). **[V] `internal/agent.Permissions{Profile string; Payload any}`** is the
nearest existing precedent for "configure a per-run parameter" — an opaque grant carried across
the `Runner` boundary whose concrete shape is harness-specific and deliberately uninspected
outside the addressed `Runner`. Neither is scoped to *which agents exist and what each does* —
both are scoped to tool/sandbox permission, not persona/prompt/model per named agent. **[V]**
No Go type represents "an agent" (name + description + tools + model + harness variants) as
data anywhere in the repo today — a team-config feature is new package design, not an extension
of an existing struct.

**[V] Existing config-loading precedent:** the one machine-level config,
`$LF_HOME/config.yaml`, is strict YAML (`gopkg.in/yaml.v3`, `Decoder.KnownFields(true)`) —
unknown keys are a startup error, never silently ignored. Validation errors use dotted-path keys
(`internal/config/policy.go`), errors are joined and deterministically ordered (explicit
`sortedKinds`/`sortedTiers`/`sortedProfileNames` — Go map order is never trusted). Every config
value is scanned for secrets at startup regardless of key name (`secretscan.go`). **[V]** There
is no JSON config anywhere in the repo except `.claude/settings.json` (Claude Code's own
settings file, unrelated to teams) — YAML is litefactory's only config format precedent. **[V]**
There is **no existing config-file version field** (e.g. `version: N`) in `config.Config`'s
schema; the closest thing to schema evolution is a single deprecated-key conflict check (a
`claude:` top-level key vs. `agents.claude:`) inside `Decode` — not a versioned/migratable
format.

**[V] The one real migration mechanism in the codebase is DB-schema-specific, not
config-specific:** `internal/store/migrate.go:20` embeds `migrations/*.sql`, filename-versioned
(`NNNN_*.sql`), tracked via a `schema_migrations` table; `AGENTS.md:109-112` states the
convention explicitly: "A schema change is a new `.sql` file and nothing else — do not add a
migration library." This cannot be reused directly for a JSON config file, but its shape
(monotonic version number, one file/step per version, applied in order, tracked) is a candidate
conceptual model — see Domain / Problem Space §5.

**[V] CLI command structure:** `internal/cli/commands.go` holds a single flat
`Commands() []*Command` registration point. Convention (`internal/cli/command.go:1-22`): one
`cmd_<group>.go` file per command group, one constructor function, flags bound via a
`New func(fs *flag.FlagSet) Handler` closure, one line added to `Commands()` — "no switch
statement to update and no registry to mutate." New commands (`team add`, `team update`,
`team manage`) would live in a new `internal/cli/cmd_team.go`, following the same pattern used
for e.g. `repo` commands.

**[V] Existing per-repo override precedent:** `.aiteam/rulebook.md` is referenced by
`research-orchestrator`/`codebase-tour` as an optional, per-repo file that scanners fall back
from when absent (confirmed: no `.aiteam/rulebook.md` exists in this repo, and both scanners
in this run explicitly fell back to generic practice). This is the nearest existing "repo may
supply its own file to override/extend a default" convention, though it is prose-based
(Markdown), not JSON/schema-based, and is consumed by prompt-reading sub-agents, not by Go code.

**[V] Security guardrails already in place that any new "set an agent parameter" command must
route through, not bypass:** `internal/config/policy.go`'s `forbiddenFlag()` exists specifically
to prevent configuration from ever producing a dangerous flag (the kind that skips permission
checks) — any new per-agent parameter-setting surface must be validated through equivalent
logic, not a raw pass-through of user-supplied `key=value` pairs. Separately,
`config.Codex.AllowUserConfig=false` already demonstrates that a config toggle can silently
change *which files an agent tool reads* (it suppresses Codex's own `.codex/skills` discovery)
— precedent that a user-suppliable override JSON needs the same class of scrutiny before being
trusted.

## Domain / Problem Space

### 1. Per-harness agent definition formats today (external, as of Sep 2026)

**[W]** Each of the three named harnesses has its own current, independently-evolving format —
none of them share a schema:

- **Claude Code** — Markdown + YAML frontmatter under `.claude/agents/` (matches what this repo
  already uses), fields: `name`, `description` (required), plus optional `tools`,
  `disallowedTools`, `model` (alias or `inherit`), `permissionMode`, `maxTurns`, `skills`,
  `mcpServers`, `hooks`, `memory`, `background`, `effort`, `isolation`, `color`,
  `initialPrompt`, and an `experimental` block. **There is also a JSON form**: the `claude
  --agents` flag accepts a JSON object keyed by agent name using the same fields plus `prompt`
  in place of the Markdown body — this is a real, already-existing precedent for "the same
  agent, expressed as JSON" that a litefactory-generated config could target or emit toward.
  Precedence order when multiple sources define the same agent name: managed org settings →
  `--agents` flag → `.claude/agents/` (project) → `~/.claude/agents/` (user) → plugin
  directories.
- **Codex CLI** — standalone **TOML** files, one per agent, under `.codex/agents/` (project) or
  `~/.codex/agents/` (personal). Required: `name`, `description`, `developer_instructions`.
  Optional: `model`, `model_reasoning_effort`, `sandbox_mode`, `mcp_servers`, `skills.config`.
  Global subagent behavior (enable/disable, concurrency) sits under an `[agents]` table in
  `config.toml`. **This repo's actual Codex integration does not use this format** — it uses
  the separate, cross-tool "Agent Skills" convention (`SKILL.md` under `.codex/skills/`), which
  describes an invocable capability, not a persona. **[W] confidence: medium** — read from
  OpenAI's "Learn" documentation portal rather than raw source, and this surface has moved
  quickly through 2025–2026.
- **opencode** — either an `"agent"` object embedded in `opencode.json`, or standalone Markdown
  + YAML frontmatter files under `.opencode/agents/` (project) / `~/.config/opencode/agents/`
  (global). Fields: `description` (required), `mode`, `model`, `prompt` (path), `temperature`,
  `top_p`, `steps`, `disable`, `hidden`, `color`. Tool permission is a **separate** `permission`
  object with per-capability `"allow"|"ask"|"deny"` values and glob-pattern control for `bash`.

**Implication for the requested feature:** "one JSON document, one shape, all harnesses" is not
achievable without either (a) generating each harness's native file from a shared source, or
(b) picking one harness's format as canonical and adapting others to it. The Claude `--agents`
JSON flag format is the most promising candidate for a canonical shared shape, since it is
already JSON and already a first-class input to one of the two harnesses this repo integrates
with today — but it does not natively express Codex's TOML-specific fields
(`sandbox_mode`, `model_reasoning_effort`) or opencode's separate `permission` object, so a
harness-agnostic core with harness-specific extension fields (broadly what
`config.ToolProfile` already does for tool permissions) looks like the right shape, not a
literal reuse of any one harness's schema.

### 2. Cross-tool / vendor-neutral standards for a portable agent definition

**[W]** No settled, widely-adopted standard exists (as of Sep 2026) that covers "persona +
system prompt + tool permissions + model choice" in a way portable across harnesses:
- **AGENTS.md** — formalized as an open spec (Aug 2025, OpenAI-led; donated to the Linux
  Foundation's Agentic AI Foundation, Dec 2025); read by 30+ tools. But it is plain Markdown
  prose, repo-scoped, single-file — not structured, not a multi-agent team format.
- **Agent Skills (`SKILL.md`)** — published as an open standard by Anthropic (2025-12-18,
  agentskills.io); this repo already uses it for the Codex side. Closest real cross-tool
  convention that exists, but it describes an invocable **capability/procedure**, not a full
  persona (no tool-permission grant list, no model-selection field in the core spec).
- **A2A "Agent Card"** — a JSON manifest for network-discoverable, independently-hosted agent
  *services* (reached v1.0 in 2026). Architecturally a service-discovery document (auth
  schemes, endpoints, task lifecycle) — not fit for a local CLI's sub-agent roster; only
  name/description/skills/model overlap conceptually.
- **"Agent Manifest" (agent-manifest-spec.org)** — exists, JSON-Schema-based, but
  single-maintainer with minimal adoption; not a reliable foundation.

**[W] Not settled / genuine gap:** no cross-tool JSON schema variant of AGENTS.md aimed at
*multi-agent team definitions* (as opposed to single-repo instructions) was found with
meaningful adoption. **Recommendation for Plan mode: design litefactory's own schema rather
than adopt an external one**, but keep the shape close enough to the Claude `--agents` JSON
form and to `SKILL.md`'s field vocabulary that a converter to/from either is mechanical, not a
rewrite.

### 3. Proposed team-config JSON shape (candidate, for Plan mode to finalize)

The shape suggested in the request (`teams: [{ "<name>": [agents] }], shared: [...]`) works but
has two rough edges worth flagging before Plan mode locks it in:
- A team as `{ "Go Team": [agents] }` makes "Go Team" a JSON *key*, which is awkward for
  ordering, renaming, and for attaching team-level metadata (description, default harness).
  A list of objects with a `name` field (`{"name": "Go Team", "agents": [...]}`) is the more
  conventional and more extensible shape, and matches the sorted-list, no-map-order-trust
  convention already used elsewhere in `internal/config` (`sortedKinds`/`sortedTiers`).
- "Agents" as a flat list of name strings loses the very thing this feature needs to
  configure — per-harness variants and per-agent parameters. **[R this run]** the more capable
  shape is a top-level `agents` registry (each agent defined once, with per-harness stanzas,
  matching the `config.ToolProfile{Claude *X; Codex *Y}` pattern already in the codebase), and
  `teams`/`shared` become references into that registry by name, not inline definitions. This
  also directly answers "several agents are shared across teams" — a shared agent is simply
  referenced by more than one team, with no duplication.

Illustrative sketch (not a final schema — Plan mode owns the schema):
```json
{
  "schemaVersion": 1,
  "agents": {
    "requirements-engineer": {
      "description": "...",
      "harnesses": {
        "claude": { "tools": ["Read","Write","Glob","Grep"], "model": "sonnet" },
        "codex":  { "sandbox_mode": "workspace-write" },
        "opencode": { "permission": { "edit": "allow" } }
      }
    },
    "go-implementer": { "...": "..." }
  },
  "shared": ["requirements-engineer"],
  "teams": [
    { "name": "Go Team", "agents": ["go-implementer", "go-architect"] }
  ]
}
```
This keeps "shared" as a genuine cross-team pool rather than a parallel, differently-typed list,
and gives `litefactory team manage <agentname> set <param>=<value>` a single addressable target
(`agents.<name>.harnesses.<kind>.<param>` or a harness-agnostic field) instead of having to
search every team's inline copy.

### 4. Ownership boundary conflict — the load-bearing finding of this research

**[V]** litefactory today explicitly disclaims writing inside a registered repo's clone at all
(`internal/cli/cmd_repo.go:54-58`) and treats `aiteam` (installed via `npx aiteam`) as the
external owner of `.claude/agents/`/`.codex/skills/`. The requested feature — litefactory CLI
commands that **add** and **update** an agent team **in a repo** — reads as litefactory writing
generated agent/skill files into that same repo, which is either:
(a) a deliberate expansion of litefactory's role (it starts owning agent-file generation,
    superseding or working alongside `aiteam`), or
(b) scoped more narrowly than it sounds — litefactory only manages the **JSON team-config
    source of truth**, and a separate (existing or new) generation step turns that JSON into
    `.claude/agents/*.md` / `.codex/skills/*/SKILL.md`, possibly still via `aiteam` or via a
    litefactory-owned generator that this feature would also need to build.
This is not a detail to resolve unilaterally — it changes what "add a team to a repo" means
mechanically (write JSON only vs. write JSON + generate/write harness-native files vs. call out
to `aiteam`). Carried into Open Questions and Recommended Scope below.

**[R2 — resolved in Plan mode, not re-litigated here]** `plan_brief.md`'s binding Scope Decision
(DEV-1) settles this in favor of option (a): litefactory adopts write-ownership of the five Owned
Paths (`.aiteam/team.json`, `.aiteam/team.lock.json`, `.claude/agents/<agent>.md`,
`.codex/skills/<agent>/SKILL.md`, `.opencode/agents/<agent>.md`) as an explicit, scoped override
of the `cmd_repo.go:57-58` rule, sanctioned by the human sponsor before planning began. This
research brief's Open Question 1 (below) is retained for provenance but should be read as
answered by the plan, not as still-open.

### 5. Config versioning and migration — the explicitly-flagged hard problem

**[W]** The generally low-ceremony, well-established external pattern: an explicit
`schemaVersion` (Kubernetes' `apiVersion` is the widely-known analog) field in the document; on
load, parse generically first, branch on that field, then run **sequential one-step migration
functions** (v1→v2→v3→…) until the document reaches current, only then unmarshal into the
current typed struct. Keep the config schema version independent of the tool's own release
version, since one binary may need to read several historical schema versions written by older
litefactory versions or hand-edited by users.

**[W]** For Go tooling specifically:
- JSON Schema *validation* has a real, currently-maintained option:
  `santhosh-tekuri/jsonschema` (v6.x, draft-4 through 2020-12) is the actively recommended
  choice over the older, now largely legacy `xeipuuv/gojsonschema`. Google also published
  `google/jsonschema-go` (2026-01-21), built primarily for LLM tool-schema/MCP use — a viable
  validation building block, not migration-specific.
- **No dominant Go library for versioned JSON-config migration** was found (unlike
  `golang-migrate/migrate` for SQL). This class of problem is commonly hand-rolled per project.
  **[W] confidence: medium** — an absence claim from a bounded search, not an exhaustive
  ecosystem survey.

**[V] Internal precedent worth adapting, not reusing directly:** `internal/store/migrate.go`'s
embedded, filename-versioned `.sql` files with a tracked `schema_migrations` table is a real,
working migration model in this codebase, but it is DB-specific and the project's own
convention (`AGENTS.md:109-112`) explicitly rejects adding a migration *library* for that case.
The analogous, repo-consistent answer for a JSON config file is likely: a `schemaVersion` int
field, hand-written Go migration functions (not a third-party library) applied sequentially at
load time, mirroring the "one step per version, no framework" philosophy already established
for the DB — this keeps the new feature stylistically consistent with the rest of the codebase
rather than introducing the project's first schema/migration dependency.

**[R this run] Two independent versioning axes should not be conflated:**
1. **Schema version** — the shape of the JSON document itself (fields, nesting) — needs the
   migrate-on-load treatment above.
2. **Content/override version** — a user's own team-config file being layered over
   litefactory's shipped fallback, and litefactory shipping a newer fallback later. This is a
   *merge/update* problem (does the user's override still make sense against a newer fallback?
   did a referenced shared agent get renamed or removed upstream?), not a schema-migration
   problem, and needs its own answer (e.g. a fallback version pinned inside the user's override
   file, with `team update` diffing against it) — this is the harder of the two and was flagged
   by the request itself as needing comprehensive research; this run surfaces the axis split as
   its main contribution and leaves the mechanism to Plan mode.

**[R2 — resolved in Plan mode]** `plan_brief.md`'s Shared Decision 1 (the three-hash model:
`renderedHash`/`sourceHash`/`baseSourceHash`, plus a top-level `hashVersion` distinct from the
lockfile's own `schemaVersion`) is the mechanism this axis-2 question needed. It is now settled
and binding; see the Addendum below for how it surfaces in a concrete lockfile.

### 6. Two-tier fallback/override precedence — external naming conventions

**[W]** No single naming convention is a strict de facto standard, but recognizable patterns
exist: `docker-compose.yml` + `docker-compose.override.yml` (base + override, auto-merged,
override wins) is the most widely recognized instance of "ship a base, let an external file
override it." A `.local` suffix (`*.local.json`) is a second common convention for
untracked/personal overrides layered on a shared file. Most real agent-tool CLIs (Codex, Claude
Code, opencode) actually implement three-plus tiers (built-in defaults → user/global config →
project/repo config), not strictly two — worth considering whether litefactory's fallback vs.
user-override request is really two tiers or should be three (litefactory-shipped fallback →
organization/corporate config → repo-local override).

**[R2 — resolved in Plan mode]** Settled as three tiers, exactly as flagged: embedded fallback
(`//go:embed`) → org layer (`--team-config`/`$LF_HOME/config.yaml`'s `team_config:` key) → repo-
local `.aiteam/team.json`. See `plan_brief.md`'s Shared Decision 2, "Chain B."

## Constraints & Risks

- **[V] Ownership-boundary risk (see Domain §4).** This is the single biggest risk to scope
  correctly before planning: building "add/update a team in a repo" without first deciding
  whether litefactory now writes `.claude/agents/`/`.codex/skills/` files (previously
  `aiteam`'s job, per explicit doc comment) risks either duplicating `aiteam`'s responsibility
  or building a JSON source-of-truth that never actually reaches the harnesses that need to
  read it. *(Resolved in Plan mode — see DEV-1. Retained here for provenance.)*
- **[V] No existing Go abstraction for "an agent" as data.** This is greenfield package design,
  not a refactor — expect real design effort in Plan mode, not just CLI plumbing.
- **[V] `internal/config` purity constraint.** If the team config is meant to layer into or
  near `internal/config`, it must respect the stdlib-only leaf-package rule
  (`purity_test.go`) — a JSON-Schema validation library would likely need to live in a
  different, non-leaf package, or the purity boundary would need an explicitly justified,
  scoped exception (precedent exists for scoped exceptions but they are not free — see
  `AGENTS.md:117-150`'s stated import graph).
- **[V] Security surface.** `litefactory team manage <agentname> set <param>=<value>` is a
  generic key=value setter — by shape, this is exactly the kind of raw pass-through the
  existing `forbiddenFlag()` guard was built to prevent for `config.yaml`. Any implementation
  must route per-agent parameter values through equivalent validation (tool ceilings, no
  dangerous-flag equivalents) rather than writing user input straight through to a generated
  harness file.
- **[V] Every config value gets secret-scanned today** (`secretscan.go`) regardless of key
  name — a new JSON config surface must preserve this blanket behavior, not carve out an
  allowlist.
- **[W] Harness format churn.** Codex CLI's and opencode's config surfaces were both described
  as moving quickly through 2025–2026; a converter/generator built against today's field names
  carries real drift risk and should be designed to fail loudly (not silently drop fields) when
  a harness's shape changes upstream.
- **[W] No dominant Go migration library** for JSON config specifically — expect to hand-roll
  the migration mechanism (consistent with the codebase's existing DB-migration philosophy of
  "no library"), not to pull one in.
- **[V] Stale doc note (unrelated to this feature but adjacent):** `internal/config/tier.go`'s
  struct doc comment (lines 117-123) says "Only 'claude' is populated by Defaults() today," but
  `defaultModelByTier()` (lines 152-165) already populates Codex entries — a one-line
  documentation fix, flagged for whoever next touches that file, not part of this feature's
  scope.

## Open Questions

1. **Does litefactory take over ownership of `.claude/agents/*.md` / `.codex/skills/*/SKILL.md`
   generation from `aiteam`, or does the new JSON config sit alongside `aiteam`'s output as a
   separate, litefactory-only source of truth that still needs a bridge to reach the harness
   tools?** *(Answered by `plan_brief.md` DEV-1: litefactory takes ownership, scoped to five
   named Owned Paths. Retained for provenance, not open in practice.)*
2. **[V]** The string "opencode" appears nowhere in this repo's in-scope code or docs, but
   *does* appear in 7 files under prior `.aiteam/` session artifacts tied to feature slugs
   `lf-setup` and `lf-8-add-codex-as-second-party-item` (content not read — out of this
   session's scope per the session boundary). Plan mode should check
   `.aiteam/APP_CONTEXT.md`/`APP_CONSTRAINTS.md` for whether opencode support was already
   scoped or decided against, rather than this feature re-deriving that decision from scratch.
3. **What exactly does `litefactory team manage <agentname> set <param>=<value>` need to be
   able to set?** *(Answered by `plan_brief.md`'s US-13/US-14: a closed parameter allowlist,
   validated via `config.ContainsForbiddenFlag` and `config.ClassifySecret`.)*
4. **How does the content/override versioning problem (Domain §5, axis 2) actually resolve
   when litefactory ships a new fallback team config?** *(Answered by `plan_brief.md` Shared
   Decision 1 — the three-hash model and the `team status`/`team update` decision table.)*
5. **Where should the fallback JSON physically live inside the litefactory repo**, and how is
   it distributed to installed copies of litefactory? *(Answered: `//go:embed`, per
   `plan_brief.md`'s Out of Scope note "Distributing the fallback config outside the binary —
   `//go:embed` only.")*
6. **[V]** A possible mismatch between `.claude/AGENT_ROSTER.md` (13 rows) and `.claude/agents/*.md`
   (13 files) was flagged by the codebase tour but not cross-verified row-by-row this run — if
   real, it's a live example of the exact drift a config-driven roster is meant to eliminate,
   worth a quick confirmation before Plan mode cites it as motivation. *(Still genuinely open —
   not addressed by `plan_brief.md`.)*

## Recommended scope for Plan mode

**In scope:**
- Design a harness-agnostic JSON schema for agent + team definitions, informed by Domain §1–3
  above (a registry of agents with per-harness stanzas; `teams` and `shared` as references into
  that registry, not inline duplicates).
- Resolve Open Question 1 (ownership boundary with `aiteam`) as a first planning decision —
  it changes the shape of `team add`/`team update` from "write JSON" to "write JSON and
  generate/write harness files," which is a materially different and larger scope.
- Design the `schemaVersion`-based migrate-on-load mechanism for the JSON document itself
  (Domain §5, axis 1), following the codebase's existing "hand-written steps, no migration
  library" philosophy rather than introducing a new dependency.
- Design the content/override update story (Domain §5, axis 2) — this is the hardest and most
  explicitly-flagged part of the request; it deserves its own dedicated design section in the
  plan, not a one-line answer.
- Define the initial parameter surface for `litefactory team manage <agentname> set
  <param>=<value>` (Open Question 3), including how it routes through the existing
  `forbiddenFlag()`-style validation rather than a raw pass-through.
- New `internal/cli/cmd_team.go` following the existing one-file-per-command-group,
  `Commands()`-registration convention.

**Out of scope for this feature (flag if requested later, do not silently absorb):**
- Building or modifying `aiteam` itself, or replicating its scaffolding role wholesale, unless
  Open Question 1 is answered in favor of litefactory taking over that responsibility.
- opencode support as a fully first-class harness (parser/generator for its config), pending
  Open Question 2's answer from prior session context — treat as a stretch goal / follow-on
  unless that context says otherwise.
- Adopting any external cross-tool standard (Agent Manifest, A2A Agent Card) as the schema —
  Domain §2 found none fit for purpose; design litefactory's own schema instead.

---

## Addendum — 2026-09-04 Follow-up: CLI repo-scoping & sample team-config JSON

**Context.** `plan_brief.md` and its 14 sub-issues (US-1..US-14) are complete and are *not*
edited by this addendum — reconciling the plan against these findings is a future Plan run's
job. The human sponsor reviewed the sub-issues and raised two concrete questions before approving
any change to the plan. This addendum answers both, headless (no clarifying question was put to
a human; ambiguities are resolved with the most reasonable reading and recorded as assumptions).

### Follow-up Request

1. The plan's CLI surface (`team list`, `team add`, `team update`, `team manage`, ...) gives every
   command a `--repo <path>` flag, but doesn't visibly separate "commands that operate on
   litefactory itself" from "commands that operate on one connected repo's installed team," and
   doesn't explain how `--repo` relates to how litefactory already names/addresses a connected
   repo elsewhere. Research the existing convention and use it to classify the plan's six
   commands and to shape the repo-scoped ones concretely.
2. Produce one concrete, realistic sample of a team-config JSON document, consistent with the
   schema and drift-detection design already locked in `plan_brief.md`.

### 1. How litefactory already identifies a connected repo — findings

**[V] `repo add <path>` derives identity; it does not accept an assigned name.** It takes a
positional filesystem path (`ArgSpec{Names: []string{"path"}, Min: 1}`,
`internal/cli/cmd_repo.go:59`) and *derives* an identifier by reading the clone's `origin` remote
and parsing it into `git.RepoRef{Forge, Owner, Name}` (`internal/repos/registration.go:144-194`,
`internal/git/remote.go:24-38`). The stored row (`store.Repo`, `internal/store/repos.go:14-44`)
has a surrogate int64 `ID` plus `Path`, `Forge`, `Owner`, `Name`; `Repo.Slug()`
(`internal/store/repos.go:48`) renders `owner/name`.

**[V] The only column that is actually `UNIQUE` is `path`**
(`internal/store/migrations/0001_init.sql:32`) — `UpsertRepo` is keyed on path
(`internal/store/repos.go:58-92`). `(forge, owner, name)` has only a plain, non-unique index
(`0001_init.sql:47`). Nothing stops two different registered clones from resolving to the same
`owner/name` (two local checkouts of one GitHub repo). This matters for what follows.

**[V] Neither `repo list` nor any existing `repo` subcommand lets a user select one specific
already-registered repo.** `repo list` (`internal/cli/cmd_repo.go:72-86`) takes no arguments and
always prints every registered repo. There is no `repo show`. So the `repo` noun itself has no
precedent for "address one repo."

**[V] The real precedent lives in a different command group: `logs`.**
`litefactory logs <issue> [--repo owner/name]` (`internal/cli/cmd_logs.go:70-171`) is the one
place litefactory disambiguates "which registered repo." The flag is declared to take exactly
`owner/name` (`parseRepoFlag`, `cmd_logs.go:851-862`, rejecting anything that isn't a single
`/`-separated pair), resolved via `store.DB.GetRepoBySlug(ctx, forge, owner, name)`
(`internal/store/repos.go:115-126`, doc comment: "This is how `--repo owner/name` resolves"). A
positional task ref can carry the repo too (`github:acme/api#123`, `naming.ParseRef`,
`internal/naming/ref.go:62-90`) — passing both a ref and `--repo` is a usage error
(`cmd_logs.go:170`). Because `owner/name` is not DB-unique (see above), a match against more than
one registered repo returns `*store.AmbiguousIssueError` (`internal/store/errors.go:24-60`),
printed as `"Pass --repo owner/name to choose one:"` (`cmd_logs.go:802-809`) — ambiguity is a
handled, expected case, not an edge case litefactory currently ignores. `task clean <issue>`
(`internal/cli/cmd_task.go:29-49,163-181`) deliberately has *no* `--repo` flag and refuses
outright on ambiguity instead, so the `logs` pattern is a deliberate design choice, not the only
option in the codebase, but it is the only one that supports disambiguated addressing.

**[V] The `owner/name` component charset is validated** by `naming.validateRefComponent`
(`internal/naming/ref.go:95-124`): ASCII letters, digits, `.`, `_`, `-` only — chosen because
these components are interpolated into filesystem paths
(`$LF_HOME/logs/runs/<forge>/<owner>/<repo>/<issue>`). This is filename-safe and CLI-safe, and
would be equally usable for team-command addressing.

**[V] No `<noun> <identifier> <verb>` shape exists anywhere in litefactory today.** Every
existing multi-word command is registered as a flat, space-joined literal `Command.Name`
(`Name: "repo add"`, `cmd_repo.go:51`; `Name: "task clean"`, `cmd_task.go:31`) — see
`internal/cli/commands.go`. The closest existing pattern for "address one repo" is a **flag after
the verb** (`logs <issue> --repo owner/name`), not a positional segment between noun and verb.
`litefactory teams <repo_name> <command>` (the shape floated in review) would be a genuinely new
CLI dispatch pattern — the literal-string `Command.Name` matching convention has no support today
for a variable middle segment, so adopting it would mean inventing new router logic solely for
this feature, where extending the flag convention needs none.

**[R this run] Note on terminology collision.** `plan_brief.md`'s own provenance marker grammar
uses `team=<slug>` for the *team's* slug (e.g. `go-team-lite`). This is a different "slug" from
the *repo's* `owner/name` slug found here. Both are real, both are called "slug," and Plan mode
should use distinct field/flag names (e.g. `--repo` for the repo slug, `team` positional/field for
the team slug) so a reconciliation pass doesn't conflate them.

### 2. Recommendation — litefactory-level vs. repo-level split

| Command | Level | Repo argument | Why |
|---|---|---|---|
| `team list` | **litefactory-level** | none | Lists team names + membership from the resolved embedded⊕org layers only. Nothing repo-specific to show without a target repo, so it should not require one. |
| `team show <team>` | **litefactory-level**, with an optional repo-scoped preview | optional `--repo <owner/name>` | Without `--repo`: shows the embedded⊕org resolution (what any repo would get by default). With `--repo`: additionally layers that repo's `.aiteam/team.json`, naming which layer supplied each field — a genuine preview, not a write. |
| `team add <team>` | **repo-level** | required `--repo <owner/name>` | Installs generated files into exactly one connected repo. |
| `team status` | **repo-level** | required `--repo <owner/name>` | Drift-checks one repo's installed lockfile. |
| `team update` | **repo-level** | required `--repo <owner/name>` | Rewrites one repo's installed files. |
| `team manage <agent> set\|get\|unset` | **repo-level** | required `--repo <owner/name>` | Writes to one repo's `.aiteam/team.json` (`set`/`unset`); `get` reads the same repo-scoped resolution. |

**Concrete, binding recommendation for Plan mode to reconcile against `plan_brief.md`'s CLI
surface (Shared Decision 2):**

- **Change `--repo <path>` to `--repo <owner/name>`**, matching the existing `logs` convention
  exactly (same flag name, same shape, same validation, same
  `store.DB.GetRepoBySlug`/`AmbiguousIssueError` resolution path) — do not invent a second way to
  address a repo. Keep the flag-after-verb shape (`team add <team> --repo <owner/name>`); do not
  adopt the `teams <repo> <command>` positional shape (see dispatch-mechanism cost above).
- **This is a real, not cosmetic, design change from the current draft.** The plan's current
  `--repo` resolves an arbitrary filesystem path (default `.`, requiring only a `.git` entry, no
  registration check at all) — it can target *any* git working tree, registered with litefactory
  or not. Switching to `owner/name` means `team` commands can only ever target repos that have
  already been through `repo add`. That is a deliberate narrowing (consistent addressing, reuses
  ambiguity handling) but it removes the "run against any git checkout" capability the current
  draft has by default. Plan mode must decide this explicitly, not inherit it silently.
- **Default when `--repo` is omitted, for repo-level commands:** resolve cwd against the repos
  registry (absolute path lookup); if the cwd matches exactly one registered repo, use it; if it
  matches none, fail with `PreconditionError` naming that cwd is not a registered repo and
  suggesting `repo add`; cwd can never match *more than one* registered repo since `path` is
  DB-unique, so no ambiguity is possible on this path specifically (ambiguity only arises from
  `owner/name` lookups). This preserves today's "just cd into the repo and run the command" UX
  without reintroducing the raw-path behavior.
- **`team list` and the litefactory-level half of `team show` need no `--repo` at all** and should
  not require one — this is the direct fix for the reviewer's "listing what's available in
  litefactory itself" expectation.

### 3. Sample team-config JSON, consistent with `plan_brief.md`'s locked design

Three documents, worked as one consistent example, because the plan deliberately keeps them
separate (Shared Decision 1: the config document and the lockfile answer different questions and
have different owners). Hash values below are **illustrative placeholders** (repeated-hex
patterns for readability), not computed hashes — Plan/Implement mode must compute real ones.
Agent names are drawn from this repo's actual current roster
(`.claude/agents/*.md`) for realism.

**(A) Embedded fallback (`//go:embed`, litefactory-shipped, `schemaVersion: 1`)** — abbreviated to
five representative agents rather than the full 13-agent roster:

```json
{
  "schemaVersion": 1,
  "agents": {
    "requirements-engineer": {
      "description": "Runs the planning conversation; produces feature-spec.md.",
      "harnesses": {
        "claude": { "tools": ["Glob", "Grep", "Read", "Write", "TodoWrite"], "model": "sonnet" },
        "codex":  { "sandboxMode": "workspace-write" },
        "opencode": { "mode": "primary", "permission": { "edit": "allow", "bash": "ask" } }
      }
    },
    "go-architect": {
      "description": "Reviews plan_brief.md and adds architectural hints per story.",
      "harnesses": {
        "claude": { "tools": ["Read", "Write", "Glob", "Grep", "Bash", "TodoWrite"], "model": "opus" },
        "codex":  { "sandboxMode": "workspace-write", "modelReasoningEffort": "high" },
        "opencode": { "mode": "primary", "permission": { "edit": "allow", "bash": "allow" } }
      }
    },
    "go-implementation-engineer": {
      "description": "Implements one user story's tasks at a time.",
      "harnesses": {
        "claude": { "tools": ["Read", "Write", "Edit", "Glob", "Grep", "Bash", "TodoWrite"], "model": "sonnet" },
        "codex":  { "sandboxMode": "workspace-write" },
        "opencode": { "mode": "subagent", "permission": { "edit": "allow", "bash": "allow" } }
      }
    },
    "go-code-reviewer": {
      "description": "Reviews Phase 1 output against architectural hints; writes CODE_REVIEW.md.",
      "harnesses": {
        "claude": { "tools": ["Read", "Write", "Glob", "Grep", "Bash", "TodoWrite"], "model": "sonnet" },
        "codex":  { "sandboxMode": "read-only" },
        "opencode": { "mode": "subagent", "permission": { "edit": "deny", "bash": "ask" } }
      }
    },
    "codebase-tour": {
      "description": "Runs the Two-Pass Codebase Tour for Research mode.",
      "harnesses": {
        "claude": { "tools": ["Read", "Glob", "Grep", "TodoWrite"], "model": "haiku" },
        "codex":  { "sandboxMode": "read-only" }
      }
    }
  },
  "shared": ["requirements-engineer", "codebase-tour"],
  "teams": [
    {
      "name": "go-team",
      "description": "Go backend feature delivery: plan, implement, review.",
      "agents": ["requirements-engineer", "go-architect", "go-implementation-engineer", "go-code-reviewer", "codebase-tour"]
    }
  ]
}
```

**(B) Repo-local override (`.aiteam/team.json`, written by `team manage`, layered per Chain B)** —
a realistic partial override, not a redefinition: bumps one agent's model, disables another
(excluded from every team and all generated output, per the plan's "removal is `disabled: true`,
never deletion" rule), and adds one repo-specific agent that has **no embedded definition at
all** (the case `baseSourceHash: ""` exists for):

```json
{
  "schemaVersion": 1,
  "agents": {
    "go-implementation-engineer": {
      "harnesses": { "claude": { "model": "opus" } }
    },
    "go-code-reviewer": {
      "disabled": true
    },
    "docs-writer": {
      "description": "Repo-specific: keeps README and docs/ in sync with shipped code.",
      "harnesses": {
        "claude": { "tools": ["Read", "Write", "Glob", "Grep"], "model": "sonnet" },
        "codex":  { "sandboxMode": "workspace-write" }
      }
    }
  },
  "teams": [
    {
      "name": "go-team-lite",
      "description": "This repo's trimmed team: skip code review, use opus for implementation, add a docs writer.",
      "agents": ["requirements-engineer", "go-architect", "go-implementation-engineer", "docs-writer"]
    }
  ]
}
```

**(C) Lockfile (`.aiteam/team.lock.json`, litefactory-exclusive, after `team add go-team-lite
--harness claude,codex`)** — `.opencode/` is omitted because this repo's `--harness` auto-detect
found only `.claude/`/`.codex/` present; `go-code-reviewer` correctly produces **no artifacts**
(disabled, excluded from output). Artifacts are sorted by path, as required:

```json
{
  "schemaVersion": 1,
  "hashVersion": 1,
  "team": "go-team-lite",
  "artifacts": [
    {
      "path": ".claude/agents/docs-writer.md",
      "agent": "docs-writer",
      "harness": "claude",
      "contentVersion": 1,
      "renderedHash": "b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1",
      "sourceHash":   "b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1b1",
      "baseSourceHash": ""
    },
    {
      "path": ".claude/agents/go-architect.md",
      "agent": "go-architect",
      "harness": "claude",
      "contentVersion": 1,
      "renderedHash": "a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2",
      "sourceHash":   "a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2",
      "baseSourceHash": "a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2"
    },
    {
      "path": ".claude/agents/go-implementation-engineer.md",
      "agent": "go-implementation-engineer",
      "harness": "claude",
      "contentVersion": 1,
      "renderedHash": "c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3",
      "sourceHash":   "c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3",
      "baseSourceHash": "c4c4c4c4c4c4c4c4c4c4c4c4c4c4c4c4c4c4c4c4c4c4c4c4c4c4c4c4c4c4c4c4"
    },
    {
      "path": ".claude/agents/requirements-engineer.md",
      "agent": "requirements-engineer",
      "harness": "claude",
      "contentVersion": 1,
      "renderedHash": "d5d5d5d5d5d5d5d5d5d5d5d5d5d5d5d5d5d5d5d5d5d5d5d5d5d5d5d5d5d5d5d5",
      "sourceHash":   "d5d5d5d5d5d5d5d5d5d5d5d5d5d5d5d5d5d5d5d5d5d5d5d5d5d5d5d5d5d5d5d5",
      "baseSourceHash": "d5d5d5d5d5d5d5d5d5d5d5d5d5d5d5d5d5d5d5d5d5d5d5d5d5d5d5d5d5d5d5d5"
    },
    {
      "path": ".codex/skills/docs-writer/SKILL.md",
      "agent": "docs-writer",
      "harness": "codex",
      "contentVersion": 1,
      "renderedHash": "e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6",
      "sourceHash":   "e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6",
      "baseSourceHash": ""
    },
    {
      "path": ".codex/skills/go-architect/SKILL.md",
      "agent": "go-architect",
      "harness": "codex",
      "contentVersion": 1,
      "renderedHash": "f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7",
      "sourceHash":   "f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7",
      "baseSourceHash": "f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7"
    },
    {
      "path": ".codex/skills/go-implementation-engineer/SKILL.md",
      "agent": "go-implementation-engineer",
      "harness": "codex",
      "contentVersion": 1,
      "renderedHash": "0808080808080808080808080808080808080808080808080808080808080808",
      "sourceHash":   "0808080808080808080808080808080808080808080808080808080808080808",
      "baseSourceHash": "0909090909090909090909090909090909090909090909090909090909090909"
    },
    {
      "path": ".codex/skills/requirements-engineer/SKILL.md",
      "agent": "requirements-engineer",
      "harness": "codex",
      "contentVersion": 1,
      "renderedHash": "1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a",
      "sourceHash":   "1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a",
      "baseSourceHash": "1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a"
    }
  ]
}
```

**What this example demonstrates, mapped to the locked design:**
- `go-architect` and `requirements-engineer` (shared): all three hashes equal per artifact — the
  file is exactly what the embedded fallback would produce, untouched by any override, and
  unedited on disk. `team status` reports `clean`.
- `go-implementation-engineer`: `renderedHash == sourceHash` (freshly installed, no drift) but
  `baseSourceHash` differs from `sourceHash` — the repo-local override (`model: opus`) changed
  the fully-resolved definition relative to the embedded-only baseline. This is exactly the
  `source-changed` signal Shared Decision 1's `baseSourceHash` exists to carry, independent of
  drift.
- `docs-writer`: `baseSourceHash: ""` — a repo-local-only agent with no embedded definition,
  the documented legitimate state, not corruption.
- `go-code-reviewer`: `disabled: true` in the override → zero lockfile entries, zero generated
  files, correctly excluded from the installed team.
- Every generated file's first body line would carry the provenance marker, e.g.:
  `<!-- litefactory: agent=docs-writer team=go-team-lite contentVersion=1 -->` — matching
  `plan_brief.md`'s stated grammar (`agent=<name> team=<slug> contentVersion=<n> ...`). The
  trailing `...` in that grammar is unspecified beyond these three fields; Plan mode should
  finalize whether `harness` or `hashVersion` belong in the marker too.

**Assumption made this run, flagged as a new open question:** the lockfile above assumes **one
installed team per repo** (a single top-level `team` field). Neither `plan_brief.md`'s Shared
Decision 1 nor its Story Index states whether a second `team add <other-team>` on the same repo
is expected to extend the same lockfile (multiple teams, artifacts unioned) or is disallowed
outright. This is now Open Question 7 below.

### New Open Questions (continuing the numbering above)

7. **Can a repo have more than one team installed at once?** `team add <team>` is
   currently singular; the lockfile's shape (one `team` field vs. an array, or a `team` field per
   artifact instead of top-level) depends on the answer. Not addressed by `plan_brief.md`;
   this addendum's sample assumes single-team-per-repo as the more conservative reading.
8. **Should `team list`/`team show` truly need zero `--repo` for their litefactory-level form,** or
   is there a real repo-level need to ask "which team, if any, is currently installed in *this*
   repo" (distinct from `team status`'s per-artifact drift report, which doesn't surface the
   installed team's name/description on its own)? This addendum recommends `team show <team>
   --repo <owner/name>` cover that need as a preview, but Plan mode should confirm that's
   sufficient rather than warranting a dedicated read command.
9. **`--repo owner/name` inherits `logs`'s ambiguity handling** (two registered clones resolving
   to the same `owner/name`) — Plan mode needs an explicit sentinel/error path for this in
   `internal/team` (an `ErrAmbiguousRepo`-shaped equivalent to `store.AmbiguousIssueError`),
   not an assumption that `owner/name` is unique.
10. **The exact provenance-marker field grammar** (`agent=<name> team=<slug> contentVersion=<n>
    ...`) needs its trailing fields finalized — this addendum's sample used only the three
    fields `plan_brief.md` states explicitly; whether `harness` and/or `hashVersion` belong in
    the marker too is unresolved.

### Recommended scope update for Plan mode (this addendum only — does not edit `plan_brief.md`)

- Change Shared Decision 2's `--repo <path>` to `--repo <owner/name>` on the five repo-level
  commands (`team show` optionally, `team add`, `team status`, `team update`, `team manage`),
  resolved via the existing `store.DB.GetRepoBySlug` path, with cwd-based default resolution
  through the repos registry (not a raw `.git` check) as described above.
  `--repo`'s absence should not be an error for `team list` or for `team show` used without a
  repo-preview.
  This is a **breaking change to an already-drafted CLI surface**, not an addition — it should
  be reconciled explicitly in the sub-issues that define it (`US-4`, and the acceptance criteria
  in `US-9`, `US-10`, `US-11`, `US-13` that reference `--repo`), not layered on top silently.
- Add the four new open questions above to whatever Plan mode treats as its residual-risk list
  for this feature.
- The JSON samples above are illustrative, not the literal schema Plan mode must ship — but they
  are consistent with every binding decision already recorded in `plan_brief.md` and can be used
  directly as fixtures for golden-file tests (Shared Decision 3) once real hashes replace the
  placeholders.
