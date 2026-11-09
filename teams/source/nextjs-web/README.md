# Next.js web team source

This is the canonical source of nextjs-web 1.0.0. The six workflow folders' graphs, roles and result skills come from `teams/source/base-team`; this folder adds only `release.json`, `inherit.json` (the base variables), `stack-profile.json`, the `plan-nextjs-web-guidance` plan skill and the `nextjs-web-stack` skill of the implement, bug and validate modes. Edit this source; it is installed as it is with:

```sh
litefactory team install nextjs-web --source /absolute/path/to/litefactory/teams/source
```

Teams stay at 1.0.0: never change the version. A change to this source or to `teams/source/base-team` reaches the machine when the owner reinstalls the team. Only the owner installs teams and links versions.

The stack profile installs dependencies with `npm ci` and checks with `npm run lint` and `npm run build`. A repository with more scripts (for example a test script) overrides `checks` in `.litefactory/stack.overrides.json`.
