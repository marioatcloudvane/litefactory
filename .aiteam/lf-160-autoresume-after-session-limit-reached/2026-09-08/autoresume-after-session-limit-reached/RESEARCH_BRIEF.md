# Research Brief — autoresume-after-session-limit-reached

## Request

[O] Issue #142 requests automatic continuation when a coding agent reaches its session usage limit during a run: report the limit, have litefactory post a `/schedule` event for the provider's reset time plus one minute, and make the behavior configurable through a boolean that defaults to false.

[O] This is a headless Research run. No clarification or approval is available; research ends with this brief, without implementation, commits, pushes, or issue comments sent by the research agent.

Provenance: **[V]** verified during this run, with repository or primary-documentation citations; **[O]** owner requirement; **[W]** second-hand evidence not established as a provider contract. Recommendations and assumptions are explicitly identified as such. No earlier research was carried forward.

## Codebase Context

### Architecture and relevant ownership

[V] This is a single Go 1.26 module. The relevant boundaries are the shared agent result contract, Claude and Codex runner adapters, turn execution and reporting, daemon scheduling/dispatch, the schedule grammar, and SQLite persistence. Both adapters return `agent.RunResult`, with compatibility aliases still consumed by turn/report. The daemon adapts `turn.Execute` into its executor interface. (`go.mod:1`; `internal/agent/result.go:67`; `internal/turn/result.go:113`; `internal/daemon/executor.go:9`.)

[V] The normal flow is owner comment admission, command parsing, mode/agent selection, turn preparation/execution, persistence and harvest, then report and trigger consumption. `/schedule` already branches before agent execution to persist delayed work and acknowledge it. (`internal/daemon/tick.go:1304`, `:1359`, `:1490`; `internal/turn/harvest.go:47`.)

### Scheduling already exists, but bot comments are intentionally inert

[V] A literal factory-authored `/schedule` comment does not currently create a schedule. Admission occurs before command parsing, and rejected comments are consumed. Numeric bot identity, the forge's bot flag, or a factory marker each identify a comment as self-authored. This is an existing loop-prevention and authorization boundary, not a missing parser feature. (`internal/daemon/tick.go:1313`; `internal/turn/classify.go:263`.)

[V] The existing command grammar supports `/schedule -by HH:MM:SS <payload>` and `/schedule -to MM/DD/YYYY HH:MM:SS [UTC|IANA-zone] <payload>`. Absolute times normalize to UTC, ambiguous civil times are rejected, and nested scheduling commands are refused. A reset timestamp can therefore be represented using the absolute UTC form without introducing another public time grammar. (`internal/schedule/schedule.go:46`, `:88`, `:220`, `:246`.)

[V] Scheduling is durable: creation precedes acknowledgment, and the `(session_id, source_comment_id, ordinal)` identity deduplicates repeat creation. However, schedule creation requires a nonzero source comment ID, whereas issue-body runs use trigger ID zero. Automatic continuation needs an identity that also works for those initial runs. (`internal/daemon/tick.go:1359`, `:1153`; `internal/store/migrations/0005_schedules.sql:17`; `internal/store/schedules.go:263`.)

[V] Due schedules run before ordinary comments, share worker limits and in-flight protection, require an open issue, and respect GitHub backoff before atomic reservation. Their private trigger identity is `1<<62 + schedule.ID`. The stored payload and owner handle feed ordinary run resolution, which rereads current mode and agent labels. Existing rows do not pin the interrupted mode, agent, segment, or originating run. (`internal/daemon/schedules.go:17`, `:69`, `:79`, `:97`, `:119`; `internal/store/schedules.go:34`; `internal/daemon/tick.go:1490`.)

[V] Dependency scheduling, agent proposals, reservation/reconciliation, and descendant blocking already exist. Failed schedules block descendants immediately. Orphan recovery requeues a reserved schedule if it has no run and finishes reservations with a run. There is no paused-for-quota lifecycle. (`internal/store/schedules.go:11`, `:186`; `internal/store/runs.go:518`; `internal/store/migrations/0005_schedules.sql:1`.)

### Runner output lacks normalized quota evidence

[V] Shared outcomes are succeeded, failed, timed_out, abandoned, and no_result. Results include final text, reason, stderr, session reference, and a discovered resume token, but no typed quota classification or reset time. (`internal/agent/result.go:8`, `:76`.)

[V] Claude's stream extraction interprets system/init and result events. Its result fields include `is_error`, `result`, cost, duration, and turn count. Other stream events remain in JSONL but do not update interpreted result state. (`internal/harness/stream.go:33`, `:65`, `:160`.)

