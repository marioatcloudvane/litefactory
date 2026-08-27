package daemon

import (
	"regexp"
	"strings"

	"github.com/cloudvane/litefactory/internal/report"
)

// toolLabelPrefix is the whole of the label vocabulary: one label, of the shape
// lf:tools=<profile>, naming a profile the operator wrote in config.yaml.
//
// It is *detected* case-insensitively and *accepted* only byte-exact, and the
// distance between those two is the whole of D-6. GitHub label names keep the
// case they were typed in, so a case-sensitive scan would not see "LF:Tools="
// at all: the run would proceed on the harness's default surface having
// silently ignored a permission request, which is the one outcome this feature
// exists to prevent. Seeing the label and then refusing it turns the typo into a
// loud refusal instead — a maintainer who mis-typed the prefix is told so on the
// thread rather than left to assume a profile took effect.
//
// So "lf:tools=research" resolves the research profile, and "LF:Tools=research"
// is a malformed label: refused, not resolved, and never a no-op.
//
// The value after the "=" is held to the same standard for the same reason. It
// is a lookup key, it is compared to the configuration exactly as it arrived,
// and [toolProfileName] admits lowercase only — so "lf:tools=Research" is
// malformed too.
const toolLabelPrefix = "lf:tools="

// toolProfileName is the shape of a profile name, and it is the same pattern
// internal/config validates the operator's profile keys against at startup.
//
// The two copies are deliberate rather than shared. This one is not validating
// configuration — that already happened, at boot, where it belongs. It is
// deciding whether a string that arrived from the forge is even the *shape* of a
// key, before it is used as one and long before internal/report is asked to
// print it back. A label that fails here never becomes a map lookup and never
// becomes text in a comment.
var toolProfileName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,39}$`)

// toolDecision is what the labels on an issue say about the tools its next run
// may have.
//
// There are exactly three outcomes and the zero value is the first of them:
// no label, so no profile, so the run proceeds on the harness's configured
// surface with argv byte-identical to what it was before this feature existed
// (AC-2.3). Otherwise either Refusal is set and nothing runs (C9), or a profile
// resolved and Tools carries the operator's own strings.
type toolDecision struct {
	// Refusal is why the run must not happen, or [report.ReplyInvalid] when it
	// may. It doubles as the reply litefactory owes the thread.
	Refusal report.ReplyKind

	// Profile is the resolved profile name, and it is set on exactly two paths:
	// a successful resolution, and the unknown-profile refusal, which is the one
	// refusal allowed to name what it could not find. It has passed
	// [toolProfileName] in either case, so it is safe to render — and
	// internal/report checks that for itself before it does (S-8).
	//
	// The malformed and ambiguous refusals leave it empty, and that is the whole
	// mechanism by which they cannot echo label text: there is no field for the
	// text to travel in (S-9).
	Profile string

	// MiscasedPrefix records that this malformed refusal was drawn by a label
	// that was seen case-insensitively but did not carry the prefix byte for
	// byte, as opposed to one whose value was the wrong shape. It is set on that
	// one branch and read only by the log line in [Daemon.refuseTools].
	//
	// It is a bool on purpose. The operator needs to know *that* a mis-cased
	// prefix was refused, because the reply cannot say so; nothing here can
	// carry *which* label said it, for the same reason Profile stays empty on
	// this path (S-9).
	MiscasedPrefix bool

	// Tools and AllowedTools are the profile's surface, copied. Empty unless a
	// profile resolved.
	Tools        []string
	AllowedTools []string
}

// refused reports whether this decision forbids the run.
func (d toolDecision) refused() bool { return d.Refusal != report.ReplyInvalid }

// resolveTools decides what an issue's labels grant its next run.
//
// The labels are the tick's fresh snapshot and are read again on every tick;
// nothing here is cached, and no API call is made for them. The rules, in order:
//
//   - No label whose prefix equals lf:tools= ignoring case: nothing is resolved
//     and nothing is set. This is the overwhelmingly common case and it must
//     stay indistinguishable from the behaviour before profiles existed. A
//     label that does not carry the prefix in any casing is not a tools label
//     at all and is passed over in silence, not refused.
//   - More than one such label: ambiguous, and refused. One is never picked and
//     two are never unioned — "which of these two permission grants did you
//     mean" has no safe answer, and inventing one is how a narrow label gets
//     quietly widened by a second one somebody else applied. A mis-cased label
//     counts as one of the two; see the comment on the count below.
//   - The single label's prefix, byte for byte: it must be the lowercase
//     lf:tools=. Anything else is malformed, and refused (AC-3.5). This is the
//     accept-strictly half of D-6 and it is the only reason the detection above
//     is lenient at all.
//   - A value that is not the shape of a profile name, including the empty
//     string: malformed, and refused.
//   - A well-formed value that is not a key of the profile map: unknown, and
//     refused. An absent or empty tool_profiles map takes this path too, and
//     that is D-5: a label asking for a named permission set, on a daemon that
//     has none configured, is answered by refusing rather than by silently
//     running with everything the default surface allows.
//   - A key of the map: resolved.
//
// The lookup is an exact map index on the value's own bytes. It is not trimmed,
// lower-cased or otherwise repaired first, because the profile keys were not
// normalised when the operator's config was validated either: a lookup that
// canonicalised more than the key space does could match a profile the operator
// cannot see in their own file. The prefix is not repaired either, for the
// stronger reason that repairing it is indistinguishable from accepting it.
func (d *Daemon) resolveTools(labels []string) toolDecision {
	// A label that was seen, with the two facts the rules below need about it:
	// whether the prefix survived a byte comparison as well as a folded one, and
	// what followed it. Neither the label nor this struct leaves this function.
	type seen struct {
		exactPrefix bool
		value       string
	}

	var found []seen
	for _, label := range labels {
		if len(label) < len(toolLabelPrefix) {
			continue
		}
		prefix, value := label[:len(toolLabelPrefix)], label[len(toolLabelPrefix):]
		if !strings.EqualFold(prefix, toolLabelPrefix) {
			continue
		}
		found = append(found, seen{exactPrefix: prefix == toolLabelPrefix, value: value})
	}

	// Counted before any of them is judged, and a mis-cased label counts in the
	// total. Two detected labels are two permission requests however they were
	// typed, so "lf:tools=research" beside "LF:Tools=build" is ambiguous rather
	// than malformed, and that ordering is chosen rather than incidental:
	//
	//   - The malformed reply tells the maintainer to fix the shape of a label.
	//     Doing so would leave them with two well-formed labels and a second
	//     refusal, so it is the less useful of two true answers — removing one
	//     label is the thing that has to happen first either way.
	//   - Judging one label before counting them means choosing which of two
	//     competing grants gets to describe the failure. Choosing between them
	//     is exactly what the ambiguous rule exists to refuse, and it would make
	//     the refusal depend on the order GitHub happened to return the labels.
	//
	// Both answers refuse and both leave Profile empty, so neither can resolve
	// one of the two, echo either, or fall through to the default surface (C9).
	switch len(found) {
	case 0:
		return toolDecision{}
	case 1:
	default:
		return toolDecision{Refusal: report.ReplyAmbiguousToolLabels}
	}

	label := found[0]
	if !label.exactPrefix {
		return toolDecision{Refusal: report.ReplyMalformedToolLabel, MiscasedPrefix: true}
	}
	name := label.value
	if !toolProfileName.MatchString(name) {
		return toolDecision{Refusal: report.ReplyMalformedToolLabel}
	}
	profile, ok := d.toolProfiles[name]
	if !ok {
		return toolDecision{Refusal: report.ReplyUnknownToolProfile, Profile: name}
	}

	// Copied, and this is not a formality (P-3). The map is the configuration
	// this process is running on and it lives until the process exits; the
	// harness request these slices end up on is mutated in place on the
	// cold-rerun path. One append into spare capacity downstream would rewrite
	// the operator's profile for every later run in the process.
	return toolDecision{
		Profile:      name,
		Tools:        append([]string(nil), profile.Tools...),
		AllowedTools: append([]string(nil), profile.AllowedTools...),
	}
}
