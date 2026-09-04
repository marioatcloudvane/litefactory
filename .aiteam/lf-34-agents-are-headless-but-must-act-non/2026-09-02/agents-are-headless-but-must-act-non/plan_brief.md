# Plan Brief — agents-are-headless-but-must-act-non
# Branch: lf/34-agents-are-headless-but-must-act-non
# Date: 2026-09-02
# Parent issue: https://github.com/marioatcloudvane/litefactory/issues/34
# Citation base: b43ff330dbcb3f0e2eb19d74dc35aef7c73282b0
# Research brief: RESEARCH_BRIEF.md

## Shared Decisions
- Treat each specialist's Claude definition and Codex skill copy as one atomic semantic pair while preserving their tool-specific frontmatter and Codex preamble.
- Shared behavior: no live respondent exists in-run; ordinary ambiguity gets a reported reasonable assumption; material uncertainty returns to the invoking orchestrator; only the run owner ends the run and hands a decision to litefactory; nobody waits or polls.
- Requirements Engineer is question-oriented; Implement is autonomous except for severe blockers; Proxy Product Owner escalates genuine scope uncertainty; Go Architect escalates major plan/spec deviations only.
- Spike remains only in `.claude/modes/spike.md`; Codex handles it inline, so no Codex Spike artifact is created.

## Workflow
- Shipping order: all five stories may run independently because each owns distinct files; cross-pair semantic review follows.
- Hard couplings: each Claude/Codex pair ships together; all Implement edits share one owner; Spike ships alone.

## Cross-Story Constraints
- Platform: definition prose only; no Go, harness, runtime prompt, routing, configuration, dependency, or API changes.
- Security: preserve production-target refusal, never solicit/reproduce secrets, and preserve SEC-1.
- Tests: targeted content checks, pair review, `gofmt -l .`, `go vet ./...`, `go test ./...`, then `make check`; never run `make build` or write `dist/`.
- Truthfulness: STYLE-1 applies; do not weaken policy/surface tests (TEST-10).
- Ring-fenced: prior `notes.md` drafts are not source of truth.

## Story Index
| ID | Title | Size | Depends On | Sub-issue |
|----|-------|------|-----------|-----------|
| US-2 | Requirements Engineer uses litefactory-mediated clarification | S | — | https://github.com/marioatcloudvane/litefactory/issues/42 |
| US-4 | Implement Orchestrator is autonomous and environment-driven | M | — | https://github.com/marioatcloudvane/litefactory/issues/44 |
| US-6 | Spike records a blocked-needs-decision outcome | XS | — | https://github.com/marioatcloudvane/litefactory/issues/46 |
| US-7 | Proxy Product Owner conditionally returns scope uncertainty | S | — | https://github.com/marioatcloudvane/litefactory/issues/47 |
| US-8 | Go Architect escalates major deviations without personal naming | S | — | https://github.com/marioatcloudvane/litefactory/issues/48 |

## Out of Scope
- Runtime asking behavior, Go/harness or permission changes, labels, architecture docs, generators, other agents, raw whole-file equality, and a dedicated Codex Spike artifact.

## Provenance
- Citation base: b43ff330dbcb3f0e2eb19d74dc35aef7c73282b0
- Known-stale areas: the earlier plan was Claude-only and split shared-file edits across nine stories; this plan supersedes it.
- Re-pinning requirements: confirm the four pairs still correspond and Codex still has no dedicated Spike counterpart before merge.
