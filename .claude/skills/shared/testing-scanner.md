# Testing Scanner

A procedure for auditing the test quality and coverage patterns of a codebase or module. Read this file and execute the procedure inline — do not spawn a sub-agent.

## Rulebook

Before scanning, look for a `testing:` section in the rulebook passed by the caller. If no rulebook exists or it has no `testing:` section, apply the generic principles at the bottom of this file and note that in your findings.

## What to analyse

Evaluate each area below and produce a finding: ✅ **compliant**, ⚠️ **concern**, or ❌ **violation**, with a one-line explanation.

### Coverage shape
- Do tests exist for the area in scope? Are they unit, integration, or end-to-end?
- Are the critical paths (business logic, error paths, edge cases) covered?
- Are there large areas of untested code — not just a missing line here and there, but whole concerns with no tests?

### Test isolation
- Do unit tests run without external dependencies (database, network, file system)?
- Are there tests that depend on external state or each other's execution order?
- Is test data set up and torn down within each test, or does it leak across tests?

### Test quality
- Do tests assert meaningful outcomes, or do they only verify that something was called?
- Are test names descriptive enough to understand what breaks when they fail?
- Are there tests that never fail (always pass regardless of the code)?

### Conventions
- Do tests follow a consistent structure (table-driven, Arrange/Act/Assert, or equivalent)?
- Are fakes/mocks used consistently with the team's established patterns?
- Are test files co-located with source or centralised — and is that consistent?

### Infrastructure
- Is the test setup (fixtures, factories, helpers) reusable, or is it duplicated across test files?
- Is there a clear way to run the full test suite and a subset (e.g. just unit tests, `go test -short`)?
- Do tests run in CI? Are there tests that are routinely skipped or marked as flaky?
- Is `go test -race` part of the standard run for anything with concurrency?

## Generic best practices (fallback when no rulebook)

- Unit tests do not touch the database, network, or file system
- Every exported function with non-trivial logic has at least one test
- Tests are deterministic — same code produces same result every run
- Test names describe the behaviour being tested, not the implementation
- Fakes/mocks are used at package boundaries, not inside business logic

## Output format

Produce a markdown section to be included in the caller's output:

```markdown
### Testing Findings

**Scope:** <what was scanned>
**Rulebook:** <used — `<path>` / not found — applied generic best practices>

| # | Area | Status | Finding |
|---|---|---|---|
| 1 | Coverage shape | ✅ | ... |
| 2 | Test isolation | ⚠️ | ... |
| 3 | Test quality | ❌ | ... |

**Summary:** <1–2 sentences on overall test health and the most important gap to address>
```
