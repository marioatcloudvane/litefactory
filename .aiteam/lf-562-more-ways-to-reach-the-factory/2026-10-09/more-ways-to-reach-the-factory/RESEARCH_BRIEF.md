# Research brief

## Request

Shape more-ways-to-reach-the-factory: settle Discord first-release goal, boundaries, data model, interfaces, permissions, validation and edge cases.

## Decisions

- **d1** — This pass shapes the Discord first release without code, stories, runtime changes, commits or publication.
- **d2** — Add a transport-neutral two-way messaging boundary alongside issue tracking while GitHub remains the authoritative task and workflow record.
- **d3** — Use one server-installed Discord bot per factory with slash commands over Gateway and REST delivery, without arbitrary-message ingestion.
- **d4** — Keep operator-managed connection settings, principal mappings and explicit guild/channel project bindings in a factory communication configuration section.
- **d5** — Each channel binding selects factory scope or one registered repository and can only narrow factory capabilities.
- **d6** — Separate transport delivery, identity and action admission, and existing factory services; model agents cannot grant authority.
- **d7** — Normalize inbound events as platform, connection, immutable interaction ID, numeric actor and guild/channel IDs, action, text, task target and optional wait/revision correlation.
- **d8** — Persist admitted requests, resolved identity and scope, processing state and outbound delivery records with stable delivery keys and remote message IDs.
- **d9** — Require unique connection and binding IDs, valid credential references, registered repository targets, nonconflicting channel bindings and explicit numeric identity mappings.
- **d10** — Reject unbound guilds/channels, DMs, bots, unknown actors, unauthorized actions, blank or oversized inputs and task targets outside the selected project.
- **d11** — Acknowledge Discord interactions within three seconds and deliver longer results through durable bot messages rather than expiring interaction tokens.
- **d12** — Deduplicate interactions across reconnects and serialize conflicting task actions through existing factory admission.
- **d13** — Honor rate limits and retain uncertain deliveries for reconciliation without repeating workflow execution.
- **d14** — Deleted or inaccessible destinations block delivery visibly in factory status and never cause fallback to another channel.
- **d15** — Disabled connections stop new admission, and permission or binding changes are rechecked before processing and sending retained work.
- **d16** — Doctor commands use explicit issue targets and existing diagnosis isolation checks, with no Discord repair, restart or apply authority.
- **d17** — Research answers must reference the exact active task, question wait and displayed revision, and stale or consumed answers are rejected.
- **d18** — Acceptance requires authentic Discord evidence for command acknowledgment, access rejection, long-result delivery, reconnect deduplication and missing-destination handling before enabling the integration.


## Assumptions

- **a1** — GitHub remains the task system because this checkout exposes only that provider; Linear support is unverified and excluded from this release.
- **a2** — The proposed first-release story is that the owner checks a bound project's status, requests diagnosis for an existing issue and answers a research question from Discord without starting an unrelated workflow.
- **a3** — Factory YAML is the configuration source of truth because configuration is already operator-owned; no separate team tenant or editable Discord administration surface is proposed.
- **a4** — The proposed permission default is an explicit mapping from a Discord numeric user ID to the existing authenticated owner, without role-based approval or self-service account linking.
- **a5** — The proposed command surface is help, factory health, project/task status, doctor diagnose and research answer, plus configured task-state and question notifications.
- **a6** — One Discord connection is sufficient for the first release because the issue requests one implementation; the normalized interface leaves other transports possible without building them now.
- **a7** — No live Discord integration has been exercised; engineering validation and doctor provider isolation remain separate launch requirements.
- **a8** — Proposed delivery uses approved restricted guild channels with redacted outputs and no attachments, raw logs, source dumps or automatic mentions.
- **a9** — Proposed retention removes Discord-only redacted text after 30 days while canonical task answers follow existing task retention and minimal deduplication/audit metadata survives until task archival.
- **a10** — Doctor retains its existing 1–32768-byte instruction limit, research answers use the existing workflow limit, and transport oversize input is rejected rather than silently truncated.
- **a11** — Factory health exposes a bounded operational summary, and project status exposes existing task state and links without model invocation.
- **a12** — Notifications cover waiting-for-owner, completion and failure transitions for explicitly bound existing tasks, with no subscription management or arbitrary event feed.
- **a13** — Offline commands are not promised durable acceptance unless the factory has acknowledged and persisted them; saturated intake returns an explicit retry response.
- **a14** — A Discord research answer is recorded once as authenticated workflow input with its source identity, while formal review approval remains on the existing owner surface.


## Open questions

### q1 — Which use cases should the first Discord release include?

_Asked of: Product Owner Questions_

- **a** — Recommended: help, health and status, issue diagnosis, research question answers and task notifications; keep formal approvals and workflow starts on GitHub.  *(recommended)*
- **b** — Read-only launch: help, health, status and notifications only; defer diagnosis and research answers.
- **c** — Add project chat and task proposals to the recommended set; this requires a separate conversation context, budget and explicit task-creation review.

### q2 — Who may use the first Discord integration?

_Asked of: Product Owner Questions_

- **a** — Recommended: only explicitly mapped existing owners may run commands or answer questions; Discord roles do not grant authority.  *(recommended)*
- **b** — Allow explicitly mapped collaborators to read status and request diagnosis, while research answers remain owner-only; add per-project capability assignments.

### q3 — Where may factory replies appear and how long may Discord-only conversation text be retained?

_Asked of: Product Owner Questions_

- **a** — Recommended: redacted replies and notifications in explicitly approved restricted server channels; remove locally retained Discord-only text after 30 days, preserve canonical task records, and retain minimal deduplication metadata until archival; Discord-hosted messages follow server policy.  *(recommended)*
- **b** — Keep Discord limited to acknowledgments, brief status and links to GitHub; do not deliver diagnosis or research question text there, reducing disclosure but requiring GitHub for detailed conversations.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
