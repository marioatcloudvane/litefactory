package daemon

import (
	"regexp"
	"strings"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/report"
)

// toolLabelPrefix is the whole of the label vocabulary: one label, of the shape
// "Tools: <Profile>", naming a profile the operator wrote in config.yaml.
//
// It is *detected* case-insensitively and *accepted* only byte-exact, and the
// distance between those two is the whole of D-6 — the same split
// [agentLabelPrefix] documents, byte for byte. GitHub label names keep the
// case they were typed in, so a case-sensitive scan would not see
// "tools: research" at all: the run would proceed on the harness's default
// surface having silently ignored a permission request, which is the one
// outcome this feature exists to prevent. Seeing the label and then refusing
// it turns the typo into a loud refusal instead — a maintainer who mis-typed
// the prefix is told so on the thread rather than left to assume a profile
// took effect.
//
// So "Tools: Research" resolves the research profile, and "tools: Research"
// is a malformed label: refused, not resolved, and never a no-op.
//
// The value after the prefix is held to a related but not identical standard.
// [toolProfileName] requires an upper-case first letter — the same shape
// [agentKindName] requires of a coding-agent name — and the whole value is
// then lower-cased before it is used as the lookup key, so "Tools: Research"
// and a configured profile key of "research" are the same profile. A value
// that does not start with an upper-case letter, such as "Tools: research",
// is malformed: the display shape is part of what is checked, not just the
// character set.
const toolLabelPrefix = "Tools: "

// legacyToolLabelPrefix is the label syntax this feature shipped with before
// the rename to [toolLabelPrefix]: lf:tools=<profile>. It is matched
// case-insensitively, the same way [toolLabelPrefix] is detected, and any
// match at all — regardless of casing, of the value that follows it, or of
// how many such labels an issue carries — is refused (US-2).
//
// The refusal exists so an issue that still carries the old label is never
// silently treated as carrying no label at all. Before this rung existed, an
// operator who had not yet relabelled an issue would have a run resolve with
// no tool profile and no indication anything was wrong — the exact silent
// permission drop this whole feature exists to prevent, just relocated to the
// rename instead of the original feature. Refusing loudly, and naming the new
// notation in the reply, is what lets that operator notice and relabel
// instead of assuming a profile is still active.
//
// It is checked before [toolLabelPrefix]'s own ladder runs (see
// [Daemon.resolveTools]), so an issue carrying both an old-style and a
// new-style label is refused for carrying the old one rather than falling
// through to resolve the new one — the old label is a syntax error on this
// issue regardless of what else is present, and it is not this function's
// job to guess which of the two the operator meant to leave behind.
const legacyToolLabelPrefix = "lf:tools="

// toolProfileName is the shape of a tool-profile label's *displayed* value —
// deliberately the same pattern [agentKindName] holds a label's displayed
// agent name to, not the pattern internal/config validates a profile's
// stored *key* against (config.profileKeyRE). The two differ on purpose: a
// human types "Tools: Research" onto a label, upper-case first letter to
// match [agentLabelPrefix]'s "Coding Agent: Claude" convention, and this
// package lower-cases the whole value before it is ever used as a map key
// into d.toolProfiles — see [Daemon.resolveTools]. A label that fails this
// check never becomes a map lookup and never becomes text in a comment.
var toolProfileName = regexp.MustCompile(`^[A-Z][A-Za-z0-9_-]{0,39}$`)

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
	// [toolProfileName] in either case (after lower-casing), so it is safe to
	// render — and internal/report checks that for itself before it does (S-8).
	//
	// Every other refusal, [report.ReplyLegacyToolLabel] included, leaves it
	// empty, and that is the whole mechanism by which they cannot echo label
	// text: there is no field for the text to travel in (S-9).
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

	// Tools and AllowedTools are the profile's Claude-stanza surface, copied.
	// Empty unless a profile resolved and that profile carries a claude:
	// stanza (T-7.1) — a resolved profile with no claude: stanza leaves both
	// empty here without refusing anything at this stage: whether that is a
	// problem depends on which agent the run actually uses, and this
	// function does not know that. See [Resolved].
	Tools        []string
	AllowedTools []string

	// Resolved is the full profile as configured — both its claude: and
	// codex: stanzas — kept alongside the already-narrowed Tools/AllowedTools
	// above so a later stage (T-7.2's per-agent narrowing, in tick.go) can
	// check whether it carries a stanza for the agent this run will actually
	// use, without this function needing to know what that agent is. nil on
	// every unresolved path (no label, or any refusal).
	Resolved *config.ToolProfile
}

