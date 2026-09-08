# Research Brief — agents-outside-repo

## Run 0014 — feature scope and default discovery URL

### Request and binding owner decision

[O] Explain exactly what this feature adds. Provide an operator-configurable location for the remote teams JSON file in `config.yaml`; when the setting is absent, use exactly `https://github.com/marioatcloudvane/litefactory/teams/teams.json`.

[O/R0] This supersedes run 0013's decision to require explicit source configuration when no verified public default was known. Remote distribution remains primary; neither an embedded catalog nor a local directory becomes an automatic fallback. This is a Research clarification, not an implementation or a rewrite of the six story bodies.

### What the complete feature adds

1. [R0] Independently released team content: fetch a discovery index and its versioned, integrity-checked agent/skill payloads over HTTPS, without rebuilding litefactory. Independent content publishing is a deliverable (US-6), not merely a downloader assumption.
2. [V/R0] One centrally managed installed catalog under `$LF_HOME/teams`, reusable by multiple repositories and worktrees. Repositories select a team; they do not own its definitions. The central foundation exists (`internal/config/paths.go:67`, `internal/cli/cmd_repo.go:55`).
3. [V/R0] Explicit ordered rosters by mode, shared references or direct Markdown definitions, tiers and associated skills. Schema and snapshot selection exist (`internal/team/types.go:8`, `internal/teamsource/snapshot.go:45`); native per-run delivery is still planned. The intended token improvement comes from exposing only the selected mode's dependency closure and keeping classifiers free of the team catalog, not from moving files on disk alone.
4. [R0] Immutable per-run bundles delivered to the configured CLI, with provenance and conversation boundaries preventing an old run from silently adopting changed instructions. Keep the mode orchestrators and the main roster correction; do not replace sequencing with the small workflow skill.
5. [R0] Explicit remote add/update and refresh, content versions and pins, installed-versus-upstream reporting, and local-change protection. Preserve the five byte-drift states and report remote freshness/version separately. Ordinary runs use installed content without fetching and remain usable offline.
6. [R0] Corporate distribution through the same configurable HTTPS source protocol. Validate paths, TLS/redirects, sizes and checksums before publishing installed content; a failed download or integrity check must not replace a valid installation. Same-source checksums provide integrity, not independent publisher authentication.

[R0] Implement remains focused on implementation and tests; Validate retains review in its agreed read-only scope. Broader maintenance/updater authority, automatic downloads during runs, backward compatibility and measured token-saving claims are not included.

### Current code versus intended outcome

[V] The codebase-tour specialist verified central installation/management, explicit repository selection, schema-v2 rosters and bounded snapshots. `team add` still defaults to embedded content (`internal/cli/cmd_team.go:139`, `internal/teamsource/manage.go:285`, `internal/team/embedded.go:12`). Remote URL configuration, network acquisition and independently versioned remote updates are absent. External-team classifier/work execution still fails explicitly because per-run delivery is unavailable (`internal/daemon/mode.go:90`, `internal/turn/prepare.go:100`). No runtime token savings can yet be claimed.

[V] Existing `settings.team_config` is an absolute local-filesystem-path setting, not a URL field (`internal/config/config.go:90`, `internal/config/validate.go:115`); `Config` currently has no `teams` block (`internal/config/config.go:46`). Do not instruct operators to paste the requested URL into that old field.

### Configuration requirement for the next Plan handoff

[O] The requirement is a configurable discovery-file URL plus an automatic built-in URL default when omitted. [Recommendation] A clear minimal user-facing spelling is:

```yaml
# Proposed configuration; not supported by the current binary.
teams:
  index_url: "https://github.com/marioatcloudvane/litefactory/teams/teams.json"
```

Omitting `teams.index_url` should be equivalent to this value. A corporate operator can supply their own HTTPS index URL instead. This is a proposed key name, not an owner-prescribed spelling or a claim that the parser already supports it. Run 0013's `teams.sources`/`teams.default_source` alias design must be reconciled with this simpler default-source requirement in Plan; do not introduce competing defaults. Retain an installed team's resolved source identity so changing a configuration default does not silently retarget existing installations. An invalid explicit source must fail clearly rather than silently switch to the public source.

### Endpoint verification and risks

[V] Both direct browsing and the web-researcher could not retrieve the exact requested URL: the browsing tool returned cache/internal errors, not a confirmed HTTP 404. Whether it currently serves machine-readable JSON is **unverified**. Preserve the owner's literal requested default in the requirement; do not silently substitute another endpoint or claim it is live.

