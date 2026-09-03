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

	// ReplyUnknownToolProfile answers a "Tools: <Profile>" label naming a
	// profile the operator has not configured — including the case where no
	// profiles are configured at all, which is the same answer from the
	// thread's point of view: the profile you asked for is not there.
	//
	// It is the one refusal that names what it was asked for, in [Reply.Profile],
	// and it may do so precisely because the name passed the profile-name
	// pattern before it got here and is checked against it again below.
	ReplyUnknownToolProfile

	// ReplyMalformedToolLabel answers a "Tools: <Profile>" label whose value is
	// not the shape of a profile name at all, the empty value included.
	//
	// It carries no profile and quotes nothing back. The label text failed the
	// one check that would have made it safe to print, so the reply describes
	// the rule instead of the input — and [Reply] gives it no field to put the
	// input in, which is what makes that a property of the type rather than a
	// habit of whoever edits this next.
	ReplyMalformedToolLabel

	// ReplyAmbiguousToolLabels answers an issue carrying more than one
	// "Tools: <Profile>" label.
	//
	// It names neither of them. Naming both would echo exactly the text
	// [ReplyMalformedToolLabel] refuses to echo, with the added indignity that
	// one of the two may be a well-formed profile name and the other an attempt
	// to smuggle a sentence into a comment litefactory signs.
	ReplyAmbiguousToolLabels

	// ReplyAmbiguousAgentLabels answers an issue carrying more than one
	// Coding Agent label, the agent-selection counterpart to
	// [ReplyAmbiguousToolLabels] and shaped the same way for the same reason:
	// it names neither label.
	ReplyAmbiguousAgentLabels

	// ReplyMalformedAgentLabel answers a Coding Agent label whose value is not
	// the shape of an agent name at all, the empty value included — the
	// agent-selection counterpart to [ReplyMalformedToolLabel]. It carries no
	// field for the label text to travel in, for the same reason that one
	// does not.
	ReplyMalformedAgentLabel

	// ReplyUnknownAgent answers a Coding Agent label naming a value that is a
	// well-formed agent name but not one litefactory has a [Runner]
	// implementation registered for. Unlike [ReplyMalformedAgentLabel] it may
	// name what was asked for, because a value that passed the agent-name
	// shape check is safe to quote back the same way a tool profile name is
	// (see [ReplyUnknownToolProfile]).
	ReplyUnknownAgent

	// ReplyAgentSwitchRefused answers a comment that asked to continue an
	// existing conversation under a different coding-agent tool than the one
	// that conversation already belongs to. A segment's Agent is fixed at
	// creation (internal/store), and switching it mid-conversation would
	// either hand one tool's session identity to another tool's process or
	// silently start a second, unrelated conversation under the first run's
	// number — so the request is refused rather than guessed at.
	ReplyAgentSwitchRefused

	// ReplyProfileNotAvailableForAgent answers a resolved "Tools: <Profile>"
	// label that carries no stanza for the agent this run would otherwise use.
	// A profile with a stanza for one tool and none for another is not treated
	// as "fall back to that tool's own default surface" — the whole point of
	// a profile is the operator's explicit boundary, and a run this package
	// cannot resolve a surface for is refused rather than widened.
	ReplyProfileNotAvailableForAgent

	// ReplyLegacyToolLabel answers an issue or comment still carrying the old
	// lf:tools=<profile> label syntax, which this feature shipped with before
	// the label was renamed to "Tools: <Profile>" (US-2).
	//
	// It exists so a legacy label is never treated the same as no label at
	// all. Silently ignoring it would resolve the run with no tool profile
	// and no indication anything was wrong — the exact silent permission drop
	// this whole feature exists to prevent, just relocated to the rename. The
	// reply names the new notation so the operator relabels rather than
	// assuming a profile is still active.
	//
	// Like [ReplyMalformedToolLabel] and [ReplyAmbiguousToolLabels] it carries
	// no field for the label text to travel in (S-9): the label itself is a
	// syntax error regardless of what value follows it, so there is nothing
	// about it worth quoting back.
	ReplyLegacyToolLabel
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
	case ReplyAmbiguousAgentLabels:
		return "ambiguous_agent_labels"
	case ReplyMalformedAgentLabel:
		return "malformed_agent_label"
	case ReplyUnknownAgent:
		return "unknown_agent"
	case ReplyAgentSwitchRefused:
		return "agent_switch_refused"
	case ReplyProfileNotAvailableForAgent:
		return "profile_not_available_for_agent"
	case ReplyLegacyToolLabel:
		return "legacy_tool_label"
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
	case ReplyAmbiguousAgentLabels, ReplyMalformedAgentLabel, ReplyUnknownAgent:
		return true
	case ReplyAgentSwitchRefused, ReplyProfileNotAvailableForAgent:
		return true
	case ReplyLegacyToolLabel:
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
	case ReplyUnknownToolProfile, ReplyMalformedToolLabel, ReplyAmbiguousToolLabels, ReplyLegacyToolLabel:
		return true
	case ReplyInvalid, ReplyUnsupportedCommand, ReplyUnknownCommand, ReplyEmptySpike:
		return false
	default:
		return false
	}
}

