# Runtime branch reconciliation and final-output evidence

Scope basis: the frozen AC-005 source-family mapping is authoritative. The original classified Research f19–f43 sub-branch inventory is not present in the current bundle or in issue #1067 and its 29 published comments (read through the GitHub connector). The supplemental source arms below therefore reconcile the approved named scope without asserting exact equivalence to an unavailable original table. No archived local sessions were read. No deferral is assumed approved.

Evidence terminology: **assembled** means a test obtains a final rendered body, saved conversation reply, permitted diagnostic or delivered message. **projection** means an input fragment only, and does not independently prove final-output compliance. **source review** records the current branch and a concrete disposition without claiming an executed final-output capture. The parent records final package-run results separately.

## Checks actually executed by the evidence worker

- `go test ./internal/turn -run '^TestRuntimeStopGuidanceEndToEnd$' -count=1 -timeout=5m`: passed. Existing executor, SQLite, reporter and fake forge produce the complete published disabled-recovery report and replay it without another invocation, action or comment.
- `go test ./internal/turn -run '^TestRuntime(Recovery|Stopped)' -count=1 -timeout=5m`: passed after correcting the new test's missing-cause expectation. Complete report assembly covers the four absent-continuation causes, missing cause, unknown eligibility and settled success; real cancelled database read returns an error without claiming absence; final stopped projection rendering names the affected run ordinal.
- `go test ./internal/messaging -run 'Test(NaturalExecution|ResearchCommand|QuestionOutcome|DiagnosisSummary|ConversationDetails|Notice)' -count=1 -timeout=5m`: passed after replacing an obsolete exact runtime-refusal expectation with semantic identity/effect assertions.
- `go test ./internal/messaging -run 'Test(Conversation|Inbox|Natural|Research|Question)' -count=1 -timeout=10m`: passed (62.179 seconds). Two subsequent text refinements made the unprotectable-output fallback static and supplied the affected Research issue URL; the parent's complete run must cover the final snapshot.

The common worker separately reports a passed focused report/Doctor/CLI run matching `Test(RuntimeRefusalInspectionRoutes|CreationUncertainOutcome|Reporter_Creation|ReplyConfirm|Doctor|RepairPresentationUsesRetainedDisposition|TypedRepairOutcomePresentation|RetainedTypedRepairOutcome|VerifiedTypedRepair)`. Those executed names are referenced below; unexecuted existing tests are explicitly labelled pending the parent's complete run.

## f19 common refusals — AC-001/005, T-001

| Source arm in `internal/report/reply.go` | Deficiency/disposition | Final evidence and limit |
|---|---|---|
| UnknownCommand, EmptySpike | Existing supported command grammar or required instruction; unchanged | Existing assembled `TestReplies`, `TestReplyBodiesTellTheReaderHowToReply`; pending complete report run |
| LegacyToolLabel, AmbiguousAgentLabels | Existing remove unsupported label or retain exactly one canonical tool; unchanged | Existing assembled legacy-refusal test and agent-label coverage; source review confirms no launch and new issue comment destination |
| MalformedAgentLabel, MalformedModeLabel | Canonical label and destination were incomplete; changed to exact supported labels and issue-comment retry | Source review; no separate final-output capture yet |
| AmbiguousModeLabels, ConflictingModeSelection, UnknownAgent, AgentSwitchRefused | Existing selected tool/mode conflict, current label, recovery identity and fresh-comment consequences remain | Source review; actual semantic guidance is not inferred from predicate-only tests |
| MissingTeam | Bare missing installation became operator `litefactory team link <name> <version>` from registered clone before another issue comment | Source review; installed name/version remain explicitly required, not invented |
| UnsupportedWorkflow | Operator inspection uses existing `litefactory team inspect <name> <version>` before choosing supported workflow | Source review |
| MissingModeMapping | Existing Linear mode-label alternatives retained | `TestMissingModeMappingReply` assembles output; pending complete report run; connection editing requires the configured operator |
| MissingTicketPublisher | Identifies retained connection, blocked Implement, original publisher restoration and fresh Implement request after restoration | Source review; no publisher-account or permission change |
| BugUnavailable | Retained Bug admission, absent launch and operator restoration of existing bindings now explicit | Source review |
| BugModeSwitch | Retains Bug admission/workflow/progress; source branch originally lacked an immediate next step | Reported to parent as a demonstrated gap; do not infer closure from other refusal tests |
| DoctorUnsupported, GraphResumeRefused | Existing diagnosis is unavailable or launch evidence missing; one affected-task host logs inspection replaces unhelpful diagnosis/retry advice | **Assembled passed** `TestRuntimeRefusalInspectionRoutes`: unique task-specific command, host, marker, no truncation |
| CodeControlRefused | Existing no record/push/merge/close claim plus permitted owner/new issue comment now explicit | `TestRenderReplyCodeControlRefused` assembles output; pending complete report run |
| RefusesSync, RefusesSyncTooFarBehind, SuggestsResolution, AppliesResolution | Existing conflicts/budget/cap, supported resolution and exact guarded confirmation boundary; preserve same-run limits | Existing assembled sync/conflict/confirmation tests; pending complete report run. No budget reset behavior changed |

