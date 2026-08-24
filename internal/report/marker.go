package report

import (
	"fmt"

	"github.com/cloudvane/litefactory/internal/naming"
)

// MarkerPrefix is the substring the self-filter tests for (S-8).
//
// Both marker forms begin with it, which is the reason the filter is a
// substring test on this prefix and not a match on either whole marker: a
// comment litefactory wrote is a comment litefactory must not act on, whether
// it was a run report or a one-line reply. Exported so the filter and this
// package cannot drift apart.
const MarkerPrefix = "<!-- lf:"

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
