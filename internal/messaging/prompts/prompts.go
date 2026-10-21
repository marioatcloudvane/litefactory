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

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/instructionpackage"
)

// Chat role names select only registered instruction-package roles.
const (
	// Draft selects the feature draft role.
	Draft = "chat-draft"
	// MainMarker begins the main section in draft files.
	MainMarker = "<!-- main -->"
	// CorrectionMarker begins the correction section in draft files.
	CorrectionMarker = "<!-- correction -->"
)

// Resolver retains baseline access so invalid owner formatting can fall back as
// one complete role, without reading another live owner file.
type Resolver interface {
	Capture(context.Context, string) (*instructionpackage.RoleCapture, []instructionpackage.Diagnostic, error)
	CaptureShipped(context.Context, string) (*instructionpackage.RoleCapture, error)
}

// Diagnostic contains only source identity and failure category, never wording.
type Diagnostic struct {
	Role     string
	Source   instructionpackage.Source
	Digest   string
	Category string
}

// Sink reports instruction selection failures without exposing instruction text.
type Sink func(context.Context, Diagnostic)

// Role holds both sections selected and validated before any provider call.
type Role struct{ Main, Correction string }

// Load captures once and validates the whole selected role. A nil resolver uses
// the shipped package; daemon consumers normally inject their configured one.
func Load(ctx context.Context, resolver Resolver, role string, sink Sink) (Role, error) {
	if err := ctx.Err(); err != nil {
		return Role{}, err
	}
	shippedOnly := resolver == nil
	if resolver == nil {
		var err error
		resolver, err = instructionpackage.New(ctx, config.Paths{})
		if err != nil {
			return Role{}, err
		}
	}
	var capture *instructionpackage.RoleCapture
	var diagnostics []instructionpackage.Diagnostic
	var err error
	if shippedOnly {
		capture, err = resolver.CaptureShipped(ctx, role)
	} else {
		capture, diagnostics, err = resolver.Capture(ctx, role)
	}
	if err != nil {
		return Role{}, err
	}
	if capture == nil {
		return Role{}, errors.New("chat instructions: missing capture")
	}
	for _, diagnostic := range diagnostics {
		emit(ctx, sink, capture, diagnostic.Category)
	}
	parsed, err := parse(role, string(capture.Instructions()))
	if err == nil {
		if err := ctx.Err(); err != nil {
			return Role{}, err
		}
		return parsed, nil
	}
	if err := ctx.Err(); err != nil {
		return Role{}, err
	}
	if source(capture) != instructionpackage.SourceOwner {
		emit(ctx, sink, capture, "format")
		return Role{}, fmt.Errorf("chat instructions %s: invalid shipped format", role)
	}
	emit(ctx, sink, capture, "format")
	capture, err = resolver.CaptureShipped(ctx, role)
	if err != nil {
		return Role{}, err
	}
	if capture == nil {
		return Role{}, errors.New("chat instructions: missing shipped capture")
	}
	parsed, err = parse(role, string(capture.Instructions()))
	if err != nil {
		emit(ctx, sink, capture, "format")
		return Role{}, fmt.Errorf("chat instructions %s: invalid shipped format", role)
	}
	if err := ctx.Err(); err != nil {
		return Role{}, err
	}
	return parsed, nil
}

func source(capture *instructionpackage.RoleCapture) instructionpackage.Source {
	snapshot := capture.Snapshot()
	for _, file := range snapshot.Files {
		if file.Path == "agents/"+snapshot.Role+".md" {
			return file.Source
		}
	}
	return instructionpackage.SourceShipped
}

func emit(ctx context.Context, sink Sink, capture *instructionpackage.RoleCapture, category string) {
	if sink != nil {
		snapshot := capture.Snapshot()
		sink(ctx, Diagnostic{Role: snapshot.Role, Source: source(capture), Digest: snapshot.EffectiveDigest, Category: category})
	}
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
