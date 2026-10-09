# Research brief

## Request

Research what exists today for a Bug-only babysit mode (issue #530): triggering, approval authority, agent and tier selection, rights, load.

## Decisions

- **D1** — Scope stays Bug workflow only. Settled by the owner comment of 2026-10-08.


## Assumptions

- **A1** — Babysit approval will be a new daemon-authenticated path tied to one analysis revision, not an owner-impersonating comment. Rests on F1, F2 and the issue text 'never as the owner's identity'.
- **A2** — Load-flake guidance and queueing build on the same watch loop as the challenger. Rests on the issue text and the earlier story dependencies.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
