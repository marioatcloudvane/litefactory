# Python FastAPI project toolchains

Use the repository's Python and dependency constraints, locks and frontend engine constraints. The team imposes no exact toolchain or dependency pins; Python 3.12 is supported. Repositories needing stricter versions can enforce them in setup/check overrides.

## Selection and setup

Recipes start at the repository root. Prefer root pyproject.toml or requirements.txt, falling back to backend/ only when neither exists at root. A pyproject.toml with uv.lock uses uv sync --locked --all-groups --no-python-downloads. Otherwise create or reuse the selected project's .venv and install requirements.txt with its python -m pip, or install an unlocked installable pyproject. The project supplies pytest and any configured check dependencies.

Checks use that .venv. pytest runs without a forced test directory, honoring pytest.ini and pyproject configuration, and fails on zero collection. Ruff format/check runs only with [tool.ruff], ruff.toml or .ruff.toml; mypy runs only with [tool.mypy] or a [mypy] section in mypy.ini, .mypy.ini or setup.cfg. Configured tool failures are failures.

Frontend work is optional. Discover package.json at root or nested project locations, excluding dependency, environment, generated and .git directories. At each package location use npm ci with a package-lock.json, otherwise npm install, with engine-strict and ignored lifecycle scripts. Run declared typecheck, lint, test and build scripts without assuming React, TypeScript or a particular test runner. Configure scripts to terminate in CI. Selected paths must remain within the worktree; caches stay under each project's .cache. Existing permission and network ceilings still apply.

## Overrides and hosts

Only setup and checks can be replaced in .litefactory/stack.overrides.json, schema version 1. Replace full nonempty arrays for stricter guards or repository commands. Guidance, host requirements and environment policy remain fixed. Overrides grant no permission. Bug/Spike pinned phase recipes retain authority; Implement executes setup within writable authority; Validate reviews evidence read-only.

Supported hosts remain factory-admitted macOS (darwin), arm64 or amd64. Only python3 is universally required. uv is needed for locked Python projects; node and npm are needed only for discovered package.json projects. Do not install host tools or expand permission ceilings.

This source change does not update installed releases or retained runs. The owner releases and links a new version; fresh admission captures its profile. Package validation does not prove an example application, a released cockpit workflow or a deployment.
