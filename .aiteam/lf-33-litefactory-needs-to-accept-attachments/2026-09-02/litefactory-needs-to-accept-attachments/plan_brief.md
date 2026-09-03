# Plan Brief — Attachment Ingestion (issue #33)
# Branch: lf/33-litefactory-needs-to-accept-attachments
# Date: 2026-09-03
# Parent issue: #33 — "Litefactory needs to accept attachments"
# Citation base commit: 4f22b48
# Research brief: `RESEARCH_BRIEF.md` (2026-09-02, incl. Owner Pushback Addendum) plus the
#   Plan-mode detour brief at `../2026-09-03/codex-cli-file-limits/RESEARCH_BRIEF.md`
# Feature spec: `feature-spec.md` (APPROVED, 2026-09-03)

**Headless run.** The project owner replied on issue #33 answering the feature
spec's numbered Open Questions 1-7; there was no channel to ask a follow-up on
this run. Per that reply's own instruction ("no idea, please explain more
detailed"), three of the owner's replies were requests for a plain-language
explanation rather than a decision — those explanations are reproduced verbatim
below, for the issue thread, not for the architect. Two further judgment calls
surfaced only once the architect reviewed the approved spec (the US-1
case-folding rule, and the US-8 retry-placement tension against this codebase's
"`internal/github` retries nothing" constraint); both are recorded as resolved
decisions in the relevant story's Architecture Notes, with the reasoning that
justifies each, rather than posed back as a question. This plan is written to be
actionable as-is; anything the owner wants to correct can happen as a comment on
the issue or its sub-issues.

## Answering the Owner's Questions

Plain-language answers to the three questions the owner asked as questions rather
than decisions, carried forward from the feature spec.

**"What are possible documents from GitHub? Show me the list, then I can decide!"**

GitHub itself decides what a user is allowed to drag onto an issue or comment. The
full list, verified against GitHub's own docs, is:

- **Images and GIFs** — PNG, JPEG, GIF, WebP, SVG. Limit **10 MB**.
- **Video** — MP4, MOV. Limit **10 MB** on free plans, **100 MB** on paid.
- **Everything else — limit 25 MB**, covering: PDF; Microsoft Office (DOCX, DOC,
  PPTX, XLSX, XLS, XLSM); OpenDocument (ODT, ODS, ODP); plain text and data (TXT,
  MD, CSV, TSV, LOG, JSON, RTF); common source-code extensions; archives (ZIP, GZ,
  TGZ); and a few odds and ends (MSG, EML, DMP, PDB).

Nothing has to be built on the GitHub side — Word docs and PowerPoints can already
be dropped onto an issue today, exactly like images. The owner's answer to
question 7 already picks the subset for this first release out of that list:
**images, PDF, DOCX, PPTX**. Everything else above (spreadsheets, archives, video,
email files) is deliberately left out for now — see Out of Scope. If someone
attaches one of those, litefactory says so on the thread and carries on rather
than failing the run.

**"No idea, please explain more detailed!" (how the downloaded file reaches the agent)**

There were two ways to hand a downloaded file to the agent, and the question was
which one to build.

- **Option A — put the file somewhere the agent already looks.** litefactory
  writes the file into the run's own session folder, and the prompt says "there is
  a mockup at this path." The agent opens it with the file-reading tool it already
  has. This matches how the team already works — everything else about a run is
  passed as "here is the folder, read what you need" rather than pushed through
  the plumbing.
- **Option B — make attachments a first-class thing in the plumbing.** Add a
  proper "attachments" field to the internal request object and thread it through
  every layer that builds and dispatches a run, including both coding-agent
  runners. Tidier on paper, but it touches a lot of code that has nothing to do
  with attachments, and every touch point is a place to get it wrong.

**Decision taken: Option A**, with one small concession — the prompt gets a short
generated "Attachments" section so the agent is told the files exist rather than
having to go looking. That is a single line of text, not a new data structure, and
no changes to either coding-agent runner. This is the smaller, lower-risk change,
and it matches the owner's answer to question 1: litefactory fetches the file and
passes it on, staying the place where interception and logging happen, without
becoming the thing that parses documents.

**"No idea what that means, please explain easier!" (the GitHub token concern)**

litefactory's bot does not log in to GitHub as a person. It uses a short-lived,
narrowly-scoped credential that GitHub issues to installed apps — an
*installation token* — instead of a personal account's *personal access token*.
Normally they behave the same.

Someone online reported once that this app-style token gets a "not found" error
when used to download the actual bytes of an attachment file, in a case where a
personal token worked fine. It is a single, unconfirmed report; nobody has
reproduced it against litefactory, and it may well be wrong.