StoryLocked, StoryPlanOnly, StoryPlanning, GraphReplyBetweenQuestions, GraphOwnerWaitRefused, BugCommentNotApplied, ImplementCommentNotApplied, ValidateCommentNotApplied and Implement-finished decision handoffs remain story-003's responsibility. They are not claimed fixed here.

## f20 scheduling and creation — AC-003/005, T-001/002

| Source/branch | Deficiency/disposition | Final evidence and limit |
|---|---|---|
| ScheduleInvalid | Exact existing delay/date/prerequisite grammar plus fresh issue-comment destination and nothing scheduled | Source review |
| ScheduleBlocked | Names failed prerequisite and states waiting will not resume blocked item; exact affected-task inspection | **Assembled passed** `TestRuntimeRefusalInspectionRoutes` |
| ScheduleProposalReady | Existing exact accept/reject commands and arming/discard consequences retained | Source review: real alternatives, no new approval |
| ScheduleProposalFailed | Bare failure now states no scheduling and exact fresh proposal request | Source review |
| ScheduleConfirmed, ScheduleProposalAccepted, ScheduleProposalRejected | Successful scheduling/rejection progress, no unresolved stop; unchanged | Not deficient stop branches; existing IDs/status retained |
| ConfirmInvalid, ConfirmNotOwner, ConfirmUnknownProposal, ConfirmExpiredProposal, ConfirmSupersededProposal | Existing exact grammar/current identity/expiry/binding constraints; changed owner/destination/current-proposal advice only where missing | **Assembled passed** `TestReplyConfirm*` focused run |
| ConfirmAlreadyApplied | Known already-applied no-op, no new effect; unchanged | **Assembled passed** existing already-applied confirmation test |
| ConfirmFailed | Cleanup/push effects cannot be called restored; one task-specific operator inspection before another confirmation | **Assembled passed** `TestReplyConfirmFailedDisclosesOutcomeWithoutClaimingRestoration` |
| CreationProposal: linked/attach × legacy/graph/unavailable/unknown/unrecognized publication | Exact reviewed bytes/hash/relationship remain; available ordinary-comment route distinguished from unavailable/unknown publication | **Assembled passed** `TestReporter_CreationProposalGuidance`, `TestReporter_CreationProposalPreservesCompleteApproval`; no truncation or mutated proposal |
| CreationAccepted historical admission: linked/attach × publication states | Admission identity/time does not imply publication, remote creation or current mutable proposal authority | **Assembled passed** `TestReporter_CreationAcceptanceHistoricalGuidance`; replay identical, mutable inputs excluded |
| CreationAccepted partial/uncertain issues and relationships | Confirmed issue creation, uncertain creation and uncertain attachment kept separate; inspect existing issues/repository before proposing only confirmed missing work | **Assembled passed** `TestCreationUncertainOutcomeRequiresInspectionBeforeReplacement` |
| CreationRejected syntax/payload/version/fields/mode/disabled/proposal/controls | Existing bounded closed-category guidance requires fresh corrected comment; starts no work/grants no authority | **Assembled passed** `TestReporter_CreationRejectionSafeGuidance` |
| Invalid/redacted/truncated proposal/admission/rejection metadata | Fail closed rather than publish incomplete confirmation | **Assembled passed** corresponding `TestReporter_Creation*` invalid/unsafe output tests |

## f18/f21/f30/f31/f32 ordinary execution — AC-001/002/003/005, T-001/002

