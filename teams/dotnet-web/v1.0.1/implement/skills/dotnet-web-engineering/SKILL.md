---
name: dotnet-web-engineering
description: .NET Blazor repository engineering and workflow obligations.
resources:
  - engineering.md
  - workflow.md
---
Apply engineering.md and workflow.md together with the supplied role and the captured Blazor skill. This skill grants no tools or authority.

# Tests and linters

Roles that run tests and linters (story worker, sync resolver, bug fixer, Validate repair) use these rules for their scope. The repository's own Makefile, CI and project instructions win where they say more.

- Run each test project in the scope with the captured .NET recipe, filtered while fixing and completely once at the end with a finite timeout, keeping the repository's runner and configuration. Do not add Go race flags or assume this repository is litefactory.
- Lint and format as the repository's CI and project instructions name it, using the captured .NET build and format recipes.
