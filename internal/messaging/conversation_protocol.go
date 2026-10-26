package messaging

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/messaging/ask"
	"github.com/cloudvane/litefactory/internal/messaging/presentation"
)

const conversationEnvelopeLimit = 65536

// ErrConversationEnvelope indicates unsafe or malformed provider output.
var ErrConversationEnvelope = errors.New("messaging: invalid conversation envelope")

var errInterpretation = errors.New("messaging: invalid interpretation")

// ConversationEnvelopeError is a rejected provider output with a short plain
// reason the factory can hand back in a bounded correction. It matches
// ErrConversationEnvelope. Argument is the argument it rejected, if any, for
// the owner's reply; Ungrounded marks a declared argument that no owner span
// or current factory reference supported.
type ConversationEnvelopeError struct {
	Reason     string
	Argument   *ArgumentError
	Ungrounded bool
}

func (e *ConversationEnvelopeError) Error() string {
	return ErrConversationEnvelope.Error() + ": " + e.Reason
}

// Is matches the shared invalid-envelope error.
func (e *ConversationEnvelopeError) Is(target error) bool { return target == ErrConversationEnvelope }

// Unwrap exposes the rejected argument, which matches ErrInvalidInvocation.
func (e *ConversationEnvelopeError) Unwrap() error {
	if e.Argument == nil {
		return nil
	}
	return e.Argument
}

// conversationEnvelopeReason returns the plain rejection reason, if any.
func conversationEnvelopeReason(err error) string {
	var rejected *ConversationEnvelopeError
	if errors.As(err, &rejected) {
		return rejected.Reason
	}
	return ""
}

// conversationObject tolerates harmless noise around the protocol object, such
// as leading prose, a stray tool tag or a code fence. It returns the single
// complete JSON object in raw, or nil when there is none or more than one, so
// ambiguity still fails. The extracted object then passes the same strict
// validation as a clean reply; the discarded noise is never shown or acted on,
// and no authority lives in provider output, so tolerating it is safe.
func conversationObject(raw []byte) []byte {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && trimmed[0] == '{' && trimmed[len(trimmed)-1] == '}' && json.Valid(trimmed) {
		return trimmed
	}
	if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') {
		return nil
	}
	var found []byte
	objects, attempts := 0, 0
	for i := 0; i < len(raw); {
		j := bytes.IndexByte(raw[i:], '{')
		if j < 0 {
			break
		}
		start := i + j
		if attempts++; attempts > 256 {
			return nil
		}
		decoder := json.NewDecoder(bytes.NewReader(raw[start:]))
		var object map[string]json.RawMessage
		if decoder.Decode(&object) != nil {
			return nil
		}
		end := start + int(decoder.InputOffset())
		objects++
		found = raw[start:end]
		i = end
	}
	if objects != 1 {
		return nil
	}
	return found
}

// ConversationEnvelope is a suggestion or response, never execution authority.
type ConversationEnvelope struct {
	Version   int                        `json:"version"`
	Kind      string                     `json:"kind"`
	Text      string                     `json:"text,omitempty"`
	Blocks    []presentation.AnswerBlock `json:"blocks,omitempty"`
	Command   string                     `json:"command,omitempty"`
	Arguments []ConversationArgument     `json:"arguments,omitempty"`
	// Question and Tried form a consult: the owner's question restated, and
	// what was tried and why it is not enough.
	Question string `json:"question,omitempty"`
	Tried    string `json:"tried,omitempty"`
	// Proposal and Confirmation form a confirm or cancel: the open proposal's
	// reference, and for a confirm the owner's words that say yes.
	Proposal     string            `json:"proposal,omitempty"`
	Confirmation *ConversationSpan `json:"confirmation,omitempty"`
}

// ConversationSpan is an exact UTF-8 byte span of one owner message.
type ConversationSpan struct {
	SourceID string `json:"source_id"`
	Start    int    `json:"start"`
	End      int    `json:"end"`
}

