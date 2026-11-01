# Research brief

## Request

Add configurable plain-language operator alerts in Discord (disk low, factory blocked, provider failure, task stuck, main red, deploy failed). One message per incident with cooldown, an all-clear on resolution, and owner mute and list controls.

## Decisions

- **S-01** — mode=feature+integration, size=large — a new alerting capability across daemon, store, Discord and config, plus a new GitHub CI input. (agent's call)
- **O-01** — Each rule raises one alert per incident, honours cooldown, mute and quiet hours, and posts an all-clear on resolution. (owner said: issue #1308 acceptance)
- **O-02** — Every rule can be disabled in config, and a disabled rule never posts. (owner said: issue #1308 acceptance)
- **O-03** — Disk low is a critical alert raised when free space is below the run threshold, cleared when it is back above. (owner said: issue #1308 alert table)
- **O-04** — Factory blocked is a critical alert raised when the pool is full longer than its 'after' (default 30m) while work waits, or a task shows no progress longer than its 'after' (default 2h). (owner said: issue #1308 table and config draft)
- **O-05** — Provider failure is a critical alert for sign-in expired, quota, repeated safety refusal or repeated 5xx from an agent or conversation provider. (owner said: issue #1308 table)
- **O-06** — Task stuck is a warning raised when a task's queue stops, a report delivery stays uncertain longer than 'after' (default 15m), or a dispatch is dead. (owner said: issue #1308 table)
- **O-07** — Main red is a warning raised when CI on the factory's own main branch fails, cleared when it passes again. (owner said: issue #1308 table; in this issue pending q1)
- **O-08** — Deploy failed is a warning raised when a watched project's production deploy fails or refuses. (owner said: issue #1308 table; what counts as a deploy pending q2)
- **O-09** — Every alert says in plain words what happens, what is affected and what the owner can do, like the #1273 style. (owner said: issue #1308 Rules)
- **O-10** — Critical alerts are sent at once and mention the owner per mention_owner_on (default critical) using the existing Discord owner ID. (owner said: issue #1308 Rules; owner ID reuse agent's call)
- **O-11** — During quiet hours warnings are held and sent as one digest when quiet hours end; critical alerts still go at once. (owner said: issue #1308 config draft; digest timing agent's call)
- **O-12** — The owner can mute one rule for a time, turn all alerts off until a time, and list active alerts from Discord. (owner said: issue #1308 Rules; command form pending q4)
- **O-13** — Incident state (open, muted, resolved, last sent) survives a daemon restart, so a restart neither repeats nor loses an alert. (agent's call)
- **O-14** — factory_overview lists active alerts read-only, so ChatGPT and Claude see them too. (agent's call)
- **O-15** — Alerts are configured in a new optional 'alerts' block as drafted in the issue, plus an overall enabled switch. (owner said: issue #1308 config draft; switch agent's call)
- **O-16** — A simulated disk-low and a simulated stuck-task incident each reach the owner's Discord as one readable message, then an all-clear. (owner said: issue #1308 acceptance, manual)
- **N-01** — No e-mail, push, other channels, per-repository routing or dashboard in the first version. (owner said: issue #1308 'Not included')
- **N-02** — Alerts never fix anything themselves; a suggested fix like 'go clean -cache' is text only. (agent's call)
- **N-03** — No alert history view, no posting alerts to GitHub or Linear issues, and no different channels per rule. (agent's call)
- **N-04** — No CI alerts for project repositories; only the factory's own main is watched for red CI. (agent's call, from issue #1308 'CI on the factory's main')
- **N-05** — No natural-language alert control through the conversation model in this version. (agent's call; depends on q4)
- **K-01** — Chat must keep not starting, approving, stopping or repairing work; mute commands only change alert state. (agent's call)
- **K-02** — The disk hold, its WARN log line and pool scheduling behave exactly as today; alerts only observe. (agent's call)
- **K-03** — Existing notices, conversations and the 'new conversation' phrase keep working unchanged. (agent's call)
- **K-04** — An existing config without an 'alerts' block still loads and validates. (agent's call)
- **K-05** — Existing GitHub and Linear webhook subscriptions keep working with their current events. (agent's call)


## Assumptions

- **a1** — Alerts go through the existing Announce path and notice channel, with alerts.channel_id as an override that must be an allowed channel. If neither resolves, alerts stay off and doctor says so. (agent's call)
- **a2** — Incident state is stored in SQLite with a new migration. Promoted to O-13. (agent's call)
- **a3** — Main-red and deploy-failed results come from GitHub Actions run results; webhook versus polling is open in q3. (agent's call)
- **a4** — Alerts appear in factory_overview as a read-only list. Promoted to O-14. (agent's call)
- **a5** — Built in slices: alert engine plus disk first, then pool and stuck tasks, then provider, then CI and deploy. (agent's call)
- **a6** — An incident is one rule plus one subject (disk; pool; a task; a provider account; a repo workflow). Two stuck tasks are two incidents. (agent's call)
- **a7** — 'Gets worse' means the severity rises; then one new message goes out. No reminders while an incident stays open at the same severity. (agent's call)
- **a8** — Cooldown (default 1h) stops the same incident from alerting again within an hour after its all-clear, so a flapping condition does not spam. (agent's call)
- **a9** — An all-clear is sent once the condition has been clear on two checks in a row. A muted incident also sends no all-clear while muted. (agent's call)
- **a10** — 'No progress' ignores tasks waiting for the owner or on a planned wait; progress means a stage change, run start or end, or a new result. (agent's call)
- **a11** — Provider thresholds: sign-in expired and quota alert at once; safety refusal after 2 in a row on the same conversation or task; 5xx after 3 in a row within 15 minutes. (agent's call)
- **a12** — Only the configured Discord owner can use alert commands; anyone else is ignored. (agent's call)
- **a13** — Outside quiet hours warnings go one by one as they open; the digest is only for quiet hours. (agent's call)
- **a14** — Mute takes minutes, hours or days up to 7 days; 'until tomorrow' means until 07:00 or the end of quiet hours. Times use the factory host's local time zone. (agent's call)
- **a15** — Delivery-uncertain and dead-dispatch signals are read from existing store records; Plan traces which record maps to #1240. (agent's call)


## Open questions

### q1 — Should main-red and deploy-failed alerts be part of this issue?

_Asked of: Product Owner Questions_

- **a** — Yes, all six alerts here, built last (recommended). You named both as real pain (#1179 red for a week, MAR-64 not live); they only need the GitHub CI input from q3.  *(recommended)*
- **b** — Split them into a follow-up issue. This ships the four internal alerts sooner, but main can go red unnoticed again meanwhile.
- **c** — Main-red here, deploy-failed later. Main-red is simple; deploy needs q2 settled first.

### q2 — What counts as a project's production deploy for the deploy-failed alert?

_Asked of: Product Owner Questions_

- **a** — A GitHub Actions workflow you name per project in config, e.g. repo plus workflow 'deploy-prod' (recommended). Clear and explicit; works if the cockpit deploy runs in GitHub Actions.  *(recommended)*
- **b** — Any workflow with 'deploy' in its name in the factory's repos. No config, but may catch the wrong workflows.
- **c** — The project reports its own deploy result to the factory (a small command or MCP call). Needed if the cockpit deploy is a script on a server, not GitHub Actions; tell us which it is.

### q4 — How should you control alerts from Discord?

_Asked of: Product Owner Questions_

- **a** — Short fixed commands: 'alerts', 'mute disk 2h', 'alerts off until 07:00', 'unmute disk' (recommended). Predictable, cannot be misread, works like 'new conversation' today.  *(recommended)*
- **b** — Plain sentences understood by the chat assistant, e.g. 'please mute disk for two hours'. Friendlier, but may misread and depends on the provider being up, which is exactly when provider alerts fire.

### q5 — Should alerts be on by default once this ships?

_Asked of: Product Owner Questions_

- **a** — On by default, posting to the notice channel, all rules enabled (recommended). You stop being blind without editing config; turn off with alerts.enabled: false.  *(recommended)*
- **b** — Off until you add an 'alerts' block. Safer for other installs, but nothing changes until you configure it.

### q3 — How should the factory learn CI and deploy results from GitHub?

_Asked of: Technical Questions_

- **a** — Poll the latest GitHub Actions runs every 5 minutes (recommended). Works on existing installs, no webhook re-registration, survives restarts; costs a few API calls and up to 5 minutes delay.  *(recommended)*
- **b** — Add the workflow_run webhook event. Instant, but every existing repo subscription must be updated and the GitHub app may need more permissions.
- **c** — Both: webhook for speed, poll as safety net. Most robust, most work.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
