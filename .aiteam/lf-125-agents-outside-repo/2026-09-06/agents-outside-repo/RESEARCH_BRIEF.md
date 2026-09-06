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