// validOwnerConfirmationSpan rejects out-of-bounds, quoted or forwarded agreement.
func validOwnerConfirmationSpan(text string, span ConversationSpan) bool {
	if span.Start < 0 || span.End <= span.Start || span.End > len(text) || !utf8.ValidString(text[:span.Start]) || !utf8.ValidString(text[span.Start:span.End]) || quotedSpan(text, span.Start, span.End) || quotedConfirmationSpan(text, span.Start, span.End) {
		return false
	}
	return true
}

// quotedConfirmationSpan detects quoted context even when the selected agreement
// is only part of a longer quote. Apostrophes within words are not delimiters.
func quotedConfirmationSpan(text string, start, end int) bool {
	var closing rune
	for offset, ch := range text[:end] {
		if offset >= start && closing != 0 {
			return true
		}
		if ch == '\'' {
			previous, _ := utf8.DecodeLastRuneInString(text[:offset])
			next, _ := utf8.DecodeRuneInString(text[offset+1:])
			if unicode.IsLetter(previous) && (closing == 0 || unicode.IsLetter(next)) {
				continue
			}
		}
		if closing != 0 {
			if ch == closing {
				closing = 0
			}
			continue
		}
		switch ch {
		case '"', '\'', '`':
			closing = ch
		case '“':
			closing = '”'
		case '‘':
			closing = '’'
		}
		if offset >= start && closing != 0 {
			return true
		}
	}
	return false
}

// ownerArgumentSpan accepts literal quoted data without giving it confirmation
// authority. The caller selects the interior byte span of delimiter quotes;
// selected bytes are never unescaped, trimmed or reconstructed.
func ownerArgumentSpan(text string, span ConversationSpan) (string, bool) {
	if span.Start < 0 || span.End <= span.Start || span.End > len(text) || !utf8.ValidString(text[:span.Start]) || !utf8.ValidString(text[span.Start:span.End]) || untrustedOwnerSpan(text, span.Start, span.End) {
		return "", false
	}
	return text[span.Start:span.End], true
}

// ConversationArgument selects exactly one original owner span, factory-issued
// reference, or (for reads only) bounded scalar. Offsets are UTF-8 byte offsets.
type ConversationArgument struct {
	Name      string `json:"name"`
	SourceID  string `json:"source_id,omitempty"`
	Start     int    `json:"start,omitempty"`
	End       int    `json:"end,omitempty"`
	Reference string `json:"reference,omitempty"`
	Value     string `json:"value,omitempty"`
}

// ConversationReference is factory-owned data. Callers supply only references
// loaded for this authorized channel and recheck current target revisions before
// reading or preparing a proposal. A reference is not a permission grant.
type ConversationReference struct {
	ID         string            `json:"id"`
	OwnerID    string            `json:"-"`
	ChannelID  string            `json:"-"`
	OriginTurn string            `json:"-"`
	RepoID     int64             `json:"-"`
	Revision   string            `json:"revision,omitempty"`
	ExpiresAt  time.Time         `json:"-"`
	Arguments  map[string]string `json:"arguments"`
}

// ConversationDecodeRequest holds factory-authorized sources and capabilities.
type ConversationDecodeRequest struct {
	Turns              []DialogueTurn
	Commands           []CommandDefinition
	References         []ConversationReference
	OwnerID, ChannelID string
	Now                time.Time
	// Proposal is the reference of the one open proposal, if any.
	Proposal string
}

// ConversationCommands projects only existing enabled factory capabilities.
func (s *Service) ConversationCommands() []CommandDefinition {
	var out []CommandDefinition
	for _, def := range s.NaturalCommands() {
		if conversationCommand(def) {
			out = append(out, def)
		}
	}
	return out
}

func conversationCommand(def CommandDefinition) bool {
	if !def.NaturalLanguage {
		return false
	}
	if standardsCommand(def.Name) {
		return true
	}
	switch def.Name {
	case "help", "health", "status", "meter", "prices", "ask", "question", "inbox":
		return def.Effect == ReadEffect
	case "draft", "confirm", "diagnose", "answer":
		return def.Effect == ChangeEffect
	}
	return false
}

