# Research brief

## Request

Evaluate installable versioned team sources, catalogue and team.config, install/remove/update/list/link/unlink commands; identify embedded setup seams and Markdown tests; establish whether a one-file Fasttrack workflow and file-defined new stages work.

## Decisions

- **d1** — This invocation shapes the Research scope without implementation, issue creation or mode selection.
- **d2** — Teams install side by side and each repository links to an exact team version supplying its graphs and Markdown resources.
- **d3** — Proposed scope: catalogue metadata discovers releases while team.config authoritatively maps supported modes to workflow files.
- **d4** — Proposed scope: release identity includes source, slug, version and digest, and conflicting bytes for the same identity are rejected.
- **d5** — Proposed scope: update installs a new release without changing repository bindings and link explicitly repins future admissions.
- **d6** — Proposed scope: linked release removal is blocked and unlink restores the built-in default for future admissions.
- **d7** — Proposed scope: active instances always use their captured effective definitions during execution and recovery.
- **d8** — Proposed scope: package definitions may use existing modes and actions but cannot introduce new capabilities or cross-mode execution.
- **d9** — Proposed scope: validation and failed changes preserve the previous usable installation and binding.
- **d10** — Owner excludes rights management, rights invocation, rights-catalogue integration and review approval from this setup scope.
- **d11** — Proposed scope: retain executable engine contract coverage and remove standard-bundle editorial prose requirements from core engine tests.
- **d12** — Proposed scope: existing execution paths remain supported until separate retirement work.
- **d13** — Proposed success requires repository version separation, independent editable clones, missing-mode rejection, failure-safe management, immutable run recovery and supported provider delivery.
- **d14** — Owner selects configurable custom team sources, including a company-hosted location.
- **d15** — Owner selects a clone command specifying source team/version and new team name/version for an independent editable copy.
- **d16** — Owner selects rejection when the linked team lacks the requested mode.
- **d17** — Owner removes the requirement for one editable DOT definition per supported mode, so no workflow file count is prescribed.
- **d18** — Owner places invocation of rights and Plan story approval in separate work rather than team setup.


## Assumptions

- **a1** — Interpret a new stage in two senses: a graph node/role within an existing workflow, and a new top-level execution identity. The code supports the former under existing contracts and rejects the latter.
- **a2** — Treat the named GitHub URL and teams.json/team.config layouts as proposed interfaces, not verified published resources. A web fetch of the supplied URL failed with cache miss.
- **a3** — Proposed immutable installed releases and captured active instances follow the owner's version separation and rollout boundary; repository updates require explicit repinning.
- **a4** — The local operator remains authorized to manage sources and repository bindings under existing administration controls; package installation does not confer runtime rights.
- **a5** — Catalogue retrieval uses HTTPS and content digests with confined package reads; signatures, trust marketplaces and automatic merges are not required for the first release.
- **a6** — Propose company-hosted HTTPS catalogues using the common catalogue format and existing secret handling where authentication is needed, excluding new SSO integrations.
- **a7** — Propose clone collision rejection, recorded origin and per-admission validation of editable copies, while keeping each running snapshot immutable.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
