# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

Chat requests are response-only and stateless: no resume, no tools, no files. Interpreter, narrator and checker each call this once.

Evidence:

- `internal/agent/chat.go:9`
- `internal/messaging/interpretation.go:31`

## f2

Session identity: Claude resumes by a litefactory-chosen session ID; Codex reports its thread ID after the first run and resumes with it. Refused resumes yield ErrSessionNotResumable and a cold rerun path exists.

Evidence:

- `internal/agent/session.go:3`
- `internal/agent/request.go:26`
- `internal/codex/runner.go:523`

## f3

The Codex chat path is closed: RunChat always returns ErrDoctorIsolationUnverified, so a persistent Codex chat agent has no working launch path today.

Evidence:

- `internal/codex/chat.go:18`

## f4

The factory already has typed MCP tool launch specs with exact permitted tool lists and a family dispatcher with receipts; a read-only chat tool set can reuse it.

Evidence:

- `internal/agent/mcp.go:19`
- `internal/turn/mcp_families.go:126`

## f5

Natural messages are stored per channel and processed in order one at a time under a step lock; finished message content and dialogue are purged after 24 hours. No persisted per-channel session table exists.

Evidence:

- `internal/store/chat_natural.go:83`
- `internal/store/chat_natural_dialogue.go:182`
- `internal/messaging/natural.go:39`

## f6

Existing handlers use fixed budgets (ask and feature 5 min, meter 30 s). The natural pipeline code is about 3000 lines across natural, interpretation, presentation and the presentation, response and ask subpackages; these are the removal candidates.

Evidence:

- `internal/messaging/ask.go:44`
- `internal/messaging/meter.go:80`