// DecodeConversationEnvelope rejects structural ambiguity before resolving any
// argument. Authoritative identities and permissions are never model fields.
func DecodeConversationEnvelope(raw []byte, request ConversationDecodeRequest) (ConversationEnvelope, Invocation, error) {
	reason := "the output did not follow the turn protocol"
	var rejected *ArgumentError
	ungrounded := false
	bad := func() (ConversationEnvelope, Invocation, error) {
		return ConversationEnvelope{}, Invocation{}, &ConversationEnvelopeError{Reason: reason, Argument: rejected, Ungrounded: ungrounded}
	}
	if len(raw) == 0 || len(raw) > conversationEnvelopeLimit || !utf8.Valid(raw) {
		return bad()
	}
	object := conversationObject(raw)
	if object == nil && safeConversationProse(raw) {
		// Feed answer-only prose through the same bounds and presentation checks.
		// No command or authority can be recovered from this fallback.
		var err error
		object, err = json.Marshal(ConversationEnvelope{Version: 1, Kind: "answer", Text: string(raw)})
		if err != nil {
			return bad()
		}
	}
	if object == nil {
		reason = "the output must contain exactly one JSON object"
		return bad()
	}
	raw = object
	if uniqueJSON(raw) != nil {
		reason = "the JSON object has duplicate keys, null values or trailing data"
		return bad()
	}
	if validateConversationTurns(request.Turns, request.Commands) != nil || len(request.References) > 32 {
		return bad()
	}
	var envelope ConversationEnvelope
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&envelope) != nil || envelope.Version != 1 {
		reason = "the JSON object has unknown fields or is not version 1"
		return bad()
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return bad()
	}
	required := []string{"version", "kind"}
	switch envelope.Kind {
	case "answer", "clarify":
		reason = "answer blocks are out of bounds"
		if envelope.Kind == "answer" && len(envelope.Blocks) > 0 {
			required = append(required, "blocks")
			if len(envelope.Blocks) > 128 {
				return bad()
			}
			total, references := 0, 0
			for _, block := range envelope.Blocks {
				total += len(block.Text)
				references += len(block.FactRefs)
				if strings.ContainsRune(block.Text, 0) || strings.TrimSpace(block.Text) == "" || len(block.FactRefs) > 32 {
					reason = "every answer block needs natural text"
					return bad()
				}
				if raw := presentation.ConversationRecordSyntax(block.Text); raw != "" {
					reason = fmt.Sprintf("the answer pastes raw record syntax (%q); say it in plain words", raw)
					return bad()
				}
				for _, ref := range block.FactRefs {
					if ref == "" || len(ref) > 128 {
						return bad()
					}
				}
			}
			if total > 32768 || references > 32 {
				return bad()
			}
		} else {
			required = append(required, "text")
			limit := 32768
			if envelope.Kind == "clarify" {
				limit = 1000
			}
			if strings.TrimSpace(envelope.Text) == "" || len(envelope.Text) > limit || strings.ContainsRune(envelope.Text, 0) {
				reason = fmt.Sprintf("%s needs non-empty text of at most %d bytes", envelope.Kind, limit)
				return bad()
			}
			if raw := presentation.ConversationRecordSyntax(envelope.Text); raw != "" {
				reason = fmt.Sprintf("the answer pastes raw record syntax (%q); say it in plain words", raw)
				return bad()
			}
		}
	case "lookup", "propose":
		required = append(required, "command", "arguments")
	case "confirm", "cancel":
		required = append(required, "proposal")
		reason = "there is no open proposal to confirm or cancel"
		if request.Proposal == "" {
			return bad()
		}
		reason = "the proposal must be the reference of the open proposal"
		if envelope.Proposal != request.Proposal {
			return bad()
		}
		if envelope.Kind == "confirm" {
			required = append(required, "confirmation")
			reason = "confirmation must be an exact span of the owner's current message"
			current := request.Turns[len(request.Turns)-1]
			if envelope.Confirmation == nil || envelope.Confirmation.SourceID != current.SourceID {
				return bad()
			}
			if !validOwnerConfirmationSpan(current.Text, *envelope.Confirmation) {
				return bad()
			}
		}
	case "consult":
		required = append(required, "question", "tried")
		reason = "consult needs a non-empty question and tried of at most 4000 bytes each"
		for _, text := range []string{envelope.Question, envelope.Tried} {
			if strings.TrimSpace(text) == "" || len(text) > 4000 || strings.ContainsRune(text, 0) {
				return bad()
			}
		}
	default:
		reason = "kind must be answer, clarify, lookup, consult, propose, confirm or cancel"
		return bad()
	}
	if !exactConversationFields(fields, required...) {
		reason = "the object must have exactly the fields " + strings.Join(required, ", ")
		return bad()
	}
	switch envelope.Kind {
	case "answer", "clarify", "consult", "confirm", "cancel":
		return envelope, Invocation{}, nil
	}
	var def *CommandDefinition
	for i := range request.Commands {
		d := &request.Commands[i]
		if d.Name == envelope.Command && conversationCommand(*d) {
			def = d
			break
		}
	}
	if def == nil || len(envelope.Arguments) > len(def.Arguments) || (envelope.Kind == "lookup" && def.Effect != ReadEffect) || (envelope.Kind == "propose" && def.Effect != ChangeEffect) {
		reason = "the command is not enabled for this kind, or has too many arguments"
		return bad()
	}
	reason = "an argument is not an exact owner span, a current factory reference, or a plain value (lookups, and standards action, category_id, check, applies_to, version and mode)"
	var arguments []map[string]json.RawMessage
	if json.Unmarshal(fields["arguments"], &arguments) != nil {
		return bad()
	}
	declared := map[string]bool{}
	for _, a := range def.Arguments {
		declared[a.Name] = true
	}
	inv := Invocation{Command: envelope.Command, Arguments: map[string]string{}}
	for i, arg := range envelope.Arguments {
		// Each rejection names the argument and why, so a bounded correction can
		// fix it; an undeclared name is untrusted and never echoed.
		ground := func(why string) {
			rejected = &ArgumentError{Why: why}
			if declared[arg.Name] {
				rejected.Name = arg.Name
				ungrounded = true
			}
			reason = rejected.Detail() + "; each argument needs an exact owner span, a current factory reference, or a plain value (lookups, and standards action, category_id, check, applies_to, version and mode)"
		}
		if _, exists := inv.Arguments[arg.Name]; exists {
			ground("given more than once")
			return bad()
		}
		f := arguments[i]
		var value string
		switch {
		case exactConversationFields(f, "name", "source_id", "start", "end"):
			found := false
			for _, turn := range request.Turns {
				if turn.SourceID != arg.SourceID {
					continue
				}
				var ok bool
				if value, ok = ownerArgumentSpan(turn.Text, ConversationSpan{SourceID: arg.SourceID, Start: arg.Start, End: arg.End}); !ok {
					ground("the span is not an exact part of that owner message")
					return bad()
				}
				found = true
				break
			}
			if !found {
				ground("the span names no supplied owner message")
				return bad()
			}
		case exactConversationFields(f, "name", "reference"):
			if conversationProtectedArgument(envelope.Command, arg.Name) {
				ground("owner content needs an exact owner span, not a reference")
				return bad()
			}
			if arg.Reference == "" || len(arg.Reference) > 128 || request.OwnerID == "" || request.ChannelID == "" || request.Now.IsZero() {
				ground("not a current factory reference")
				return bad()
			}
			found := false
			for _, ref := range request.References {
				if ref.ID != arg.Reference {
					continue
				}
				if ref.OwnerID != request.OwnerID || ref.ChannelID != request.ChannelID || ref.OriginTurn == "" || ref.ExpiresAt.IsZero() || !request.Now.Before(ref.ExpiresAt) {
					ground("the reference is expired or not from this conversation")
					return bad()
				}
				var exists bool
				value, exists = ref.Arguments[arg.Name]
				if !exists {
					ground("the reference carries no such argument")
					return bad()
				}
				found = true
				break
			}
			if !found {
				ground("not a current factory reference")
				return bad()
			}
		case exactConversationFields(f, "name", "value"):
			if (envelope.Kind != "lookup" && (!standardsCommand(envelope.Command) || !standardsPlainValue(arg.Name))) || conversationProtectedArgument(envelope.Command, arg.Name) || arg.Value == "" {
				ground("a plain value is allowed only in lookups, and in standards changes for action, category_id, check, applies_to, version and mode")
				return bad()
			}
			value = arg.Value
		default:
			ground("needs exactly one of value, source_id/start/end or reference")
			return bad()
		}
		inv.Arguments[arg.Name] = value
	}
	// A log analysis has one change action; the owner never has to say it.
	if envelope.Kind == "propose" && envelope.Command == "standards-analysis" && inv.Arguments["action"] == "" {
		inv.Arguments["action"] = "start"
	}
	validated, _, err := validateInvocation(*def, inv)
	if err != nil {
		// The diagnostics name fields only, never owner content.
		reason = "the command arguments are invalid: " + argumentFaults(err)
		errors.As(err, &rejected)
		return bad()
	}
	if standardsCommand(validated.Command) {
		if _, err := standardsOperation(validated); err != nil {
			rejected = &ArgumentError{Name: "action", Why: err.Error()}
			reason = "the command arguments are invalid: " + rejected.Detail()
			return bad()
		}
	}
	reason = "the current message names an explicit issue; use that target"
	// A current explicit issue target cannot be replaced by a remembered target.
	latest := request.Turns[len(request.Turns)-1].Text
	targets := ask.ExplicitIssues(latest)
	if len(targets) > 0 && !quotedSpan(latest, 0, len(latest)) {
		switch validated.Command {
		case "ask":
			if !slices.Equal(targets, ask.ExplicitIssues(validated.Arguments["question"])) {
				return bad()
			}
		case "diagnose", "answer", "question":
			if len(targets) != 1 || validated.Arguments["issue"] != strconv.Itoa(targets[0]) {
				return bad()
			}
		case "meter":
			if len(targets) != 1 || validated.Arguments["task"] != strconv.Itoa(targets[0]) {
				return bad()
			}
		}
	}
	return envelope, validated, nil
}