The plan neither believes it nor ignores it: the download is tried with the normal
token, and if it comes back "not found" or "unauthorised," it is retried once
without the token (these files are often served openly anyway), with the log
recording which route worked. That costs almost nothing and turns a possible
mystery failure into a line in the audit trail. This is something to smoke-test
the first time the feature runs for real, not something that needed settling
before implementation starts.

## Shared Decisions

- **Package placement.** Attachment ingestion lives in one new package,
  `internal/attach` (fetch policy, allow-list, type sniffing, size ceilings,
  filename sanitisation, manifest rendering). It is a leaf: it may import
  `internal/config`, is imported only by `internal/turn`, and must not import
  `internal/daemon`, `internal/harness`, `internal/report`, or `internal/store`.
- **The one HTTP hop.** The actual GET of an asset URL is a new, no-retry method
  (`FetchAsset`) on the concrete `*github.Client` in `internal/github` — the only
  package holding the token and permitted to make authenticated forge calls. It
  returns a typed `ErrAssetUnauthorized` rather than retrying itself. This is a
  deliberate, documented amendment to `internal/github/surface_test.go`'s pinned
  "exactly seven endpoints, no interface declared in this package" test — change
  the pin in the same commit as `FetchAsset`, with a comment explaining why.
  `internal/attach` consumes `FetchAsset` through an interface it declares at its
  own point of use, never the reverse.