// RefusesAgent reports whether k is one of the three agent-selection
// refusals a resolved lf:agent= label ladder draws: a reply drawn by the
// labels on the issue rather than by the command in the comment.
//
// It mirrors [ReplyKind.RefusesTools] exactly and exists for the same
// reason: internal/turn has to tell "this reply answers a label" from "this
// reply answers a command" before it can decide what [ReplyRequest] shape is
// valid, and this package owns what its own kinds mean.
//
// [ReplyAgentSwitchRefused] and [ReplyProfileNotAvailableForAgent] are also
// agent-related, but they are drawn by later stages of the four-stage
// refusal precedence (segment-ownership and per-agent stanza narrowing) with
// their own request shapes, so they are deliberately excluded here — this
// method answers only for the label ladder this story implements.
func (k ReplyKind) RefusesAgent() bool {
	switch k {
	case ReplyAmbiguousAgentLabels, ReplyMalformedAgentLabel, ReplyUnknownAgent:
		return true
	default:
		return false
	}
}

// RefusesProfileForAgent reports whether k is the stage-3 refusal T-7.2
// draws: a resolved lf:tools= profile with no stanza for the agent a run
// would otherwise use. It is evaluated after the label ladders
// [ReplyKind.RefusesTools] and [ReplyKind.RefusesAgent] answer for, which is
// exactly why [RefusesAgent]'s own doc excludes it — this is its own request
// shape, checked on its own, so a caller can tell all three refusal families
// apart.
func (k ReplyKind) RefusesProfileForAgent() bool {
	return k == ReplyProfileNotAvailableForAgent
}

// RefusesSwitch reports whether k is [ReplyAgentSwitchRefused]: a reply
// drawn when a session's resolved agent does not own the segment its
// conversation already belongs to (US-3). It has its own request shape, read
// from the store rather than from the issue's labels alone, which is exactly
// why [RefusesAgent]'s own doc excludes it the same way it excludes
// [RefusesProfileForAgent] — this method is what lets a caller tell all four
// refusal families apart.
func (k ReplyKind) RefusesSwitch() bool {
	return k == ReplyAgentSwitchRefused
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

	// Profile is the tool profile name a "Tools: <Profile>" label asked for,
	// and it is read by [ReplyUnknownToolProfile] and
	// [ReplyProfileNotAvailableForAgent].
	//
	// It reaches this package from a label anybody with triage permission can
	// apply, so it is checked against [profileNamePattern] here before it is
	// rendered — see [profileSpan]. Empty, or anything that fails that check,
	// produces a reply that describes the problem without quoting the input,
	// which is the same shape [commandSpan] already imposes on command tokens.
	Profile string

	// Agent is the coding-agent-tool name an lf:agent= label asked for, or
	// the one a resolved segment is already committed to. It is read by
	// [ReplyUnknownAgent] and [ReplyProfileNotAvailableForAgent] alone, and
	// like Profile it is checked against [agentNamePattern] here before it
	// is rendered — see [agentSpan].
	Agent string

	// OwningAgent and RequestedAgent are read by [ReplyAgentSwitchRefused]
	// alone (US-3): OwningAgent is the coding-agent tool a session's segment
	// already belongs to, and RequestedAgent is the one this run's labels
	// resolved to instead. Both are checked against [agentNamePattern] here
	// before they are rendered, the same as [Agent].
	OwningAgent    string
	RequestedAgent string
}

// profileNamePattern is the shape of an operator-authored tool profile *key*,
// as stored in `tool_profiles` — lower-case, unlike the upper-case-first shape
// a "Tools: <Profile>" label's displayed value must have (internal/daemon's
// toolProfileName). internal/config validates the operator's profile keys
// against this pattern at startup, and internal/daemon lower-cases a label's
// value before using it as a lookup key, so by the time a name arrives here it
// has already taken this shape twice over. It is checked a third time all the
// same, and the pattern is written out here rather than imported, for the
// reason given at [commandSpan]: this package is the one place where text
// that came from a third party is echoed into a comment litefactory signs its
// own name to, and the restriction is re-applied here rather than assumed. A
// rule that is only enforced by whoever remembers to enforce it upstream is
// not a rule.
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