[V] Codex extraction recognizes thread/turn starts, final agent messages, completion, and failure. Failure extraction reads a top-level `message`; it does not interpret nested error objects or reset metadata. An upstream field's existence would therefore not by itself make that evidence available to the daemon. (`internal/codex/runner.go:315`, `:335`.)

[V] Final model text is explicitly untrusted. Treating the agent's ordinary prose as authoritative scheduling input would cross that trust boundary. Reporting renders and validates output before posting and can refuse it. (`internal/agent/result.go:113`; `internal/turn/report.go:412`.)

### Continuation and failure paths materially affect this feature

[V] Existing subsequent-run preparation resumes Claude after a prior terminal run; Codex additionally requires a persisted resume token. (`internal/turn/prepare.go:453`, `:493`.)

[V] Codex's newly discovered token is persisted into segment state only when `runResult.Succeeded()` is true. A first run that emits `thread.started` and then hits a limit can consequently lose its usable handle from durable segment state and start a new conversation on the next attempt. (`internal/turn/harvest.go:70`.)

[V] A resumed Codex invocation that fails with a nonzero exit and no `turn.started` is classified as not resumable, prompting an immediate cold rerun. A quota refusal before turn start could meet the same condition. Reliable limit detection must distinguish those cases if the intended behavior is to wait and continue. (`internal/codex/runner.go:276`; `internal/turn/harvest.go:108`.)

[V] Failed runs still commit and push partial work; abandoned runs keep changes locally. Outcomes are persisted before harvest. These are existing behaviors that a new interruption classification could affect. (`internal/turn/harvest.go:77`, `:224`.)

[V] Reporting posts before recording the result comment and consuming the trigger. Replay avoids reexecuting a consumed run and reconstructs an unreported result from durable state without restoring the live `Harness` result. Run rows have no structured limit or reset fields. A decision held only in memory can therefore disappear during recovery; comment publication and intent persistence need independent replay treatment. (`internal/turn/report.go:401`, `:423`, `:443`; `internal/turn/execute.go:481`, `:523`; `internal/store/runs.go:75`.)

[V] Scheduled completion currently decides success from durable exit code zero alone, although shared agent outcomes can indicate failure at exit zero. A terminal limit/error event at exit zero could be incorrectly treated as schedule success and release dependent work. (`internal/daemon/schedules.go:145`; `internal/agent/result.go:20`.)

### Configuration and validation surfaces

[V] Configuration is typed strict YAML with defaults applied before decoding; daemon-wide settings use snake_case. Defaults and the generated configuration template are separate surfaces that must agree. (`internal/config/config.go:41`, `:81`, `:359`, `:512`; `internal/config/template.go:26`.)

Recommendation: use `auto_resume_on_session_limit: false` as the public YAML setting, retaining the owner's terminology while matching existing naming. Treat it as daemon-wide unless Plan establishes a reason for per-repository overrides. This is a proposed name and scope, not existing configuration.

[V] Relevant constraints include strict configuration/default/template parity (CFG-1–3), numeric identity checks (SEC-8), real SQLite transaction tests (TEST-6), and deterministic concurrency tests without sleeps (TEST-8). Store retains SQL ownership. No `.aiteam/rulebook.md` exists; global constraints were consulted instead. (`.aiteam/APP_CONSTRAINTS.md:96`, `:114`, `:176`, `:185`.)

[V] Existing tests cover schedule grammar/DST/nesting, dispatch, replay/dependencies/proposal atomicity/orphans, resume/fallback, and shared Codex results. No actual provider session-limit fixtures were identified in the inspected sources. (`internal/schedule/schedule_test.go:83`; `internal/daemon/schedules_test.go:11`; `internal/store/schedules_test.go:8`, `:51`, `:95`, `:123`; `internal/turn/resume_test.go:40`, `:223`, `:284`; `internal/codex/runner_test.go:18`.)

## Domain / Problem Space

### What counts as a session limit

Working assumption: the request concerns exhausted coding-agent subscription/session quota with a provider-established future availability time. Generic network failures, GitHub throttling, invalid credentials, billing failures, and local turn/budget limits are not sufficient evidence for this automatic action. Existing GitHub backoff is separate, in-memory, capped at one hour, and gates dispatch; it is not a durable agent-session reset mechanism. (`internal/daemon/backoff.go:8`.)

