# Research Brief — litefactory-needs-to-accept-attachments

## Request

From GitHub issue #33 ("Litefactory needs to accept attachments"), opened by the project owner:

> A user might want to add mockups or alike with an issue or in a comment. Research how we can enable this and also think about how we should process this. In a later setup, i might want word documents, powerpoints and alike about a gesture to be added here as well - or chat history or call transcripts and alike!

Two phases are implied: **Phase 1** — images/mockups attached to issues or comments. **Phase 2** (explicitly "later") — Word documents, PowerPoint decks, chat history exports, call transcripts.

## Codebase Context

**[V] litefactory currently ingests only the text body of issues and comments — no attachment handling exists anywhere in the repo today.**

- Issues and comments are fetched via `google/go-github/v90` (a Go REST client), wrapped by `internal/github/client.go:74-166`. The auth token is installed once via `gogithub.WithAuthToken()` at `client.go:155`.
- `internal/github/issues.go:43-47` — `Issue.Body string`, documented "verbatim and untrusted." `issues.go:214` (`GetIssue`) / `issues.go:248-256` (`convertIssue`) extract only `Body: payload.GetBody()` (`issues.go:256`) plus title/author/labels/state — nothing else off the payload.
- `internal/github/comments.go:28-30` — `Comment.Body string`, "verbatim and unredacted." `comments.go:58` (`ListCommentsAfter`) / `comments.go:327-333` (`convertComment`) — same pattern, `Body: payload.GetBody()` (`comments.go:333`).
- **[V] No markdown parsing exists anywhere in `internal/github`.** Image/attachment URLs embedded in issue or comment markdown (e.g. `![mockup](https://github.com/user-attachments/assets/...)`) pass through as opaque substrings inside `Body`. Nothing resolves, downloads, or even flags them.
- **[V] No file-download, base64-decode, or multipart-handling code exists in production code anywhere in the repo.** A grep for `base64|multipart|DownloadContents|http.Get|http.NewRequest` across `internal/` turned up exactly one non-test hit, and it is a false positive: `internal/redact/doc.go:16`, a comment about how a *secret* could be obfuscated for the redaction scanner — not real download/decode logic. All existing size/format handling (`internal/report/run.go:31` `maxResultBytes = 32 << 10`; `internal/report/text.go:214-234` `truncateEnd`/`elideMiddle`) is outbound text-byte-budget management for posting GitHub comments, not inbound binary handling.

**[V] Tool-gating mechanism (`lf:tools=<profile>`) — the model any new capability would have to fit into:**

- `internal/daemon/tools.go:30` — `toolLabelPrefix = "lf:tools="`. `tools.go:133-212` `(d *Daemon) resolveTools(labels []string)` resolves the label (case-insensitive prefix, byte-exact key match) into a tool decision; malformed/ambiguous/unknown labels produce explicit reply errors, not silent fallback.
- `internal/config/policy.go:80-82` — `defaultTools()` = `{Read, Glob, Grep, Edit, Write, Bash, Task}`. **No `WebFetch`/`WebSearch` by default.**
- `internal/config/policy.go:273-298` / `:373-409` — every named tool profile is bounded by a `tool_ceiling` that an operator sets once; a profile can only narrow the ceiling, never widen it.
- `internal/config/template.go:143-169` — doc comment states plainly: *"a label is authenticated by nothing, and anyone who can label an issue can select ANY profile below — so it is the ceiling above, not the label, that bounds what a run can do."*
- `.claude/agents/web-researcher.md:18-28` — confirms `--tools`/`--allowedTools` argv (not agent frontmatter) is the real enforcement boundary, and it caps sub-agents too; `WebFetch`/`WebSearch` are admitted only when the issue carries `lf:tools=web`.
- **[V] Implication:** downloading a GitHub attachment is only possible on a run that has been granted outbound fetch capability — today that means `lf:tools=web` or a new equivalent label/profile. There is no separate, always-on network path for this. Any new "fetch an attachment" capability inherits the same unauthenticated-label trust model, which the codebase itself already treats as a known, explicit risk (see Constraints & Risks).

