# Architecture Scanner

A procedure for auditing the architectural health of a codebase or module. Read this file and execute the procedure inline — do not spawn a sub-agent.

## Rulebook

Before scanning, look for an `architecture:` section in the rulebook passed by the caller. If no rulebook exists or it has no `architecture:` section, apply the generic principles at the bottom of this file and note that in your findings.

## What to analyse

Evaluate each area below and produce a finding: ✅ **compliant**, ⚠️ **concern**, or ❌ **violation**, with a one-line explanation.

### Structure
- Is the directory and module layout consistent with the architecture implied or stated in the codebase (layered, hexagonal, flat, feature-sliced)?
- Are there files or modules that sit outside the expected structure without explanation?

### Boundaries
- Do modules import only from permitted layers (e.g. domain does not import from infrastructure)?
- Are there unexpected cross-boundary or cross-service imports?
- Is the public surface area of each module intentional and minimal?

### Abstractions
- Are key abstractions (services, repositories, handlers, use cases) identifiable and consistently shaped?
- Is business logic separated from infrastructure concerns (database, HTTP, file I/O, external APIs)?
- Are there god classes or modules with more than one clear responsibility?

### Patterns
- Does the code follow a consistent design pattern where one is implied (all services shaped the same, all repositories shaped the same)?
- Are there parallel implementations of the same concern — duplication at the design level, not just code level?

### Dependencies
- Is the dependency graph acyclic within the codebase?
- Are external dependencies (third-party libraries, services) isolated behind abstractions?
- Are there direct calls to infrastructure scattered throughout business logic?

## Generic best practices (fallback when no rulebook)

- Single responsibility per module
- Dependencies point inward: domain does not depend on infrastructure
- No circular imports
- Frameworks and libraries are adapters, not the core
- New code follows the shape of existing code in the same layer

## Output format

Produce a markdown section to be included in the caller's output:

```markdown
### Architecture Findings

**Scope:** <what was scanned>
**Rulebook:** <used — `<path>` / not found — applied generic best practices>

| # | Area | Status | Finding |
|---|---|---|---|
| 1 | Structure | ✅ | ... |
| 2 | Boundaries | ⚠️ | ... |
| 3 | Abstractions | ❌ | ... |

**Summary:** <1–2 sentences on overall architectural health and the most important thing to address>
```
