# Manifest
# Branch: lf-308-hans-wants-to-know-how-this-works-with
# Session: 2026-09-16

## Modes

- [ ] **Research** — `RESEARCH_BRIEF.md` *(incomplete — blocked on clarification)*
- [ ] **Plan** — `plan_brief.md` *(not started)*
- [ ] **Implement** — code + tests *(not started)*
- [ ] **Validate** — read-only review *(not started)*
- [ ] **Spike** — `SPIKE.md` *(not started)*

Modes do not follow on from one another. A box is ticked by a run that a human
asked for; nothing here licenses starting the next one.

## History

- 2026-09-16 — session created; research requested for issue #308, run 0001.
- 2026-09-16 — run 0001: repo-wide search found zero references to "pydev" or
  "on-prem" anywhere in source, docs, or `.aiteam` archives; issue #308 has no
  comments and no further disambiguation beyond its title. Documented the
  `internal/agent.Runner` host-extension seam and the render registry's
  `opencode` target as the closest architectural evidence, but neither
  confirms what "pydev" refers to. Research left incomplete; clarifying
  questions returned to the issue owner instead of guessing the target.

## Notes

- Headless run: resolve ambiguity through stated assumptions; ask by stopping
  only when material and unresolvable from the repo.
- Run 0001 stopped: "pydev" and "on-prem" are both unresolvable from repo
  evidence and the issue has no comments. See RESEARCH_BRIEF.md "Decisions →
  Open" for the three questions blocking productive research.
