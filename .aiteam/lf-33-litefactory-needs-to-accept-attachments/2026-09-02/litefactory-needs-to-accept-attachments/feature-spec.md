# Feature Spec: Attachment Ingestion (issue #33)
# Status: APPROVED
# Date: 2026-09-03

Basis: `RESEARCH_BRIEF.md` (2026-09-02, incl. Owner Pushback Addendum) plus the
Plan-mode detour brief at
`.aiteam/lf-33-litefactory-needs-to-accept-attachments/2026-09-03/codex-cli-file-limits/RESEARCH_BRIEF.md`,
and the project owner's reply on issue #33 answering the brief's Open Questions 1-7.

Headless run: the owner's issue reply is the standing approval input; no interactive
confirmation was possible. Every judgment call not settled by that reply is recorded
under **Assumptions** rather than posed back as a question.

## Goal

Let a user attach mockups, screenshots and documents (images, PDF, DOCX, PPTX) to a
GitHub issue or comment and have litefactory download them, land them where the
invoked `claude`/`codex` subprocess can read them, and reject anything it cannot
safely hand over — so a run can act on visual and document context instead of body
text alone.

## User Stories

| ID | Story |
|----|-------|
| US-1 | As an issue owner, I want the tool label spelled `Tools: <Profile>` instead of `lf:tools=<profile>`, so the label vocabulary reads the same way as the shipped `Coding Agent: <Kind>` label |
| US-2 | As an issue owner, I want a legacy `lf:tools=` label to be refused loudly rather than ignored, so a stale label never silently drops a permission grant |
| US-3 | As an issue owner, I want to drag a mockup image onto an issue or comment and have the agent see it, so I can ask for work against a picture |
| US-4 | As an issue owner, I want to attach a PDF, DOCX or PPTX and have the agent read it (converting it itself if it must), so specs and decks are usable input |
| US-5 | As an operator, I want attachment ingestion gated by a tool profile selected by a single `Tools: <Profile>` label, so downloading from untrusted issue bodies is an explicit, ceiling-bounded grant |
| US-6 | As an issue owner, I want an attachment that is too large, of an unsupported type, or unfetchable to be rejected with a clear reason on the thread while the run still proceeds, so a bad file never silently degrades or kills a turn |
| US-7 | As an operator, I want every ingested attachment recorded (source URL, size, hash, type, landing path) in the session, so I can audit and later intercept what was handed to an agent |
| US-8 | As an issue owner on a private repo, I want attachments to fetch reliably even though their URLs are short-lived and signed, so private-repo runs are not intermittently broken |

## Data Model

### Attachment manifest record (one per ingested or rejected attachment, per turn)

| Entity | Field | Type | Required | Constraints |
|--------|-------|------|----------|-------------|
| Attachment | Index | int | yes | 1-based, document order within the turn |
| Attachment | SourceURL | string | yes | must match the GitHub asset allow-list (see Business Rules); stored verbatim, never rendered into argv |
| Attachment | Origin | enum | yes | `issue_body` \| `comment_body` |
| Attachment | OriginCommentID | int64 | no | 0 when Origin is `issue_body` |
| Attachment | AltText | string | no | markdown alt/link text, sanitised before rendering; ≤200 runes |
| Attachment | Filename | string | yes | litefactory-derived, `NN-<slug>.<ext>`; sanitised, no path separators, no `..`, ASCII `[a-z0-9._-]` only |
| Attachment | ContentType | string | yes | sniffed from bytes, cross-checked against the HTTP `Content-Type` |
| Attachment | Kind | enum | yes | `image` \| `pdf` \| `docx` \| `pptx` |
| Attachment | SizeBytes | int64 | yes | raw (pre-base64) bytes as landed on disk |
| Attachment | SHA256 | string | yes | hex, lowercase, 64 chars |
| Attachment | Path | string | yes | repo-relative path under `$session/attachments/`; empty when Status is not `ingested` |
| Attachment | Status | enum | yes | `ingested` \| `rejected_too_large` \| `rejected_type` \| `rejected_budget` \| `rejected_count` \| `fetch_failed` \| `skipped_not_granted` |
| Attachment | Reason | string | no | one-line human-readable reason; required when Status ≠ `ingested` |

