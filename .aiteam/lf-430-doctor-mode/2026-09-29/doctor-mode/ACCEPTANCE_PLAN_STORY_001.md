# Proposed acceptance plan: story-001

Story-001 will let the owner diagnose one named issue from the CLI, retain the conversation across interruptions and review a scoped repair proposal without changing workflow state. I propose 12 observable criteria and 10 linked tasks. Each criterion names future proof; none is claimed as met. The settled intent and architecture are retained. No owner decision blocks this acceptance proposal.

Current parent index: [story-001](https://github.com/marioatcloudvane/litefactory/issues/432), [story-002](https://github.com/marioatcloudvane/litefactory/issues/433), [story-003](https://github.com/marioatcloudvane/litefactory/issues/434). Story content and its criteria/tasks remain on the respective sub-issue.

The exact active set is story-001, story-002 and story-003, with no retired entries. Dependencies remain: story-001 has none; story-002 requires story-001; story-003 requires story-001 and story-002. The promoted set is unchanged. The current shared decisions remain unchanged: one issue only; one shared engine; doctor is a control command rather than a mode; read-only diagnosis; explicit approval before agent repair; four context layers and saved conversations; flagship selection each turn with no downgrade; owner-only, strictly prefixed GitHub messages with priority over blocked workflow; retained backups; one restart after verified repair. Successful-repair file-copy cleanup belongs to #413 and is not a delivery dependency.

This assembled review records the supplied current state: story-001 is at acceptance after its story and technical reviews; story-002 remains at story review; story-003 remains at technical review. Their later criteria/tasks and owner reviews remain outstanding. The complete set cannot yet be handed to implementation. The factory binds this review to its exact current story, dependency and shared-decision revisions; no new approval stage is proposed.

Retained assumptions include protected local-state access as CLI owner authority, persisted provider selection with installation-default fallback, a usable compatible database, and no automatic repair rollback. The real stuck-case field-guide review must precede implementation, and each provider needs authentic isolation evidence before enablement. Those observations have not been performed here. Tasks distinguish ordinary Go tests from external host observations; no integration build tag is proposed. Input acknowledgment will not mean owner acceptance, publication or implementation completion.

## Review binding snapshot

Factory-supplied state only; this record grants no approval. Shared decisions: initial. Promoted set: 6cdffa1031da4116796b9039d5d1583f23364d58745afd86ddd08ba8cf4d226b.

- story-001: candidate plan-137-1296; acceptance; expected revision c1b3bc4f7d9b01f5f725b3ded923e90176340ea4a26550de0361a3be5436aabc; no prerequisites.
- story-002: candidate plan-136-1280; story; revision 055d3b5e1a1e0259b4988883eba6d83cd47155aa9aebccec339ae574ff0a24a7; requires story-001.
- story-003: candidate plan-138-1305; technical; revision eee4824bb8ba7b3b497238d78984546e9788f871bdc34688f757edb3d3f8b48e; requires story-001 and story-002.

No retired entries. Only story-001 criteria and tasks change. The factory supplies the new candidate and exact assembled-review binding.

## Retained story

As factory owner, I want to diagnose one explicitly named issue through the CLI with an agent and resume that conversation, even when the daemon is down, so that I can understand why the issue is stuck and review a specific repair proposal without manually piecing together its history or changing its state.

When an issue stops progressing, the owner has to connect scattered status records, comments, files and logs to understand what failed. A stuck workflow or stopped daemon can also prevent normal interaction. The owner needs an independent way to get an explanation and a proposed fix, while keeping the conversation across interruptions.

### In scope

- Start, resume and explicitly stop a doctor conversation from the CLI for one named issue, including closed issues and use while the daemon is down or the issue's workflow is stuck.
- Provide the shared doctor diagnosis and saved conversation capability for the later GitHub and repair stories.
- Use an installed agent with the reviewed fixed system prompt, a separate short factory field guide, fresh evidence for the selected issue and saved conversation memory.
- Cover the issue's session and runs, comments and processing position, schedules, graph and story progress, report deliveries, worktree and session files, and recent relevant logs. Remove secrets before giving evidence to the agent.
- Keep diagnosis read-only and restricted to the selected issue. Treat comments, logs and file contents as evidence, not authority to instruct the agent.
- Explain findings in plain language with supporting evidence and uncertainty. Propose specific row or file changes with reasons, reversibility and rollback information; report missing or unreliable evidence without inventing a diagnosis.
- Preserve issue identity, conversation, findings, the current proposal and previous attempts across pauses, failures and restarts; continue from saved findings with fresh evidence.
- Use the selected provider's flagship model for every doctor agent invocation, independently of ordinary workflow model settings; make this requirement reusable by later repair work. Report unavailability and retain the conversation without downgrade.
- Review the factory field guide against a real stuck case so that its explanation of factory behavior is grounded in observed evidence.

### Not in scope

- Receiving or posting doctor conversations through GitHub, GitHub owner authentication, command-prefix handling or priority delivery of GitHub commands.
- Applying repairs, changing the issue's database rows or files, stopping conflicting activity, creating repair backups, rolling back repairs or restarting the factory.
- Cleanup of repair backup copies, which belongs to the separate cleanup work.
- Inspecting other issues or repositories, or scanning the whole factory.
- Runtime edits to litefactory source code, migrations, configuration or workflow rules; deleting history; or changing remote issues, labels or branches as repairs.
- Adding a workflow mode or starting workflow stages from doctor.
- Silently falling back to a weaker model.

### Retained notes

- Retained assumption: saved doctor conversations live in the factory database and are available to the later GitHub entry point.
- Retained assumption: resolve the selected provider's configured flagship tier on every doctor turn; if unavailable, report the failure and preserve memory without using a weaker model.
- Evidence limitation: earlier incidents were studied through saved reports; live daemon logs and the live database have not been inspected. The factory field guide needs review against a real stuck case before implementation.
- Read-only diagnosis permits saving the doctor's own conversation and findings; it does not permit repairing the issue's workflow state or files.

## Proposed criteria

### AC-001

The local owner can start or resume diagnosis with an explicit repository and positive issue number, including a closed issue, an issue without a workflow session, a blocked workflow and a stopped daemon. Invalid targets or an unusable database/schema produce a clear error without creating or dispatching workflow work.

Future proof: Run future CLI scenarios for each target and daemon condition using an isolated installation and controlled provider. Capture output, provider launch and workflow-dispatch calls; compare workflow records before and after. Exercise missing target, nonpositive number and incompatible schema and show the operator-facing error.

### AC-002

Conversation identity, owner messages, findings, immutable proposal revisions and attempts survive CLI exits, failures and restarts. Resuming uses saved findings plus fresh evidence. Stop retains history, revokes pending result authority and permits a later start only as a new conversation generation.

Future proof: Use a temporary database across separate CLI processes: diagnose, terminate, reopen, resume, stop and start again. Compare saved history and proposal references, inspect the next prompt and submit a delayed old-generation result to show refusal without overwriting history.

### AC-003

Each turn receives the fixed doctor prompt, a separately versioned field guide, fresh issue evidence and saved memory. Evidence covers sessions/runs, comments/watermark, schedules, graph/story progress, report delivery/recovery, worktree status, session-file inventory, relevant logs and daemon liveness, with source identity and capture time. Missing, changed or truncated sources are explicit; default windows are the latest 30 comments and 200 issue log lines, with bounded excerpts, rows, files and total bytes.

Future proof: Capture future prompts and evidence snapshots from populated, absent-source, oversized and changing-source fixtures. Check every evidence category, provenance, versions, configured byte limits and omission markers. Resume after changing a fixture and show fresh evidence while the retained proposal and relevant findings remain available.

### AC-004

Only evidence proven to belong to the selected issue reaches the agent or output. Shared parent records expose only its projection; logs use canonical task/run identity. Unsafe file paths and file types are refused. Credentials and secret files are excluded, and secrets are redacted from evidence, owner text, retained agent text and output. Unreliable core identity or failed redaction prevents launch.

Future proof: Seed distinguishable sibling-issue, other-repository and secret canaries in rows, shared blobs, similarly numbered logs, comments, files and messages. Exercise symlinks, path traversal, nonregular files and changed descriptors. Inspect collector access and all disclosure surfaces for absence of excluded content; inject identity/redaction failures and show no agent launch.

### AC-005

The diagnosis agent has only prepared context and invocation-scoped result tools. It cannot use native filesystem, shell, network/search, delegation, ambient MCP, project instructions or user skills, and runs outside the issue worktree. Evidence cannot expand its authority. Unsupported or unverified provider isolation refuses launch.

Future proof: Use deterministic adapter tests for permissions, working directory, environment and malicious evidence. Before enabling each supported provider, retain an authentic isolated external observation with binary version, exact tool surface and attempted forbidden operations against harmless canaries. Record unavailable observations as missing and keep that adapter disabled; simulated results alone cannot establish this criterion.

### AC-006

Diagnosis changes only doctor-owned history, attempts, evidence, proposals, result receipts, usage and private transport artifacts. It does not change workflow rows or issue files, create a workflow mode/run, cancel normal work, repair anything, mutate remote state or restart the daemon. Apply is visibly unavailable in this increment.

Future proof: For successful, failed and adversarial diagnosis scenarios, compare workflow-table snapshots, worktree/session file bytes, modes and git index before and after in a controlled installation. Inspect process, forge, git and dispatch call records for forbidden effects and inspect output for a truthful diagnosis-only boundary. Separate independently running workflow writes from doctor effects.

### AC-007

Every invocation, including a resumed turn or separate attempt, resolves the persisted issue provider or installation default and that provider's current flagship mapping, independently of normal workflow settings. Configured and observed models are recorded separately. Missing mapping, unavailable flagship or unsupported isolation reports failure and preserves memory without a weaker-model or other-provider retry.

Future proof: Use provider spies with a balanced normal workflow setting, then change flagship mapping between turns and inspect each launch and saved model record. Exercise absent provider selection, empty mapping, model outage and isolation refusal; show zero downgrade launches and unchanged prior findings/proposal. Correlate authentic provider observation with its configured/observed model fields.

### AC-008

Findings explain the problem in plain language with evidence references, uncertainty and named coverage gaps. Any proposed repair has numbered steps with an issue-owned target, observed precondition, intended change, reason, reversibility and rollback information. Invalid, foreign or unresolved targets cannot become the current actionable proposal; neither a proposal nor its receipt authorizes repair.

Future proof: Validate future result fixtures for complete and incomplete evidence and every required proposal field. Reject foreign targets, invented evidence handles and unresolved paths; show the prior valid proposal remains. Inspect representative rendered diagnoses for understandable findings and uncertainty, and show SQL/shell-looking content is stored only as inert proposal data.

### AC-009

Diagnosis adopts only a validated, durable result from preflight, submit and receipt, bound to the active doctor invocation. Lost submission replies are recovered by receipt lookup; a replay preserves identity and cannot duplicate a result. Terminal text cannot substitute for a result, and a display failure leaves the accepted diagnosis readable.

Future proof: Inject invalid payloads, absent receipts, lost submit replies, duplicate bytes, stale bindings and output-writer failure. Inspect lookup/retry ordering and saved result count, then read the accepted conversation from a new process. Exercise normal workflow result validation as a regression check.

### AC-010

Only one doctor turn may write an issue's conversation at a time. A competing diagnosis reports busy; stop and newer generations reject late completions. Cancellation, process death and provider/result failures retain prior findings and proposal, record a truthful failed/interrupted doctor attempt and do not recover or alter ordinary workflow runs.

Future proof: Use coordinated multi-process and race-enabled tests for competing commands, stop during launch/submission, process death and delayed completion. Reopen the database and compare history, attempt states and workflow rows. Check that doctor locks do not depend on workflow admission and that no stale writer changes the current proposal.

### AC-011

Before implementation, the field guide is reconciled with one authentic issue-scoped stuck-case capture and current source behavior. The review records capture time, source/version, guide version, supported explanations, corrections and missing evidence; historical incidents are not presented as currently unfixed defects.

Future proof: Prepare a versioned review record linking a redacted authentic capture to individual guide statements and current source references. An independent reviewer checks that the capture is real, scoped and sufficient for the stated conclusions. Keep missing evidence and historical limitations explicit; synthetic fixtures do not satisfy this review.

### AC-012

Doctor persistence and shared result/usage records belong to doctor invocations without fabricated workflow runs or duplicate ledgers. Existing workflow validation and usage export attribution remain intact. Schema changes use the normal explicit migration lifecycle; diagnosis reports incompatible schema without changing it.

Future proof: Run future migration and store tests against new and existing databases. Check exactly-one-owner constraints, conversation uniqueness, immutable history, doctor/workflow result authority and metering/export attribution, including interrupted attempts. Show diagnosis does not invoke migrations, and run make check plus the repository's race-enabled suite when the implementation is complete.

## Proposed tasks

### T-001

Reconcile the separate factory field guide with current source and one authentic, redacted stuck-case capture before implementation; retain the versioned review and unresolved evidence gaps.

Owner role: go-integration-test-engineer. Criteria: AC-011. Depends on: none.

### T-002

Deliver doctor conversation, attempt, snapshot and immutable proposal persistence, short scoped store reads, doctor locking/generations and explicit migrations; extend existing result/usage ownership without creating workflow runs.

Owner role: go-implementation-engineer. Criteria: AC-002, AC-003, AC-010, AC-012. Depends on: T-001.

### T-003

Deliver bounded issue-only evidence collectors, parent projections, safe file and git inspection, provenance/coverage markers and redaction at every disclosure boundary.

Owner role: go-implementation-engineer. Criteria: AC-003, AC-004, AC-006. Depends on: T-002.

### T-004

Extract reusable result-service mechanics while retaining workflow checks; add the typed diagnosis/proposal validator, invocation binding, preflight/submit/receipt recovery and doctor metering attribution.

Owner role: go-implementation-engineer. Criteria: AC-008, AC-009, AC-012. Depends on: T-002.

### T-005

Deliver the mode-free provider invocation contract, per-turn flagship resolution, private scratch context, fixed prompt plus reviewed field guide, restricted tools and fail-closed provider enablement.

Owner role: go-implementation-engineer. Criteria: AC-003, AC-005, AC-006, AC-007. Depends on: T-001, T-004.

### T-006

Deliver shared Diagnose, ReadConversation and Stop operations plus explicit-target CLI commands, saved-memory reconstruction, plain-language output, busy handling, fencing and failure recovery. Expose diagnosis only.

Owner role: go-implementation-engineer. Criteria: AC-001, AC-002, AC-006, AC-008, AC-009, AC-010. Depends on: T-003, T-004, T-005.

### T-007

Deliver deterministic migration, store and result tests for history retention, one-owner constraints, concurrency/fencing, crash and reply-loss recovery, proposal validation and existing workflow/usage compatibility.

Owner role: go-unit-test-engineer. Criteria: AC-002, AC-008, AC-009, AC-010, AC-012. Depends on: T-002, T-004, T-006.

### T-008

Deliver ordinary Go tests for CLI target/status cases, bounded fresh context, cross-issue and secret canaries, unsafe files, injection resistance, flagship selection and absence of diagnosis side effects.

Owner role: go-unit-test-engineer. Criteria: AC-001, AC-003, AC-004, AC-005, AC-006, AC-007, AC-008. Depends on: T-003, T-005, T-006.

### T-009

Retain authentic external provider observations for each adapter before enabling it: binary version, configured/observed model, exact tools, working directory/environment and harmless forbidden-operation probes. Keep unsupported or unobserved isolation disabled; do not invoke unsupported nested providers.

Owner role: go-integration-test-engineer. Criteria: AC-005, AC-007. Depends on: T-005.

### T-010

Exercise the finished CLI in an isolated installation across daemon-down, wedged/closed/no-session targets, process restart, concurrent doctor commands and display failure. Retain transcripts and before/after evidence, complete make check and the race-enabled suite, and map observations or remaining gaps to every criterion. Use ordinary Go tests and explicit external observations; introduce no integration build tag or integration-test convention.

Owner role: go-integration-test-engineer. Criteria: AC-001, AC-002, AC-003, AC-004, AC-005, AC-006, AC-007, AC-008, AC-009, AC-010, AC-011, AC-012. Depends on: T-007, T-008, T-009.

## Retained architecture

### general

- area: Fit and reuse
- decision: Add an independent doctor application service called directly by the CLI and later by GitHub intake. Reuse CLI command registration, config.Paths and naming identities, store accessors, internal/git, redaction, provider process infrastructure and the shared result transport. Do not dispatch through turn.Execute, a workflow graph, mode selection or the task's admission lock.
- rationale: internal/cli/commands.go has no doctor command. internal/agent/request.go requires workflow mode, run and segment identity; ordinary execution would inherit the blockage doctor must diagnose.

- area: Guidance
- decision: Apply plan-go-guidance to CLI, service, persistence and provider changes; apply plan-agent-setup-guidance to the doctor prompt, field guide, tool ceiling and result contract.
- rationale: This story changes both application code and the agent's operating contract. It has no frontend design.

- area: Authority and read isolation
- decision: Give diagnosis only the prepared evidence, retained doctor memory and scoped communication tools. Disable native file access, shell, search/network tools, delegation, ambient MCP servers, project instructions and user skills. Use a private factory-owned scratch directory, never the issue worktree as the agent working directory. Factory collectors alone read original sources.
- rationale: A read-only filesystem prevents writes but still permits unrelated reads. internal/harness/settings.go explicitly distinguishes tool boundaries from additive permission rules; internal/codex/runner.go already disables shell and multi-agent tools for bounded review. Missing enforceable provider isolation must refuse launch.

- area: Read-only boundary
- decision: The engine may write doctor conversation, attempt, evidence, proposal, result-receipt and usage records plus its private transport artifacts. It may not update workflow sessions, runs, comments, watermarks, scheduling, graph progress or issue files. Diagnosis never cancels workflow work or restarts the daemon.
- rationale: Read-only diagnosis still needs durable memory. Separate ownership avoids disguised repairs and keeps stories 002 and 003 distinct.

- area: Provider and model
- decision: At each invocation, use the issue's persisted provider selection, or the loaded installation default if none exists, and resolve config.Tiers.ResolveModel(provider, TierFlagship). Require a nonempty mapping and supported isolated adapter. Record configured and observed model separately. Never reuse a normal workflow runner's cached balanced model or retry on a weaker provider/model.
- rationale: internal/config/tier.go already owns provider mappings. Provider/default selection is an architectural assumption; the settled flagship rule applies to every resumed turn and later repair invocation.

- area: Scope of local authority
- decision: Treat the CLI caller with access to the installation's protected local state as the local owner, following existing administrative CLI practice. Require repository plus positive issue number; do not search across repositories or require an open issue. Do not grant forge credentials to the agent.
- rationale: GitHub numeric-owner authentication belongs to story-002. An explicit local target avoids ambiguous issue numbers and works without the daemon.

- area: Evidence limits
- decision: Retain historical incident reports as versioned evidence, not current runtime truth. Ground the field guide in current store/control-flow source, then reconcile it against one authentic, issue-scoped stuck-case capture before implementation. Record capture version, time, source and missing material.
- rationale: Saved reports for #314 and #366 show command-consumption and parent-effect wedges, but this pass did not inspect live state. The historical #314 report also records subsequent fixes; a known pattern must not be presented as a current unfixed bug.

- area: Dependencies and delivery
- decision: Story-001 has no story prerequisite. It exports diagnosis, conversation and proposal contracts for stories 002 and 003. It exposes no apply capability and does not depend on cleanup #413. Assume usable local database, compatible schema and installed provider; daemon-down support does not imply database-corruption repair.
- rationale: A broken database or unavailable provider must produce a clear failure instead of silently creating another source of truth. No owner-only architecture decision remains open.

### data_model

- entity: Doctor conversation
- definition: New doctor-owned conversation record keyed by canonical repository identity and issue number, with durable conversation ID, generation, active/stopped state, revision and current proposal reference. Link to an existing workflow session when present without requiring or creating one.
- constraints: At most one active conversation per issue. Stop preserves history and clears active authority; a later start creates a new generation. Never cascade workflow cleanup into doctor history.

- entity: Doctor turn and invocation
- definition: Append-only owner messages, findings and attempts under the conversation. Record source and source request identity, sequence, provider/model, prompt/guide versions, evidence reference, start/end state, failure and result reference. A turn can have multiple separately identified attempts, never conflated retries.
- constraints: Persist input and invocation before launch. Use unique source identities for deduplication, expected conversation revisions and one live doctor writer per issue across CLI processes and the later daemon adapter. A process-held per-issue doctor lock plus durable fencing generation prevents stale completions; it is independent of workflow locks. On orphan recovery mark only doctor attempts interrupted.

- entity: Evidence snapshot
- definition: Immutable, bounded, redacted snapshot with schema version, issue identity, capture times, source keys, row/file digests where available, observations, and explicit missing/truncated/changed-during-read markers.
- constraints: Every source record is proven to belong to the target. Store reads form a short snapshot transaction; no transaction or cursor spans file/network/model/output I/O. Cross-source observations are not claimed to be globally atomic.

- entity: Repair proposal
- definition: Immutable revision under a doctor turn with finding/evidence references and numbered steps: target kind, issue-owned row key or rooted relative file path, observed precondition, intended change, reason, reversibility and rollback information. Current proposal is an explicit pointer to a validated revision.
- constraints: No SQL or shell string is executable authority. Invalid, foreign or unresolved targets cannot become actionable proposals. Preserve superseded proposals and prior attempts. Refreshing a diagnosis cannot approve or apply any proposal; later repair rechecks exact proposal revision and preconditions.

- entity: Result and usage ownership
- definition: Extend the existing result-attempt and meter-attempt ownership to represent either a normal run or a doctor invocation, with exactly one owner. Preserve the existing result revision, receipt, usage observation and pinned price-catalog models.
- constraints: internal/store/result_bindings.go currently requires a running workflow row and meter_attempts requires run_id. Add explicit doctor ownership through new embedded SQL migrations; do not fabricate workflow runs or duplicate receipt/meter ledgers. Doctor authority expires on turn completion, stop or fencing change. Preserve existing workflow validation and export attribution.

### service_design

- component: CLI adapter
- responsibility: Register doctor <repository> <issue> <instructions> and doctor stop <repository> <issue>; repeat diagnosis to resume. Render saved findings or explicit unavailable/interrupted status through cli.Env writers. Local invocation starts its own broker and engine without contacting a daemon socket.
- access: Reuse Command.New closures, context cancellation, config loading, repository identity resolution and existing precondition/domain-error conventions. No forge mutation, git mutation or workflow dispatch.

- component: internal/doctor application service
- responsibility: Own target validation, conversation lifecycle, doctor-only serialization, evidence assembly, model selection, prompt construction, invocation and durable result adoption. Expose context-first Diagnose, ReadConversation and Stop operations with typed inputs/results for both entry points.
- access: Use narrow consumer-owned interfaces for store, provider, evidence and result service. Depend on lower-level config, naming, agent, store, git and redact capabilities; neither CLI nor daemon policy belongs in this package.

- component: Evidence collectors
- responsibility: Collect session/runs, comments/watermark, schedules, graph attempts/waits/dispatches, issue-owned story progress, report delivery/recovery, worktree status, session-file inventory and relevant logs. Include daemon liveness as an observation, without assuming a PID proves a run is healthy.
- access: Read scoped store methods and internal/git only. Reuse config-owned paths and log naming, not CLI handlers. Optional fresh issue/comment reads use the factory GitHub client without persisting normal intake state. Unavailable remote reads are marked; local diagnosis may continue.

- component: internal/store
- responsibility: Add scoped evidence reads and doctor persistence/accessors while preserving the single SQLite connection pool, context-first APIs and short transactions. Own all SQL and embedded migrations.
- access: No direct sql.DB outside store. Installation/startup uses the existing explicit migration lifecycle; diagnosis never lets an agent change schema. A missing/incompatible schema reports the required operator action rather than opportunistically repairing it.

- component: Provider adapters in internal/harness and internal/codex
- responsibility: Add a doctor-specific invocation contract in internal/agent. Reuse binary resolution, safe child environment, stdin prompts, cancellation, bounded stream handling and usage collection. Supply only scoped result MCP tools and a verified diagnostic tool ceiling.
- access: Do not weaken ordinary agent.Request validation or fake a workflow mode. Use a new mode-free invocation path. Construct per-turn flagship model selection explicitly. Provider authentication remains provider-owned; factory secrets and raw evidence paths stay outside the child.

- component: Shared application-result service
- responsibility: Reuse communication preflight, submit and receipt, strict decoding, retained revisions and durable request deduplication. Add a typed doctor diagnosis payload and trusted doctor-attempt binding. Separate reusable service mechanics from the current turn-owned workflow resolver into a lower-level service so doctor need not depend on workflow execution.
- access: internal/turn/agent_results.go currently combines reusable result logic with workflow binding. Keep existing workflow façades and authority checks intact; doctor grants only its own result and receipt operations. Terminal text is never the diagnosis data source.

### solution_design

- element: CLI lifecycle
- contract: Resolve the explicit target and load history, acquire the doctor-only lock, persist the message/attempt, capture evidence, launch one bounded flagship invocation, validate and durably adopt its result, then render. A stop records stopped state and revokes pending result authority; it does not kill ordinary workflow work. A concurrent doctor command reports busy rather than interleaving messages.
- rationale: One command produces one resumable turn without waiting on the issue's workflow queue. Normal workflow may continue, so evidence and future repairs must detect drift.

- element: Four context layers
- contract: Ship a fixed doctor role and separate versioned field guide as factory-owned resources, then append escaped issue evidence and saved conversation as untrusted data. Present only the current local owner message as the turn instruction. Preserve the reviewed diagnosis/proposal/uncertainty protocol; render apply as unavailable in this increment, never tell the owner a repair or restart occurred.
- rationale: Repository code and supplied artifacts cannot replace authority. The research prompt's apply instructions describe later scope and must not imply a usable apply endpoint in story-001.

- element: Memory and bounds
- contract: Reconstruct each agent turn from durable doctor history with fresh evidence rather than depending on a provider's resumed session. Keep the current proposal and relevant findings intact; use bounded prior-turn excerpts with explicit omissions and stable references. Default evidence windows follow the reviewed pack: latest 30 comments, bounded comment excerpts and latest 200 issue log lines, plus bounded rows/files and a total byte budget. Refuse or report overflow, never silently claim complete evidence.
- rationale: Conversation survives provider failure or restart, while newly selected flagship and permissions take effect on every invocation. Durable full history and a bounded prompt serve different purposes.

- element: Issue-only collection
- contract: Join database evidence through the exact issue/session identity. Project only the selected story's fields from parent-owned aggregate records; do not traverse sibling issues or expose raw aggregate payloads. Filter logs by canonical task/run identity, not a bare number match. Refuse symlinks, path escapes, nonregular files and changing descriptors; bound bytes before allocation. Read worktree git status with optional index writes disabled and describe unpushed state against the last local remote reference, without fetching.
- rationale: Worktrees, parent Plan blobs and daemon logs can contain other-issue data even when reached from this issue. Root membership alone is not sufficient proof of scope.

- element: Redaction and provenance
- contract: Apply the existing Redactor to evidence, owner text, retained agent text and rendered output at their disclosure boundaries. Exclude credentials, secret files and unrelated content before redaction. Preserve original local logs unchanged. Bind evidence handles and proposal targets to engine-owned identity; agent text cannot choose absolute roots or permission grants.
- rationale: Existing comments and daemon logs can contain raw data. Redaction supports but does not replace source scoping, child environment isolation and tool restrictions.

- element: Failure and crash behavior
- contract: Missing evidence yields a named coverage gap; unreliable core identity or failed redaction refuses launch. Model failure, cancellation, invalid result or missing receipt retains prior findings/proposal and records a failed/interrupted doctor attempt. Successful submission is adopted before display; if display fails, the saved result remains readable. Look up a known receipt before replay, and use identical bytes only when none exists. Stop or a newer fencing generation rejects late writes.
- rationale: No failure may erase conversation, advance workflow state, turn partial evidence into certainty or cause an automatic repair. A receipt acknowledges input, not a repaired issue.

- element: Evidence still needed
- contract: Current source establishes available seams, not authentic provider isolation. Record supported binary versions and verify the exact tool surface in isolated external observations before enabling each provider. Use source-backed synthetic cases for deterministic failure behavior; do not require an unsupported nested provider invocation or claim it was observed. The referenced nested-agent evidence document is absent in this checkout, so its detailed guidance remains unavailable.
- rationale: Repository constraints EXT-2, EXT-4 and EXT-5 distinguish source review and fixtures from authentic host evidence. Real stuck-case field-guide review and provider isolation evidence remain implementation prerequisites, not claimed results from this architecture pass.

Guidance: go, agent-setup. Frontend: no.

All proof methods describe future work. No implementation, test, host observation, publication or owner acceptance is claimed.
