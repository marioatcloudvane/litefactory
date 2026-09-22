# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

At revision 0a0c567e9a78a57766c6f76800743315c6b014ed, creation handling still leaves mode, unavailable runtime and approval errors unconsumed. Approval success reaches Consume. The exact parser rejects the reported malformed text when preceding gates admit it.

Evidence:

- `internal/daemon/mcp_creation.go:33`
- `internal/turn/mcp_creation.go:17`

## f2

At the inspected revision, leave logs errors without consuming; drain stops at the first unconsumed comment and returns a backlog indication for later retries. Under unchanged deterministic rejection, later comments on that task remain blocked.

Evidence:

- `internal/daemon/tick.go:1450`
- `internal/daemon/tick.go:1474`
- `internal/daemon/tick.go:2126`

## f3

Identity filtering precedes command execution. Creation authority checks precede approval parsing, so the historical authority-refused message does not prove a syntax failure. Wrong or missing Research mode and unavailable runtime can block earlier.

Evidence:

- `internal/daemon/tick.go:1595`
- `internal/daemon/mcp_creation.go:34`
- `internal/turn/mcp_creation.go:93`

## f4

Existing reportReply wraps replies in recovery keyed by session and source comment; resumeReport can recover pending delivery. These are reuse candidates. Earlier findings also identified malformed confirmation, schedule, follow-up and exception refusal reporting; the count of leave callers alone does not establish equivalent defects across all commands.

Evidence:

- `internal/daemon/report_recovery.go:255`
- `internal/daemon/report_recovery.go:272`
- `internal/daemon/confirm.go:35`
- `internal/turn/followups.go:72`
- `internal/turn/owner_exceptions.go:92`

## f6

The prior investigation recorded five focused tests passing on Go 1.27.0 darwin/arm64 at revision 40fa12605a4c5bb5b366a6e2806add039c7cab40: exact approval grammar, retained proposal intake, queue stop after report failure, backlog retention and invalid confirmation reply/consumption. These are historical results, not tests rerun in this pass or an end-to-end live reproduction.

Evidence:

- `internal/turn/mcp_creation_unit_test.go:13`
- `internal/daemon/mcp_creation_test.go:14`
- `internal/daemon/tick_test.go:299`
- `internal/daemon/backlog_test.go:109`
- `internal/daemon/confirm_test.go:54`

## f7

At revision 0a0c567e9a78a57766c6f76800743315c6b014ed, preparation errors also leave the comment unconsumed. Preparation checks provenance, decodes the payload, rejects unsupported versions and retains a proposal before presentation. Those deterministic failures share a return path with dependency and persistence failures.

Evidence:

- `internal/daemon/mcp_creation.go:46`
- `internal/turn/publication_recovery.go:114`

## f8

Owner-intent admission checks freshness, proposal scope, relationship and current proposal before looking up an existing intent for the source comment. It returns both refusal and database errors. A retry after successful admission but failed consumption can encounter changed eligibility before reaching existing-intent recovery; classification must account for recorded effects.

Evidence:

- `internal/store/mcp_creation.go:153`
- `internal/store/mcp_creation.go:179`
- `internal/daemon/mcp_creation.go:58`
