// Package attach defines the shared vocabulary for attachment ingestion: why
// litefactory declines to ingest a given attachment, and the shape one
// rejection takes wherever it is reported.
//
// # Rejections are values, not errors
//
// A [Rejection] is data, not something that propagates up an error-returning
// chain. Ingestion code that turns an attachment away builds a [Rejection]
// and keeps going: other attachments in the same turn still ingest (AC-6.1),
// and the run itself never fails just because one attachment did not make it
// in. The only error this package defines, [ErrInvalidRejection], guards
// construction — a caller that tries to build a [Rejection] with no name or
// an invalid [RejectionReason] made a programming mistake, which is a
// different thing from "this attachment did not qualify."
//
// # Five reasons, one shape
//
// [RejectionReason] enumerates the five ways an attachment can fail to
// ingest: too large for its type ([ReasonOversized]), a sniffed type
// litefactory does not accept ([ReasonUnsupportedType]), a fetch that could
// not complete ([ReasonFetchFailed]), and the two turn-wide ceilings, which a
// caller evaluates before any per-file ceiling and which can reject an
// attachment that would otherwise fit within its own type's limit: too many
// attachments already accepted this turn ([ReasonOverCount]) and too many
// attachment bytes already accepted this turn ([ReasonOverTotalBytes]).
// Every ingestion path reports into this one vocabulary rather than each
// inventing its own.
//
// # What reaches a comment, and what does not
//
// [Rejection.Name] is the attachment's filename as declared by the forge —
// third-party-controlled, so [Rejection.Line] sanitizes it before it is ever
// echoed anywhere, the same way internal/report re-checks forge-supplied
// text immediately before rendering it into a comment rather than trusting
// whatever validated it upstream. [RejectionReason.ReasonText] is
// litefactory's own classification and is always safe to state directly. No
// line this package composes, and no [Rejection] value built the way
// [NewRejection] requires, ever carries raw attachment bytes, a base64 blob,
// a fetch URL, or the installation token (AC-6.5) — there is no field on
// [Rejection] that could carry any of them.
//
// # Freestanding, for now
//
// This package has no live caller yet: attachment fetch, sniff and land
// logic does not exist until a later story. What is here — the reason
// vocabulary, the reply-line composer, and the manifest-record shape — is
// built so that story has somewhere to report a no from the moment it
// starts fetching, rather than inventing its own vocabulary under deadline.
//
// A [Rejection] is deliberately not routed through internal/report's
// ReplyKind/Reply mechanism, even though that is the pattern most other
// cross-package reply shapes in this codebase follow. That mechanism
// renders the lf:reply marker specifically because no run happened; an
// attachment rejection happens during a run that still executes, and other
// attachments in the same turn still ingest (AC-6.1). Routing a rejection
// through it would tag a turn that did run with a marker asserting that it
// did not. The natural home for surfacing a rejection alongside a run that
// did happen is the ordinary run report, which already carries structured,
// non-textual detail about the run (see report.Run.Files) — deciding
// exactly how is left to the story that also builds the attachment
// manifest file itself, since it needs the full picture to get that
// integration right.
package attach
