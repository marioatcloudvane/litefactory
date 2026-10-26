# Team distribution

The catalogue and the `<slug>/v<version>` releases are generated from the
canonical team sources by `go run ./cmd/teamrelease`. Do not edit them. Check drift
with `go run ./cmd/teamrelease -check`. Only the owner releases and links versions.

`go-development/v1` is the immutable pre-consolidation release kept as a
non-release fixture for historical capture and compatibility tests; it is not in
`teams.json`. It is not a current authoring source. Saved
runs retain those original bytes, builtin identity and hashes.

## Base and inheritance

`teams/source/base-team` is a shared base copied from go-development 1.0.5 (source files only: `stack-profile.json` and the six workflow folders). It has a `base.json` that declares its variables (`team`) and no `release.json`, so it is never built, listed or installed; the release build refuses it with "base-team is a base, not a team".

A team source can name a base in `inherit.json`:

```json
{"base": "base-team", "replace": ["plan/plan.dot"], "exclude": ["spike/some-file.md"], "variables": {}}
```

- A file the base lacks is added. A file the base has must be listed under `replace`, or the build reports a collision.
- `exclude` removes a base file; the team must not also provide it.
- `{{name}}` in base files is filled from `variables`. `{{team}}` is always the release slug and cannot be set. Every other declared variable must be set.
- One level only: a base cannot name a base. All problems are reported together, sorted.

## Where each source lives

- `teams/source/go-development`: release metadata and `inherit.json`; inherits `base-team`. This is the release command's default source.
- `teams/source/python-fastapi`: release metadata and `inherit.json`; inherits `base-team` and keeps only the files that replace, add to or exclude base content. Its build is byte-identical to v1.0.5.
- `teams/source/swift-ios`, `teams/source/dotnet-web`: full sources. Some files inside still name the old `internal/teamsource/...` path in their text; they are built into releases, so they stay as they are.
- The program holds no team content. Tests load teams from these folders.

## Owner-facing reporting guidance

The canonical `owner-report` skill lives in
`source/base-team/plan/skills/owner-report`. Its declared resources contain mode
guidance and representative scenarios for human review. Base and replacing child
stack profiles select it; Go inherits the base profile. Existing stack capture
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
