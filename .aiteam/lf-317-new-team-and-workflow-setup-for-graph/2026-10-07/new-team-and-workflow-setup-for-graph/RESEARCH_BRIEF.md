# Research brief

## Request

Evaluate installable versioned team sources, catalogue and team.config, install/remove/update/list/link/unlink commands; identify embedded setup seams and Markdown tests; establish whether a one-file Fasttrack workflow and file-defined new stages work.

## Decisions

- **d1** — Owner requests Research only: evaluate and sketch, without implementation, child issue creation or mode selection.
- **d2** — Requested target includes side-by-side local team versions and repository-specific links, with graph and Markdown resources supplied by the installed team.


## Assumptions

- **a1** — Interpret a new stage in two senses: a graph node/role within an existing workflow, and a new top-level execution identity. The code supports the former under existing contracts and rejects the latter.
- **a2** — Treat the named GitHub URL and teams.json/team.config layouts as proposed interfaces, not verified published resources. A web fetch of the supplied URL failed with cache miss.
- **a3** — Treat installed versions as immutable inputs for new admissions and keep current captured definitions for existing instances; this follows the requested repository version separation and supplied rollout boundary. Exact update and customization policy belongs to shaping.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
