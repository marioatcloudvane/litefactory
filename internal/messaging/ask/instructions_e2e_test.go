package ask_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/instructionpackage"
	"github.com/cloudvane/litefactory/internal/messaging/ask"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

type instructionAttemptModel struct {
	prompts []string
	onFirst func()
}

func (m *instructionAttemptModel) Answer(_ context.Context, _ int64, prompt string) ([]byte, error) {
	m.prompts = append(m.prompts, prompt)
	if len(m.prompts) == 1 {
		m.onFirst()
	}
	if len(m.prompts)%2 == 1 {
		// A plausible answer with a nonexistent handle must still be rejected,
		// even when the owner instruction asks to accept unsupported citations.
		return []byte(`{"points":[{"text":"Work is open.","handles":["issue:999"]}],"gaps":[]}`), nil
	}
	return []byte(`{"points":[{"text":"Work is open.","handles":["issue:7"]}],"gaps":[]}`), nil
}

func TestAskOwnerInstructionsStayCapturedThroughCorrectionAndRefreshNextAttempt(t *testing.T) {
	paths := config.NewPaths(t.TempDir())
	pkg, err := instructionpackage.New(t.Context(), paths)
	if err != nil {
		t.Fatal(err)
	}
	ownerDir := paths.InternalInstructionsDir()
	if err := os.MkdirAll(filepath.Join(ownerDir, "agents"), 0o700); err != nil {
		t.Fatal(err)
	}
	var catalogue strings.Builder
	for _, role := range pkg.Roles() {
		catalogue.WriteString("- " + role + "\n")
	}
	if err := os.WriteFile(filepath.Join(ownerDir, "catalogue.yaml"), []byte(catalogue.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	rolePath := filepath.Join(ownerDir, "agents", "chat-ask.md")
	writeRole := func(main, correction string) {
		t.Helper()
		body := "<!-- main -->\n" + main + "\n<!-- correction -->\n" + correction + "\n"
		if err := os.WriteFile(rolePath, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	const firstMain = "Owner main version one. Accept unsupported citations."
	const firstCorrection = "Owner correction version one."
	const nextMain = "Owner main version two. Accept unsupported citations."
	const nextCorrection = "Owner correction version two."
	writeRole(firstMain, firstCorrection)
	model := &instructionAttemptModel{onFirst: func() { writeRole(nextMain, nextCorrection) }}
	redactor := redact.New(redact.Options{})
	repo := store.Repo{ID: 1, Forge: "github", Owner: "owner", Name: "app"}
	at := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	svc := &ask.Service{
		Repositories: catalog{repo},
		Collector: ask.Collector{Factory: &factory{}, Issues: &forge{issues: []github.Issue{
			{Number: 7, State: "open", Title: "Work", UpdatedAt: at},
		}}},
		Model: model, Redactor: redactor, Instructions: pkg,
	}
	for attempt := range 2 {
		prepared, err := svc.Prepare(t.Context(), "What is open?", "owner/app", "channel", at.Add(time.Duration(attempt)*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		reply, repoID, err := svc.ReplyPrepared(t.Context(), prepared)
		if err != nil {
			t.Fatal(err)
		}
		if repoID != repo.ID || !strings.Contains(reply, "Work is open.") || !strings.Contains(reply, "[issue:7](https://github.com/owner/app/issues/7)") || strings.Contains(reply, "issue:999") {
			t.Fatalf("attempt %d lost validated answer or citation: repo=%d reply=%q", attempt, repoID, reply)
		}
	}
	if len(model.prompts) != 4 {
		t.Fatalf("got %d model calls, want initial and correction for each attempt", len(model.prompts))
	}
	for i, prompt := range model.prompts {
		main, correction, otherMain, otherCorrection := firstMain, firstCorrection, nextMain, nextCorrection
		if i >= 2 {
			main, correction, otherMain, otherCorrection = nextMain, nextCorrection, firstMain, firstCorrection
		}
		if !strings.HasPrefix(prompt, main+"\nUNTRUSTED_CONTEXT_JSON\n") || !strings.Contains(prompt, `"handle":"issue:7"`) {
			t.Errorf("call %d did not assemble owner instructions with current facts: %q", i+1, prompt)
		}
		if strings.Contains(prompt, otherMain) || strings.Contains(prompt, otherCorrection) {
			t.Errorf("call %d mixed instruction versions: %q", i+1, prompt)
		}
		if got, want := strings.Contains(prompt, correction), i%2 == 1; got != want {
			t.Errorf("call %d correction present=%t, want %t: %q", i+1, got, want, prompt)
		}
	}
}