**[V] Session/artifact storage conventions:**

- `.claude/modes/session.md:7-16` derives `$session = .aiteam/$branch/$date/$feature`, shell-literal, and `session.md:29-43` scaffolds `$session/MANIFEST.md` up front.
- `session.md:56-60` — **"`$session` is the only directory under `.aiteam/` that a run reads from or writes to"** — a hard boundary, enforced by convention across all modes.
- No attachments subdirectory convention exists today, and nothing writes binary artifacts into `$session`. But the boundary rule makes `$session/attachments/<n>.<ext>` (or similar) the architecturally consistent place to land downloaded files, since `Read` can already view local images by path and every sub-agent already reads `$session` freely.

**[V] Agent permission model:**

- `.claude/agents/<name>.md` frontmatter (`tools:`, `model:`, `color:`) is documentation only — stated outright at `web-researcher.md:18-28`. Real enforcement is the resolved `--tools` argv (above).
- No existing agent combines "fetch a URL" and "write a file to `$session`." `web-researcher` has `WebSearch`/`WebFetch` but no `Write`. This is a genuine gap: a new capability ("fetch attachment, save under `$session/attachments/`") needs either a new narrow agent/tool with both grants, or an orchestrator-level pre-step that runs before any sandboxed sub-agent is invoked.

**[V] Request-passing mechanism — how "the request" reaches sub-agents today, and what would have to change:**

- `internal/turn/prepare.go:222-240` — the full `harness.PromptInput` struct: `Ref, FeatureSlug, Branch, Mode, RunOrdinal, Instruction, IssueTitle, IssueBody, IssueAuthor, CommentBody, CommentAuthor, BodyIsInstruction, Resume, MayAsk, PreviousMayAsk, PreviousMode, History` — every field is a string, timestamp, or bool. No attachment field of any kind.
- `internal/agent/request.go:51-94` — the final `agent.Request` crossing into a `Runner`: `Ref, RunOrdinal, SegmentOrdinal, Session, Mode, Path, WorktreePath, Prompt string, Permissions`. Again no attachment field; `Prompt` is delivered on stdin, "never in argv" (`request.go:85`).
- CLAUDE.md's own router rule reinforces the existing pattern: *"Pass the orchestrator the request and `$session`. Do not pass it file contents — it reads what it needs."*
- **Lowest-friction extension path**: not a new struct field threaded through `PromptInput` → `agent.Request` → every `Runner` implementation (harness, codex), but new *files* under `$session` (e.g. `$session/attachments/` plus a manifest) populated before orchestrator invocation, discovered by agents reading `$session` — consistent with the existing "it reads what it needs" pattern. A struct-field approach is the alternative but touches the prompt template, `agent.Request.Validate()`, and every `Runner` implementation, a materially larger change.

## Domain / Problem Space

