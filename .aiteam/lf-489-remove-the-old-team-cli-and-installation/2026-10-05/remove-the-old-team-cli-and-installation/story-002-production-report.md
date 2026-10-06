# Story-002 production implementation

Frozen identity: story-002, issue #491 (5715665378), revision 30d696533ada1ea0903b96a53512c4a0e034bac31d7613dc72860789f4b6ea9d. Dependency story-001 was supplied as engineering complete (lf489-story001-run1051).

Production and documentation work is complete; test specialists and aggregate checks remain the orchestrator's responsibility. No acceptance review performed.

## Changes

- Removed all seven team command constructors/registration and the obsolete CLI catalog tests.
- Removed Teams and Settings.TeamConfig models, defaults, validation, source settings template, and catalog/cache path helpers. Added removedTeamKeysError, which refuses teams and settings.team_config with one actionable error per present key and never inspects their values.
- Deleted internal/team, internal/teamremote, internal/teampublish, cmd/team-publish, cmd/team-benchmark, teams/, and .github/workflows/team-content.yml. Removed the command-specific lint exception.
- Deleted teamsource management, overrides, resolve, remote installation and installed snapshot types/capture. Retained the frontmatter parser, source/snapshot resource limits and skill header needed by embedded capture. Moved hashing beside its built-in callers in teamsource/hash.go and turn/hash.go.
- Removed per-harness renderer registry, old installation renderers and their tests. Retained native renderer and shared Frontmatter/ResolvedSource/RenderedFile types.
- Removed external-snapshot delivery planning; normal preparation uses the admitted built-in workflow bundle. Retained confined writer, verification, evidence, stored historical fields and ordinary project instruction handling.
- Replaced README Central teams with Built-in agents; updated package comments and Plan graph documentation to remove obsolete distribution references.

## Executed production check

Command: go build -o /private/tmp/lf489-story002-build ./cmd/litefactory
Result: exit 0; no stdout/stderr.

Earlier build failed with unused imports after removing external delivery; imports corrected before the passing build. No test suite or aggregate check executed by this worker.

Production/docs search for teams/ and removed package paths under internal, docs, README.md, .github and .golangci.yml, excluding test files and testdata, returned no matches.

## Test handoff

- Rewrite internal/turn/delivery_test.go's external Snapshot fixture and planDelivery calls through the built-in workflow path, preserving concurrent publication, confinement, integrity, retained identity and provider-switch checks.
- Rewrite internal/planbrief/task_owner_roles_test.go's deleted teams/source/index.json fixture.
- Retained built-in teamsource tests may use deleted distribution mirror files; replace required behavior fixtures with embedded captures and remove mirror-only assertions.
- Add command/config retirement, historical evidence readability and untouched operator-home coverage as frozen story requires.

## Retained items and scope

Built-in role/skill bytes and graphs are unchanged. No operator home files read or edited. Historical agentresult exchange corpus includes source path strings referring to old teams/source resources; it is a protocol fixture rather than an installation consumer and remains for the test specialist's determination. Repository instruction trees and AGENTS.md also reference old shared-source locations and were not altered because they are outside this assigned story's ownership. Historical .aiteam artifacts remain unchanged except this new session report.
