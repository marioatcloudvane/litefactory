# Research brief

## Request

Establish the facts for #1218: a Plan-only fast track when Research S-01 is small or every story is S, preserving owner set promotion, final approval, Covers/Ruling notes and explicit return to normal planning if size grows.

## Decisions

- **S-01** — mode=feature, size=medium — Adds size-dependent Plan routing across graph control, retained approvals, team guidance and dependency-owned sizing; existing gate machinery limits the scope. (agent's call)
- **O-01** — Small work takes a shorter Plan path with fewer stages or owner stops, using #984's generic gate mechanism; eligibility is Research S-01 small or every story sized S by #1217. (owner said: issue #1218, What I want)
- **O-02** — If planning discovers work larger than S, return to the normal path and tell the owner. (owner said: issue #1218, What I want)
- **K-01** — Set promotion and final assembled plan approval remain owner decisions. (owner said: issue #1218 and retained #984 agreement)
- **K-02** — Stories retain Covers: O-… and any narrowing has a Ruling: line. (owner said: issue #1218)
- **N-01** — Sizing itself belongs to #1217; Implement and Validate are outside this work. (owner said: issue #1218, Not in this story)
- **O-03** — The owner can see fast-track eligibility and its reason at set review, any return to normal planning when it happens, and path history with graph-approved stages at final review. (agent's call)
- **O-04** — Fast-track progress survives resumes without an extra owner reply between eligible stages, while questions and guarded approval failures retain the existing review or failure behavior. (agent's call)
- **K-03** — Existing approval identity, upstream dependencies, holds, retirement review and change invalidation rules remain effective on both planning paths. (agent's call)
- **K-04** — Existing saved plans and captured team versions retain their behavior through the established release and capture process. (agent's call)
- **N-02** — New commands, manual fast-track overrides, automatic plan migration and changes to team release authority are outside this scope. (agent's call)
- **O-05** — The feature consumes the sizing contract supplied by #1217 without creating a competing size calculation or weakening normal planning when size evidence is unavailable. (agent's call)


## Assumptions

- **AS-01** — Proposed answer to q1: retain the existing stage responsibilities and outputs, shortening the path through fewer owner stops; combining stages remains an owner alternative until answered. (agent's call)
- **AS-02** — Proposed answers to q2/q3: explicit M/L overrides Research-small for the whole plan, unknown sizes qualify only via Research-small, a nonempty all-S current set can enable fast track, and a later growth fallback persists for the planning cycle without reopening unaffected approvals; these policies await the owner. (agent's call)
- **AS-03** — Previously captured teams retain their graph behavior; source changes apply through the established release/capture process. No automatic migration of saved plans or captures is assumed. (agent's call)
- **AS-04** — Eligibility uses the current proposed set at set review and the active promoted set thereafter, excludes retired stories and never treats an empty set as all-S; normal planning remains available without #1217 size fields, and no independent sizing logic is introduced. (agent's call)
- **AS-05** — Retained path evidence must identify the sizing basis and any growth fallback so a resumed run makes the same decision; exact storage shape and internal interface names belong to Plan. (agent's call)
- **AS-06** — Success requires observable automatic continuation through all three eligible story stages, owner-only set/final approval, correct unknown/mixed/growth routing, resume stability and unchanged Covers/Ruling reporting; these are scope acceptance boundaries, not executed checks. (agent's call)
- **AS-07** — No new permission role or external interface is needed: existing owner commands, stage reviews and assembled review expose the feature, with automatic approvals retaining their existing provenance. (agent's call)
- **AS-08** — Scope remains mode=feature and size=medium as recorded in S-01; retaining stages bounds the change, while merging stages would require reassessing that size. (agent's call)


## Open questions

### q1 — Should the fast track keep all planning stages and remove their routine owner stops?

_Asked of: Product Owner Questions_

- **a** — Recommended: retain story, technical and acceptance work, but advance automatically through guarded graph approval; keep owner set promotion, final approval and any question or safety review.  *(recommended)*
- **b** — Combine planning stages as well as removing stops; this reduces agent handoffs but expands the work into changing stage responsibilities and outputs.

### q2 — Should explicit M or L sizes override Research-small eligibility for the whole plan?

_Asked of: Product Owner Questions_

- **a** — Recommended: use the fast track when Research is small or a nonempty current set has every story explicitly S, provided no current story is M/L; unknown story sizes qualify only through Research-small, and absent or invalid size evidence otherwise takes the normal path.  *(recommended)*
- **b** — Use story sizes only after set promotion: every current story must explicitly be S, even when Research says small; unknown sizes always require normal planning.

### q3 — When work grows beyond S, should normal planning stay active without reopening unaffected approvals?

_Asked of: Product Owner Questions_

- **a** — Recommended: return the whole plan to normal owner gates for the rest of that planning cycle, retain unaffected approvals under existing change rules, and show the reason plus earlier graph approvals in final review; changed or uncertain approvals follow existing reopen and hold rules.  *(recommended)*
- **b** — Return to normal planning and require owner review of all earlier graph-approved stages; this adds retrospective stops before the final plan can be approved.
- **c** — Return to normal planning while M/L evidence remains, but allow automatic re-entry when all sizes become S again; this needs repeated path changes to remain clear across resumes.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