| Branch/source | Deficiency/disposition | Final evidence and limit |
|---|---|---|
| nil/no-result run | Credential speculation removed; outcome/cause unknown, precise affected-run inspection | Existing full-body `TestEveryOutcomeRendersDistinguishably`, sparse/missing-result report tests; pending complete report run |
| interrupted run | Retained incomplete work and recovery status must survive assembly | **Assembled passed** `TestRuntimeStopGuidanceEndToEnd`; retained commit, final forge body, replay counts |
| failed exit/signal/rejected result, timed out, abandoned/shutdown/unrecognized | Retained reason and work preserved; ordinary stopped inspection no longer generic | Existing full-body outcome/rejection/sparse tests; pending complete report run |
| before-agent, after-success, secondary failure and partial-feature overlays | Preserve process outcome versus surrounding failure and retained work | Existing full-body `TestAFailureAroundTheRunIsItsOwnReport`, `TestNonSuccessOutcomeWinsOverPartialTally`, failure/worktree tests; pending complete report run |
| graph/dispatch retained terminal run | Correlated attempt/run supplies exact session ordinal, not database ID | **Assembled passed** `TestRuntimeStoppedProjectionAssembledRunIdentity`; **projection** `TestRecoveryProjectionUsesAffectedRunOrdinal` |
| graph pending/running work, absent launch/liveness, completed/nonrunning/cancelled | Projection distinguishes wait, retained result and genuinely unknown evidence; no admission change | Source review and existing graph-stop recovery tests; complete final-output evidence for every status arm not individually captured by the new test |
| absent continuation: disabled/unknown reset/missing identity/exhausted attempts/missing reason | Explicit nothing scheduled, retained cause or named missing cause; one affected-run logs step; no second manual advice | **Assembled passed** `TestRuntimeRecoveryGuidanceAssembledStates` |
| existing continuation missing eligibility; settled succeeded | Known action/status, unknown due time or settled result and one exact run inspection | **Assembled passed** `TestRuntimeRecoveryGuidanceAssembledStates` |
| pending/reserved due time; running; failed/cancelled | Persisted action disposition; scheduled/running says wait, settled failure inspection; recovery policy unchanged | **Projection** `TestRecoveryUsesCurrentActionDisposition`; existing durable interruption replay test captures scheduled/disabled/missing-reset final reports; pending complete turn run |
| failed retained-state read versus confirmed absence | Read error stays error and cannot assert no continuation | Passed `TestRuntimeRecoveryReadFailureDoesNotClaimAbsence` uses cancelled real DB read; not an emitted error-report capture |
| cancellation and one-time retained input | Cancellation action/run identity and exact task log command; input names task/owner destination | Source review of daemon cancellation and turn owner-input reporting; existing cancellation/input tests pending complete parent runs |
| lost report response with retained result | Known succeeded execution remains separate from uncertain delivery; automatic reconciliation says no owner action | `TestReportRecoveryLostResponseNamesRetainedExecution` captures actual permitted diagnostic; parent must record its run outcome |
| generic f32 unavailable publication | Existing transport diagnostics only; never new comment through failed/forbidden channel | Same lost-response diagnostic, delivery journal/replay tests; workflow f32 content excluded |

## f37–f38 Doctor/CLI/owner stop — AC-004/005, T-002

- `TestUnavailableDoctorApplyReportsRetainedRepair` captures the complete published issue response for absent, active and settled retained repairs, checks this command performed no repair, exact logs command once, retained identity/status and no repair-state mutation. Parent records execution outcome.
- **Assembled passed** `TestRepairPresentationUsesRetainedDisposition`: reserved/applying/uncertain restart says wait/no owner action; completed/failed/needs-review/unknown state gets one task-specific local inspection. No fresh-diagnosis loop or competing Doctor command.
- **Assembled passed** typed repair outcome tests: completed/recovered, failed/stopped/needs-review and pending restart retain step verification, backup and unresolved facts; no unavailable-apply attribution to an old repair.
- **Assembled passed** `TestDoctorLocalPresentationUsesCurrentCapabilityAndSettlement`: available proposal gives supported apply command; unavailable, active and settled states use their distinct actual capabilities and retained repair.
- Existing owner-stop controls/runner-termination, repair timeout and asynchronous settlement tests exercise no false completion. Their final parent run is required; source review is not a substitute for full issue/CLI diagnosis-failure, repair/rollback/restart/owner-stop captures.

## f39–f41 factory conversation/navigation wrappers — AC-003/004/005, T-002

