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

Workers and sync resolvers require plain reasons and forbid direct owner questions. Worker incomplete results retry before exhaustion; blocked results park immediately. Guidance improvements therefore need coordination between worker result content and factory handoff, while preserving who may ask and resume.

Evidence:

- `internal/teamsource/implement/story-worker.md:47`
- `internal/teamsource/implement/story-worker.md:62`
- `internal/teamsource/implement/sync-resolver.md:30`

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

Existing tests cover reply text, Validate admission, story comments, graph stop recovery and owner resume semantics. They are useful places to verify the requested guidance without changing authorization or retry behavior. This research pass ran no tests and changed no product code.

Evidence:

- `internal/report/reply_test.go:177`
- `internal/turn/validate_admission_test.go:191`
- `internal/turn/implement_graph_run_test.go:494`
- `internal/turn/validate_story_report_test.go:1`

## f15

Validate command parsing requires a whole single-line comment with revision and command-specific evidence; next-command generation is state-aware and the combined ship path can record owner passes and optional deferrals, so guidance must explain effects rather than infer shipping blockers from the item list alone.

Evidence:

- `internal/turn/validate_commands.go:77`
- `internal/turn/validate_commands.go:196`
- `internal/turn/validate_commands.go:241`

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

Explicit unassessed inventory remainder for the next pass: all Validate admission/control/stale/delivery/repair/handback branches, graph-stop and stop-recovery producers, session recovery and automatic-shutdown producers, Doctor commands/owner-stop summaries, conversation notices and truncation, Plan question/report templates and workflow footers, Bug/Spike/Research publication paths, maintained stack role sources and release propagation. Fixed-reply and run-outcome inventory above is a bounded source recheck, not exhaustive coverage of these remaining families.

_No source reference was recorded for this finding._
