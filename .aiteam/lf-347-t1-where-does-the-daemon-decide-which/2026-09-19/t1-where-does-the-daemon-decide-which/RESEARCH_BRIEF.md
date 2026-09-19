# Research brief

## Request

Identify where litefactory chooses the coding agent, which labels/configuration/session inputs it uses, and what happens when a label disagrees with the last session segment.

## Decisions

- **d1** — Scope is a bounded repository investigation for issue #347. Findings are the deliverable; no implementation or owner decision is required.


## Assumptions

- **a1** — Current behavior means the checked-out repository at revision 7d5799834b5031c84286080708aaff6091ea63ee. A running daemon may use another build; its configuration and runtime state were not inspected.
- **a2** — Evidence is static source and test inspection, not test execution or an authentic end-to-end graph observation. This handoff does not establish subsequent graph publication or acceptance.
