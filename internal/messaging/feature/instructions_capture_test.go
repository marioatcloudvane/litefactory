package feature

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/instructionpackage"
	"github.com/cloudvane/litefactory/internal/messaging/prompts"
	"github.com/cloudvane/litefactory/internal/redact"
)

type capturedDraftModel struct {
	t       *testing.T
	path    string
	prompts []string
}

func (m *capturedDraftModel) Answer(_ context.Context, _ int64, prompt string) ([]byte, error) {
	m.prompts = append(m.prompts, prompt)
	if len(m.prompts) == 1 {
		if err := os.WriteFile(m.path, []byte("<!-- main -->\nedited main\n<!-- correction -->\nedited correction\n"), 0600); err != nil {
			m.t.Fatal(err)
		}
		return []byte(`{"title":"invalid","unknown":true}`), nil
	}
	return []byte(`{"needs_clarification":"Which export?"}`), nil
}

func TestDraftRetainsCapturedCorrection(t *testing.T) {
	owner := t.TempDir()
	if err := os.Mkdir(filepath.Join(owner, "agents"), 0700); err != nil {
		t.Fatal(err)
	}
	catalogue, err := os.ReadFile("../../instructionpackage/embedded/catalogue.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(owner, "catalogue.yaml"), catalogue, 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(owner, "agents", prompts.Draft+".md")
	if err = os.WriteFile(path, []byte("<!-- main -->\noriginal main\n<!-- correction -->\noriginal correction\n"), 0600); err != nil {
		t.Fatal(err)
	}
	resolver, err := instructionpackage.Load(t.Context(), os.DirFS("../../instructionpackage/embedded"), owner)
	if err != nil {
		t.Fatal(err)
	}
	model := &capturedDraftModel{t: t, path: path}
	mem := &previewMemory{}
	d := Drafter{Instructions: resolver, Repositories: catalog{}, Model: model, Store: mem, Redactor: redact.New(redact.Options{Environ: []string{}})}
	for _, id := range []string{"first", "second"} {
		text, _, err := d.Reply(t.Context(), "test", "channel", id, "app", "idea", time.Now())
		if err != nil || !strings.Contains(text, "Please clarify") {
			t.Fatalf("reply: %s %v", text, err)
		}
	}
	if len(model.prompts) != 3 || !strings.HasPrefix(model.prompts[0], "original main\nUNTRUSTED_CONTEXT_JSON\n") || !strings.HasSuffix(model.prompts[1], "\noriginal correction") || strings.Contains(model.prompts[1], "edited") || !strings.HasPrefix(model.prompts[2], "edited main\nUNTRUSTED_CONTEXT_JSON\n") || mem.saves != 0 {
		t.Fatalf("capture or validator changed: %v", model.prompts)
	}
}
