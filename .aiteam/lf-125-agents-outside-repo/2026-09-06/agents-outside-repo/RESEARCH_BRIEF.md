# Research Brief — agents-outside-repo

## Request

[O] Determine whether agent definitions can live outside every task worktree, whether their current size is a real operational problem, and whether Claude Code and Codex can be given only the agent capability required for a run mode (for example, Research).

## Codebase Context

[V] litefactory is a single Go module. Its vendor-neutral agent contract is in `internal/agent/mode.go:8-21,59-68`; Claude execution is isolated in `internal/harness` and Codex execution in `internal/codex` (`internal/harness/doc.go:1-10`, `internal/codex/runner.go:1-21`). `internal/turn` creates the task worktree and sends a runner request, while `internal/daemon` selects the runner and permissions (`internal/turn/prepare.go:253-270`, `internal/daemon/runners.go:30-51`, `internal/daemon/tools.go:23-47`). This separation is a suitable boundary for vendor-specific external-agent loading.

[V] The worktree does not get agents through a litefactory copy step. Worktree creation is ordinary `git worktree add` (`internal/turn/prepare.go:861-903`, `internal/git/clone.go:148-183`); therefore all tracked `.claude` and `.codex` assets arrive as part of every checkout. No post-checkout copying of agent content was found.

[V] Current tracked instruction inventory:

| Content | Bytes | Notes |
|---|---:|---|
| `.claude/` | 226,345 | 13 specialist definitions plus modes, shared scanners, hooks/settings and roster assets |
| `.codex/skills/` | 188,179 | 13 `SKILL.md` counterparts |
| Total | 414,524 (404.8 KiB) | Agent/instruction content per worktree |
| Five worktrees | 2,072,620 (1.98 MiB) | Before filesystem block overhead |
| 13 paired specialist definitions | ~373.8 KiB | Portion removable by externalizing the paired definitions |
| Remaining Claude support assets | 40,707 | Modes/scanners/settings/hooks remain behaviorally relevant |

[V] The storage burden is not material: five concurrent worktrees consume roughly 2 MiB of source-text data, while litefactory’s default concurrent-run limit is three (`internal/config/config.go:441-452`). The issue is instead instruction discovery and model context. The paired definitions are deliberately maintained as copies and acknowledge possible drift (`AGENTS.md:37-62`).

[V] The classifier paths are distinct from work execution: Claude has a tool-free classifier (`internal/harness/runner.go:24-67`) and Codex uses a read-only classifier with `--ignore-user-config` and `--ignore-rules` (`internal/codex/runner.go:104-119`). The repository has no token trace establishing whether either CLI reads all discovered definitions during classification. The owner’s token-cost concern is plausible but unmeasured, so it must not be quantified as a verified saving.

[V] Codex currently has an all-or-nothing discovery choice. Its default run suppresses user configuration; permitting user configuration also enables repository skills (`internal/codex/runner.go:238-267`, `internal/config/config.go:170-181`). Neither runner configuration currently represents an external agent/skill location or a mode-to-instruction-set mapping (`internal/config/config.go:110-182`).

[V] Claude runs from the task worktree and supplies controlled settings, but its settings are additive; personal Claude agents and skills can still be discovered (`internal/harness/doc.go:49-59`, `internal/harness/settings.go:12-34`). Strict MCP configuration limits project MCP servers, not agent discovery. Any redesign must retain least-privilege and prevent unexpected ambient instruction sources.

## Domain / Problem Space

### Claude Code

[V] Claude Code supports external agent content per run by two documented mechanisms:

1. `claude --agents '<JSON>'` injects session-only custom subagent definitions. These definitions take precedence over project, user and plugin agents (managed definitions rank higher). This can supply precisely the roles required for the selected mode.
2. `claude --plugin-dir /absolute/path/to/plugin` loads a local plugin for that session. A plugin may contain `agents/` and `skills/`, allowing mode-specific external plugin directories without copying files into each worktree.

[V] `~/.claude/agents/` is an external persistent location, but it makes all agents available to all projects and does not itself solve per-mode selection. `--add-dir` is not an agent-discovery mechanism: it can expose external skills but does not load subagents from the added directory. `CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD=1` only expands instruction/rules-file discovery.

[V] Claude plugin agent frontmatter has limits: `hooks`, `mcpServers`, and `permissionMode` are ignored. A deployed version check is required: plugin ZIP support requires Claude Code v2.1.128+, and agent behavior has changed in the v2.1.1xx series.

