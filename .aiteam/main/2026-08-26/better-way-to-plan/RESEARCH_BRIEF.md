# Research Brief — better-way-to-plan (external verification pass)
# Branch: main | Session: 2026-08-26 | Feature: better-way-to-plan
# Issue: marioatcloudvane/litefactory#5 "Better way to plan"

> **This brief is the second half of one piece of research, not a replacement for the first.**
> Its predecessor —
> `.aiteam/lf-5-better-way-to-plan/2026-08-25/better-way-to-plan/RESEARCH_BRIEF.md` — read the
> worktree directly and cited `path:line`. Its findings **F-1..F-14** and **C-1..C-6** stand and
> are **not restated here**. What it could not do was reach the network: its Questions 2 (GitHub
> capabilities) and 4 (OpenSpec) were pure model recall and were correctly marked ⚠️ UNVERIFIED.
>
> This run had `WebSearch` and `WebFetch`. It re-ran those two surveys against live sources.
> **Every claim below carries a URL and was retrieved on 2026-08-26.** Anything that could not be
> settled is labelled UNVERIFIED rather than guessed — the predecessor's refusal to fabricate is
> why this run was cheap, and that standard is preserved.
>
> **Headline: the predecessor's single highest-confidence external claim was wrong, and so was
> its highest-ranked recommendation.** See *Corrections* immediately below.

---

## Request

Verify, correct or refute the predecessor's unverified external sections: OpenSpec, GitHub's
issue-planning API surface, and the shortlist of rival spec/plan tools. Then restate OD-1 (web
tools in the headless tool surface) now that the value of those tools is known.

---

## Corrections — what this run changes in the predecessor

Read this table as a patch against the old brief. **Six of its external claims were wrong**, two
of them load-bearing.

| # | Old claim | Verdict | Now |
|---|---|---|---|
| **X-1** | "Projects v2 is **GraphQL only** (HIGH confidence)… forces a GraphQL transport" | **WRONG** | A **REST API for Projects v2 shipped 2025-09-11**, including user-owned projects. No GraphQL transport needed. |
| **X-2** | CODEOWNERS + required review is "**~Zero** cost", ranked #2, "best value-to-cost item" | **WRONG on cost** | On a **private** repo, required reviews / protected branches / code owners are **GitHub Pro** features. This repo is private. On Free the gate **cannot be built at all**. |
| **X-3** | Issue dependencies: "existence uncertain", LOW confidence, ranked #8 | **WRONG** | **GA since 2025-08-21** with a documented REST API. Should rank far higher. |
| **X-4** | OpenSpec: npm-only, TypeScript, MEDIUM maturity, "may **rewrite `CLAUDE.md`**" | **WRONG** | v1.10.0, 66k stars, MIT, also on **Homebrew/Nix/deno**. It does **not** touch `CLAUDE.md`. |
| **X-5** | Spec Kit: "believed **no GitHub-issues integration** despite the GitHub ownership" | **WRONG** | It has one: **`/speckit.taskstoissues`**. |
| **X-6** | Beads: issues stored in-repo as **JSONL** | **WRONG** | Source of truth is an embedded **Dolt** SQL database; JSONL is an export. (Go + binary releases: **confirmed**.) |
| **X-7** | Sub-issues: REST believed, limits "~100 children, ~8 levels (LOW confidence)" | **CONFIRMED** | REST confirmed, `sub_issue_id` confirmed, and the limits are **exactly** 100 and 8, quoted from docs. Upgrade to HIGH. |
| **X-8** | Issue types: "believed org-only" | **CONFIRMED, and it bites** | Org-only is stated in docs. `marioatcloudvane` is a **personal account**, so issue types are **unavailable here**. |
| **X-9** | Issue forms return only rendered markdown | **CONFIRMED** | No structured field anywhere in REST, GraphQL or webhooks. |
| **X-10** | Comment body cap 65,536 bytes → 422 | **CONFIRMED, with a caveat** | Confirmed only from the API's own live error text; **not documented** on docs.github.com. |

Two further facts the predecessor had no way to know:

- **X-11 — The "rewrites your `CLAUDE.md`" fear was real, but aimed at the wrong tool.** OpenSpec
  does not do this. **Beads does** — `bd init` writes `AGENTS.md`, and `bd setup claude` writes a
  marker-delimited managed block into `CLAUDE.md`.
