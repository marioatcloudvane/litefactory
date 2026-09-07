# Feature Spec: Agents Outside the Repository — Team-First, Mode-Aware Rosters
# Status: APPROVED
# Date: 2026-09-06

## Goal

- Resolve a repository's named agent team to only the specialists needed for the already-selected workflow mode, from litefactory-owned external bundle content, so classifiers and unrelated runs do not discover the full specialist catalog or copy it into every worktree.

## User Stories

| ID | Story |
|----|-------|
| US-1 | As an operator, I want a team-first JSON index with a description and one roster per workflow mode, so the configured team visibly states which agents are available in each state. |
| US-2 | As an operator, I want a mode roster entry to either reference one shared agent definition or declare a local Markdown source, so common specialists are defined once without preventing team-specific roles. |
| US-3 | As a coding-agent runner, I want to resolve the repository's team and the selected mode after classification, so I receive only that mode's external instruction bundle. |
| US-4 | As a maintainer, I want the existing v1 team index migrated deterministically to v2, so an installed index remains loadable while its all-mode legacy roster is explicitly visible and can be narrowed. |
| US-5 | As an operator, I want `team list` and `team show` to display rosters by mode and source, so I can audit exactly which specialist definitions a run may receive. |

## Data Model

| Entity | Field | Type | Required | Constraints |
|----|-------|------|----------|-------------|
| TeamIndex | schemaVersion | integer | yes | Current version is 2; positive; newer version is a hard load error. |
| TeamIndex | agents | object keyed by agent key | yes | Shared definition registry; keys match `^[a-z0-9][a-z0-9-]{0,63}$`. |
| TeamIndex | teams | object keyed by team key | yes | Team keys match the agent-key pattern; at least one team. |
| SharedAgent | source | string | yes | Non-empty, root-relative Markdown path; cannot be absolute or escape the resolved index root. |
| Team | description | string | yes | Non-empty, single-line human-readable description, maximum 500 characters. |
| Team | modes | object keyed by mode | yes | Keys are exactly `research`, `plan`, `implement`, `validate`, or `spike`; each present key has a non-empty ordered roster. |
| ModeRosterEntry | ref | string | one of ref/source forms | Existing key in `TeamIndex.agents`; cannot appear with `name` or `source`. |
| ModeRosterEntry | name | string | required with source | Agent key; unique within the mode roster; cannot appear with `ref`. |
| ModeRosterEntry | source | string | one of ref/source forms | Root-relative Markdown path under the resolved index root; cannot appear with `ref`. |

### Relationships

- `TeamIndex.teams[teamKey].modes[mode]` → ordered `ModeRosterEntry` values.
- A reference entry → `TeamIndex.agents[ref]`.
- A direct entry → its own `{name, source}` Markdown definition.
- A registered repository's declared aiteam team name → one `TeamIndex.teams` key.
- A classified `agent.Mode` → one roster in that team; roster resolution occurs only after classification succeeds.

### Canonical v2 Example

```json
{
  "schemaVersion": 2,
  "agents": {
    "research-orchestrator": { "source": "agents/research-orchestrator.md" },
    "codebase-tour": { "source": "agents/codebase-tour.md" },
    "web-researcher": { "source": "agents/web-researcher.md" },
    "go-implementation-engineer": { "source": "agents/go-implementation-engineer.md" },
    "go-unit-test-engineer": { "source": "agents/go-unit-test-engineer.md" },
    "go-code-reviewer": { "source": "agents/go-code-reviewer.md" }
  },
  "teams": {
    "go-team": {
      "description": "A team for standard Go development.",
      "modes": {
        "research": [
          { "ref": "research-orchestrator" },
          { "ref": "codebase-tour" },
          { "ref": "web-researcher" }
        ],
        "implement": [
          { "ref": "go-implementation-engineer" },
          { "ref": "go-unit-test-engineer" }
        ],
        "validate": [
          { "ref": "go-code-reviewer" },
          { "name": "test-evidence-verifier", "source": "agents/test-evidence-verifier.md" }
        ]
      }
    }
  }
}
```

## API / Interface Changes

| Method | Endpoint / Action | Auth | Notes |
|--------|------------------|------|-------|
| Package API | `teamsource.ResolveMode(teamName, mode)` | local | Resolves one validated, ordered roster and its source filesystem from the selected index tier; no renderer or write side effects. |
| Package API | mode-bundle renderer | local | Converts only the resolved roster to the vendor-specific external per-run bundle/arguments; Claude and Codex remain isolated behind their current runner packages. |
| Runner | classifier invocation | local | Receives no team specialist bundle; it retains only its fixed classifier prompt and controlled settings. |
| Runner | work invocation | local | Receives exactly the resolved team/mode bundle after `agent.Mode` is known; failure to resolve blocks the work invocation before a CLI starts. |
| CLI | `litefactory team list [--repo path] [--json]` | local | Shows teams, description, and agent keys grouped by mode; does not write agent files. |
| CLI | `litefactory team show <team> [--mode mode] [--repo path] [--json]` | local | Shows all mode rosters or one roster, marking entries as `ref` or `direct` and displaying resolved source paths. |
| CLI | existing materialization commands | local | Must not copy a complete team roster into `.claude/agents/` or `.codex/skills/` as part of this feature; replace or retire that behavior in favor of runner-owned external per-run bundles. |

## Business Rules

