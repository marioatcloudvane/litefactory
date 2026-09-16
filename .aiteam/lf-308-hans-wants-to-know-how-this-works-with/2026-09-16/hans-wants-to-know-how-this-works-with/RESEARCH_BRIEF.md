# Research Brief — #308 "Hans wants to know how this works with on-prem"

Revision cited throughout: `eb893565687d582bcb79d75edb14023881d0dd73` (HEAD at run time).

## Status: incomplete — blocked on unresolved ambiguity

## Request

Issue body (verbatim, the only instruction given): "Evaluate how we can plug in
pydev". Title (context, not instruction): "Hans wants to know how this works
with on-prem". No comments on the issue (`gh issue view 308` — comments: []).
Labels: `Coding Agent: Claude`, `Mode: Research`. Opened by
@marioatcloudvane.

## Current behavior / evidence

### "pydev" — no repo evidence found
- Full-repo case-insensitive search for `pydev`, `PyDev`, `Pydev`: zero matches
  anywhere in the tree (source, docs, `.aiteam` archives, `teams/`).
- Full-repo case-insensitive search for `Python`: matches only in archived
  `.aiteam/**` session artifacts from unrelated prior issues (research briefs,
  spec docs) and a handful of non-semantic hits (`internal/config/config.go`,
  `docs/workflow/owner-exceptions.md`, `.github/workflows/team-content.yml`) —
  none referring to a "pydev" tool, a Python coding-agent host, or a Python
  developer workflow integration.
- litefactory's own coding-agent host abstraction is documented at
  `AGENTS.md:99-107` and `.aiteam/APP_CONTEXT.md:81,94`: `internal/harness`
  runs Claude Code, `internal/codex` runs Codex CLI; both implement
  `internal/agent.Runner`. `.aiteam/APP_CONTEXT.md:94` states the extension
  point explicitly: "add a new coding-agent-tool implementation →
  `internal/agent`'s `Runner` interface | Zero vendor-naming allowed in any
  exported identifier (enforced: `purity_test.go`)." Only two `Runner`
  implementations exist today (`internal/harness`, `internal/codex`); no
  third package for any other host exists under `internal/`.
- `internal/render` (registry at `internal/render/registry.go`) additionally
  has renderer targets for `claude`, `codex`, `opencode`, and `native`
  (`internal/render/opencode.go` exists) — i.e. agent/skill *definitions* can
  already be rendered for an "OpenCode" target, but there is no corresponding
  `internal/opencode` runner package that launches or drives an OpenCode (or
  any Python-based) process. This is a definition-rendering target, not a
  running host integration.