- **Retry placement resolves the CONC-6 tension.** The spec calls for one retry on
  401/404 and for 30s/120s budgets; this codebase's constraint is that
  `internal/github` retries nothing and sleeps nowhere. Both hold: `FetchAsset`
  stays dumb and single-shot, and the one re-resolve-and-refetch retry, plus both
  timeouts (via `context.WithTimeout` off the turn's own context), live in
  `internal/attach`, one layer up. If an implementer instead puts the retry inside
  `internal/github`, that is a real deviation from this codebase's constraints and
  must be logged in `.aiteam/AC_AMENDMENTS.md` before merge, not silently shipped.
- **Handoff contract, frozen.** Exactly one new field, `harness.PromptInput.Attachments
  string`, pre-rendered by `internal/turn` and printed by `BuildPrompt` into its
  own `## Attachments` section. No change to `agent.Request`, no change to any
  `Runner` implementation, no per-attachment struct crosses into `internal/harness`.
  This is Option A from "Answering the Owner's Questions" above; Option B is closed.
- **Insertion point.** Ingestion runs in `internal/turn/prepare.go`, after the
  worktree exists and before `BuildPrompt` is called. Files land under
  `$session/attachments/` inside the worktree (gitignored); `attachments.md` is
  the committed audit artefact. Bytes never enter argv, the git index directly, or
  a reply body.
- **Gating shape.** Attachment ingestion is a *permission*, not a *capability* —
  on only when a `Tools: <Profile>` label resolved and the resolved
  `config.ToolProfile` carries the attachment keys. No label means no ingestion,
  and argv plus prompt bytes stay byte-identical to today. Ingestion grants the
  agent no new tool (no `WebFetch`/`WebSearch` implied); litefactory downloads
  in-process, before the subprocess is spawned, and the agent reads a local file
  with the `Read` tool it already has.
- **Label parsing is one shared ladder, twice instantiated.** `Tools: ` reuses the
  exact detect-loosely/accept-strictly ladder already shipped for
  `Coding Agent: ` (case-insensitive detection, byte-exact acceptance, count
  ambiguity before judging any label). The tool-profile-key regex stays
  deliberately duplicated between the daemon's label parser and `internal/config`
  — sharing it would move config validation out of boot, and both files should
  document the duplication as intentional, not accidental.
- **Config shape.** All five new keys (`attachments`, `attachment_max_image_bytes`,
  `attachment_max_document_bytes`, `attachment_max_total_bytes`,
  `attachment_max_count`) live under the policy stanza and inside `ToolProfile`,
  defaulted by decoding, mirrored into `Defaults()` and the shipped config
  template in the same commit, validated with aggregated (`errors.Join`) errors,
  and out-of-range at boot is a startup error — never a runtime clamp.
- **Rejections are values, not errors.** Every rejected attachment is a recorded
  outcome flowing into the manifest and the reply comment; no rejection may fail
  the turn or abort the run. This is the plan's "nothing fails the turn"
  principle and it applies uniformly across every rejection reason.
- **Attachments is an ordinary profile name.** `Tools: Attachments` is not a
  special case in code — it is an ordinary tool-profile name whose profile has
  `attachments: true`, keeping the single-label, never-union rule intact. An
  operator who wants web and attachments together writes one profile that grants
  both.

## Workflow

Shipping order and the reason for each position:

| Order | Story | Why here |
|---|---|---|
| 1 | US-1 | Pure rename of the label vocabulary; every later story's gate reads it. Independently valuable on its own. |
| 2 | US-2 | The legacy-`lf:tools=` refusal is the other half of the rename; shipping US-1 without it silently ignores every existing label. |
| 3 | US-5 | Config keys and the gate decision. Nothing may be fetched before the permission that authorises fetching exists. |
| 4 | US-6 | Rejection outcomes and their reply/manifest shape; every ingestion path in US-3/US-4 needs somewhere to put a "no." |
| 5 | US-3 | First real ingestion: images. Smallest type set, exercises fetch, sniff, land and handoff end to end. |
| 6 | US-7 | Audit manifest and session record; the data it records only exists once US-3 has landed a file. |
| 7 | US-4 | Documents: same pipeline as US-3 plus a larger ceiling and "convert it yourself" prompt guidance. Additive over a proven path. |
| 8 | US-8 | Signed-URL/private-repo reliability hardening; it tunes a fetch path the earlier stories must already have. |

**Dependency graph:** US-1 → US-2; US-1 → US-5 → {US-6, US-3}; US-3 → {US-7, US-4};
US-3 → US-8. US-6 has no consumer of its own but must land before US-3, so
ingestion never has a rejection path with nowhere to report.

**Hard couplings — ship together, in one commit each:**
- US-1 + US-2. A rename without a refusal for the old spelling is a silent
  permission drop, and the rename is user-visible and breaking for any issue
  currently carrying an `lf:tools=` label — the loud refusal is what makes
  shipping the rename safe at all.
- Any new `report.ReplyKind` and its case in `internal/turn/report.go`'s
  `replyKind()` — `report` cannot import `turn`, so the two halves are one change.
  Affects US-2 and US-6.
- Any new or changed config key, `Defaults()`, and the shipped config template.
  Affects US-5.
- `PromptInput.Attachments`, the `BuildPrompt` section that renders it, and the
  update to the pinned cold-prompt test. Affects US-3.

## Cross-Story Constraints

**Platform**
- `context.Context` is first-parameter and request-scoped in every fetch, timeout
  and stream copy; no `context.Background()` in the fetch path.
- Any fan-out of downloads is a bounded pool with context cancellation and a
  `WaitGroup`, never an unbounded `go func()` per URL.
- Errors wrap with `%w`; new failure conditions get sentinel/typed errors in
  `internal/attach`; no `panic` for an expected fetch failure.
- `log/slog` only; the fetch path logs URL host and outcome, never bytes and
  never the token.
- No new third-party dependency for fetching, sniffing or hashing —
  `net/http`, `net/url`, `crypto/sha256`, `mime`, `path/filepath` cover all of it.
- `internal/` core stays shallow: `internal/attach` is a leaf with one inbound
  edge (`turn`) and one outbound interface (its own fetcher).

**Security**
- SSRF: the fetch allow-list is host- and path-scoped to GitHub asset URLs only
  (`github.com/user-attachments/assets/<uuid>`,
  `github.com/<owner>/<repo>/assets/<user-id>/<uuid>`, `*.githubusercontent.com`),
  applied after parsing and before any dial, and re-applied on every redirect
  hop — a signed-URL 302 to an attacker host is the realistic exploit. A URL that
  does not match is ignored, not refused: an ordinary link in a body is normal.
- Landing filenames are litefactory-derived (never taken from alt text, the URL
  path, or `Content-Disposition`); no `..`, no path separators, no absolute paths
  reach a join.
- Byte ceilings are enforced during the stream (bounded reader + abort), never by
  trusting `Content-Length`; total-bytes and count ceilings apply across the whole
  turn, evaluated before per-file ceilings.
- Content type is decided by sniffing the fetched bytes; the served
  `Content-Type` header and the file extension are advisory only — a mismatch is
  a rejection, not a coercion.
- Raw bytes or base64 never appear in argv, in a git-tracked file other than the
  manifest, in a reply body, or in the event stream. The manifest and every reply
  line pass through the existing redactor before posting; the manifest itself is a
  worktree file, not a reply, and is not routed through it.
- The installation token is reused for the asset fetch and is never logged or
  written to the manifest. A signed URL's query string is itself a bearer
  credential — the manifest records scheme, host and path with the query string
  stripped, and says so.

**Tests that must stay green**
- The pinned client method set and no-interface-in-package test in
  `internal/github/surface_test.go` — amend deliberately alongside `FetchAsset`,
  never edit it just to pass.
- The no-forge-abstraction pin — `internal/github` stays a concrete `*Client`; no
  `Forge` interface may appear to make fetching testable.
- The redacted-body pin — the type-enforced redaction ordering holds unchanged
  with attachments in play.
- `internal/config`'s no-third-party-import purity test.
- `internal/harness`'s prompt purity and its byte-exact cold-prompt pin: a turn
  with no attachments must produce identical prompt bytes to today. This is the
  single most likely accidental regression in the whole feature.
- The daemon's tools/agent label-ladder tests: no-label-is-a-no-op, ambiguity
  counted before judging, and the rule that a refusal carries no field capable of
  echoing label text.

**Policy (numeric caps, shared across US-3/US-4/US-5/US-6)**
- Image cap 5 MiB raw per file, document (PDF/DOCX/PPTX) cap 10 MiB raw per file,
  25 MiB total and 10 attachments per turn — all four operator-configurable. No
  confirmed authoritative ceiling exists for either coding-agent harness; Claude's
  documented image ceiling is 10 MB base64, and base64 inflates raw bytes ~33%, so
  5 MiB raw sits under it with margin. **If the Claude Code invocation routes
  through Bedrock or Vertex rather than the direct Anthropic API, that tier's
  ceiling is 5 MB base64 and the image cap must drop to 3.5 MiB raw** — which
  backend is in use was not confirmed at plan time, hence the caps being
  operator-configurable rather than hard-coded.
- The installation-token 404-on-asset-fetch report is unverified; the one-shot
  unauthenticated/re-resolve retry (US-8) is the cheap mitigation regardless of
  whether the report is true, and the first real run is the smoke test.
- Coding-agent CLI limits are a fast-moving target; nothing here should be
  hard-coded on the strength of today's snapshot when a config knob is just as
  cheap.

**Ring-fenced**
- `dist/` — a live daemon runs from it; do not build into it, write to it, or
  restart it.
- `~/.litefactory/config.yaml` — not edited directly; only the version-controlled
  shipped template is touched.
- `git add`/`commit`/`stash`/`push` — no agent in this team runs these;
  litefactory commits and pushes itself.
- The store's hand-rolled embedded migrations — additive only, appended, never
  edited in place.
- `internal/redact`'s type discipline and `internal/harness`'s purity (no clock,
  no filesystem, no env) are not to be relaxed to make attachments easier to wire.

## Story Index

| ID | Title | Size | Depends On | Sub-issue |
|----|-------|------|-----------|-----------|
| US-1 | Rename tool label to `Tools: <Profile>` | M | — | (pending creation) |
| US-2 | Refuse legacy `lf:tools=` label loudly | S | US-1 | (pending creation) |
| US-3 | Ingest and hand off attached images | L | US-5, US-6 | (pending creation) |
| US-4 | Ingest and hand off PDF/DOCX/PPTX documents | M | US-3 | (pending creation) |
| US-5 | Gate attachment ingestion by tool profile | M | US-1 | (pending creation) |
| US-6 | Reject oversized/unsupported/unfetchable attachments without failing the turn | M | US-5 | (pending creation) |
| US-7 | Record an audit manifest for every attachment | M | US-3 | (pending creation) |
| US-8 | Fetch attachments reliably against short-lived signed URLs | M | US-3 | (pending creation) |

## Out of Scope

- Chat history and call-transcript ingestion (Slack JSON exports, VTT, SRT) —
  Phase 2+, untouched by the owner's reply.
- Spreadsheets (XLSX/XLS/XLSM/ODS), OpenDocument, RTF, DOC, archives
  (ZIP/GZ/TGZ), video, MSG/EML/DMP/PDB.
- litefactory-side conversion, parsing, OCR, thumbnailing or preview rendering of
  any attachment.
- Any Go DOCX/PPTX library dependency — conversion is the invoked agent's job via
  its own shell access, not litefactory's.
- Installing, detecting or health-checking `pandoc`/LibreOffice/Poppler —
  explicitly refused by the owner.
- Fetching non-GitHub URLs found in issue bodies (Google Docs, Confluence,
  arbitrary links).
- Image dimension/pixel-count validation and PDF page-count validation.
- Attachments on pull requests, or on comments older than the turn's ingested
  batch.
- litefactory posting images or files back to GitHub.
- Persisting attachments in SQLite — the manifest file is the record; no new
  table, no new migration required by this plan (US-7's task table notes a
  migration as conditional, only if the audit trail needs a queryable row beyond
  the manifest).
- Committing attachment bytes to git.
- Raising `max_concurrent_runs`, touching the store connection cap, or any
  config surface unrelated to attachments.

## Provenance

- Citation base: `4f22b48` (branch `lf/33-litefactory-needs-to-accept-attachments`, HEAD at plan time).
- Known-stale areas: none flagged beyond the Codex CLI capability findings in the
  2026-09-03 detour brief, which are themselves sourced from third-party reports
  and marked unconfirmed rather than stale.
- Re-pinning requirements before merge: re-verify the exact pinned-test names and
  line numbers cited in this brief and in each sub-issue's Architecture Notes
  against the implementation branch's actual HEAD — the architect review that
  produced them cites the citation base above, and Implement mode may have moved
  past it.
