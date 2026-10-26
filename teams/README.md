# Team distribution

The catalogue and the `<slug>/v<version>` releases are generated from the
canonical team sources by `go run ./cmd/teamrelease`. Do not edit them. Check drift
with `go run ./cmd/teamrelease -check`. Only the owner releases and links versions.

`go-development/v1` is the immutable pre-consolidation release kept as a
non-release fixture for historical capture and compatibility tests; it is not in
`teams.json`. It is not a current authoring source. Saved
runs retain those original bytes, builtin identity and hashes.

## Base and inheritance

`teams/source/base-team` is the shared, stack-neutral base: the six workflow folders with their graphs, roles and result skills. It holds no `stack-profile.json`, no stack guidance and no stack skill; each team adds its own. It has a `base.json` that declares its variables and no `release.json`, so it is never built, listed or installed; the release build refuses it with "base-team is a base, not a team".

Base variables (declared in `base.json`, set by every team's `inherit.json`):

| Variable | Used in | go-development | python-fastapi | dotnet-web | swift-ios |
|---|---|---|---|---|---|
| `team` | graph `team=` attribute | release slug (always; cannot be set) | | | |
| `stack_skill` | `lf_skills` of the Bug, Implement and Validate agent nodes | `go-stack,litefactory-rules` | `python-fastapi-stack` | `dotnet-web-engineering` | `swift-ios-boundaries` |
| `guidance_id` | Plan evidence example | `go` | `python-fastapi` | `blazor` | `swift-ios` |
| `example_check` | example check command in result skills | `go test ./...` | `uv run --no-sync pytest tests` | `dotnet test <explicit-test-project>` | `xcodebuild ... build` |
| `example_file`, `example_test_file` | example paths in result skills | `search.go`, `search_test.go` | `search.py`, `test_search.py` | `Search.cs`, `SearchTests.cs` | `Search.swift`, `SearchTests.swift` |

Roles say only "run the tests and linters your stack skill names". The per-language test, race, timeout, lint and format rules live in each team's stack skill (the `stack_skill` above), under a "Tests and linters" section.

A team source can name a base in `inherit.json`:

```json
{"base": "base-team", "replace": ["plan/plan.dot"], "exclude": ["spike/some-file.md"], "variables": {}}
```

- A file the base lacks is added. A file the base has must be listed under `replace`, or the build reports a collision.
- `exclude` removes a base file; the team must not also provide it.
- `{{name}}` in base files is filled from `variables`. `{{team}}` is always the release slug and cannot be set. Every other declared variable must be set.
- One level only: a base cannot name a base. All problems are reported together, sorted.

## Where each source lives

- Every team inherits `base-team` and keeps only its release metadata, `inherit.json` (variables), `stack-profile.json`, its stack guidance and its stack skill. No team replaces or excludes a base file today.
- `teams/source/go-development`: also owns `plan-go-guidance`, the `go-stack` skill and the `litefactory-rules` skill (rules for developing litefactory itself, which uses this team). This is the release command's default source.
- `teams/source/python-fastapi`, `teams/source/swift-ios`, `teams/source/dotnet-web`: their stack profile, plan guidance skill and stack skill (copied under bug/, implement/ and validate/, because a skill is delivered per workflow folder).
- The program holds no team content. Tests load teams from these folders.

## Owner-facing reporting guidance

The canonical `owner-report` skill lives in
`source/base-team/plan/skills/owner-report`. Its declared resources contain mode
guidance and representative scenarios for human review. Every team's
stack profile selects it. Existing stack capture
attaches that closure to every admitted workflow node, and native delivery makes
it available to both supported providers. Reporting roles use it alongside their
own result contracts; it changes no authority, schema or workflow gate.

These source edits reach only future generated teams after a separate owner-led
release and new captures. No release is cut here. Existing release versions,
catalogues, saved captures and historical reports remain immutable. Cold replay
uses retained resources, without falling back to mutable source. Delivery checks
establish availability, not model compliance, publication success or useful prose;
the scenarios need a separate semantic review.

The existing agent-guidance standard violation in `internal/harness/prompt.go`
remains for operator disposition: around line 492, Go strings advise Plan
continuation and reporting; around lines 514–516, they instruct conflict
resolution and its result. Relocating those instructions is outside this story.