- The most recent, directly relevant prior work is issue #274 "Native host
  integration and verification" (merged: commits `8681723`, `659c723`,
  `987ab77`), which wired the existing MCP adapter into Claude Code's
  `--mcp-config` and Codex CLI's `mcp_servers` launch configuration
  (`internal/harness/runner.go:629`, `internal/codex/runner.go:398/407`).
  That work is scoped strictly to Claude Code and Codex CLI; it does not
  mention Python, PyDev, or any third coding-agent host. Nothing in #274 or
  its research chain (#265, #272, #273) references "pydev".
- Conclusion: nothing in the repository names, documents, or hints at a
  product/tool called "pydev". The architecture has a documented, general
  extension seam for adding a new coding-agent host (`agent.Runner`), which
  would be the relevant mechanism *if* "pydev" turns out to mean a
  Python-based coding-agent CLI/host — but that is an inference from the
  general architecture, not evidence that "pydev" was ever considered or
  named anywhere in-repo.

### "on-prem" — no repo evidence found
- Full-repo case-insensitive search for `on-prem`, `on prem`, `on-premise`,
  `onprem`, `self-host`, `self host`, `air-gap`, `air gap`, `deploy`: zero
  matches in source or docs (the one `deploy` hit was in `README.md`'s prose
  about "deploy" only inside the on-prem search which returned no hits at
  all — i.e., truly zero).
- `.aiteam/APP_CONTEXT.md` (149 lines) and `.aiteam/APP_CONSTRAINTS.md` (329
  lines), the project's own architecture/constraint ledgers, contain no
  on-prem, self-hosted, air-gapped, or deployment-model language.
- Structurally, litefactory is already a locally-run daemon: it drives a
  locally installed `claude` or `codex` CLI subprocess inside a git worktree
  on the operator's own machine (`AGENTS.md:1-8`, `.aiteam/APP_CONTEXT.md:8`).
  There is no SaaS/cloud-hosted component described anywhere in the docs
  read. This is background architecture, not a targeted answer to whatever
  "on-prem" constraint or question Hans has — the issue gives no indication
  of what on-prem concern (model hosting? network isolation? private Git
  hosting? licensing?) is actually being asked about.
- The title's "on-prem" phrase is not echoed anywhere in the issue body, so
  it is uncertain whether "how this works with on-prem" is even the actual
  question, or leftover/paraphrased framing from whoever titled the issue
  (opened by @marioatcloudvane, not attributed to Hans directly in the body).

## Options

Because the instruction is the sole source of intent and gives zero
disambiguating signal, and the repo contains no reference to "pydev" or
"on-prem" to narrow it, at least three materially different research targets
are plausible:

1. **PyDev (Eclipse Python IDE plugin)** — evaluate whether/how litefactory
   could integrate with or be invoked from the PyDev Eclipse plugin (e.g., as
   an editor-triggered workflow). No evidence of an IDE-plugin story anywhere
   in this repo.
2. **A Python developer workflow** — evaluate how litefactory (a Go backend
   driving Claude Code/Codex CLI agents) would work for a team/repo whose
   primary language is Python, or whether litefactory needs Python-specific
   tooling support (linters, test runners, etc.) analogous to its Go-specific
   `go vet`/`golangci-lint` assumptions baked into `AGENTS.md`/`Makefile`.
3. **A specific named tool "pydev"** (internal, third-party, or a
   Python-based coding-agent CLI/host analogous to Claude Code/Codex) — if
   this is a real product Hans wants litefactory to support as a third
   `agent.Runner` host, the relevant extension point is
   `internal/agent.Runner` (see evidence above), but confirming this
   reading requires knowing what "pydev" actually is.

None of these can be selected from repository evidence; the answer changes
the entire research scope (IDE-plugin question vs. language-support question
vs. new-host-integration question).

## Recommendation

Do not proceed on any one reading. Ask the issue owner (or whoever can speak
for Hans) to disambiguate before further research is productive, since
guessing risks fully researching the wrong target.

## Risks

- Researching "how to add pydev as a new agent.Runner host" when Hans
  actually meant "does litefactory work for a Python codebase" (or vice
  versa) would produce a materially wrong, wasted research artifact.
- "On-prem" may be a separate, independent question from "pydev" (title vs.
  body may not be about the same thing), so even resolving "pydev" alone may
  leave the on-prem half of the title unanswered.

## Decisions

### Open
1. What does "pydev" refer to in this issue? (PyDev Eclipse plugin / a
   Python-based coding-agent host or CLI litefactory should support as a
   third `internal/agent.Runner` implementation / general Python-language
   developer support in repos litefactory manages / an internal or
   third-party product named "pydev" / something else.)
2. What does "on-prem" mean here, and is it a separate concern from
   "pydev", or does resolving "pydev" answer it? (e.g., private/self-hosted
   Git forge instead of github.com, network-isolated or air-gapped
   deployment, local-only model/LLM providers, licensing/data-residency for
   an on-prem customer.)
3. Who is "Hans" relative to this request (customer, prospect, internal
   stakeholder), and is there a source (conversation, ticket, email) outside
   GitHub that has the missing detail and could be pasted into the issue?

### Answered
None yet.

## Sources

- `AGENTS.md` (full file read)
- `README.md` (full file read)
- `.aiteam/APP_CONTEXT.md` (grepped for `Claude|Codex|IDE|provider|deploy|host`)
- `.aiteam/APP_CONSTRAINTS.md` (size-checked; not fully read, no on-prem/pydev
  hits from repo-wide grep that would justify full read)
- `internal/render/` directory listing (`registry.go`, `opencode.go`,
  `claude.go`, `codex.go`, `native.go`)
- `internal/harness/`, `internal/codex/` directory listings
- `gh issue view 308 --json title,body,labels,comments` (revision: live
  GitHub state at run time, 2026-09-16T08:34:40Z)
- `gh issue view 274 --json title,body,labels` (revision: live GitHub state
  at run time, 2026-09-16T08:34:40Z)
- `git log --oneline -20` at `eb89356` (repo history for #274/#272/#273 MCP
  and native-host-integration chain)
- Repo-wide `Grep` (case-insensitive) for: `pydev`, `on-prem`/`on prem`/
  `on-premise`/`onprem`, `deploy|self-host|air-gap`, `\bPython\b`, `opencode`
