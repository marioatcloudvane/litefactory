# Research brief

## Request

Establish existing options for a short path for minor changes to existing behavior, including whether extending Spike is preferable to another workflow.

## Decisions

- **S-01** — mode=change, size=medium — the existing Spike and routing capabilities overlap the request, but their contracts and delivery boundaries need comparison across workflow, agent and runtime layers. (agent's call)
- **O-01** — Establish a lower-overhead path for minor changes to existing behavior and compare extending Spike with other options before selecting an approach. (owner said: "often i just want to do a small change, not create a new feature" and "We could beef up the spike for that or do we have other options? what's best?")
- **O-02** — Proposed scope, pending q1: extend Spike to complete one clearly stated repair or intentional change to existing behavior without a story set or a full planning cycle. (agent's call)
- **O-03** — The proposed scope covers local behavior tweaks, internal renames, documentation and configuration edits that meet the same eligibility rules. (agent's call)
- **O-04** — Before editing, establish one outcome, affected files, impact, excluded change kinds and concrete verification, then measure the actual change against those same bounds afterward. (agent's call)
- **O-05** — Replace the universal defect-and-new-test requirement with a truthful change description and verification evidence, while retaining regression coverage for defects and behavior checks for executable changes. (agent's call)
- **O-06** — Documentation-only edits may use a recorded content review and applicable existing checks without a fabricated defect or test file, while configured focused and full phases remain required. (agent's call)
- **O-07** — Align automatic routing examples, Spike roles, result contracts, measured gates and owner documentation so they describe the same eligible work and outcomes. (agent's call)
- **O-08** — An unclear request gets at most the existing one question round, and an ineligible proposal stops before editing with reasons and advice to use Research for unresolved scope or Plan for already settled broader work. (agent's call)
- **O-09** — An actual change that exceeds the bounds is reported as oversized and unfinished through the existing hand-back path, without automatic mode switching or further repair. (agent's call)
- **O-10** — Success must distinguish verified completion, unavailable checks, failed checks and exceeded scope, with at most the existing one repair after failed checks. (agent's call)
- **N-01** — Creating a new product feature, multiple independent changes, new packages, public API changes and schema migrations are outside the proposed short path. (agent's call)
- **N-02** — Redesigning Research, Plan, Implement, Validate or Bug, adding a PR or merge pipeline, and automatic transfer into another mode are outside this change. (agent's call)
- **N-03** — Rewriting retained sessions, migrating historical results and changing already captured teams are outside this change. (agent's call)
- **K-01** — Keep existing Spike commands and labels, read-only investigation, write access only after eligibility, and the existing permission ceiling. (agent's call)
- **K-02** — Keep the existing no-pre-edit-approval behavior and factory inspection/commit boundary, with no new promise of PR creation, merge or delivery. (agent's call)
- **K-03** — Preserve defect repairs and investigation-only answers, and keep old graph captures and retained result formats usable under their original contracts. (agent's call)
- **K-04** — Keep refusal of retired or ambiguous legacy Spike sessions and require normal team release/capture adoption for the new behavior. (agent's call)
- **O-11** — Acceptance must cover a bounded repair, an intentional behavior change, a documentation-only edit, rejected risky or oversized work, post-edit scope growth, unavailable checks and replay of old graph results. (agent's call)


## Assumptions

- **AS-01** — The request includes intentional behavior changes, documentation/configuration edits and similar tweaks, rather than only repairs of defects. The owner explicitly contrasts minor changes to existing behavior with creating a feature. (agent's call)
- **AS-02** — Extending Spike is the recommended scope in q1 because its measured gate, checks and bounded repair already provide the short path; the owner has not yet selected this approach. (agent's call)
- **AS-03** — File count and dependency eligibility await q2/q3; verification, unchanged approval/delivery boundaries and hand-back behavior are explicitly proposed in O-04 through O-10 and K-01 through K-04. (agent's call)
- **AS-04** — The proposed five-file ceiling accommodates implementation, tests and documentation but is a policy recommendation, not a measured predictor of low risk; one outcome and impact exclusions remain mandatory. (agent's call)
- **AS-05** — No business-data migration is needed for this workflow change; the relevant data surface is retained Spike proposals, fixer reports and captured workflow contracts, which must remain compatible rather than be rewritten. (agent's call)


## Open questions

### q1 — Should the short path extend Spike or have its own workflow?

_Asked of: Product Owner Questions_

- **a** — Extend Spike (recommended): accept one clear small change as well as defects and narrow investigations; reuse its checks, one repair, no pre-edit approval and current factory delivery boundary.  *(recommended)*
- **b** — Create a separate small-change workflow: keep Spike's defect-only editing contract, but add a separately maintained route with the same proposed bounds and delivery rules.
- **c** — Use a lighter Plan: keep explicit planning approval before editing, accepting more steps and a broader change to Plan.

### q2 — How many changed files should one short-path change allow?

_Asked of: Product Owner Questions_

- **a** — Up to five (recommended): enough for a small implementation, tests and documentation; one outcome and all impact exclusions still apply, and tests, docs and generated files count except current session artifacts.  *(recommended)*
- **b** — Keep three: preserve the original strict ceiling, counting the same files; more otherwise simple requests will need a longer path.

### q3 — Should dependency and toolchain updates be included in the first version?

_Asked of: Product Owner Questions_

- **a** — Exclude them (recommended): keep manifest, lockfile and toolchain changes outside the short path and correct routing examples; a small diff does not bound their effect.  *(recommended)*
- **b** — Allow only patch updates to existing dependencies: require no additions, removals, toolchain changes or migration, count lockfile edits and run the configured checks; this adds an eligibility rule beyond file count.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
