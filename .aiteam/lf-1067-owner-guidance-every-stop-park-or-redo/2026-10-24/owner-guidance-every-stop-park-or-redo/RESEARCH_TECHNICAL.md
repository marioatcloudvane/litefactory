# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

The requested behavior is a cross-workflow owner guidance contract: every stop, park, refusal, wait and redo should explain cause, affected work, exact permitted replies and their consequences, and ordering. The reported #728 incident is owner-supplied evidence; it was not reproduced against a live factory.

_No source reference was recorded for this finding._

## f2

Implement's parent handoff lists only parked stories, gives each a reason and generic resume guidance, and creates questions without options. It does not enumerate dependent waiting stories or provide a decision-specific reply. The story park renderer flattens the submitted result under 'Story parked' rather than adding a structured recovery plan.

Evidence:

- `internal/turn/implement_coordinator.go:89`
- `internal/turn/implement_park_note.go:89`

## f3

Current story workers cannot submit owner questions, but the coordinator sync resolver can submit blocked questions with options and recommendation through the factory; this corrects the initial finding that grouped both roles together. Incomplete worker results retry before exhaustion and blocked results park immediately; handoff must preserve role-specific authority.

Evidence:

- `internal/teamsource/implement/story-worker.md:48`
- `internal/teamsource/implement/sync-resolver.md:30`
- `internal/turn/implement_steps.go:373`
- `internal/turn/implement_steps.go:487`

## f4

Validate admission reports unmerged story states but ends with 'Finish Implement first, then send /validate again.' This names the blocked stage but does not explain how to resolve each parked story or why waiting stories have not started.

Evidence:

- `internal/turn/validate_admission.go:165`

## f5

Validate already has reusable state-aware command generation and separates check, finding choice and delivery commands. Findings are traversed in stored order; this function does not rank owner actions by shipping impact. Story comments distinguish blocks and owner items, but check templates use pass|fail syntax and do not explain both consequences individually.

Evidence:

- `internal/turn/validate_commands.go:196`
- `internal/turn/validate_story_report.go:249`
- `internal/turn/validate_state.go:372`

## f6

Some Validate recovery is already precise: merge-conflict guidance gives /validate and an optional return-to-Implement command with consequences. Other delivery errors use 'repeat your command to retry' and issue errors use the same generic wording. These paths need state-specific comparison rather than blanket replacement.

Evidence:

- `internal/turn/validate_commands.go:279`
- `internal/turn/validate_commands.go:357`
- `internal/turn/validate_report.go:1`

## f7

Additional definite gaps exist in fixed refusals: Bug execution unavailable names internal dependencies without a recovery action; graph resume refusal says 'reply again once that has changed'; owner-only delivery refusal has no next step; unsupported Doctor only states availability. Team and workflow configuration refusals give broad setup instructions rather than concrete commands.

Evidence:

- `internal/report/reply.go:922`
- `internal/report/reply.go:925`
- `internal/report/reply.go:931`
- `internal/report/reply.go:975`
- `internal/report/reply.go:992`

## f8

Scheduling adds independent stop paths absent from the issue samples: failed prerequisites produce only 'will not run because one of its prerequisites failed'; proposal generation failure says no work was scheduled without recovery guidance. Confirmation failures and expired/superseded proposals also use generic regeneration/comment-again wording.

Evidence:

- `internal/report/reply.go:1122`
- `internal/report/reply.go:1146`
- `internal/report/reply.go:1166`
- `internal/report/reply.go:1180`

## f9

Run outcomes are another reporting surface: unknown outcome points to local logs; session limit only says feature incomplete; failure leads with tool exit status and diagnostic; timeout asks to raise timeout or narrow task; shutdown says comment again. They do not consistently provide precise owner options or explain other work that continues.

Evidence:

- `internal/report/run.go:478`
- `internal/report/run.go:495`
- `internal/report/run.go:519`

