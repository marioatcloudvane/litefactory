# Research brief

## Request

Research #473 for simplification of the shared Research/Plan graph core while preserving both modes' successful behavior; report current lines of code. No implementation or mode change.

## Decisions

- **d1** — The owner explicitly prioritizes simplification while preserving Research and Plan, which they report already work well.
- **d2** — This pass establishes current implementation facts and advisory simplification targets only. It does not choose a replacement design or remove approval/publication safeguards.


## Assumptions

- **a1** — Read 'both modes' as mode-specific code plus their shared infrastructure and supported legacy code, rather than two standalone packages. Use a disjoint reproducible filename/package inventory, with its limits stated.
- **a2** — Historical issue reports are owner-provided evidence, not reproduced incidents. No live daemon state or GitHub mutation was used; source inspection and inventory are the validation performed. No tests were run and no production code was changed.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
- Reference data: [code&#45;inventory&#46;csv](code-inventory.csv)
