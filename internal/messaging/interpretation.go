package messaging

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/messaging/ask"

	"github.com/cloudvane/litefactory/internal/messaging/prompts"
	"github.com/cloudvane/litefactory/internal/messaging/response"
)

const interpretationLimit = 16384

var errInterpretation = errors.New("messaging: invalid interpretation")

// ResponsePresenter is the shared isolated response-only provider seam.
type ResponsePresenter interface {
	Present(context.Context, int64, string, string, string) ([]byte, error)
}

// NativeInterpreter grants the model only bounded owner text and catalogue data.
type NativeInterpreter struct {
	Model                  ResponsePresenter
	Instructions           prompts.Resolver
	InstructionDiagnostics prompts.Sink
}

// Interpret makes one bounded response-only suggestion request.
func (n *NativeInterpreter) Interpret(ctx context.Context, request InterpretationRequest) ([]byte, error) {
	if n.Model == nil {
		return nil, errInterpretation
	}
	if err := validateInterpretationRequest(request); err != nil {
		return nil, err
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, response.Budget)
	defer cancel()
	instructions, err := prompts.Load(ctx, n.Instructions, prompts.IntentInterpreter, n.InstructionDiagnostics)
	if err != nil {
		return nil, err
	}
	prompt := instructions.Main + "\nInput: " + string(payload)
	digest := sha256.Sum256([]byte(request.Turns[len(request.Turns)-1].SourceID))
	identity := "interpretation-" + hex.EncodeToString(digest[:])
	raw, err := n.Model.Present(ctx, 0, identity, "generation", prompt)
	if err != nil {
		return nil, err
	}
	if len(raw) > interpretationLimit {
		return nil, errInterpretation
	}
	return raw, nil
}

func validateInterpretationRequest(r InterpretationRequest) error {
	if len(r.Turns) == 0 || len(r.Turns) > 8 || len(r.Commands) > 32 {
		return errInterpretation
	}
	seen := map[string]bool{}
	total := 0
	for _, t := range r.Turns {
		total += len(t.Text)
		if t.SourceID == "" || len(t.SourceID) > 128 || seen[t.SourceID] || !utf8.ValidString(t.Text) || strings.ContainsRune(t.Text, 0) || total > 32768 {
			return errInterpretation
		}
		seen[t.SourceID] = true
	}
	return nil
}

// DecodeInterpretation refuses structural ambiguity before binding original bytes.
func DecodeInterpretation(raw []byte, request InterpretationRequest) (Interpretation, Invocation, error) {
	bad := func() (Interpretation, Invocation, error) { return Interpretation{}, Invocation{}, errInterpretation }
	if len(raw) == 0 || len(raw) > interpretationLimit || !utf8.Valid(raw) || validateInterpretationRequest(request) != nil {
		return bad()
	}
	if err := uniqueJSON(raw); err != nil {
		return bad()
	}
	var result Interpretation
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&result) != nil || result.Version != 1 {
		return bad()
	}
	fields := map[string]json.RawMessage{}
	if json.Unmarshal(raw, &fields) != nil {
		return bad()
	}
	required := []string{"version", "kind"}
	switch result.Kind {
	case "ignore", "unsupported":
	case "clarify":
		required = append(required, "question")
		if strings.TrimSpace(result.Question) == "" || len(result.Question) > 1000 || strings.ContainsRune(result.Question, 0) {
			return bad()
		}
	case "propose":
		required = append(required, "command", "arguments")
	default:
		return bad()
	}
	if len(fields) != len(required) {
		return bad()
	}
	for _, key := range required {
		if _, ok := fields[key]; !ok {
			return bad()
		}
	}
	if result.Kind != "propose" {
		return result, Invocation{}, nil
	}
	var spans []map[string]json.RawMessage
	if json.Unmarshal(fields["arguments"], &spans) != nil {
		return bad()
	}
	for _, span := range spans {
		if len(span) != 4 {
			return bad()
		}
		for _, key := range []string{"name", "source_id", "start", "end"} {
			if _, ok := span[key]; !ok {
				return bad()
			}
		}
	}
	var def *CommandDefinition
	for i := range request.Commands {
		d := &request.Commands[i]
		if d.Name == result.Command && d.NaturalLanguage && (d.Effect == ReadEffect || d.Effect == ChangeEffect) {
			def = d
			break
		}
	}
	if def == nil || len(result.Arguments) > len(def.Arguments) {
		return bad()
	}
	turns := map[string]string{}
	for _, t := range request.Turns {
		turns[t.SourceID] = t.Text
	}
	inv := Invocation{Command: result.Command, Arguments: map[string]string{}}
	for _, a := range result.Arguments {
		text, ok := turns[a.SourceID]
		if !ok || a.Start < 0 || a.End <= a.Start || a.End > len(text) {
			return bad()
		}
		value := text[a.Start:a.End]
		if !utf8.ValidString(value) || !utf8.ValidString(text[:a.Start]) || quotedSpan(text, a.Start, a.End) {
			return bad()
		}
		if _, exists := inv.Arguments[a.Name]; exists {
			return bad()
		}
		inv.Arguments[a.Name] = value
	}
	validated, _, err := validateInvocation(*def, inv)
	if err != nil {
		return bad()
	}
	// Bind explicit targets from the latest unquoted request, never prior replies.
	latest := request.Turns[len(request.Turns)-1].Text
	if !quotedSpan(latest, 0, len(latest)) {
		targets := ask.ExplicitIssues(latest)
		if len(targets) > 0 {
			switch validated.Command {
			case "details", "health", "status":
				return bad()
			case "ask":
				if !slices.Equal(targets, ask.ExplicitIssues(validated.Arguments["question"])) {
					return bad()
				}
			case "diagnose":
				if len(targets) != 1 || validated.Arguments["issue"] != strconv.Itoa(targets[0]) {
					return bad()
				}
			}
		}
		// A listing request must not silently degrade to aggregate health.
		lower := strings.ToLower(latest)
		if validated.Command == "health" && strings.Contains(lower, "tasks") && (strings.Contains(lower, "running") || strings.Contains(lower, "waiting")) {
			for _, command := range request.Commands {
				if command.Name == "status" && command.NaturalLanguage && command.Effect == ReadEffect {
					result.Command = "status"
					validated, _, err = validateInvocation(command, Invocation{Command: "status", Arguments: map[string]string{}})
					if err != nil {
						return bad()
					}
					break
				}
			}
		}
	}
	return result, validated, nil
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
