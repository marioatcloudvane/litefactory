# Research brief

## Request

Shape more-ways-to-reach-the-factory: settle Discord first-release goal, boundaries, data model, interfaces, permissions, validation and edge cases.

## Decisions

- **d1** — This pass shapes the Discord first release without code, stories, runtime changes, commits or publication.
- **d2** — Discord channels are authoritative for conversation history, GitHub issues for created tasks, and existing factory records for execution, approvals and meter evidence.
- **d3** — Use one server-installed Discord bot with explicit slash-command chat, Gateway intake and REST replies, without passive channel-message ingestion.
- **d4** — Keep operator-managed connection settings, principal mappings and explicit guild/channel project bindings in a factory communication configuration section.
- **d5** — Each channel binding selects factory scope or one registered repository and can only narrow factory capabilities.
- **d6** — Separate transport delivery, identity and action admission, and existing factory services; model agents cannot grant authority.
- **d7** — Normalized events carry interaction and source-message IDs, actor and destination IDs, explicit action, project/task target and any wait or draft revision.
- **d8** — Persist only identity and scope references, command fingerprints, correlation and effect states, remote message IDs and temporary redacted payloads needed for unresolved work.
- **d9** — Require unique connection and binding IDs, valid credential references, registered repository targets, nonconflicting channel bindings and explicit numeric identity mappings.
- **d10** — Reject unbound guilds/channels, DMs, bots, unknown actors, unauthorized actions, blank or oversized inputs and task targets outside the selected project.
- **d11** — Acknowledge Discord interactions within three seconds and deliver longer results through durable bot messages rather than expiring interaction tokens.
- **d12** — Deduplicate interactions across reconnects and serialize conflicting task actions through existing factory admission.
- **d13** — Honor rate limits and retain uncertain deliveries for reconciliation without repeating workflow execution.
- **d14** — Deleted or inaccessible destinations block delivery visibly in factory status and never cause fallback to another channel.
- **d15** — Disabled connections stop new admission, and permission or binding changes are rechecked before processing and sending retained work.
- **d16** — Doctor commands use explicit issue targets and existing diagnosis isolation checks, with no Discord repair, restart or apply authority.
- **d17** — Research answers must reference the exact active task, question wait and displayed revision, and stale or consumed answers are rejected.
- **d18** — Launch acceptance requires authentic Discord evidence for owner checks, chat context retrieval, project answers, meter fidelity, exact-draft issue confirmation, notification delivery, restart deduplication and transient-text cleanup.
- **d19** — Owner Q1 settles inclusion of project questions, in-progress status, notifications, existing meter reporting and issue creation from chat alongside help, diagnosis and research answers.
- **d20** — Owner Q2 settles command and confirmation access to the explicitly mapped authenticated owner only.
- **d21** — Owner Q3 settles channel-owned conversation history and minimum necessary factory storage instead of a local transcript archive.
- **d22** — The chat surface accepts explicit questions within a bound project and retrieves current issue and factory state through read-only services.
- **d23** — Answers cite task links and observation time, disclose unavailable or stale state, and never claim execution completion from chat prose.
- **d24** — Conversation context consists of the latest 20 bot-authored accepted-turn records in the same channel or bound child thread, capped at 32768 UTF-8 bytes with visible truncation.
- **d25** — The bot publishes each accepted chat input with its reply so later commands can recover validated owner turns without ingesting arbitrary human messages.
- **d26** — Issue creation previews one redacted title and body in Discord and requires a separate owner confirmation bound to repository, preview message, content fingerprint and revision.
- **d27** — Issue confirmation expires after 24 hours and rejects edited, missing, superseded or already-consumed previews.
- **d28** — Confirmed issue creation records its remote issue identity and returns its link without starting research or another workflow.
- **d29** — Uncertain issue-creation effects are reconciled by their stable request marker before retry, and unresolved ambiguity blocks another creation.
- **d30** — Meter commands reuse current measured tokens, frozen USD estimates, known subtotals, coverage and model attribution without treating unknown values as zero.
- **d31** — Meter supports existing factory, project and task scopes and mode, coding-agent and model filters within channel authority, with optional bounded detail pages.
- **d32** — Notifications report waiting-for-owner, run start, mode/stage changes, completion, failure and stop transitions once per transition with task links and final run meter summary when available.
- **d33** — Successful or canceled Discord-only work clears local text promptly, while unresolved text is cleared after a maximum 24 hours and retained status becomes expired.
- **d34** — An unresolved remote mutation remains fenced by its effect metadata even after payload expiry and is never automatically repeated.
- **d35** — Deleted or unreadable conversation messages are not reconstructed from a local archive, and actions requiring missing context require a fresh owner command.
- **d36** — Existing doctor, workflow and meter evidence retention remains intact because it records execution rather than a duplicated Discord transcript.
- **d37** — Bot permissions cover approved channels and threads, history reads and sends, without administrator permission or arbitrary-message content intake.
- **d38** — Identity, scope, command fingerprints and unresolved-effect references survive restart, while resolved deduplication keys remain until the bound task is archived.
- **d39** — Generic factory-level chat requires an explicit project for project-specific questions or issue drafts and never guesses between repositories.


