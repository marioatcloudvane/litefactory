// Package prompts assembles captured chat wording without granting it authority.
// Draft files contain exactly one <!-- main --> line followed by exactly one
// <!-- correction --> line. Both sections must be nonempty. Marker lines are
// framing, not prompt text. No template tokens are accepted. Runtime context is
// appended by the consumer.
package prompts

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/factorylib"
)

// Chat role names select only registered factory library roles.
const (
	// Draft selects the feature draft role.
	Draft = string(factorylib.ChatDraft)
	// MainMarker begins the main section in draft files.
	MainMarker = "<!-- main -->"
	// CorrectionMarker begins the correction section in draft files.
	CorrectionMarker = "<!-- correction -->"
)

// Resolver is the factory library chat roles are captured from.
type Resolver = factorylib.Source

// Diagnostic contains only role identity and failure category, never wording.
type Diagnostic struct {
	Role     string
	Digest   string
	Category string
}

// Sink reports instruction selection failures without exposing instruction text.
type Sink func(context.Context, Diagnostic)

// Role holds both sections selected and validated before any provider call.
type Role struct{ Main, Correction string }

// Load captures once and validates the whole selected role. There is no
// fallback text: a library role in the wrong format fails the call.
func Load(ctx context.Context, resolver Resolver, role string, sink Sink) (Role, error) {
	if err := ctx.Err(); err != nil {
		return Role{}, err
	}
	capture, err := factorylib.CaptureRole(ctx, resolver, factorylib.Role(role))
	if err != nil {
		return Role{}, err
	}
	parsed, err := parse(role, string(capture.Instructions()))
	if err != nil {
		if sink != nil {
			sink(ctx, Diagnostic{Role: role, Digest: capture.Digest(), Category: "format"})
		}
		return Role{}, fmt.Errorf("chat instructions %s: invalid format: %w", role, err)
	}
	if err := ctx.Err(); err != nil {
		return Role{}, err
	}
	return parsed, nil
}

func parse(role, text string) (Role, error) {
	var result Role
	switch role {
	case Draft:
		// Require marker lines and no content outside the two framed sections.
		if strings.Count(text, MainMarker) != 1 || strings.Count(text, CorrectionMarker) != 1 || !strings.HasPrefix(text, MainMarker+"\n") {
			return result, errors.New("invalid section markers")
		}
		parts := strings.SplitN(strings.TrimPrefix(text, MainMarker+"\n"), "\n"+CorrectionMarker+"\n", 2)
		if len(parts) != 2 {
			return result, errors.New("invalid section order")
		}
		result = Role{Main: parts[0], Correction: strings.TrimSuffix(parts[1], "\n")}
	default:
		return result, errors.New("unknown chat role")
	}
	if strings.TrimSpace(result.Main) == "" || strings.TrimSpace(result.Correction) == "" {
		return Role{}, errors.New("empty instruction section")
	}
	for _, value := range []string{result.Main, result.Correction} {
		if strings.Contains(value, "{{") || strings.Contains(value, "}}") {
			return Role{}, errors.New("unsupported placeholder")
		}
	}
	return result, nil
}