## f10

Generic graph stop recovery preserves dispatch limits for the same instance; this is not a universal owner-reply invariant, because Implement parked-story resumption resets its worker-attempt ledger and starts a new epoch, and failed-coordinator guidance explicitly describes fresh step limits.

Evidence:

- `internal/turn/graph_stops.go:60`
- `internal/store/implement_stories.go:243`
- `internal/turn/implement_sync_park.go:167`
- `internal/turn/implement_graph.go:283`

## f11

Doctor provides an existing strong guidance example: diagnosis instructions require an answer-first explanation and a final numbered options finding with exact commands and consequences. Repair failure rendering offers a concrete fresh-diagnosis command. Owner-stop summaries merely state that workflow/conversation stopped and history was retained.

Evidence:

- `internal/instructionpackage/embedded/agents/doctor-diagnosis.md:87`
- `internal/doctor/presentation.go:88`
- `internal/doctor/service.go:474`

## f12

Conversation is a separate instruction surface: the action-outcome agent is limited to one or two sentences and must use saved facts, including background execution. This can compete with the requested multi-option stop guidance; downstream shaping must account for short chat presentation and avoid inventing commands or execution state.

Evidence:

- `internal/instructionpackage/embedded/agents/conversation-action-outcome.md:1`
- `internal/instructionpackage/embedded/agents/conversation-factory.md:89`
- `internal/messaging/notices.go:40`
- `internal/messaging/presentation/render.go:5`

## f13

Role sources and shipped releases are distinct. The canonical Go team declares version 1.0.5; generated release documentation forbids direct distribution edits and says historical releases and captures are immutable. Shared embedded agents are another source tree. Updating only shipped worker text would miss source ownership and other installed stack releases.

Evidence:

- `internal/teamsource/release.json:4`
- `teams/go-development/v1.0.5/GENERATED.md:3`
- `teams/teams.json:1`
- `internal/instructionpackage/embedded/agents/doctor-diagnosis.md:1`

## f14

Existing tests cover reply text, Validate admission, story comments, graph stop recovery and owner resume semantics; the initial bounded research pass ran none, while subsequent rechecks ran targeted tests recorded in f23 and f36. No product code has changed.

Evidence:

- `internal/report/reply_test.go:177`
- `internal/turn/validate_admission_test.go:191`
- `internal/turn/implement_graph_run_test.go:494`
- `internal/turn/validate_story_report_test.go:1`

## f15

Current Validate controls support explicit revisions and displayed decision references for supported shorthand, resolved once to retained exact input. Combined shipping now records a reasoned waiver of displayed blockers while preserving factual suite/review/choice evidence; the earlier description of shipping as recording owner passes and optional deferrals must not be used as the current recovery contract.

Evidence:

- `internal/turn/owner_decisions.go:17`
- `internal/turn/validate_ship_waiver.go:74`
- `internal/turn/validate_state.go:360`
- `internal/turn/validate_resolve.go:261`

## f16

The substantive judge review (issue comment 6395698360) raised two supported concerns: exhaustive inventory was deferred, and the synchronization-budget rule conflated same-instance continuation with story restart; prior canceled judge runs supplied no substantive findings. Owner comment 6395721203 requests this code recheck.

_No source reference was recorded for this finding._

## f17

Recovery trace: generic graph continuation reparks a sync gate when its same-instance limit is exhausted; Implement parked-reply targeting resumes named parked keys or all parked keys when no existing key is named, transitions parked to waiting, retains branch and guidance, clears attempts/last failure/worker session, and updates StateAt; old failed worker graphs predating StateAt are skipped, preventing automatic recovery from reparking the newly resumed story. Separate failed-coordinator reporting tells owners a reply starts Implement from base sync with fresh step limits. Guidance must name the actual recovery path, without promising that restart fixes the underlying conflict.

Evidence:

