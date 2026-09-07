# Feature Spec: agents-outside-repo
# Status: APPROVED
# Date: 2026-09-07

## Goal
- Give the operator externally managed, reusable teams whose selected workflow mode exposes only its required specialists and skills, reducing instruction tokens across repositories and concurrent task worktrees.

## Evidence and Assumptions
| Item | Verified baseline / decision |
|---|---|
| Source baseline | Run 0008 refresh targets merged checkout `69f701c3ed45819d39b0a9fa05df64f29f08fbe6`; primary source is the current-session run 0007 `RESEARCH_BRIEF.md`, whose source review targets `a465e55e0f9ae6c61267f8fdbb4802526ac2867d`. |
| Available team feature | `internal/repos/aiteam.go` detects team names from repository markers; `internal/store/repos.go` stores `AiteamTeam`; `internal/cli/cmd_repo.go` registers and lists that association. |
| Available agent features | Vendor-neutral `agent.Runner`/`Request`; Claude and Codex adapters; configured vendor tiers and mode permission profiles; explicit mode selection and tool-free classifier. |
| Available team management | Merged `internal/team`, `internal/teamsource`, `internal/render`, embedded definitions, provenance/hash/drift/write engines and `team list/show/add/status/update/manage` commands are the implementation foundation; the integration prerequisite is satisfied. |
| Reuse and replace | Reuse #68's package ownership, strict aggregated errors, renderer, embedded definitions, drift/provenance and safe writes. Replace its flat team arrays plus implicit shared-agent inclusion, repository source tier, per-repo overrides and per-worktree materialization with explicit mode rosters and external ownership. |
| Available isolation | Claude managed configuration/home isolation is merged and must be reused. Exact classifier/work catalog isolation and Codex transport remain implementation capability gates. |
| Workflow content gap | Embedded and Claude Implement definitions retire app-context updating; the Codex copy still invokes it. Resolve this confirmed parity drift using embedded team definitions as canonical shipped content, with generated vendor copies verified against them. |
| Token evidence gap | Current Claude rendering copies full specialist bodies into root instructions as well as standalone files; benchmark that exposure separately from catalog descriptions. No real vendor isolation probe or token benchmark has yet passed. |
| Accepted user decisions | Teams belong outside any project; team-first mode rosters; shared definitions or Markdown paths; token reduction is primary; no backward compatibility or configuration migration. |
| Headless assumptions | One operator-owned registry at `$LF_HOME/teams/index.json`; external source tree under `$LF_HOME/teams/`, metadata under its `.litefactory/` directory, immutable snapshots under `$LF_HOME/bundles/`; explicit repository association; five current modes; no maintenance/updater restoration. |
| Scope interpretation | Replace repository-owned specialist discovery in managed runs; retain repository-specific instructions, application context, constraints and session artifacts. |
| Run 0010 clarification | Central ownership was already in US-1–US-3; make source installation, storage layout, CLI delivery and snapshot cleanup explicit acceptance requirements without adding a duplicate registry story. Current-session run 0009 research confirms linked remote stories are stale; local revision alone does not synchronize them. |
| Lifecycle assumption | Explicit `team add` installs shipped defaults or imports an operator-selected package into the managed central tree; work runs never load directly from the original package. Persist identity/provenance with existing run records; retain finalized bundles by default until safe vendor restoration/resume is proven. |

## User Stories
| ID | Story |
|---|---|
| US-1 | As an operator, I can define teams and shared specialists in one external registry with explicit mode rosters, so multiple repositories reuse the same instructions. |
| US-2 | As an operator, I can install, import, update, inspect and assign centrally stored agents and teams through litefactory, so repositories share one managed source tree. |
| US-3 | As an operator, I get isolated per-run team/mode bundles delivered from central storage to the selected CLI after classification with safe retention and cleanup, so unrelated specialists and personal configuration do not enter a managed run. |
| US-4 | As an operator, I get Implement focused on code and test authoring, and Validate focused on read-only review and evidence, without restoring the retired context updater. |
| US-5 | As an operator, I can inspect bundle provenance and compare instruction/token evidence across modes, so performance claims are measurable and reproducible. |

