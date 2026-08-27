package report

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cloudvane/litefactory/internal/naming"
)

// ReplyKind is why litefactory is answering a comment without running
// anything.
//
// It is this package's own enum rather than internal/turn's CommandKind, and
// deliberately so: internal/turn depends on this package to render its replies,
// so the dependency cannot point the other way. Mapping one to the other is
// three lines at the call site and keeps the wording of the reply — which is
// user-visible output — owned here with the rest of it.
type ReplyKind int

const (
	// ReplyInvalid is the zero value and renders nothing. A reply is always
	// sent for a reason, and a reply with no reason is a caller bug rather
	// than a message worth inventing.
	ReplyInvalid ReplyKind = iota

	// ReplyUnsupportedCommand answers a real command from a later milestone:
	// "/plan", "/implement". The distinction from an unknown command is the
	// whole point — "not yet" and "never heard of it" call for different
	// things from the person who typed it (AC-2.18).
	ReplyUnsupportedCommand

	// ReplyUnknownCommand answers a slash command that is not in the
	// vocabulary at all, which is usually a typo.
	ReplyUnknownCommand

	// ReplyEmptySpike answers a bare "/spike" with nothing after it (AC-2.20).
	ReplyEmptySpike

	// ReplyUnknownToolProfile answers an lf:tools= label naming a profile the
	// operator has not configured — including the case where no profiles are
	// configured at all, which is the same answer from the thread's point of
	// view: the profile you asked for is not there.
	//
	// It is the one refusal that names what it was asked for, in [Reply.Profile],
	// and it may do so precisely because the name passed the profile-name
	// pattern before it got here and is checked against it again below.
	ReplyUnknownToolProfile

	// ReplyMalformedToolLabel answers an lf:tools= label whose value is not the
	// shape of a profile name at all, the empty value included.
	//
	// It carries no profile and quotes nothing back. The label text failed the
	// one check that would have made it safe to print, so the reply describes
	// the rule instead of the input — and [Reply] gives it no field to put the
	// input in, which is what makes that a property of the type rather than a
	// habit of whoever edits this next.
	ReplyMalformedToolLabel

	// ReplyAmbiguousToolLabels answers an issue carrying more than one lf:tools=
	// label.
	//
	// It names neither of them. Naming both would echo exactly the text
	// [ReplyMalformedToolLabel] refuses to echo, with the added indignity that
	// one of the two may be a well-formed profile name and the other an attempt
	// to smuggle a sentence into a comment litefactory signs.
	ReplyAmbiguousToolLabels
)

// String renders the kind for logs and test failures.
func (k ReplyKind) String() string {
	switch k {
	case ReplyUnsupportedCommand:
		return "unsupported_command"
	case ReplyUnknownCommand:
		return "unknown_command"
	case ReplyEmptySpike:
		return "empty_spike"
	case ReplyUnknownToolProfile:
		return "unknown_tool_profile"
	case ReplyMalformedToolLabel:
		return "malformed_tool_label"
	case ReplyAmbiguousToolLabels:
		return "ambiguous_tool_labels"
	case ReplyInvalid:
		return "invalid"
	default:
		return "invalid"
	}
}

// valid reports whether k is one of the real kinds.
func (k ReplyKind) valid() bool {
	switch k {
	case ReplyUnsupportedCommand, ReplyUnknownCommand, ReplyEmptySpike:
		return true
	case ReplyUnknownToolProfile, ReplyMalformedToolLabel, ReplyAmbiguousToolLabels:
		return true
	case ReplyInvalid:
		return false
	default:
		return false
	}
}

// RefusesTools reports whether k is one of the three tool-permission refusals:
// a reply drawn by the labels on the issue rather than by the command in the
// comment.
//
// It is exported because internal/turn has to tell the two apart — a refusal is
// triggered by a command that *would* have spawned a run, so every guard that
// asks "does this command draw a reply" gives the wrong answer for one — and
// this package owns what its own kinds mean.
func (k ReplyKind) RefusesTools() bool {
	switch k {
	case ReplyUnknownToolProfile, ReplyMalformedToolLabel, ReplyAmbiguousToolLabels:
		return true
	case ReplyInvalid, ReplyUnsupportedCommand, ReplyUnknownCommand, ReplyEmptySpike:
		return false
	default:
		return false
	}
}

// Reply is a run-less answer to a comment: the reply forms from LF-2, which
// carry the lf:reply marker and no run ordinal because no run happened.
type Reply struct {
	// Ref is the task the comment was on. It appears in the marker, so it must
	// validate.
	Ref naming.Ref

	// Kind is why litefactory is replying instead of running.
	Kind ReplyKind

	// Command is the command token as parsed, including its leading slash —
	// "/plan", "/frobnicate". It is quoted back so the reply names what it is
	// answering. Empty is allowed and produces slightly vaguer wording.
	Command string

	// Profile is the tool profile name an lf:tools= label asked for, and it is
	// read by [ReplyUnknownToolProfile] alone.
	//
	// It reaches this package from a label anybody with triage permission can
	// apply, so it is checked against [profileNamePattern] here before it is
	// rendered — see [profileSpan]. Empty, or anything that fails that check,
	// produces a reply that describes the problem without quoting the input,
	// which is the same shape [commandSpan] already imposes on command tokens.
	Profile string
}

