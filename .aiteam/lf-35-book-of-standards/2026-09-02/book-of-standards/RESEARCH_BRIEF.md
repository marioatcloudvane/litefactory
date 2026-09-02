# Research Brief — book-of-standards

## Request

[O] Research the proposed “Book of Standards” feature and show how standards are currently written down. The proposed direction is a curated global standards repository, optional custom/company sources, a pull from source into a litefactory instance and then into a project, editable downstream copies, and selective acquisition such as “take 1, 3, 9.”

## Codebase Context

### What “standards” means today

[V] The repository already uses **Book of Standards** as the explicit description of `.aiteam/APP_CONSTRAINTS.md`. That document is project-local Markdown, written normatively with RFC 2119 terms, organized into fixed topical sections, and assigns stable identifiers such as `SEC-1` to individual constraints. It also records how constraints are enforced (`.claude/agents/app-context-updater.md:253`, `.claude/agents/app-context-updater.md:293`).

[V] Standards are nevertheless spread across several layers rather than represented by one catalog:

- `.aiteam/APP_CONSTRAINTS.md` is the fuller, normative project rulebook. An updater agent derives it from repository evidence and maintains it alongside the descriptive `.aiteam/APP_CONTEXT.md` (`.claude/agents/app-context-updater.md:1`, `.claude/agents/app-context-updater.md:55`).
- `AGENTS.md` is the Codex-facing summary of cross-agent working rules and directs readers to `APP_CONSTRAINTS.md` for the complete normative set (`AGENTS.md:1`, `AGENTS.md:24`, `AGENTS.md:158`, `AGENTS.md:167`).
- `CLAUDE.md` defines workflow standards: mode routing, artifact gates, one run per mode, and the no-commit responsibility split (`CLAUDE.md:8`, `CLAUDE.md:43`, `CLAUDE.md:96`, `CLAUDE.md:128`).
- Specialist prompts contain procedural standards and, in places, concrete engineering conventions. The Go implementation prompt is one such source (`.claude/agents/go-implementation-engineer.md:179`, `.claude/agents/go-implementation-engineer.md:245`).
- `.claude/skills/go/platform-constraints.md` is a reusable standards document covering project layout, errors, concurrency, interfaces, dependencies, database access, tests, logging, and formatting (`.claude/skills/go/platform-constraints.md:10`, `.claude/skills/go/platform-constraints.md:29`, `.claude/skills/go/platform-constraints.md:54`, `.claude/skills/go/platform-constraints.md:106`, `.claude/skills/go/platform-constraints.md:137`, `.claude/skills/go/platform-constraints.md:164`, `.claude/skills/go/platform-constraints.md:193`, `.claude/skills/go/platform-constraints.md:205`).
- Some standards are executable rather than documentary: source boundaries, validation, hooks, APIs, and tests enforce them. For example, the registered-clone abstraction structurally exposes no write operation (`internal/git/clone.go:13`, `internal/git/clone.go:25`).
- Operator policy is represented as editable YAML with explanatory comments, while defaults and accepted fields remain code-owned (`internal/config/template.go:22`, `internal/config/template.go:32`, `internal/config/config.go:400`, `internal/config/config.go:447`).

### How standards are currently authored and consumed

[V] The current project-standards flow is:

1. The context updater scans the repository.
2. It creates or merges `.aiteam/APP_CONTEXT.md` and `.aiteam/APP_CONSTRAINTS.md`; Research initializes context and Implement updates it after changes (`.claude/agents/app-context-updater.md:1`, `.claude/agents/app-context-updater.md:18`, `.claude/agents/app-context-updater.md:39`, `.claude/agents/app-context-updater.md:55`).
3. Each constraint is kept as one fact with a stable, never-reused identifier and RFC 2119 wording; updates are expected to prune stale material and respect document size budgets (`.claude/agents/app-context-updater.md:301`, `.claude/agents/app-context-updater.md:320`).
4. Architects read the full rulebook, cite applicable rules in task guidance, and record deviations instead of silently contradicting it (`.claude/agents/go-architect.md:190`, `.claude/agents/go-architect.md:217`, `.claude/agents/go-architect.md:282`, `.claude/agents/go-architect.md:301`).
5. Implementation engineers read the relevant topical sections and treat applicable constraints as non-negotiable (`.claude/agents/go-implementation-engineer.md:22`, `.claude/agents/go-implementation-engineer.md:39`).

[V] This is a scan-and-maintain flow, not a standards distribution system. It has no source catalog, install manifest, version/revision state, selection model, or upstream reconciliation.

