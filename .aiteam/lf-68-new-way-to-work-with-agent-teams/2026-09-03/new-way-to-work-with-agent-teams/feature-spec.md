# Feature Spec: Configurable Agent Teams
# Status: APPROVED
# Date: 2026-09-04

## Goal

Let a litefactory operator install, update and tune a named agent team inside a connected repo from the CLI, driven by a versioned JSON definition that litefactory owns, so agent rosters stop being hand-authored Markdown that drifts per repo and per harness.

## Scope Decision (binding, not open)

- litefactory **does** adopt write-ownership of agent-team files inside a connected repo. The prior rule at `internal/cli/cmd_repo.go:54-58` ("litefactory never installs it and never writes inside the clone") is explicitly overridden **for the paths listed in "Owned Paths" only**.
- Every other path in a connected repo stays untouched.
- litefactory writes files; it never runs `git add`/`commit`/`push` in a connected repo. Generated changes are left in the working tree.

## Owned Paths (per connected repo)

| Path | Owner | Content |
|---|---|---|
| `.aiteam/team.json` | user (litefactory writes on `team manage`) | repo-local override layer |
| `.aiteam/team.lock.json` | litefactory (exclusive) | provenance + hashes of every generated artifact |
| `.claude/agents/<agent>.md` | litefactory (tracked in lockfile) | generated Claude sub-agent |
| `.codex/skills/<agent>/SKILL.md` | litefactory (tracked in lockfile) | generated Codex skill |
| `.opencode/agents/<agent>.md` | litefactory (tracked in lockfile) | generated opencode agent |

## User Stories

| ID | Story |
|----|-------|
| US-1 | As an operator, I want a JSON team config with an agent registry and teams that reference it by name, so one agent shared by two teams is defined once. |
| US-2 | As an operator, I want litefactory to resolve three config layers (embedded fallback → org config → repo-local override) with a documented precedence, so a company can extend the shipped teams without forking them. |
| US-3 | As an operator, I want the config document to carry a `schemaVersion` and be migrated on load by hand-written sequential steps, so an older file written by an older binary still loads. |
| US-4 | As an operator, I want `litefactory team list`, so I can see which teams and agents the resolved config offers before installing anything. |
| US-5 | As a maintainer, I want one shared agent definition rendered into each harness's native format through a pluggable renderer contract, so adding a fourth harness is a new renderer and nothing else. |
| US-6 | As a Claude Code user, I want the Claude renderer to emit `.claude/agents/<agent>.md` with correct frontmatter, so the generated agent is loadable by Claude Code unchanged. |
| US-7 | As a Codex CLI user, I want the Codex renderer to emit `.codex/skills/<agent>/SKILL.md`, so the same roster works under Codex. |
| US-8 | As an opencode user, I want the opencode renderer to emit `.opencode/agents/<agent>.md`, so the same roster works under opencode. |
| US-9 | As an operator, I want `litefactory team add <team> [--repo path]` to write the team's harness files and a lockfile, so a repo gets a working roster in one command. |
| US-10 | As an operator, I want `litefactory team status` to diff installed files against the version they were generated from, so I know exactly what my company customised before I update anything. |
| US-11 | As an operator, I want `litefactory team update` to overwrite only artifacts that are unmodified and flag every drifted one instead of clobbering it, so an update never silently destroys a local customisation. |
| US-12 | As an operator, I want `team update` to also report which upstream agent definitions changed since the installed `contentVersion`, so I can tell "litefactory changed this" apart from "we changed this". |
| US-13 | As an operator, I want `litefactory team manage <agent> set/get/unset <param>[=<value>]`, so I can retune one agent's model or tools without hand-editing JSON. |
| US-14 | As a security-conscious operator, I want every settable parameter checked against an allowlist, a value validator and the existing secret scan, so a config surface cannot smuggle in a permission-skipping flag or a credential. |

## Data Model

### Entity: TeamConfig (the JSON document)