// argumentFaults lists each rejected argument as "name: why".
func argumentFaults(err error) string {
	faults := []error{err}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		faults = joined.Unwrap()
	}
	parts := make([]string, 0, len(faults))
	for _, fault := range faults {
		var arg *ArgumentError
		if errors.As(fault, &arg) {
			parts = append(parts, arg.Detail())
		} else {
			parts = append(parts, strings.ReplaceAll(fault.Error(), "\n", "; "))
		}
	}
	return strings.Join(parts, "; ")
}

func conversationProtectedContent(name string) bool {
	switch name {
	case "text", "note", "intent", "title", "body", "operations", "request", "reason", "config", "departure", "name":
		return true
	}
	return false
}

// standardsPlainValue names the standards change arguments that take a plain
// value: enum and id fields the factory validates against the book and its
// enums. Prose stays an exact owner span.
func standardsPlainValue(name string) bool {
	switch name {
	case "action", "category_id", "check", "applies_to", "version", "mode":
		return true
	}
	return false
}

// standardsProtectedContent names the prose fields of a standard. They are
// owner content only on standards commands; other commands keep these names
// as ordinary arguments.
func standardsProtectedContent(name string) bool {
	switch name {
	case "rule", "description", "evidence", "scope":
		return true
	}
	return false
}