- **X-12 — A newer REST API version exists.** `2026-03-10` is now supported alongside
  `2022-11-28`. The client's pin is safe (`2022-11-28` is supported to **2028-03-10** and is
  still the default), but the Projects v2 docs are written against `2026-03-10`.
  ([api-versions](https://docs.github.com/en/rest/about-the-rest-api/api-versions))

---

## Codebase Context

Predecessor's F-1..F-14 stand unchanged. This run read only enough of the tree to ground the
external questions, and added three small verified facts:

- **N-1 — The repo is private, and owned by a personal account.**
  `git config` gives origin `https://github.com/marioatcloudvane/litefactory.git`
  (`/Users/mariomh/litefactory/.git/config:9`). `https://api.github.com/users/marioatcloudvane`
  returns `"type": "User"` — **not an Organization**. `https://api.github.com/repos/marioatcloudvane/litefactory`
  returns **404 unauthenticated** while being a live push target — i.e. **private**. This single
  fact drives X-2 and X-8.
- **N-2 — litefactory guarantees the bot and the owner are different GitHub accounts.**
  `/Users/mariomh/litefactory/internal/cli/cmd_init.go:118-125` refuses to initialise if they
  match:

  > `if hasBot && bot.ID == owner.UserID {` … *"the configured bot token belongs to the same
  > account as the owner… Point github.token_keychain or github.token_env at a separate machine
  > or bot account."*

  This is a **precondition the CODEOWNERS design needs and already has**: the PR author (bot) can
  never be the approving code owner (human). See *Question 2 → CODEOWNERS*.
- **N-3 — `--tools` is the real boundary; `--allowedTools` leaks.** `internal/config/config.go:100-113`
  records this as measured, not assumed (SD-2): *"--allowedTools is **additive** to the operator's
  own settings — 250 ambient allow-rules leaked through and Bash executed while absent from
  --allowedTools. It is therefore defence in depth, not the sandbox."* Claude Code's own docs
  agree that `--tools` is the hard surface. This is decisive for *OD-1, restated*.

---

## Domain / Problem Space

### Question 2 — GitHub capabilities, now VERIFIED

Re-ranked by value ÷ cost for *this* client (hand-rolled `net/http`, REST-only, no SDK), with
verified status replacing the old confidence guesses.

| Rank | Capability | API status (verified) | Cost here | Δ vs old |
|---|---|---|---|---|
| 1 | **Sub-issues** | REST, GA, 5 endpoints | ~3 endpoints, existing style | ↑ from 3 |
| 2 | **Labels** | REST, full CRUD | Trivial | = |
| 3 | **Issue dependencies** | REST, **GA 2025-08-21** | 4 endpoints | **↑ from 8** |
| 4 | **Closing keywords + timeline + state** | Keywords free; timeline & state REST | ~1 endpoint | = |
| 5 | **Milestones** | REST | 2 endpoints | = |
| 6 | **Projects v2** | **REST since 2025-09-11** | Moderate — but see the version caveat | **↑ from 9** |
| 7 | **CODEOWNERS + ruleset gate** | Not an API. **Requires GitHub Pro on a private repo** | **£ + unknown plan** | **↓ from 2** |
| 8 | **Issue forms** | Body markdown only | You own a parser | = |
| — | **Issue types** | REST exists but **org-only** | **Unavailable — personal account** | ✗ ruled out |

#### Sub-issues — confirmed, and the best fit in the survey
[docs.github.com/en/rest/issues/sub-issues](https://docs.github.com/en/rest/issues/sub-issues),
[adding-sub-issues](https://docs.github.com/en/issues/tracking-your-work-with-issues/using-issues/adding-sub-issues)

Five endpoints, not the two or three assumed:

- `GET  /repos/{owner}/{repo}/issues/{issue_number}/parent`
- `GET  /repos/{owner}/{repo}/issues/{issue_number}/sub_issues`
- `POST /repos/{owner}/{repo}/issues/{issue_number}/sub_issues`
- `DELETE /repos/{owner}/{repo}/issues/{issue_number}/sub_issue` — **singular** in this one path
- `PATCH /repos/{owner}/{repo}/issues/{issue_number}/sub_issues/priority`

**The payload question is settled: `sub_issue_id`, the internal issue ID — not the issue
number.** The predecessor guessed this correctly. Optional `replace_parent` (boolean) exists;
reprioritise takes `sub_issue_id` plus `after_id`/`before_id`.

Limits, quoted verbatim: *"You can add up to 100 sub-issues per parent issue and create up to
eight levels of nested sub-issues."* The predecessor's "~100 / ~8, do not design against these
numbers" was right, and they can now be designed against.

Two bonuses the old brief did not have: `GET .../issues/{n}` already returns
**`sub_issues_summary`** (`total`, `completed`, `percent_completed`) and **`parent_issue_url`` —
so hierarchy is readable with **no new endpoint at all**, on a call the client already makes
(`internal/github/issues.go:130`). Sub-issue add/remove and parent add/remove also appear in the
issue timeline.

Confirmed lineage: tasklist blocks were retired **2025-04-30** and sub-issues are their stated
replacement ([changelog 2025-02-18](https://github.blog/changelog/2025-02-18-github-issues-projects-february-18th-update/)).
The predecessor's warning that markdown task-list checkboxes are unsafe (no per-item identity,
whole-body `PATCH`, silent clobber) stands and is now moot — sub-issues is the supported path.

**One unresolved contradiction, and it is decisive.** The sub-issues docs state **no** account-type
restriction (verified: no plan banner on the page), whereas the issue-types docs *do* say
plainly *"Issue types are currently only available to organizations."* That asymmetry is good
evidence sub-issues work on a personal account. But a GitHub-staff community reply claims both
are org-only. **Docs win over a forum post, but this is one authenticated `curl` away from
certain** — see Open Question 1.

#### Issue dependencies — GA, REST, and the predecessor missed it entirely
[docs.github.com/en/rest/issues/issue-dependencies](https://docs.github.com/en/rest/issues/issue-dependencies),
[changelog 2025-08-21](https://github.blog/changelog/2025-08-21-dependencies-on-issues/) — *"Dependencies on issues are now generally available!"*

- `GET/POST /repos/{o}/{r}/issues/{n}/dependencies/blocked_by` — body `issue_id`
- `DELETE  /repos/{o}/{r}/issues/{n}/dependencies/blocked_by/{issue_id}`
- `GET     /repos/{o}/{r}/issues/{n}/dependencies/blocking`

Note the field is **`issue_id`**, not `sub_issue_id` — a different object model from sub-issues.
The two are **separate features**: sub-issues express *decomposition* (parent/child), dependencies
express *sequencing* (blocked-by/blocking). Together they are exactly the "epic / story / task
plus ordering" structure currently encoded as markdown headings and a `Depends On` column in
three incompatible templates (F-3). A reported cap of "50 issues per relationship type" is
**UNVERIFIED** — do not hard-code it.

#### Projects v2 — the big correction
[changelog 2025-09-11](https://github.blog/changelog/2025-09-11-a-rest-api-for-github-projects-sub-issues-improvements-and-more/) ·
[docs.github.com/en/rest/projects](https://docs.github.com/en/rest/projects)

The predecessor called this GraphQL-only at HIGH confidence and dropped it to rank 9 on that
basis. **It shipped a REST API eleven months ago.** Verified endpoint surface:

- Projects (**read-only**): `GET /orgs/{org}/projectsV2[/{project_number}]`,
  `GET /users/{username}/projectsV2[/{project_number}]`
- Items (**full CRUD**): `GET/POST /users/{username}/projectsV2/{n}/items`,
  `GET/PATCH/DELETE .../items/{item_id}`, `GET .../views/{view_number}/items`
- Fields: `GET` list/single and `POST` create

Crucially the `/users/{username}/...` variants mean **user-owned projects are supported** — this
works on a personal account. Field values are updated by `PATCH` with a `fields` array of
`{id, value}`.

Three caveats that keep this at rank 6 rather than higher:

1. **The docs specify `X-GitHub-Api-Version: 2026-03-10`**, not the client's pinned
   `2022-11-28`. Whether the old version serves these endpoints is **UNVERIFIED** — one `curl`
   settles it. If it does not, adopting Projects means a per-call version header or a pin bump.
2. **Creating or deleting a project itself over REST is UNVERIFIED** — no such endpoint was found,
   and no doc says either way. Probably still GraphQL.
3. The old brief's rate-limit reasoning was right in substance: GraphQL is point-based and has
   **no ETag/304 escape hatch**, whereas REST 304s are free — *"Making a conditional request does
   not count against your primary rate limit if a 304 response is returned"*
   ([best practices](https://docs.github.com/en/rest/using-the-rest-api/best-practices-for-using-the-rest-api)).
   That argument now **supports** using the REST Projects API rather than avoiding Projects.

**What this changes: "not Projects v2, because GraphQL" is no longer a valid reason.** If Projects
is rejected it must now be rejected on the honest ground the predecessor also gave — *it buys a
board nobody has asked to look at*. That argument survives; the technical one does not.

Also corrected: Projects **classic** REST was sunset **2025-04-01**, and its docs have been
relocated to `/rest/projects-classic/*`. The predecessor's "surviving docs are a trap for anyone
searching" is now **stale** — `/rest/projects` serves v2.

#### CODEOWNERS — the ranking's biggest casualty
The predecessor called this *"the standout… Best value-to-cost item in the survey"* at "~Zero"
cost. The mechanism is real. **The entitlement may not be.**

What is confirmed:

- CODEOWNERS lives at `.github/CODEOWNERS`, root, or `docs/`; gitignore-ish patterns, but **no
  `!` negation and no `[]` ranges**; last match wins; **owners must have write access** or the
  entry is silently ignored. `@username` works fine with no org and no teams.
  ([about-code-owners](https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/customizing-your-repository/about-code-owners))
- The gate is **path-scoped**: a PR touching no matched path is not blocked. Draft PRs do not
  request code-owner review until marked ready.
- Self-approval is barred at platform level: **"Pull request authors cannot approve their own
  pull requests."**
  ([approving-a-pull-request-with-required-reviews](https://docs.github.com/en/pull-requests/collaborating-with-pull-requests/reviewing-changes-in-pull-requests/approving-a-pull-request-with-required-reviews))
  Combined with **N-2**, the "bot proposes, human owner approves" direction works cleanly.
- Rulesets have a REST API (`GET/POST /repos/{o}/{r}/rulesets`, …) and the rule parameter is
  `require_code_owner_review`. ([available-rules](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-rulesets/available-rules-for-rulesets), [rest/repos/rules](https://docs.github.com/en/rest/repos/rules))

What breaks it — quoted verbatim from GitHub's own plans page, listing what **Pro** adds over
**Free** *for private repositories*:

> *"Required pull request reviewers, Multiple pull request reviewers, Protected branches, Code
> owners, Auto-linked references, GitHub Pages, Wikis, Repository insights graphs"*

([githubs-plans](https://docs.github.com/en/get-started/learning-about-github/githubs-plans))

This repo is **private** (N-1). So:

- **On GitHub Pro or above: the gate works, and is still excellent value.**
- **On GitHub Free: it cannot be built.** And there is no free workaround — a required status
  check needs branch protection (same gating), and deployment environments with required
  reviewers are explicitly *"Users with GitHub Free plans can only configure environments for
  public repositories"*
  ([managing-environments](https://docs.github.com/en/actions/how-tos/managing-workflow-runs-and-deployments/managing-deployments/managing-environments-for-deployment)).
  On Free there is **no first-party mechanism to require human approval before merge on a private
  personal repo.**

The account's plan tier is **UNVERIFIED** — it needs `gh api user` or the billing page, neither
reachable from here. **This is now the top blocking question.**

One honesty note the predecessor could not make: even on Pro this is a **discipline gate, not a
cryptographic one.** The repo admin is the same person the gate constrains, can add themselves to
`bypass_actors`, and can edit or delete the ruleset at will. It raises friction and leaves an
audit trail. It does not make self-merge impossible. That is still a large improvement on
file-existence (F-4), but it should be sold as what it is.

#### Everything else, confirmed
- **Labels** — full REST CRUD as assumed. Description ≤100 chars (documented); a 50-char name
  limit is **UNVERIFIED** (error-message sourced). ([rest/issues/labels](https://docs.github.com/en/rest/issues/labels))
- **Milestones** — five endpoints; assignment via `PATCH .../issues/{n}` field `milestone`, and
  *"milestone changes are silently dropped"* without push access. ([rest/issues/milestones](https://docs.github.com/en/rest/issues/milestones))
- **Closing keywords** — close/closes/closed, fix/fixes/fixed, resolve/resolves/resolved; work in
  a PR body; **cross-repo supported** (`Fixes octo-org/octo-repo#100`). ([using-keywords](https://docs.github.com/en/get-started/writing-on-github/working-with-advanced-formatting/using-keywords-in-issues-and-pull-requests))
- **Issue state** — `PATCH .../issues/{n}` with `state`, and `state_reason` ∈
  `completed | not_planned | duplicate | reopened | null`. `not_planned` and `duplicate` are
  directly useful for F-13 ("how does a plan get marked wrong").
- **Issue forms** — predecessor fully vindicated. `GET .../issues/{n}` exposes only `body`,
  `body_html`, `body_text`; the `issues` webhook carries no structured form data; real
  body-parser Actions exist because of it (`issue-ops/parser`, `stefanbuck/github-issue-parser`).
  Field types are `markdown, input, textarea, dropdown, checkboxes, upload`, and **`textarea`
  supports `render:`** — which makes the predecessor's suggestion (a fenced ```yaml``` block in
  one textarea) not just better but *directly supported*. ([syntax-for-issue-forms](https://docs.github.com/en/communities/using-templates-to-encourage-useful-issues-and-pull-requests/syntax-for-issue-forms))
- **Rate limits** — REST 5,000 req/hr authenticated. The binding constraint for a bot is the
  **secondary** limit: **80 content-generating requests per minute and 500 per hour**, plus 100
  concurrent requests. ([rate-limits](https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api))
  The predecessor's instinct that content-creation limits bite well before 5,000/hr is
  **confirmed with numbers**.
- **Comment body cap** — 65,536 characters, HTTP 422, error text *"Body is too long (maximum is
  65536 characters)"*. Confirmed from live API errors across many projects but **not stated
  anywhere on docs.github.com** — an undocumented limit the existing truncation ladder
  (`internal/report/doc.go:44-60`) already handles.

### Question 4 — OpenSpec, now VERIFIED

**Identity confirmed.** [github.com/Fission-AI/OpenSpec](https://github.com/Fission-AI/OpenSpec)
resolves. *"Spec-driven development (SDD) for AI coding assistants."* TypeScript, **MIT**,
created 2025-08-05, **66,335 stars**, 4,567 forks, 204 open issues, last push 2026-08-24,
homepage `openspec.dev`. Latest release **v1.10.0** (2026-08-19). *(Figures from
`api.github.com/repos/Fission-AI/OpenSpec`, retrieved 2026-08-26, and independently re-fetched.)*

**This is not the "MEDIUM confidence, possibly obscure" tool the predecessor described.** It is
past 1.0, heavily starred, actively developed, and permissively licensed. The disambiguation
warning stands — bare "OpenSpec" searches are swamped by the OpenAPI Specification — but no
*second* project of that name was found.

Corrections that matter:

- **Not npm-only.** `@fission-ai/openspec` on npm (v1.10.0, Node ≥20.19.0), **but also
  `brew install openspec`** (homebrew-core, v1.10.0), Nix, and `deno install`. The
  predecessor's central argument — *"adopting the CLI means the project acquires a Node toolchain
  dependency that CI and every contributor must satisfy"* — is **substantially weakened**. A
  Homebrew-installed single command is roughly the footprint of any other dev tool. It is still
  not zero, and there is no Go distribution.
- **It does NOT rewrite `CLAUDE.md`.** This was the predecessor's "collision nobody should miss"
  (MED-LOW confidence, high impact). Refuted at source: for Claude Code, OpenSpec writes
  `.claude/skills/openspec-*/SKILL.md` and `.claude/commands/opsx/<id>.md` and nothing else
  ([docs/supported-tools.md](https://raw.githubusercontent.com/Fission-AI/OpenSpec/main/docs/supported-tools.md)).
  For the vendor-neutral `.agents/` target the docs state flatly *"No `AGENTS.md` is created or
  edited."* An **older** version did insert `<!-- OPENSPEC:START -->` marker blocks; `openspec
  update` now strips those legacy markers while preserving surrounding content.
  **Residual risk: LOW but not zero** — this is from docs, not a source read.
- **Layout confirmed, with one fix.** `openspec/project.md`, `openspec/specs/<capability>/spec.md`
  (living), `openspec/changes/<change-id>/{proposal.md,tasks.md,design.md,specs/...}` (delta).
  The archive path is **`openspec/changes/archive/<date>-<change-id>/`** — date-prefixed, not the
  bare `changes/archive/` guessed.
- **Spec format confirmed exactly:** `## ADDED Requirements` / `### Requirement: <name>` in SHALL
  language / `#### Scenario: <name>` with `**WHEN**` / `**THEN**` bullets.
- **CLI is much larger than guessed, and `diff` does not exist.** Real surface includes `init`,
  `update`, `list`, `view`, `show`, `validate`, `archive`, `new change`, `status`, `context`,
  `workset`, `store`, `schema`, `config`, `doctor`, `templates`, `instructions`, `completion`.
  `diff` is an **open feature request (issue #427)**, not a shipped command — the predecessor
  listed it as part of the CLI.
- **No GitHub Issues integration.** Only an RFC (Discussion #657, "GitHub Issues Integration —
  Hooks & Adapters"). Not shipped.

**Verdict: the predecessor's recommendation survives, but its reasoning does not.** Two of its
three arguments against OpenSpec (npm-only Node dependency; may rewrite `CLAUDE.md`) are wrong or
much weaker than stated. What remains is the argument that actually matters and was always the
strongest: **the root cause here is unenforced process, not file format** (F-3, F-13), and
OpenSpec does not fix that — it has no GitHub integration, no notion of a plan being falsified by
a commit, and it would sit *alongside* a four-mode system that already exists. Its one genuinely
good idea — **living spec / change delta / archive-on-completion** — remains adoptable as plain
markdown with zero dependency, and directly addresses F-6 and F-11. *Steal the idea, skip the
CLI* is still the right call. It is now a closer call than the predecessor made it, and Plan mode
should be told that honestly.

### The rival tools — verified

| Tool | Status | Runtime | Maturity (2026-08-26) | Touches `CLAUDE.md`? |
|---|---|---|---|---|
| **Spec Kit** `github/spec-kit` | CONFIRMED + corrected | Python 3.11 / `uv` | **131,651★**, MIT, **v1.0.0** (2026-08) | No — `.claude/skills` |
| **OpenSpec** `Fission-AI/OpenSpec` | CONFIRMED + corrected | Node / **also brew** | 66,335★, MIT, v1.10.0 | **No** |
| **BMAD-METHOD** `bmad-code-org/BMAD-METHOD` | CONFIRMED + corrected | Node **and** Python/`uv` | 52,331★, v6 rewrite | Installs into `.claude/` |
| **Beads** `gastownhall/beads` | CONFIRMED + corrected | **Go, single binary** | 26,619★, MIT, v1.2.2 | **Yes — marker block** |
| **Task Master** `eyaltoledano/claude-task-master` | CONFIRMED | Node/npm, MCP | 28,022★, **MIT + Commons Clause** | — |
| **Backlog.md** `MrLesk/Backlog.md` | CONFIRMED | **TypeScript/npm** (not dependency-free) | 6,545★, MIT | — |
| **Agent OS** `buildermethods/agent-os` | CONFIRMED | **Shell + markdown, no runtime** | 5,335★, MIT, last push 2026-05-05 | — |
| **Kiro** (AWS) | CONFIRMED + corrected | IDE **and a CLI** | GA ~2026-03 | `.kiro/specs/` |
| **Tessl** / **Traycer** | CONFIRMED real | Commercial | — | — |

Notes worth carrying:

- **Spec Kit's "no GitHub Issues integration" was wrong** — `/speckit.taskstoissues` converts
  `tasks.md` into GitHub Issues (one-way). Given this repo's whole question is "use more of what
  GitHub offers", that is the single most relevant capability in the survey and the predecessor
  dismissed it on a bad recall. Spec Kit is also by far the largest project in the category.
- **Kiro is not dismissible in one line.** The predecessor's "an IDE, not a CLI — not composable"
  is wrong; a Kiro CLI exists. (Whether it is worth adopting is a separate matter.)
- **Beads is the only credible Go-native option** and the predecessor was right that it is the
  best *runtime* fit — Go, single static binary, real releases for `darwin_arm64` et al., MIT,
  actively developed. But two facts change the calculus: its source of truth is an **embedded
  Dolt SQL database** under `.beads/` (not plain JSONL in git — so the C-1 "files are the agent's
  only memory" property is *not* obviously preserved), it has **no GitHub Issues sync**, and it
  **writes a managed block into `CLAUDE.md`**. It is a *replacement* for an issue tracker, not a
  bridge to one — the opposite of what issue #5 asks for.
- **BMAD needs Node *and* Python/`uv`**, has 9 agents (not 6), underwent a major v6 rewrite away
  from pure persona ceremony, and GitHub's licence detector reports **`NOASSERTION`** with a
  separate `TRADEMARK.md` — not clean MIT.
- **Gap hunt:** no credible Go spec/planning tool exists besides Beads. GitHub-Issues-native
  agent orchestrators do exist (`ryanmac/code-conductor` 110★ and stalled since 2026-03;
  `mraza007/baton` 20★) but are far too immature. **The landscape has not consolidated.** Nothing
  in it does what litefactory does *and* integrates with GitHub Issues at a maturity worth
  adopting — which is a quiet vindication of building it here.

---

## Constraints & Risks

- **The CODEOWNERS gate is plan-gated and the plan is unknown.** On Free + private, it does not
  exist and has no free substitute. This is the largest open risk in the brief and it invalidates
  the predecessor's *(d)* scope item until settled.
- **Issue types are ruled out**, verified: org-only, and this is a personal account.
- **Sub-issues on a personal account is one contradiction away from certain.** Docs say no
  restriction; a staff forum post disagrees. Settle before designing on it.
- **Projects v2 REST is documented against API version `2026-03-10`.** The client pins
  `2022-11-28`. Cheap to test, but not free.
- **Secondary rate limits (80/min, 500/hr content-generating)** are the real ceiling for a bot
  that posts more, not the 5,000/hr primary limit.
- **The 65,536-character comment cap is undocumented** — it is enforced but not promised. Do not
  build anything that assumes it will not change.
- **C-1 still stands above everything**: no `--resume`, so in-repo files remain the agent's only
  cross-run memory. Nothing found externally changes this. Note that **Beads would move that
  memory into a Dolt database**, which is exactly the property C-1 warns about.
- **A REST Projects v2 API removes the technical objection to Projects but not the product one.**
  Do not let "it's now cheap" become "so let's build it".

---

## Open Questions

> **Addendum, 2026-08-26 — blocking questions 1 and 2 are now ANSWERED.** Settled after this
> brief was written, by interactive `curl` against the live API using the token in the git
> credential store (the same path the brief recommends, since `gh` is logged out here). The
> question text below is left as written; these are the answers.
>
> **Q1 — the plan is Free, and it is decisive.** Both probes on
> `marioatcloudvane/litefactory` (private, personal account) return **HTTP 403,
> `"Upgrade to GitHub Pro or make this repository public to enable this feature."`** —
> `GET /branches/main/protection` and `GET /rulesets` alike. `GET /user` does not expose
> `.plan` to this token, so the capability probe is the better evidence anyway: it tests the
> feature rather than the entitlement. **Scope item (d), the CODEOWNERS-enforced gate, cannot
> be built on this repo as it stands.** The three ways out — make the repo public, upgrade to
> Pro, or move it into an organisation — are an owner decision, not a design decision.
>
> **Q2 — sub-issues work here, and cost no version bump.** `GET /issues/5` returns
> `sub_issues_summary: {total: 0, completed: 0, percent_completed: 0}`, and
> `GET /issues/5/sub_issues` returns **HTTP 200** with an empty list — both under the client's
> currently pinned `X-GitHub-Api-Version: 2022-11-28`. The docs-vs-forum contradiction resolves
> in favour of the docs: private personal-account repos are fine. The rank-1 recommendation
> stands and gets cheaper.
>
> **Q3 — still open, and my probe was inconclusive.** `GET /users/marioatcloudvane/projectsV2`
> 404s under *both* `2022-11-28` and `2026-03-10`. A 404 here is ambiguous: a fine-grained token
> without the Projects permission 404s rather than 403s, to avoid disclosing existence. This is
> **not** evidence against X-1. It needs a token known to carry the scope before it means
> anything.

**Blocking — settle before any design work**

1. **What GitHub plan is `marioatcloudvane` on?** `gh api user` → `.plan.name`, or the billing
   page. Free vs Pro decides whether the human-approval gate can exist at all. *(Note: the `gh`
   CLI is logged out on this machine; the working token is in the git credential store.)*
2. **Do sub-issues work on a private personal-account repo?** One authenticated call —
   `POST /repos/marioatcloudvane/litefactory/issues/{n}/sub_issues` with a `sub_issue_id`, or
   simply read `sub_issues_summary` off an existing issue. Resolves the docs-vs-forum
   contradiction. Everything in the rank-1 recommendation depends on it.
3. **Does `X-GitHub-Api-Version: 2022-11-28` serve the `projectsV2` REST endpoints?** Decides
   whether Projects costs a version bump.
4. **Can a project itself be created/deleted over REST**, or is that still GraphQL? UNVERIFIED
   either way. Only matters if Projects is adopted.

**Carried forward from the predecessor, unchanged and still open**

5. Old Q6–Q15 (canonical template, `proxy-product-owner`, un-blocking `/plan` and `/research`,
   artifact awareness in Go, `.aiteam/` after merge, **how a plan gets marked wrong (F-13 — still
   the root failure)**, retiring the 746-line plan, `--resume` as a prerequisite, reinstating
   D-CI-1). Nothing external changes any of them.

**Newly opened by this run**

6. **Does Spec Kit's `/speckit.taskstoissues` do what this repo actually wants?** It is the only
   verified GitHub-Issues bridge in the whole tool survey, and it was dismissed on a wrong recall.
   Worth thirty minutes before deciding to hand-roll the same thing.
7. **Do sub-agents inherit the parent's `--tools` surface?** Claude Code's documentation suggests
   sub-agent `tools:` frontmatter is scoped independently, which would mean a sub-agent can hold a
   tool the parent was not granted. **The observed evidence contradicts this** — four sub-agents in
   the predecessor's run hit the same wall as the parent. **UNVERIFIED and contested.** It matters:
   if true, web access could be granted to one research sub-agent rather than to every run.

---

## OD-1, restated

The predecessor recorded OD-1 as an owner decision that was approved and then lost inside a
`.superseded.md` file (F-6), and noted its absence degraded that run (F-9). It flagged the
security trade-off but could not weigh it. Here is the weighing.

**What the web tools actually bought.** This run overturned six external claims, two of them
load-bearing — a GraphQL transport that turned out to be unnecessary, and a "~zero cost"
recommendation that may be impossible on the current plan. Both errors would have survived into
Plan mode and into code. That is the concrete value: **not better prose, but the prevention of
two architectural mistakes.** For a Research-mode run specifically, a tool surface with no
network is close to useless for any question about a third party.

**Recommended tool surface for a headless research run:**

- Add **`WebFetch`** and **`WebSearch`** to `claude.tools` — the `--tools` surface. Per **N-3**
  and Claude Code's own docs, this is the only list that reliably narrows what can be called;
  putting them anywhere else does nothing.
- Prefer scoping this to **Research runs only** rather than globally, if a per-mode tool surface
  is achievable. Implement and Spike runs have no need of egress. *(Whether litefactory should
  learn about modes to do this collides with `mode-closure/SPIKE.md:70-73` — Open Question 9 in
  the predecessor. Not resolved here.)*
- Constrain `WebFetch` by domain where possible: the rule syntax is **`WebFetch(domain:example.com)`**,
  with `*.example.com` matching subdomains only.
  ([permissions](https://code.claude.com/docs/en/permissions)) Put restrictive rules in the
  **`permissions.deny`** block of the litefactory-controlled settings file, not in
  `allowed_tools` — deny is evaluated **before** ask and allow, and per **N-3** the allow side is
  additive and leaky.
- **`WebSearch` cannot be domain-scoped.** It is all-or-nothing. This is the wider hole of the
  two and there is no mitigation short of not granting it.

**The honest security trade-off.** It is real, and it is a genuine widening — not a formality:

- litefactory's existing secret boundary is **not** an egress boundary. It is (a) an explicit env
  allowlist so the child never receives the daemon's environment (AC-3.13), and (b) `Read` deny
  rules over `$LF_HOME`, `~/.ssh`, `~/.aws`, `~/.gnupg`, `~/.config/gh`, `~/.claude`, `~/.netrc`,
  `./.env*` (`internal/harness/settings.go:83-96`). **Every deny rule is a `Read` rule. There is
  no egress control today, because there is no egress tool today.** Granting one changes the
  shape of the boundary, not just its size.
- The design already understands this. `internal/harness/doc.go:56-59`: *"Redaction is hygiene,
  not a boundary. An adversarial agent can base64 a secret past any pattern matcher… The boundary
  is not giving the subprocess the secret in the first place."* That reasoning is sound and it
  survives — but it was written for a process with no way to talk to the outside world. With
  `WebFetch`, **any file the agent can read, it can also send**, by encoding it in a URL.
- **The concrete exposure is the private repo's source**, plus the branch content of whatever it
  is working on. That is the material risk and it should be stated plainly rather than minimised.
- **One conditional escalation to avoid.** Under `github.token_keychain` the bot token is not in
  the child's environment. Under **`github.token_env`** it is — `doc.go:52-54` says so directly:
  *"the bot token is a variable in the daemon's own environment, and an agent holding Bash can
  simply read it."* Bash alone leaks it to nothing; **Bash plus WebFetch leaks it off the
  machine.** Recommendation: if web tools are enabled, **keep keychain auth and do not use
  `token_env`**.
- A further point that cuts against a naive "it never leaves the machine" defence: WebFetch is
  reported to send the target **hostname to `api.anthropic.com`** for a safety check and to
  summarise fetched content with a **server-side Haiku call**. *(Third-party analysis, not
  official docs — **UNVERIFIED**, flagged because it changes the threat model if true: content
  leaves the machine either way.)*

**Net recommendation.** Reinstate OD-1, scoped: `WebFetch` + `WebSearch` in `--tools` for
Research runs, keychain auth only, `WebFetch` domain-denied down to a documentation allowlist
where practical, and `WebSearch` accepted as an unscopable hole. The threat model moves from
"an agent that can read secrets it was never given" to "an agent that can exfiltrate the repo it
was given" — a real widening, accepted knowingly, in exchange for the class of error this run
just caught twice. **This is a recommendation only. No configuration was changed.**

---

## Recommended scope for Plan mode

The predecessor's four-part scope — **(a)** make plans falsifiable, **(b)** subtract, **(c)**
archive, **(d)** make one gate real — is still the right shape, and **(a)** is still the root
failure. Three amendments from this run:

- **(d) is now conditional.** Do not scope the CODEOWNERS gate until Open Question 1 is answered.
  On Free it is not buildable, and Plan mode would produce another plan for work that cannot be
  done — the exact failure mode F-13 documents.
- **Sub-issues plus issue dependencies is the strongest verified addition**, and is cheap: nine
  REST endpoints in the existing hand-rolled style, and hierarchy is already readable from
  `sub_issues_summary` on a call the client makes today. Promote it from "also in scope, if sized
  separately". Gate it on Open Question 2.
- **"Not Projects v2, because GraphQL" must be dropped from the out-of-scope list as stated.**
  Projects v2 may still be excluded — *"it buys a board nobody has asked to look at"* is a fine
  reason — but excluding it on a technical claim that is now false would put a verified error
  into the plan.

**Still explicitly out of scope:** adopting OpenSpec, Spec Kit, BMAD or Beads as tools; webhooks;
moving plans wholesale out of the repo; rewriting `CLAUDE.md`'s mode model.

**On sizing.** The predecessor's closing note applies to this brief too, and with more force: the
subject is a 503-line plan discarded after twelve minutes. This run's job was to delete wrong
claims, not add new ambition. Four of the ten scope items above are *subtractions* or
*conditions*. A plan that grows in response to this brief has misread it.