| Source/branch | Disposition | Executed assembled evidence or limit |
|---|---|---|
| conversation unavailable/native identity/isolation/authorization pause | Retained conversation and cause, no replacement/replay, one operator logs route | `TestConversationPausedGuidanceUsesRetainedIdentityAndOperatorLogs` checks saved final reply and health output; parent complete run required |
| provider fail/timeout/envelope rejection/rate budget | Existing typed known cause and supported retry/reset preserved; generic interruption names no saved answer/no input replay | Passed `TestConversationFailureDiagnosticsAndRetry`, access-boundary/correction tests; typed-cause unit output alone not assembled proof |
| permanent access/forbidden channel | Suppression remains mandatory, no replacement publication | Passed conversation access/cancellation/paused tests; internal errors excluded rather than forced into an owner reply |
| details absent/denied/expired/no tools | Current authorized question/narrower task request replaces bare notice; no unauthorized export | Passed `TestConversationDetailsEmptyAndExpiredNeverSubstitute` and access-recheck tests inspect final saved reply; partition/delivery test proves unchanged saved bytes and export completeness |
| natural action executing/unknown/finished without outcome | Uncertain effect retained; one read-only status request naming admitted issue/project; no replay | Passed `TestNaturalExecutionInterruptedEffectNeverReplays`, changed-invocation and retained-outcome tests inspect saved guarded outcome |
| proposal too large; expired/already-used; changed project/protection/current question | Existing no-authorization/no-effect and fresh shorter proposal advice retained | Passed conversation proposal/factory flow tests; `natural_proposal.go` complete fixed proposal sentences source-reviewed without rewriting compliant effects |
| draft failure/absent confirmer/uncertain confirmation | Known no-created-issue versus uncertain creation retained; one operator host inspection before repeat | Passed factory conversation domain-outcome tests exercise preserved uncertainty; new exceptional wrapper arms have source review, not a dedicated final capture |
| generic handler interruption/unprotectable outcome | One status inspection, static fallback when protection fails; no dynamic unprotected text added | Passed factory narration/decoded-output protection tests; exceptional newly changed fallback wording not separately captured |
| Research project/task/question lookup, diagnosis unavailable/queue failure, unconfirmed/unrecorded answers | Existing issue URL or exact task log command/operator route; uncertain answer never becomes no effect | Passed `TestResearchCommandResolutionAndPrivateInput`, `TestQuestionOutcomeStatus`; no refused port execution, private text omitted, affected URL and unconfirmed token retained |
| standards invalid/unavailable/read failure | Existing discovery or operator availability inspection; saved incomplete evidence remains incomplete | Source review; existing standards conversation tests pending complete parent run, no claim provider narration necessarily copies wrapper |
| inbox invalid/expired/unavailable/stale reference, access changed | One current authorized inbox request or operator access restoration; unavailable not empty | Existing inbox saved-answer journeys, expired delivery, access and partial-access tests inspect delivered/saved output; these tests await the parent complete run because their names do not match the evidence worker’s focused expression. Exact new advice is not individually asserted per arm |
| organizer unavailable/budget/invalid output | Existing full factual baseline fallback and full navigation retained; unchanged | Passed `TestInboxOrganizerFailureFinalizesCompleteFallbackOnce`; existing organized saved-answer journey awaits parent complete run |
| organizer missing/revoked ticket/request/malformed reference | One fresh authorized inbox capture; revoked saved slots never select another decision | Passed `TestInboxOrganizedRequestDoesNotRetargetRevokedPosition`; existing partial-access and organized journey tests await parent complete run |

## f42 authorized transport/shortening/input — AC-003/005, T-002/003

- Failed/stopped short notice adds immediate action: open the linked retained issue report before more work. Passed notice mapping/title/budget/collection tests inspect final short message; parked/Plan-specific owner choices remain story-003.
- Conversation answer multipart/details use immutable permitted saved results. Passed details partition/retry/access tests verify export manifests and unchanged bytes; source access is rechecked before delivery.
- Common run shortening now points to affected saved event log using the exact `litefactory logs '<full task>' --run <ordinal> --raw` route; unavailable log identity is explicitly named instead of calling diagnostics complete. Existing oversize/truncation tests only prove limits and notices; following every omitted recovery command/consequence in the final saved event requires the parent's dedicated shortening evidence.
- Automatic report reconciliation uses retained execution and delivery identities and emits no-owner-action wait only on existing diagnostics. No exactly-once, recovery deadline, empty-lookup proof of non-delivery or new notification channel is asserted.

## Exclusions and regression boundaries

f10/f17 restart semantics remain regression constraints. Same-graph continuation keeps limits; supported coordinator restart can obtain fresh existing limits. f25–f29 Validate decisions/effects/repair, workflow f32–f33, f42 parked summaries/Plan child holds and the workflow-specific f19 arms listed above belong to story-003. f34–f35 and agent-authored wording belong to story-001. Internal/N/forbidden-channel branches remain excluded for their visibility/authorization reason. No agent wording pins, schema, delivery-body mutation, repair authority, retry policy or extra invocation was added.

## Outstanding engineering evidence (not an owner-approved deferral)

The table intentionally distinguishes final output assertions from source-only arms. It does not claim every source arm has an independent automated test, nor infer a complete saved destination from a short link alone. Parent must incorporate final complete package outcomes, settle the reported BugModeSwitch guidance gap, review complete shortened saved-log reachability and decide whether any remaining known family final-output evidence is still missing. This report records engineering only; Validate decides acceptance.

