# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

At revision 40fa12605a4c5bb5b366a6e2806add039c7cab40, authenticated comment routing intercepts the creation command before generic unknown-command replies. Creation handling leaves mode, availability and approval errors unconsumed. Approval success alone reaches Consume. The reported malformed text deterministically fails the exact parser when the preceding gate admits it.

Evidence:

- `internal/daemon/tick.go:1595`
- `internal/daemon/mcp_creation.go:33`
- `internal/turn/mcp_creation.go:17`
- `internal/turn/publication_recovery.go:105`

## f2

leave logs non-shutdown errors and returns without advancing the watermark. drain reads the watermark after each comment and stops at the first unconsumed comment. Backlog discovery retries that session; other tasks have separate workers. This control flow establishes indefinite blocking under unchanged malformed input; there is no attempt-count or age-based escape on this path.

Evidence:

- `internal/daemon/tick.go:2126`
- `internal/daemon/tick.go:1434`
- `internal/daemon/tick.go:1474`

## f3

Creation authority checks precede parsing. Disabled creation or invalid control provenance returns ErrCreationAuthority before parser execution. The historical authority-refused log therefore cannot uniquely identify a parse failure. Wrong/missing Research mode and unavailable runtime are earlier blocking branches.

Evidence:

- `internal/turn/mcp_creation.go:93`
- `internal/daemon/mcp_creation.go:34`

## f4

Existing reusable behavior: schedule parse errors call replySchedule; confirm parse errors call replyConfirm, whose lifecycle posts before consuming. reportReply wraps reporting with source-keyed recovery. Followup and owner-exception parsers already route malformed input to refusal reporting, so the issue's broad call-count argument does not prove all those paths defective.

Evidence:

- `internal/daemon/tick.go:1667`
- `internal/daemon/confirm.go:35`
- `internal/daemon/confirm.go:138`
- `internal/daemon/report_recovery.go:255`
- `internal/turn/followups.go:72`
- `internal/turn/owner_exceptions.go:92`

## f5

The complete CLI registration surface contains task clean and task retire, but no dedicated stuck-comment skip or watermark recovery command.

Evidence:

- `internal/cli/commands.go:8`

## f6

Focused existing tests passed on Go 1.27.0 darwin/arm64, exit 0: go test ./internal/turn ./internal/daemon -run '^(TestOwnerGrantCommand_ExactPublicGrammar|TestMCPOwnerIntake_RetainedProposalAndTrustedMetadata|TestQueueStopsAtTheFirstCommentWhoseReportFailed|TestAnUnconsumedCommentLeavesTheBacklogFlagSet|TestHandleConfirmInvalidGrammarRepliesAndConsumes)$' -count=1. These verify parser rejection, admission, queue ordering/backlog and a working rejection-response pattern. They do not constitute an end-to-end reproduction of the exact historical creation comment. Worktree remained clean.

Evidence:

- `internal/turn/mcp_creation_unit_test.go:13`
- `internal/daemon/mcp_creation_test.go:14`
- `internal/daemon/tick_test.go:299`
- `internal/daemon/backlog_test.go:109`
- `internal/daemon/confirm_test.go:54`
