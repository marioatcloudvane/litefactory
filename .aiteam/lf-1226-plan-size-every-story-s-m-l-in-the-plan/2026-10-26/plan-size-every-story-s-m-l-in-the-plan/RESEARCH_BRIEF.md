# Research brief

## Request

Give every Plan story a size (S, M or L) with a one-line reason, assigned by the plan-set agent, refining Research's S-01. Show it in the plan set and final review. Later stages scale depth by size while coverage and proof links stay required. New plans only, no backfill. The fast track (#1218) is out of scope.

## Decisions

- **S-01** — mode=change+agent-guidance, size=medium — extends an existing set contract with one optional field and rewrites several role and skill texts; no new infrastructure. (agent's call)
- **O-01** — The plan-set agent assigns every story in a new set a size of S, M or L with a one-line reason covering parts touched, risk, unknowns and code to retire or migrate. (owner said: "The plan-set agent assigns S / M / L to every story, with a one-line reason")
- **O-02** — Sizing starts from Research's S-01 and refines it per story without duplicating it. (owner said: "It starts from Research's S-01 sizing ... and refines it per story; it doesn't duplicate it.")
- **O-03** — Size and reason are visible next to each story in the plan set and in the final review. (owner said: "The size and reason are visible next to each story in the plan set and the final review.")
- **O-04** — Later stages pick depth by size: S gets few tasks and short criteria, M normal detail, L full detail. Coverage of agreed outcomes and proof links stay required at every size. (owner said: "an S story gets few tasks and short acceptance criteria, M normal detail, L full detail. Coverage of agreed outcomes and proof links stay required at every size.")
- **N-01** — The fast track (fewer stages for small work) is not part of this work; it is #1218. (owner said: "The fast track ... is #1218")
- **N-02** — No backfill and no sizing of old plans, including when they are next changed. (owner said: "New plans only. Existing plans without sizes keep working; no backfill.")
- **K-01** — Existing saved plans and sets without sizes must keep loading, keep their revision and keep working. (owner said: "Existing plans without sizes keep working")
- **K-02** — Set rules stay: 1 to 12 entries, unique ids and names, no cycles, only the owner promotes. (agent's call)


## Assumptions

- **a1** — Size is stored as two optional typed fields on the set entry (size: S, M or L; a short size reason), so the factory can validate them, show them in the owner table and pass them to later stages. This is preferred over writing size into the intent text, which cannot be validated. (agent's call)
- **a2** — A new set from the plan-set agent must carry a valid size and reason on every entry, and the factory refuses one without. Old saved sets stay valid because the fields are optional in saved state. (agent's call)
- **a3** — A changed size changes the set revision, which needs a new owner review, like any other change to an entry. Old sets keep their old revision. (agent's call)
- **a4** — Depth-by-size is done through the role texts for story, technical and acceptance, and through the size passed in the stage prompt. The factory enforces no task counts. Coverage and proof-link checks stay the same at every size. (agent's call)
- **a5** — The final review shows size because the acceptance role is told to show it next to each story. The factory table shows it in code. (agent's call)
- **a6** — Team changes are made in the base source and the 1.0.0 releases are regenerated with the release tool, with no version bump, as recent commits did. (agent's call)

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