| Entity | Field | Type | Required | Constraints |
|--------|-------|------|----------|-------------|
| TeamConfig | schemaVersion | int | yes | ≥ 1; > binary's current → load error |
| TeamConfig | contentVersion | int | yes in embedded fallback, optional in overrides | monotonic; identifies the shipped fallback an override was authored against |
| TeamConfig | agents | map[string]AgentDefinition | yes | key matches `^[a-z0-9][a-z0-9-]{0,63}$`; keys iterated in sorted order everywhere |
| TeamConfig | shared | []string | no | each entry must exist in `agents` |
| TeamConfig | teams | []Team | yes | non-empty in the fallback; names unique, case-insensitive |

### Entity: AgentDefinition

| Entity | Field | Type | Required | Constraints |
|--------|-------|------|----------|-------------|
| AgentDefinition | description | string | yes | 1–500 chars, single line |
| AgentDefinition | promptPath | string | yes | relative path to a Markdown body; resolved relative to the file that declared it; may not escape its root (no `..`, no absolute) |
| AgentDefinition | model | string | no | harness-agnostic tier hint; a stanza `model` overrides it |
| AgentDefinition | tools | []string | no | harness-agnostic tool names; renderers map them |
| AgentDefinition | harnesses | map[string]HarnessStanza | no | keys restricted to `claude`, `codex`, `opencode`; unknown key → load error |

### Entity: HarnessStanza (per-harness, discriminated by map key)

| Entity | Field | Type | Required | Constraints |
|--------|-------|------|----------|-------------|
| ClaudeStanza | model | string | no | Claude alias or `inherit` |
| ClaudeStanza | tools | []string | no | replaces the agnostic `tools` when present |
| ClaudeStanza | disallowedTools | []string | no | — |
| ClaudeStanza | color | string | no | — |
| CodexStanza | model | string | no | — |
| CodexStanza | sandboxMode | string | no | one of `read-only`, `workspace-write` |
| CodexStanza | modelReasoningEffort | string | no | one of `low`, `medium`, `high` |
| OpencodeStanza | model | string | no | — |
| OpencodeStanza | mode | string | no | one of `primary`, `subagent`, `all` |
| OpencodeStanza | temperature | float | no | 0.0–2.0 |
| OpencodeStanza | permission | map[string]string | no | values one of `allow`, `ask`, `deny` |

### Entity: Team

| Entity | Field | Type | Required | Constraints |
|--------|-------|------|----------|-------------|
| Team | name | string | yes | `^[A-Za-z0-9][A-Za-z0-9._ -]{0,63}$` |
| Team | slug | string | yes | `^[a-z0-9][a-z0-9-]{0,63}$`; the CLI argument |
| Team | description | string | no | — |
| Team | agents | []string | yes | non-empty; each must exist in `agents`; `shared` entries are appended implicitly and may not be re-listed |

### Entity: Lockfile (`.aiteam/team.lock.json`, litefactory-exclusive)

| Entity | Field | Type | Required | Constraints |
|--------|-------|------|----------|-------------|
| Lockfile | schemaVersion | int | yes | lockfile's own shape version, independent of TeamConfig's |
| Lockfile | team | string | yes | installed team slug |
| Lockfile | harnesses | []string | yes | harnesses generated for, sorted |
| Lockfile | configSchemaVersion | int | yes | schemaVersion of the config that generated this install |
| Lockfile | contentVersion | int | yes | fallback contentVersion at install time |
| Lockfile | installedAt | timestamp | yes | UTC RFC 3339 ms |
| Lockfile | litefactoryVersion | string | yes | build version that wrote it |
| Lockfile | artifacts | []Artifact | yes | sorted by `path` |

### Entity: Artifact (lockfile entry — the drift primitive)

| Entity | Field | Type | Required | Constraints |
|--------|-------|------|----------|-------------|
| Artifact | path | string | yes | repo-relative, forward slashes |
| Artifact | agent | string | yes | agent registry key |
| Artifact | harness | string | yes | `claude` / `codex` / `opencode` |
| Artifact | renderedHash | string | yes | sha256 of the bytes litefactory wrote |
| Artifact | sourceHash | string | yes | sha256 over the canonicalised resolved AgentDefinition + prompt body |

