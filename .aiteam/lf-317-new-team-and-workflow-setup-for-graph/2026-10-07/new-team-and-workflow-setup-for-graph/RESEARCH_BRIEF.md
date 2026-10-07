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
- **d10** — Proposed scope: selected repository definitions supply the rights catalogue without expanding Implement, waiver or merge authority.
- **d11** — Proposed scope: retain executable engine contract coverage and remove standard-bundle editorial prose requirements from core engine tests.
- **d12** — Proposed scope: existing execution paths remain supported until separate retirement work.
- **d13** — Proposed scope: success requires version-separated repository execution, snapshot-safe recovery, failure-safe management, provider delivery and permitted delegated replies.


## Assumptions

- **a1** — Interpret a new stage in two senses: a graph node/role within an existing workflow, and a new top-level execution identity. The code supports the former under existing contracts and rejects the latter.
- **a2** — Treat the named GitHub URL and teams.json/team.config layouts as proposed interfaces, not verified published resources. A web fetch of the supplied URL failed with cache miss.
- **a3** — Proposed immutable installed releases and captured active instances follow the owner's version separation and rollout boundary; repository updates require explicit repinning.
- **a4** — The local operator remains authorized to manage sources and repository bindings under existing administration controls; package installation does not confer runtime rights.
- **a5** — Catalogue retrieval uses HTTPS and content digests with confined package reads; signatures, trust marketplaces and automatic merges are not required for the first release.


## Open questions

### q1 — Which team sources should the first release support?

_Asked of: Product Owner Questions_

- **standard** — Recommended: ship only the standard team through one configured HTTPS source, while making source identity part of the package model so other catalogues can be added later.  *(recommended)*
- **custom** — Allow operator-configured HTTPS catalogues and third-party teams immediately, requiring the same package validation and an explicit trust decision at installation.

### q2 — How should users customize an installed team?

_Asked of: Product Owner Questions_

- **copy** — Recommended: create a separate local editable team copy with its own identity, recorded base release and effective digest; link explicitly, and offer comparison with upstream without automatically merging edits.  *(recommended)*
- **overlay** — Use repository-specific replacement files over an immutable release, record each override and block upgrades until conflicts are resolved; this adds an overlay and conflict model.

### q3 — What should happen when a linked team does not define the requested mode?

_Asked of: Product Owner Questions_

- **reject** — Recommended: reject the request with the missing mode and available modes; never silently mix teams, and allow an explicit unlink to restore the built-in default.  *(recommended)*
- **fallback** — Use the built-in definition for missing modes and show that fallback during inspection and admission; a repository may then run workflows from two teams.

### q4 — How much delegated decision support belongs in this issue?

_Asked of: Product Owner Questions_

- **one** — Recommended: replace the rights catalogue and deliver one tagged Plan stage approval end to end, including exact active grants, current-review validation and ignored unauthorized replies; defer Research delegation, remaining Plan reviews and Validate sign-off.  *(recommended)*
- **all** — Include all described Research and Plan delegated actions plus Validate sign-off; this also requires settling Validate's missing graph policy and broadens this issue substantially.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