- The classifier never resolves, injects, or discovers team specialist instructions; mode classification precedes team-roster resolution.
- `research`, `plan`, `implement`, `validate`, and `spike` are the closed roster-key vocabulary and must match `internal/agent.Mode`; an omitted mode is unavailable for that team.
- A work request whose registered repository has no team, whose team is absent, or whose selected mode has no roster fails before invoking Claude or Codex and reports the missing team/mode and available alternatives.
- A mode roster preserves JSON array order; an agent key may occur at most once in a roster after reference resolution.
- An agent key may be reused across teams and modes only by `{ "ref": ... }`; a direct entry is local to its team/mode and may not shadow a registry key.
- Shared and direct Markdown definitions use the existing renderer/frontmatter contract; destination paths are renderer-owned and are not stored in the roster index.
- Source paths are resolved relative to the selected embedded, static, or registered-repository index source; source paths never select an ambient user skill or agent directory.
- The index-source precedence remains one complete source at a time: embedded default, then configured static path, then registered-repository override; fields and rosters from different sources are never merged.
- The runtime bundle is litefactory-owned, read-only to the coding-agent process, version-pinned to the selected index content, and outside the task worktree; it is not a writable sandbox root.
- Claude and Codex command construction must use their tested per-run external loading mechanisms and must not re-enable ambient personal configuration to discover the bundle.
- The built-in `go-team` roster is intentionally narrow: Research contains research orchestration, codebase analysis, and optional web research; Plan contains requirements, Go architecture, and product breakdown; Implement contains implementation and test authoring only; Validate contains read-only review, acceptance/test-evidence, and drift checks; Spike contains only the minimum experiment/research roles.
- `app-context-updater` is not in `implement` or strict `validate`; it remains an explicitly authorized maintenance/finalization capability outside this feature's strict validation roster.
- Go-specific implementation, testing, and review definitions remain separate language skills; a generic cross-language workflow role may select them, but this Go-only team must not load prospective Python or other language expertise.
- The feature must record benchmark inputs and results for current discovery versus per-mode bundles, including CLI version, team, mode, fixed prompt, elapsed time, and available token accounting; no token-saving claim is made without those measurements.

## Migration

- `schemaVersion: 1` indexes migrate in memory to v2 before strict v2 validation.
- The v1 top-level `shared` map and every v1 inline team agent map become v2 `agents` entries with the same keys and source paths.
- Each v1 team array entry becomes a v2 team object with description `Migrated legacy team <name>.`.
- Every migrated team's five mode rosters receive the same ordered union of its former inline agents followed by former shared agents, expressed as `ref` entries.
- The migration preserves v1's broad-discovery behavior but labels it as legacy through CLI output; it does not silently guess a narrower role-to-mode mapping.
- `team migrate-index` or the existing explicit index-migration path writes a v2 document only on explicit operator request; normal resolution does not rewrite an input index.

## Edge Cases & Error Handling

- Unknown field at any level → aggregated, stable, dotted-path validation errors; no best-effort decode.
- Invalid team key, agent key, mode name, description, or source path → validation error naming the exact path.
- Empty roster for a declared mode → validation error; omit the mode instead when the team does not support it.
- Entry with both `ref` and `source`, neither form, a missing direct `name`, or an extra field → validation error.
- Reference to an unknown agent, direct entry shadowing a shared key, or duplicate resolved key within one roster → validation error naming the team, mode, and entries.
- A source file missing, unreadable, malformed, or whose frontmatter identity conflicts with its roster key → mode-resolution error before runner launch.
- A repository's declared team not found in the selected index → precondition error naming available team keys.
- A valid team missing the classified mode → precondition error naming its declared modes; no fallback to another mode or all agents.
- A vendor's installed version cannot load the generated external mode bundle → precondition error with the vendor, version probe result, and supported configuration hint; no fallback to repository-wide agent discovery.
- Bundle creation failure or an external source hash change during bundle assembly → abort the run before invoking the coding CLI; do not reuse a partial bundle.

## Out of Scope

- A claim of a specific token or latency reduction before the benchmark is collected.
- Automatic classification of an agent into a mode from its Markdown text.
- Loading all teams, all modes, or all language skills as a fallback for an unknown task.
- Write-capable Validate, test authoring in Validate, production-code edits in Validate, or durable context-document updates in Validate.
- Personal user-level agent/skill discovery and changes to user-managed Claude or Codex configuration.
- Parallel full workflow stacks for languages not used by the selected repository/team.

## Acceptance Criteria

- [ ] A v2 index decodes only the team-keyed `teams`, shared `agents`, and per-team `modes` shape, with strict unknown-field and path-aware validation.
- [ ] `go-team` can declare different ordered specialist rosters for Research, Plan, Implement, Validate, and Spike, using both a shared reference and a direct Markdown entry.
- [ ] A valid classifier run constructs no external specialist roster or vendor agent/skill arguments.
- [ ] After classification, a Research request resolves only `go-team.modes.research`; an Implement request resolves only `go-team.modes.implement`.
- [ ] A missing team or mode prevents the coding-agent process from starting and produces an actionable error without falling back to all agents.
- [ ] A direct entry or a resolved reference with an invalid/missing source fails before the coding-agent process starts.
- [ ] No mode-bundle path is copied into a task worktree's `.claude/agents` or `.codex/skills` directory by this feature.
- [ ] Claude and Codex command-construction tests prove the selected mode bundle is passed through their supported per-run mechanism while ambient user configuration stays disabled/controlled.
- [ ] Migrating a valid v1 index produces a valid v2 index whose every mode roster preserves the former all-agent availability, and an explicit migration command is required to write it.
- [ ] `team list` and `team show` display a team description, available modes, roster order, reference/direct status, and source paths deterministically.
- [ ] The shipped Go roster puts only implementation and test-authoring roles in Implement, and does not place `go-code-reviewer` or `app-context-updater` there.
- [ ] A reproducible baseline-versus-mode-bundle benchmark record exists for both configured coding-agent CLIs without asserting a saving unsupported by its measurements.
