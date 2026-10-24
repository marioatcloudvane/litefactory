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
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/messaging/ask"
	"github.com/cloudvane/litefactory/internal/messaging/presentation"
)

const conversationEnvelopeLimit = 65536

// ErrConversationEnvelope indicates unsafe or malformed provider output.
var ErrConversationEnvelope = errors.New("messaging: invalid conversation envelope")

var errInterpretation = errors.New("messaging: invalid interpretation")

// ConversationEnvelopeError is a rejected provider output with a short plain
// reason the factory can hand back in its one correction. It matches
// ErrConversationEnvelope.
type ConversationEnvelopeError struct{ Reason string }

func (e *ConversationEnvelopeError) Error() string {
	return ErrConversationEnvelope.Error() + ": " + e.Reason
}

// Is matches the shared invalid-envelope error.
func (e *ConversationEnvelopeError) Is(target error) bool { return target == ErrConversationEnvelope }

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
			i = start + 1
			continue
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

// ownerSpan returns the exact owner words of span, or false when the span
// is out of bounds, splits a character or lies in quoted or forwarded text.
func ownerSpan(text string, span ConversationSpan) (string, bool) {
	if span.Start < 0 || span.End <= span.Start || span.End > len(text) || !utf8.ValidString(text[:span.Start]) || !utf8.ValidString(text[span.Start:span.End]) || quotedSpan(text, span.Start, span.End) {
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
	case "help", "health", "status", "meter", "prices", "ask", "question":
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
	bad := func() (ConversationEnvelope, Invocation, error) {
		return ConversationEnvelope{}, Invocation{}, &ConversationEnvelopeError{Reason: reason}
	}
	if len(raw) == 0 || len(raw) > conversationEnvelopeLimit || !utf8.Valid(raw) {
		return bad()
	}
	if raw = conversationObject(raw); raw == nil {
		reason = "the output must contain exactly one JSON object"
		return bad()
	}
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
			if _, ok := ownerSpan(current.Text, *envelope.Confirmation); !ok {
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
	reason = "an argument is not an exact owner span, a current factory reference, or (lookup only) a plain value"
	var arguments []map[string]json.RawMessage
	if json.Unmarshal(fields["arguments"], &arguments) != nil {
		return bad()
	}
	inv := Invocation{Command: envelope.Command, Arguments: map[string]string{}}
	for i, arg := range envelope.Arguments {
		if _, exists := inv.Arguments[arg.Name]; exists {
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
				if value, ok = ownerSpan(turn.Text, ConversationSpan{SourceID: arg.SourceID, Start: arg.Start, End: arg.End}); !ok {
					return bad()
				}
				found = true
				break
			}
			if !found {
				return bad()
			}
		case exactConversationFields(f, "name", "reference"):
			if arg.Reference == "" || len(arg.Reference) > 128 || request.OwnerID == "" || request.ChannelID == "" || request.Now.IsZero() || conversationProtectedContent(arg.Name) {
				return bad()
			}
			found := false
			for _, ref := range request.References {
				if ref.ID != arg.Reference {
					continue
				}
				if ref.OwnerID != request.OwnerID || ref.ChannelID != request.ChannelID || ref.OriginTurn == "" || ref.ExpiresAt.IsZero() || !request.Now.Before(ref.ExpiresAt) {
					return bad()
				}
				var exists bool
				value, exists = ref.Arguments[arg.Name]
				if !exists {
					return bad()
				}
				found = true
				break
			}
			if !found {
				return bad()
			}
		case exactConversationFields(f, "name", "value"):
			if (envelope.Kind != "lookup" && (!standardsCommand(envelope.Command) || arg.Name != "action")) || conversationProtectedContent(arg.Name) || arg.Value == "" {
				return bad()
			}
			value = arg.Value
		default:
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
		reason = "the command arguments are invalid: " + strings.ReplaceAll(err.Error(), "\n", "; ")
		return bad()
	}
	if standardsCommand(validated.Command) {
		if _, err := standardsOperation(validated); err != nil {
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

func conversationProtectedContent(name string) bool {
	switch name {
	case "text", "note", "intent", "title", "body", "operations", "request", "reason", "config", "departure", "name":
		return true
	}
	return false
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
	// Fenced payloads and forwarded snapshots are never independent instructions.
	if strings.Contains(text, "```") || strings.Contains(text, "~~~") {
		return true
	}
	lineStart := strings.LastIndex(text[:start], "\n") + 1
	for _, line := range strings.Split(text[lineStart:end], "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, ">") || strings.HasPrefix(strings.ToLower(trimmed), "forwarded:") {
			return true
		}
	}
	trimmed := strings.TrimSpace(text)
	return len(trimmed) > 1 && ((strings.HasPrefix(trimmed, "\"") && strings.HasSuffix(trimmed, "\"")) || (strings.HasPrefix(trimmed, "“") && strings.HasSuffix(trimmed, "”")))
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
