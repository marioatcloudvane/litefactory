# Python FastAPI team source

This is the canonical source of python-fastapi 1.0.0. It is installed as it is, straight from `teams/source`. Teams stay at 1.0.0: never change the version. A change to this source or to `teams/source/base-team` reaches the machine when the owner reinstalls the team. Only the owner installs teams and links versions.

For an already registered application clone:

```sh
litefactory team install python-fastapi --source /absolute/path/to/litefactory/teams/source
litefactory team link python-fastapi 1.0.0 --repo /path/to/registered-clone
```

When the same name and version are installed from more than one source, `team link` takes `--source` to pick one. No global factory settings change. Both providers use the same captured profile and guidance. See the python-fastapi-stack skill (supported-versions.md) for project toolchains, conditional executables, defaults, override policy, admitted hosts and package compatibility evidence limits. These instructions do not install host tools or authorize broader permissions.
