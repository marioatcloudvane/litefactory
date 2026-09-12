# Task Report: S-4 — Claude completion-message rules

Status: DONE (implementation; test execution delegated to the test specialist).

Frozen story: #250, issue identity 5427442485, revision
`029f46d4e9d07d64686388b0c59b1cdc7bbcccc8a891483b51171d48dc42d963`.
Dependencies: none.

## Files modified

- `.claude/agents/plan-orchestrator.md`: added the shared plain-language rule and ordered Plan completion rules before Exit; existing unresolved-question handling wins for early stops.
- `.claude/agents/implement-orchestrator.md`: added the shared plain-language rule and ordered Implement completion rules after Escalating; the existing escalation shape wins for early stops.

## Engineering self-check

Copied each existing Codex completion section verbatim by hand. An executed
Python section comparison passed for both Plan and Implement, and established
exact equality and exactly one Completion message heading in each file. Existing
Exit and Escalating content was preserved by insertion-only patches. No Go code,
tests, shared sources, or Codex instruction files were changed. No generator ran.
No Go checks or aggregate gates were run by this implementation engineer.

## Decisions and blockers

The explicit mirroring request applies to both mode sections, satisfying the full
frozen S-4 scope. No scope changes, unresolved questions, or blockers.
