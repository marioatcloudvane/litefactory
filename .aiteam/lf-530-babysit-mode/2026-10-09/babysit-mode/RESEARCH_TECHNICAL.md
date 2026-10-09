# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## F1

The daemon treats only the owner as an instruction source. The identity package resolves the owner from gh and checks bot != owner so the daemon never reads its own comments as instructions. A bot-signed babysitter comment cannot start a run. Source: internal/identity/doc.go; internal/daemon/tick.go comment handling.

_No source reference was recorded for this finding._

## F2

Bug approval requires the authenticated owner. AdmitGraphBugOwnerReply accepts only the exact approve-bug analysis command with a revision; store.GraphOwnerReply validation requires AuthorID == OwnerID. docs say only factory-authenticated exact owner approval authorizes repair. A babysitter approval needs a new daemon path. Source: internal/turn/bug_conversation.go; internal/store/graph_owner_waits.go:74; docs/workflow/bug-runtime.md.

_No source reference was recorded for this finding._

## F3

Bug workflow shape: one graph with an owner conversation using a read-only flagship analyst, approval, one fixer, then factory finalization (push, PR, merge, issue close). Stopped origins resume on an ordinary owner reply as guidance; guidance grants no approval. Source: docs/workflow/bug-runtime.md; internal/turn/bug_analysis_policy.go.

_No source reference was recorded for this finding._

## F4

Only /approve-bug is routed as a Bug control command; other commands fall through to normal handling. A /babysit command does not exist. Source: internal/daemon/bug_commands.go.

_No source reference was recorded for this finding._

## F5

No babysit code exists: a case-insensitive repo-wide search for babysit found nothing.

_No source reference was recorded for this finding._

## F6

Tiers flagship, balanced and fast map to models per vendor. Agents are an ordered config list; the first is the default. No hook for using the other agent was found. Source: internal/config/tier.go; internal/config/config.go.

_No source reference was recorded for this finding._

## F7

internal/rights covers only actions tagged lf_rights and answers whether a person may take an action. No delegated-actor or bot-actor concept was found. Issue #495 is closed; #529 is open. Source: internal/rights/doc.go; gh issue state.

_No source reference was recorded for this finding._

## F8

Only a run-count limit exists: settings.max_concurrent_runs, default 3. No CPU or load measure was found. Source: internal/config/config.go.

_No source reference was recorded for this finding._

## F9

TestMCPServer_SizeBounds is a real test in internal/mcpadapter. Known-flake detection would need a list or a rerun policy. Source: internal/mcpadapter/server_test.go:380.

_No source reference was recorded for this finding._
