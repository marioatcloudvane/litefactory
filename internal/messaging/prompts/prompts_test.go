package prompts

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/factorylib"
	"github.com/cloudvane/litefactory/internal/factorylib/factorylibtest"
)

func TestInvalidFormatFailsWithoutFallback(t *testing.T) {
	for _, body := range []string{
		"owner main", MainMarker + "\nowner main\n", MainMarker + "\nowner main\n" + CorrectionMarker + "\n", MainMarker + "\nowner main {{Other}}\n" + CorrectionMarker + "\nowner correction\n", MainMarker + "\nowner main\n" + CorrectionMarker + "\nowner correction\n" + CorrectionMarker + "\nextra",
	} {
		t.Run(body, func(t *testing.T) {
			lib := factorylibtest.With(t, map[string]string{factorylib.ChatDraft.Path(): body})
			var diagnostics []Diagnostic
			if _, err := Load(t.Context(), lib, Draft, func(_ context.Context, d Diagnostic) { diagnostics = append(diagnostics, d) }); err == nil {
				t.Fatal("invalid format accepted")
			}
			if len(diagnostics) != 1 || diagnostics[0].Category != "format" || diagnostics[0].Role != Draft || len(diagnostics[0].Digest) != 64 {
				t.Fatalf("diagnostics: %#v", diagnostics)
			}
		})
	}
}

func TestMissingLibraryAndCancellationFail(t *testing.T) {
	if _, err := Load(t.Context(), nil, Draft, nil); err == nil {
		t.Fatal("missing library accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Load(ctx, factorylibtest.Library(t), Draft, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestRestrictedPlaceholder(t *testing.T) {
	for _, tc := range []struct {
		role, body string
		valid      bool
	}{
		{Draft, MainMarker + "\nmain\n" + CorrectionMarker + "\nfix\n", true}, {Draft, MainMarker + "\nlimit {{MaxClaims}}\n" + CorrectionMarker + "\nfix\n", false}, {Draft, MainMarker + "\nmain\n" + CorrectionMarker + "\n{{Other}}\n", false}, {"chat-reply-writer", "plain text", false},
	} {
		got, err := parse(tc.role, tc.body)
		if (err == nil) != tc.valid {
			t.Fatalf("%s %q: %v", tc.role, tc.body, err)
		}
		if tc.valid && strings.Contains(got.Main, "{{") {
			t.Fatal("unrendered scalar")
		}
	}
}

// TestLibraryBodies checks only that the library chat-draft body parses into
// non-empty sections; wording is owned by the Markdown, not by tests.
func TestLibraryBodies(t *testing.T) {
	body, err := Load(t.Context(), factorylibtest.Library(t), Draft, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(body.Main) == "" || strings.TrimSpace(body.Correction) == "" {
		t.Fatal("library chat-draft has an empty section")
	}
}