## Data Model
| Entity | Field | Type | Required | Constraints |
|---|---|---|---|---|
| Registry | schema_version | integer | yes | Exactly 2; unsupported or legacy structures rejected with replacement guidance. |
| Registry | teams | map of team ID to team | yes | Nonempty; unique bounded identifiers. |
| Registry | shared | map of shared ID to definition | yes | Reuse existing shared vocabulary; may be empty; no recursive references or implicit inclusion in all teams. |
| Team | desc | string | yes | Nonempty bounded description. |
| Team | research, plan, implement, validate, spike | ordered roster arrays | yes | Missing roster invalid; explicit empty roster permits root-only operation. |
| Roster entry | ref | string | alternative | Exactly one of shared `ref` or inline definition; referenced shared definition must exist. |
| Agent definition | id, desc, path | strings | yes | Unique effective ID per roster; Markdown path resolved from external registry directory, never task CWD; regular readable bounded file outside project roots. |
| Agent definition | tier | capability tier | yes | Reuse existing tier vocabulary and vendor model mapping; no independent model registry. |
| Agent definition | skills | ordered Markdown references | yes | May be empty; descriptions discoverable, body loaded only when needed; external regular files and bounded total size. |
| Repository association | team ID | string | yes for managed runs | Operator-selected registry key persisted with repository registration. |
| Bundle snapshot | team, mode, content digest, selected IDs, input bytes | immutable values | yes | Covers resolved instruction and skill content; reproducible for duration of run; no mutable shared runtime directory across concurrent runs. |

### Relationships
- Registry team → five mode rosters → shared or inline agent definitions → selectively loaded skills.
- Skill frontmatter `requires` and `resources` → explicit relative transitive skill/resource closure; reject cycles, missing resources and path escapes, preserve relative trees and hash every included byte.
- Registered repository → operator-selected external team ID; association does not require writing a marker into the clone.
- Resolved mode and team → immutable run bundle → existing vendor runner; classifier receives no bundle.

### Central Storage and Lifecycle
| Location | Owner and purpose | Lifecycle |
|---|---|---|
| `$LF_HOME/teams/index.json` | Operator-owned version-2 team/mode registry, shared across all registered repositories | Created through explicit installation; validated and updated through team commands. |
| `$LF_HOME/teams/agents/shared/<id>.md` | Canonical installed shared agent source referenced explicitly by rosters | Installed/imported once; shared updates affect future runs of all referring repositories. |
| `$LF_HOME/teams/agents/<team-id>/<id>.md` | Canonical installed direct agent source | Team-scoped identity; direct paths resolve from the registry, never a repository CWD. |
| `$LF_HOME/teams/skills/<skill-id>/` | Installed `SKILL.md` and its declared dependencies/resources | Copy the complete validated closure; preserve relative resource paths. |
| `$LF_HOME/teams/.litefactory/` | Source origin, content hashes, override records and management locks | External management state; no duplicate registry in the database. |
| `$LF_HOME/bundles/<forge>/<owner>/<repo>/<issue>/<run-ordinal>/` | Litefactory-owned immutable selected-mode sources and vendor renderings | Publish before work launch; retain finalized bundles until vendor resume/restoration is safe; permitted removal requires stopped children and durable identity/provenance. |
| Existing repository/run records | Explicit team association and bundle identity/evidence | Associations store keys rather than source bodies; run identity survives bundle cleanup. |
- Agent and skill subdirectories above are the documented installed layout; imported references are rewritten/normalized to contained managed paths while preserving dependency-relative trees.
- Empty central registry → actionable explicit installation guidance before work launch; no silent embedded or repository fallback.
- Bootstrap example → `litefactory team add go-team`, `litefactory team validate`, then `litefactory repo add /path/to/repo --team go-team`.
- Custom-package example → `litefactory team add go-team --source /path/to/external-package`; read `index.json`, validate the selected closure, copy it centrally and record its origin for later status/update.