Sources: [Claude Code subagents](https://code.claude.com/docs/en/sub-agents), [CLI usage](https://code.claude.com/docs/en/cli-usage), [plugins](https://code.claude.com/docs/en/plugins), [additional-directory permissions](https://code.claude.com/docs/en/permissions) (retrieved 2026-09-06).

### Codex CLI

[V] Codex supports persistent external custom agents at `~/.codex/agents/<name>.toml`; project agents belong in `.codex/agents/<name>.toml`. An agent TOML definition requires `name`, `description`, and `developer_instructions`.

[V] Codex supports a per-run external role through repeatable configuration overrides. A documented pattern is:

```sh
codex exec \
  -c 'agents.researcher.description="Research-focused role."' \
  -c 'agents.researcher.config_file="/opt/litefactory/codex/agents/researcher.toml"' \
  '...'
```

`codex exec -c key=value` applies configuration for that invocation, and `agents.<name>.config_file` is an agent TOML configuration layer. An external profile under `$CODEX_HOME` is an alternative. Current documentation does not offer a direct `--agent-dir` or `--agents` equivalent, and does not fully specify name-collision behavior between dynamic and discovered agent definitions; smoke-test against the installed release before production use.

[V] Codex directly supports shared external skills at `$HOME/.agents/skills` and machine-wide `/etc/codex/skills`; symlink targets are followed. Skill discovery is progressive: the initial prompt receives a catalog (capped at 2% of context or 8,000 characters if context is unknown), while a complete `SKILL.md` is read only when selected. This directly supports only loading full mode-relevant instructions. It lowers main-thread context pressure but does not prove reduced total token use: Codex documents that subagent workflows themselves may consume more tokens than a comparable single-agent run.

Sources: [Codex subagents and custom agents](https://developers.openai.com/codex/subagents/), [configuration reference](https://developers.openai.com/codex/config-reference/), [CLI reference](https://developers.openai.com/codex/cli/reference/), [skills](https://developers.openai.com/codex/skills/) (retrieved 2026-09-06).

## Constraints & Risks

- [V] Shared external content changes provenance: current definitions are branch-pinned, reviewable repository content. A shared installation needs an explicit ownership, release/pinning, update and rollback model so a task’s code and its instructions cannot silently drift.
- [V] Claude’s external plugin/inline-agent choice is strong for mode-specific loading, but plugin restrictions and ambient personal discovery must be verified against litefactory’s current controlled settings and permission model.
- [V] Codex external skills are well aligned with progressive disclosure; Codex external agent selection needs an installed-version smoke test before being treated as a supported dispatch contract.
- [V] The external location must be readable to the CLI without adding it as a writable sandbox root. Codex validates writable roots as absolute security-sensitive paths (`internal/config/config.go:131-145`, `internal/config/validate.go:303-314`).
- [V] No external-agent capability probe or integration test exists today. Feature work must add deterministic command construction and tests for each configured agent/mode, without calling real CLIs in unit tests.
- [V] Agent relocation alone does not eliminate all worktree instruction assets: Claude modes, scanners, settings and hooks total about 40.7 KiB and some have execution/security implications.

## Open Questions

1. [V] Which installed Claude Code and Codex CLI versions must litefactory support? Their exact external-agent behavior should be smoke-tested before choosing the runtime contract.
2. [V] Is the desired source of truth a versioned global installation, an externally versioned plugin bundle, or a litefactory-managed per-version cache? Plan mode must define update, pinning and rollback behavior.
3. [V] Does the desired change externalize only paired specialist roles, or also Claude modes/scanners/settings/hooks? The latter needs separate security and workflow-equivalence analysis.
4. [V] What actual classifier token and latency delta occurs with the proposed configurations? Establish a representative benchmark before claiming a performance outcome.
5. [V] Can Claude’s controlled settings fully constrain personal agent/skill discovery while retaining required plugins/inline agents? This needs a CLI behavior test.
6. [V] Can Codex’s `agents.<name>.config_file` dynamic role coexist safely with ignored user configuration in the exact deployed version? This needs a CLI behavior test.

## Recommended scope for Plan mode

Plan a runner-specific instruction-source abstraction and a minimal, versioned litefactory-owned external bundle. Cover only the 13 paired specialist definitions first; preserve repository scaffolding unless behavior tests justify moving it. For Claude, compare per-mode local plugins with inline `--agents` definitions and select one after a versioned smoke test. For Codex, use external shared skills with progressive disclosure and test per-run `agents.<name>.config_file` injection. Add command-construction/unit coverage and a documented, reproducible benchmark for classifier tokens/latency. Do not frame checkout storage as a driver: it is small. Do not enable ambient user configuration merely to obtain skills, and do not widen write permissions for the shared bundle.

## Research Update — run 0002

### Mode stack and instruction weight

[V] The human-facing workflow defines Research, Plan, Implement and Spike (`CLAUDE.md:7-15,70-97`), whereas the runtime also defines Validate and requires a permission profile for it (`internal/agent/mode.go:11-28`, `internal/config/policy.go:344-420`). Validate currently has no orchestrator, specialist roster, artifact convention or router; it is only a read-only permission profile (`internal/config/config.go:406-436`, `internal/config/policy.go:423-450`).

[V] The largest individual role instructions are architect (25,898 bytes), implementation orchestrator (23,950), implementation engineer (23,074), plan orchestrator (17,875), research orchestrator (17,117), and context updater (16,906). The 13 Claude role definitions total 185,638 bytes; their 13 Codex counterparts total 188,179 bytes. No project telemetry proves their per-run prompt-token cost.

| Mode | Current stack | Assessment |
|---|---|---|
| Research | Research orchestrator; codebase tour; optional web researcher; optional scanners | Already relatively narrow. Load only its orchestrator, tour, web researcher and scanner skills when triggered. |
| Plan | Plan orchestrator; requirements engineer; architect; product owner | Architect-heavy but appropriately pre-coding. Load platform constraints, task decomposition and forward-looking security guidance only here. |
| Implement | Implementation engineers; code review; environment gate; unit/integration test engineers; bug loop; drift report; app-context update | Overloaded: it contains coding, test creation, review, environment validation, reporting and durable documentation maintenance. |
| Validate | No workflow roles; read-only profile | Suitable for independent review and verification only if they return findings rather than write artifacts. |

[V] Implement’s current sequence is development, code-review gate, conditional environment setup, unit/conditional integration testing, up to two bug cycles, drift report, then context update (`.claude/agents/implement-orchestrator.md:224-419`). The implementation engineer also has analysis-only bug-root-cause and setup-only environment roles (`.claude/agents/go-implementation-engineer.md:86-104`).

### Proposed responsibility split

[V] The following is a research recommendation, not an implementation plan:

| Stage | Keep / move | Specialist set to make available | Required authority |
|---|---|---|---|
| Research | Keep | Research orchestrator, codebase tour, optional web researcher; architecture/security/testing scanners only when the question requires them | Existing research profile |
| Plan | Keep | Plan orchestrator, requirements engineer, architect, product owner; planning/platform/task-decomposition skills | Existing plan profile |
| Implement | Reduce to code and test creation | Implementation engineer plus unit-test engineer; integration-test engineer only when real infrastructure is touched | Existing write-capable implement profile |
| Validate | Add as a read-only workflow | Code reviewer, test verifier, environment validator, drift checker | Existing Validate profile if all results return inline and do not write artifacts |
| Post-validate maintenance | Separate explicit maintenance/finalization stage, or grant a tightly scoped authority | App-context updater; optional report writer | Cannot run under current strictly read-only Validate profile |

[V] This split has two non-negotiable consequences. First, current code review reads without changing source but writes a review artifact and routes blocking findings back to implementation (`.claude/agents/go-code-reviewer.md:20-24,147-224`, `.claude/agents/implement-orchestrator.md:253-276`). It can become Validate only if the verdict is returned to litefactory/the issue without an on-disk report, or Validate is deliberately granted a narrowly scoped session-artifact write permission. Second, the app-context updater writes durable `.aiteam` reference/rule documents (`.claude/agents/app-context-updater.md:1-11`), so it cannot honestly be called a current read-only validation action. It needs a separate maintenance stage or a changed authority contract.

[V] Current test engineers write test code, so they belong in Implement when “testing” means creating coverage. A new read-only test verifier could belong in Validate when “testing” means executing and assessing existing tests. The existing integration-test specialist also instructs use of build tags, which conflicts with this repository’s no-new-build-tags/no-integration-test-name rule (`.claude/agents/go-integration-test-engineer.md:10,183-188`, `AGENTS.md:103-108`); resolve that drift before reorganizing the stack.

### External, per-mode bundle sketches

#### Claude Code

[V] Use one self-contained, versioned local plugin directory per mode. The runner selects exactly one via `--plugin-dir`; a plugin may include `agents/` and `skills/`. Add inline `--agents` only for small generated definitions. The following is a conceptual launch shape based on documented flags:

```sh
claude -p "$PROMPT" \
  --setting-sources project \
  --strict-mcp-config --mcp-config /opt/litefactory/mcp/research.json \
  --plugin-dir /opt/litefactory/claude-bundles/research
```

```text
/opt/litefactory/claude-bundles/
  research/agents/{research-orchestrator,codebase-tour,web-researcher}.md
  plan/agents/{plan-orchestrator,requirements-engineer,go-architect,proxy-product-owner}.md
  implement/agents/{implement-orchestrator,go-implementation-engineer,go-unit-test-engineer}.md
  validate/agents/{go-code-reviewer,test-verifier,environment-validator,drift-checker}.md
  maintenance/agents/{app-context-updater}.md
```

[V] `--plugin-dir` is session-only and can be repeated; `--agents` definitions are session-only and outrank project/user/plugin agents. `--setting-sources project` excludes user/local settings but does not establish a documented deny-list for project-discovered agents. Therefore old project agent files must be removed from project discovery paths after a behavior test, rather than assuming plugin injection excludes them. Plugin agent frontmatter ignores `hooks`, `mcpServers`, and `permissionMode`.

Sources: [Claude plugins](https://code.claude.com/docs/en/plugins), [Claude subagents](https://code.claude.com/docs/en/sub-agents), [Claude settings](https://code.claude.com/docs/en/settings), [Claude CLI](https://code.claude.com/docs/en/cli-usage) (retrieved 2026-09-06).

#### Codex CLI

[V] Treat specialist instructions as skills where possible and place the canonical bundles outside worktrees. Codex discovers shared skills from `$HOME/.agents/skills` and `/etc/codex/skills`, follows skill-directory symlinks, exposes initially only name/description/path metadata, and reads a full skill body only on selection. A bundled layout can be:

```text
/opt/litefactory/codex-skills/
  research/{research-orchestration,codebase-tour,web-research}/SKILL.md
  plan/{requirements,architecture,product-breakdown}/SKILL.md
  implement/{implementation,unit-test-authoring,integration-test-authoring}/SKILL.md
  validate/{code-review,test-verification,environment-validation,drift-check}/SKILL.md
  maintenance/{app-context-update}/SKILL.md
```

[V] The public documentation does not provide an arbitrary per-run skill-root flag. A mode-selected isolated discovery layout is therefore a design to validate, not a guaranteed CLI contract. A minimal worktree pointer could be a symlink such as `.agents/skills/research -> /opt/litefactory/codex-skills/research`; a more isolated option is a litefactory-owned `CODEX_HOME` with per-mode configuration. The current runner source independently identifies a litefactory-owned `CODEX_HOME` as the intended long-term solution (`internal/config/config.go:170-181`).

[V] Per-run external Codex agent roles can be declared through configuration overrides, for example:

```sh
codex exec \
  -c 'agents.reviewer.description="Independent read-only review."' \
  -c 'agents.reviewer.config_file="/opt/litefactory/codex-agents/reviewer.toml"' \
  "$PROMPT"
```

This needs a version-pinned smoke test, including the interaction with `--ignore-user-config` and collision behavior. Current Codex documentation confirms progressive skill disclosure and a 2%-of-context (or 8,000-character) cap for the initial skill list; it does not prove a lower total token bill, because selected skill bodies and subagent work still cost tokens.

Sources: [Codex skills](https://developers.openai.com/codex/skills/), [Codex subagents](https://developers.openai.com/codex/subagents/), [Codex configuration](https://developers.openai.com/codex/config-reference/), [Codex agent loop](https://openai.com/index/unrolling-the-codex-agent-loop/) (retrieved 2026-09-06).

### Token hypothesis and measurement

[O] The desired optimization is token reduction from injecting only relevant specialists per mode, not disk reduction.

[V] It is reasonable to expect smaller initial instruction catalogs with mode bundles. For Codex, the documented mechanism bounds all available-skill metadata but does not make it free; for Claude, official sources examined document selection mechanisms but not a comparable prompt-token accounting. Neither source establishes how much current classifier token use comes from agent discovery.

[V] The decision criterion should be a controlled, version-pinned benchmark, not file byte counts. For each vendor and each mode, run the same fixed classification and work prompt with (a) current repository discovery and (b) the external mode bundle; record CLI version, effective configuration, input/output/total token usage where available, first-response latency, subagent count, task outcome, and any unexpected discovered agent. Compare median and p95 over repeated runs. A change that reduces classifier context but increases total task tokens through extra delegation is not a clear win.

## Research Update — run 0005

### Owner decision: no legacy compatibility

[O] The owner states that the prior index version was never used and explicitly requires no backward/downward compatibility.

[V] This removes the need for in-memory v1-to-v2 migration, legacy provenance/status, an explicit index-migration writer, and tests which preserve the former all-agent availability across every mode. Native schema v2 can be the only accepted index format from the first release.

[V] The completed Plan artifact currently includes compatibility as US-4 and makes bundle wiring/inspection depend on it. That plan is now stale relative to this owner decision. A subsequent Plan run must remove US-4; remove the migration command/task from US-5; remove US-4 dependencies from US-3 and US-5; and replace migration-related acceptance criteria, tests and documentation with a hard rejection of unsupported schema versions. The remaining team-first schema, per-mode roster resolution, private external bundle and strict Validate design are unaffected.

### Recommended scope for the next Plan update

[V] Preserve four stories: team-first strict schema, ref/direct definition resolution, post-classification private mode bundles, and deterministic read-only team inspection. Make an unsupported `schemaVersion` a clear startup/inspection error naming the supported version. Do not retain a compatibility parser, converter, legacy label or operator migration command.
