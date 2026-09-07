package report

import (
	"fmt"
	"regexp"
	"strings"
	"time"

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

	// ReplyUnknownCommand answers a slash command that is not in the
	// vocabulary at all, which is usually a typo.
	ReplyUnknownCommand

	// ReplyEmptySpike answers a bare "/spike" with nothing after it (AC-2.20).
	ReplyEmptySpike

	// ReplyAmbiguousAgentLabels answers an issue carrying more than one Coding
	// Agent label. It names neither label.
	ReplyAmbiguousAgentLabels

	// ReplyMalformedAgentLabel answers a Coding Agent label whose value is not
	// the shape of an agent name at all, the empty value included — the
	// expected shape. It carries no field for the label text to travel in.
	ReplyMalformedAgentLabel

	// ReplyUnknownAgent answers a Coding Agent label naming a value that is a
	// well-formed agent name but not one litefactory has a [Runner]
	// implementation registered for. Unlike [ReplyMalformedAgentLabel] it may
	// name what was asked for, because a value that passed the agent-name
	// shape check makes it safe to quote back.
	ReplyUnknownAgent

	// ReplyAgentSwitchRefused answers a comment that asked to continue an
	// existing conversation under a different coding-agent tool than the one
	// that conversation already belongs to. A segment's Agent is fixed at
	// creation (internal/store), and switching it mid-conversation would
	// either hand one tool's session identity to another tool's process or
	// silently start a second, unrelated conversation under the first run's
	// number — so the request is refused rather than guessed at.
	ReplyAgentSwitchRefused

	// ReplyRefusesSync answers a turn whose task branch could not be merged
	// with the base branch cleanly (US-4): [git.Worktree.MergePreview] found
	// conflicting paths and the configured policy says a conflict of this
	// kind stops the run rather than merely disclosing it. Like
	// [ReplyAgentSwitchRefused] it is drawn from the state of the world
	// rather than from the comment's command, and it has its own request
	// shape: [Reply.ConflictPaths] carries the paths that collided, exactly
	// as the merge preview reported them.
	ReplyRefusesSync

	// ReplyMalformedModeLabel answers a malformed or unknown Mode label.
	ReplyMalformedModeLabel
	// ReplyAmbiguousModeLabels answers multiple competing Mode labels.
	ReplyAmbiguousModeLabels
	// ReplyConflictingModeSelection answers a command/label disagreement.
	ReplyConflictingModeSelection

	// ReplyLegacyToolLabel answers an issue or comment still carrying the old
	// lf:tools=<profile> label syntax.
	//
	// It exists so a legacy label is never treated the same as no label at
	// all. Silently ignoring it would resolve the run with no tool profile
	// and no indication anything was wrong — the exact silent permission drop
	// this whole feature exists to prevent, just relocated to the rename. The
	// reply names the new notation so the operator relabels rather than
	// assuming a profile is still active.
	//
	// It carries no field for the label text to travel in (SEC-5): the label is a
	// syntax error regardless of what value follows it, so there is nothing
	// about it worth quoting back.
	ReplyLegacyToolLabel

	// ReplyScheduleInvalid answers a /schedule control whose factory grammar
	// could not be accepted. It is deliberately a factory reply: no agent sees
	// an invalid scheduling request.
	ReplyScheduleInvalid

	// ReplyScheduleConfirmed confirms that Litefactory durably recorded a
	// direct scheduling control before consuming its source comment.
	ReplyScheduleConfirmed

	// ReplyScheduleBlocked tells the owner that a scheduled item will not run
	// because one of its prerequisites failed.
	ReplyScheduleBlocked

	// Proposal replies are factory-owned acknowledgements. They never expose
	// an agent's proposed payload in a signed forge comment.
	ReplyScheduleProposalReady
	ReplyScheduleProposalAccepted
	ReplyScheduleProposalRejected
	ReplyScheduleProposalFailed
)

