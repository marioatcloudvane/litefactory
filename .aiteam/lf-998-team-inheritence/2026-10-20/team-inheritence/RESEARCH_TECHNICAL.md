# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

Releases are self-contained generated packages from a canonical source via cmd/teamrelease; no inheritance concept exists. Release metadata has no parent field.

Evidence:

- `cmd/teamrelease/main.go`
- `internal/teamrelease/release.go`
- `teams/source/dotnet-web/release.json`

## f2

Role files are partly identical across teams: validate-code identical in python, dotnet source and go; bug-fixer identical in go and python; research.md and plan-story.md identical in python and dotnet. story-worker and shaping differ per stack, and swift differs on all sampled roles.

Evidence:

- `teams/python-fastapi/v1.0.2/validate/validate-code.md`
- `teams/source/dotnet-web/validate/validate-code.md`
- `teams/swift-ios/v1.0.1/validate/validate-code.md`

## f3

The team source README says each mode ships only its own roles and skills, with no shared guidance tree; skills are duplicated per mode.

Evidence:

- `internal/teamsource/README.md`

## f4

Published versions are immutable and only the owner releases them; the existing check command detects drift in generated files.

Evidence:

- `internal/teamsource/README.md`
- `teams/README.md`