**[V] How GitHub represents attached images/files (verified live against GitHub's changelog and docs):**

- URL scheme changed materially on **May 9, 2023**. Old scheme: `https://user-images.githubusercontent.com/<user-id>/<guid>.<ext>` — long-lived, fully public/unauthenticated, even for private-repo attachments (a real historical leak vector GitHub closed). New scheme: `https://github.com/<owner>/<repo>/assets/<user-id>/<uuid>`, now commonly rendered as the repo-agnostic `https://github.com/user-attachments/assets/<uuid>`; privately these resolve through `https://private-user-images.githubusercontent.com/...?jwt=<short-lived-token>`.
- **For private repos**, a bare unauthenticated fetch of a new-scheme URL will fail — the reliable path is to re-fetch the parent issue/comment via the GitHub API immediately before download, which returns the body with a fresh signed JWT URL. **Attachment URLs should not be cached long-term**; they expire.
- **[W] Unconfirmed/anecdotal**: one community report claims GitHub App installation tokens 404 on attachment URLs where a PAT succeeds — flagged by web-researcher as a single-thread, unverified claim. Worth defensive handling (retry logic, or always route through the API rather than a raw URL) rather than assuming it away.
- **[V] No structured "attachments" field exists on `Issue`/`IssueComment`/`PullRequest`** in REST or GraphQL. Attachments are discoverable **only** by parsing markdown image/link syntax out of the body text for the known URL patterns above. There is no official "list attachments on this issue" API call.
- **[V] GitHub's own upload allow-list already covers Phase 2's formats natively**: images/GIFs (10 MB), video (10 MB free / 100 MB paid), and "all other files" (25 MB) including PDF, DOCX/PPTX/XLSX/XLS/XLSM, ODT/ODS/ODP, RTF, DOC, TXT/MD/CSV/TSV/LOG/JSON, common source extensions, ZIP/GZ/TGZ, and a few others (MSG, EML, DMP, PDB). This means Word docs and PowerPoint decks can already be dropped onto an issue/comment today via the same drag-and-drop mechanism as images — **no new GitHub-side integration is needed to receive them**, only new litefactory-side parsing once fetched.

**[V] What Claude can ingest natively (verified live against Anthropic's current docs):**

- The Messages API `document` content block supports **only `application/pdf` and `text/plain`** (32 MB / 100 pages per PDF). Images (JPEG/PNG/GIF/WEBP) are supported via a separate `image` block on all current models.
- **DOCX/PPTX/XLSX are explicitly not supported** as document blocks. Anthropic's docs state verbatim: convert unsupported types to plain text, or convert DOCX-with-images to PDF first, then use PDF support.
- Claude Code's own `Read` tool (as described in this run's own tool list) matches this: it can view images and PDFs (PDFs capped at ~20 pages/10 at a time per the tool description used in this environment), but has no native DOCX/PPTX path.
- **Net effect**: images and PDFs are a comparatively short path (fetch → save under `$session` → `Read`). DOCX/PPTX/chat-transcript formats all require a **pre-conversion step to plain text (or PDF)** before any agent can usefully consume them.

**[W] Go-side conversion options for Phase 2, surveyed but not exhaustively load-tested this run:**

- `unidoc/unioffice` — most feature-complete pure-Go DOCX/XLSX/PPTX library, but went commercial (AGPLv3 or paid license, metered even for "free" tier) — a licensing blocker for a closed-source product; community forks exist but are unmaintained/unofficial.
- `fumiama/go-docx` — actively maintained, permissively licensed, DOCX-focused.
- PPTX-specific Go tooling is thin (`kenny-not-dead/gopptx`, `baliance.com/gooxml`) with low maintenance signal; `chchench/textract` is a minimal DOCX/PPTX/XLSX plain-text extractor worth evaluating.
- A dependency-free fallback: DOCX/PPTX are ZIP containers of XML; stdlib `archive/zip` + `encoding/xml` can pull `word/document.xml` / `ppt/slides/slideN.xml` and extract text runs directly, sidestepping licensing risk at the cost of writing a thin custom XML walker.
- The more commonly used pattern in practice, per the survey, is **shelling out** to LibreOffice headless (`soffice --headless --convert-to pdf/txt`) or `pandoc` — better format fidelity, especially for PPTX, at the cost of an external-binary runtime dependency (would need to be present wherever litefactory's sandboxed runs execute).

**[V] Chat history / call transcript formats to anticipate:**

- Slack exports are JSON, one file per channel per day, in a documented folder structure.
- Zoom cloud-recording transcripts are natively WebVTT (`.vtt`) only. Teams and other tools commonly produce VTT or SRT; third-party transcription tools add JSON/DOCX/TXT/PDF variants.
- Go tooling: `asticode/go-astisub` is the most comprehensive, actively-used library, covering SRT/WebVTT/SSA/TTML/STL and handling edge cases (cue settings, styling, BOM) already — a bespoke parser should not be necessary for Phase 2's transcript formats.

## Constraints & Risks

- **[V] No outbound network path exists for any run unless a tool-gating label already grants it.** `lf:tools=web` is currently the only such label; attachment downloading needs an equivalent, explicitly-granted capability (new profile, or extended `web` profile) — it cannot be assumed present.
- **[V] The label-trust model is inherently unauthenticated** — `internal/config/template.go`'s own doc comment states anyone who can label an issue can select any configured profile; the operator-set `tool_ceiling` is what actually bounds risk. A new "fetch attachment" capability sits inside this same model, and needs the same ceiling discipline. This raises an SSRF-adjacent concern: an attachment "URL" embedded in an untrusted issue body is attacker-influenceable input to a fetch — worth constraining to GitHub's own known asset domains rather than fetching arbitrary URLs found in body text.
- **[V] Private-repo attachment URLs expire and require re-fetching the parent issue/comment via the API for a fresh signed URL** — a naive "grab the URL once, download later" design will intermittently fail.
- **[W] Possible GitHub App installation-token gap on attachment URLs** — unverified, single community report, but worth defensive handling (API-mediated fetch, PAT fallback) rather than an assumption either way.
- **[V] No agent today combines fetch + local-write capability.** Adding attachment support needs either a new narrow agent/tool, or a pre-orchestrator step (outside the current sandboxed sub-agent model) — this is an architectural decision, not just a config change.
- **[V] DOCX/PPTX cannot be handed to Claude's `Read` tool directly** and require a conversion step before any agent sees usable content — **superseded by the Addendum below**: this conversion does not need to be a litefactory-side Go dependency (`unioffice` or otherwise). The corrected model has the invoked agent perform the conversion itself at runtime via its existing `Bash` access (shelling out to `pandoc`), the same pattern Anthropic's own official Skill uses. The real, unresolved dependency is operational — whether `pandoc`/LibreOffice is installed on the deployment host — see Addendum A2 and new Open Questions 8-11.
- **[V] Nothing in the request-passing chain (`PromptInput`, `agent.Request`) currently carries attachment references** — this is a real gap, not just missing convenience code, and the two extension paths (new `$session` files vs. new struct fields threaded through every `Runner`) have materially different blast radius.
- **[R0] None** — this is the first research pass on this feature; there is no prior-run context to carry forward.

## Open Questions

1. Should attachment ingestion happen **before** the orchestrator/sub-agent chain runs at all (a router-level pre-step, outside the sandboxed tool-call model), or as a new sandboxed capability granted to a specific sub-agent? This is a first-order architecture decision the codebase tour could not resolve on its own — it depends on how much litefactory's owners want attachment-fetching to be reviewable/auditable versus fast to add.
2. What label/profile scheme should gate attachment downloading — reuse `lf:tools=web`, or introduce a distinct `lf:tools=attachments` (or similar) profile with a narrower ceiling (e.g. GitHub asset domains only, no general WebFetch)? The security posture argues for the narrower option, but that's a product decision, not just a technical one.
3. Should the fetch target allow-list be restricted to GitHub's own attachment domains (`github.com/user-attachments/...`, `*.githubusercontent.com`), or should it be a general-purpose fetch? Restricting reduces SSRF exposure but forecloses other attachment sources (e.g. a linked Google Doc or Confluence page) that a user might reasonably paste instead of uploading.
4. Is `$session/attachments/` (new files, discovered by convention) the intended extension mechanism, or does the team want a first-class `AttachmentPaths`/manifest field threaded through `PromptInput`/`agent.Request`? This wasn't decided by this research and materially changes the size of the Plan-mode implementation.
5. For Phase 2 format conversion (DOCX/PPTX), does the project prefer a pure-Go dependency (accepting `unioffice`'s licensing terms, or a weaker free library/stdlib XML-walk with narrower fidelity) or an external-binary dependency (LibreOffice/pandoc, requiring it to be present in the run's sandbox/container)? This is a build/deploy-footprint decision as much as a code decision.
6. Is the anecdotal GitHub App installation-token gap on attachment URLs (Open Question in Domain section) something the team has independently observed with litefactory's own bot credentials? If not already known, this should be smoke-tested early in Plan/Implement rather than assumed away.
7. What size/format allow-list should litefactory itself enforce on ingestion (independent of GitHub's own 10/25 MB upload caps) — e.g. a smaller cap to avoid blowing through Claude's PDF/image size limits, or a rejection path for formats litefactory can't yet process (Phase 1 ships images only; what happens when a user attaches a PDF or DOCX before Phase 2 lands)?

## Addendum — Owner Pushback (2026-09-02)

The project owner challenged two claims in the sections above via a comment on issue #33:

> "ok, what is a paid conversion tooling? i think every agent can read powerpoint or word files if we attach them? litefactory would only pass them on, not parse them ...?"

This addendum answers both points with new research. **It corrects, rather than restates, the original brief's Phase 2 recommendation** — see "Corrected recommendation" below. The line items it supersedes are marked inline above; this section is the authoritative version going forward.

### A1. What was the "paid conversion tooling"?

**[V] The library referenced was `unidoc/unioffice` (github.com/unidoc/unioffice)**, named in the original Domain section ("Go-side conversion options") as "the most feature-complete pure-Go DOCX/XLSX/PPTX library."

- **[V] It is a commercial product, full stop.** Its own `LICENSE.md` states: *"This software package is a commercial product and requires a license code to operate."* Unlike UniDoc's separate `unidoc/unidoc` PDF library (which is dual-licensed with an AGPL option), `unioffice` has **no open-source/AGPL escape hatch** — a closed-source project cannot use it for free by accepting AGPL's copyleft terms, because that option doesn't exist for this specific library.
- **[V] Pricing is two-track**, per unidoc.io/pricing (checked live, September 2026):
  - **Cloud metered:** Free = 20 credits/month; Starter $20/mo (150 credits, $0.15/doc); Pro $100/mo (1,000 credits, $0.10/doc); Scale $300/mo (4,000 credits, $0.08/doc); Enterprise $500/mo (10,000 credits, $0.05/doc).
  - **Offline yearly licenses** (Business / Gold / Platinum tiers, scaling by developer seats, source obfuscation, SLA): dollar figures are quote-only, not published.
- **[V] Why it was flagged as "strongest":** it was the only surveyed Go library judged feature-complete across all three OOXML formats (DOCX/XLSX/PPTX) with active maintenance — the free/permissive alternatives found (`fumiama/go-docx`, `gopptx`, `gooxml`, `textract`) are each narrower in scope, less actively maintained, or DOCX-only.

### A2. Re-examining "litefactory would only pass them on, not parse them"

**[V] The owner's premise is substantially correct for this project's actual invocation model — the original brief's framing was misleading, and its recommendation needs correcting.**

The original brief's claim ("DOCX/PPTX cannot be handed to Claude directly," "only PDF and plain-text are supported") was true of the **raw Anthropic Messages API `document` content block** — but litefactory does not call that API directly. `codebase-tour` confirmed both agent paths spawn a **local CLI subprocess**, not a hosted API call:

- **[V] Claude path:** `internal/harness/runner.go:478` runs the resolved `claude` binary via `exec.CommandContext`, prompt piped on stdin (`runner.go:483`), CWD set to the task's git worktree (`runner.go:479`).
- **[V] Codex path:** `internal/codex/runner.go:130` runs the resolved `codex` binary the same way (`exec.CommandContext`), prompt on stdin (`runner.go:131`).
- **[V] The spawned `claude` process's tool boundary (`--tools`) includes both `Bash` and `Read` by default** — `internal/config/policy.go:80-82`, enforced as the actual argv boundary at `internal/harness/runner.go:119-124`. This holds for the top-level spawned process itself, not just Task-spawned sub-agents inside it.
- **[V] Codex has no equivalent per-tool allow list** — its only litefactory-configured boundary is `--sandbox <read-only|workspace-write>` plus `--add-dir` writable roots (`internal/codex/runner.go:187-203`); Codex's own CLI decides internally what shell/file operations are permitted inside that sandbox mode. This is a real asymmetry between the two paths worth carrying into Plan mode.
- **[V] No code exists today that downloads or writes a raw attachment to `$session/` or the worktree** — the owner's "just pass it on" model is unimplemented, not contradicted, by the current codebase.

Given that, the question is not "does the Anthropic API understand DOCX" (it doesn't, but that's the wrong question) — it's "can the spawned `claude`/`codex` process, using tools it already has, read a DOCX/PPTX file sitting in its own worktree." New web research settles this:

- **[V] Claude Code's `Read` tool rejects `.docx` outright** — confirmed via a live, primary-source GitHub issue (anthropics/claude-code #35911, filed March 2026 against Claude Code CLI + Opus 4.6, closed as duplicate — meaning the limitation is acknowledged, not fixed): the tool returns *"This tool cannot read binary files... Please use appropriate tools for binary file analysis."* **[W] `.pptx` was not independently tested but is the same OOXML zip-of-XML format family; treat as the same limitation by extension, not as separately confirmed.**
- **[W] Codex CLI's own docs do not describe a dedicated document-reading tool with DOCX/PPTX support** — thin official docs, absence of a feature isn't proof of absence, but nothing found contradicts the same limitation applying.
- **[V] However — and this is the correction — both agents have `Bash`, and the sanctioned, real-world pattern for exactly this problem is to shell out to a converter, not to hand the vendor's `Read` tool a raw OOXML file.** Anthropic's own official `docx` Skill (`anthropics/skills`, `skills/docx/SKILL.md`, source-available) does precisely this: its documented read path is the literal shell command `pandoc -t markdown file.docx`, with `pandoc`, LibreOffice (`soffice`), and Poppler (`pdftoppm`) as declared dependencies; for structural edits it unzips the OOXML container directly (`python scripts/office/unpack.py`) since DOCX/PPTX are just ZIP archives of XML. **This confirms the owner's underlying instinct — the agent does the conversion, not litefactory — while correcting the mechanism: it happens via the agent's `Bash` tool shelling out to a converter already in its runtime, not via any native "read a Word doc" capability in the `Read` tool or the Messages API.**

**[W] Two things remain genuinely unresolved, and both are operational rather than code questions:**

1. Whether Anthropic's `docx`/`pptx` Skills (and their pandoc/LibreOffice/Poppler dependency) are present **by default** in a plain Claude Code CLI session or must be explicitly installed — sources conflict, none primary/authoritative enough to settle here.
2. Whether `pandoc`, LibreOffice, or Poppler are actually installed on the host(s) where litefactory spawns these processes. `codebase-tour` found **no container/Dockerfile evidence anywhere in the repo** — the daemon runs as a bare macOS host process under launchd (`internal/config/executable.go:13-19`, `internal/harness/runner.go:73-75`), with an explicitly allowlisted `PATH` passed through to the child (`internal/harness/env.go`, `internal/codex/runner.go:266-275`) — so whatever conversion binaries exist on that host's `PATH` is what the agent's `Bash` tool would actually have access to. **This was not verifiable from the repository and needs to be checked against the actual deployment host(s), not assumed either way.**

### Corrected recommendation

**The original brief's Phase 2 framing — "litefactory needs to build or license a Go-native DOCX/PPTX conversion dependency (`unioffice` or a custom XML walker) before any agent can use these attachments" — is superseded.** It conflated the Anthropic Messages API's document-block limitation (real, but not what this project calls) with what the actually-invoked `claude`/`codex` CLI subprocesses can do once handed a raw file and their existing `Bash` access.

The corrected model: **litefactory downloads the raw attachment byte-for-byte and lands it under `$session/` (or the worktree) with no litefactory-side parsing or conversion — exactly the owner's proposal — and the invoked agent converts it itself at runtime by shelling out to `pandoc` (or an equivalent already-installed tool), the same pattern Anthropic's own official Skill uses.** This means:

- **No Go DOCX/PPTX library is required as a mandatory litefactory dependency.** `unioffice`'s licensing cost and the free-alternative library survey in the Domain section above are **no longer load-bearing for the recommended path** — they'd only become relevant again if the team specifically wanted litefactory itself to pre-render a preview or validate attachment content server-side, which is a different, optional feature, not a Phase 2 prerequisite.
- **The dependency shifts from a Go library choice to an operational one:** whether `pandoc` (or LibreOffice/Poppler) is installed on the host(s) that spawn `claude`/`codex` processes. This is materially cheaper to satisfy (a host package-manager install, `brew install pandoc` on the current macOS/launchd deployment) than licensing or vendoring a commercial Go library — but it is a real, unverified dependency, not a given.
- **This does not change Phase 1** (images) at all — images were already agent-readable via the existing `Read` tool's native image support; nothing above touches that path.

### New Open Questions (append to the section above)

8. Is `pandoc` (or LibreOffice/Poppler) actually installed on the host(s) where litefactory's daemon runs and spawns `claude`/`codex` subprocesses today? This is now the load-bearing dependency for Phase 2 DOCX/PPTX support and was not verifiable from the repository — check the deployment host directly before Plan mode commits to this approach.
9. Are Anthropic's official `docx`/`pptx` Skills available by default in the specific Claude Code CLI version litefactory invokes, or would they need explicit installation/enabling as part of Plan/Implement? Unresolved by this run; would need Anthropic's own Claude Code docs/changelog rather than secondary sources.
10. Does the Codex path need different handling than Claude given the tool-gating asymmetry (Codex has no `--tools`/`--allowedTools` equivalent — only `--sandbox` mode governs shell access)? If Codex's sandbox mode is `read-only` for a given run, would that block the `Bash`-based pandoc conversion pattern entirely for Codex-driven runs even where Claude-driven runs would succeed?
11. Should litefactory's own ingestion step verify a converter is reachable (e.g., a startup/health check for `pandoc` on PATH) and fail loudly if absent, rather than relying on the agent's own run-time discovery that conversion tooling is missing?

## Recommended scope for Plan mode

**In scope:**
- Phase 1: fetching and surfacing image attachments (mockups) from issue/comment bodies — markdown-URL extraction, authenticated download via the GitHub API (not raw URL caching), storage convention under `$session`, and the new tool-gating/agent-capability work this requires.
- The tool-gating and agent-capability design needed to support Phase 1 safely (label/profile shape, ceiling scoping, which agent or pre-step performs the fetch) — this is architecturally coupled to Phase 1 and should not be deferred.
- A named, explicit decision on the `$session`-files-vs-struct-field extension mechanism (Open Question 4), since it shapes both Phase 1 and Phase 2 work.

**Out of scope for this Plan pass (flag as Phase 2, revisit once Phase 1 ships):**
- DOCX/PPTX handling (Open Questions 5, 8-11) — still separable, larger work, and the issue itself frames it as "in a later setup." **Note the scope has shifted per the Addendum**: this is no longer primarily a Go-library licensing decision (`unioffice` vs. free alternatives) but an operational one — confirming a converter (`pandoc`/LibreOffice) is available on the deployment host so the invoked agent's own `Bash` tool can convert the file at runtime, plus landing the raw attachment file where that agent can reach it. Plan mode should re-scope this item accordingly rather than carrying forward the original "which Go library" framing.
- Chat history / call transcript ingestion (Slack JSON, VTT/SRT) — same reasoning; flagged as future scope, not blocking Phase 1 design.
- General-purpose (non-GitHub-domain) URL fetching for attachments — Open Question 3 should be resolved as "GitHub asset domains only" for the first pass unless the product owner explicitly wants broader fetch now.
