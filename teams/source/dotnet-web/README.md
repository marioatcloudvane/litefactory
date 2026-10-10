# dotnet-web source candidate

This unfinished source is not registered or released. Do not install or advertise it.
The profile currently contains only a fail-closed repository preflight recipe.
Its sentinel arguments require explicit repository overrides; no solution is discovered
or chosen automatically. The SDK pin is intentionally unset until verified against
release-time Microsoft metadata. No .NET application execution is claimed.

## Two solutions to the predecessor findings

The team-owned solution uses bounded shell argv in the existing profile contract.
A preflight recipe takes an exact SDK, repository-relative build target, explicit
test project, runner declaration and locked-restore declaration. Run it before any
restore/build/format/test recipe. Retain its bytes with the effective profile;
repository overrides replace complete recipes, never global factory settings.
Complete this candidate by validating actual test-project metadata and runner
selection, constructing locked/unlocked restore and runner-specific test commands,
and testing every refusal with recording executors. A shell process starting is
preflight evidence, not a restore/build/test start. Overrides must retain equivalent
refusal behavior. Do not install SDKs, workloads or tools automatically.

The shared-contract alternative belongs to the prerequisite, outside this story:
add neutral version-probe assertions and explicit required repository input paths,
then validate them before the affected command batch. Capture those declarations
with the effective profile and exercise incompatible host versions, absent targets,
missing tests and changed overrides with recording executors. This would let teams
supply values without their own preflight recipes. It is a proposal, not implemented
shared capability or approval to widen this story.

Remaining: six neutral workflow/resource mappings, complete recipes and their
failure tests, verified SDK/runtime/tool compatibility pins, Blazor guidance,
immutable generation and catalogue entry, exact-selection/capture tests, complete
affected-package tests, formatting/lint and retained engineering evidence.
