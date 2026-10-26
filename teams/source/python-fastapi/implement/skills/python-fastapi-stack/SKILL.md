---
name: python-fastapi-stack
description: Python FastAPI and React TypeScript Vite stack rules and supported versions.
resources:
  - python-fastapi.md
  - react-typescript-vite.md
  - supported-versions.md
---
Apply python-fastapi.md to backend work, react-typescript-vite.md to frontend work and supported-versions.md for project toolchains and admitted hosts. Preserve the repository's own constraints. This skill grants no tools or authority.

# Tests and linters

Roles that run tests and linters (story worker, sync resolver, bug fixer, Validate repair) use these rules for their scope. The repository's own Makefile, CI and project instructions win where they say more.

- Run the captured backend and frontend check recipes from the stack profile on the scope (changed components, and their consumers when you changed something they rely on), filtered while fixing and completely once at the end with a finite timeout. Do not use successful no-test skips.
- Lint and format with the captured Ruff and frontend lint/format recipes and the repository's instructions.
