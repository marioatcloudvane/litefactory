# Code Review
# Feature: we-need-to-rework-the-config-yaml-i-have
# Reviewed by: go-code-reviewer
# Date: 2026-09-03
# Status: CLEAN

---

## BLOCKING findings

> These must be resolved before testing proceeds. The orchestrator will route
> each finding back to the responsible engineer.

### CR-B-001: Validate's Claude profile is enforced as read-only — RESOLVED
**File:** `internal/config/policy.go:409`
**Story:** US-7 / Canonical profiles and Validate mode
**Category:** Security | Cross-story
**Resolution:** Claude Validate now excludes `Bash`, `Edit`, and `Write` from the load-bearing `--tools` surface and accepts only `Read`, `Glob`, `Grep`, and `Task`; its additive allow rules are limited to the three read tools. The generated canonical profile uses the same surface. Ambient Bash permissions can no longer widen the run because Bash is absent from the actual tool boundary. Codex retains bounded verification through its independently enforced read-only sandbox.

---

## ADVISORY findings

> These are improvement opportunities. Testing proceeds. Findings are added to
> notes.md for the drift report.

None.

---

## Cross-story summary

The final fix aligns configuration validation, generated defaults, and the two agents' native security boundaries. Claude Validate has a read-only tool surface with no shell or file-edit primitive, while Codex retains verification commands inside its read-only sandbox. CR-B-001 is resolved without introducing a fallback or weakening the other mode profiles.

---

## Verdict

CLEAN — CR-B-001 is resolved. Proceeding to environment setup.