// conversationProtectedArgument reports whether the argument of command must
// be an exact owner span.
func conversationProtectedArgument(command, name string) bool {
	return conversationProtectedContent(name) || (standardsCommand(command) && standardsProtectedContent(name))
}

func exactConversationFields(fields map[string]json.RawMessage, names ...string) bool {
	if len(fields) != len(names) {
		return false
	}
	for _, name := range names {
		if _, ok := fields[name]; !ok {
			return false
		}
	}
	return true
}

// MarshalJSON preserves explicit zero offsets for owner spans while emitting
// only the fields belonging to the selected argument form.
func (a ConversationArgument) MarshalJSON() ([]byte, error) {
	if a.SourceID != "" {
		return json.Marshal(struct {
			Name     string `json:"name"`
			SourceID string `json:"source_id"`
			Start    int    `json:"start"`
			End      int    `json:"end"`
		}{a.Name, a.SourceID, a.Start, a.End})
	}
	if a.Reference != "" {
		return json.Marshal(struct {
			Name      string `json:"name"`
			Reference string `json:"reference"`
		}{a.Name, a.Reference})
	}
	return json.Marshal(struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}{a.Name, a.Value})
}

// validateConversationTurns bounds the owner turns and command choices a
// provider request may carry.
func validateConversationTurns(turns []DialogueTurn, commands []CommandDefinition) error {
	if len(turns) == 0 || len(turns) > 8 || len(commands) > 32 {
		return errInterpretation
	}
	seen := map[string]bool{}
	total := 0
	for _, t := range turns {
		total += len(t.Text)
		if t.SourceID == "" || len(t.SourceID) > 128 || seen[t.SourceID] || !utf8.ValidString(t.Text) || strings.ContainsRune(t.Text, 0) || total > 32768 {
			return errInterpretation
		}
		seen[t.SourceID] = true
	}
	return nil
}

