# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

Current CLI commands have one explicit registration list. It registers MCP serving, team administration, logs, doctor, rights and submission commands; it contains no standards/category management commands.

Evidence:

- `internal/cli/commands.go:8`

## f2

The existing MCP server is stdio and requires a live attempt descriptor. Launch registration carries immutable exact tool names, and family registration requires matching result bindings and permission material. This is a reusable integration boundary, not evidence of an existing general standards service.

Evidence:

- `internal/cli/cmd_mcp.go:14`
- `internal/agent/mcp.go:10`
- `internal/turn/mcp_families.go:53`

## f3

Factory MCP documentation lists result, issue-summary, comments, activity and mode-specific capabilities. Tool listing and invocation are both checked against authority. Issue reads are summaries rather than full bodies or comments, so the requested historical mining cannot assume the existing issue-summary surface supplies all needed evidence.

Evidence:

- `docs/guides/mcp.md:19`

## f4

Internal instruction packages already retain exact role/skill bytes, provenance, versions and digests before launch; replay restores retained content independently of current source. Corruption fails closed. This is a precedent for version retention, but these identities describe instructions rather than a standards catalogue.

Evidence:

- `internal/instructionpackage/doc.go:14`
- `internal/instructionpackage/doc.go:34`
- `internal/instructionpackage/capture.go:33`

## f5

Migration inputs exist in several forms: workflow role reporting instructions and result skills; project-specific litefactory rules; factory prompt framing and workflow guidance; internal operational advisor roles. Their responsibilities differ, so migration must inventory semantics rather than move all instructions wholesale. Current code explicitly leaves tools, modes, gates, schemas and execution authority outside instruction-package authority.

Evidence:

- `internal/teamsource/implement/story-worker.md:81`
- `internal/teamsource/research/skills/research-evidence/SKILL.md:8`
- `teams/go-development/v1.0.2/implement/skills/litefactory-rules/litefactory.md:1`
- `internal/harness/prompt.go:731`
- `internal/instructionpackage/doc.go:48`

## f6

Workflow capture already includes graphs, roles and transitive skills with bounded reads and resource hashes. Stack guidance captures profile-selected resource closures. The tree includes Python FastAPI and Swift iOS source profiles as well as Go; cross-stack preservation must keep this existing mechanism intact.

Evidence:

- `internal/teamsource/doc.go:1`
- `internal/teamsource/stack_guidance.go:18`
- `internal/teamsource/python-fastapi/stack-profile.json:1`
- `internal/teamsource/stack-profile.json:1`
- `internal/teamsource/swift-ios/stack-profile.json:1`

## f7

The doctor is an existing CLI-invoked diagnosis agent that reads live logs, bounded database results, git and issue comments. It cannot publish arbitrary changes: diagnosis proposes a fixed repair catalogue and later owner action controls repair. Its current role does not maintain standards; its invocation/capture and evidence approach are adjacent reuse candidates.

Evidence:

- `internal/cli/cmd_doctor.go:23`
- `internal/instructionpackage/embedded/agents/doctor-diagnosis.md:1`

## f8

The logs CLI exposes retained run JSONL, run selection, raw output, event filtering and follow behavior for provider streams. This establishes a local evidence source for log analysis, not an existing standards miner or a reliable automatic measure of unnecessary complexity.

Evidence:

- `internal/cli/cmd_logs.go:24`
- `internal/cli/cmd_logs.go:48`
