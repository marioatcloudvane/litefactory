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

Replace the path. This fragment is not a whole file. If you remove Claude, also remove the Claude cell from every permission profile. Restart the daemon (see [running the daemon](../operations/running-the-daemon.md)); the daemon reads the file only at start. A missing/non-executable binary reports the path or searched directories. Use an absolute binary path where launchd's PATH does not include your installation.

## Models and conversations

`model_tier` selects `flagship`, `balanced` or `fast` through the central [model table](config-yaml.md#tiers). Both agents default to `balanced`; changing an alias does not establish provider availability. Configure literal model names only in that table. Native team delivery also resolves specialist tier assignments against the configured provider.

Each agent defaults to a 1,800-second wall-clock timeout, independent of whether its process emits completion events. Claude defaults to `autocompact: 100k` (allowed: `auto`, `100k` to `1000k`, `1M`). A timeout is a failed/interrupted invocation, not proof that edits or external actions did not occur. Inspect the report and [recovery guidance](../operations/recovery.md).

Follow-up runs normally resume the retained conversation. Agent identity belongs to its conversation segment. An ordinary owner turn selecting a different configured agent starts a fresh segment; it does not convert one provider's conversation into another's. Automatic recovery is bound to the original segment and refuses an agent mismatch; use a new ordinary owner instruction to switch, then inspect the next report. Factory changes to effective permissions or delivered instructions can require a fresh conversation. Missing/unusable provider resume state can trigger the documented cold fallback; do not treat a resumed process as an unchanged permission boundary.

## Permissions and troubleshooting

- **Build cache denied:** add only the needed absolute directory to Codex `writable_roots`; restart. Factory does not infer home-directory grants.
- **Network or local test listener denied:** `network_access` is a separate opt-in for workspace-write; it grants outbound networking too.
- **Personal Codex settings/skills absent:** default `allow_user_config: false` isolates ordinary runs. Enabling it also admits personal configuration; native team delivery still supplies isolated factory-owned role files.
- **Claude auth unexpectedly fails:** check whether opted-in `forward_anthropic_env` overrides normal CLI authentication. Keep secrets out of configuration and reports.
- **Wrong permissions:** verify the selected mode's cell and the [permission boundaries](permissions.md), not a deprecated issue permission label.

Inspect the run report and [local logs](../operations/running-the-daemon.md) for the effective agent, mode and outcome. Local logs are unredacted.
