# Research Brief — agents-outside-repo

## Request

[O] Run 0007: “ok, i have now merged down the changes, re-check them!” Recheck the merged implementation against the current session's five-story plan. Retain the owner's decisions: teams are managed outside individual repositories, mode-specific specialist availability should reduce instruction tokens, and no backward compatibility is required. This Research run assesses the plan; it does not implement or rewrite the plan/stories.

## Codebase Context

### Citation base and evidence

[V] Source review targets `a465e55e0f9ae6c61267f8fdbb4802526ac2867d`. The new `internal/team`, `internal/teamsource`, `internal/render`, team management commands, and Claude configuration isolation are present in this checkout. The previous plan's claim that the task checkout lacks that implementation is obsolete (`plan_brief.md`, Workflow and Provenance). Package/API adaptation can now target the actual merged source rather than a separate branch hypothesis.

[V] Root-agent read-only ancestry checks returned exit 0 for `git merge-base --is-ancestor 16ac43633e8d2d18deaf7560487d51f1f40de62c HEAD` (the planned #68 baseline) and for `24ddd50e96a2e6b20fcb19b6ac7b88dbcb2c341d` (the planned main baseline). Both inspected lineages are integrated. HEAD has parents `0e2d34916bd1d77a954a2bc6eeb3efd94e800b48` and `8589b1f6bf935928047c1478f6fcdae0de4e637f`.

[V] The plan's Story Index contains issue links #127–#131. Its final provenance paragraph still says remote issue URLs do not exist; that sentence is stale. This verifies recorded links only, not remote issue content or synchronization. The manifest is corrected accordingly; plan and story artifacts remain untouched in this Research run.

[V] Two-pass source review by the codebase-tour specialist supplied the source findings below. Root-agent diagnostics passed `go test ./internal/team/... ./internal/teamsource/... ./internal/harness/... ./internal/config/...` and `go test ./internal/cli/... ./internal/turn/... ./internal/codex/...`. Some results were cached. No full `make check`, race suite, real vendor CLI probe, or token benchmark was performed. These checks support the merged baseline's tested behavior, not completion of #125.

### What can be reused now

[V] Existing package boundaries are a useful foundation: `team` owns schema/provenance, locks, overrides, drift classification and guarded writes; `teamsource` composes source loading and rendering; `render` translates Markdown into vendor artifacts; CLI commands currently choose rosters and destinations. Mode selection remains in `daemon`; process execution remains in the vendor adapters. Reuse those owners rather than add a second registry implementation.

[V] Strict unknown-field checking already covers each schema level (`internal/team/decode.go:18`, `:89`, `:132`). Destination paths are re-derived and guarded (`internal/team/paths.go:80`); writes reject symlinks and publish by atomic rename (`internal/team/writefile.go:18`, `:70`, `:123`). Override reads validate containment, extension, regular-file status and symlinks (`internal/team/overrides.go:124`, `:239`). These are reusable safety mechanisms whose root assumptions must change with external ownership.

[V] Claude's managed configuration/home isolation has landed (`internal/harness/claudehome.go:72`, `internal/harness/env.go:99`). It provides process isolation infrastructure; it does not resolve or inject a mode-specific team.

### What the current team implementation actually does

[V] The schema remains `Index{Teams []TeamEntry, Shared map[string]AgentEntry}`. Teams have `Name`/`Agents`; agents have `Source`/`Dest`. It does not yet define team descriptions, mode rosters, explicit shared references, skill dependency closure or a schema version (`internal/team/types.go:16`, `:24`, `:32`). The CLI automatically unions **all shared agents** into every team's roster and sorts by key, with no mode argument (`internal/cli/cmd_team.go:296`). Semantic validation rejects duplicate keys across teams and shared definitions (`internal/team/validate.go:16`), which must be adapted for explicit reuse and team-qualified inline identities.

[V] Sources can already come from embedded defaults, a configured static directory or a registered repository override (`internal/teamsource/resolve.go:69`, `:104`, `:137`, `:175`). This is external source reading, not external team management. Static/registered source loading still calls `team.Migrate` (`internal/teamsource/resolve.go:80`); the owner's no-compatibility decision means this reader must leave the proposed supported path rather than be expanded into migration work.

[V] Installation still targets repository `.claude/agents/`, `.codex/skills/`, `.opencode/agents/`, root instructions and `.litefactory` metadata (`internal/team/paths.go:9`, `:23`, `:112`). Management commands describe repository installation/maintenance (`internal/cli/cmd_team.go:1303`, `:1331`, `:1361`, `:1406`). Overrides remain scoped to repository/agent and use a Markdown source inside that repository root (`internal/team/overrides.go:124`). Merely relocating the source directory will therefore not satisfy external ownership.

[V] `config.Paths` has Claude configuration locations but no teams-registry path (`internal/config/paths.go:94`). Stored `AiteamTeam` is detected membership (`internal/store/repos.go:32`); registration still assumes prior installation and marker-derived membership (`internal/cli/cmd_repo.go:57`, `:152`). Explicit selection of an external team remains new work, distinct from that detected metadata.

## Domain / Problem Space

### Token scope remains the central gap

[V] Claude rendering produces a standalone agent file **and includes the full agent body in `CLAUDE.md`** (`internal/render/claude.go:46`, `:95`). Thus the benchmark must account for root-instruction body exposure as well as specialist catalog descriptions. This run verifies emitted content, not the exact number of tokens any installed CLI consumes.

[V] Codex rendering creates a standalone skill, rejects model/tool metadata and emits no corresponding root section (`internal/render/codex.go:31`, `:62`). Its existing renderer cannot simply be treated as proof of native external-agent transport or tier semantics. Reuse parsing/rendering deliberately while preserving the distinction between skills and agents.

[V] Classification is already a separate stateless invocation but runs in the registered clone (`internal/daemon/mode.go:89`). Claude classification uses an empty tool set, managed settings/home and strict MCP configuration (`internal/harness/runner.go:27`). Codex classification constructs ignore-config/ignore-rules/read-only arguments (`internal/codex/runner.go:106`). These source facts do not prove the installed vendors accept every argument or load no project/specialist instructions. Tool restriction alone is not catalog isolation.

[V] The vendor-neutral request includes mode, working directory, session, prompt and permissions, but no resolved bundle content/identity (`internal/agent/request.go:62`). Mode-resolved bundle publication, transport and conversation identity remain necessary. Codex configuration still documents coupling between personal-config suppression and repository-skill discovery, with a managed home described as future work (`internal/config/config.go:178`).

### Workflow content and Validate

[V] Validate already exists as a closed workflow mode (`internal/agent/mode.go:18`) and is defined as bounded read-only verification with no workflow artifact (`internal/daemon/mode.go:66`). Tests enforce Claude tools `Read, Glob, Grep, Task`, allowed tools `Read, Glob, Grep`, reject Bash/Edit/Write, and require Codex read-only execution (`internal/config/toolprofiles_test.go:21`). Review and assessment can fit that authority; executing tests or writing review reports cannot silently be added to the current Claude profile.

[V] Implement still includes review, environment setup, testing, a bug loop and drift-report writing (`internal/team/embedded/teams/shared/implement-orchestrator.md:252`, `:279`, `:326`, `:393`). The proposed slimmer Implement has not landed merely because team management did.

[V] There is material workflow parity drift: embedded and Claude Implement definitions retire context maintenance (`internal/team/embedded/teams/shared/implement-orchestrator.md:113`, `.claude/agents/implement-orchestrator.md:113`), while the Codex copy still schedules and invokes the updater (`.codex/skills/implement-orchestrator/SKILL.md:126`, `:140`, `:432`). “Updater retired” is therefore not yet true across all supported instruction copies. Content consolidation must resolve this before comparing behavior or tokens across vendors.

## Constraints & Risks

- [O] No backward compatibility is needed. Remove unsupported legacy loading from the target design; do not reintroduce a converter or compatibility story. Ordinary additive persistence migrations are a different concern.
- [V] The existing all-shared union, root-body rendering and repository-owned destinations would undermine the proposed instruction reduction if retained unchanged.
- [V] Current path/override protections assume repository ownership. Retargeting to an operator-owned registry and immutable per-run bundles needs explicit containment, symlink, concurrency and cross-run isolation coverage; existing passing tests do not cover the new ownership model.
- [V] Claude configuration isolation is reusable but insufficient by itself to establish empty classifier catalogs or exact mode-only availability. Codex transport likewise remains unproven by this review.
- [V] Workflow parity drift could produce different actions and token costs even with equivalent rosters. The shipped canonical definitions and their generated copies need explicit agreement.
- [R0] The current plan's required transport/version probes and measured token comparison remain valid gates. No percentage savings or universal reduction in total task tokens is established; delegation and supporting-skill loading can offset catalog savings.

## Open Questions

1. Which exact supported CLI versions and transports demonstrably preserve authentication/project instructions and permission ceilings while exposing only the selected mode bundle? Implementation probes must settle this; no new upstream support claim is made here.
2. Which definition is canonical during the content cutover, and how will embedded/Claude/Codex copies stay synchronized? The observed updater discrepancy makes this a concrete content decision.
3. What classifier and work-run input-token reductions remain after removing Claude root-body duplication and resolving actual mode/skill closure? Measure both baseline sources of exposure independently; report total workflow cost as well as initial input cost.
4. Are the recorded issue bodies synchronized with the local current-session plan? Their links are populated, but no remote content was fetched in this run. The plan's pre-merge and unpublished-link prose also needs correction before implementation consumes it.

## Recommended scope for Plan mode

[V] The merged code supplies the anticipated reusable foundation. Source inspection and ancestry checks satisfy the prerequisite to integrate both planned baselines; the next plan refresh should pin this HEAD and replace the obsolete “checkout lacks team management” instruction. It should retain the five-story scope rather than restart team management from scratch:

| Story | Reuse now verified | Remaining #125 work / adjustment |
|---|---|---|
| US-1 / #127 | Schema validation and source-loader owners | External versioned registry, explicit mode rosters/references, skill closure; replace implicit shared union and legacy loading. |
| US-2 / #128 | Management, overrides, drift, safe writes | External output/provenance ownership and explicit repository association; adapt repository-root assumptions. |
| US-3 / #129 | Separate classifiers, common request boundary, Claude home isolation | Proven transport, neutral classifier context, stable immutable bundles and conversation identity. |
| US-4 / #130 | Existing mode definitions and read-only Validate policy | Slim Implement, fit review to Validate authority, focus language skills, resolve confirmed updater parity drift. |
| US-5 / #131 | Focused package tests and deterministic render infrastructure | Actual token/isolation evidence; explicitly include Claude full-body root composition in the baseline. |

[O/R0] Keep the operator-owned `$LF_HOME/teams` direction, explicit repository team selection, no compatibility cutover, small workflow roles with selected language skills, and read-only Validate. These are prior owner/plan decisions, not features found implemented by this source review. No code, story scope, branch merge, runtime configuration or remote issue was changed in this run.
