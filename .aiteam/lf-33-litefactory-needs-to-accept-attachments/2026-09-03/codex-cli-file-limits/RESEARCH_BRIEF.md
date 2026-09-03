# Research Brief — codex-cli-file-limits

## Request

Narrow Plan-mode detour on issue #33 (attachment ingestion). litefactory spawns
local `claude` and `codex` CLI subprocesses and hands them file paths to read via
each CLI's own built-in file-reading tool (not the raw vendor chat-completions
API). We already know, from prior research this session, that Claude Code's
`Read` tool caps PDFs at ~20 pages per invocation (paging through 10 at a time
for larger PDFs) and refuses `.docx` outright as an unreadable binary file.

Four questions, to set litefactory's own ingestion-time size-based rejection
thresholds:

1. Does Codex CLI have a built-in file-reading tool analogous to Claude Code's
   `Read`? What image formats and PDF handling does it support natively?
2. What size limits (byte, page-count, resolution/pixel) apply to images and
   PDFs read by Codex CLI's own tooling?
3. Does Codex CLI reject DOCX/PPTX the same way Claude Code does?
4. (Secondary) Is there a documented raw byte-size cap on images/PDFs for
   Claude Code's `Read` tool, independent of the ~20-page PDF cap?

## Codebase Context

N/A — this detour is purely about third-party CLI tool behavior (`codex-rs`
upstream source, Anthropic's Claude Code and Vision docs). No `codebase-tour`
was run; the question does not turn on litefactory's own code, and the parent
session already covers litefactory's ingestion surface.

## Domain / Problem Space

**[W] 1. Codex CLI's file-reading tool.** Codex CLI (`openai/codex`, Rust-based)
has a dedicated, agent-invoked **`view_image`** tool
(`VIEW_IMAGE_TOOL_NAME`, `codex-rs/core/src/tools/spec.rs` /
`codex-rs/protocol/src/models.rs`), described as "View a local image file from
the filesystem when visual inspection is needed. Use this for images already
available on disk." (confirmed via PR #23949, merged 2026-05-26; in use since
at least codex-cli 0.104.0 per issue #12439). No distinct, dedicated tool for
reading arbitrary text/PDF/office files was confirmed — evidence points to
Codex relying on shell access (`cat`, `sed`) and its `apply_patch`/shell tool
for text-file reads, not a structured `Read`-equivalent. This could not be
fully ruled out: the `codex-rs/core/src/tools/` directory could not be
enumerated completely (raw GitHub fetches 404'd during research).

**[W] 2. Image formats and PDF handling.** The claim that `view_image`
supports PNG/JPEG/WEBP/GIF came from an aggregated web-search summary, not a
primary Codex source — **flagged unreliable**, likely blended from OpenAI's
general Vision/Files API docs rather than Codex-CLI-specific documentation.
PDF handling: **[V] Codex CLI does not support reading PDFs natively.**
`openai/codex#1797` ("PDF support"), opened 2025-08-02, is an open,
unresolved feature request asking that Codex be able to read text/pictures/
charts/tables in PDFs — confirming it cannot today. No page-count analog to
Claude Code's ~20-page cap exists, because PDF reading isn't supported at all.

**[W] 3. Size limits enforced by Codex CLI's own tooling.** No official
numeric cap (docs/README/CHANGELOG) was found. Real-world evidence
(`openai/codex#13508`, Codex desktop app v26.303.1606, reported 2026-03-04):
attaching a 134 MB PNG produces an upstream HTTP 413 "Payload Too Large" that
**corrupts/freezes the session** rather than being cleanly rejected at
ingestion — the issue explicitly asks OpenAI to add a client-side
max-attachment-size check because none currently exists. This strongly
suggests **Codex CLI performs no pre-flight size check of its own** and
depends entirely on the upstream API to reject oversized files, with a poor
failure mode when it does. This is desktop-app evidence, not CLI-specific,
but the attachment pathway is understood to be shared.

**4. DOCX/PPTX rejection behavior — not settled.** No issue or doc was found
describing Codex CLI's own file-reading pathway encountering a `.docx`/`.pptx`
and returning an error comparable to Claude Code's "This tool cannot read
binary files" refusal. Adjacent but non-equivalent findings: Codex **Desktop's
GUI file previewer** (not the agent tool) fails on some `.docx` files with
generic XML-parse errors (`#26353`); Codex-**generated** `.pptx` exports
sometimes trigger PowerPoint repair-dialog warnings on open (`#16315`) — an
output-quality issue, not input rejection. Separately, OpenAI's general
Responses/Azure API documents `.docx`/`.pptx` as **unsupported input file
types** entirely, but that is the raw API, not Codex CLI specifically.

**[V] 5. Claude Code / Claude API image byte-size cap.** Anthropic's
official Vision docs (`platform.claude.com/docs/en/build-with-claude/vision`,
fetched 2026-09-03) state: **max image size 10 MB (base64-encoded) via the
direct Claude API and via claude.ai; 5 MB (base64-encoded) via Amazon Bedrock
/ Google Cloud Vertex.** Max image dimensions **8000×8000 px**; when a single
request holds more than 20 images/documents, a stricter per-image pixel limit
applies and oversized images are rejected with `invalid_request_error`
("many-image requests"). Supported formats: **JPEG, PNG, GIF, WebP**
(animated formats: first frame only). This is a Messages-API-level limit, not
stated verbatim as "the Read tool's limit" in Claude Code's own docs — Claude
Code very likely inherits it since it sends images as the same `image`
content blocks, but that linkage is an inference. **[W]** Real-world Claude
Code GitHub issues (`#2146`, `#20021`) show users hitting exactly a 5.0 MB
base64 cap in practice, consistent with a Bedrock/Vertex-backed session rather
than the 10 MB direct-API tier — the effective cap depends on which backend
litefactory's Claude Code invocation routes through. No distinct PDF
byte-size cap (independent of the ~20-page limit) was found in the Vision
docs page.

## Constraints & Risks

- **Codex CLI has no confirmed client-side size guard.** If litefactory hands
  Codex a large image (and, per #13508, this can hang/corrupt the whole
  session rather than fail cleanly), the failure mode is worse than a clean
  rejection — this is a strong argument for litefactory enforcing its own
  byte-size cap *before* invoking Codex, using a conservative threshold, since
  Codex will not protect itself.
- **PDFs must not be handed to Codex CLI at all** — there is no read path for
  them today (`#1797` open). Any PDF ingestion story needs either a
  Codex-specific carve-out (reject/convert before hand-off) or must be scoped
  to Claude Code only.
- **DOCX/PPTX rejection for Codex CLI is unverified.** litefactory cannot
  currently assume Codex will reject these the same way Claude Code does; the
  safe default is to apply litefactory's own extension-based rejection (or
  conversion) ahead of hand-off for both agents, rather than relying on
  agent-side behavior for Codex.
- **Claude Code's effective image cap depends on backend.** If litefactory's
  Claude Code invocation is Bedrock/Vertex-backed, budget for 5 MB
  (base64-encoded); if direct Anthropic API, 10 MB. Since base64 encoding
  inflates raw bytes by ~33%, the *raw file* threshold should be set below
  the base64 cap accordingly (roughly 7.5 MB raw for the 10 MB base64 tier,
  ~3.75 MB raw for the 5 MB tier) — litefactory should confirm which backend
  its Claude Code invocation actually uses before picking a number.
- **Codex CLI is a fast-moving target.** Tool names/descriptions have already
  changed at least once during this research window (PR #23949); any
  threshold litefactory sets from today's findings should be revisited if
  Codex CLI is upgraded.

## Open Questions

- What exact image-format allowlist and size/resolution cap does Codex CLI's
  `view_image` tool enforce, if any — needs verification directly against
  `codex-rs` source (the `codex-rs/core/src/tools/` directory could not be
  fully enumerated this run) rather than aggregated web summaries.
- Does Codex CLI have any dedicated structured tool for reading text files
  (as opposed to shell `cat`/`sed` access), and if so, does it carry its own
  size/line limits comparable to Claude Code's `Read`?
- What does Codex CLI's agent tool actually do when pointed at a `.docx` or
  `.pptx` path — clean rejection, silent garbage, or a crash/hang similar to
  the oversized-image case in `#13508`? No direct evidence found either way.
- Which backend does litefactory's own Claude Code invocation route through
  (direct Anthropic API vs. Bedrock vs. Vertex) — this determines which of
  the 10 MB / 5 MB base64 image caps actually applies in production.
- Is there a distinct PDF byte-size cap (independent of the ~20-page limit)
  for Claude Code's `Read` tool? Not found in the Vision docs page consulted;
  may require checking Claude Code's own repo/CHANGELOG rather than the
  general Messages API docs.

## Recommended scope for Plan mode

Plan mode should treat Codex CLI's file-handling as **more restrictive and
less self-protecting** than Claude Code's: no PDF path exists at all, no
confirmed client-side size guard exists for images, and DOCX/PPTX behavior is
unverified. The safe, implementable design is for litefactory itself to apply
its own extension allowlist and byte-size cap at ingestion time — before
handing any file to either subprocess — rather than depending on each CLI's
own tool to reject what it cannot handle. Plan mode should not assume feature
parity between the two agents' file-reading capabilities; where they diverge
(PDFs, DOCX/PPTX), litefactory's ingestion policy should be the single source
of truth, not the invoked agent's own error handling. The open questions above
(especially Codex's true `view_image` limits and its DOCX/PPTX behavior)
should be treated as unresolved for now, not blocking, given the
conservative fallback of litefactory enforcing its own caps regardless.