// agentNamePattern is the shape of a coding-agent-tool name — deliberately
// the same shape as [profileNamePattern], for the same reason: it is checked
// again here, at the one place a value that reached this package from a
// forge label is echoed into a comment litefactory signs its own name to
// (SEC-5), rather than trusted from whatever validated it upstream.
var agentNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,39}$`)

// agentSpan renders a coding-agent-tool name for quoting back, the same way
// [profileSpan] does for a tool profile name: no salvaged prefix, nothing
// rendered at all for anything that fails the pattern.
func agentSpan(name string) (string, bool) {
	if !agentNamePattern.MatchString(name) {
		return "", false
	}
	return "`" + name + "`", true
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
			"`Tools: ` label off the issue, and comment again.")

	case ReplyMalformedToolLabel:
		b.WriteString("The `Tools: ` label on this issue does not name a usable tool profile. ")
		b.WriteString(toolsRefusalSentence)
		b.WriteString(sectionGap)
		b.WriteString("A tool profile label looks like `Tools: Research` — an upper-case first " +
			"letter, then letters, digits, `-` and `_`, up to 40 characters, naming one of the " +
			"profiles in litefactory's config. The label text is not repeated here on purpose.")

	case ReplyAmbiguousToolLabels:
		b.WriteString("This issue carries more than one `Tools: ` label, so there is no single " +
			"answer to which tools this run should have had. ")
		b.WriteString(toolsRefusalSentence)
		b.WriteString(sectionGap)
		b.WriteString("Neither label was chosen and they were not combined. Leave exactly one " +
			"`Tools: ` label on the issue and comment again.")

	case ReplyLegacyToolLabel:
		b.WriteString("This issue carries the old `lf:tools=` label, which litefactory no longer reads. ")
		b.WriteString(toolsRefusalSentence)
		b.WriteString(sectionGap)
		b.WriteString("Replace it with a `Tools: <Profile>` label — for example `Tools: Research` — " +
			"and comment again. The label text is not repeated here on purpose.")

	case ReplyAmbiguousAgentLabels:
		b.WriteString("This issue carries more than one `Coding Agent: …` label, so there is no single " +
			"answer to which coding-agent tool this run should have used. ")
		b.WriteString(agentRefusalSentence)
		b.WriteString(sectionGap)
		b.WriteString("Neither label was chosen and they were not combined. Leave exactly one " +
			"`Coding Agent: Claude` or `Coding Agent: Codex` label on the issue and comment again.")

	case ReplyMalformedAgentLabel:
		b.WriteString("The `Coding Agent: …` label on this issue does not name a usable coding-agent tool. ")
		b.WriteString(agentRefusalSentence)
		b.WriteString(sectionGap)
		b.WriteString("Use exactly `Coding Agent: Claude` or `Coding Agent: Codex`. The label " +
			"text is not repeated here on purpose.")

	case ReplyUnknownAgent:
		if name, named := agentSpan(r.Agent); named {
			fmt.Fprintf(&b, "This issue asks for the coding-agent tool %s, which litefactory has no "+
				"implementation registered for.", name)
		} else {
			b.WriteString("This issue asks for a coding-agent tool litefactory has no implementation registered for.")
		}
		b.WriteString(" ")
		b.WriteString(agentRefusalSentence)
		b.WriteString(sectionGap)
		b.WriteString("Take the `Coding Agent: …` label off the issue, or change it to a tool litefactory " +
			"has running, and comment again.")

	case ReplyAgentSwitchRefused:
		b.WriteString("This task's conversation already belongs to a different coding-agent tool than " +
			"this comment asked for. ")
		b.WriteString(agentRefusalSentence)
		b.WriteString(sectionGap)
		b.WriteString("A conversation stays with the tool it started with — litefactory does not hand " +
			"one tool's session to another. Comment again without asking for a different agent, " +
			"or start a new task for the one you want.")

	case ReplyProfileNotAvailableForAgent:
		profile, profileNamed := profileSpan(r.Profile)
		name, agentNamed := agentSpan(r.Agent)
		switch {
		case profileNamed && agentNamed:
			fmt.Fprintf(&b, "The tool profile %s does not have a configuration for %s, which is the "+
				"coding-agent tool this run would otherwise have used.", profile, name)
		case profileNamed:
			fmt.Fprintf(&b, "The tool profile %s does not have a configuration for this run's "+
				"coding-agent tool.", profile)
		default:
			b.WriteString("The resolved tool profile does not have a configuration for this run's coding-agent tool.")
		}
		b.WriteString(" ")
		b.WriteString(toolsRefusalSentence)
		b.WriteString(sectionGap)
		b.WriteString("Add a stanza for that tool to the profile, or take the `Tools: ` label off " +
			"the issue, and comment again.")

	case ReplyInvalid:
		// Unreachable: RenderReply rejects an invalid kind before calling this.
		return ""

	default:
		return ""
	}

	// Every reply above ends by asking for another comment, so every reply is a
	// comment somebody is about to answer — which makes this the last line of
	// all of them rather than a decoration on some (AC-S0.6).
	b.WriteString(sectionGap)
	b.WriteString(ReplyPlainlyNotice)
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

// agentRefusalSentence is the one thing every agent-selection refusal has to
// say, for the same reason [toolsRefusalSentence] exists: litefactory refuses
// a run it cannot resolve which coding-agent tool to use for, rather than
// silently falling back to a different one than was asked for.
const agentRefusalSentence = "Nothing was run: litefactory refuses a run it cannot " +
	"resolve the coding-agent tool for rather than falling back to a different one."
