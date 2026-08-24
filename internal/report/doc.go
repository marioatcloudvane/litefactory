// Package report renders the comment bodies litefactory posts on an issue.
//
// # What this package is for
//
// The comment is the whole user-visible output of a run. Everything else —
// the JSONL stream, the metadata sidecar, daemon.log — lives on Mario's disk
// and is read only when something has already gone wrong. So this package owns
// the one artefact a human actually reads, and its job is to answer "what
// happened?" without them having to open a terminal.
//
// It renders two shapes:
//
//   - a run report, for a run that finished, in either its success or its
//     failure form ([Reporter.RenderRun]);
//   - a run-less reply, for a comment that asked for something litefactory
//     cannot do — an unsupported command, or a bare "/spike" with no text
//     ([Reporter.RenderReply]).
//
// # It renders; it does not post
//
// This package takes plain data in and returns a [redact.Body] out. It reaches
// no network and touches no database: posting the body, recording the result
// comment id and advancing the watermark are one transaction owned by the
// caller. Keeping the two apart is what makes every body in here assertable in
// a unit test with no fixtures.
//
// # Why the renderers return redact.Body and never a string
//
// S-7 requires redaction to be the last transformation before the API call, and
// AC-4.6 requires that a token-shaped string in run output never reaches an
// outbound body. A renderer that returned a string would leave a window in
// which an unredacted body exists and could be posted; the caller would have to
// remember to close it. Returning a [redact.Body] — which only
// [redact.Redactor.Apply] can construct, and which github.PostComment is the
// only consumer of — closes that window in the type system instead. There is
// deliberately no exported way to obtain the body text from this package.
//
// The local copy in $LF_HOME/logs is never redacted. That asymmetry is the
// point: the verbatim output is what you need at 23:00, and the redacted one is
// what is safe to publish.
//
// # Size, and what gets cut first
//
// GitHub rejects a comment over 65536 bytes with a 422, so a body has to be
// assembled to fit. Redaction changes the length — usually shrinking, but
// "[redacted]" is ten bytes and grows anything shorter — so the sequence
// [redact.Redactor.Apply] owns is truncate to [redact.Budget] → redact →
// assert → hard-truncate. This package aims its own truncation at that budget
// and does the smart cut, in this priority order (AC-4.8):
//
//	marker, heading, status and the metadata list  never cut
//	the run's final result text                    cut second, from the end
//	the files-touched list                         capped by entry count
//	the run output tail in <details>               cut first, from the middle
//
// The parts a human needs to act — what happened, and the absolute path of the
// local log — are at the top and are never the thing that gets removed. The log
// tail is the expendable part, because the full copy of it is on disk and the
// body says where. Every cut carries an explicit notice naming the log path, so
// a truncated comment never looks like a complete one.
//
// The layout is also ordered so that the caller cannot be hurt by Apply's own
// last-resort hard truncate, which cuts from the bottom: everything load-
// bearing is above the log tail.
//
// # Markdown and untrusted text
//
// Result text and run output are written by a model that read third-party
// issue comments, so they are untrusted markup as well as untrusted content.
// Both are quoted inside a code fence long enough to contain any run of
// backticks they hold, and both are stripped of ANSI escapes and control
// characters — a NUL or a lone surrogate in a body is a 422 that reads like a
// mystery.
package report