// refused reports whether this decision forbids the run.
func (d toolDecision) refused() bool { return d.Refusal != report.ReplyInvalid }

// resolveTools decides what an issue's labels grant its next run.
//
// The labels are the tick's fresh snapshot and are read again on every tick;
// nothing here is cached, and no API call is made for them. The rules, in order:
//
//   - Any label whose prefix equals [legacyToolLabelPrefix] ignoring case: the
//     old lf:tools= syntax, refused loudly rather than treated as absent
//     (US-2). This is checked before anything below, and unconditionally —
//     casing, the value, and how many such labels are present are all
//     irrelevant to this rung, because the label itself is a syntax error on
//     this issue regardless of what it says.
//   - No label whose prefix equals "Tools: " ignoring case: nothing is
//     resolved and nothing is set. This is the overwhelmingly common case and
//     it must stay indistinguishable from the behaviour before profiles
//     existed. A label that does not carry the prefix in any casing is not a
//     tools label at all and is passed over in silence, not refused.
//   - More than one such label: ambiguous, and refused. One is never picked and
//     two are never unioned — "which of these two permission grants did you
//     mean" has no safe answer, and inventing one is how a narrow label gets
//     quietly widened by a second one somebody else applied. A mis-cased label
//     counts as one of the two; see the comment on the count below.
//   - The single label's prefix, byte for byte: it must be "Tools: ", exactly.
//     Anything else is malformed, and refused (AC-3.5). This is the
//     accept-strictly half of D-6 and it is the only reason the detection above
//     is lenient at all.
//   - A value that is not the shape [toolProfileName] describes, including the
//     empty string: malformed, and refused.
//   - A well-formed value, lower-cased, that is not a key of the profile map:
//     unknown, and refused. An absent or empty tool_profiles map takes this
//     path too, and that is D-5: a label asking for a named permission set, on
//     a daemon that has none configured, is answered by refusing rather than
//     by silently running with everything the default surface allows.
//   - A key of the map: resolved.
//
// The lookup is an exact map index on the lower-cased value. The profile keys
// themselves were not normalised when the operator's config was validated
// either, so this is the one deliberate exception to "the label is not
// repaired before it is used": lower-casing is part of the accepted shape,
// not a repair of a rejected one — see [toolProfileName]'s doc. The prefix is
// never repaired, for the stronger reason that repairing it is
// indistinguishable from accepting it.
func (d *Daemon) resolveTools(labels []string) toolDecision {
	for _, label := range labels {
		if len(label) < len(legacyToolLabelPrefix) {
			continue
		}
		if strings.EqualFold(label[:len(legacyToolLabelPrefix)], legacyToolLabelPrefix) {
			return toolDecision{Refusal: report.ReplyLegacyToolLabel}
		}
	}

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
	// typed, so "Tools: Research" beside "tools: Build" is ambiguous rather
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
	displayName := label.value
	if !toolProfileName.MatchString(displayName) {
		return toolDecision{Refusal: report.ReplyMalformedToolLabel}
	}
	name := strings.ToLower(displayName)
	profile, ok := d.toolProfiles[name]
	if !ok {
		return toolDecision{Refusal: report.ReplyUnknownToolProfile, Profile: name}
	}

	// Copied, and this is not a formality (P-3). The map is the configuration
	// this process is running on and it lives until the process exits; the
	// harness request these slices end up on is mutated in place on the
	// cold-rerun path. One append into spare capacity downstream would rewrite
	// the operator's profile for every later run in the process.
	//
	// This function does not refuse a profile with no claude: stanza (T-7.1)
	// — it does not know which agent the run will use, so it resolves what
	// the profile actually says (Resolved, both stanzas) and leaves the
	// per-agent narrowing and its refusal to stage 3 (T-7.2, in tick.go's
	// run()), which runs after the stage-1 agent-label ladder has already
	// settled which agent this comment's run addresses. Tools/AllowedTools
	// stay Claude-specific for backward compatibility with every caller that
	// only cares about that stanza; a profile.Claude == nil resolution simply
	// leaves them empty rather than panicking on a nil dereference.
	decision := toolDecision{Profile: name, Resolved: &profile}
	if profile.Claude != nil {
		decision.Tools = append([]string(nil), profile.Claude.Tools...)
		decision.AllowedTools = append([]string(nil), profile.Claude.AllowedTools...)
	}
	return decision
}
