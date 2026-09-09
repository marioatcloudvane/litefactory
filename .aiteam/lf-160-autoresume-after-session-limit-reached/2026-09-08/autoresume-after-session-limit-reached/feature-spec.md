# Feature Spec: autoresume-after-session-limit-reached
# Status: APPROVED
# Date: 2026-09-08

## Goal
- Allow the owner to opt into durable automatic continuation of interrupted Codex and Claude Code work once provider session quota resets, with visible scheduling and preserved conversation context.

## User Stories
| ID | Story |
|---|---|
| US-1 | As the owner, I want recovery explicitly configurable and disabled by default so unattended retries remain my choice. |
| US-2 | As the owner, I want genuine session-limit interruptions detected for both coding agents so unrelated failures are not retried. |
| US-3 | As the owner, I want recovery durably scheduled and visibly reported so I know when interrupted work can continue. |
| US-4 | As the owner, I want the interrupted conversation resumed only while its intent remains current so obsolete work cannot restart. |
| US-5 | As the owner, I want recovery to survive restarts and preserve schedule dependencies so temporary quota exhaustion neither loses work nor releases dependent work early. |

## Data Model
| Entity | Field | Type | Required | Constraints |
|---|---|---|---|---|
| Daemon configuration | auto_resume_on_session_limit | boolean | No | Global strict YAML setting; absent defaults to false. |
| Run interruption | source run, reason, evidence kind | existing run identity and bounded classifications | Yes | Durable; session quota distinct from ordinary failure and invalid resume. |
| Run interruption | reset bound and precision | UTC instant and bounded precision | No | Trusted provider diagnostic; absent or invalid prevents automatic recovery. |
| Continuation | source run | existing run identity | Yes | At most one continuation per interrupted run, including issue-body triggers. |
| Continuation | root interrupted run and attempt | identity and integer | Yes | Maximum three actual automatic invocations per root interruption. |
| Continuation | due time | UTC instant | Yes | Latest required reset bound plus 60 elapsed seconds. |
| Continuation | task, worktree, agent, mode, segment, resume handle | existing identities | Yes | Snapshot interrupted intent; no automatic cold conversation. |
| Continuation | originating schedule | schedule identity | No | Preserve prerequisite identity across continuation chains. |
| Continuation | lifecycle and cancellation reason | bounded values | Yes | Pending/reserved/running/terminal recovery distinguishable durably. |
| Announcement | continuation identity and delivered comment | durable references | Yes/No | Recover reporting independently of schedule creation and execution. |

### Relationships
- Interrupted run → at most one continuation; continuation attempt → same root interruption and preserved conversation.
- Interrupted scheduled prerequisite → recovery chain; descendants remain attached to the original prerequisite.
- Managed Implement continuation → factory-supplied approved story revision bundle; story completion remains distinct from aggregate feature success.

## API / Interface Changes
| Method | Endpoint / Action | Auth | Notes |
|---|---|---|---|
| Configuration | auto_resume_on_session_limit | Existing configuration access | Default/template/documentation false; existing loaded-configuration lifecycle. |
| Factory action | Existing durable scheduler | Trusted internal factory authority | Persist automatic action before announcement; no new public command or grammar. |
| Issue comment | /schedule -to MM/DD/YYYY HH:MM:SS UTC continuation payload | Factory identity | Human-visible record of persisted action; bot ingestion remains inert. |
| Agent result | Normalized session-limit evidence | Provider-owned terminal diagnostics | Shared provider-neutral metadata; arbitrary generated prose carries no scheduling authority. |

## Business Rules
- Ship both Codex and Claude Code through their existing CLI runner paths; preserve authentic diagnostic evidence and distinguish observed forms from synthetic edge cases.
- Schedule only when enabled, a terminal quota interruption has reliable future reset evidence, a usable same-conversation identity exists, and the attempt cap is not exhausted.
- Claude requires blocking top-level rejection plus terminal error semantics; allowed/allowed_warning notifications and overage rejection alone do not qualify. Interpret resetsAt as Unix seconds; subtype success with is_error=true remains an error.
- Codex permits narrowly recognized provider-owned terminal usage-limit error messages, including nested turn.failed.error.message; ordinary agent messages do not qualify.
- For all explicitly known exhausted windows require usable resets and choose the latest necessary bound. A nonexhausted later window must not delay recovery; an unenumerated hypothetical window is not a blocker.
- Interpret timezone-less Codex retry clocks in machine-local time without modifying child environments. Explicit dates use that location; time-only text requires invocation-start and terminal-observation bounds on the same local date.
- Resolve a unique Codex displayed minute interval, then use its next-minute boundary plus 60 elapsed seconds: 15:00 becomes 15:02. Claude exact seconds use reset plus 60 seconds. Persist UTC once and do not reinterpret queued deadlines after timezone changes.
- Due time is earliest eligibility; normal polling, worker capacity, locks, issue state and forge backoff may delay execution.
- Persist interruption evidence and continuation intent before posting; one source-run identity prevents duplicate scheduling across completion/replay/restart.
- Preserve newly discovered conversation handles on verified quota interruption. A resumed Codex refusal before turn.started must not trigger an immediate cold rerun.
- Resume the original worktree, agent, mode, segment and explicit conversation with the interrupted request context; retain existing permission resolution and factory-supplied managed story revisions.
- Preserve current work-saving policy; reports truthfully describe interruption, saved/unsaved work and scheduling without claiming implementation or feature success.
- Permit at most three actual automatic invocations per root interruption; reservation and reporting retries do not consume attempts. Each further automatic invocation requires newly verified future-reset evidence.
- Before due automatic dispatch, reconcile newer admitted owner work, conversation reset and changed agent/mode/segment; those changes permanently cancel stale continuations. Schedule controls alone do not supersede unrelated work.
- Issue closure or effective false in loaded configuration cancels pending automatic work; reopening/re-enabling does not resurrect it. Running invocations retain existing behavior; no configuration hot reload is added.
- An interrupted scheduled prerequisite remains durably suspended/nonterminal while eligible recovery is pending/running; descendants wait through restarts and repeated limits.
- Eventual semantic success releases descendants using final completion time; terminal failure, cancellation, missing recovery evidence/identity or exhausted attempts blocks descendants. Exit code zero alone cannot prove success.

