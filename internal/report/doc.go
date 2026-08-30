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
// # Links, without a forge client
//
// A run report links the session's research brief, so the short version on the
// issue reaches the long one in a click. The URL is built here, from
// [naming.Ref] and the run's sha_after, and from nothing else: the ref already
// carries the owner and the repository, so the permalink needs no forge client,
// no base URL and no I/O — importing internal/github for a hostname would put a
// token and seven endpoints inside the package whose whole promise is that it
// has neither.
//
// Two rules make a link honest enough to put in front of someone.
//
// It is pinned to the run's sha_after, never to the branch and never to the
// commit sha. A branch-relative URL shows whatever the branch holds when it is
// opened, which for a report of run 3 may be what run 7 wrote; and the commit
// sha is empty whenever claude committed for itself through its own Bash tool,
// which is a supported path with real, published work behind it.
//
// And it is rendered only for a pushed run. Unpushed is not a rare failure —
// the branch not moving and an abandoned run are both ordinary — and a link
// into a commit that exists only in a local worktree is a guaranteed 404. No
// link is better than a dead one, because a dead one invites the click.
//
// The path to the brief is derived rather than discovered: this package cannot
// look at a filesystem, and looking for the brief in the files-touched list
// would link it only on the run that rewrote it, never on the run that merely
// read it. See [naming.AITeamSessionDir]. The rendered line says the derivation
// is a derivation, because a session that has not run Research has no brief and
// the report must not claim otherwise.
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
//	marker, heading, status, metadata, reply notice  never cut
//	the run's final result text                      cut second, from the end
//	the files-touched list                           capped by entry count
//	the run output tail in <details>                 cut first, from the middle
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