## API / Interface Changes
| Method | Endpoint / Action | Auth | Notes |
|---|---|---|---|
| CLI | `litefactory team validate` | local operator | Strict registry and file validation without modifying registry or projects. |
| CLI | `litefactory team list` | local operator | Show team IDs and descriptions independent of current repository. |
| CLI | `litefactory team show <team> --mode <mode>` | local operator | Effective ordered roster, source provenance and byte counts; no model invocation. |
| CLI | `litefactory repo add <path> --team <team>` | local operator | Reuse registration/update workflow, persist explicit association; remove requirement to install aiteam in repository. |
| CLI | `litefactory repo list` | local operator | Display assigned external team; no inference from Markdown prose. |
| CLI | Existing `team add/status/update/manage` command family | local operator | Evolve #68 commands for operator-owned external installation, drift/update and overrides; remove repository-required team-management scope and all-worktree copying. Keep list/show independent of repository CWD. |
| Runtime | Mode selection then team resolution | existing owner authorization | Reuse five-mode selection, configured runner choice and permission profiles; reject missing team/mode before work subprocess. |

## Business Rules
- Registry editing belongs to the operator; managed agents receive read-only external instruction material and cannot update shared definitions.
- Team management is independent of a repository; the database stores only the repository association and existing run metadata, not a competing registry.
- Runtime has one active external registry; embedded definitions are installed explicitly, without embedded/static/repository precedence or fallback. Operator imports copy complete dependencies from external source roots into the managed tree.
- Registry metadata owns identity, description and tier; repeated Markdown frontmatter must agree. Absolute runtime paths are allowed only beneath the canonical registry root; shared override IDs and team-qualified inline override IDs are unambiguous.
- New explicit team association remains distinct from marker-derived `AiteamTeam`; an additive SQLite migration for association/segment metadata is permitted and does not constitute legacy team-config migration.
- External management retains safe-write preflight, locks, truthful override provenance and five drift states; source capture shares the management lock only while producing a stable snapshot.
- Roster membership grants instruction availability only; it cannot widen existing tool ceilings, filesystem authority, network permissions or mode restrictions.
- Classifier is isolated from team agents, team skills and repository specialist catalogs; use the existing classifier interface and selection precedence.
- Each work run sees only its selected mode roster; neither vendor silently unions personal, repository or other team catalogs into it.
- Keep project instructions and session conventions available without embedding the entire global specialist roster in project bootstrap files.
- Reuse existing tiers, runner adapters, repository registration, mode persistence, session machinery and profile validation instead of introducing parallel implementations; adopt #68 team/teamsource/render ownership and evolve its existing commands, locks, hashes, drift states and safe-write logic for external targets.
- Root workflow roles remain small; language-specific behavior is supplied through focused skills; provide the current Go team without adding Python or other full team stacks.
- Implement authors production code and tests, runs the tests needed to verify its changes, and reports completion; dedicated review, context updating and environment administration leave its orchestration.
- Validate performs acceptance review, language-aware code review, test-evidence assessment and application-context drift detection; verdict and findings are inline, with no code, test or durable documentation edits.
- Validate uses existing evidence and read-only inspection; the current Claude profile does not permit shell execution, so running tests is not part of this scope. Missing test execution or required artifacts is reported as missing evidence, not a pass.
- Review findings return to the owner for a later Implement instruction; no automatic fix loop or new mode transition is introduced.
- Do not restore app-context-updater, its tier entry, or its automatic workflow invocation; any future writable maintenance feature requires a separate scope decision.
- Changing mode, team or bundle digest cannot resume a conversation containing a different specialist catalog; open a fresh existing conversation segment when necessary.
- Configuration cutover is deliberate: no legacy schema reader, fallback to repository team definitions, migration command or inferred conversion.

