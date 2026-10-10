# Security Scanner

A procedure for auditing the security posture of a codebase or module. Read this file and execute the procedure inline — do not spawn a sub-agent.

## Rulebook

Before scanning, look for a `security:` section in the rulebook passed by the caller. If no rulebook exists or it has no `security:` section, apply the generic principles at the bottom of this file and note that in your findings.

## What to analyse

Evaluate each area below and produce a finding: ✅ **compliant**, ⚠️ **concern**, or ❌ **violation**, with a one-line explanation.

### Input validation
- Is all user-supplied input validated at system boundaries before use?
- Are there unvalidated inputs passed to queries, file paths, shell commands, or templates?
- Is input validation centralised (one place per boundary) or scattered?

### Authentication & authorisation
- Are authentication checks applied consistently — no routes or operations inadvertently left unprotected?
- Is authorisation (who can do what) enforced at the right layer, not just at the transport edge?
- Are session tokens, JWTs, or API keys handled and stored correctly?

### Secrets & configuration
- Are secrets (API keys, passwords, tokens, private keys) absent from source files and version control?
- Is secret loading from environment variables or a secrets manager, not hardcoded defaults?
- Are there commented-out credentials or debug keys left in code?

### Data handling
- Is sensitive data (PII, financial, health) identified and handled with appropriate care?
- Is sensitive data logged, or could it leak into logs or error messages?
- Are there direct database queries with unsanitised inputs (SQL injection risk)?

### Dependencies & supply chain
- Are third-party dependencies pinned to specific versions (`go.sum`)?
- Are there known-vulnerable packages in use (flag for manual checking — do not assume)?

### Error handling
- Do error responses expose internal details (stack traces, system paths, query strings) to callers?
- Are errors logged with enough context internally without leaking to external callers?

## Generic best practices (fallback when no rulebook)

Based on OWASP Top 10:
- Validate all input at system boundaries
- Use parameterised queries — never string-interpolate SQL
- Store secrets in environment variables, never in source
- Enforce least-privilege access on every operation
- Return minimal error detail to external callers

## Output format

Produce a markdown section to be included in the caller's output:

```markdown
### Security Findings

**Scope:** <what was scanned>
**Rulebook:** <used — `<path>` / not found — applied OWASP-based generic practices>

| # | Area | Status | Finding |
|---|---|---|---|
| 1 | Input validation | ✅ | ... |
| 2 | Auth & authz | ⚠️ | ... |
| 3 | Secrets | ❌ | ... |

**Summary:** <1–2 sentences on overall security posture and the highest-priority item to address>
```