### Relationships

- TeamConfig → AgentDefinition via `agents[<name>]`
- Team → AgentDefinition via `teams[].agents[]` (name reference; no inline definitions)
- TeamConfig → AgentDefinition via `shared[]` (name reference; implicitly a member of every team)
- AgentDefinition → HarnessStanza via `harnesses[<kind>]`
- AgentDefinition → prompt body (Markdown file) via `promptPath`
- Lockfile → Artifact via `artifacts[]`; Artifact → AgentDefinition via `agent`, and → harness renderer via `harness`

## API / Interface Changes

New file `internal/cli/cmd_team.go`; one constructor per command plus one line each in `Commands()` (`internal/cli/commands.go`).

| Method | Endpoint / Action | Auth | Notes |
|--------|------------------|------|-------|
| CLI | `litefactory team list [--repo path] [--json]` | local | Prints teams, their agents, and which agents are shared. Reads the resolved config; touches no repo file. |
| CLI | `litefactory team show <team> [--repo path] [--json]` | local | Per-agent resolved view: description, model, tools, per-harness stanzas, and which layer each value came from. |
| CLI | `litefactory team add <team> [--repo path] [--harness k,…] [--dry-run] [--adopt]` | local | Renders and writes the team's artifacts + lockfile. Refuses if a lockfile already exists (points at `team update`). |
| CLI | `litefactory team status [--repo path] [--json]` | local | Per artifact: `clean` / `modified` / `missing` / `orphan`, plus `upstream-changed` when `sourceHash` differs from the currently resolved definition. Exit 0 clean, exit 1 any non-clean. |
| CLI | `litefactory team update [--repo path] [--dry-run] [--force] [--prune]` | local | Rewrites `clean` and `missing` artifacts; skips and reports `modified` ones unless `--force`; removes `orphan` ones only under `--prune`. Rewrites the lockfile only for artifacts it actually wrote. |
| CLI | `litefactory team manage <agent> set <param>=<value> [--repo path] [--harness k]` | local | Writes into `.aiteam/team.json` only. Validated per US-14. Does not re-render; prints the `team update` hint. |
| CLI | `litefactory team manage <agent> get [<param>] [--repo path] [--json]` | local | Prints resolved value(s) and the originating layer. |
| CLI | `litefactory team manage <agent> unset <param> [--repo path] [--harness k]` | local | Removes the key from the repo-local override so the lower layer wins again. |
| Flag | `--team-config <path>` (on every `team` command) | local | Points at the org/corporate JSON, overriding the `team_config` key in `$LF_HOME/config.yaml`. |
| Config | `team_config: <path>` in `$LF_HOME/config.yaml` | local | New optional key: the org layer's location. Absent → only fallback + repo-local layers. |

## Business Rules

### Config resolution
- Three layers, lowest to highest precedence: **embedded fallback** (`//go:embed` in the new team package) → **org config** (`--team-config` / `config.yaml: team_config`) → **repo-local** (`.aiteam/team.json`).
- Merge is per-field within an agent, keyed by agent name: a higher layer supplying `harnesses.claude.model` replaces only that field. List-valued fields (`tools`, `disallowedTools`, `agents`) are replaced wholesale, never element-merged.
- A higher layer may define a new agent or a new team; it may not delete one. Removal is expressed as `"disabled": true` on the agent, which excludes it from every team.
- Unknown keys anywhere in any layer are a load error, matching the `KnownFields(true)` behaviour of `$LF_HOME/config.yaml`.
- Validation errors use dotted-path keys (`agents.go-architect.harnesses.codex.sandboxMode`), are joined, and are ordered deterministically by sorting keys — never by Go map order.
- Every string value in every layer is passed through the existing startup secret scan regardless of key name; no allowlist carve-out.

