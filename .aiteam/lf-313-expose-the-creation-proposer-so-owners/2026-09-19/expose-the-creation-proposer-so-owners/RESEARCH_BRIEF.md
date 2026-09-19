# Research brief

## Request

Verify the claim in issue #313: story creation via /mcp-create is unusable because only a hand-authored-JSON owner comment can ever create a retained proposal, while the agent-side proposer (Prepare) is unreachable. Establish current behavior, what exists, and what shaping needs to decide.

## Decisions

_No decisions have been settled yet._

## Assumptions

- **a1** — The three open questions the issue itself raises (random vs. deterministic nonce for the agent path, whether the 48000-byte presentation cap should apply when the factory posts its own proposal, and how or whether to add a mode label to created issues) are design choices for shaping to decide, not facts to settle here. Findings f5 and f6 lay out the concrete code-level tradeoff each one touches.