// String renders the kind for logs and test failures.
func (k ReplyKind) String() string {
	switch k {
	case ReplyUnknownCommand:
		return "unknown_command"
	case ReplyEmptySpike:
		return "empty_spike"
	case ReplyAmbiguousAgentLabels:
		return "ambiguous_agent_labels"
	case ReplyMalformedAgentLabel:
		return "malformed_agent_label"
	case ReplyUnknownAgent:
		return "unknown_agent"
	case ReplyAgentSwitchRefused:
		return "agent_switch_refused"
	case ReplyRefusesSync:
		return "sync_refused"
	case ReplyMalformedModeLabel:
		return "malformed_mode_label"
	case ReplyAmbiguousModeLabels:
		return "ambiguous_mode_labels"
	case ReplyConflictingModeSelection:
		return "conflicting_mode_selection"
	case ReplyLegacyToolLabel:
		return "legacy_tool_label"
	case ReplyScheduleInvalid:
		return "schedule_invalid"
	case ReplyScheduleConfirmed:
		return "schedule_confirmed"
	case ReplyScheduleBlocked:
		return "schedule_blocked"
	case ReplyScheduleProposalReady:
		return "schedule_proposal_ready"
	case ReplyScheduleProposalAccepted:
		return "schedule_proposal_accepted"
	case ReplyScheduleProposalRejected:
		return "schedule_proposal_rejected"
	case ReplyScheduleProposalFailed:
		return "schedule_proposal_failed"
	case ReplyInvalid:
		return "invalid"
	default:
		return "invalid"
	}
}

// valid reports whether k is one of the real kinds.
func (k ReplyKind) valid() bool {
	switch k {
	case ReplyUnknownCommand, ReplyEmptySpike:
		return true
	case ReplyAmbiguousAgentLabels, ReplyMalformedAgentLabel, ReplyUnknownAgent:
		return true
	case ReplyAgentSwitchRefused:
		return true
	case ReplyRefusesSync:
		return true
	case ReplyMalformedModeLabel, ReplyAmbiguousModeLabels, ReplyConflictingModeSelection:
		return true
	case ReplyLegacyToolLabel:
		return true
	case ReplyScheduleInvalid, ReplyScheduleConfirmed, ReplyScheduleBlocked,
		ReplyScheduleProposalReady, ReplyScheduleProposalAccepted,
		ReplyScheduleProposalRejected, ReplyScheduleProposalFailed:
		return true
	case ReplyInvalid:
		return false
	default:
		return false
	}
}

// RefusesMode reports whether k is a mode-selection refusal.
func (k ReplyKind) RefusesMode() bool {
	switch k {
	case ReplyMalformedModeLabel, ReplyAmbiguousModeLabels, ReplyConflictingModeSelection:
		return true
	default:
		return false
	}
}

