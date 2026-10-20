package prompts

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
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
			pkg := fixture(t, Ask, shipped, owner)
			var diagnostics []Diagnostic
			role, err := Load(t.Context(), pkg, Ask, func(_ context.Context, d Diagnostic) { diagnostics = append(diagnostics, d) })
			if err != nil {
				t.Fatal(err)
			}
			if role.Main != "shipped main" || role.Correction != "shipped correction" {
				t.Fatalf("mixed fallback: %#v", role)
			}
			if len(diagnostics) != 1 || diagnostics[0].Category != "format" || diagnostics[0].Role != Ask || diagnostics[0].Source != instructionpackage.SourceOwner || len(diagnostics[0].Digest) != 64 {
				t.Fatalf("diagnostics: %#v", diagnostics)
			}
		})
	}
}

func TestInvalidShippedAndCancellationFail(t *testing.T) {
	pkg := fixture(t, Ask, "no sections", "invalid owner")
	if _, err := Load(t.Context(), pkg, Ask, nil); err == nil {
		t.Fatal("invalid shipped format accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Load(ctx, pkg, Ask, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestRestrictedPlaceholder(t *testing.T) {
	for _, tc := range []struct {
		role, body string
		valid      bool
	}{
		{ReplyWriter, "limit {{MaxClaims}}", true}, {ReplyWriter, "{{.MaxClaims}}", false}, {ReplyWriter, "{{MaxClaims}} {{Other}}", false}, {IntentInterpreter, "{{MaxClaims}}", false}, {ReplyChecker, "plain text", true},
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

// These digests pin exact migrated bodies, including JSON shapes and wording.
// Framing separators remain consumer-owned and are covered by consumer fixtures.
func TestShippedBodies(t *testing.T) {
	expected := map[string]string{
		"chat-ask":                "e82620891bbc5628744a7891839dda255cbf7ca80970f9330d6e96ea687d8a25",
		"chat-draft":              "a51fd332a7de1f92c39aa5c1cf69715f4e696ba25f71c1bf8a11403917e1f255",
		"chat-intent-interpreter": "eb38d21cb4416a042a21648c9f854e18119d14aec724946fe55fcad84b25e013",
		"chat-reply-writer":       "81207446abb140b8282b45971394ee8dc7f1fe18d199a01f12fd9bee1dbffae4",
		"chat-reply-checker":      "06829db1a24cdddeac303b6961dae37c974d70898d3ccdf96d88a974134749dd",
	}
	for role, want := range expected {
		body, err := Load(t.Context(), nil, role, nil)
		if err != nil {
			t.Fatal(err)
		}
		got := fmt.Sprintf("%x", sha256.Sum256([]byte(body.Main+"\x00"+body.Correction)))
		if got != want {
			t.Fatalf("%s migrated body digest %s, want %s", role, got, want)
		}
	}
}