[V] Claude specialist definitions under `.claude/agents/` are duplicated as Codex skills under `.codex/skills/*/SKILL.md`. The repository says these copies are generated and must be changed together, but no synchronization command or generator was found (`AGENTS.md:35`, `AGENTS.md:63`). This is a concrete example of the drift risk a distribution model must address.

[V] An architect prompt already mentions `book-of-standards/templates/service.go.tmpl` as an example, but the referenced local repository/directory does not exist (`.claude/agents/go-architect.md:117`, `.claude/agents/go-architect.md:134`). It should be treated as an aspirational reference, not an implemented facility.

### Existing seams relevant to the proposal

[V] The CLI registers commands centrally and currently supports initialization, repository management, daemon lifecycle, logs, and task cleanup. There is no standards command group (`internal/cli/commands.go:3`, `internal/cli/commands.go:19`).

[V] `$LF_HOME` path ownership is centralized, but its present layout contains configuration, database, logs, lock state, and Claude settings only. There is no instance standards directory (`internal/config/paths.go:13`, `internal/config/paths.go:30`, `internal/config/paths.go:63`, `internal/config/paths.go:93`).

[V] SQLite records identities, repositories, sessions, segments, runs, comments, watermarks, and polling state. It contains no standards catalog, source, selection, or installation tables (`internal/store/migrations/0001_init.sql:16`, `internal/store/migrations/0001_init.sql:144`, `internal/store/migrations/0003_agent.sql:43`, `internal/store/migrations/0003_agent.sql:47`). All future SQL would have to remain within `internal/store`.

[V] The nearest precedent for curated-global → editable-instance material is configuration generation: code-owned defaults render an explanatory file, the first write is exclusive, existing operator edits are not overwritten, and loading validates the result (`internal/config/template.go:268`, `internal/config/template.go:345`, `internal/config/config.go:462`, `internal/config/config.go:475`).

[V] The nearest project registration seam currently points in the opposite direction. `repo add` states that aiteam must already be installed and litefactory does not write inside the registered clone (`internal/cli/cmd_repo.go:48`, `internal/cli/cmd_repo.go:59`). Repository resolution is local and read-only, while the registered-clone git API intentionally omits pull, checkout, and commit operations (`internal/repos/registration.go:128`, `internal/repos/registration.go:193`, `internal/git/clone.go:13`, `internal/git/clone.go:25`).

[V] Project detection currently recognizes root `CLAUDE.md`, `.aiteam/`, and team markers; it has no standards manifest or source provenance (`internal/repos/aiteam.go:24`, `internal/repos/aiteam.go:56`, `internal/repos/aiteam.go:73`, `internal/repos/aiteam.go:101`).

[V] Litefactory itself does not parse or inject a standards catalog. Claude relies on agent-tool discovery in the worktree. Codex is launched with repository rules ignored, and repository-local Codex skills additionally depend on the user-configuration allowance (`internal/codex/runner.go:49`, `internal/codex/runner.go:53`, `internal/codex/runner.go:193`, `internal/codex/runner.go:222`). A project installation format therefore does not automatically imply that every configured agent will consume it.

## Domain / Problem Space

[O] The proposal implies a three-layer distribution model:

1. **Source/global layer:** litefactory-curated standards, with optional custom or company-owned sources.
2. **Instance layer:** selected standards copied into a particular litefactory installation, where they become editable.
3. **Project layer:** selected instance standards copied into a registered project, where they are also editable.

[O] “Take 1, 3, 9” implies selection below the source level, but the requested unit is not defined. It may mean stable rule IDs, displayed ordinal positions, sections, files, or templates.

[V] The existing stable IDs in `APP_CONSTRAINTS.md` offer a useful conceptual precedent, but they are topic-prefixed identifiers rather than numeric catalog positions (`.claude/agents/app-context-updater.md:253`, `.claude/agents/app-context-updater.md:293`). Display ordinals would be fragile under source updates; stable source-qualified identifiers would better preserve intent, but choosing that belongs to Plan mode.

[V] The proposal combines at least four distinct responsibilities that the current code does not model: source acquisition, catalog/metadata representation, local editable materialization, and update/merge behavior. Authentication for private sources and agent consumption are additional concerns.

## Constraints & Risks

### Repository and ownership constraints

- [V] Installing into a project would intentionally cross the current guarantee that repository registration never writes inside the clone (`internal/cli/cmd_repo.go:54`, `internal/cli/cmd_repo.go:59`). Plan mode must either define an explicit, user-invoked exception or place material elsewhere.
- [V] Git operations must remain behind `internal/git`, database operations behind `internal/store`, `$LF_HOME` path derivation in `config.Paths`, and orchestration in the CLI/repository layer consistent with the existing dependency map (`AGENTS.md:87`, `AGENTS.md:115`, `AGENTS.md:117`, `AGENTS.md:156`).
- [V] Config rejects unknown keys and literal-looking credentials, so private-source authentication cannot casually introduce inline secrets (`internal/config/config.go:4`, `internal/config/config.go:24`).
- [V] No parser analogous to “1, 3, 9” exists. Current routing handles flags, positional arity, and usage errors at the command boundary (`internal/cli/router.go:58`, `internal/cli/router.go:139`).

