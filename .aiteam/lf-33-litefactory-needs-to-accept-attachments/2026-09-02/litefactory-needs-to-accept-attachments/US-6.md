# US-6 — Reject oversized/unsupported/unfetchable attachments without failing the turn

> As an operator watching a run, I want an attachment that is too big, an
> unsupported type, or unfetchable to be reported on the issue and skipped, so
> that one bad attachment never takes down an otherwise-good run.

Delivers the shared rejection vocabulary, reply and manifest-record shape that
every ingestion path (US-3, US-4, US-8) reports into.

## Why This Matters

Attachments arrive from other people, on issues litefactory does not control, so
something will eventually be too big, the wrong type, or simply gone by the time
litefactory tries to fetch it. This story is what keeps any one of those from
becoming an outage: every reason an attachment can fail turns into a plain
sentence on the issue and a line in the audit trail, and the run itself keeps
going regardless. It is the piece that makes it safe to point this feature at
attachments litefactory did not choose and cannot fully trust.

## Architecture Notes

- Defines a typed rejection-reason vocabulary in `internal/attach` (oversized,
  unsupported type, fetch failed, over per-turn count, over per-turn total
  bytes) that US-3, US-4 and US-8 all report into — this story owns the shape,
  not each ingestion path individually.
- Needs a new `report.ReplyKind` and its case in `internal/turn/report.go`'s
  `replyKind()` switch, in the same commit as detection — the same `report`/`turn`
  coupling as US-2's refusal reply.
- Total-bytes and count ceilings are evaluated before per-file ceilings, across
  the whole turn — an attachment can be rejected for a turn-level reason even
  when it individually fits within its own type's ceiling.
- Every rejection reply passes through the existing redactor before posting, and
  never carries a raw URL, byte content, or the installation token.
- Depends on US-5 — there is nothing to reject until the gate exists and a fetch
  attempt is possible.

## Tasks

| ID | Title | Type | Depends On | Est |
|----|-------|------|-----------|-----|
| T-6.1 | Define the typed rejection-reason vocabulary in `internal/attach` | code | — | S |
| T-6.2 | Add `report.ReplyKind` + `replyKind()` case for attachment rejections | code | T-6.1 | S |
| T-6.3 | Compose the rejection reply line(s) — reason only, no URL/bytes/token | code | T-6.1, T-6.2 | M |
| T-6.4 | Ensure every rejection also produces a manifest-record entry (data shape here; file format in US-7) | code | T-6.1 | S |
| T-6.5 | Regression test: a rejected attachment never fails the turn; the run always proceeds | test | T-6.3, T-6.4 | M |

## Acceptance Criteria

| ID | Criterion |
|----|-----------|
| AC-6.1 | An attachment exceeding its type's per-file ceiling is rejected with a reply naming "too large"; other valid attachments in the same turn still ingest. |
| AC-6.2 | An attachment of an unsupported or unrecognised sniffed type is rejected with a reply naming "unsupported type," never coerced from its declared extension. |
| AC-6.3 | A turn whose combined attachments exceed `attachment_max_total_bytes` or `attachment_max_count` rejects the excess and reports which attachments were dropped. |
| AC-6.4 | A fetch failure (network error, non-401/404 error) produces a rejection reply and a manifest record, never a turn failure. |
| AC-6.5 | No rejection reply or manifest record ever contains raw bytes, base64, or a bearer-bearing query string. |
