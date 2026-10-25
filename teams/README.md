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
