# Research brief

## Request

Route the Implement story worker in DOT: valid S/M inherit the role tier (currently balanced); L and missing/invalid sizing use flagship. Keep size-to-tier policy out of Go, preserve provenance evidence, and exclude reviewer routing and mid-run resizing.

## Decisions

- **S-01** — mode=change+agent-guidance, size=medium — changes Implement graph routing, supporting factory fact/check boundaries and generated team definitions (agent's call).
- **K-01** — Routing lives in implement.dot; no size-to-tier policy in Go (owner said: "The routing belongs in the Implement graph (DOT), like Plan’s fast track. No size-to-tier policy in Go.").
- **A-01** — q1 “Which size routing should the story worker get, now that every role is on flagship?” — owner: "Following the judge: q1=a. Write out the full routing table: valid S and M stay on the role tier, L runs the worker on flagship, and a missing or invalid size also runs on flagship. Reviewers and mid-run resizing stay out of this version. Note: PR #1294 put story-worker back on balanced, so re-check the current role tier before shaping." (owner said: comment 6564476496).
- **O-01** — Full routing: valid S → role tier (currently balanced); valid M → role tier (currently balanced); L → flagship; missing size → flagship; invalid sizing → flagship. The explicit owner table governs over the older option-a wording (owner said: comment 6564476496).
- **N-01** — Reviewer routing and mid-run resizing are excluded (owner said: "Reviewers and mid-run resizing stay out of this version.").
- **O-02** — The shipped Implement graph validates; automated evidence shows selected worker tier/model in run provenance; manual acceptance follows an L story end to end (owner said: issue #1296 draft acceptance).
- **O-03** — Implement exposes authoritative saved size and sizing-validity facts to DOT and selects exactly one of two mutually exclusive worker paths, each with its own result source and existing node tier mechanism (agent's call).
- **O-04** — Sizing used for routing must belong to the worker's parent, story key and approved generation/revision, with unprovable associations treated as invalid rather than borrowing a newer plan's sizing (agent's call).
- **O-05** — Retain the routing facts for the implementation of that approved story so repairs, restarts and owner resumes use the same sizing basis (agent's call).
- **O-06** — Valid sizing means exact S/M/L plus a nonblank, valid UTF-8, single-line reason of at most 280 trimmed characters and, when present, the supported Plan route version (agent's call).
- **O-07** — A Plan fast-track fallback alone does not change a valid story's size or tier, while absent sizing and unsupported route versions take the flagship path (agent's call).
- **O-08** — Update the shared base graph and all four generated shipped team graphs together with their manifest so newly captured definitions consistently apply the routing table (agent's call).
- **O-09** — Automated acceptance covers S, M, L, missing and invalid sizing through graph selection and model delivery into retained provenance, including an overridden role tier proving S/M inheritance (agent's call).
- **O-10** — Acceptance also covers repair/restart stability, generation mismatch, old unsized plans, retained single-worker graph replay, selected-worker result lookup and capacity accounting, and rejection of branches that bypass synchronization or mix worker results (agent's call).
- **O-11** — The manual acceptance run uses a newly captured graph and an L story, checking the selected worker's flagship tier and resolved configured model through completion (agent's call).
- **K-02** — Preserve the current balanced role default and inherit future captured role defaults for valid S/M instead of hard-coding balanced in the graph (agent's call).
- **K-03** — Preserve synchronization before every worker attempt, the shared story repair budget, save and park behavior, and selected-worker capacity accounting regardless of branch (agent's call).
- **K-04** — Preserve worker role, standards, skill bindings, result contract, permissions and approval gates across both worker paths (agent's call).
- **K-05** — Existing captured graphs retain their captured routing and remain valid and resumable without recapture or rewriting history (agent's call).
- **K-06** — Preserve Plan sizing and routing behavior, other modes' condition boundaries and existing configured tier-to-model resolution (agent's call).
- **K-07** — Operational storage failures remain execution errors rather than being disguised as missing sizing or triggering a silent cheaper-model fallback (agent's call).
- **N-02** — Do not add dynamic single-node tier expressions, a general routing language, user configuration switches or model-provider changes (agent's call).
- **N-03** — Do not backfill old plans, migrate retained graph captures, change worker prompts or tune resolver/coordinator tiers (agent's call).
- **N-04** — Do not make cost savings or fewer repair rounds release criteria because this scope does not include a cost or quality benchmarking study (agent's call).
- **O-12** — Any retained sizing metadata needed to bind routing to approved work must be additive and optional for old records, which continue through the missing-sizing flagship route when using the new graph (agent's call).


## Assumptions

- **a3** — Use Plan's existing sizing validity including reason validation and supported route version when a route exists; missing route metadata alone does not invalidate otherwise valid sizing, and Plan fallback alone does not force flagship (agent's call; settled in O-06/O-07 from planStoryFacts).
- **a4** — Old unsized plans need no backfill; updated captures route them to flagship while retained old captures keep their original routing and resume semantics (agent's call; settled in K-05/O-12).

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