## Parent-confirmed complete checks and resource audit update

The parent reports the complete messaging package with race detection passed (240.938 seconds), covering the existing owner-inbox journeys/access tests previously marked pending above. Complete agent-result, meterview, Discord messaging and main-command packages also passed with race detection. The second complete report package race run passed (6.154 seconds). The real CLI oversized-report test followed the offered raw-log route and passed. The first complete Doctor race run passed (53.651 seconds) and CLI run without race detection passed (32.152 seconds); both require another complete run after a subsequent failed-repair presentation flag change. The daemon complete run exposed obsolete expected output and a real stranded-repair waiting issue; the parent fixed both, and its final daemon/turn outcomes were still pending when this update was written. These statements report executed engineering checks, not acceptance.

The retained GitHub response was re-audited without another network fetch: all 29 comment bodies, structured fields and HTML metadata contain no classified f19–f43 source table, Research result/snapshot identifier or downloadable artifact link. Published links point only to the separate issue #1128 and the three planned child issues. Delivery hashes and an implementation handoff marker do not provide an inventory retrieval route. The public Research summary says the inventory is retained but does not expose it. Exact reconciliation against that original inventory remains inaccessible from the supplied mapping and published response; no archived session was read to bypass this boundary.

## Final engineering review

Three subagents contributed independent runtime, common/Doctor, and end-to-end/evidence work. The parent read their changes and completed the delivery diagnostics, retained-input action and saved-log destination checks. No commit, stage, push, merge, release or production test was performed.

- `ReplyBugModeSwitch` is a Bug-workflow-specific unapplied mode command, retained under the frozen story-003 boundary for Bug/Implement/Validate unapplied replies. It was left unchanged; this story does not claim another story's fix.
- `TestRuntimeShorteningRetainsRecoveryDestination` uses the existing real CLI logs fixture, puts an omitted recovery instruction and its duplicate-effect consequence beyond the rendered result limit, follows the exact offered full-task/run-ordinal `--raw` route, and verifies the complete saved bytes and omitted instruction/consequence. It passed; the complete CLI package subsequently passed without race detection.
- The complete messaging race run passed, including the inbox, organizer, authorization, immutable details, proposal and uncertain-effect journeys previously marked awaiting parent execution. This does not turn source-only review into an independent test per arm or claim that agent narration always reproduces supplied wrapper facts.
- A real failed-repair presentation gap found during complete testing was corrected: a retained intermediate phase after a command error no longer proves automatic progress. A transient presentation flag selects inspection while retaining the stored phase, journal and repair approvals. The complete assembled semantic test checks this distinction.
- The historical fixed `/doctor check` suffix is removed from displayed repair failures only, preserving stored failure bytes. Its displayed complete report retains one affected-task inspection command, while unknown outcome and duplicate-effect warnings remain.
- Session recovery removes only known factory manual-advice suffixes; arbitrary semicolon-separated cause details remain visible. It retains a launch refusal's diagnostic after the old manual-advice phrase instead of discarding it.
- No new agent-directed instruction literals or shipped-agent/skill wording pins were introduced. The touched production presentation paths supply owner-facing state and existing commands.

All complete runs use finite timeouts and logs. The first report/daemon/turn runs exposed stale output expectations caused by this change; those expectations were updated to inspect supported commands and retained status, not weakened. No pre-existing failure has been identified. The latest passing package outputs and final turn result are recorded in the companion check record when available. Build, scoped pinned lint and formatting checks also passed. Repository-wide tests and the repository full check were not run.

The story remains blocked on access to the original classified Research inventory required for exhaustive AC-005 reconciliation. The supplied branch-family mapping is not claimed to be that missing source. No owner deferral, acceptance or engineering completion is inferred from this report.

## Final check outcomes

All latest complete scoped package runs passed; see CHECKS.md for actual output. The final turn run passed in 496.351 seconds, daemon in 117.847 seconds, CLI in 32.321 seconds, Doctor with race detection in 50.585 seconds, report with race detection in 11.242 seconds, and messaging with race detection in 234.566 seconds. Discord messaging and the other recorded caller packages also passed. Final build, pinned scoped lint, formatting and diff checks passed.

The final generic interrupted-conversation fallback now explicitly asks for the previous request status in the retained conversation; its input remains unreplayed. Its focused semantic check and the subsequent full messaging race run passed.

Required original-inventory reconciliation remains blocked on the unavailable retained source. No completion, acceptance, or owner-approved deferral is claimed.
