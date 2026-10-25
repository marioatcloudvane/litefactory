# Research findings

_Technical detail behind [the brief](RESEARCH_BRIEF.md). Findings carry the source they rest on._

## f1

Releases are self-contained generated packages built from a source folder by the team release tool. Release metadata has no parent field.

Evidence:

- `internal/teamrelease/release.go`
- `internal/teamsource/release.json`

## f2

Compared python to go file by file: 67 python files; 26 byte-identical to go, 25 differ by 2 to 8 lines (team name in the graph, stack skill name, 'Go' versus 'implementation' wording, one example command), 16 are python-only stack skill files.

Evidence:

- `internal/teamsource/python-fastapi`
- `internal/teamsource/implement/implement.dot`
- `internal/teamsource/python-fastapi/implement/implement.dot`

## f3

Python and swift sources are subfolders of the go source folder, each with its own release metadata and stack profile. Dotnet source is under a separate folder.

Evidence:

- `internal/teamsource/python-fastapi/release.json`
- `internal/teamsource/swift-ios/release.json`
- `teams/source/dotnet-web/release.json`

## f4

No shared guidance tree; skills are duplicated per mode. Published versions are immutable and owner-released; the check command detects drift.

Evidence:

- `internal/teamsource/README.md`

## f5

Real stack logic in roles is small: the implementation story worker (race flags, lint recipes) differs materially; most other role differences are wording.

Evidence:

- `internal/teamsource/implement/story-worker.md`
- `internal/teamsource/python-fastapi/implement/story-worker.md`

## f6

Go-only files today: the litefactory rules skill (identical 7-line entry and 99-line rules file in bug, implement and validate), one 12-line go plan guidance skill, and the stack profile.

Evidence:

- `internal/teamsource/implement/skills/litefactory-rules/litefactory.md`
- `internal/teamsource/plan/skills/plan-go-guidance/SKILL.md`
- `internal/teamsource/stack-profile.json`