### Schema versioning (axis 1 — document shape)
- Load is two-phase: decode generically, read `schemaVersion`, apply hand-written one-step migrations (v1→v2→v3…) in order, then decode into the current typed struct.
- Migrations are hand-written Go functions in the team package. No migration library, no third-party JSON-Schema dependency, consistent with `internal/store/migrations`.
- `schemaVersion` greater than the binary's current → hard error naming both versions and telling the user to upgrade litefactory. Never a best-effort load.
- `schemaVersion` is independent of litefactory's release version and of `contentVersion`.
- Migrating an on-disk `.aiteam/team.json` is not automatic: `team update` reports that the repo-local file is at an older schemaVersion and rewrites it only with the user's explicit `--migrate-config`.

### Content versioning and drift (axis 2 — installed copy vs. source)
- Every generated file's bytes are hashed at write time into `artifacts[].renderedHash`; the definition it came from is hashed into `artifacts[].sourceHash` (canonical JSON of the resolved AgentDefinition, plus the prompt body bytes).
- Drift classification, per artifact:

| On-disk state | Classification | `team update` behaviour |
|---|---|---|
| file hash == `renderedHash` | `clean` | rewrite silently |
| file hash != `renderedHash` | `modified` | skip, report path + agent; only `--force` overwrites |
| file absent | `missing` | recreate |
| file present, no lockfile entry, path in an owned directory | `orphan` | report; delete only under `--prune` |
| lockfile entry present, `sourceHash` != current resolved definition | `upstream-changed` | orthogonal flag on top of the four above; drives the "what litefactory changed" report |
- `modified` + `upstream-changed` together is the conflict case: reported as such, never auto-resolved, never merged. litefactory does not attempt a three-way merge in this feature.
- Each generated Markdown file carries a human-facing provenance marker as its first body line (`<!-- litefactory: agent=<name> team=<slug> contentVersion=<n> — regenerate with `litefactory team update`; local edits are preserved but flagged -->`). The marker is advisory; the lockfile is the sole authority for drift.
- `team add --adopt` takes ownership of pre-existing files at owned paths by overwriting them and recording the generated hashes. Without `--adopt`, pre-existing untracked files at owned paths abort the whole command before anything is written.

### Rendering (per-harness templating)
- One renderer per harness behind a single contract: `Render(agent ResolvedAgent) ([]Artifact, error)`, where a renderer returns the repo-relative paths and bytes it wants written. A harness is registered in one map, mirroring the "a third tool is an additional map key, never a new case" convention in `internal/config`.
- Renderers are pure: no filesystem access, no network, no clock. Writing is done by the caller, after every renderer for the run has succeeded.
- A renderer that receives a field it cannot express **fails loudly** with the dotted path and the harness name. Silent dropping is forbidden — this is the guard against upstream harness format churn.
- Field mapping is per renderer: agnostic `tools` → Claude `tools` frontmatter list, → omitted for Codex (Codex does not restrict tools per agent), → opencode `permission` object entries.
- `--harness` selects which renderers run; default is every harness already installed in the repo (`.claude/` / `.codex/` / `.opencode/` present), falling back to `claude,codex` when none is detectable.
- Generated output is byte-deterministic for a given input: sorted keys, fixed field order, LF endings, single trailing newline. Two runs of `team add` on the same config produce identical bytes.

### `team manage` parameter surface
- Settable parameters are a closed allowlist of dotted paths, resolved against `--harness` when the path is harness-scoped:

| Param | Scope | Value constraint |
|---|---|---|
| `description` | agnostic | 1–500 chars, single line |
| `model` | agnostic or per-harness | non-empty, no whitespace, ≤ 64 chars |
| `tools` | agnostic or `claude` | comma-separated; each name must be in the known-tool list |
| `disallowedTools` | `claude` | as above |
| `color` | `claude` | known colour name |
| `sandboxMode` | `codex` | `read-only` \| `workspace-write` |
| `modelReasoningEffort` | `codex` | `low` \| `medium` \| `high` |
| `mode` | `opencode` | `primary` \| `subagent` \| `all` |
| `temperature` | `opencode` | float 0.0–2.0 |
| `permission.<capability>` | `opencode` | `allow` \| `ask` \| `deny` |
| `disabled` | agnostic | `true` \| `false` |
- Any other `<param>` is a usage error listing the allowlist. There is no raw pass-through of arbitrary `key=value` into a generated file.
- Values are additionally screened by a `forbiddenFlag()`-equivalent check before being accepted, so no parameter can produce a permission-skipping flag in any harness's output.
- Values are secret-scanned before being written to `.aiteam/team.json`.
- `team manage` writes only the repo-local layer. It never edits the embedded fallback or the org config.

