package daemon

import (
	"regexp"
	"strings"

	"github.com/cloudvane/litefactory/internal/report"
)

// toolLabelPrefix is the whole of the label vocabulary: one label, of the shape
// lf:tools=<profile>, naming a profile the operator wrote in config.yaml.
//
// The prefix is matched case-insensitively (D-6). GitHub label names keep the
// case they were typed in, and "LF:Tools=" is the same request as "lf:tools=" to
// everybody except a byte comparison — a permission request that silently
// missed and ran on the default surface is the one outcome this feature exists
// to prevent. The *value* after the "=" is a different matter: it is a lookup
// key, it is compared to the configuration exactly as it arrived, and
// [toolProfileName] admits lowercase only. So "LF:Tools=research" selects the
// research profile, and "LF:Tools=Research" is a malformed label and refused.
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
//   - No lf:tools= label: nothing is resolved and nothing is set. This is the
//     overwhelmingly common case and it must stay indistinguishable from the
//     behaviour before profiles existed.
//   - More than one: ambiguous, and refused. One is never picked and two are
//     never unioned — "which of these two permission grants did you mean" has no
//     safe answer, and inventing one is how a narrow label gets quietly widened
//     by a second one somebody else applied.
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
// cannot see in their own file.
func (d *Daemon) resolveTools(labels []string) toolDecision {
	var values []string
	for _, label := range labels {
		if len(label) < len(toolLabelPrefix) {
			continue
		}
		if !strings.EqualFold(label[:len(toolLabelPrefix)], toolLabelPrefix) {
			continue
		}
		values = append(values, label[len(toolLabelPrefix):])
	}

	switch len(values) {
	case 0:
		return toolDecision{}
	case 1:
	default:
		return toolDecision{Refusal: report.ReplyAmbiguousToolLabels}
	}

	name := values[0]
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
