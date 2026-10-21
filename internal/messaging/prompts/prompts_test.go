package prompts

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/cloudvane/litefactory/internal/instructionpackage"
)

func fixture(t *testing.T, role, shipped, owner string) *instructionpackage.Package {
	t.Helper()
	dir := t.TempDir()
	catalogue := "- " + role + "\n"
	if err := os.Mkdir(filepath.Join(dir, "agents"), 0700); err != nil {
		t.Fatal(err)
	}
	for path, text := range map[string]string{"catalogue.yaml": catalogue, "agents/" + role + ".md": owner} {
		if err := os.WriteFile(filepath.Join(dir, path), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	pkg, err := instructionpackage.Load(t.Context(), fstest.MapFS{
		"metadata.yaml":          {Data: []byte("schema: 1\nversion: fixture\n")},
		"catalogue.yaml":         {Data: []byte(catalogue)},
		"agents/" + role + ".md": {Data: []byte(shipped)},
	}, dir)
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

func TestFormatFallbackIsWholeRole(t *testing.T) {
	shipped := MainMarker + "\nshipped main\n" + CorrectionMarker + "\nshipped correction\n"
	for _, owner := range []string{
		"owner main", MainMarker + "\nowner main\n", MainMarker + "\nowner main\n" + CorrectionMarker + "\n", MainMarker + "\nowner main {{Other}}\n" + CorrectionMarker + "\nowner correction\n", MainMarker + "\nowner main\n" + CorrectionMarker + "\nowner correction\n" + CorrectionMarker + "\nextra",
	} {
		t.Run(owner, func(t *testing.T) {
			pkg := fixture(t, Draft, shipped, owner)
			var diagnostics []Diagnostic
			role, err := Load(t.Context(), pkg, Draft, func(_ context.Context, d Diagnostic) { diagnostics = append(diagnostics, d) })
			if err != nil {
				t.Fatal(err)
			}
			if role.Main != "shipped main" || role.Correction != "shipped correction" {
				t.Fatalf("mixed fallback: %#v", role)
			}
			if len(diagnostics) != 1 || diagnostics[0].Category != "format" || diagnostics[0].Role != Draft || diagnostics[0].Source != instructionpackage.SourceOwner || len(diagnostics[0].Digest) != 64 {
				t.Fatalf("diagnostics: %#v", diagnostics)
			}
		})
	}
}

func TestInvalidShippedAndCancellationFail(t *testing.T) {
	pkg := fixture(t, Draft, "no sections", "invalid owner")
	if _, err := Load(t.Context(), pkg, Draft, nil); err == nil {
		t.Fatal("invalid shipped format accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Load(ctx, pkg, Draft, nil); !errors.Is(err, context.Canceled) {
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

// TestShippedBodies checks only that the shipped chat-draft body parses into
// non-empty sections; wording is owned by the Markdown, not by tests.
func TestShippedBodies(t *testing.T) {
	body, err := Load(t.Context(), nil, Draft, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(body.Main) == "" || strings.TrimSpace(body.Correction) == "" {
		t.Fatal("shipped chat-draft has an empty section")
	}
}
