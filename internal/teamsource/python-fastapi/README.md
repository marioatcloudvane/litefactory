# Python FastAPI team source

This is the canonical source of python-fastapi 1.0.0. Generate the distribution with:

```sh
go run ./cmd/teamrelease -source internal/teamsource/python-fastapi
go run ./cmd/teamrelease -check -source internal/teamsource -source internal/teamsource/python-fastapi
```

Only the owner releases and links versions: fixes and features edit this source and never change the version or publish a release. Published release bytes are immutable. The distribution is a derived artifact.

For an already registered application clone, use the existing installation and binding interface:

```sh
litefactory team install python-fastapi 1.0.0
litefactory team link python-fastapi 1.0.0 --repo /path/to/registered-clone
```

Use the existing `--source` option when selecting a specific installed catalogue source. No global factory settings change. Both providers use the same captured profile and guidance. See the python-fastapi-stack skill (supported-versions.md) for project toolchains, conditional executables, defaults, override policy, admitted hosts and package compatibility evidence limits. These instructions do not install host tools or authorize broader permissions.