### Tool profile (config extension)

| Entity | Field | Type | Required | Constraints |
|--------|-------|------|----------|-------------|
| ToolProfile | Attachments | bool | no | default `false`; a profile may only set `true` when the operator's ceiling permits attachments (see Business Rules); exact YAML nesting under the existing `tool_profiles[<name>]` stanza is the architect's call |
| Claude (policy) | AttachmentsCeiling | bool | no | default `false`; operator-set ceiling — no profile can enable attachments when this is `false` |
| Claude (policy) | AttachmentMaxImageBytes | int64 | no | default 5 MiB |
| Claude (policy) | AttachmentMaxDocumentBytes | int64 | no | default 10 MiB |
| Claude (policy) | AttachmentMaxTotalBytes | int64 | no | default 25 MiB per turn |
| Claude (policy) | AttachmentMaxCount | int | no | default 10 per turn |

### Prompt surface

| Entity | Field | Type | Required | Constraints |
|--------|-------|------|----------|-------------|
| harness.PromptInput | Attachments | string | no | a pre-rendered text block listing repo-relative paths, kinds and alt text; empty string when nothing was ingested. **One string field only** — no typed attachment slice, and no change to `agent.Request` or any `Runner` |

### Relationships

- Issue → Attachment via markdown/HTML references parsed out of `Issue.Body`
- Comment → Attachment via markdown/HTML references parsed out of `Comment.Body`
- ToolProfile → run capability via the single `Tools: <Profile>` label on the issue
- Attachment → file on disk via `$session/attachments/<Filename>`
- Attachment set → `harness.PromptInput.Attachments` (rendered once, per turn)

## API / Interface Changes

| Method | Endpoint / Action | Auth | Notes |
|--------|------------------|------|-------|
| Label | `Tools: <Profile>` | none (label trust model) | replaces `lf:tools=<profile>`. Detected case-insensitively, accepted byte-exact, exactly as `Coding Agent: ` already is. Value is Title-case (`Web`, `Attachments`) and maps to the operator's lowercase profile key |
| Label | `lf:tools=<anything>` | none | **refused**, not ignored: new reply kind telling the maintainer the label was renamed. Detected case-insensitively; counts toward the ambiguity rule alongside a `Tools: ` label |
| Label | `Tools: Attachments` | none | not a special case in code — an ordinary profile name whose profile has `attachments: true` |
| Config | `tool_profiles[<name>].attachments` | operator-only | new per-profile boolean, ceiling-bounded |
| Config | attachment size/count limits | operator-only | four new numeric keys with the defaults above |
| GET | existing `GetIssue` / `ListCommentsAfter` | installation token | re-used, not changed; re-called immediately before download to obtain fresh signed attachment URLs |
| GET | attachment URL fetch (new outbound HTTP) | installation token, with unauthenticated retry | new code path in `internal/github`; `net/http` only, consistent with the package's existing constraint |
| File | `$session/attachments/<NN-slug.ext>` | — | raw bytes, **gitignored** |
| File | `$session/attachments.md` | — | the manifest above, rendered as a table; **committed** |
| Reply | attachment outcome block | — | appended to the run's existing reply comment; new `report.ReplyKind` values as needed for the label refusals |
| Prompt | `Attachments` block | — | new section in the prompt template, rendered from the manifest |

## Business Rules

### Label vocabulary (US-1, US-2)

- The tools label prefix becomes `Tools: `, mirroring `Coding Agent: ` byte for byte in behaviour: detected case-insensitively, accepted only byte-exact, value shape `^[A-Z][A-Za-z0-9_-]{0,39}$`.
- The label value is Title-case and resolves to the operator's configured profile key by lowercasing the first character only (`Web` → `web`, `Research-Deep` → `research-deep`). A value that does not resolve to a configured key is the existing unknown-profile refusal.
- More than one `Tools: ` label is ambiguous and refused. Unchanged rule, unchanged reason: two grants are never unioned.
- A `lf:tools=` label (any casing) is refused with a rename-specific reply naming the new spelling. It is never resolved and never ignored. This rule is what makes the rename safe to ship: the failure mode the existing label design exists to prevent is a permission request that is silently dropped.
- The refusal ladder ordering is unchanged: agent label (stage 1) before tools label.

