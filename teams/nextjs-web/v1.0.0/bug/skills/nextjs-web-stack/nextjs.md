# Next.js web guidance

Read the repository's package.json, next.config.ts, tsconfig.json, eslint.config.mjs and AGENTS.md before changing code, and keep the versions they pin. Do not upgrade Next.js, React or other dependencies unless the story asks.

- Next.js: newer releases (for example Next.js 16) change APIs, conventions and file structure. When AGENTS.md says so, read the matching guide in `node_modules/next/dist/docs/` before writing code and heed deprecation notices. If `next dev` re-adds its generated block to AGENTS.md, commit it rather than reverting it.
- Routing: use the router the repository uses. With the App Router (`app/` or `src/app/`), pages are `page.tsx`, layouts `layout.tsx`, and API endpoints are route handlers in `route.ts`. Components are Server Components by default; add `'use client'` only to components that need state, effects or browser APIs, and keep that boundary as low as practical.
- TypeScript: keep `strict` on. Do not silence errors with `any`, non-null or unsafe assertions, or `@ts-ignore`. Use the configured path alias (for example `@/*` for `./src/*`) the way existing imports do.
- ESLint: the flat config extends `eslint-config-next` (core-web-vitals and typescript). Fix findings instead of disabling rules.
- Styling: follow the existing setup. With Tailwind CSS v4 through `@tailwindcss/postcss`, the theme lives in CSS (`@import "tailwindcss"` and tokens in the global stylesheet), not in a tailwind.config file. Reuse existing tokens and utilities instead of hard-coded colours.
- Fonts and images: keep using `next/font` and `next/image` as the repository does. `next/font/google` downloads fonts at build time, so `npm run build` needs network access.
- Secrets: read server-only secrets with `process.env` in server code (route handlers, Server Components). Document new variables in `.env.example`; never commit `.env.local` or put secrets in client components or `NEXT_PUBLIC_` variables.
- Content: keep the language, tone and accessibility of existing pages (semantic elements, labels, alt text, keyboard use, visible focus).