### Ownership and safety
- litefactory writes only to the paths in "Owned Paths". Any computed output path outside them is a bug and aborts the command.
- Writes are atomic per file (temp file + rename) and the whole command is all-or-nothing: render everything, validate every target path, then write. A mid-run failure leaves no partial install.
- litefactory stages nothing and commits nothing; success output names the changed files and tells the user to review and commit them.
- Repo registration (`repo add`) is not a prerequisite; `team` commands operate on a clone on disk.

## Edge Cases & Error Handling

- Team slug not found in the resolved config → usage error listing available slugs.
- `teams[].agents[]` or `shared[]` names an agent not in `agents` → validation error with the dotted path; nothing is written.
- Agent name collides with an existing non-litefactory file at an owned path → abort listing every colliding path; suggest `--adopt`.
- `harnesses` contains an unknown key → load error naming the key and the three known harnesses.
- `promptPath` missing, unreadable, empty, or escaping its root → load error naming the file and the declaring layer.
- Two teams declare the same `slug` → load error naming both; slug uniqueness is enforced across all layers after merge.
- An agent appears in both `shared` and a team's `agents` → validation error (implicit membership makes it a duplicate).
- `.aiteam/team.lock.json` is absent but owned files exist → `team status` reports every one as `orphan`; `team update` refuses and points at `team add --adopt`.
- `.aiteam/team.lock.json` is corrupt or has an unreadable schemaVersion → hard error; never silently re-initialised, because that would erase the record of what the user customised.
- `team update` finds every artifact `modified` → writes nothing, exits non-zero, prints the full list plus the `--force` hint.
- `--force` is used → each overwritten `modified` artifact is named individually in the output before it is written.
- `--dry-run` on `add`/`update` → prints the exact per-file plan (`create` / `rewrite` / `skip (modified)` / `delete`) and writes nothing, including no lockfile.
- Target repo path is not a git working tree → precondition error.
- Target repo path is not writable → precondition error before any rendering.
- `team manage set` on an agent not in the resolved config → usage error listing known agents.
- `team manage set` with a harness-scoped param and no `--harness` → usage error naming which harnesses accept that param.
- `team manage set` value fails validation, forbidden-flag screening, or the secret scan → refuse, write nothing, and do not echo the offending value back.
- Renderer rejects a field it cannot express → whole command aborts naming agent, harness and dotted path; no partial write.
- Repo-local `.aiteam/team.json` at an older `schemaVersion` → in-memory migration for this run, warning printed, file untouched unless `--migrate-config`.

## Out of Scope

- Building, modifying, invoking or replacing the external `aiteam` npm tool. litefactory generates the owned paths itself; coexistence with a prior `aiteam` install is handled only by `--adopt`.
- Three-way merge or auto-reconciliation of a `modified` + `upstream-changed` artifact. Report only.
- Any runtime hook that makes litefactory's Go code choose which sub-agent a harness invokes. The generated files remain the only mechanism; `internal/harness`/`internal/codex` prompt construction is unchanged.
- Adding `opencode` to `agent.Kind` / `turn.RunnerSet` as a runnable harness. This feature only *generates files for* opencode; litefactory still runs Claude and Codex only.
- Reading, writing or repurposing the existing `AiteamTeam` registration column (`internal/repos/registration.go:123`) or `repos.DetectAiteam`. That value stays descriptive metadata.
- Regenerating or replacing `.claude/AGENT_ROSTER.md`.
- Adopting an external cross-tool standard (Agent Manifest, A2A Agent Card) as the schema.
- Any JSON-Schema validation library, any config-migration library, any templating library. Hand-written validation and `text/template`-or-simpler rendering from the stdlib.
- Distributing the fallback config outside the binary (registry, network fetch). `//go:embed` only.
- A `team remove`/uninstall command.
- Per-user (`~/.claude/agents/`) or machine-global agent generation. Repo-scoped only.

