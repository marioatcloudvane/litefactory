package report

import (
	"fmt"
	"strings"

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

// StoryMarker renders the marker for the sub-issue a user story ships as:
//
//	<!-- lf:story task=github:acme/acme-api#123 story=C1 -->
//
// It carries the *parent* task ref — the issue the plan was asked for on — and
// the story's own id within that plan. The pair is the identity of one
// sub-issue: it is what a later run looks for among the parent's sub-issues
// before creating anything, so that a bulk creation interrupted halfway can be
// re-run without filing every story a second time (AC-C1.4).
//
// Both halves are interpolated verbatim, so both are validated first. The ref
// is [naming.Ref.Validate]'s job, as it is for [StatusMarker]; the story id is
// [Reporter.RenderCompleteStory]'s, which refuses anything outside the shape a plan
// gives a story. That is what confines the marker to characters which cannot
// close the HTML comment early and leave the rest of it rendered on the page.
//
// It begins with [MarkerPrefix] like the other two, which has a second
// consequence here that it does not have for a comment: a sub-issue *body*
// carrying this marker is a body the self-filter reads as litefactory's own, so
// the daemon will not take a story it wrote itself as an instruction to run
// (S-8, AC-2.14).
func StoryMarker(ref naming.Ref, storyID string) string {
	return fmt.Sprintf("%sstory task=%s story=%s -->", MarkerPrefix, ref, storyID)
}

// StoryMergedMarker renders the marker of the note posted on a story when its
// branch landed on the integration branch:
//
//	<!-- lf:story-merged task=github:acme/acme-api#123 story=story-002 commit=<sha> -->
//
// Like the other markers it begins with [MarkerPrefix], so the self-filter
// ignores the comment. The story key and commit are bound to characters that
// cannot close the comment or start a second marker.
func StoryMergedMarker(ref naming.Ref, storyKey, commit string) string {
	return storyMergedPrefix(ref, storyKey) + markerToken(commit) + " -->"
}

func storyMergedPrefix(ref naming.Ref, storyKey string) string {
	return fmt.Sprintf("%sstory-merged task=%s story=%s commit=", MarkerPrefix, ref, markerToken(storyKey))
}

// ParseStoryMergedMarker reads the commit from a story-merged marker that opens
// body and names exactly this task and story. It reports false for any other body.
func ParseStoryMergedMarker(body string, ref naming.Ref, storyKey string) (commit string, ok bool) {
	rest, found := strings.CutPrefix(body, storyMergedPrefix(ref, storyKey))
	if !found {
		return "", false
	}
	commit, _, found = strings.Cut(rest, " -->")
	if !found || commit == "" || strings.ContainsAny(commit, " \t\r\n") {
		return "", false
	}
	return commit, true
}

// ImplementHandoffMarker renders the marker of the one handoff report the
// parent of an Implement job keeps:
//
//	<!-- lf:implement-handoff task=github:acme/acme-api#123 -->
func ImplementHandoffMarker(parent naming.Ref) string {
	return fmt.Sprintf("%simplement-handoff task=%s -->", MarkerPrefix, parent)
}

// markerToken keeps a value inside one marker field: anything that is not a
// letter, digit or one of ._/- becomes an underscore.
func markerToken(value string) string {
	out := make([]rune, 0, len(value))
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-', r == '/':
			out = append(out, r)
		default:
			out = append(out, '_')
		}
	}
	return string(out)
}
