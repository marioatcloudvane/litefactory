---
name: nextjs-web-stack
description: Next.js App Router, TypeScript, ESLint and Tailwind rules and the npm checks a role runs.
resources:
  - nextjs.md
---
Apply nextjs.md together with the supplied role. Preserve the repository's own constraints. This skill grants no tools or authority.

# Tests and linters

Roles that run tests and linters (story worker, sync resolver, bug fixer, Validate repair) use these rules for their scope. The repository's own package.json scripts, CI and project instructions (AGENTS.md, CLAUDE.md) win where they say more.

- Install dependencies with `npm ci` from the committed package-lock.json. Do not switch package managers or edit the lock file unless the story changes dependencies.
- Run `npm run lint` and `npm run build` with a finite timeout; failures must propagate. `next build` also type checks, so a type error fails the build.
- Run `npm test` only when package.json declares a `test` script. Do not add a test framework, test script or other tooling the story does not ask for.
- Never run the repository's full check or full suite yourself: not `make check`, not a CI or check script (for example `scripts/ci-test.sh`), not the browser or end-to-end suite, not a Docker image build of the project. `npm run lint`, `npm run build` and the tests for the files you changed, with a bounded timeout, are your checks. Validate runs the full check once, after you finish.
