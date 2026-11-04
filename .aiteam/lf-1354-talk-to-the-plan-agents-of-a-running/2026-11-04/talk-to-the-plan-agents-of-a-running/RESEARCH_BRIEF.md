# Research brief

## Request

Check #1354 against current implementation and explain its impact on Research, Bug, Plan and connector workflows.

## Decisions

- **S-01** — mode=feature, size=medium — proposed connector capability crosses Plan target selection, advisory isolation and owner gates, but current implementation already covers its core. (agent's call)
- **A-01** — Owner request — owner: "check this. and tell me how this impacts the workflows!" (owner said: issue comment 6625055491)
- **O-01** — Establish current Plan advisory behavior and workflow impacts before shaping any remaining change. (agent's call)
- **N-01** — Do not authorize Plan approvals or candidate edits through private advisory answers; the supplied draft explicitly excludes conversational approval. (agent's call)
- **K-01** — Preserve Research and Bug advisory behavior and Plan owner gates when considering any remaining extension. (agent's call)
- **O-02** — Proposed pending q1=a: an authorized owner can ask for rationale or independently judge the saved current Plan review through the existing connector operations without advancing Plan. (agent's call)
- **O-03** — Proposed pending q1=a: verification must cover parent story-set and assembled review plus story-issue story, technical and acceptance review through production snapshot capture and advisory execution with captured stage guidance. (agent's call)
- **O-04** — Proposed pending q1=a: acceptance must establish unchanged candidate, stage, approval and wait after ask, judge and follow-up, and correct stale-basis reporting when Plan advances. (agent's call)
- **O-05** — Proposed pending q1=a: verification must cover unavailable targets for between-stage stories, parent intervals after set promotion, holds, pending effects, retired stories, unsealed parallel groups and missing or mismatched captured resources. (agent's call)
- **O-06** — Proposed pending q1=a: connector usage guidance must make parent versus story targeting, current-stage selection, refusal boundaries and the existing approval route clear. (agent's call)
- **O-07** — Proposed pending q1=a: observed ask and judge journeys in both ChatGPT and Claude on an authorized test installation must record client and server versions, captured target, returned result and unchanged Plan state before claiming hosted compatibility. (agent's call)
- **K-02** — Retain automatic current-target selection, no previous-stage fallback, and the same target eligibility for Plan asking and judging without an extra content threshold. (agent's call)
- **K-03** — Retain dedicated isolated advisory execution using saved stage guidance rather than resuming a live Plan graph agent. (agent's call)
- **K-04** — Retain existing repository-scoped task:advisory consent, request recovery, result access checks, freshness reporting and separately consented summary publication. (agent's call)
- **K-05** — Retain discussion_open as conversation registration that neither selects an eligible review nor starts advisory execution. (agent's call)
- **N-02** — Exclude role selection, historical-stage access, between-stage fallback and expanded access during unsealed parallel work unless the owner selects a new behavior scope. (agent's call)
- **N-03** — Exclude new connector operations, data models, migrations, permission grants and changes to in-graph judges or issue-thread discussion behavior. (agent's call)
- **N-04** — Exclude deployment, production probes and captured-team migrations from the proposed verification scope. (agent's call)
- **A-02** — Shaping continuation — owner: "Continue the workflow at its shape stage." (owner said)


## Assumptions

- **AS-01** — Current checked-out commit is the implementation baseline; the deployed server version may differ and was not observed. (agent's call)
- **AS-02** — Treat between-stage fallback, wider parallel eligibility, and further judgment requirements as shaping choices, not missing owner evidence that blocks factual research. (agent's call)
- **AS-03** — No implementation is authorized by this research pass; the issue draft is context, not settled owner scope. (agent's call)
- **AS-04** — O-02 through O-07 are a concrete proposed scope pending q1, not owner-approved work; selecting existing evidence instead removes those additional obligations through explicit supersession. (agent's call)
- **AS-05** — The inspected shaping baseline is 45de2d5b265d082bec98f673ba58afe165b1d27d; earlier retained findings and test results describe the preceding research pass and were not rerun here. (agent's call)
- **AS-06** — Any defect discovered by the proposed verification needs its own evidence and bounded scope decision; this proposal does not authorize arbitrary runtime repair. (agent's call)


## Open questions

### q1 — What remaining outcome should this issue require now that Plan advisory support exists?

_Asked of: Product Owner Questions_

- **a** — Verification and usage clarity (recommended): retain current behavior; require proof through the production saved-state capture and advisory path for all five Plan contexts, plus observed ask/judge journeys in both ChatGPT and Claude on a test installation; clarify parent versus story targeting and refusals. This establishes usable delivery without redesigning Plan.  *(recommended)*
- **b** — Existing evidence is sufficient: retain the research findings and workflow explanation as this issue's outcome, with no further engineering or live-client verification; deployed compatibility remains unverified.
- **c** — Extend behavior: identify which limitation must change—between-stage access, access during unsealed parallel work, or choosing a role yourself—so that extension can be scoped before any build.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
