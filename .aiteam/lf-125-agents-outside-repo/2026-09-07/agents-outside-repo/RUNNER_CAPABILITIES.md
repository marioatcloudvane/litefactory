# T3.1 exact-version capability gate — run 0011

## Run 0015 correction and verification boundary

The historical EPERM below came from the outer Codex workspace-write sandbox,
not a demonstrated Codex transport limitation. The owner reports that the same
codex-cli 0.153.4 role-config command succeeds outside that sandbox and returns
`turn.completed`. That correction supersedes any earlier treatment of this result
as a hard implementation gate. Native delivery is implemented in run 0015;
fake-runner and source-byte tests are not authenticated end-to-end proof.

No paid CLI probes were run again in run 0015. To verify live behavior, execute
the bounded scratch-fixture procedure below from a Claude-driven run or an
operator-controlled process outside the outer Codex sandbox, while retaining the
child's normal sandbox and permission policy. Do not bypass permissions. Record
the exact installed CLI version, selected catalog initialization evidence,
ordinary project-instruction marker, actual delegated model, forbidden-operation
result and concurrent-mode separation. A failure before inference must record
the execution environment separately from product capability. Missing credentials
or unavailable telemetry are explicit unverified rows, never inferred passes.

The reproducible offline byte command is `go run ./cmd/team-benchmark` and its
inputs and output are retained under `teams/`. Use
`teams/benchmark-run-template.json` for authorized future full-catalog/selected-mode
trials with equivalent tasks, models and cache conditions. Parent-only usage does
not measure delegated workload; no numeric token or latency savings are claimed.

## Historical run 0011 record (not a current product blocker)

Status: **NOT PASSED (remaining end-to-end isolation gates), not a claim that native external delivery is impossible.** Initial inspection used help only. After the orchestrator confirmed that the approved plan authorizes bounded manual verification, three small authenticated Claude calls ran; Codex was blocked by the execution environment before inference. No credential contents were printed, live LF_HOME changes or production runner edits were made. Do not describe a fake-argv test as passing this gate.

## Evidence executed

On 2026-09-07, read-only version/help commands were executed both in the task checkout and in `/private/tmp/lf-125-capability.Vd8Vvt`, a fresh empty scratch directory:

```
codex --version
claude --version
codex exec --help
claude --help
claude agents --help
```

Observed versions: **codex-cli 0.153.4**, **Claude Code 2.1.263**. Both commands resolve from `/Users/mariomh/.nvm/versions/node/v24.16.0/bin/`. Codex printed a nonfatal warning that sandbox permissions prevented creating PATH aliases; help/version exited zero. Scratch execution tested CLI availability/help only, not startup catalog behavior.

## Candidate transports and concrete limitations

Claude's installed help advertises session-local `--agents <json>` and `--plugin-dir <path>`. JSON can deliver native agent bodies; a per-mode external plugin can package agents and skills. Neither help entry promises that ambient project agents/skills are excluded. `--setting-sources` describes settings, not a complete discovery allowlist. `--disable-slash-commands` disables all skills, so cannot implement a selected supporting-skill roster on its own. `claude agents` now manages background sessions: it is not a catalog-inspection command.

Claude `--bare` is concretely incompatible with retaining the current OAuth/project-discovery contract unchanged: help explicitly says it skips CLAUDE.md auto-discovery and does not read OAuth/keychain credentials. Therefore do not use it as a drop-in isolation fix. Existing `harness.EnsureClaudeConfigDir` owns credential-link creation and refresh continuity; reuse that lifecycle. It isolates the personal project registry, but its implementation does not suppress repository discovery. Agent injection must preserve runner `--tools` as the permission ceiling and avoid assuming a per-agent tools declaration may expand it.

Codex's installed help advertises per-run `-c key=value`, `--strict-config`, and `--ignore-user-config`. The last explicitly retains auth via CODEX_HOME but only promises to ignore its config.toml; it does not promise to ignore repository catalogs. Current official configuration documents `agents.<name>.config_file` for external role TOML, role descriptions, and `skills.config` path/enablement overrides. These are candidate native transport controls, not an exact-version isolation proof. [Configuration reference](https://learn.chatgpt.com/docs/config-file/config-reference).

