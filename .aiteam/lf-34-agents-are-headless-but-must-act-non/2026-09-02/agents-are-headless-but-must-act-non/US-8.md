# US-8 — Go Architect escalates major deviations without personal naming

> As a feature owner, I want major plan deviations surfaced neutrally so architectural review does not silently reshape approved scope.

This aligns both Go Architect definitions and removes the hardcoded decision-maker name.

## Why This Matters
Architectural judgment is autonomous, but materially reshaping an approved plan needs an owner decision. Neutral role language keeps reusable instructions correct.

## Architecture Notes
- Major means contradicting/materially reshaping the approved spec/plan or needing an unassumable standards deviation.
- Return conflict, impact, and options without waiting; minor judgment remains normal guidance.
- Replace the personal name with `owner` or `human` in both examples.
- Risk: a broad threshold turns review into general questioning.

## Tasks
| ID | Title | Type | Depends On | Est |
|----|-------|------|------------|-----|
| T-8.1 | Add paired major-deviation handoff and neutral ownership language | logic | — | S |
| T-8.2 | Validate threshold, autonomy, name removal, and wrappers | test | T-8.1 | XS |

## Acceptance Criteria
| ID | Criterion |
|----|-----------|
| AC-8.1 | Both definitions share the same major-deviation threshold and return path. |
| AC-8.2 | Routine judgment stays autonomous and neither definition waits. |
| AC-8.3 | No hardcoded `mario` remains in current agent/skill instruction content. |
| AC-8.4 | Tool-specific wrappers remain intact. |
