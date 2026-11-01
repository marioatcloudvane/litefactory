# React, TypeScript and Vite guidance
Apply this guidance when the repository uses React, TypeScript and Vite; a frontend is optional. Preserve project versions and scripts. For TypeScript projects, use strict checking; do not silence boundary errors with any or unsafe assertions. Run declared typecheck, lint, test and build scripts; failures must propagate.

Use typed API access based on backend OpenAPI request/response and error contracts. Follow the repository's client generation convention rather than adding a generator. Handle loading, empty, error and success states explicitly; cancel stale requests and respect component lifetimes. Do not expose tokens in logs or frontend bundles.

Prefer accessible semantic elements, labels, keyboard interactions and visible focus. Keep state local where practical and follow existing routing and state conventions. Where configured, test components with Vitest and Testing Library through observable user interactions, including loading/error states and accessibility. Use the repository's DOM test environment and avoid pass-with-no-tests.

Run the declared production build when present. Follow existing authentication, persistence and deployment conventions; this team does not choose infrastructure or scaffold an example application.
