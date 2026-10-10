# Python FastAPI team source

This is the canonical source of python-fastapi 1.0.0. Generate the distribution with:

```sh
go run ./cmd/teamrelease -source internal/teamsource/python-fastapi
go run ./cmd/teamrelease -check -source internal/teamsource -source internal/teamsource/python-fastapi
```

The generator preserves existing catalogue entries and historical releases. Edit this source before initial publication; published release identities are immutable and later changes need a new version. Generated workflow guidance copies and the distribution are derived artifacts.

For an already registered application clone, use the existing installation and binding interface:

```sh
litefactory team install python-fastapi 1.0.0
litefactory team link python-fastapi 1.0.0 --repo /path/to/registered-clone
```

Use the existing `--source` option when selecting a specific installed catalogue source. No global factory settings change. Both providers use the same captured profile and guidance. See shared/team-guidance/supported-versions.md for exact pins, required executables, defaults, override policy, admitted hosts and package compatibility evidence limits. These instructions do not install host tools or authorize broader permissions.
