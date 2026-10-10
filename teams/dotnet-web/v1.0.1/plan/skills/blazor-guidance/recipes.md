# Repository recipe inputs

Select the installed release with the existing repository team link interface:
`team link dotnet-web 1.0.0`. This changes repository selection, not shared factory
settings. The profile schema is version 1 independently of team release version.

Default checks are four bounded shell argv recipes: restore, Release build with
analyzers, format verification and tests. The embedded script is captured profile
content, not an executable outside the verified package. sh is a declared host
requirement. No .NET-specific factory runtime branch is required.

Before running, place complete check replacements in the allowlisted repository
`.litefactory/stack.overrides.json`, version 1. Copy each default check and replace
only its target/test/runner/locked arguments and, when needed, working directory.
The sentinel build/test arguments deliberately refuse execution: this release
cannot guess a solution name or choose among several projects. Paths are relative
to the explicit working directory. Repeat complete test recipes for every affected
test project; do not collapse required projects into a successful empty run.

Each argv ends with: recipe phase, explicit solution/project, explicit test project,
runner (`VSTest` or `Microsoft.Testing.Platform`), and declared locked restore
(`true` or `false`). Keep the embedded SDK/runtime pins and preflight guards.
If package lockfiles are declared, use true: both restore targets use locked mode.
Otherwise restore follows repository NuGet configuration. Later phases disable
implicit restore and tests disable implicit build. Build both explicit targets in
Release for net10.0 with analyzers. Verify formatting without rewriting files.

Test recipes evaluate imported MSBuild test properties after restore/build; a
missing test SDK or non-test project refuses test execution. Select the runner
already recorded by the repository's global.json and project packages. MTP uses
`--project` and `--minimum-expected-tests 1`; VSTest uses a positional project and
`RunConfiguration.TreatNoTestsAsError=true` after `--`. Do not mix these options.
The SDK diagnoses global.json runner incompatibility; the profile also refuses an
MTP application under the VSTest recipe or a non-MTP application under MTP.

Repository overrides replace entire command arrays. They must retain SDK/target,
required-test, zero-test and lockfile safeguards; profile allowlisting is not an
approval to remove required checks. Missing tools, inputs, packages or permissions
must remain failed diagnostics, never a Go fallback, successful skip or automatic
host installation. Existing captured Bug/Spike recipes retain their authority.
Fresh admission resolves current permitted overrides; resume and provider fallback
use retained effective bytes and guidance. Validate reviews that retained evidence
read-only, without rerunning commands.
