# RESEARCH_BRIEF.md — structure and depth

Read at synthesis, once the specialists have reported. This is the technical
artifact, for Plan mode, engineers and the architect. The business register is
separate: `.claude/skills/shared/research-report-entry.md`.

```
# Research Brief — <feature-tag>

## Request
<what the user asked for, verbatim or lightly paraphrased>

## Codebase Context
<findings from the codebase tour, or "N/A — greenfield">

## Domain / Problem Space
<the problem being solved, the domain, the user need>

## Constraints & Risks
<technical constraints, unknowns, risks surfaced during research>

## Open Questions
<see `.claude/skills/shared/research-open-questions.md` for what each entry carries>

## Recommended scope for Plan mode
<what Plan mode should and should not cover>
```

## Depth

- **Provenance tags** on every claim: `[V]` verified this run · `[O]`
  owner-reported · `[W]` second-hand, unverified · `[R0]` carried forward.
- **`path:line`** for anything about this codebase; **source URL, version and
  date** for anything outside it.
- **No size limit.** A short business report is what makes writing more here
  affordable, not a reason to write less.

## Fidelity

Synthesise what the specialists returned. Do not improve on it, and never fill a
reported gap from memory. Uncertain stays uncertain. If a specialist said it saw
more than it returned, say the area is larger than the findings suggest.