## Edge Cases & Error Handling
- Missing/malformed/stale reset at interruption, unsupported diagnostic, known exhausted window without a reset, missing/invalid resume identity → explain manual recovery; no guessed retry and no cold start.
- Time-only midnight-spanning Codex output, stale local clock, DST fold/gap, contradictory explicit zone, unavailable/inconsistent machine-local context → report-only; never roll yesterday's clock into tomorrow or normalize invalid civil time.
- Persisted valid due time passes during downtime → retain work and dispatch when normal admission permits.
- Initial issue-body, owner-comment and scheduled-trigger interruptions → same source-run deduplication, independent of nonzero comment identities.
- Crash after reservation → reconcile durable reservation/run evidence before launching another attempt.
- Announcement fails or response is lost → retry delivery using durable identity; existing at-least-once comment semantics may duplicate a comment but never the schedule or execution.
- Pre-dispatch owner/issue observation fails → defer rather than assume continuation is current.
- Cancellation races with reservation → no launch after cancellation wins admission; already-running behavior unchanged.
- Fresh quota rejection on automatic attempt → next verified reset within remaining attempt budget; final capped failure visibly requests manual recovery.

## Assumptions and Provenance
| Decision | Basis |
|---|---|
| Both agents, conservative displayed precision, machine-local Codex timezone without environment override | Explicit settled scope carried from issue #142 into #160; imported PROVIDER_EVIDENCE.md supersedes the research brief's original detection blocker. |
| Global snake_case opt-in setting and internally persisted action with visible /schedule record | Owner requirement plus existing configuration/scheduler and bot-admission conventions. |
| Three automatic invocations per root interruption | Carried-forward headless product assumption; bounds unattended repeated exhaustion without an extra setting. |
| New owner work/context changes, issue closure and loaded disable cancel pending recovery permanently | Carried-forward headless assumption prioritizing current owner intent. |
| Scheduled dependencies wait through eligible recovery | Existing prerequisite semantics require actual eventual success. |
| Historical Codex binary version remains unknown; Claude captures identify 2.1.261/2.1.263 | Authentic preserved evidence; document supported current diagnostic versions during implementation without recapturing paid quota events. |
| Status APPROVED denotes readiness for architectural planning | Headless Plan convention; no claim of separate Implement authorization. |
| New managed stories under #160; no adoption of superseded #149–#153 | Issue states old children are closed and unmanaged; factory alone supplies identities and publishes the proposal. |

## Out of Scope
- Other agents, generic retries for unrelated errors, provider-account-wide quota coordination, automatic cold starts.
- Arbitrary model-prose parsing, speculative reset dates/windows, provider transport migration or fresh paid quota-exhaustion experiments.
- Replacement scheduler, public schedule grammar changes, relaxed bot admission or permissions, altered ordinary partial-work policy.
- Per-repository overrides, retry-count configuration, hot reload, new public cancellation commands.
- Agent-side forge reads/publication, adoption of superseded story identities, local per-story Markdown sources, implementation during this Plan run.

## Acceptance Criteria
- [ ] AC-F1: Absent/false configuration creates and dispatches no automatic continuations; true enables eligible recovery; template/documentation default false.
- [ ] AC-F2: Authentic sanitized Codex and Claude diagnostic fixtures demonstrate terminal quota detection through existing runner paths, with honest provenance and documented supported forms.
- [ ] AC-F3: Allowed/warning events, generic failures, model prose, malformed/stale resets and missing known blocking-window resets never schedule recovery.
- [ ] AC-F4: Claude exact resets schedule no earlier than +60 seconds; Codex local minute bounds schedule no earlier than next-minute-boundary +60 seconds; ambiguous date/DST/zone cases remain report-only and child environments are unchanged.
- [ ] AC-F5: Persisted UTC deadlines survive restart/timezone changes without reinterpretation; valid overdue work remains eligible.
- [ ] AC-F6: A factory interruption comment identifies the persisted absolute-UTC /schedule action; consuming that comment cannot create extra work.
- [ ] AC-F7: First-run/resumed interruptions preserve conversation and task context, including managed approved revisions; pre-turn quota and unusable handles never cause automatic cold fallback.
- [ ] AC-F8: Issue-body/comment/schedule triggers, duplicate completion, report replay and crash boundaries produce one continuation per source run and no duplicate invocation.
- [ ] AC-F9: Failed/lost comment delivery does not lose durable scheduling; reporting recovers with truthful work and outcome status.
- [ ] AC-F10: New admitted work, context changes, issue closure and loaded disable cancel stale pending recovery before dispatch; observation failures defer.
- [ ] AC-F11: Repeated quota exhaustion stops after three actual automatic invocations and reports manual recovery; delivery/reservation retries do not consume attempts.
- [ ] AC-F12: Scheduled descendants wait through recovery, release only after eventual semantic success, and block on terminal failure/cancellation including exit-zero quota errors.
- [ ] AC-F13: Existing manual scheduling, authorization, permission policy, work-saving behavior, worker/lock limits and ordinary failure handling retain regression coverage.