## Acceptance Criteria

- [ ] A JSON config with `schemaVersion`, `agents`, `shared` and `teams` loads into typed Go values; an agent referenced by two teams is decoded from a single registry entry.
- [ ] An unknown key at any nesting depth in any layer produces a load error naming its dotted path.
- [ ] Validation errors from a config with multiple faults are emitted in a stable, sorted order across repeated runs.
- [ ] Precedence test: a value set in all three layers resolves to the repo-local one; a value set only in the fallback survives an org layer that sets a different field of the same agent.
- [ ] A `disabled: true` override removes the agent from every team's resolved membership and from generated output.
- [ ] A document at `schemaVersion: 1` is migrated to current by sequential steps and loads; a document at current+1 fails with an upgrade message and loads nothing.
- [ ] `team list` prints every team, its agents and its shared agents, and writes no file (verified by a filesystem snapshot).
- [ ] `team show <team>` reports, per resolved field, which layer supplied it.
- [ ] Renderer contract test: a fake harness registered in the renderer map is exercised by `team add --harness fake` with no change to any other package.
- [ ] A renderer given a field it cannot express returns an error naming agent, harness and dotted path, and the command writes nothing.
- [ ] The Claude renderer's output for a known agent parses as YAML frontmatter with `name` and `description` present and `tools` rendered as Claude's comma-separated literal list.
- [ ] The Codex renderer writes `.codex/skills/<agent>/SKILL.md` with `name` and `description` frontmatter and omits tool restrictions.
- [ ] The opencode renderer writes `.opencode/agents/<agent>.md` with `description` frontmatter and a `permission` object derived from the agent's tools.
- [ ] Two consecutive renders of the same config produce byte-identical output for every artifact.
- [ ] `team add <team>` on a clean repo creates every expected artifact plus `.aiteam/team.lock.json`, with one lockfile entry per artifact carrying `renderedHash` and `sourceHash`.
- [ ] `team add` on a repo with a pre-existing file at an owned path aborts, names that path, and leaves the filesystem byte-identical.
- [ ] `team add --adopt` on that same repo overwrites and records the file, and a following `team status` reports it `clean`.
- [ ] `team add` where a renderer fails mid-run leaves the filesystem byte-identical (no partial install, no lockfile).
- [ ] `team status` on a fresh install reports every artifact `clean` and exits 0.
- [ ] Editing one generated file by one byte makes `team status` report exactly that artifact `modified`, the rest `clean`, and exit non-zero.
- [ ] Deleting one generated file makes `team status` report it `missing`.
- [ ] An extra file at an owned path with no lockfile entry is reported `orphan`.
- [ ] Changing an agent's definition in the repo-local layer makes `team status` report that agent's artifacts `upstream-changed` while their drift class stays `clean`.
- [ ] `team update` rewrites `clean` and `missing` artifacts, leaves every `modified` one byte-identical, and names each skipped path in its output.
- [ ] `team update` with every artifact `modified` writes nothing and exits non-zero.
- [ ] `team update --force` overwrites `modified` artifacts and names each one before writing.
- [ ] `team update --prune` deletes `orphan` artifacts; without it they are reported and left in place.
- [ ] `team update` rewrites lockfile entries only for artifacts it actually wrote; a skipped artifact's entry is unchanged.
- [ ] `team update --dry-run` prints a per-file plan and leaves the filesystem byte-identical, including the lockfile.
- [ ] A corrupt `.aiteam/team.lock.json` causes `team status` and `team update` to fail without rewriting it.
- [ ] `team manage <agent> set model=x` writes only `.aiteam/team.json`, leaves every generated file untouched, and `team manage get model` then resolves to `x` reporting the repo-local layer.
- [ ] `team manage <agent> unset model` removes the key so `get` resolves back to the lower layer's value.
- [ ] `team manage <agent> set <unknown-param>=v` is a usage error listing the allowlist, and writes nothing.
- [ ] `team manage <agent> set sandboxMode=…` without `--harness` is a usage error naming the harnesses that accept it.
- [ ] `team manage <agent> set temperature=9` is rejected by range validation.
- [ ] A value that trips the forbidden-flag screening is rejected and not written.
- [ ] A value that trips the secret scan is rejected, not written, and not echoed in the error message.
- [ ] Every `team` command is registered by a single constructor in `internal/cli/cmd_team.go` plus one line in `Commands()`; no switch and no mutable registry is introduced.
- [ ] No command in this feature invokes `git add`, `git commit`, `git stash` or `git push`, verified against a `gittest` fixture snapshot of the index.
- [ ] No command writes to any path outside "Owned Paths", verified by a full-tree snapshot diff around `team add` and `team update`.
- [ ] `make check` passes; `internal/config`'s purity test still passes (the team package carries any non-stdlib need, and this feature adds none).

