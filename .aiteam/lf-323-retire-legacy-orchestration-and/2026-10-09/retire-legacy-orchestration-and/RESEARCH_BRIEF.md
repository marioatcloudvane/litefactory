# Research brief

## Request

Shape the supplied cleanup findings into a buildable scope with explicit safety boundaries; resolve whether the owner wants bounded cleanup or the full retirement program.

## Decisions

- **d1** — This invocation shapes the Research result only and authorizes no implementation, issue creation, commit or push.
- **d2** — The proposed first build scope removes only individually verified unused helpers and corrects stale guidance.
- **d3** — Keep persisted data, engine and definition pins, active legacy routes, public interfaces and permission/result contracts unchanged.
- **d4** — Retain any candidate with uncertain callers, compatibility behavior or unavailable evidence and record why it was retained.
- **d5** — Preserve meaningful coverage on surviving behavior and require affected checks plus complete repository release checks before accepting cleanup.
- **d6** — Exclude the dependency-related Plan review notice defect from cleanup because it changes behavior and needs its own reproduction and acceptance rules.
- **d7** — Keep the parent retirement gate open after bounded cleanup until cohort, pending-effect, authentic five-mode provider, recovery, permission, result and rollback evidence is established.
- **d8** — Use the retained candidate findings as the bounded removal list rather than treating every legacy-looking package as removable.


## Assumptions

- **a1** — Treat the owner's statement that all paths use the new stack as adoption context, not proof that persisted legacy cohorts have drained; supplied source findings establish the compatibility boundary.
- **a2** — Retain behavior whose removal depends on installation cohorts or authentic provider evidence unavailable in the source investigation.
- **a3** — No-production-caller observations remain candidates until direct, indirect and registered action use and useful test coverage are checked.
- **a4** — No new data model, user interface or operator command is needed for the proposed behavior-preserving cleanup.


## Open questions

### q1 — Should the next build scope be bounded cleanup or full retirement?

_Asked of: Product Owner Questions_

- **cleanup** — Bounded cleanup first (recommended): remove verified unused helpers and correct guidance, preserve supported routes and data, and leave the parent retirement gate open.  *(recommended)*
- **retirement** — Full retirement program: include cohort inventory and drain or explicitly authorized migration, authentic five-mode Claude/Codex evidence, admission and rollback decisions, then gate engine removal on those results.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
