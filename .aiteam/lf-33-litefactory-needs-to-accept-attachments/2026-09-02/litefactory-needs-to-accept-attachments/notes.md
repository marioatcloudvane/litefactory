# Notes — Implement mode, litefactory-needs-to-accept-attachments

## Resumed run (2026-09-04)

Prior run was cut off by a rate-limit/budget timeout. Verified via git log/build/test
before resuming: US-1, US-2, US-5 already landed and committed (`85b3dc9`,
`b7aeff6`); `go build/vet/test ./...` green. US-3, US-4, US-6, US-7, US-8 had no
code yet (`internal/attach` did not exist). Resuming with the 5 remaining stories run
**sequentially** (not the default parallel Phase 1 pattern) in the plan's Workflow
order — US-6, US-3, US-7, US-4, US-8 — because all funnel through the new shared
`internal/attach` package and US-7/US-8 both touch the manifest shape, which is a
genuine file-overlap risk under parallel agents.

## US-6 — DONE, with one flagged task-sequencing deviation

Engineer built `internal/attach/{doc.go,reason.go,errors.go,rejection.go}`: the typed
`RejectionReason` vocabulary (T-6.1), the `Rejection` struct doubling as manifest-record
shape and reply-line composer (T-6.3/T-6.4), and `attachmentNameSpan` (a salvaged-prefix
filename sanitizer mirroring `internal/report`'s `commandSpan`).

**Deviation on T-6.2:** the engineer did not add a `report.ReplyKind` case or a
`internal/turn/report.go` `replyKind()` case. Reasoning given: `report.Reply`/
`RenderReply`'s doc comments and shared sentence constants are structurally reserved
for "no run happened" ("Nothing was run"), which directly contradicts AC-6.1's
requirement that the run proceeds and other attachments still ingest when one is
rejected — wiring a rejection through `report.Reply` would mislabel a turn that did
run as one that didn't. T-6.2/T-6.3's intent (a place to compose rejection text) was
instead satisfied inside `internal/attach` itself (`Rejection.Line()`/`Lines()`), as
freestanding functions with no live caller yet.

Engineer's recommendation: the actual wiring point for posting rejection text to the
issue should be `report.Run`/`RenderRun` (already carries structured non-textual run
detail via `Files []git.FileChange`), and that integration naturally belongs to US-7
(which already owns building `attachments.md` and needs the full picture to decide
correctly). **Action:** the US-7 engineer invocation below is instructed to pick up
this integration point and to reply-post via `report.Run`/`RenderRun`, not via a new
`report.ReplyKind` case. This is a task-level implementation-approach deviation, not
a plan-level architectural one (the vocabulary/shape and redaction/no-raw-data
guarantees from US-6's ACs are all still met) — flagging for code review's attention
rather than treating as a plan violation requiring `.aiteam/AC_AMENDMENTS.md`. Will
confirm code reviewer concurs with that read during Phase 1.5.

T-6.5 (regression test) intentionally left to the unit test engineer in Phase 3, per
this team's convention that implementation engineers do not write tests.

## Second resumption (2026-09-04, new run)

Prior resumed run was itself cut off after landing US-6 (commit `87cc675`). Verified
current state before continuing: `go build ./...` green; `go vet ./...` and
`go test ./...` both fail for two reasons, neither belonging to US-6's own files
(`internal/attach/{doc,errors,reason,rejection}.go`), both first appearing in the
same `87cc675` commit alongside US-6's:

- `internal/github/scratch_test.go` fails to compile (`client.BareDo` called with
  an extra `context.Context` argument the current signature doesn't take). Name and
  shape suggest a leftover ad-hoc exploration file, not a real test — this team's
  engineers don't write tests as part of implementation tasks.
- `internal/config/paths.go` imports `internal/naming`, which fails
  `internal/config`'s pinned `TestValidationHasNoThirdPartyImports` purity test.
  `internal/naming/paths.go` also exists (18 lines, same shape) — looks like a
  confused duplicate: the `config.Paths` landing-dir method (US-3's T-3.7) plus a
  first, wrong-package attempt at `FetchAsset`'s home (US-3's T-3.3, in
  `internal/github/assets.go` + `errors.go`) apparently got started opportunistically
  during the US-6 session, ahead of the dependency order, and left broken when that
  session was cut off.

None of this is a scope or architecture question — it's leftover partial/broken
code from an interrupted run, on files that US-3 owns outright (T-3.3, T-3.7).
Folded into the US-3 engineer's brief below with an explicit instruction to inspect,
reconcile or replace, and leave the repo green rather than starting T-3.3/T-3.7 from
a blank slate.

Continuing in the plan's Workflow order, sequentially (per the first resumption's
documented reasoning: US-3/US-4/US-6/US-7/US-8 all funnel through the new shared
`internal/attach` package and US-7/US-8 both touch the manifest shape, a real
file-overlap risk under parallel agents): US-3 next, then US-7, US-4, US-8.

## US-3 — DONE, applied by hand from the CLI (2026-09-04)

Run 7 was stopped at 42 minutes rather than left to time out. Four Implement runs
(90m timeout, 40m, 90m timeout, 42m) had produced four of eight stories, so US-3
was built directly instead. The three causes those runs died of are now fixed and
in this branch: the Bash allow-list gained grep/sed -n/head/tail/wc, the
orchestrator learned that a thrashed agent is deterministic, and
`internal/github/scratch_test.go` — which no agent could delete — is gone.

What already existed from earlier runs and was kept: T-3.2 (the SSRF allow-list)
and T-3.3 (`Client.FetchAsset`) in `internal/github/assets.go`, and
`naming.AttachmentsDir` for T-3.7's landing path.

New in `internal/attach`:

- `scan.go` — finds attachments in markdown, HTML `<img>` and bare URLs, across
  several bodies at once, deduplicating by URL. A URL that is not GitHub-hosted
  never becomes a Ref, which is what makes AC-3.2 true by construction rather
  than by a check further down.
- `sniff.go` — `http.DetectContentType` over the first 512 bytes decides the
  type and the extension. The declared Content-Type and the filename are never
  consulted (AC-3.5).
- `name.go` — mints the on-disk name from litefactory's own ordinal plus a
  slug. `name_test.go` pins it against traversal, separators, NUL, leading dots
  and dashes, and a 4KB name.
- `ingest.go` — bounded concurrent fetch (T-3.11), stream-time size enforcement
  with mid-stream abort (T-3.5), the ceiling precedence, and `PromptSection`.

**Deviation from the spec, deliberate.** US-3's architecture note says attachments
land under `$session/attachments/` via a new `config.Paths` method. They land
under `<RunsDir>/NNNN.attachments` instead, $LF_HOME-rooted. That decision was
made by an earlier run and is kept: an attachment inside the worktree shows up in
that repository's own `git status` and can collide with the coding agent's
commits. The first attempt at the `config.Paths` method also broke
`internal/config`'s purity test by importing `internal/naming`; the helper
belongs in `internal/naming`, where it now is.

**Two things worth knowing for review.**

Writes go through `os.Root`, not through joined path strings. `mintFileName` is
proven safe by its own test, but this is the one place in litefactory where text a
stranger typed on a public issue reaches a file creation, and `os.Root` makes
containment a kernel guarantee rather than a property of my sanitizer. It also
retires the one `gosec` G304 finding this story introduced, without a `#nosec`.

Cancellation had a real bug, found by its own test. `fetchAll` raced context
cancellation against acquiring a worker slot in one `select`; when both are ready
Go picks at random, so a cancelled run still dialed for however many workers won
the toss. The context is now re-checked after the slot is held.

Not done here, by design: US-4 (documents), US-7 (the manifest) and US-8 (retry
hardening). `Rejection` values are produced and returned but nothing renders them
yet — that is US-7's integration point, as the US-6 engineer recommended.

## US-4, US-8, US-7 — DONE, applied by hand from the CLI (2026-09-04)

Built in dependency order after US-3: types, then the retry that adds the route
field, then the manifest that records it.

**US-4 — documents.** PDF is a magic number and needs nothing new. DOCX and PPTX
are not: both are ordinary ZIP archives beginning `PK`, exactly like
holiday-photos.zip, and what tells them apart is the part inside —
`word/document.xml` versus `ppt/presentation.xml` — which in a real Office file
sits well past the 512 bytes the sniffer sees, because `[Content_Types].xml` is
stored first and is larger than that alone. So a ZIP is accepted provisionally,
landed under the document ceiling, and classified from its entry names with
stdlib `archive/zip` (no parsing dependency, per the story). A plain archive
renamed `.docx` is refused and removed. The cost is that an unsupported archive
touches disk before it is rejected; it takes the same removal path an oversized
file does.

**US-8 — retry and budgets.** `internal/github` stays single-shot; the retry and
both timeouts (30s per fetch, 120s per turn) live in `internal/attach`, per the
plan's CONC-6 resolution. `internal/github/assets_policy_test.go` now enforces
that split from both sides: `assets.go` may not import `time`, contain a loop, or
call `time.Sleep`, and `internal/attach` must still carry the timeouts and the
retry route. Without it, the natural fix for "attachments sometimes 404" is three
lines in the wrong package that compile and pass everything else.

Two deliberate readings worth review:

- *"Re-resolved URL"* (AC-8.1) is satisfied by asking again with the same
  `Ref.URL`. That URL is the stable `user-attachments` address; GitHub answers it
  with a redirect to a short-lived **signed** URL, and `FetchAsset` follows that
  redirect fresh on every call. So a second call genuinely re-resolves the
  signature, which is precisely the expiry the story is defending against.
- *Retry scope.* The story names `ErrAssetUnauthorized` and 404. `internal/attach`
  cannot see that sentinel — it does not import `internal/github`, by design and
  by test — so it retries **any** fetch failure exactly once. That is a superset
  of the required behaviour at the cost of one round trip on an attachment that
  was going to be reported unfetchable anyway. A decision (unsupported type,
  oversized) is never retried. If the distinction ever earns its keep, the honest
  way to get it is an error-classifying method on the `Fetcher` interface, not an
  import.

**US-7 — the manifest.** `attachments.md` in the worktree's `.aiteam/`, so
litefactory's end-of-run commit picks it up like any other change; rewritten per
run, with history left to git. No table, no migration (AC-7.4). Not routed
through `internal/redact` (AC-7.3) — that guards text going to a forge comment,
and what keeps this file safe is instead that `stripQuery` runs at record
construction, so a signed URL's credential never reaches a `Record` at all.
T-7.3's proposed store migration stays dropped, as the story instructs.

**One bug found by its own test.** The manifest renders a markdown table, and a
pipe is a legal filename character that `attachmentNameSpan` does not stop at —
and a pipe survives inside backticks too, because a table is split into cells
before code spans are parsed. An attachment named `a|b` therefore split its row
and shifted every value after it, which in an audit record means reporting the
wrong byte count against the wrong file. Escaped where the table is built, not in
the shared sanitizer, which has no idea it is being put in one.

Also corrected while reviewing rendered output: an oversized attachment was
recorded with route "not fetched" when it had in fact been fetched and then
overran. Post-fetch rejections now carry their route.

All five remaining stories of this plan are now implemented. `make check` clean:
22 packages, golangci-lint 0 issues.

## Code Review Advisories

(populated after Phase 1.5)
