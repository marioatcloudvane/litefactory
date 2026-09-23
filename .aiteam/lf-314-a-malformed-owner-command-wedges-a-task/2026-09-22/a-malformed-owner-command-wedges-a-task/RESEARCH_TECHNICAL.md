# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

At inspected revision 1be56786ec00c214d60733f6b16ac87a83a0bd5d, creation mode, availability and approval failures remain unconsumed, while the exact approval parser rejects malformed syntax.

Evidence:

- `internal/daemon/mcp_creation.go:33`
- `internal/turn/mcp_creation.go:17`

## f2

Prior source findings establish that leaving a comment unconsumed stops that task's drain and retains backlog for retry; the reported incident duration is not a new measurement.

Evidence:

- `internal/daemon/tick.go:1450`
- `internal/daemon/tick.go:2126`

## f3

Current routing consumes filtered bot and third-party comments without execution, resumes pending delivery before creation handling, and checks creation authority before approval parsing.

Evidence:

- `internal/daemon/tick.go:1595`
- `internal/turn/mcp_creation.go:93`

## f4

Current reply delivery retains rendered bytes and a stable marker before dispatch, reconciles possibly sent replies, and resumes pending delivery before command reevaluation.

Evidence:

- `internal/turn/report_delivery.go:58`
- `internal/turn/report_delivery.go:70`
- `internal/turn/report_delivery.go:128`
- `internal/daemon/report_recovery.go:255`

## f6

The earlier investigation recorded five focused tests passing at revision 40fa12605a4c5bb5b366a6e2806add039c7cab40; these remain historical evidence and were not rerun during shaping.

Evidence:

- `internal/turn/mcp_creation_unit_test.go:13`
- `internal/daemon/mcp_creation_test.go:14`
- `internal/daemon/tick_test.go:299`
- `internal/daemon/backlog_test.go:109`
- `internal/daemon/confirm_test.go:54`

## f7

Current preparation checks trusted metadata, decodes payloads, rejects unsupported versions and retains proposals before presentation; its handler leaves every preparation error unconsumed.

Evidence:

- `internal/turn/publication_recovery.go:114`
- `internal/daemon/mcp_creation.go:46`

## f8

Current approval admission checks freshness and current proposal before looking up prior source-comment intent, allowing retry after failed consumption to encounter changed eligibility first.

Evidence:

- `internal/store/mcp_creation.go:153`
- `internal/store/mcp_creation.go:179`
