package turn

import (
	"slices"
	"strings"
	"unicode"
)

// SpikeCommand is the only command litefactory acts on at M0.
const SpikeCommand = "/spike"

// knownCommands are the commands later milestones will implement. They are
// listed so that typing a real command early gets "not supported yet" rather
// than "unknown command" — the difference between "wait for the next release"
// and "you typed it wrong".
//
// The list is sorted; keep it that way.
var knownCommands = []string{
	"/approve", "/implement", "/include", "/plan",
	"/research", "/reset", "/retry", "/status",
}

// CommandKind is what a comment body turned out to be asking for.
//
// Every kind advances the watermark once it has been handled. Consuming the
// comment is what stops the daemon from reprocessing it on the next tick, and
// that has to happen whether the comment started a run, drew a reply or was
// ignored.
type CommandKind int

const (
	// KindNone is a body with nothing in it — empty or whitespace only. It is
	// recorded and ignored: no reply, no run.
	//
	// Free text used to land here too. It no longer does: an owner who writes
	// a sentence is asking for something, and [KindFreeText] acts on it.
	KindNone CommandKind = iota

	// KindSpike is an actionable "/spike <text>".
	KindSpike

	// KindFreeText is an owner comment that opens with something other than a
	// command token. It spawns a run whose mode the agent decides for itself.
	//
	// litefactory does not classify the request. The worktree the run executes
	// in holds the target repository's own CLAUDE.md, and if that file defines
	// modes then it is the authority on which one this comment calls for. The
	// prompt says so; the agent reads it and routes itself. Doing the same
	// reasoning here would cost a second model call to derive a label the run
	// derives anyway.
	KindFreeText

	// KindSpikeEmpty is "/spike" with no text after it. It draws a reply
	// asking for text and spawns nothing (AC-2.20).
	KindSpikeEmpty

	// KindUnsupported is a command from a later milestone. It draws a short
	// "not supported" reply and spawns nothing (AC-2.18).
	KindUnsupported

	// KindUnknown is a slash command that is not in the vocabulary at all. It
	// is handled exactly like KindUnsupported; the two are kept apart only so
	// the reply can say something more useful.
	KindUnknown
)

// String renders the kind for logs and test failures.
func (k CommandKind) String() string {
	switch k {
	case KindNone:
		return "none"
	case KindSpike:
		return "spike"
	case KindFreeText:
		return "free_text"
	case KindSpikeEmpty:
		return "spike_empty"
	case KindUnsupported:
		return "unsupported"
	case KindUnknown:
		return "unknown"
	default:
		return "invalid"
	}
}

// Command is the result of parsing one comment body: what the comment asked
// for, and the argument if it asked for a spike.
//
// It is a value, not a tuple of booleans, because two callers consume it — the
// daemon, deciding whether to start a turn, and the reply path, deciding what
// to post — and they must not be able to disagree about what the comment said.
type Command struct {
	// Kind is what the body asked for.
	Kind CommandKind

	// Name is the command token in canonical lowercase form, including the
	// leading slash, for example "/spike" or "/plan". It is empty for
	// [KindNone] and [KindFreeText], neither of which named a command.
	//
	// This is the lowercased token rather than what the author typed, and its
	// character set is restricted by the parser, so a reply may quote it
	// without smuggling markup out of a comment body and back into one.
	Name string

	// Text is the instruction the comment carries, with surrounding whitespace
	// trimmed: the argument to /spike for [KindSpike], and the whole body for
	// [KindFreeText]. Newlines are kept, so a multi-line request keeps its
	// shape. Non-empty exactly when the command spawns a run.
	Text string
}

// Spawns reports whether this command starts a run.
func (c Command) Spawns() bool { return c.Kind == KindSpike || c.Kind == KindFreeText }

// NeedsReply reports whether this command should draw a comment back. The
// kinds that act — a spike and free text — are answered by the run report, and
// an empty body is answered with silence.
func (c Command) NeedsReply() bool {
	switch c.Kind {
	case KindSpikeEmpty, KindUnsupported, KindUnknown:
		return true
	case KindNone, KindSpike, KindFreeText:
		return false
	default:
		return false
	}
}

// ParseCommand classifies the body of a forge comment.
//
// Only the first non-whitespace token is considered (AC-2.18). A "/spike"
// halfway down a comment is prose, not an instruction — a comment discussing
// what to spike must not become one. Leading whitespace and blank lines before
// the token are skipped.
//
// Matching is case-insensitive: phone keyboards capitalise the first word of a
// message, and "/Spike" is a typo the tool should absorb rather than answer
// with "unknown command". There is no command whose meaning depends on case,
// so nothing is lost.
//
// A token only counts as a command if it is a slash followed by a plain word
// — letters, digits, underscores and hyphens. That keeps a comment opening
// with a path such as "/usr/bin/security is on the PATH" as free text, and it
// bounds what [Command.Name] can contain.
//
// A body that names no command at all is [KindFreeText] and carries the whole
// body as its instruction. Only a body with nothing in it is [KindNone].
//
// ParseCommand never fails. Every body classifies as something, because every
// comment the daemon reads has to be disposed of one way or another.
func ParseCommand(body string) Command {
	token, rest := firstToken(body)
	if !isCommandToken(token) {
		// firstToken returns an empty token only for a body that is entirely
		// whitespace, which is the one thing there is nothing to act on in.
		if token == "" {
			return Command{Kind: KindNone}
		}
		return Command{Kind: KindFreeText, Text: strings.TrimSpace(body)}
	}

	name := strings.ToLower(token)
	switch {
	case name == SpikeCommand:
		text := strings.TrimSpace(rest)
		if text == "" {
			return Command{Kind: KindSpikeEmpty, Name: name}
		}
		return Command{Kind: KindSpike, Name: name, Text: text}
	case slices.Contains(knownCommands, name):
		return Command{Kind: KindUnsupported, Name: name}
	default:
		return Command{Kind: KindUnknown, Name: name}
	}
}

// firstToken splits off the first whitespace-delimited token of body and
// returns it with everything after it. Both are empty for a body that is
// entirely whitespace.
func firstToken(body string) (token, rest string) {
	start := strings.IndexFunc(body, func(r rune) bool { return !unicode.IsSpace(r) })
	if start < 0 {
		return "", ""
	}
	body = body[start:]

	end := strings.IndexFunc(body, unicode.IsSpace)
	if end < 0 {
		return body, ""
	}
	return body[:end], body[end:]
}

// isCommandToken reports whether token is a slash followed by a plain word.
func isCommandToken(token string) bool {
	word, ok := strings.CutPrefix(token, "/")
	if !ok || word == "" {
		return false
	}
	for i, r := range word {
		alphanumeric := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		interiorPunctuation := i > 0 && (r == '-' || r == '_')
		if !alphanumeric && !interiorPunctuation {
			return false
		}
	}
	return true
}