func quotedSpan(text string, start, end int) bool {
	if untrustedOwnerSpan(text, start, end) {
		return true
	}
	// Agreement inside quote delimiters is data, including inline quotes.
	before, after := strings.TrimSpace(text[:start]), strings.TrimSpace(text[end:])
	for _, pair := range [][2]string{{"\"", "\""}, {"'", "'"}, {"“", "”"}, {"‘", "’"}, {"`", "`"}} {
		if strings.HasSuffix(before, pair[0]) && strings.HasPrefix(after, pair[1]) {
			return true
		}
	}
	trimmed := strings.TrimSpace(text)
	return len(trimmed) > 1 && ((strings.HasPrefix(trimmed, "\"") && strings.HasSuffix(trimmed, "\"")) || (strings.HasPrefix(trimmed, "“") && strings.HasSuffix(trimmed, "”")))
}

func untrustedOwnerSpan(text string, start, end int) bool {
	// Fenced payloads and forwarded snapshots are never independent instructions.
	if strings.Contains(text, "```") || strings.Contains(text, "~~~") {
		return true
	}
	for _, line := range strings.Split(text[:end], "\n") {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "forwarded:") {
			return true
		}
	}
	lineStart := strings.LastIndex(text[:start], "\n") + 1
	for _, line := range strings.Split(text[lineStart:end], "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, ">") || strings.HasPrefix(strings.ToLower(trimmed), "forwarded:") {
			return true
		}
	}
	return false
}

// uniqueJSON catches duplicate keys at every depth and disallows null and trailing data.
func uniqueJSON(raw []byte) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var walk func() error
	walk = func() error {
		t, err := d.Token()
		if err != nil {
			return err
		}
		if t == nil {
			return errInterpretation
		}
		delimiter, ok := t.(json.Delim)
		if !ok {
			return nil
		}
		switch delimiter {
		case '{':
			keys := map[string]bool{}
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return err
				}
				s, ok := key.(string)
				if !ok || keys[s] {
					return errInterpretation
				}
				keys[s] = true
				if err := walk(); err != nil {
					return err
				}
			}
		case '[':
			for d.More() {
				if err := walk(); err != nil {
					return err
				}
			}
		default:
			return errInterpretation
		}
		_, err = d.Token()
		return err
	}
	if err := walk(); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return fmt.Errorf("%w: trailing input", errInterpretation)
	}
	return nil
}