### Content lifecycle risks

- [V] Editable downstream copies make refresh semantics the central difficulty. Blind overwrite loses user changes; never refreshing strands users; an implicit merge can silently alter normative instructions.
- [V] There is currently no provenance manifest, revision pin, content hash, base copy, or conflict representation. Without one, the system cannot reliably distinguish upstream changes from local edits.
- [V] Standards are already duplicated across project constraints, agent guidance, platform constraints, and generated Codex copies. Adding a catalog without defining authority and projection rules could increase rather than reduce drift.
- [V] Stable identifiers are already a documented requirement for generated project constraints. Selection and refresh must preserve identity across revisions, not depend on presentation order (`.claude/agents/app-context-updater.md:301`, `.claude/agents/app-context-updater.md:320`).

### Security and trust risks

- [V] A remote/custom source introduces URL/repository validation, destination traversal, authentication, revision pinning, integrity verification, and supply-chain concerns that the current local-only repository registration does not have (`internal/repos/registration.go:128`, `internal/repos/registration.go:143`).
- [V] Pulled standards become instructions to coding agents. A compromised or overly broad source is therefore an instruction/prompt-injection boundary, not merely a document-download risk.
- [V] Private company sources need an authentication mechanism compatible with indirect secret references; existing GitHub authentication does not define authorization for arbitrary standards hosts.

### Agent compatibility risks

- [V] Writing a project rulebook does not guarantee consumption by all runners. In particular, the present Codex invocation ignores repository rules, while local skill availability is configurable (`internal/codex/runner.go:49`, `internal/codex/runner.go:53`, `internal/codex/runner.go:193`, `internal/codex/runner.go:222`).
- [V] Multiple output targets—`APP_CONSTRAINTS.md`, `AGENTS.md`, `CLAUDE.md`, tool-specific skills, or a new manifest—have different audiences and semantics. Treating them as interchangeable would reproduce current duplication.

## Open Questions

1. [O] What is the selectable unit: an individual stable rule, a section, a file, a template, or a bundle?
2. [O] Does “1, 3, 9” refer to stable IDs, display ordinals, ranges, or catalog positions? How are selections made non-fragile across source revisions?
3. [O] What source types are supported initially: a curated built-in/git repository, arbitrary git URL, local directory, archive, GitHub repository, or registry?
4. [O] Is the curated source immutable to users, with only instance and project copies editable? This is the most natural reading of “other than the first one,” but it is not explicit.
5. [O] Where does instance-level content live under `$LF_HOME`, and is its metadata file-based, database-backed, or both?
6. [O] Which project artifact receives installed standards: the existing `APP_CONSTRAINTS.md`, a separate imported standards file plus manifest, agent-specific files, or generated projections into several targets?
7. [O] How are upstream refreshes reconciled with local edits: replace, preserve, three-way merge from a recorded base, patch application, or explicit conflicts?
8. [O] How are sources pinned, verified, authenticated, and trusted, especially private company sources?
9. [O] Is project installation an explicit exception to the current registered-clone no-write contract, and must it operate in the clone or only in a task worktree?
10. [O] How should installed standards reach Codex while its current runner ignores repository rules, and how should behavior remain consistent across configured agents?
11. [V] Should existing generic standards embedded in prompts/platform constraints be migrated into the catalog, referenced from it, or remain independent? The answer determines whether the feature is a distributor, a new source of truth, or both.
12. [V] What is the intended relationship between the already-named project “Book of Standards” (`APP_CONSTRAINTS.md`) and the proposed template repository also described as standards?

## Recommended scope for Plan mode

[V] Plan mode should define a narrow first vertical slice across all three layers rather than attempting every source and projection type. It should specify: the canonical catalog schema and stable identity model; one curated source transport plus a source-qualified custom-source contract; instance storage under `$LF_HOME`; an explicit project target and write boundary; selective install syntax; provenance/revision metadata; local-edit and refresh/conflict semantics; trust and authentication boundaries; and how each supported agent consumes the result.

[V] Plan mode should explicitly exclude implementation, broad migration of every embedded prompt standard, an open-ended remote registry, and silent automatic merges until the authority and lifecycle questions above are decided. The existing `APP_CONSTRAINTS.md` should be treated as the primary project-level integration candidate, not automatically replaced, because it already has stable identities and established architect/engineer consumers.
