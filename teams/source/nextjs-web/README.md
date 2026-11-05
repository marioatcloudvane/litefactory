# Next.js web team source

This is the canonical source of nextjs-web 1.0.0. The six workflow folders' graphs, roles and result skills come from `teams/source/base-team`; this folder adds only `release.json`, `inherit.json` (the base variables), `stack-profile.json`, the `plan-nextjs-web-guidance` plan skill and the `nextjs-web-stack` skill of the implement, bug and validate modes. Edit this source, not the generated release. Generate and check with:

```sh
go run ./cmd/teamrelease -source teams/source/nextjs-web
go run ./cmd/teamrelease -check -source teams/source/nextjs-web
```

Only the owner releases and links versions: fixes and features edit this source and never change the version or publish a release. Published release bytes are immutable.

The stack profile installs dependencies with `npm ci` and checks with `npm run lint` and `npm run build`. A repository with more scripts (for example a test script) overrides `checks` in `.litefactory/stack.overrides.json`.