### Gating (US-5)

- Attachment ingestion happens **only** when the run's resolved tool profile has `attachments: true`.
- No profile can enable attachments above the operator's ceiling. A profile that sets `attachments: true` under a `false` ceiling is a config validation error at boot, not a runtime refusal.
- Attachment ingestion grants the agent **no new tools**. litefactory performs the download itself, in-process, before the subprocess is spawned; the agent reads a local file with the `Read` tool it already has. `WebFetch`/`WebSearch` are neither required nor implied.
- Attachments detected on a run whose profile does not grant them are **not** a refusal: the run proceeds, and the reply says how many were skipped and why.

### Discovery and fetch (US-3, US-4, US-8)

- Bodies scanned: the issue body, plus every comment body the turn ingested in that tick (trigger comment and any comments in the same batch).
- Reference forms recognised: markdown image `![alt](url)`, markdown link `[text](url)`, and HTML `<img src="url">`.
- Fetch allow-list, exact host/path match, everything else in body text ignored (not refused — ordinary links are normal):
  - `https://github.com/user-attachments/assets/<uuid>`
  - `https://github.com/<owner>/<repo>/assets/<user-id>/<uuid>`
  - `https://*.githubusercontent.com/...` (incl. `private-user-images.` and `user-images.`)
- Attachment URLs are never cached across turns as URLs. Immediately before downloading, the parent issue/comment is re-fetched through the existing API client so the body carries a fresh signed URL.
- Downloads are de-duplicated by source URL within a turn, and idempotent across turns within a session: if the manifest already carries the URL and the file is present on disk with a matching SHA-256, the download is skipped.
- Authentication: the resolved installation token is sent on the fetch. On `401`/`404` the fetch is retried **once** unauthenticated (these assets are commonly served without credentials), and the manifest records which path succeeded. This is defensive handling for the unverified installation-token report, not an assertion that the report is true.
- Per-download timeout 30 s; whole-turn ingestion budget 120 s. Exceeding either marks the remaining attachments `fetch_failed` and lets the run proceed.

### Type and size policy (US-6)

