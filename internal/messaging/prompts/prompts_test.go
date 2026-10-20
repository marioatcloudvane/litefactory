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
		"chat-ask":                "59a708928dbc59d595cbfb5a60d7c0ee5086249ce05cb81ddc92095a29e2a1b9",
		"chat-draft":              "124e691a2d73cecc310403d59114772a0b05d3e6e60b8205616a2656940e322d",
		"chat-intent-interpreter": "6daef9a24d7dbf99ece755d1d454bfa0d64587efbf74f4ae24754e71cba736eb",
		"chat-reply-writer":       "9983b7b59c127c3b89ae48df6c87b61ba4d2df8242b52a3f925bcc18011076e6",
		"chat-reply-checker":      "b00c055570f37bca1e3903ebe9c9ce32ab9ae5bfbef91d94f7250d8bdedecdc2",
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
