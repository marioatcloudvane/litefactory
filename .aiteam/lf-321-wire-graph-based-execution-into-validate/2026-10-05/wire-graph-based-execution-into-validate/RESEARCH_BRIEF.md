# Research brief

## Request

Shape graph-based Validate execution from retained findings, settling its goal, boundaries, evidence model, interfaces, permissions, validation and recovery rules.

## Decisions

- **d1** — This pass shapes Research without implementing, creating issues now or selecting another mode.
- **d2** — Owner feedback permits configurable automatic Validate entry and a separate minor-repair role while preserving read-only review agents and independent delivery approval.
- **d3** — Owner selects three parallel agents for acceptance, code review and architecture review against the final tested snapshot.
- **d4** — Owner permits minor repairs in Validate and requires larger repairs to stop with explicit Implement handback.
- **d5** — Owner removes the full-suite obligation from graph-based Implement and assigns it to Validate.
- **d6** — The proposed retained contracts cover command recipes, snapshot identity, repair attempts, review findings, owner choices and independently recovered external effects.
- **d7** — Failed or missing full-suite evidence blocks acceptance and delivery while permitting useful static findings.
- **d8** — Owner selects final issue-creation or delivery choices, with exact-head authorization and closure limited to the displayed accepted cohort.
- **d9** — Framework execution is language neutral and does not depend on failure parsers.
- **d10** — Owner settles q2 as serial full-suite tests rather than exclusive slots or recurring windows.
- **d11** — Proposed repair handback creates a linked repair execution rather than rewinding a completed graph.
- **d12** — Proposed live reporting shows per-stage status, criterion progress, stable findings and owner options in one updated report.


## Assumptions

- **a1** — Drift remains code, architecture and approved-plan compliance.
- **a2** — Remote execution remains unresolved in q1 after explaining its distributed execution and evidence obligations.
- **a3** — Serial full-suite tests apply per factory daemon and do not guarantee CPU or memory isolation.
- **a4** — Prior source and historical evidence remain bounded to the inspected revisions and sampled runs; current graph-based Implement reentry is not established.
- **a5** — Serialized field names and storage design remain for Plan while this scope fixes required information and observable behavior.
- **a6** — Propose manual automatic-entry default, with opt-in configuration as standing owner authorization, because the owner requested a setting without specifying its default.
- **a7** — Interpret approval agent as acceptance review rather than permission to merge because the owner separately specifies an end-of-workflow delivery choice.
- **a8** — Interpret close all issues as the displayed accepted workflow cohort, excluding unrelated or unresolved issues.


## Open questions

### q1 — Should remote test-server execution be included in this release?

_Asked of: Product Owner Questions_

- **local** — Local first (recommended): keep the framework-neutral contract and defer authenticated remote runners, artifact transfer and distributed recovery to a separate scope.  *(recommended)*
- **remote** — Include remote execution now: build and verify the runner, host/environment selection, credentials, snapshot transfer, logs, cancellation and recovery contract described above.

### q3 — What changes count as minor repairs inside Validate?

_Asked of: Product Owner Questions_

- **bounded_source** — Small source or test corrections within approved behavior (recommended): at most two repair rounds, each with a 15-minute agent budget; no public API, schema, dependency, architecture or criteria changes, and no weakened assertions; rerun the full suite after each repair and hand back any uncertain or larger fix.  *(recommended)*
- **mechanical** — Mechanical changes only: formatting, imports and obvious compilation or test-fixture mistakes, with the same two-round and time limits; send behavior fixes to Implement.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