Official documentation allows model/reasoning settings in custom agent files. Preserve configured tier resolution when evaluating this route; the existing `internal/render/codex.go` is a skill renderer and explicitly rejects nonempty Model/Tools. Do not delete those fields or mistake skill output for native-agent delivery. [Subagents](https://learn.chatgpt.com/docs/agent-configuration/subagents).

Official skill discovery includes repository ancestors, user, admin and system directories, and follows symlinked skill directories. This is why moving definitions to an isolated CODEX_HOME alone is insufficient evidence for zero ambient catalogs. [Skill discovery](https://learn.chatgpt.com/docs/build-skills).

## Required remaining evidence before enabling a transport

| Requirement | Evidence still needed |
|---|---|
| Selected roster | Real init/catalog trace containing only the chosen team/mode agents and supporting skills, with unique sentinel definitions for each mode |
| Classifier | Zero team definitions even with sentinel agents/skills in project, ancestor and personal discovery locations; no inherited team body in project instructions |
| Project instructions | Ordinary project safety instructions remain loaded when generated specialist sections are excluded |
| Authentication | Real authenticated startup using current credential lifecycle, including token refresh behavior; never persist secrets in the report |
| Tier/model | Actual delegated model matches each resolved tier; missing mappings and unsupported fields fail with an actionable error |
| Permissions | Child/subagent cannot expand the resolved parent ceiling; read-only mode cannot write through agent delegation or shell tools |
| Concurrency | Simultaneous modes use disjoint immutable bundles without shared-home catalog mutation; traces contain no other mode's sentinels |
| Unsupported behavior | Exact runner/version/mode and missing capability are identified before launch; no silent full-catalog fallback |

Create scratch fixture directories with the relevant sentinel files and keep all permitted writes within those fixtures. Record exact command/config, working directory, effective catalog, model, permission result and sanitized trace per matrix cell. Re-run on CLI upgrade. A manually authorized real CLI verification is necessary for the authenticated/model-execution rows; no production runtime should claim these rows passed from help output.

## Follow-up authenticated scratch evidence

Scratch: `/private/tmp/lf-125-live-probe.KHIZtm`. Fixtures contain ordinary CLAUDE.md/AGENTS.md project marker `PROJECT_RULE_125`, a `.claude/agents/ambient-probe.md`, a `.agents/skills/ambient-probe/SKILL.md`, external selected native agent JSON, and an external Claude plugin containing `selected-skill`. A new empty Claude config directory contained only empty settings and a symlink to the existing operator credential file, matching the current helper's lifecycle. No credential contents were inspected.

All Claude calls used a 45-second process timeout, 0.03 USD per-call budget, `--model haiku` (the existing fast tier mapping), `--tools ''`, `--strict-mcp-config`, `--setting-sources ''`, and `--no-session-persistence`. Prompts requested a single sentence about project marker/catalog presence and prohibited tool use. Actual parent model was `claude-haiku-4-5-20251001`; authenticated terminal results were successful with `apiKeySource: none`.

| Variant | Actual initialization evidence | Result |
|---|---|---|
| Safe mode plus selected agent JSON | tools empty; agents `claude, Explore, general-purpose, Plan`; neither ambient nor selected sentinel | Success, project marker absent; 0.006166 USD |
| Normal plus selected agent JSON | tools empty; agents `claude, Explore, general-purpose, Plan, selected-probe, statusline-setup`; ambient sentinel absent | Success, project marker absent; 0.015107 USD |
| Normal plus selected agent JSON, external plugin, explicit project-instruction append | same selected agent catalog; skills includes `lf-probe:selected-skill`; ambient sentinel absent | Success, project marker present; 0.015747 USD |

Total observed Claude cost: 0.037020 USD. Project marker presence is model-reported, while agent/skill/tool membership is from actual init events. Built-in skills remained listed even in safe mode; zero team catalogs does not imply an empty list of vendor-provided skills. No delegated execution was requested, so these runs prove the parent model and empty tool surface, not the selected agent's model or privilege inheritance. Concurrent mode isolation, all ancestor discovery variants and credential refresh remain untested.

This establishes a credible Claude candidate: isolate the config directory, disable settings-source discovery, inject selected agents/plugin explicitly, and explicitly append permitted ordinary project instructions. It does not establish a general safe extractor for retained root instructions or prove full AC3.1. Safe mode suppresses explicitly injected custom agents, so is unsuitable for the selected-agent transport, though useful for a classifier candidate.

Codex used the installed CLI with `exec --json --ignore-user-config --ignore-rules --strict-config --sandbox read-only --skip-git-repo-check --ephemeral --model gpt-5.6-luna`, low reasoning and per-run `agents.selected-probe.description`/`config_file` overrides to external role TOML. The role declared the existing fast-tier model, low reasoning, read-only sandbox, and sentinel instructions. It exited 1 before inference: `failed to initialize in-process app-server client: Operation not permitted (os error 1)`. This is an execution-environment denial, not proof that the role configuration is unsupported. No bypass or unsandboxed retry was attempted. Codex authentication, discovery and delegation are unverified.

## Baseline

See `BASELINE_CATALOG.md`: original commit and Git blob identities preserve the full-catalog fixture, original roots and old rendering code before concurrent changes. Baseline and selected-mode token/latency evidence is still unmeasured. Count root body duplication, catalogs, dynamic skill reads and delegated calls separately; byte counts alone cannot establish savings.
