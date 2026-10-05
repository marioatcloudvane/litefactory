# Research brief

## Request

Research removal of the old team CLI, installation and publishing setup while retaining all production internal/teamsource files and dependencies, inert database columns and untouched operator disk data.

## Decisions

- **d1** — Issue #489 supplies the reviewed scope replacing #324. Preserve every production internal/teamsource file and required dependencies until #317; do not apply old roadmap gates.
- **d2** — Owner decision reported in issue #489: external selections require explicit switching through re-registration; ordinary re-registration must not clear them silently. New runs must refuse with an actionable switch step.
- **d3** — Retain the three team-related database columns without a migration; never inspect, acquire or delete operator catalogs, caches, overrides or credentials.
- **d4** — Shaping decision: use repo add <path> --use-builtin for an explicit, idempotent switch and remove --team entirely.
- **d5** — Shaping decision: ordinary re-registration succeeds without catalog access, preserves external selection and warns that new runs remain refused.
- **d6** — Shaping decision: switching clears external_team only among legacy fields after successful registration validation while retaining repository identity and prior option defaults.
- **d7** — Shaping decision: refuse nonempty external selections across all supported new-run admissions and story-contract upgrade before team resolution, launch or upgrade remote effects.
- **d8** — Shaping decision: retire installation-marker detection from registration and leave retained legacy marker values inert without a migration or bulk rewrite.
- **d9** — Shaping decision: ignore both retired config keys with one value-free notice per load while retaining active-setting and YAML validation.
- **d10** — Shaping decision: preserve production built-in definitions and transitive consumers while removing only distribution-exclusive assets and unreachable installation code outside that package.
- **d11** — Shaping decision: remove all 27 teamsource tests and retain or relocate behavioral coverage at graph, result, rendering and harness boundaries without recreating obsolete installation checks.
- **d12** — Shaping decision: acceptance includes catalog-free fresh registration and execution, all-mode external refusal, atomic explicit switching, retired-config handling, disk-data preservation and make check.
- **d13** — Shaping decision: treat roughly 7,000 fewer Go lines as a measured reduction target rather than a reason to delete necessary protections.
- **d14** — Shaping decision: retain existing pinned run artifacts without changing their definition source or adding installed-catalog reads.


## Assumptions

- **a1** — Interpret 'fresh repository registers' as removal of the legacy aiteam presence prerequisite as well as the installed-catalog prerequisite. This follows the explicit fresh-registration outcome and removal of installation setup.
- **a2** — Both emitted legacy settings are retired; the proposed compatibility policy ignores any syntactically valid retired subtree, emits key-only notices once per load and retains strict validation for active settings.
- **a3** — Original inventory describes HEAD 680c4f2ba575a18b2f704a90f8b777079470724f; focused shaping reads were performed at c352b5a3b0fe3113c28663961ec3046a58fe6508, with no engineering tests or live execution probes.
- **a4** — The explicit switch spelling is a technical scope choice for owner review, based on the existing repo add re-registration surface and removal of the selector, rather than a new product decision.

## Research documents

- Technical findings: [RESEARCH&#95;TECHNICAL&#46;md](RESEARCH_TECHNICAL.md)
