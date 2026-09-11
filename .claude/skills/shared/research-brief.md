# RESEARCH_BRIEF.md — structure and depth

Read this at the synthesis step, when the specialists have reported and you are
about to write the brief. It is the **technical** artifact: written for Plan
mode, engineers and the architect. The business-facing report is a separate
register with its own procedure —
`.claude/skills/shared/research-report-entry.md`.

## Structure

```
# Research Brief — <feature-tag>

## Request
<what the user asked for, verbatim or lightly paraphrased>

## Codebase Context
<summary of findings from the codebase tour, or "N/A — greenfield">

## Domain / Problem Space
<what was learned about the problem being solved, the domain, or the user need>

## Constraints & Risks
<technical constraints, unknowns, risks surfaced during research>

## Open Questions
<unresolved questions Plan mode must address — see
 `.claude/skills/shared/research-open-questions.md` for what each entry carries>

## Recommended scope for Plan mode
<a brief statement of what Plan mode should and should not cover>
```

## Depth

Write at **full technical depth**. The brief keeps:

- **Provenance tagging** — `[V]` verified this run / `[O]` reported by the owner /
  `[W]` second-hand, unverified / `[R0]` carried forward from an earlier run.
- **`path:line` citations** for every claim about this codebase, and source URLs
  with versions and dates for every claim about anything outside it.
- **Section depth** — the awkward, detailed parts belong here precisely because
  the comment cannot carry them.

There is **no size limit on the brief.** The existence of a short business report
is not a reason to write less here; it is what makes writing more here
affordable.

## Fidelity to the specialists

Synthesise what the specialists returned. Do not improve on it, and do not fill a
gap they reported with something you recall. Where a specialist said a thing was
uncertain, the brief says it is uncertain. Where a specialist said it saw more
than it returned, the brief says the area is larger than the findings suggest.