## Edge Cases & Error Handling
- Unknown team, malformed registry, duplicate effective agent ID, invalid mode key, unresolved reference, unreadable/oversized/nonregular source, or ambiguous inline/reference entry → actionable refusal before spawning work.
- Relative paths → resolve against registry location; symlink escape from permitted external source roots → refuse; URL downloads are unsupported.
- Registry changes during a run → current immutable snapshot remains stable; next run observes a newly validated snapshot.
- Original import package moved or unavailable → installed central definitions remain usable for runs; status/update reports unavailable upstream rather than treating it as synchronized or deleting installed content.
- Work exit, launch failure or cancellation → clean failed unpublished staging after child processes stop; retain finalized bundles until safe vendor resume/restoration is proven. Permitted finalized cleanup requires durable metadata; metadata failure retains the bounded bundle. Never remove source definitions or another active run's bundle.
- Interrupted cleanup → report the bounded leftover path; later recovery removes only validated factory-owned inactive run paths, without broad deletion or a new clock/worker service.
- Eligible conversation resume after cleanup → reconstruct a bundle from current central sources and compare persisted identity. Changed/missing identity or unrestorable vendor path references require a fresh segment; matching hashes alone do not prove resume safety.
- Same team used concurrently in five worktrees → independent runtime snapshots, shared read-only source, no roster contamination or external writes by agents.
- Existing registration has only a marker-derived team → require explicit operator reassignment; do not silently reinterpret it as an external-registry association.
- Vendor cannot enforce selected catalog with supported CLI surface → fail capability validation with a precise limitation; never fall back to loading all agents.
- CLI authentication must remain functional under isolation; shared instruction isolation must not depend on granting personal configuration access.
- Actual token counts unavailable from a runner → report input-byte estimates separately and label token/latency evidence unavailable.

## Out of Scope
- Backward compatibility, registry migration, remote registry fetching, multi-user service, automatic publishing and arbitrary plugin installation; delete the unused legacy schema-migration path from #68 as part of the schema replacement.
- New classifier mode named Maintenance, updater resurrection, automatic review/remediation loops, writable Validate, or test/environment provisioning inside Validate.
- Shipping complete non-Go language teams; introducing a second model/tier configuration or replacing the existing runner/session abstractions.
- Claiming token savings from disk usage or claiming universal reduction in total task tokens before measurement.

## Acceptance Criteria
- [ ] A checked-in example external registry shows a described Go team with all five mode rosters, one shared reference, one direct Markdown definition and focused skill references; installation copies operator-selected definitions outside repository roots.
- [ ] A repository with no team files can be registered with `--team`; two repositories and five task worktrees reuse the same registry without committing team definitions.
- [ ] Strict validation rejects unsupported schema and invalid references before any work subprocess or partial registry update.
- [ ] Classifier fixtures prove zero team specialists/skills exposed; each vendor's work fixtures prove exact selected mode membership and no ambient specialist catalog.
- [ ] Repository-specific instructions and session artifact conventions still work after managed specialist discovery is externalized.
- [ ] Tier resolution, permissions, mode selection, runner choice and provenance reuse their current owners; roster content cannot elevate authority.
- [ ] Changing bundle identity prevents stale-specialist conversation reuse; concurrent runs cannot overwrite each other's bundle snapshots.
- [ ] Implement roster contains implementation/test roles; Validate exposes review/evidence/drift roles and produces no tracked changes; neither includes the retired app-context-updater.
- [ ] A reproducible benchmark compares current full catalogs with selected mode catalogs for classification and representative work runs, separately accounting for Claude root-body duplication and catalog descriptions, recording bytes, available input/cached/total tokens, latency, vendor/version and task outcome without promising an unmeasured savings percentage.
- [ ] Embedded team definitions are canonical shipped content; generated Claude/Codex copies, workflow scaffolding and tests are updated together, remove all Implement updater invocations and preserve equivalent user-visible behavior.
- [ ] Implementation reuses the merged team schema/resolver/renderer/write owners and Claude isolation infrastructure directly; no branch-integration prerequisite or parallel registry implementation remains.
- [ ] From an empty isolated `$LF_HOME`, explicit default installation or custom-package import creates a complete usable registry, agent sources, skill resources and provenance outside project roots; rejected imports leave no advertised partial team.
- [ ] Two repositories use one installed shared agent source; changing it through external management affects their next validated bundles while an active run retains its original bytes. Moving the original import package does not break installed runs.
- [ ] Both vendor adapters receive only the selected immutable central snapshot while executing in the supplied task worktree; no installation step copies managed specialists into project instruction directories.
- [ ] Success, spawn failure, cancellation and recovery tests prove bundle cleanup preserves durable run identity, active/sibling bundles and all central definitions; a later resume reconstructs and compares identity before reusing conversation context.
