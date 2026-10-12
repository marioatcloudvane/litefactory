# Team distribution

The catalogue and the `<slug>/v<version>` releases are generated from the
canonical team sources by `go run ./cmd/teamrelease`. Do not edit them. Check drift
with `go run ./cmd/teamrelease -check`. Only the owner releases and links versions.

`go-development/v1` is the immutable pre-consolidation release kept as a
non-release fixture for historical capture and compatibility tests; it is not in
`teams.json`. It is not a current authoring source. Saved
runs retain those original bytes, builtin identity and hashes.
