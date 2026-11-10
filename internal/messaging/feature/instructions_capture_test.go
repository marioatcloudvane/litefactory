package feature

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/factorylib"
	"github.com/cloudvane/litefactory/internal/factorylib/factorylibtest"
	"github.com/cloudvane/litefactory/internal/redact"
)

type capturedDraftModel struct {
	current *factorylib.Library
	edited  *factorylib.Library
	prompts []string
}

// Answer reinstalls an edited library after the first call, mid-draft.
func (m *capturedDraftModel) Answer(_ context.Context, _ int64, prompt string) ([]byte, error) {
	m.prompts = append(m.prompts, prompt)
	if len(m.prompts) == 1 {
		m.current = m.edited
		return []byte(`{"title":"invalid","unknown":true}`), nil
	}
	return []byte(`{"needs_clarification":"Which export?"}`), nil
}

func TestDraftRetainsCapturedCorrection(t *testing.T) {
	role := factorylib.ChatDraft.Path()
	model := &capturedDraftModel{
		current: factorylibtest.With(t, map[string]string{role: "<!-- main -->\noriginal main\n<!-- correction -->\noriginal correction\n"}),
		edited:  factorylibtest.With(t, map[string]string{role: "<!-- main -->\nedited main\n<!-- correction -->\nedited correction\n"}),
	}
	resolver := factorylibtest.SourceFunc(func(context.Context) (*factorylib.Library, error) { return model.current, nil })
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
