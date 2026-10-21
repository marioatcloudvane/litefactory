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

// ConversationEnvelope is a suggestion or response, never execution authority.
type ConversationEnvelope struct {
	Version   int                        `json:"version"`
	Kind      string                     `json:"kind"`
	Text      string                     `json:"text,omitempty"`
	Blocks    []presentation.AnswerBlock `json:"blocks,omitempty"`
	Command   string                     `json:"command,omitempty"`
	Arguments []ConversationArgument     `json:"arguments,omitempty"`
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
	bad := func() (ConversationEnvelope, Invocation, error) {
		return ConversationEnvelope{}, Invocation{}, ErrConversationEnvelope
	}
	if len(raw) == 0 || len(raw) > conversationEnvelopeLimit || !utf8.Valid(raw) || uniqueJSON(raw) != nil {
		return bad()
	}
	if validateConversationTurns(request.Turns, request.Commands) != nil || len(request.References) > 32 {
		return bad()
	}
	var envelope ConversationEnvelope
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&envelope) != nil || envelope.Version != 1 {
		return bad()
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return bad()
	}
	required := []string{"version", "kind"}
	switch envelope.Kind {
	case "answer", "clarify":
		if envelope.Kind == "answer" && len(envelope.Blocks) > 0 {
			required = append(required, "blocks")
			if len(envelope.Blocks) > 128 {
				return bad()
			}
			total, references := 0, 0
			for _, block := range envelope.Blocks {
				total += len(block.Text)
				references += len(block.FactRefs)
				if strings.ContainsRune(block.Text, 0) || (strings.TrimSpace(block.Text) == "" || presentation.ConversationDiagnostics(block.Text)) || len(block.FactRefs) > 32 {
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
			if strings.TrimSpace(envelope.Text) == "" || len(envelope.Text) > limit || strings.ContainsRune(envelope.Text, 0) || presentation.ConversationDiagnostics(envelope.Text) {
				return bad()
			}
		}
	case "lookup", "propose":
		required = append(required, "command", "arguments")
	default:
		return bad()
	}
	if !exactConversationFields(fields, required...) {
		return bad()
	}
	if envelope.Kind == "answer" || envelope.Kind == "clarify" {
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
		return bad()
	}
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
				if arg.Start < 0 || arg.End <= arg.Start || arg.End > len(turn.Text) || !utf8.ValidString(turn.Text[:arg.Start]) || !utf8.ValidString(turn.Text[arg.Start:arg.End]) || quotedSpan(turn.Text, arg.Start, arg.End) {
					return bad()
				}
				value = turn.Text[arg.Start:arg.End]
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
	validated, _, err := validateInvocation(*def, inv)
	if err != nil {
		return bad()
	}
	if standardsCommand(validated.Command) {
		if _, err := standardsOperation(validated); err != nil {
			return bad()
		}
	}
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
