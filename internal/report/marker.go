package report

import (
	"fmt"

	"github.com/cloudvane/litefactory/internal/naming"
)

// MarkerPrefix is the literal the self-filter tests for (S-8).
//
// Both marker forms begin with it, which is the reason the filter tests this
// prefix and not either whole marker: a comment litefactory wrote is a comment
// litefactory must not act on, whether it was a run report or a one-line reply.
// Exported so the filter and this package cannot drift apart.
//
// The filter matches it at the start of a line, not anywhere in the body, so
// where this package puts the marker is load-bearing rather than cosmetic: both
// renderers write it at byte zero and nothing else in a body may begin a line
// with it. TestEveryBodyBeginsWithItsMarker is what holds that end up; see
// turn.HasMarker for the other.
const MarkerPrefix = "<!-- lf:"

// ReplyPlainlyNotice is the line every body litefactory posts ends with: how to
// answer it without the answer being thrown away.
//
// It is the second half of the fix for a reply that never started a run. The
// first half is turn.HasMarker matching the marker at the start of a line, which
// makes GitHub's own quote-reply safe — the "> " it puts in front of every
// quoted line means the marker no longer starts one. What that cannot make safe
// is a marker pasted back without the quote: at the start of a line it is
// indistinguishable from litefactory's own output, and litefactory has no way to
// tell the two apart short of trusting authorship over the marker, which AC-2.14
// forbids. So the remaining shape is answered by saying so, on the comment
// itself, where the person about to do it is looking.
//
// It deliberately does not contain [MarkerPrefix]. A body carrying a second one
// would put a marker somewhere other than byte zero, which is the property
// TestEveryBodyBeginsWithItsMarker and the reply tests hold this package to.
//
// Exported so the tests asserting it reached a body do not have to restate the
// wording, which would make it possible to change one and not the other.
const ReplyPlainlyNotice = "_Replying? A plain comment is enough. Quoting this one back with " +
	"GitHub's quote-reply is fine — the `> ` keeps litefactory's hidden marker from matching — but a " +
	"marker pasted back at the start of a line makes litefactory read your comment as its own and drop it._"

// StatusMarker renders the marker for a run report:
//
//	<!-- lf:status task=github:acme/acme-api#123 run=7 -->
//
// It carries the task ref and the run ordinal. The ref makes a status comment
// findable again after a database loss, and the ordinal says which run of that
// task produced it.
//
// The ref is interpolated verbatim, so callers validate it first — see
// [naming.Ref.Validate], which is what confines a ref to characters that cannot
// close an HTML comment. [Reporter.RenderRun] does that validation.
func StatusMarker(ref naming.Ref, run int) string {
	return fmt.Sprintf("%sstatus task=%s run=%d -->", MarkerPrefix, ref, run)
}

// ReplyMarker renders the marker for a run-less reply:
//
//	<!-- lf:reply task=github:acme/acme-api#123 -->
//
// There is no run attribute, because no run happened: an unsupported command
// and an empty /spike are answered without starting anything. Writing run=0
// instead would make the marker parseable as a report of a run that does not
// exist, so the attribute is absent rather than zero.
func ReplyMarker(ref naming.Ref) string {
	return fmt.Sprintf("%sreply task=%s -->", MarkerPrefix, ref)
}
