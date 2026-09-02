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
- **[V] DOCX/PPTX cannot be handed to Claude directly** — Phase 2 requires a conversion step (Go library or external binary) before any agent sees usable content, and the strongest Go-native library (`unioffice`) carries a licensing complication for this project. PPTX support in the Go ecosystem is weaker than DOCX across the board.
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

## Recommended scope for Plan mode

**In scope:**
- Phase 1: fetching and surfacing image attachments (mockups) from issue/comment bodies — markdown-URL extraction, authenticated download via the GitHub API (not raw URL caching), storage convention under `$session`, and the new tool-gating/agent-capability work this requires.
- The tool-gating and agent-capability design needed to support Phase 1 safely (label/profile shape, ceiling scoping, which agent or pre-step performs the fetch) — this is architecturally coupled to Phase 1 and should not be deferred.
- A named, explicit decision on the `$session`-files-vs-struct-field extension mechanism (Open Question 4), since it shapes both Phase 1 and Phase 2 work.

**Out of scope for this Plan pass (flag as Phase 2, revisit once Phase 1 ships):**
- DOCX/PPTX conversion tooling and library/binary choice (Open Question 5) — this is a separable, larger piece of work with its own licensing and deployment-footprint tradeoffs, and the issue itself frames it as "in a later setup."
- Chat history / call transcript ingestion (Slack JSON, VTT/SRT) — same reasoning; flagged as future scope, not blocking Phase 1 design.
- General-purpose (non-GitHub-domain) URL fetching for attachments — Open Question 3 should be resolved as "GitHub asset domains only" for the first pass unless the product owner explicitly wants broader fetch now.