// profileNamePattern is the shape of an operator-authored tool profile name.
//
// internal/config validates the operator's profile keys against this pattern at
// startup and internal/daemon checks a label's value against it before using it
// as a lookup key, so by the time a name arrives here it has been checked twice.
// It is checked a third time all the same, and the pattern is written out here
// rather than imported, for the reason given at [commandSpan]: this package is
// the one place where text that came from a third party is echoed into a comment
// litefactory signs its own name to, and the restriction is re-applied here
// rather than assumed. A rule that is only enforced by whoever remembers to
// enforce it upstream is not a rule.
var profileNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,39}$`)

// profileSpan renders a tool profile name for quoting back.
//
// Unlike [commandSpan] it does not salvage a prefix: a profile name is a lookup
// key that either matched the operator's configuration or did not, and half of
// one names nothing. Anything that fails the pattern produces no span at all and
// the caller falls back to wording that quotes nothing.
func profileSpan(profile string) (string, bool) {
	if !profileNamePattern.MatchString(profile) {
		return "", false
	}
	return "`" + profile + "`", true
}

// maxCommandLen bounds how much of a command token is quoted back.
const maxCommandLen = 32

// commandSpan renders the command token for quoting back to its author.
//
// The token reaches this package from a third party's comment. internal/turn
// already restricts it to a slash followed by a plain word, but this is the one
// place where attacker-controlled text is echoed into a comment litefactory
// signs its own name to, so the restriction is re-applied here rather than
// assumed: a reply cannot be made to carry markup, a second lf: marker, or a
// mention that pings somebody.
//
// The token stops at the first character outside that set rather than dropping
// it and carrying on. Skipping characters would let "/ev`il <!-- lf:status -->"
// be reassembled into something that looks like a real command; stopping means
// unexpected input can only ever produce a shorter, plainer token than was
// typed.
func commandSpan(command string) (string, bool) {
	var b strings.Builder
	for i, r := range command {
		alphanumeric := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		interiorPunctuation := i > 0 && (r == '-' || r == '_')
		leadingSlash := i == 0 && r == '/'
		if !alphanumeric && !interiorPunctuation && !leadingSlash {
			break
		}
		if b.Len() >= maxCommandLen {
			break
		}
		b.WriteRune(r)
	}
	token := b.String()
	if token == "" || token == "/" {
		return "", false
	}
	return "`" + token + "`", true
}

// body renders the reply. It is small on purpose: a reply is an answer to
// something litefactory will not do, and the useful part is the sentence that
// says what it will do instead.
func (r Reply) body() string {
	var b strings.Builder
	b.WriteString(ReplyMarker(r.Ref))
	b.WriteString(sectionGap)

	span, named := commandSpan(r.Command)
	switch r.Kind {
	case ReplyEmptySpike:
		b.WriteString("`/spike` needs something to work on. Say what you want tried, on the same line or the ones after it:")
		b.WriteString(sectionGap)
		b.WriteString("```text\n/spike see whether the flaky checkout test is a race in the fixture\n```")

	case ReplyUnsupportedCommand:
		if named {
			fmt.Fprintf(&b, "%s is a real command, but litefactory does not implement it yet.", span)
		} else {
			b.WriteString("That command is a real one, but litefactory does not implement it yet.")
		}
		b.WriteString(" ")
		b.WriteString(onlyCommandSentence)

	case ReplyUnknownCommand:
		if named {
			fmt.Fprintf(&b, "I do not recognise %s.", span)
		} else {
			b.WriteString("I do not recognise that command.")
		}
		b.WriteString(" ")
		b.WriteString(onlyCommandSentence)

	case ReplyUnknownToolProfile:
		if profile, named := profileSpan(r.Profile); named {
			fmt.Fprintf(&b, "This issue asks for the tool profile %s, which litefactory has no "+
				"configuration for.", profile)
		} else {
			b.WriteString("This issue asks for a tool profile litefactory has no configuration for.")
		}
		b.WriteString(" ")
		b.WriteString(toolsRefusalSentence)
		b.WriteString(sectionGap)
		b.WriteString("Add the profile to `tool_profiles` in litefactory's config, or take the " +
			"`lf:tools=` label off the issue, and comment again.")

	case ReplyMalformedToolLabel:
		b.WriteString("The `lf:tools=` label on this issue does not name a usable tool profile. ")
		b.WriteString(toolsRefusalSentence)
		b.WriteString(sectionGap)
		b.WriteString("A profile name is lowercase letters and digits, `-` and `_`, up to 40 " +
			"characters, and it has to be one of the profiles in litefactory's config. The label " +
			"text is not repeated here on purpose.")

	case ReplyAmbiguousToolLabels:
		b.WriteString("This issue carries more than one `lf:tools=` label, so there is no single " +
			"answer to which tools this run should have had. ")
		b.WriteString(toolsRefusalSentence)
		b.WriteString(sectionGap)
		b.WriteString("Neither label was chosen and they were not combined. Leave exactly one " +
			"`lf:tools=` label on the issue and comment again.")

	case ReplyInvalid:
		// Unreachable: RenderReply rejects an invalid kind before calling this.
		return ""

	default:
		return ""
	}

	b.WriteString("\n")
	return b.String()
}

// onlyCommandSentence is the one thing every reply has to say: what litefactory
// will act on. It is a single constant so the three replies cannot drift into
// three different accounts of the same vocabulary.
const onlyCommandSentence = "The only command it acts on at the moment is `/spike <what you want tried>`."

// toolsRefusalSentence is the one thing every tool-permission refusal has to
// say, for the same reason [onlyCommandSentence] exists: three refusals must not
// drift into three different accounts of the same rule.
//
// The rule is C9, in the words of the person reading the thread. A label is a
// question about permission, and the only safe answer to one litefactory cannot
// read is no — running anyway, on whatever tools the run would have had without
// the label, would answer a narrowing request by ignoring it.
const toolsRefusalSentence = "Nothing was run: litefactory refuses a run it cannot " +
	"resolve the tools for rather than falling back to its default ones."
