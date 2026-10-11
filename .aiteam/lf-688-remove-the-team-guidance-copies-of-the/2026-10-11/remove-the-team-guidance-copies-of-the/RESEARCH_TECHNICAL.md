# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

The shared tree holds 22 files (about 125 KB), mostly old agent definitions. No role file or graph references them by file name; only MIGRATION.md mentions them. SKILL.md tells readers to read litefactory.md only for litefactory.

Evidence:

- `internal/teamsource/shared/team-guidance/SKILL.md:1`
- `internal/teamsource/MIGRATION.md:3`

## f2

The release tool requires the shared tree to exist and be non-empty, copies it into every mode as skills/team-guidance with a generated marker, and writes the copy back into the source tree. It also rejects the go-development/v1 download path.

Evidence:

- `internal/teamrelease/release.go:104`
- `internal/teamrelease/release.go:185`
- `internal/teamrelease/release.go:188`
- `internal/teamrelease/release.go:211`

## f3

Every graph lists team-guidance in lf_skills on all agent nodes (bug, implement, plan, research, spike, validate) in the Go, Python, Swift and .NET sources.

Evidence:

- `internal/teamsource/implement/implement.dot:58`
- `internal/teamsource/plan/plan.dot:32`
- `internal/teamsource/validate/validate.dot:57`
- `teams/source/dotnet-web/implement/implement.dot:58`

## f4

Stack teams carry their own team-guidance trees: Python has 25 files (Go tree plus python-fastapi.md, react-typescript-vite.md, supported-versions.md), Swift has SKILL.md and boundaries.md, .NET has SKILL.md, engineering.md and workflow.md. The .NET source lives under teams/source/dotnet-web, not internal/teamsource.

Evidence:

- `internal/teamsource/python-fastapi/shared/team-guidance/supported-versions.md:1`
- `internal/teamsource/swift-ios/shared/team-guidance/SKILL.md:1`
- `teams/source/dotnet-web/shared/team-guidance/SKILL.md:1`

## f5

Needed content is small: litefactory.md (repo-only rules) and environment-safety.md (test URL and database safety). The litefactory text opens with 'These rules apply only when working on litefactory'.

Evidence:

- `internal/teamsource/shared/team-guidance/litefactory.md:1`
- `internal/teamsource/shared/team-guidance/environment-safety.md:1`

## f6

Tests that assert on team-guidance: release_test (copy, retention, delete and shape checks, comma stripping of ',team-guidance'), python_fastapi_test (shared tree and supported-versions), plus turn (validate_assets, spike_assets, implement_graph, consolidated_delivery), teamsource plan_set_graph and codex research_delivery tests.

Evidence:

- `internal/teamrelease/release_test.go:262`
- `internal/teamrelease/release_test.go:390`
- `internal/teamrelease/python_fastapi_test.go:234`
- `internal/teamsource/plan_set_graph_test.go:1`

## f7

Current versions on disk: go-development has v1, v1.1.1, v1.2.0 to v1.2.8; python-fastapi, swift-ios and dotnet-web each have v1.0.0 to v1.0.7. Source release.json files say Go 1.2.8, Python 1.0.7. teams.json lists releases per version with file hashes.

Evidence:

- `internal/teamsource/release.json:4`
- `internal/teamsource/python-fastapi/release.json:4`
- `teams/teams.json:1`

## f8

go-development/v1 is a baseline, not a release. Purity tests list its dot files, and release tests read it to compare graphs. Old Go 1.0.0 and 1.1.1 are hard-coded as legacy profile-less identities and 1.1.1 is referenced by the release tool; other tests pin v1.2.3 and v1.2.5 folders and several pin python/swift/dotnet v1.0.2.

Evidence:

- `internal/graphexec/purity_test.go:64`
- `internal/teamrelease/release_test.go:253`
- `internal/teampackage/selection.go:171`
- `internal/turn/implement_graph_run_test.go:238`
- `internal/graphexec/implement_graph_test.go:286`
- `internal/turn/validate_graph_test.go:142`

## f9

Repositories bind to an exact source, slug, version and release digest. RepositoryTeamUses lists repos using an exact version, so removal can be checked.

Evidence:

- `internal/store/migrations/20261007223923674_repository_team_bindings.sql:1`
- `internal/store/repository_team_bindings.go:122`
- `internal/cli/cmd_team_binding.go:72`

## f10

Workflow captures store the admitted files, digest and provenance (source, slug, version, digest) per launch. Deleting version folders therefore should not alter bytes of running or waiting instances (inferred from the capture design; not tested against a live waiting run such as #556's Implement).

Evidence:

- `internal/store/workflow_captures.go:13`
- `internal/store/workflow_captures.go:20`

## f11

Installing a version that already exists on disk with a different checksum fails with a conflict. A 1.0.0 re-release of python, swift and dotnet with new content cannot overwrite existing installed 1.0.0 copies. 1.0.0 is also hard-coded as legacy profile-less Go, so a new Go 1.0.0 with a stack profile may collide with that rule.

Evidence:

- `internal/teampackage/manager.go:428`
- `internal/teampackage/manager.go:433`
- `internal/teampackage/selection.go:171`

## f12

Not verified: I could not read the installed teams folder or the live database (permission denied), so installed copies and live pinned instances are unchecked.

_No source reference was recorded for this finding._