// RefusesTools reports whether k refuses an obsolete permission label rather
// than answering the command in the comment.
//
// It is exported because internal/turn has to tell the two apart — a refusal is
// triggered by a command that *would* have spawned a run, so every guard that
// asks "does this command draw a reply" gives the wrong answer for one — and
// this package owns what its own kinds mean.
func (k ReplyKind) RefusesTools() bool {
	switch k {
	case ReplyLegacyToolLabel:
		return true
	case ReplyInvalid, ReplyUnknownCommand, ReplyEmptySpike:
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
// [ReplyAgentSwitchRefused] is also agent-related, but it is drawn from
// segment ownership and has its own request shape.
func (k ReplyKind) RefusesAgent() bool {
	switch k {
	case ReplyAmbiguousAgentLabels, ReplyMalformedAgentLabel, ReplyUnknownAgent:
		return true
	default:
		return false
	}
}

// RefusesSwitch reports whether k is [ReplyAgentSwitchRefused]: a reply
// drawn when a session's resolved agent does not own the segment its
// conversation already belongs to (US-3). It has its own request shape, read
// from the store rather than from the issue's labels alone, which is exactly
// why [RefusesAgent]'s own doc excludes it.
func (k ReplyKind) RefusesSwitch() bool {
	return k == ReplyAgentSwitchRefused
}

// RefusesSync reports whether k is [ReplyRefusesSync]: a reply drawn when a
// task branch could not be merged with the base branch cleanly (US-4). It has
// its own request shape — [Reply.ConflictPaths] — the same reason
// [RefusesSwitch] does, and for the same reason [RefusesAgent]'s own doc
// excludes [ReplyAgentSwitchRefused].
func (k ReplyKind) RefusesSync() bool {
	return k == ReplyRefusesSync
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

	// Agent is the coding-agent-tool name an lf:agent= label asked for, or
	// the one a resolved segment is already committed to. It is read by
	// [ReplyUnknownAgent] alone, and
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

	// ConflictPaths is read by [ReplyRefusesSync] alone (US-4): the paths
	// [git.Worktree.MergePreview] reported as conflicting, carried through
	// exactly as syncBase recorded them. It is rendered inside a fenced block
	// the same way [Run.Sync]'s Conflicts list is (S-2), so a path cannot
	// become markup in the comment no matter what it contains — this package
	// does not sanitise or decide which paths are safe to show at all, which
	// is US-5's job; it shows whatever list it is handed.
	ConflictPaths []string

	// Schedule carries the safe, structured facts of a scheduling reply. It is
	// set only for ReplyScheduleInvalid and ReplyScheduleConfirmed.
	Schedule ScheduleReply
}

// ScheduleReply is the factory-owned information rendered for a scheduling
// control. It deliberately carries no payload: an instruction is agent input,
// not text Litefactory should echo into a signed comment.
type ScheduleReply struct {
	ID          int64
	DueAt       time.Time
	Delay       time.Duration
	After       []int64
	Outstanding int
	ProposalID  int64
	Count       int
}

// agentNamePattern is the shape of a coding-agent-tool name — deliberately
// restricted to the config-key shape. It is checked again here, at the one
// place a value that reached this package from a
// forge label is echoed into a comment litefactory signs its own name to
// (SEC-5), rather than trusted from whatever validated it upstream.
var agentNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,39}$`)

// agentSpan renders a coding-agent-tool name for quoting back, the same way
// the same way as other untrusted names: no salvaged prefix, nothing rendered
// at all for anything that fails the pattern.
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

	case ReplyUnknownCommand:
		if named {
			fmt.Fprintf(&b, "I do not recognise %s.", span)
		} else {
			b.WriteString("I do not recognise that command.")
		}
		b.WriteString(" ")
		b.WriteString(onlyCommandSentence)

	case ReplyLegacyToolLabel:
		b.WriteString("This issue carries the old `lf:tools=` label, which litefactory no longer reads. ")
		b.WriteString(toolsRefusalSentence)
		b.WriteString(sectionGap)
		b.WriteString("Remove every `lf:tools=` label and comment again. Litefactory now selects " +
			"`research`, `plan`, `implement`, or `validate` permissions automatically from the " +
			"resolved run mode, so no permission label is needed. The label text is not repeated " +
			"here on purpose.")

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

	case ReplyMalformedModeLabel:
		b.WriteString("The `Mode: …` label on this issue is malformed or unknown. Nothing was run.")
		b.WriteString(sectionGap)
		b.WriteString("Use exactly `Mode: Research`, `Mode: Plan`, `Mode: Implement`, `Mode: Validate`, or `Mode: Spike`.")

	case ReplyAmbiguousModeLabels:
		b.WriteString("This issue carries more than one `Mode: …` label, so no mode was chosen and nothing was run.")
		b.WriteString(sectionGap)
		b.WriteString("Leave exactly one canonical Mode label and comment again.")

	case ReplyConflictingModeSelection:
		b.WriteString("The slash command and the issue's `Mode: …` label select different modes, so nothing was run.")
		b.WriteString(sectionGap)
		b.WriteString("Make the command and label agree, or remove one of them, and comment again.")

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

	case ReplyRefusesSync:
		b.WriteString("This task's branch could not be merged with the base branch cleanly, so litefactory " +
			"stopped before starting the agent. ")
		b.WriteString(syncRefusalSentence)
		if len(r.ConflictPaths) > 0 {
			b.WriteString(sectionGap)
			fmt.Fprintf(&b, "Conflicting path(s) (%d):\n", len(r.ConflictPaths))
			b.WriteString(conflictsBlock(r.ConflictPaths))
		}
		b.WriteString(sectionGap)
		b.WriteString("Resolve the conflict on the base branch or on this task's branch, then comment again.")

	case ReplyScheduleInvalid:
		b.WriteString("I could not schedule that request. Litefactory accepts ")
		b.WriteString("`/schedule -by HH:MM:SS <payload>`, `/schedule -to MM/DD/YYYY HH:MM:SS [ZONE] <payload>`, ")
		b.WriteString("or `/schedule --after <id[,id...]> -by HH:MM:SS <payload>`.")

	case ReplyScheduleConfirmed:
		if r.Schedule.ID <= 0 {
			return ""
		}
		fmt.Fprintf(&b, "Scheduled work `%d`", r.Schedule.ID)
		if len(r.Schedule.After) > 0 {
			b.WriteString(" after schedules ")
			for i, id := range r.Schedule.After {
				if i > 0 {
					b.WriteString(", ")
				}
				fmt.Fprintf(&b, "`%d`", id)
			}
			fmt.Fprintf(&b, " succeed, then waits %s", r.Schedule.Delay)
		} else {
			fmt.Fprintf(&b, " for %s", r.Schedule.DueAt.UTC().Format(time.RFC3339))
		}
		fmt.Fprintf(&b, ". `scheduled=%d`.", r.Schedule.Outstanding)

	case ReplyScheduleBlocked:
		if r.Schedule.ID <= 0 {
			return ""
		}
		fmt.Fprintf(&b, "Scheduled work `%d` will not run because one of its prerequisites failed.", r.Schedule.ID)

	case ReplyScheduleProposalReady:
		if r.Schedule.ProposalID <= 0 || r.Schedule.Count <= 0 {
			return ""
		}
		fmt.Fprintf(&b, "Schedule proposal `%d` contains %d item(s). Use `/schedule --accept %d` to arm it, or `/schedule --reject %d` to discard it.", r.Schedule.ProposalID, r.Schedule.Count, r.Schedule.ProposalID, r.Schedule.ProposalID)

	case ReplyScheduleProposalAccepted:
		if r.Schedule.ProposalID <= 0 {
			return ""
		}
		fmt.Fprintf(&b, "Accepted schedule proposal `%d`; %d item(s) are now scheduled. `scheduled=%d`.", r.Schedule.ProposalID, r.Schedule.Count, r.Schedule.Outstanding)

	case ReplyScheduleProposalRejected:
		if r.Schedule.ProposalID <= 0 {
			return ""
		}
		fmt.Fprintf(&b, "Rejected schedule proposal `%d`.", r.Schedule.ProposalID)

	case ReplyScheduleProposalFailed:
		b.WriteString("I could not produce a valid schedule proposal. No work was scheduled.")

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
// will act on. It is a single constant so the replies cannot drift into
// different accounts of the active command vocabulary.
const onlyCommandSentence = "Litefactory accepts `/research`, `/plan`, `/implement`, `/validate`, `/spike`, and `/schedule`."

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

// syncRefusalSentence is the one thing every base-branch-sync refusal has to
// say, for the same reason [agentRefusalSentence] exists: litefactory refuses
// to run an agent against a task branch it could not merge the base branch
// into cleanly, rather than guessing at a resolution or running against a
// stale branch.
const syncRefusalSentence = "Nothing was run: litefactory refuses a run it cannot merge the base branch " +
	"into cleanly rather than guessing at a resolution or running against a stale branch."
