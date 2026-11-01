# Research brief

## Request

Add configurable plain-language operator alerts in Discord (disk low, factory blocked, provider failure, task stuck, main red, deploy failed). One message per incident with cooldown, an all-clear on resolution, and owner mute and list controls.

## Decisions

- **S-01** — mode=feature, size=large — a new alerting capability across daemon, store, Discord and config. (agent's call)
- **O-01** — Each rule raises one alert per incident, honours cooldown, mute and quiet hours, and posts an all-clear on resolution. (owner said: issue #1308 acceptance)
- **O-02** — Every rule can be disabled in config, and a disabled rule never posts. (owner said: issue #1308 acceptance)
- **N-01** — No e-mail, push, other channels, per-repository routing or dashboard in the first version. (owner said: issue #1308 'Not included')
- **K-01** — Chat must keep not starting, approving, stopping or repairing work; mute commands only change alert state. (agent's call)


## Assumptions

- **a1** — Alerts go through the existing Announce path and notice channel, with alerts.channel_id as an override. (agent's call)
- **a2** — Incident state (open, muted, resolved, last sent) is stored in SQLite so it survives restart. (agent's call)
- **a3** — Main-red and deploy-failed signals can come from GitHub workflow_run or check_run webhooks, with polling as fallback. Not verified against GitHub app permissions. (agent's call)
- **a4** — Alerts appear in factory_overview as a read-only list. This is cheap and matches the issue's question. (agent's call)
- **a5** — Size is large. Rules should be built in slices: engine plus disk first, then pool and stuck tasks, then provider, then CI and deploy. (agent's call)

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