## Assumptions

- **a1** — GitHub remains the task system because this checkout exposes only that provider; Linear support is unverified and excluded from this release.
- **a2** — The primary story is that the authorized owner asks about an open project, checks progress and usage, receives changes and develops a feature idea into a confirmed issue from Discord.
- **a3** — Factory YAML is the configuration source of truth because configuration is already operator-owned; no separate team tenant or editable Discord administration surface is proposed.
- **a4** — Owner-only access is settled by Q2 and implemented as explicit numeric Discord-to-factory owner mapping rather than Discord role membership.
- **a5** — The interface includes help, health, status, ask, meter, doctor diagnose, research answer, issue draft and issue confirm plus automatic bound-project transition notifications.
- **a6** — One Discord connection is sufficient for the first release because the issue requests one implementation; the normalized interface leaves other transports possible without building them now.
- **a7** — No live Discord integration has been exercised; engineering validation and doctor provider isolation remain separate launch requirements.
- **a8** — Use explicitly bound server channels and their child threads for replies, with output redaction and no attachments, raw logs, source dumps or automatic mentions.
- **a9** — Q3 replaces the proposed 30-day transcript cache with channel-owned history, prompt text deletion after resolved delivery and a 24-hour maximum for temporary unresolved payloads.
- **a10** — Doctor retains its existing 1–32768-byte instruction limit, research answers use the existing workflow limit, and transport oversize input is rejected rather than silently truncated.
- **a11** — Status and meter read existing services directly, while natural-language project questions may use a metered model restricted to read-only project evidence.
- **a12** — Automatically notify bound projects of meaningful task transitions without a separate subscription management surface.
- **a13** — Offline commands are not promised durable acceptance unless the factory has acknowledged and persisted them; saturated intake returns an explicit retry response.
- **a14** — Research answers retain only the evidence required by the existing authenticated continuation contract rather than a parallel Discord conversation archive.
- **a15** — Interpret meter functionality as reporting existing usage and price-catalog information, with pricing edits, recovery mutations and CSV exports remaining on the existing CLI.
- **a16** — Interpret channels as conversation truth rather than a replacement for existing execution, review and accounting stores because those are required for restart safety and audit.
- **a17** — Explicit slash-command chat and server threads satisfy chat from the issue without a privileged passive-message reader; arbitrary natural-language posts do not invoke the factory.
- **a18** — A 24-hour draft confirmation and temporary-payload lifetime bounds unattended remote effects while giving the owner time to review.
- **a19** — One confirmed draft creates one ordinary feature issue with a source conversation link and stable effect marker, without labels or instructions that auto-start a workflow.
- **a20** — Project questions use current issue descriptions, saved stage results and run state, excluding repository-wide source exploration or write-capable tools.
- **a21** — Chat question input is capped at 8192 UTF-8 bytes and draft content must fit existing issue limits; all oversize rejection is explicit.
- **a22** — Bot-authored conversation records are authenticated by remote bot identity and accepted-turn references, while their text remains untrusted model input rather than permission.
- **a23** — Factory-scope health and meter requests without a task retain minimal replay keys for 30 days because they have no task archival boundary.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
