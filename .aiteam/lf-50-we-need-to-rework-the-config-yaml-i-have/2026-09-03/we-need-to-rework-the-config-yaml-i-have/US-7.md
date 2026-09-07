# US-7 — Canonical profiles and Validate mode

> As an operator, I want one permission profile per process step and agent so that each workflow stage receives only the authority it needs.

This story introduces the complete top-level profile matrix and makes Validate a first-class run mode.

## Why This Matters

Research, planning, implementation, and validation have different permission needs. The hard part is expressing equivalent intent across agents with different security mechanisms while ensuring every configured combination is complete before work starts.

## Architecture Notes

- Define the profile matrix in `internal/config`, but retain agent-specific stanza types; equivalence is enforced through explicit invariants/tests rather than translation.
- Platform: the shared mode vocabulary belongs in `internal/agent`; preserve SEC-3/4, keep Validate out of question-enabled modes, and introduce no workflow artifact for it.
- Risk: Validate must be added to every closed mode list, parser, label, classifier prompt/output, request check, logging, and metadata surface without gaining source-edit authority.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-7.1 | Define and cross-validate the top-level canonical permission-profile matrix | data | T-3.1 | L |
| T-7.2 | Add Validate to the shared mode vocabulary and routing surfaces | logic | — | M |
| T-7.3 | Propagate canonical agent-specific permissions to Claude and Codex runners | logic | T-7.1, T-7.2 | L |
| T-7.4 | Cover profile completeness, least privilege, Validate routing, and runner boundaries | test | T-7.1, T-7.2, T-7.3 | L |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-7.1 | `tool_profiles.permissions` contains exactly `research`, `plan`, `implement`, and `validate`. |
| AC-7.2 | Every profile contains exactly every configured agent; missing, extra, or unconfigured-agent stanzas fail startup. |
| AC-7.3 | Claude profile tools remain within its non-empty global ceiling, and Codex profiles cannot widen agent-global roots, network, or user configuration. |
| AC-7.4 | Validate is accepted through explicit commands, labels, and classifier output; it persists and reports as Validate. |
| AC-7.5 | Validate cannot ask questions, cannot edit source, and can run only bounded project verification permitted by its agent stanza. |
| AC-7.6 | Research and Plan are read-oriented, Implement is write-capable, and both agents express equivalent intent through their native controls. |

