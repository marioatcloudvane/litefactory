# US-4 — Ingest and hand off PDF/DOCX/PPTX documents

> As a run triggered from an issue or comment carrying a spec PDF, a Word
> document, or a slide deck, I want litefactory to fetch that document and tell
> the agent where to find it, so that the agent can work from the actual
> requirements document instead of a paraphrase in the issue body.

Delivers the same fetch-land-handoff pipeline as US-3, extended to PDF, DOCX and
PPTX, with a document-sized byte ceiling in place of the image ceiling.

## Why This Matters

Specs, one-pagers and slide decks land on issues just as often as screenshots do,
and today none of them reach the agent either. This story is the owner's own
answer to "what should phase 1 ship": images plus PDF, DOCX and PPTX, nothing
wider. It deliberately does not teach litefactory to read or convert these files —
that stays the invoked agent's job, using whatever shell access it already has —
so what this story actually delivers is confidence that the file arrives intact
and the agent is told it is there; whether Claude's or Codex's own tooling can
open it once it arrives is a real, currently unverified gap, not something this
story can close from litefactory's side.

## Architecture Notes

- Purely additive over US-3's pipeline: same fetch, same sniff-then-classify,
  same stream-time size-abort mechanism — different type set (PDF/DOCX/PPTX magic
  bytes) and a different ceiling constant (`attachment_max_document_bytes`,
  10 MiB default, vs. the image ceiling).
- Explicitly out of scope for this story and this plan: no `pandoc`/LibreOffice/
  Poppler install or health-check, no Go DOCX/PPTX parsing dependency, no
  litefactory-side conversion of any kind. If the invoked agent converts the
  file, it does so with its own shell access, not litefactory's help.
- Carried risk (unmitigated by design): Claude Code's and Codex's actual ability
  to read a landed PDF, or usefully handle a DOCX/PPTX, is not confirmed —
  the only evidence is third-party reports gathered in the 2026-09-03 detour
  research brief. This story ships the fetch/land/prompt-mention plumbing
  regardless; it does not gate ingestion on proof the receiving agent can act on
  the file.
- Carried risk: even if a conversion tool is present on the host, the coding
  agent's own sandbox may block the agent from invoking it. Also accepted, not
  mitigated here.
- Depends on US-3 — this story adds no new insertion point, no new landing path,
  no new prompt field.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-4.1 | Extend sniffing to PDF/DOCX/PPTX magic bytes, additive over US-3's image sniffing | code | US-3 | M |
| T-4.2 | Apply `attachment_max_document_bytes` ceiling via the same stream-abort mechanism as US-3 | code | T-4.1 | S |
| T-4.3 | Extend `## Attachments` prompt rendering with "open/convert this yourself" guidance for document types | code | T-4.1 | S |
| T-4.4 | Land documents through the same `$session/attachments/` path and manifest flow as images | code | T-4.1, T-4.2 | S |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-4.1 | A GitHub-hosted PDF/DOCX/PPTX attachment is fetched, sniffed by magic bytes, sized against the document ceiling, and landed alongside any images from the same turn. |
| AC-4.2 | A document exceeding `attachment_max_document_bytes` is rejected via the same mechanism as an oversized image (US-6), not a separate code path. |
| AC-4.3 | The rendered `## Attachments` prompt section tells the agent these files are provided as-is for it to open or convert; litefactory performs no conversion. |
| AC-4.4 | A supported type with unconfirmed downstream handling is still landed and mentioned — litefactory does not gate ingestion on whether the receiving agent can read it. |