- Phase 1 type allow-list, by sniffed content type: PNG, JPEG, GIF, WebP, `application/pdf`, DOCX, PPTX. Anything else is `rejected_type`.
- The sniffed type is authoritative. A mismatch between the HTTP `Content-Type` and the sniffed bytes is a rejection, not a coercion.
- The landed filename and extension are derived by litefactory from the sniffed type and a sanitised slug of the alt text (or the URL's UUID when there is none). Untrusted body text never determines a path.
- Size caps are enforced by litefactory **before** handoff, uniformly for both agents, and are hard rejections — never truncation, never downscaling:
  - image: 5 MiB raw per file
  - PDF / DOCX / PPTX: 10 MiB raw per file
  - 25 MiB total per turn, 10 attachments per turn
- Attachments are processed in document order; once the total-bytes or count budget is exhausted the remainder are `rejected_budget` / `rejected_count`.
- Downloads are streamed with a hard byte ceiling: a response that exceeds the applicable cap is aborted mid-stream and never fully buffered or written.
- litefactory does **not** convert, parse, render, transcode or validate the content of any attachment. It downloads bytes, checks type and size, and lands the file. Conversion of DOCX/PPTX (and of PDF for Codex runs) is the invoked agent's job, via its own `Bash` access, per the owner's decision.
- litefactory does **not** check for, install, or health-check `pandoc`, LibreOffice or Poppler, and does not fail a run because a converter is absent.

### Handoff (US-3, US-4, US-7)

- Ingested files land under `$session/attachments/`, inside the git worktree, so both a `claude` process (CWD = worktree) and a `codex` process (workspace sandbox) can read them by repo-relative path with no new writable-root or `--add-dir` grant.
- Raw attachment bytes must not enter git history. `$session/attachments/` is gitignored; the committed audit record is `attachments.md`.
- The prompt gains one rendered `Attachments` section listing, per ingested file: repo-relative path, kind, size, and sanitised alt text. For non-image kinds the block states that the agent may need to convert the file itself with shell tooling, and that conversion tooling is not guaranteed to be present.
- No new field is added to `agent.Request`, and no `Runner` implementation changes.
- Attachment bytes are never posted back to GitHub, never written into a reply, and never placed in argv.
- The manifest and every reply line pass through the existing redactor before being posted.

## Edge Cases & Error Handling

| Case | Expected behaviour |
|------|--------------------|
| No attachments referenced anywhere | No manifest, no prompt section, no reply line; turn byte-identical to today |
| Attachments present, profile does not grant attachments | Nothing downloaded; reply notes the count and names the missing grant; run proceeds |
| Both `Tools: Web` and `Tools: Attachments` applied | Ambiguous-label refusal (unchanged rule). The operator's answer is a single profile that grants both — profiles compose in config, labels never union |
| `lf:tools=web` still applied after the rename | Loud refusal naming the new spelling; run does not start |
| `lf:tools=web` and `Tools: Web` both applied | Ambiguous-label refusal (both are seen as tools labels) |
| Image over 5 MiB | `rejected_too_large`; reply names the file, its size and the cap; other attachments and the run proceed |
| Document over 10 MiB | Same, against the 10 MiB cap |
| Eleventh attachment on a turn | `rejected_count`; first ten ingested; reply says so |
| Total over 25 MiB | Ingest in document order until the budget is exhausted; remainder `rejected_budget`; reply says so |
| XLSX, ZIP, MP4, `.eml`, or any other GitHub-permitted type | `rejected_type` with a reply naming the supported set; run proceeds |
| Extension/`Content-Type` disagrees with sniffed bytes | `rejected_type`; the file is not landed |
| Signed URL expired between body fetch and download | Parent issue/comment re-fetched once for a fresh URL; on a second failure `fetch_failed` with a reply line; run proceeds |
| Installation token 404s on the asset URL | One unauthenticated retry; outcome recorded in the manifest; on failure `fetch_failed`, run proceeds |
| Download times out or the host is unreachable | `fetch_failed`; run proceeds; never a turn failure |
| Same image referenced from the issue body and a comment | Downloaded once, one manifest row, one prompt entry |
| Same attachment already ingested in an earlier turn of the session | Reused from disk, no re-download, still listed in the prompt |
| Alt text containing path separators, `..`, control characters or a secret shape | Filename is litefactory-derived and sanitised; alt text is sanitised and redacted before rendering |
| A URL in the body that is not a GitHub asset URL | Ignored silently — it is an ordinary link, not an attachment |
| PDF handed to a Codex-routed run | Landed and listed. Codex has no native PDF read path; the prompt tells the agent to convert it with shell tooling. If it cannot, the agent reports that in its own output — litefactory does not pre-empt it |
| DOCX/PPTX on either agent | Landed and listed; conversion is the agent's job. Behaviour on Codex is unverified and is a carried risk, not a blocker |
| Codex run with a read-only sandbox and a document attachment | Conversion may be impossible; the agent reports it. litefactory does not detect or special-case this in Phase 1 |
| Disk write fails under `$session/attachments/` | `fetch_failed` with the write error as the reason; run proceeds |
| PDF longer than ~100 pages | Landed. Claude Code pages PDFs ~20 at a time; litefactory does not count pages and does not reject on page count |

## Answering the owner's questions

Plain-language answers to the three questions the owner asked as questions rather
than decisions. These are written for the issue thread, not for the architect.

**Q3 — "what are possible documents from github? show me the list, then i can decide!"**

GitHub itself decides what a user is allowed to drag onto an issue or comment. The
full list, verified against GitHub's own docs, is:

- **Images and GIFs** — PNG, JPEG, GIF, WebP, SVG. Limit **10 MB**.
- **Video** — MP4, MOV. Limit **10 MB** on free plans, **100 MB** on paid.
- **Everything else — limit 25 MB**, covering:
  - PDF
  - Microsoft Office: DOCX, DOC, PPTX, XLSX, XLS, XLSM
  - OpenDocument: ODT, ODS, ODP
  - Plain text and data: TXT, MD, CSV, TSV, LOG, JSON, RTF
  - Common source-code extensions
  - Archives: ZIP, GZ, TGZ
  - A few odds and ends: MSG, EML, DMP, PDB

Two things follow. First, nothing has to be built on the GitHub side — Word docs and
PowerPoints can already be dropped onto an issue today, exactly like images. Second,
your own answer to question 7 already picks the subset for this first release out of
that list: **images, PDF, DOCX, PPTX**. Everything else above (spreadsheets,
archives, video, email files, transcripts) is deliberately left out for now and is
listed under Out of Scope below. If someone attaches one of those, litefactory will
say so on the thread and carry on rather than fail.

**Q4 — "no idea, please explain more detailed!"** (how the downloaded file reaches the agent)

There were two ways to hand a downloaded file to the agent, and the question was
which one to build.

- **Option A — put the file somewhere the agent already looks.** litefactory writes
  the file into the run's own session folder, and the prompt says "there is a mockup
  at this path". The agent opens it with the file-reading tool it already has.
  Nothing else in litefactory has to know attachments exist. This matches how the
  team already works — everything else about a run is passed as "here is the folder,
  read what you need" rather than as data pushed through the plumbing.
- **Option B — make attachments a first-class thing in the plumbing.** Add a proper
  "attachments" field to the internal request object, and thread it through every
  layer that builds and dispatches a run, including both the Claude and the Codex
  runners. Tidier on paper, but it touches a lot of code that has nothing to do with
  attachments, and every one of those touch points is a place to get it wrong.

**Decision taken: Option A**, with one small concession — the prompt gets a short
generated "Attachments" section so the agent is told the files exist rather than
having to go looking. That is a single line of text, not a new data structure, and
no runner changes. This is the smaller, lower-risk change and it is consistent with
your answer to question 1: litefactory fetches the file and passes it on, and stays
the place where interception and logging happen, without becoming the thing that
parses it.

**Q6 — "no idea what that meant, please explain easier!"** (the GitHub token concern)

litefactory's bot does not log in to GitHub as a person. It uses a short-lived,
narrowly-scoped credential that GitHub issues to installed apps — an *installation
token* — instead of a personal account's *personal access token* (PAT). Normally
they behave the same.

Someone online reported once that this app-style token gets a "not found" error when
used to download the actual bytes of an attachment file, in a case where a personal
token worked fine. It is a single, unconfirmed report; nobody has reproduced it
against litefactory, and it may well be wrong.

We are neither believing it nor ignoring it. The cheap insurance is: try the download
with the normal token, and if it comes back "not found" or "unauthorised", retry once
without the token (these files are often served openly anyway) and record in the log
which route worked. That costs almost nothing and turns a possible mystery failure
into a line in the audit trail. If it never happens, the retry never fires. This is
something to smoke-test the first time the feature runs for real, not something that
needs settling before implementation starts.

## Assumptions

Recorded because there was no channel to ask on this run. Each is the most defensible
reading of the research, and each is safe to revisit.

| # | Assumption | Grounding |
|---|-----------|-----------|
| A-1 | The `lf:tools=` → `Tools: <Profile>` rename is **in scope** for this feature, not a follow-up | The owner's chosen label spelling (`Tools: Attachments`) cannot exist without it; shipping both notations at once would leave two competing prefixes in one namespace. A shipped precedent already exists (`Coding Agent: `), so the change is mechanical |
| A-2 | `Attachments` is an ordinary tool-profile name, not a special-cased label | Keeps the single-label, never-union rule intact. An operator who wants web + attachments writes one profile granting both |
| A-3 | Files land under `$session/attachments/`, gitignored, with a committed text manifest | Session boundary is the existing convention; the worktree is the only place both agents can read without new sandbox grants; binary blobs in git history are not wanted, but the audit record the owner asked for is |
| A-4 | Fetch allow-list is GitHub asset domains only (Open Question 3, unaddressed by the owner's reply) | The brief's own recommendation; an attachment URL comes from an untrusted issue body, so a general-purpose fetcher is an SSRF surface for no current benefit |
| A-5 | Image cap 5 MiB raw, document cap 10 MiB raw, 25 MiB / 10 files per turn | No confirmed authoritative number exists for either harness. Claude's documented image ceiling is 10 MB **base64**, and base64 inflates raw bytes ~33%, so 5 MiB raw (~6.8 MB encoded) sits under it. Codex publishes no cap and is known to fail badly rather than reject cleanly, so litefactory enforces one uniformly for both |
| A-6 | **If litefactory's Claude Code invocation routes through Bedrock or Vertex rather than the direct Anthropic API, the image cap must drop to 3.5 MiB raw** (that tier's ceiling is 5 MB base64) | Which backend is in use was not confirmed. The caps are operator-configurable specifically so this can be corrected without a code change |
| A-7 | Ingestion is an in-process pre-step in litefactory, not a new sandboxed sub-agent capability | The owner's answer to Q1, read literally. It also means no new tool grant, no `WebFetch` in any profile, and no change to the agent tool ceiling |
| A-8 | PDF/DOCX/PPTX are landed for Codex runs even though Codex's handling is unconfirmed | The file is still reachable via shell conversion; refusing it pre-emptively would remove a working path on the strength of an unverified gap. The agent reports if it cannot read it |

## Carried Risks

| # | Risk | Disposition |
|---|------|-------------|
| R-1 | `pandoc`/LibreOffice/Poppler may not be installed on the deployment host, so DOCX/PPTX conversion may simply fail at runtime (brief Open Question 8) | Accepted. The owner decided litefactory does not check or install converters. Operational, outside litefactory's responsibility; worth a `brew install pandoc` on the host before demoing DOCX |
| R-2 | Codex has no PDF read path at all (open upstream feature request) | Accepted. PDFs on Codex runs depend on shell conversion succeeding |
| R-3 | Codex behaviour on DOCX/PPTX is unverified in both directions | Accepted; litefactory's own type/size policy is the single source of truth either way |
| R-4 | Codex's sandbox mode may block the shell conversion the document path depends on (brief Open Question 10) | Accepted for Phase 1; observable in the agent's own output |
| R-5 | The installation-token 404 report is unverified | Mitigated by the one-shot unauthenticated retry; smoke-test in Implement |
| R-6 | Codex CLI is a fast-moving target; today's limits may change | Caps are operator-configurable |
| R-7 | The rename is user-visible and breaking for any issue currently carrying an `lf:tools=` label | Mitigated by the loud refusal (never a silent drop) and a README/config-template update |

## Out of Scope

- Chat history and call-transcript ingestion (Slack JSON exports, VTT, SRT) — Phase 2+, untouched by the owner's reply
- Spreadsheets (XLSX/XLS/XLSM/ODS), OpenDocument, RTF, DOC, archives (ZIP/GZ/TGZ), video, MSG/EML/DMP/PDB
- litefactory-side conversion, parsing, OCR, thumbnailing or preview rendering of any attachment
- Any Go DOCX/PPTX library dependency (`unioffice` and the free alternatives are no longer load-bearing)
- Installing, detecting or health-checking `pandoc`/LibreOffice/Poppler — explicitly refused by the owner
- Fetching non-GitHub URLs found in issue bodies (Google Docs, Confluence, arbitrary links)
- Image dimension / pixel-count validation (needs a decoder per format; WebP is not in the stdlib)
- PDF page-count validation
- Attachments on pull requests, or on comments older than the turn's ingested batch
- litefactory *posting* images or files back to GitHub
- Persisting attachments in SQLite (the manifest file is the record; no new table, no new migration)
- Committing attachment bytes to git
- Raising `max_concurrent_runs`, touching the store connection cap, or any unrelated config surface

## Acceptance Criteria

### Label rename
- [ ] `Tools: Web` on an issue resolves the operator's `web` profile and produces argv identical to what `lf:tools=web` produced before the rename
- [ ] `tools: Web` (mis-cased prefix) is seen and refused as malformed, never ignored
- [ ] `Tools: web` (mis-cased value) is refused as malformed
- [ ] `Tools: Nope` (well-formed, unconfigured) is refused as unknown and the reply names the profile
- [ ] Two `Tools: ` labels are refused as ambiguous; neither is resolved and neither is echoed
- [ ] `lf:tools=web` is refused with a reply naming the new spelling; no run starts
- [ ] `lf:tools=web` plus `Tools: Web` is refused as ambiguous
- [ ] An issue with no tools label produces argv byte-identical to before this feature

### Gating
- [ ] With a profile carrying `attachments: true`, an attached image is downloaded and landed
- [ ] With a profile carrying `attachments: false` (or none), an attached image is not downloaded, the run proceeds, and the reply says how many attachments were skipped and why
- [ ] A config setting `attachments: true` on a profile above the operator's ceiling fails validation at boot with a named error, and the daemon does not start
- [ ] Enabling attachments adds no tool to the resolved `--tools`/`--allowedTools` argv

### Ingestion
- [ ] An image referenced as `![mockup](https://github.com/user-attachments/assets/<uuid>)` in the issue body is landed under `$session/attachments/` and listed in the prompt's Attachments block
- [ ] The same for `<img src="...">` and for `[spec](.../assets/<uuid>)` link syntax
- [ ] An image referenced in the triggering comment is ingested
- [ ] The same URL in both the issue body and a comment yields exactly one download and one manifest row
- [ ] A `https://evil.example.com/x.png` reference in the body is ignored: no request is made, nothing appears in the manifest
- [ ] The parent issue/comment is re-fetched for a fresh signed URL immediately before download
- [ ] A `404` on the authenticated fetch triggers exactly one unauthenticated retry, and the manifest records which route succeeded
- [ ] A second turn in the same session with the same attachment makes no new download and still lists the file in the prompt

### Rejection
- [ ] A 6 MiB PNG is rejected as too large, is not written to disk, and the reply names the file, its size and the cap
- [ ] An 11 MiB PDF is rejected as too large under the document cap
- [ ] An `.xlsx` attachment is rejected as an unsupported type and the reply names the supported set
- [ ] A file whose bytes are a ZIP but whose `Content-Type` claims `image/png` is rejected
- [ ] The eleventh attachment on a turn is rejected on count; the first ten are ingested
- [ ] Attachments totalling more than 25 MiB are ingested in document order until the budget is exhausted; the remainder are rejected on budget
- [ ] An oversized download is aborted mid-stream: the process never buffers or writes more than the cap
- [ ] Every rejection leaves the run running and the turn successful
- [ ] A download timeout, a connection failure and a disk-write failure each produce a manifest row and a reply line, and none fails the turn

### Handoff and audit
- [ ] The prompt contains an Attachments section listing the repo-relative path, kind, size and sanitised alt text of each ingested file
- [ ] The Attachments section for a PDF/DOCX/PPTX states the agent may need to convert it with shell tooling
- [ ] `agent.Request` gains no field and no `Runner` implementation is modified
- [ ] `$session/attachments.md` records every attachment — ingested and rejected — with source URL, origin, kind, size, SHA-256, status and reason
- [ ] `$session/attachments/` is gitignored and no attachment byte appears in `git status` after a run
- [ ] `attachments.md` is present in the worktree and committable
- [ ] Alt text of `../../etc/passwd` yields a sanitised filename with no path separators, landed inside `$session/attachments/`
- [ ] A secret-shaped string in alt text is redacted before it reaches the reply comment or the manifest
- [ ] No attachment bytes appear in argv, in a reply comment, or in the run's event stream

### Regression
- [ ] `make check` passes (`fmt vet lint test`, `-race`)
- [ ] A run on an issue with no attachments and no tools label is unchanged in argv, forge calls and reply text
- [ ] `internal/config`'s no-third-party-import invariant still holds
