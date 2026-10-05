# Research brief

## Request

Shape a breaking removal of the old team CLI, installation, publishing, source configuration and legacy database storage, with no replacement selector or backward compatibility, while retaining shipped built-in agents and their active workflow delivery.

## Decisions

- **d1** — Owner scope revised: keep shipped built-in roles, skills, capture and delivery until #317, but remove installation-only production code even inside internal/teamsource.
- **d2** — Owner supersession: discard external selections as part of removal with no explicit switching requirement or compatibility refusal.
- **d3** — Owner supersession: remove legacy installation database objects and repository columns while leaving operator catalogs, caches, overrides and credentials untouched.
- **d4** — Owner decision: remove --team and add no --use-builtin or replacement selection interface.
- **d5** — Shaping decision revised: registration and re-registration have no team selection semantics and use built-in definitions.
- **d6** — Shaping decision revised: schema removal discards legacy team values while retaining repository identity, unrelated options and relational integrity.
- **d7** — Shaping decision revised: all supported new-run paths and story-contract upgrade use built-in definitions without installed-catalog resolution.
- **d8** — Shaping decision revised: remove installation-marker detection, model fields and repository columns rather than retaining inert values.
- **d9** — Owner breaking-change policy applied: remove retired team configuration fields entirely and let normal strict validation reject old keys without compatibility notices.
- **d10** — Shaping decision revised: remove installation-only APIs and dependencies throughout the tree while retaining shared functions required by built-in delivery or other live features.
- **d11** — Shaping decision revised: remove all 27 teamsource tests and relocate only uncovered protections of retained built-in or shared behavior, with no coverage obligation for removed installation behavior.
- **d12** — Shaping decision revised: acceptance covers fresh and upgraded schemas, built-in execution after legacy-selection removal, removed CLI and config rejection, intact retained delivery and make check.
- **d13** — Shaping decision: treat roughly 7,000 fewer Go lines as a measured reduction target rather than a reason to delete necessary protections.
- **d14** — Shaping decision revised: preserve historical run records and artifacts without converting or promising compatibility for old external-team runs.


## Assumptions

- **a1** — Interpret 'fresh repository registers' as removal of the legacy aiteam presence prerequisite as well as the installed-catalog prerequisite. This follows the explicit fresh-registration outcome and removal of installation setup.
- **a2** — The owner's no-backward-compatibility instruction supersedes ignored retired settings; old teams and settings.team_config keys should fail ordinary strict decoding.
- **a3** — Original inventory was at 680c4f2ba575a18b2f704a90f8b777079470724f, first shaping at c352b5a3b0fe3113c28663961ec3046a58fe6508, and this revision's focused reads at 6a14cb8c30c5b050002b5b2513032e151cf1e7f6; no engineering checks or live migration probes ran.
- **a4** — The latest request to remove everything old permits deletion of installation-only production code in teamsource, superseding blanket file preservation while retaining the built-in agent path.
- **a5** — Breaking removal does not imply automatic deletion of operator files or credentials or resetting unrelated database state, so those remain untouched.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