[V] GitHub documents raw retrieval using its repository-contents API with `Accept: application/vnd.github.raw+json`, and distinguishes browser URLs from raw download URLs: [GitHub repository contents API](https://docs.github.com/en/rest/repos/contents#get-repository-content), checked 2026-09-07. A raw-download example for the intended file is `https://raw.githubusercontent.com/marioatcloudvane/litefactory/main/teams/teams.json`, but the branch/file and endpoint were not verified. This is an example, **not** an approved replacement default. Publishing must verify the actual JSON endpoint and referenced payloads; any canonical URL change or cross-origin resolution needs explicit treatment in the source/trust contract, not unrestricted redirect following.

[O/R0] The prior Codex EPERM was attributed by the owner to the outer sandbox and must not remain an adapter-coding prerequisite. No nested vendor probe was rerun in this research. Suitable-environment CLI acceptance evidence is still necessary before claiming delivery works.

### Open questions and handoff

- Publishing/Plan must establish that the requested default serves the discovery document, or explicitly reconcile it with a canonical raw endpoint. Current tool failure does not settle availability.
- Plan must settle the final configuration key/alias precedence, apply the mandatory omitted-setting fallback to US-1/US-2 and add verification of the default endpoint to US-6. This run records the superseding requirement without claiming those plans or production code were changed.
- End-to-end CLI delivery, remote publishing and token measurements remain outstanding. This run changed only this brief and the manifest; no tests, runtime configuration edits, remote issue updates, commits or pushes were performed.

The sections below are historical. In particular, run 0012's capability-gate wording is superseded by the owner's run 0013 correction above.

## Run 0012 — why CLI delivery is incomplete

[V] Checked the committed run 0011 evidence in `RUNNER_CAPABILITIES.md` and `DRIFT_REPORT.md` at `a0f7b7563b18cbae7a9c19af72713b434fb795d3`. This explanation does not rerun probes or claim a more precise operating-system cause than the recorded error supports.

- [R0] The authenticated Codex 0.153.4 scratch probe exited before inference with `failed to initialize in-process app-server client: Operation not permitted (os error 1)`. The failing startup prevented testing external-agent discovery, authentication and delegation. It does not establish that Codex cannot accept external definitions, nor identify which syscall or environmental restriction caused the denial.
- [R0] Three bounded Claude probes found a candidate that exposed the selected agent/skill while excluding the tested ambient sentinel and retaining explicitly appended project instructions. They did not execute delegated agents or verify delegated tiers/permissions, concurrent catalogs, all ancestor discovery cases, or credential refresh. Therefore removing the Codex startup barrier alone would not finish the remaining proof matrix.
- [V] The approved US-3 task ordering makes real capability evidence a prerequisite to production transport choices and runtime enablement. Implementation stopped at that gate rather than delivering the adapter and lifecycle work without its required evidence. This was a conservative implementation decision under the approved plan, not a claim that every remaining coding task was physically impossible.
- [R0] US-1/US-2 central storage and management were delivered. Native CLI rendering/delivery, run bundle publication and conversation identity handling in US-3 remain unfinished; US-4 workflow conversion and US-5 runtime evidence depend on them. The implementation explicitly refuses assigned external-team runs rather than silently running without the selected team.

The next step is a bounded diagnosis/reproduction of the Codex startup failure in an environment permitted to run it, followed by the remaining Claude/Codex capability checks and actual US-3 wiring. Completing storage does not complete delivery, and a successful startup probe alone will not supply the missing code. No new authority is needed to explain this state; no subprocess, source change, live configuration edit or permission bypass was attempted in run 0012.

## Request

[O] Run 0009: “re-try please!” [V] This Research retry checks whether run 0008's local plan refresh persisted and whether its linked GitHub stories reflect that refresh. [O/R0] Retain the owner's decisions: teams are managed outside individual repositories, mode-specific specialist availability should reduce instruction tokens, and no backward compatibility is required. The reasonable interpretation is to diagnose and verify the incomplete handoff, not to implement the feature or rerun the entire planning pipeline.

## Run 0009 — Retry diagnosis

### Local planning succeeded; remote synchronization did not

[V] The task checkout contains the refreshed September 7 plan, approved feature spec, architecture review and all five complete story bodies. Root-agent commit inspection identifies run 0008's saved result as `0f9354e`; its live issue status reports success and a push. Intermediate patch failures in the run transcript do not establish a failed final plan: the final artifacts persist and the successful result includes nine changed files. This run does not claim every transient tool invocation succeeded.

[V] Root-agent live GitHub connector reads of all five linked issues verify that their titles and bodies still describe the September 6 decomposition. All point to commit `9571fc2b6344331853d9f89096d03ba4f977496c` and September 6 story documents. Their update timestamps remain September 6. Remote synchronization is therefore **confirmed missing**, superseding run 0008's “not performed or verified” status. No old-session artifact was opened to establish this; evidence is the current live issue bodies compared with the current-session files.

| Linked issue | Live remote title | Current local story title |
|---|---|---|
| [#127](https://github.com/marioatcloudvane/litefactory/issues/127) | Team-first index schema strict loader | External registry and mode resolution |
| [#128](https://github.com/marioatcloudvane/litefactory/issues/128) | Shared references/direct roster sources | External management and repository association |
| [#129](https://github.com/marioatcloudvane/litefactory/issues/129) | Post-classification private mode bundles | Per-run bundle isolation and conversation boundaries |
| [#130](https://github.com/marioatcloudvane/litefactory/issues/130) | Safe legacy-index migration | Focused modes and language skills |
| [#131](https://github.com/marioatcloudvane/litefactory/issues/131) | Team inspection and explicit migration CLI | Provenance, token evidence and handoff |

[V] Local mapping: `plan_brief.md:35` (Story Index). The clearest harmful conflict is remote #130: it still requests legacy migration, while the owner's approved plan explicitly excludes it (`plan_brief.md:13`, `:47`). This is substantive scope drift, not merely stale wording. Implementing from the linked issue bodies could build rejected migration work and omit current external-management/workflow/evidence acceptance criteria. The local design is reviewable; the end-to-end implementation handoff is not yet consistent.

### Central external management is already explicit in the refreshed design

[V] This requirement is present in the saved plan, not an inferred future addition:

- One operator-owned registry: `$LF_HOME/teams/index.json`; source Markdown, skills and resources reside beneath `$LF_HOME/teams/`, with external provenance/override metadata under its `.litefactory/` directory (`plan_brief.md:10`, `:11`, `:12`).
- Repositories persist only an explicit external-team association; they do not own the team source and there is no repository source tier (`plan_brief.md:11`, `:13`; `US-2.md`, Architecture Notes and AC-2.1–AC-2.3).
- Existing team-management validation, rendering, hashes, locks, drift handling and guarded writes are adapted rather than duplicated (`plan_brief.md:12`; `US-1.md`, Architecture Notes; `US-2.md`, Tasks).
- After classification, immutable selected-mode bundles are published outside worktrees at `$LF_HOME/bundles/<forge>/<owner>/<repo>/<issue>/<run-ordinal>/`; classifiers receive no team catalog (`plan_brief.md:16`; `US-3.md`, AC-3.2–AC-3.3).
- The repository may retain ordinary project instructions and session artifacts. Embedded defaults remain distribution content that an operator explicitly installs; they are not an implicit runtime source (`plan_brief.md:11`, `:15`, `:16`).

[V] Thus “outside a repository” means central source ownership and central management shared by multiple repositories. Per-run snapshots still duplicate selected rendering bytes for isolation, but no per-repository source catalog is managed. Whether that reduces actual instruction tokens remains US-5's measurement gate, not a claim proved by relocating files.

### Why an ordinary retry cannot repair existing issue contents

[V] The codebase-tour specialist inspected the current publication path and its tests. `runAndHarvest` saves the coding result and then invokes story publication (`internal/turn/harvest.go:92`). The turn layer renders Story Index rows even when they already contain issue links (`internal/turn/subissues.go:337`). It is therefore inaccurate to describe this as simply skipping linked local rows.

[V] The GitHub creation layer finds an existing child by stable story marker, returns `SubIssueExisted`, and continues without comparing or updating title, body or commit-pinned detail link (`internal/github/subissues.go:300`). This idempotency avoids duplicate issues but does not synchronize revisions. A retry can finish missing issue creation or link writeback; it cannot update already filed story contents through this path. Publication failures are logged without changing the coding run result, and missing publication dependencies disable the hook (`internal/turn/subissues.go:112`). A successful coding run is therefore not proof of successful story synchronization.

[V] Outcome tests explicitly cover this behavior: `TestFileStoryIssuesRerunIsANoOp` (`internal/turn/subissues_test.go:387`) invokes the creator twice while producing only the initial issue writes; `TestCreateSubIssuesSkipsAStoryThatIsAlreadyFiled` (`internal/github/subissues_test.go:412`) preserves an existing body and creates only missing stories. The later-mode recovery test (`internal/turn/subissues_test.go:334`) covers missing creation, not updating old descriptions. Tests were inspected, not executed in run 0009.

### Immediate consequence and remaining questions

[V] Repeating Research or Plan without a distinct synchronization action cannot repair these five already-filed descriptions through the current automatic publisher. Before implementation consumes #127–#131, an authorized publishing step must update their titles and complete bodies from current-session US-1–US-5, refresh pinned detail links, preserve issue identities, and verify all five remotely against the local plan. This is the concrete remaining handoff operation; central ownership does not require another design rewrite.

[V] No production code, plan/story body, remote issue, runtime configuration or branch was changed in this Research run. Only this brief and the manifest were updated. Source capability probes, token measurement and full implementation checks were not rerun. No new vendor CLI support claim is made.

[R0] Implementation still needs exact-version transport/isolation proof and measured input/total workflow-token evidence. Those existing gates remain valid. [V] Separately, whether future replans should automatically synchronize existing issues is a product decision about publication behavior; the current hook supports creation/idempotency only. A general publisher change is not necessary to establish the current diagnosis and was not added to this feature's implementation scope.

## Prior merged-source findings — run 0007, retained evidence

The following sections preserve the earlier merged-source research that the run 0008 plan cites. Their `[V]` tags mean verified **in run 0007**, not reverified in run 0009. In particular, the prior final recommendation to refresh the plan was completed in run 0008, and its open question about remote synchronization is answered by the live mismatch above. Historical test results do not claim fresh execution.

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
