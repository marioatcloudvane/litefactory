package instructionpackage

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
)

type captureSourceFunc func(context.Context, string) (*RoleCapture, []Diagnostic, error)

func (f captureSourceFunc) Capture(ctx context.Context, role string) (*RoleCapture, []Diagnostic, error) {
	return f(ctx, role)
}

func TestCaptureRoleReportsFallbackMetadataWithoutContent(t *testing.T) {
	paths := config.NewPaths(t.TempDir())
	if err := os.MkdirAll(paths.InternalInstructionsDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	secret := "PRIVATE OWNER INSTRUCTION CONTENT"
	if err := os.WriteFile(filepath.Join(paths.InternalInstructionsDir(), CataloguePath), []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	capture, err := CaptureRole(t.Context(), nil, paths, slog.New(slog.NewTextHandler(&logs, nil)), "mode-classifier")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(capture.Instructions()), "Classify the authorized instruction") {
		t.Fatal("did not select complete shipped fallback")
	}
	text := logs.String()
	for _, field := range []string{"instruction package fallback", "file=", "category=", "selected_source="} {
		if !strings.Contains(text, field) {
			t.Errorf("missing diagnostic metadata %q in %q", field, text)
		}
	}
	if strings.Contains(text, secret) || strings.Contains(text, string(capture.Instructions())) {
		t.Fatal("diagnostic leaked instruction content")
	}
}

func TestCaptureRoleRejectsSourceFailureAndCancellation(t *testing.T) {
	paths := config.NewPaths(t.TempDir())
	failure := errors.New("source unavailable")
	source := captureSourceFunc(func(context.Context, string) (*RoleCapture, []Diagnostic, error) { return nil, nil, failure })
	if _, err := CaptureRole(t.Context(), source, paths, nil, "mode-classifier"); !errors.Is(err, failure) {
		t.Fatalf("source failure = %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	called := false
	source = captureSourceFunc(func(context.Context, string) (*RoleCapture, []Diagnostic, error) { called = true; return nil, nil, nil })
	if _, err := CaptureRole(ctx, source, paths, nil, "mode-classifier"); !errors.Is(err, context.Canceled) || called {
		t.Fatalf("canceled source: error=%v called=%v", err, called)
	}
	if _, err := CaptureRole(t.Context(), source, paths, nil, "mode-classifier"); err == nil {
		t.Fatal("accepted empty source capture")
	}
	if _, err := CaptureRole(t.Context(), nil, paths, nil, "unknown-role"); err == nil {
		t.Fatal("accepted unknown role")
	}
}