- `internal/turn/graph_stops.go:60`
- `internal/turn/implement_graph.go:283`
- `internal/turn/implement_graph.go:318`
- `internal/store/implement_stories.go:243`
- `internal/turn/implement_sync_park.go:186`

## f18

The no-result branch is deficient: it promotes a historic Claude stale-credential anecdote into a generic likely cause and recommends manual tool login. Claude classification proves only exit zero with no terminal result and explicitly allows stdout loss; Codex also emits no-result when turn.completed is absent, without diagnosing credentials. Scope must replace unsupported certainty with known outcome, explicit uncertainty and evidence-based recovery.

Evidence:

- `internal/report/run.go:529`
- `internal/agent/result.go:37`
- `internal/harness/runner.go:1240`
- `internal/codex/runner.go:441`

## f19

Fixed reply branch inventory, inspected directly (D=deficient against the four requirements, C=compliant for this branch's bounded action, N=not a stop/ask/redo). EmptySpike D: usable example lacks blocked/continuing work and effect; UnknownCommand D: vocabulary lacks selected recovery and effect; LegacyToolLabel D: exact label removal but generic repeat; AmbiguousAgentLabels D: valid alternatives but generic repeat; MalformedAgentLabel D: correction lacks next reply/effect; BugModeSwitch D: no recovery action; DoctorUnsupported D: no supported alternative; BugUnavailable D: internal dependency list, no recovery; MissingTicketPublisher D: meaningful retry command, no precise restoration procedure/actor; MissingTeam D: generic linking; UnsupportedWorkflow D: unbound choices; MissingModeMapping D: partial flag, no concrete connection action; MalformedModeLabel D: correction without next action; AmbiguousModeLabels D: generic selection and repeat; StoryPlanning N: admitted progress; StoryLocked D: concrete replan and effect but no current parent link; StoryPlanOnly D: parent redirect without link or valid current command; GraphReplyBetweenQuestions D: generic repost without current link; BugCommentNotApplied D: revision placeholder and no current review link; ImplementCommentNotApplied D: selection semantics but no actual parked targets; GraphResumeRefused D: generic changed-condition repeat; GraphOwnerWaitRefused D: wait/repost without link or specific condition; CodeControlRefused D: correct owner restriction, no owner handoff; ValidateCommentNotApplied D: report reference without current link/options; ImplementFinished C: exact /validate and result; ImplementFinishedAutoValidate C: automatic next step and no action; ImplementFinishedValidated D: result reference without actionable current report link; ConflictingModeSelection D: generic relabel/repeat; UnknownAgent D: remove/change alternatives without installed valid choices; AgentSwitchRefused D: explains fresh conversation but generic reply; RefusesSyncTooFarBehind D: manual/config alternatives without concrete branch steps; RefusesSync D: conflict diagnosis but manual correction/generic repeat; SuggestsResolution D: legacy regeneration advice lacks supported exact regeneration action.

Evidence:

- `internal/report/reply.go:876`
- `internal/report/reply.go:920`
- `internal/report/reply.go:957`
- `internal/report/reply.go:992`
- `internal/report/reply.go:1031`

## f20

Scheduling/confirmation branch inventory: ScheduleInvalid D: accepted grammars without input-specific correction or effect; ScheduleConfirmed N: scheduled progress; ScheduleBlocked D: missing failed prerequisite identity, recovery, other schedules; ScheduleProposalReady D: exact accept/reject and effects exist, independent work/ordering absent; ScheduleProposalAccepted N: completed scheduling; ScheduleProposalRejected N: completed rejection; ScheduleProposalFailed D: no explanation/recovery; ConfirmInvalid D: grammar only with unresolved ID; ConfirmNotOwner D: no owner handoff; ConfirmUnknownProposal D: no route to current proposal; ConfirmExpiredProposal D and ConfirmSupersededProposal D: generic regeneration reply; ConfirmAlreadyApplied N: idempotent acknowledgment, not a new stop; ConfirmFailed D: raw reason and broad manual/regeneration options without exact steps; AppliesResolution N: applied outcome. CreationProposal D: concrete approval and effects, no explicit optional decline/other-work statement; CreationAccepted confirmed N; historical acceptance with unavailable/unknown/graph publication D: eligibility explanation but no concrete recovery; CreationAccepted refused expired/stale/quota/bound/disabled/default D: generic renewal/current proposal/timing/operator actions, some missing entirely; partial/uncertain publication D: correctly warns against duplicates and asks for missing-only proposal, but lacks concrete reconciliation route. CreationRejected syntax/payload/version/fields/mode/disabled/proposal/controls D: closed reasons and no-work fact exist, but state-specific corrected command or operator procedure is absent. Invalid kind and unknown rejection metadata N: renderer fails closed, not a rendered owner branch.

Evidence:

- `internal/report/reply.go:1098`
- `internal/report/reply.go:1149`
- `internal/report/creation_rejection.go:6`
- `internal/report/creation_acceptance.go:49`
- `internal/report/creation_guidance.go:20`

## f21

Run outcome branch inventory: succeeded N; nil result D: speculative interruption/local logs without exact recovery; interrupted D: feature incomplete but recovery section may qualify it (producer assessment remains unassessed); failed nonzero D: process status/diagnostic, no recovery; failed signal D: no exit status/recovery; clean exit with rejected shared result D: distinguishes rejection but no repair action; timed out D: timeout/narrowing alternatives lack concrete supported steps; abandoned D: blanket comment-again advice needs comparison with automatic recovery; no result D: unsupported credential diagnosis; unrecognised outcome D: bug/logs without recovery. Failure overlays before-agent, after-success and secondary-failure D: causal error/work location but no precise actions; partial managed-feature success D: remaining stories open without their states or next step. Recovery section branches manual-only, missing due time, succeeded and other terminal status D: unstructured reason or vague inspect-result action; pending/reserved with due time and running C for local wait instruction, with affected work delegated to full report; StopRecovery is data-driven and unassessed until its producers are traced.

Evidence:

- `internal/report/run.go:385`
- `internal/report/run.go:399`
- `internal/report/run.go:474`
- `internal/report/recovery.go:18`
- `internal/report/recovery.go:53`

## f22

Overlap established by fetching #1128: it covers only agent-written owner comments, excludes Go changes, leaves factory commands/notices/options to the factory, and proposes a shared owner-report skill after #998. #1067 includes those factory surfaces and failed-action recovery; shared agent explanations overlap, but #1128 is not a replacement for #1067. This is source issue evidence, not authorization to implement its proposal.

_No source reference was recorded for this finding._

## f23

Verification ran successfully: go test ./internal/turn -run 'TestImplementWorkerSyncFailedSettlementRecoversBeforeFreshAdmission|TestImplementCoordinatorExhaustedStepTellsTheOwnerOnce' -count=1. These existing tests exercise stale failed-sync settlement versus owner-resumed epoch and one retained coordinator exhaustion report; they do not prove a full restart end-to-end or complete reporting compliance. No product code changed.

Evidence:

- `internal/turn/implement_sync_recovery_test.go:15`
- `internal/turn/implement_graph_run_test.go:1629`

## f24

Remaining unassessed source inventory after the current pass: Doctor diagnosis/apply/restart/rollback entry refusals and their transport rendering; conversation proposal confirmation, permission/access, details/inbox, argument and command failures; lifecycle and owner-input guidance messages; final mapping of those producers to owner-visible reports. Historical immutable releases and live provider/incident behavior remain explicitly outside the source audit's completeness claim. Validate, graph recovery, Research/Spike/Plan message and maintained role classifications are now recorded in f25–f35.

_No source reference was recorded for this finding._

## f25

Validate admission inventory (D=deficient, C=compliant for the bounded action, N=internal/no new owner action): non-owner D, no owner handoff; stopped-entry replay C, exact /validate and fresh-run effect; unfinished Implement D, generic finish-first without per-story recovery; newer head while old run executes C, wait then /validate; duplicate same-head with existing report C, direct report link and no duplicate run; duplicate before report C, report will appear; missing instance/non-running/unclaimed control D, no-wait fact without current recovery. Internal lookup/binding failures N at this renderer and flow through run/graph failure handling.

Evidence:

- `internal/turn/validate_admission.go:163`
- `internal/turn/validate_admission.go:274`
- `internal/turn/validate_admission.go:362`
- `internal/turn/validate_admission.go:505`
- `internal/turn/validate_admission.go:593`

## f26

Validate owner-control inventory: no reply/replayed accepted reply N; wrong actor D, no handoff; invalid syntax or decision binding D, parser/error text without targeted corrected command; stale revision and changed delivery head D, latest-report direction lacks refresh route; not-reviewed item C for retry command and effect; ineligible check, unknown finding, check-required-before-choice, no-choice state, disallowed accept and blocking defer D, cause exists but missing exact allowed current command or full consequence; push-not-ready D, blocker list lacks order; already pushed/already merged/already closed N locally, parent Next must remain correct; merge-before-push, PR/head mismatch, close-before-merge and wrong close set D, specific prerequisite but no exact supported replacement; return unsupported/already returned/unassessed/no targets D except already-returned state can be explained by parent Next; ticket approver before code-owner ship D, no direct approver action; ship-before-assessment D; delivery complete N; changed waiver, head or admitted input D, fresh-decision requirement without precise current destination.

Evidence:

- `internal/turn/validate_resolve.go:97`
- `internal/turn/validate_resolve.go:117`
- `internal/turn/validate_resolve.go:137`
- `internal/turn/validate_resolve.go:189`
- `internal/turn/validate_resolve.go:240`
- `internal/turn/validate_resolve.go:352`

## f27

Validate Next/report inventory: returned-to-Implement C, identifies automatic revalidation; merge conflict C for supported factory return alternative with effect, manual route still needs concrete branch procedure; ship-line D, consequence not explained inline; failed-suite/remaining-blocker with return option C for repair alternatives but ordering remains absent; missing reviewers C, exact /validate with passed-suite reuse; not-assessed C, explicit automatic wait; owner items D, unordered sub-issue direction; issue creation pending C for bounded wait; next push/merge/close D, command without consequence; complete N. Owner-wait candidate D: at most 12 open items, no per-item options, no direct report link, and 'One confirmation never implies the next' conflicts with the existing explicit combined ship action. Report waiver prose exposes internal actor/instance/evidence identities instead of explaining the owner's decision simply.

Evidence:

- `internal/turn/validate_commands.go:290`
- `internal/turn/validate_owner.go:380`
- `internal/turn/validate_report.go:31`
- `internal/turn/validate_report.go:99`

## f28

Validate story-report/shortening inventory: missing reviewer D for dependency on parent rather than immediate command; failed criterion/review blocker D for incomplete choice effect or exact recovery; eligible owner check D, pass|fail template is not copyable as one valid command and each effect is absent; finding choice D, pipe alternatives omit consequences; follow-up issue retry D, exact command exists but uncertain effect/reconciliation consequence is unclear; nothing-to-do N. Full-text/bare-item shortening D: it can remove explanations; omitted-item fallback D: it promises parent commands even though the parent renders a summary table and Next rather than every omitted item's command. Scope must guarantee a real complete destination for omitted recovery items.

Evidence:

- `internal/turn/validate_story_report.go:184`
- `internal/turn/validate_story_report.go:225`
- `internal/turn/validate_story_report.go:260`
- `internal/turn/validate_story_report.go:289`
- `internal/turn/validate_report.go:81`

## f29

Validate effect/repair inventory: issue lookup failure, ambiguous marker, unconfirmed create/missing number D; push error or unobserved remote head D; PR list/create/unconfirmed head D; merge read failure, changed branch/PR head, rejected merge, unreadable outcome, not-yet-merged and wrong merged head D; partial issue closure or head drift D. All retain effect facts but owner recovery is mostly generic command retry, not state-specific reconciliation. Already completed/reconciled effects N. Ship-step push/merge/close retry has a current command but needs consequences (D); merge-conflict alternative C as above; PR mismatch or not-ready ship D. Repair spent/blocked/unfinished/too-big/handback D at owner surface: round facts/reasons and limited objectives exist, but ordered recovery comes only from generic Next; kept repair N while automatic checks continue. Handback reopened/skipped D: explains what reopened but not individual skipped-story recovery.

Evidence:

- `internal/turn/validate_remote.go:135`
- `internal/turn/validate_remote.go:250`
- `internal/turn/validate_remote.go:380`
- `internal/turn/validate_remote.go:475`
- `internal/turn/validate_resolve.go:388`
- `internal/turn/validate_repair.go:195`
- `internal/turn/validate_implement.go:415`

## f30

Graph recovery projection inventory: no retained graph N; completed graph D, vague read-result without link; non-running graph with diagnostic C for exact Doctor diagnosis step, but ongoing sibling work omitted; waiting question D, generic reply with no question link/options; claimed continuation C, wait with no completion claim; pending C, eligibility not promised start; reserved/running C, wait; cancelled with already scheduled follow-up C; cancelled with active attempt C for exact Doctor inspection; cancelled with existing run C, wait; cancelled with valid saved launch or retained Research files D, generic guidance instead of task-specific reply; cancelled without saved launch C for exact Doctor inspection and no ordinary-reply claim; missing/mismatched dispatch C for safe diagnosis fallback. Stop/dispatch publication fallbacks supply exact diagnosis commands but still lead with internal step/cause and do not show independent affected work.

Evidence:

- `internal/turn/recovery_projection.go:13`
- `internal/turn/graph_stops.go:193`
- `internal/turn/graph_delivery_reports.go:82`

## f31

Session recovery inventory: auto disabled D; unreliable reset D; missing conversation identity D; automatic-attempt limit spent D—all state manual recovery without exact supported action. Persisted pending/reserved/running automatic continuation C for local wait state; succeeded D for indirect retained-result direction; canceled/other terminal state D for manual-reason or inspect guidance. Cancellation reporting retains reason, not concrete next action (D). Chat suppresses abandoned run notices because daemon recovery relaunches them, while generic run text says comment again: these surfaces need one state-derived explanation, not duplicate retry guidance.

Evidence:

- `internal/turn/continuations.go:21`
- `internal/report/recovery.go:18`
- `internal/daemon/continuation_cancellations.go:90`
- `internal/messaging/notices.go:67`
- `internal/report/run.go:522`

## f32

Research/Spike/Plan reporting inventory: Research review footer C for exact done, change feedback and /plan alternatives/consequences; Research question footer D, answer syntax exists but no per-choice effect guarantee; failure footer D, generic new comment; final terminal footer C for /plan handoff; auto-next footer D, timing promise and interruption assertion must follow real admission state; seven-day reminder C, direct wait link and optional later reply. Spike ask/failure shares those classifications; final C for explicit advisory handoff; failed/blocked/too-big measured check D, not-ready fact without supported recovery; unconfirmed issue filing D, manual repository check with no exact inspection route. Plan ordinary failure D, restart effect stated but generic reply; capacity-exhausted failure D, removes retry advice without alternate recovery; internal unreportable/prelaunch or pending-delivery cases N at renderer and remain factory recovery debts rather than claimed successful reports.

Evidence:

- `internal/turn/research_comments.go:421`
- `internal/turn/research_reminder.go:52`
- `internal/turn/spike_workflow.go:258`
- `internal/turn/plan_graph_failure.go:56`

## f33

Plan notice inventory: stories-created C for linked sub-issues, approve/change alternatives and stage ordering; plan-ready C for exact Implement action; waiting-with-current-commands C for bounded stage approval, but no-review rows D because 'still being worked on or unresolved' merges different states. Set-promotion refusal with current set C for exact permitted command, without current set D for recovery; final-approval mismatch with current final review C, without current review C for waiting but needs accurate future update; child-reply generic refusal D, lists speculative possible reasons rather than actual one; lifecycle closed/reopened with no holds N for plain update, lifecycle/request/plan/dependency holds D for 'resolve listed cause' without exact action; parent summary missing issue binding D, blocked with no repair route. PlanReplyExplanationText D when producer carries no current command or when its shortening clips it.

Evidence:

- `internal/turn/plan_graph_notices.go:140`
- `internal/turn/plan_graph_promote.go:25`
- `internal/report/plan_progress.go:19`
- `internal/turn/plan_summary.go:125`

## f34

Maintained instruction inventory covers all 16 role files per Go, .NET, Python and Swift source tree using content comparison, not inspection of historical releases: Research investigate D for no stop-impact/action contract; shaping and Plan set/story/technical/acceptance D despite concrete recommended options because stop consequences/continuing work are not required; worker D despite plain reason; sync resolver D overall, with Go/Python/Swift supporting submitted blocked questions while .NET still instructs no owner questions; Validate acceptance/code/architecture D at owner surface despite evidence findings and no-question permissions; Validate repair D for missing owner recovery contract despite precise handback objectives; Bug analyst/fixer D for partial/blocked/reshape explanations without complete options/effects; Spike investigator/fixer D for terminal not-done handoff without complete exact next steps. Preserve each role's real authority and let the authorized factory reporting surface supply commands.

Evidence:

- `internal/teamsource/implement/sync-resolver.md:30`
- `teams/source/dotnet-web/implement/sync-resolver.md:30`
- `internal/teamsource/python-fastapi/implement/sync-resolver.md:30`
- `internal/teamsource/swift-ios/implement/sync-resolver.md:30`
- `internal/teamsource/bug/bug-analyst.md:49`
- `internal/teamsource/spike/spike-fixer.md:39`
- `internal/teamsource/validate/validate-repair.md:42`

## f35

Shared instruction and release inventory: Doctor diagnosis is a compliant guidance model (answer first, exact numbered options/effects, uncertainty and no authority escalation); conversation action-outcome D due to one/two-sentence and no-ID instructions that can hide necessary command targeting; factory/reasoner/correction roles D for no common stop-guidance exception although they preserve fact/authority limits. Maintained release sources are Go 1.0.5, Python 1.0.5, Swift 1.0.1 and .NET 1.0.1 under teams/source/dotnet-web, not internal/teamsource/dotnet-web; catalogue historical releases are immutable and not a migration target. Future instruction releases must cover all four source owners and shared embedded roles.

Evidence:

- `internal/instructionpackage/embedded/agents/doctor-diagnosis.md:87`
- `internal/instructionpackage/embedded/agents/conversation-action-outcome.md:1`
- `internal/instructionpackage/embedded/agents/conversation-factory.md:69`
- `internal/teamrelease/dotnet_web_test.go:22`
- `internal/teamrelease/release.go:81`

## f36

Further verification passed: go test ./internal/turn -run 'TestOwnerShipDisplayedDeliveryCommitKeepsTestedEvidence|TestImplementCoordinatorResolverQuestionsReachTheOwnerWithTheirOptions' -count=1. These existing tests verify current waiver evidence semantics and resolver option propagation; they do not establish overall guidance compliance. No product code was modified.

Evidence:

- `internal/turn/validate_ship_waiver_test.go:10`
- `internal/turn/implement_graph_run_test.go:1504`
