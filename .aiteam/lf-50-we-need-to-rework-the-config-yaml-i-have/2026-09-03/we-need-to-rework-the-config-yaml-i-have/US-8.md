# US-8 — Complete generated configuration

> As a new operator, I want initialization to produce the complete supported structure so that I can understand and safely edit every configuration area.

This story replaces the partial starter with a complete canonical file and updates startup presentation.

## Why This Matters

The generated file is currently the practical documentation but omits supported controls. A complete example must remain safe and runnable, derive from actual defaults, and explain semantic ordering without embedding credentials.

## Architecture Notes

- Continue rendering values from centralized defaults and decode the generated file through the production path; live YAML represents the canonical structure rather than commented dead placeholders.
- Platform: preserve CFG-3/4, TEST-10, thin command wiring, and init's non-overwrite behavior.
- Risk: the file must choose only one credential-reference form, remain complete for both agents/profiles, and avoid drifting from runtime defaults.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-8.1 | Replace the starter template with a complete canonical safe configuration | infra | T-1.1, T-2.1, T-3.1, T-5.1, T-7.1 | L |
| T-8.2 | Rewire initialization and startup summary to canonical paths and ordered default | logic | T-4.2, T-8.1 | M |
| T-8.3 | Cover generated-file validity, defaults equality, completeness, and startup output | test | T-8.1, T-8.2 | L |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-8.1 | Initialization emits the complete canonical hierarchy with both agents, every tier mapping, and all four profiles. |
| AC-8.2 | Generated values derive from runtime defaults and pass production decoding/validation after an appropriate credential reference is supplied. |
| AC-8.3 | The generated file contains references rather than credential values and never enables incompatible credential methods simultaneously. |
| AC-8.4 | Comments explain that agent order selects the default, model tiers resolve centrally, and modes select profiles automatically. |
| AC-8.5 | Initialization never rewrites an existing file, and startup reports the first configured agent as default. |
| AC-8.6 | Completeness tests fail when a live schema key or required semantic explanation is omitted. |