## Assumptions

| # | Assumption | Based on |
|---|---|---|
| A-1 | The prompt body is a separate Markdown file referenced by `promptPath`, not a JSON string field. | Real agent bodies in `.claude/agents/*.md` are multi-KB prose with embedded comments; opencode's own `prompt` field is already a path (brief Domain §1). |
| A-2 | Three config layers (embedded → org → repo-local), not two. | Request names "corporate" and "repo" as distinct override sources; brief Domain §6 records that comparable CLIs all ship three-plus tiers. |
| A-3 | The fallback ships via `//go:embed`, not a runtime fetch. | Brief Open Question 5; `internal/store/migrate.go:20` is the established precedent and no other distribution mechanism exists in the tree. |
| A-4 | The registry-plus-references schema shape wins over the request's `{"Go Team": [...]}` sketch. | Brief Domain §3 recommends it; a JSON key as team name blocks team-level metadata and the sorted-order convention in `internal/config`. |
| A-5 | Drift is decided by a sidecar lockfile's hashes, not by parsing markers out of the generated files. | Only mechanism that works uniformly across Markdown, TOML and JSON outputs, and the only one that survives a user editing the marker itself. |
| A-6 | `sourceHash` and `renderedHash` are separate fields, giving two independent signals. | Brief Domain §5 splits schema-version drift from content/override drift; one hash cannot distinguish "we edited it" from "litefactory changed it". |
| A-7 | opencode gets a renderer in this feature despite having no litefactory runner. | Explicitly named in the binding context; the generated files serve humans working in the repo, not litefactory's own `Runner`. Brief Open Question 2 found no prior decision against it in `APP_CONTEXT.md`. |
| A-8 | `team manage` takes a closed parameter allowlist rather than an arbitrary key path. | Brief's security finding on `forbiddenFlag()`; a raw `key=value` pass-through into a generated harness file is exactly the shape that guard exists to prevent. |
| A-9 | `team manage` does not re-render; the user runs `team update`. | Keeps the write surface of `manage` to one file, so a bad parameter cannot corrupt an installed roster, and keeps drift classification a single code path. |
| A-10 | Conflicts (`modified` + `upstream-changed`) are reported, never merged. | No merge machinery exists in the tree; a wrong auto-merge of a system prompt is silent and expensive to detect. |
| A-11 | `team add` refuses pre-existing owned-path files unless `--adopt`. | The overridden ownership rule makes existing `aiteam`-scaffolded files the common starting state; overwriting them by default would destroy hand-authored prompts. |
| A-12 | The org layer's location comes from a new `team_config` key in `$LF_HOME/config.yaml` plus a `--team-config` flag. | `$LF_HOME/config.yaml` is the only machine-level config precedent; every other resolution order in the CLI is "flag overrides config" (`repoAddOptions`). |
| A-13 | Repo registration is not a prerequisite for `team` commands. | Nothing in the write path needs the store; requiring `repo add` would block the first-install case the feature exists for. |
| A-14 | The existing `AiteamTeam` column and `DetectAiteam` are left alone. | They are documented descriptive metadata that nothing reads back (`internal/repos/aiteam.go:93-105`); reusing them would couple a detection heuristic to a source of truth. |
