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

Graph stop reporting can fall back to a technical step/cause when no usable reason is submitted. The published stop message names internal origin and cause and asks for guidance. Critically, a reply cannot renew an exhausted sync budget; simply advising another reply can lead to another park.

Evidence:

- `internal/turn/graph_stops.go:37`
- `internal/turn/graph_stops.go:60`
- `internal/turn/graph_stops.go:197`
- `internal/turn/implement_sync_park.go:82`

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
