package messaging_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/instructionpackage"
	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/messaging/prompts"
)

type capturedNarrationModel struct {
	t           *testing.T
	checkerPath string
	calls       []string
}

func (m *capturedNarrationModel) Present(_ context.Context, _ int64, _, phase, prompt string) ([]byte, error) {
	m.calls = append(m.calls, prompt)
	if phase == "generation" {
		if err := os.WriteFile(m.checkerPath, []byte("edited checker"), 0600); err != nil {
			m.t.Fatal(err)
		}
		return []byte(`{"claims":[{"text":"Here is the result.","facts":["result"],"protected":[]}]}`), nil
	}
	return []byte(`{"findings":[{"claim":0,"support":"supported"}]}`), nil
}

func TestNarrationCapturesBothRolesBeforeGeneration(t *testing.T) {
	owner := t.TempDir()
	if err := os.Mkdir(filepath.Join(owner, "agents"), 0700); err != nil {
		t.Fatal(err)
	}
	catalogue, err := os.ReadFile("../instructionpackage/embedded/catalogue.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(owner, "catalogue.yaml"), catalogue, 0600); err != nil {
		t.Fatal(err)
	}
	checker := filepath.Join(owner, "agents", prompts.ReplyChecker+".md")
	for name, text := range map[string]string{prompts.ReplyWriter: "original writer", prompts.ReplyChecker: "original checker"} {
		if err := os.WriteFile(filepath.Join(owner, "agents", name+".md"), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	resolver, err := instructionpackage.Load(t.Context(), os.DirFS("../instructionpackage/embedded"), owner)
	if err != nil {
		t.Fatal(err)
	}
	db := database(t)
	model := &capturedNarrationModel{t: t, checkerPath: checker}
	send := &attachmentSender{sender: sender{outcome: messaging.Delivery{Confirmed: true}}}
	s := messaging.New(db, "discord", "2", []string{"3"}, send, messaging.FactoryStatus{DB: db})
	s.Instructions = resolver
	s.EnablePresentation(model, nil)
	at := time.Date(2026, 10, 11, 10, 0, 0, 0, time.UTC)
	for _, id := range []string{"first", "second"} {
		if _, ok, err := s.Admit(t.Context(), messaging.Message{Transport: "discord", ID: id, Channel: "3", Sender: "2", Command: "health"}, at); err != nil || !ok {
			t.Fatalf("admit: %v", err)
		}
		if _, err := s.Step(t.Context(), at); err != nil {
			t.Fatal(err)
		}
	}
	if len(model.calls) != 4 || !strings.HasPrefix(model.calls[0], "original writer\nSource outcome:\n") || !strings.HasPrefix(model.calls[1], "original checker\nCheck data:\n") || !strings.HasPrefix(model.calls[3], "edited checker\nCheck data:\n") {
		t.Fatalf("capture lifetime: %v", model.calls)
	}
}

// Refusing one role must prevent both narration calls, preserving the raw reply
// and leaving the durable provider budgets unspent.
type refusingChecker struct{ prompts.Resolver }

func (r refusingChecker) Capture(ctx context.Context, role string) (*instructionpackage.RoleCapture, []instructionpackage.Diagnostic, error) {
	if role == prompts.ReplyChecker {
		return nil, nil, errors.New("checker instructions unavailable")
	}
	return r.Resolver.Capture(ctx, role)
}

func TestNarrationInstructionFailurePreventsLaunch(t *testing.T) {
	resolver, err := instructionpackage.Load(t.Context(), os.DirFS("../instructionpackage/embedded"), "")
	if err != nil {
		t.Fatal(err)
	}
	db := database(t)
	model := &replyModel{}
	// Keep delivery pending so confirmation cleanup does not erase the
	// presentation state and durable call flags under inspection.
	send := &attachmentSender{sender: sender{outcome: messaging.Delivery{}}}
	s := messaging.New(db, "discord", "2", []string{"3"}, send, messaging.FactoryStatus{DB: db})
	s.Instructions = refusingChecker{resolver}
	s.EnablePresentation(model, nil)
	at := time.Date(2026, 10, 11, 10, 0, 0, 0, time.UTC)
	if _, _, err := s.Admit(t.Context(), messaging.Message{Transport: "discord", ID: "failure", Channel: "3", Sender: "2", Command: "health"}, at); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Step(t.Context(), at); err != nil {
		t.Fatal(err)
	}
	row, err := db.ReadChatReply(t.Context(), "discord", "failure")
	if err != nil {
		t.Fatal(err)
	}
	if row.State != "pending" || model.calls != 0 || row.PresentationGeneration || row.PresentationCheck || row.PresentationState != "raw_fallback" || row.ReplyAttachment == "" {
		t.Fatalf("instruction failure launched or lost source: %+v", row)
	}
}
