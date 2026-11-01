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


## Assumptions

- **a3** — Interpret valid S/M using existing Plan sizing validity rather than the size letter alone; malformed or absent reason therefore falls back to flagship. The exact reuse boundary and route-version treatment remain for shaping (agent's call; rests on planStoryFacts).
- **a4** — Keep old unsized plans operable without backfill, routing them to flagship when they use the updated graph. Preserve retained capture/replay semantics; rollout behavior for already captured graphs remains a shaping point (agent's call).

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
