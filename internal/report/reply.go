package report

import (
	"fmt"
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
	case ReplyInvalid:
		return "invalid"
	default:
		return "invalid"
	}
}

// valid reports whether k is one of the three real kinds.
func (k ReplyKind) valid() bool {
	switch k {
	case ReplyUnsupportedCommand, ReplyUnknownCommand, ReplyEmptySpike:
		return true
	case ReplyInvalid:
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