[W] An Anthropic upstream issue reports a `rate_limit_event` from Claude Code v2.1.49 with `rate_limit_info.status`, `rateLimitType: "five_hour"`, numeric `resetsAt`, and overage fields. Its example status is **allowed**, so presence of the event is not evidence that execution was interrupted. The same report describes Python SDK v0.1.39 rejecting the unfamiliar event, demonstrating version compatibility risk. This is an upstream reproducer, not a complete contractual definition of blocking states, timestamp units, or multiple quota windows. [Anthropic SDK issue #599](https://github.com/anthropics/claude-agent-sdk-python/issues/599).

[V] Official Codex non-interactive documentation lists JSONL events including `thread.started`, `turn.failed`, and `error`. The fetched documentation does not establish a structured usage-limit reason and reset timestamp for `codex exec --json`. This is an evidence gap, not proof no relevant fields exist. [Codex non-interactive mode](https://learn.chatgpt.com/docs/non-interactive-mode).

[V] Codex **app-server**, a different interface, documents `codexErrorInfo` variants including `UsageLimitExceeded` and an `account/rateLimits/read` method with quota windows and `resetsAt` explicitly in Unix seconds. It also documents rate-limit type, optional credit data, multiple metered buckets, and version-specific generated schemas with experimental-field opt-in. These facts cannot be assumed to describe exec JSON output or used to imply the existing runner already has a usable reset time. [Codex app-server reference](https://learn.chatgpt.com/docs/app-server).

Research conclusion: provider-neutral automatic continuation is not yet supported by verified evidence from the actual deployed command-line versions. Before implementation acceptance, establish the supported version range and sanitized machine-output fixtures for a genuinely blocked run. Do not infer a reset by parsing arbitrary agent prose or by treating any rate-limit event as an interruption.

### Resume support is real; limit-specific guarantees remain unproven

[V] Claude's official sessions documentation supports session-ID resumption and interruption recovery, including local maximum-turn/budget cases. Conversation history persists, but it is not a filesystem snapshot; local transcript availability and working-directory handling matter. This establishes general resumption support, not a guarantee that every subscription-limit interruption leaves a resumable session. [Claude sessions guide](https://code.claude.com/docs/en/agent-sdk/sessions).

[V] Claude CLI supports `--resume` by ID/name and `--continue` for the latest current-directory conversation. The documented search behavior changed in v2.1.223: current behavior searches all local projects after the current project/worktrees; earlier versions searched only the latter. This reinforces the need to reason about deployed versions and explicit session identities. [Claude CLI reference](https://code.claude.com/docs/en/cli-reference).

[V] Codex documents `codex exec resume <SESSION_ID>` with a follow-up prompt, `--last` scoped to the current working directory, and `--all` for broader selection. `--ephemeral` prevents session rollout persistence. Documentation does not establish recovery guarantees for every quota-interrupted session. [Codex developer commands](https://learn.chatgpt.com/docs/developer-commands).

### Time and availability

[O] The requested eligible retry time is the provider's reset instant plus sixty seconds.

Recommendation: represent a verified absolute reset instant in UTC and use the existing scheduler. Preserve the distinction between the earliest eligible time and actual start: polling, worker availability, GitHub backoff, issue closure, and other admission conditions can delay or prevent execution. No provider source establishes that an added minute guarantees availability under all quota windows or shared-account usage.

[V] Codex app-server explicitly specifies Unix seconds. Neither fetched exec documentation nor Claude's CLI reference establishes a stable human-readable reset-message grammar or timezone contract. The Claude upstream report provides numeric examples but not a formal unit specification. Avoid treating relative wording or local clock strings as verified timestamps without provider-specific evidence. [Codex app-server reference](https://learn.chatgpt.com/docs/app-server); [Anthropic SDK issue #599](https://github.com/anthropics/claude-agent-sdk-python/issues/599).

Source scope: web documentation was fetched during this run on September 7, 2026. Rolling documentation is not tied to the installed binaries; no deployed CLI version was established. The Claude event evidence is specifically v2.1.49 / Python SDK v0.1.39. A promising Claude TypeScript reference could not be fetched successfully and was not promoted from search snippets into verified evidence.

## Constraints & Risks

1. **Scheduling authority and feedback loops.** A normal bot-authored command is rejected. Broadly accepting bot comments would weaken existing admission controls. Recommendation: a narrow, authenticated internal scheduling operation should be authoritative, with the visible `/schedule` comment reflecting the persisted action. If literal comment ingestion is a hard product requirement, Plan must explicitly design its authentication and replay boundary instead.
2. **False classification.** Allowed quota notifications, transient throttles, billing errors, and untrusted prose can look like exhaustion. Auto continuation requires a genuine blocked-run signal and a trustworthy reset time. Otherwise the safe research assumption is to report the interruption without guessing a schedule.
3. **Lost continuation context.** Failed first-run Codex token persistence and its pre-turn failure fallback currently conflict with delayed resumption. General resume support alone does not resolve these paths.
4. **Restart and publication races.** Durable quota evidence and continuation intent are absent. A crash between run persistence, report publication, and schedule insertion can lose work or duplicate continuations unless their identities and recovery rules are defined. Existing source-comment deduplication does not directly cover issue-body trigger zero.
5. **Changed intent while waiting.** Current dispatch rereads labels and does not pin interrupted mode/agent/segment. Intervening owner work, agent switching, or a fresh conversation may make an old continuation inappropriate. Automatic continuation should not silently substitute a different agent or obsolete task intent; the exact behavior remains a Plan decision.
6. **Scheduled dependencies.** Quota interruption currently becomes ordinary terminal success/failure according to existing schedule handling. Exit-code-only success and immediate descendant blocking can both be wrong for temporary quota pauses. Define whether dependencies wait for the eventual continuation or fail at the interruption.
7. **Repeated exhaustion and shared quotas.** Reset plus one minute can still fail; multiple jobs can become eligible together and consume a shared quota. The desired retry/cancellation policy is unresolved. Do not silently add a global provider-quota coordinator or assume unlimited retries.
8. **Partial work and reporting.** Current failure harvest can commit/push partial work. A new paused/interrupted status must preserve a clear account of saved work, the planned retry, and actual versus pending publication without changing that behavior accidentally.
9. **Disabled behavior.** Default false is explicit owner intent. A missing or false setting must preserve ordinary handling and must not create automatic schedules. What disabling the setting should do to already-persisted automatic continuations remains unresolved.

## Open Questions

These are handoff questions, not requests that pause this headless run.

| Question | Who resolves it / evidence needed |
| --- | --- |
| Which Claude and Codex binary versions are supported, and what exact blocked-run/reset fields do they emit? | Engineering in Plan or a focused provider investigation; version-pinned definitions and sanitized exhausted-quota fixtures. |
| Can Codex exec provide a trustworthy reset time without changing interfaces? | Engineering; verify actual exec protocol. App-server documentation is insufficient. |
| Which Claude statuses and quota windows represent actual exhaustion, with what timestamp units and overage rules? | Engineering; provider contracts or reproducible version-specific evidence. |
| Is the visible `/schedule` comment an audit record of an internal action, or must it itself be consumed as an event? | Product owner/Plan; recommended assumption is internal authority plus visible record. |
| What should happen when reset evidence is absent, ambiguous, stale, or disagrees across windows? | Product/engineering; recommended assumption is report only when no trustworthy future instant exists. |
| Does continuation pin the interrupted agent, mode, and segment, and how does newer owner activity invalidate it? | Product/architecture in Plan; preserve continuation intent rather than assume current generic schedule semantics suffice. |
| How are quota evidence, continuation identity, and comment delivery recovered and deduplicated, including issue-body and already-scheduled runs? | Architecture; durable lifecycle and crash-boundary decisions. |
| Does a paused prerequisite hold descendants until the resumed work finishes? | Product/architecture; current success/failure propagation is insufficiently expressive. |
| What bounds/cancellation rules apply to repeated automatic attempts, changed configuration, and closed issues? | Product/architecture; avoid inventing a retry policy during Research. |
| Is staged provider support acceptable if only one adapter can reliably establish reset evidence? | Product/Plan; unsupported cases should remain visible manual recovery, not speculative retries. |

## Recommended scope for Plan mode

Focus Plan on an opt-in daemon capability named `auto_resume_on_session_limit`, default false, that uses verified adapter evidence to arrange continuation no earlier than reset plus one minute and explains the action visibly on the issue. Reuse durable scheduling, preserve self-comment protections, and make session recovery and restart safety explicit parts of the scope.

Resolve supported-provider evidence first. Then cover the configuration/default/template contract, normalized interruption evidence, durable continuation identity and lifecycle, session-handle preservation, correct quota-versus-invalid-session fallback, reporting/replay, and scheduled dependency semantics. This is a research scope recommendation, not an implementation sequence or task breakdown.

Validation should include authentic provider fixtures, allowed-versus-blocked events, unknown reset data, first-run/resumed Codex interruption, setting absent/false/true, issue-body triggers, duplicate/replayed reports, restart recovery, label/session changes, scheduled prerequisites, and deterministic eligible-time behavior. Reuse the existing adapter, turn, daemon, and SQLite test surfaces; this research run did not execute tests or modify production code.

Exclude guessed natural-language reset parsing, broad acceptance of factory comments as owner commands, generic retries for all failures, and an unrequested replacement of the existing command-line runner interface. If a provider requires a new interface to supply evidence, surface that as a separate scope decision.

Assumptions carried forward: daemon-wide configuration; usage quota rather than generic failure; sixty seconds added to a verified provider reset instant; a visible command reflecting a trusted internal schedule; no speculative automatic retry when evidence is insufficient. Remaining policy choices are recorded above rather than answered on the owner's behalf.
