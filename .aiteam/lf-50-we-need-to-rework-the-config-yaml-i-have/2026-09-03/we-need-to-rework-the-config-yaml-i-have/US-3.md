# US-3 — Unified agent hierarchy

> As an operator, I want every coding agent configured in one place so that Claude and Codex follow a predictable structure.

This story makes both supported agents fully typed members of the canonical agents section.

## Why This Matters

Claude currently has a working legacy location and a newer location whose values are ignored. Consolidation removes that dangerous ambiguity while preserving each agent's distinct execution and safety controls.

## Architecture Notes

- Keep separate typed Claude and Codex settings beneath the shared hierarchy; common placement does not imply identical fields.
- Platform: runner construction remains the sole startup registration boundary, executable resolution stays in configuration ownership, and relocated strings remain subject to CFG-4 and SEC-1.
- Risk: no configured-agent failure may fall back to another agent or hidden Claude defaults.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-3.1 | Define fully typed canonical Claude and Codex agent schemas | data | T-1.1 | L |
| T-3.2 | Rewire Claude and Codex runner construction to canonical entries | logic | T-3.1 | M |
| T-3.3 | Cover single-agent, dual-agent, unsupported-agent, and runner wiring cases | test | T-3.1, T-3.2 | L |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-3.1 | Claude and Codex are configured only beneath `agents`, using fully typed agent-specific settings. |
| AC-3.2 | One or both supported agents may be configured; zero agents fails startup. |
| AC-3.3 | Legacy top-level Claude and unsupported agent keys fail with dotted replacement/error paths. |
| AC-3.4 | Existing executable, timeout, sandbox, network, writable-root, environment, and user-config safety behavior is retained. |
| AC-3.5 | A configured agent that cannot be resolved fails startup without fallback to another runner. |

