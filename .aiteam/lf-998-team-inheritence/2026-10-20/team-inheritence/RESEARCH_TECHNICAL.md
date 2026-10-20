# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

Releases are self-contained generated packages built from a source folder by the team release tool. Release metadata has no parent field.

Evidence:

- `internal/teamrelease/release.go`
- `internal/teamsource/release.json`

## f2

Role files are partly identical across teams (validate-code, bug-fixer, research, plan-story); story-worker, shaping and swift roles differ.

Evidence:

- `teams/source/dotnet-web/validate/validate-code.md`
- `teams/swift-ios/v1.0.1/validate/validate-code.md`

## f3

Correction: python and swift sources exist as subfolders of the go source folder, each with its own release metadata. Dotnet source is under a separate folder.

Evidence:

- `internal/teamsource/python-fastapi/release.json`
- `internal/teamsource/swift-ios/release.json`

## f4

No shared guidance tree; skills are duplicated per mode. Published versions are immutable and owner-released; the check command detects drift.

Evidence:

- `internal/teamsource/README.md`
