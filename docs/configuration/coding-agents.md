# Coding agents

[Documentation home](../README.md) · [Configuration](README.md) · [Key reference](config-yaml.md#agents) · [Permissions](permissions.md)

Litefactory can run Claude Code or Codex in a task worktree. Install and authenticate each CLI you configure before starting the daemon. Factory GitHub authentication is separate from agent authentication.

## Select and configure

The first entry under `agents` is the default for unlabelled work and mode classification. Use the canonical issue label `Coding Agent: Claude` or `Coding Agent: Codex` to select another configured runner. More than one agent label, a malformed label or an unconfigured selection is refused with thread feedback. See the [issue reference](../guides/owner-commands.md).

Edit the matching entry in your existing file. Every key is listed in the [key reference](config-yaml.md#agents). For example:

```yaml
agents:
  codex:
    binary: /absolute/path/to/codex
    model_tier: balanced
    timeout_seconds: 1800
```

Replace the path. This fragment is not a whole file. If you remove Claude, also remove the Claude cell from every permission profile. Restart the daemon (see [running the daemon](../operations/running-the-daemon.md)); provider settings are read at start. Selected deny groups are the exception described below. A missing/non-executable binary reports the path or searched directories. Use an absolute binary path where launchd's PATH does not include your installation.

## Models and conversations

`model_tier` selects `flagship`, `balanced` or `fast` through the central [model table](config-yaml.md#tiers). Both agents default to `balanced`; changing an alias does not establish provider availability. Configure literal model names only in that table. Native team delivery also resolves specialist tier assignments against the configured provider.

Each agent defaults to a 1,800-second wall-clock timeout, independent of whether its process emits completion events. Claude defaults to `autocompact: 100k` (allowed: `auto`, `100k` to `1000k`, `1M`). A timeout is a failed/interrupted invocation, not proof that edits or external actions did not occur. Inspect the report and [recovery guidance](../operations/recovery.md).

Follow-up runs normally resume the retained conversation. Agent identity belongs to its conversation segment. An ordinary owner turn selecting a different configured agent starts a fresh segment; it does not convert one provider's conversation into another's. Automatic recovery is bound to the original segment and refuses an agent mismatch; use a new ordinary owner instruction to switch, then inspect the next report. Factory changes to effective permissions or delivered instructions can require a fresh conversation. Missing/unusable provider resume state can trigger the documented cold fallback; do not treat a resumed process as an unchanged permission boundary.

## Permissions and troubleshooting

[Named deny groups](permissions.md#named-deny-groups) add restrictions to graph launches. Claude supports command, file-tool and all/named native-subagent restrictions. Codex 0.160.0 supports plain literal command prefixes ending in `:*` and bare `Agent` to disable all delegation; file-tool, named-agent and unsupported command meanings refuse before launch. Graph defaults and node groups combine additively. Each new or resumed attempt resolves current operator settings once and retains its snapshot through capacity retries and cold fallback. No selection keeps the existing provider path. Selected Codex attempts use a protected stable configuration home while retaining the canonical file login, sessions and usage storage under `HOME/.codex`. They preserve sandbox limits, exclude personal/project policy layers and serialize shared sessions, including group removal. Unsupported versions, credential modes, changed artifacts or unsafe placement refuse before process creation with a corrective explanation. Adding or removing groups does not require a new conversation or login. See the support guide for limits and the owner live checks; controlled-process tests do not prove live enforcement. A shell-launched CLI is governed as a command, not a native subagent.

- **Build cache denied:** add only the needed absolute directory to Codex `writable_roots`; restart. Factory does not infer home-directory grants.
- **Network or local test listener denied:** `network_access` is a separate opt-in for workspace-write; it grants outbound networking too.
- **Personal Codex settings/skills absent:** default `allow_user_config: false` isolates ordinary runs. Enabling it also admits personal configuration on ordinary runs; selected deny-group runs continue to isolate policy. Native team delivery still supplies isolated factory-owned role files.
- **Claude auth unexpectedly fails:** check whether opted-in `forward_anthropic_env` overrides normal CLI authentication. Keep secrets out of configuration and reports.
- **Wrong permissions:** verify the selected mode's cell and the [permission boundaries](permissions.md), not a deprecated issue permission label.

Inspect the run report and [local logs](../operations/running-the-daemon.md) for the effective agent, mode and outcome. Local logs are unredacted.
