# US-3 — Classify unlabelled requests through the default agent

> As an issue owner, I want unlabelled instructions classified automatically so that I do not have to manage workflow labels for every run.

This story adds a constrained classification invocation with an exact four-mode result.

## Why This Matters

Labels are useful when intent is already clear, but requiring one on every
comment makes the workflow cumbersome. The fallback must be cheaper and safer
than launching the full work agent and hoping it reports what it decided. Its
hard part is keeping untrusted issue context out of control fields while making
the result strict enough for the daemon to trust.

## Architecture Notes

- Declare the classifier interface at the daemon consumption point and define a
  small request/result contract in the neutral agent layer; do not overload the
  ordinary runner contract.
- Implement classification in each configured agent integration using its
  existing binary resolution, timeout, stdin delivery, environment allowlist,
  event parsing, and logging primitives, with no task tools or writable
  worktree.
- Read bounded routing guidance from the repository base revision through
  `internal/git`; use a repository-neutral four-mode rubric when no routing file
  exists.
- Platform: no new subprocess package or generic forge abstraction; reuse
  implementation-owned measured CLI behavior.
- Risk: Codex and Claude emit different event shapes. Each parser must accept
  only its measured terminal-result channel and then apply the same exact mode
  parser.

## Tasks
| ID | Title | Type | Depends On | Est |
|----|-------|------|------------|-----|
| T3.1 | Define the classifier request, result, strict mode parser, and bounded prompt framing | Code | — | M |
| T3.2 | Add a read-only git gateway for bounded repository routing guidance | Code | T3.1 | S |
| T3.3 | Implement and measure Claude classification invocation | Code | T3.1, T3.2 | M |
| T3.4 | Implement and measure Codex classification invocation | Code | T3.1, T3.2 | M |
| T3.5 | Bind classifiers to the runner registry and selected default agent | Code | T3.3, T3.4 | S |
| T3.6 | Test hostile context, missing guidance, timeouts, nonzero exits, malformed output, and valid outputs | Test | T3.5 | M |

## Acceptance Criteria
| ID | Criterion |
|----|-----------|
| AC3.1 | Classification receives the current authorized instruction, framed issue/history context, and bounded repository routing guidance on stdin. |
| AC3.2 | Classification creates no task worktree, segment, ordinary run row, or persisted agent conversation. |
| AC3.3 | The invocation exposes no work tools and follows the selected implementation's environment, timeout, logging, and redaction policies. |
| AC3.4 | Only one exact canonical mode value is accepted; prose, empty, multiple, `infer`, and unknown values are rejected. |
| AC3.5 | Missing repository guidance uses the documented neutral rubric; unreadable or oversized guidance fails safely according to the bounded-read contract. |
| AC3.6 | Claude and Codex classifier behavior is covered against measured event fixtures rather than assumed output formats. |

