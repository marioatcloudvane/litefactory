# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## F1

The daemon treats only the owner as an instruction source; identity checks bot != owner. Bot comments cannot drive a run, so steering needs a daemon-internal path. Source: internal/identity/doc.go; internal/daemon/tick.go.

_No source reference was recorded for this finding._

## F2

Bug approval needs the authenticated owner: the stored reply must have author equal to owner. A babysitter approval needs a new delegated path. Source: internal/turn/bug_conversation.go; internal/store/graph_owner_waits.go; docs/workflow/bug-runtime.md.

_No source reference was recorded for this finding._

## F3

internal/rights covers only tagged graph actions and has no delegated-actor concept. #495 closed. Source: internal/rights/doc.go.

_No source reference was recorded for this finding._

## F4

Doctor (#529, now closed) exists: a saved conversation, isolated read tools, typed repair with holds, and provider selection that tries the issue's agent then every configured agent at flagship. It is issue-scoped, started from the CLI. Source: internal/doctor/service.go; internal/doctor/read_tools.go; internal/cli/cmd_doctor.go.

_No source reference was recorded for this finding._

## F5

No messaging or Discord code is in main. #562 has three stories stopped (including Discord delivery guarantees). Source: repo search; gh issue 562 comments.

_No source reference was recorded for this finding._

## F6

The neutral ticket protocol (#556, #609) is open and unmerged. The code is GitHub-specific today. Source: gh issues; internal/github.

_No source reference was recorded for this finding._

## F7

No babysit code exists anywhere in the repository.

_No source reference was recorded for this finding._

## F8

Only a run-count limit exists (max concurrent runs, default 3); no load measure. Source: internal/config/config.go.

_No source reference was recorded for this finding._

## F9

The daemon can be woken by hints (the webhook sink) and by wake sources in the tick, so event-driven wake-ups have a precedent. Source: internal/webhook/doc.go; internal/daemon/daemon.go.

_No source reference was recorded for this finding._

## F10

Observed in the owner's learnings: the worst failures were changes to the running system (deploy during worker launch #587, team relink mid-Plan #562). Source: issue direction comment of 2026-10-10.

_No source reference was recorded for this finding._
