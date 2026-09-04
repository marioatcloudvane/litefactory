# US-1 — Router trusts the daemon-resolved mode header

> As a router agent reading a litefactory-generated prompt, I want to trust a mode the daemon has already resolved (shown in the prompt's `- Mode: X` header line) instead of re-deriving it from the request text, so that the daemon's mode resolution and my own behavior never silently diverge.

Adds a precondition branch to the top of `CLAUDE.md`'s `## Classify` section that trusts the daemon-resolved mode header when present and skips steps 1-3, while leaving today's classification behavior untouched whenever no resolved mode is present.

## Why This Matters
Right now, every run pays for a mode decision twice: the daemon already works out the right mode in code, then the router agent throws that answer away and works it out again from scratch by reading the request. Today the two answers happen to agree, so nothing looks broken — but nothing stops them from disagreeing in the future, and if they ever did, nobody would notice, because the two decisions never talk to each other. This closes that gap: when the daemon has already decided, the router trusts it and moves straight to the rest of the run instead of silently re-litigating a decision that has already been made. What's still open, and can only be checked by watching real runs rather than by a test, is whether an LLM router reliably follows a plain-English instruction like this one — there is no way to prove that in advance the way a compiler proves code.

## Architecture Notes
- The branch is a precondition with an explicit else, stated once, in one paragraph: header present and value is one of `research`/`plan`/`implement`/`spike` (case-insensitive) → that is the mode, skip Classify steps 1-3; header absent, empty, or `infer` → steps 1-3 run exactly as today; header present but unrecognized → steps 1-3 run and the final report notes an unrecognized mode header was seen and ignored.
- Placement: immediately under the `## Classify` heading, above the existing "For every new request, in this order:" line. An instruction placed after the numbered list competes with it instead of gating it.
- Must explicitly state that `## Gate` still runs for a header-trusted mode — this is the one clause in the whole change that carries security weight: skipping Gate would let a header-supplied `plan`/`implement` proceed without its prerequisite file (`RESEARCH_BRIEF.md`/`plan_brief.md`), which is the one way this change could become a privilege-escalation path rather than a redundancy fix.
- No hedging language anywhere ("unless it looks wrong," "unless the request clearly contradicts it," any instruction to compare header against request text) — a single such clause reinstates the inference this change deletes. If the request text plainly disagrees with the header, the header wins and the disagreement is reported, not acted on.
- `AGENTS.md` needs no edit: it already defers unconditionally to this section (`AGENTS.md:41-44`), and both Claude and Codex runs share one `harness.BuildPrompt` call (`internal/turn/prepare.go:226,241`), so it inherits the branch automatically. Confirmed by the architect against the code, not assumed — do not add text to `AGENTS.md` as part of this story.
- Platform: no Go code is touched by this story; `## Gate`, the numbered list, and the "sounds like" table in `CLAUDE.md` must remain byte-for-byte unchanged — insert only, never reflow neighbouring lines.
- Risk: whether an LLM router agent reliably honours a plain-English "skip steps 1-3" precondition is not something any test in this repository can verify — it is a property of prose plus model behavior, not of code. This risk cannot be closed by this story; the only mitigation is stating the branch as unambiguously as possible (precondition + explicit else, first in the section, no hedging) and observing behavior on subsequent runs on this branch.

## Tasks
| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-1 | Insert the mode-header precondition branch (trust / fallback / Gate-still-runs) at the top of `CLAUDE.md`'s `## Classify` section | logic | — | S |
| T-3a | Verify: `## Classify` steps 1-3 and the "sounds like" table are byte-identical to before the edit; `## Gate` is byte-identical; `AGENTS.md` has zero changed lines | test | T-1 | XS |

## Acceptance Criteria
| ID | Criterion |
|----|-----------|
| AC-1 | When the prompt's header states a resolved mode (one of `research`/`plan`/`implement`/`spike`, case-insensitive), `CLAUDE.md`'s Classify section is written to treat that mode as authoritative and skip steps 1-3. |
| AC-2 | When the header is absent, empty, or reads `infer`, Classify steps 1-3 run exactly as they do today — no wording change to that fallback path's meaning. |
| AC-3 | When the header carries a value outside the closed set (defensive case, not reachable by current code since `Request.Validate()` rejects an invalid mode before a turn executes), the branch falls back to steps 1-3 and instructs the agent to report the unrecognized value rather than invent a mode from it. |
| AC-4 | The branch explicitly states that `## Gate` still runs regardless of whether the header was trusted — a header-trusted `plan`/`implement` mode still fails Gate if its prerequisite session file is missing. |
| AC-5 | No wording anywhere instructs the agent to disregard the header if it "looks wrong" for the request, or to compare the header against the request text before trusting it. |
| AC-6 | `## Classify`'s existing numbered list (steps 1-3) and its "request sounds like" table are unchanged, byte-for-byte, apart from the new branch inserted above them. |
| AC-7 | `## Gate`'s text is unchanged, byte-for-byte. |
| AC-8 | `AGENTS.md` has zero changed lines. |
