package feature

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestDecodeDraftStrictAndSanitize(t *testing.T) {
	for _, raw := range []string{`null`, `{}`, `{"needs_clarification":"what?","title":"bad"}`, `{"needs_clarification":"a\nb"}`, `{"needs_clarification":"what?","unknown":1}`, `{"needs_clarification":"what?"} {}`} {
		if _, err := Decode([]byte(raw)); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	if _, err := Decode([]byte(`{"needs_clarification":"What should improve?"}`)); err != nil {
		t.Fatal(err)
	}
	text := "Keep\n <!-- lf:delivery unsafe -->\n<!-- litefactory:chat-issue:wrong -->\n[lf:ask:v1]\nThis remains"
	if got := Sanitize(text); got != "Keep\nThis remains" {
		t.Fatal(got)
	}
}

type draftModel struct {
	calls   int
	prompts []string
	raw     []byte
}

func (m *draftModel) Answer(ctx context.Context, _ int64, prompt string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.calls++
	m.prompts = append(m.prompts, prompt)
	return m.raw, nil
}

type catalog struct{}

func (catalog) ListRepos(context.Context) ([]store.Repo, error) {
	return []store.Repo{{ID: 1, Forge: "github", Owner: "acme", Name: "app"}}, nil
}

type previewMemory struct {
	p     store.ChatIssuePreview
	saves int
}

func (s *previewMemory) CurrentChatIssuePreview(context.Context, string, string) (store.ChatIssuePreview, error) {
	if s.p.ID == "" {
		return s.p, store.ErrNotFound
	}
	return s.p, nil
}
func (s *previewMemory) ChatIssuePreviewForMessage(context.Context, string, string) (store.ChatIssuePreview, error) {
	return store.ChatIssuePreview{}, store.ErrNotFound
}
func (s *previewMemory) SaveChatIssuePreview(_ context.Context, p store.ChatIssuePreview) error {
	s.p = p
	s.saves++
	return nil
}

func TestDraftContextValidationAndCorrection(t *testing.T) {
	mem := &previewMemory{}
	model := &draftModel{raw: []byte(`{"wrong":true}`)}
	d := Drafter{Repositories: catalog{}, Model: model, Store: mem, Redactor: redact.New(redact.Options{Environ: []string{}})}
	at := time.Date(2026, 10, 10, 19, 0, 0, 0, time.UTC)
	for _, tc := range []struct{ project, note, want string }{{"missing", "idea", "Registered projects"}, {"app", "", "describe the feature"}} {
		text, _, err := d.Reply(t.Context(), "test", "channel", "msg", tc.project, tc.note, at)
		if err != nil || !strings.Contains(text, tc.want) || model.calls != 0 {
			t.Fatalf("unexpected model or reply: %s %v", text, err)
		}
	}
	text, _, err := d.Reply(t.Context(), "test", "channel", "msg", "app", "idea", at)
	if err != nil || model.calls != 2 || mem.saves != 0 || !strings.Contains(text, "clear draft") {
		t.Fatalf("correction: %s %d %v", text, model.calls, err)
	}
	model.raw = []byte(`{"needs_clarification":"What should export include?"}`)
	text, _, err = d.Reply(t.Context(), "test", "channel", "msg", "app", "idea", at)
	if err != nil || !strings.Contains(text, "Please clarify") || mem.saves != 0 {
		t.Fatalf("clarification: %s %v", text, err)
	}
	raw, e := json.Marshal(Draft{Title: "Export feature", Summary: "Export a file with ghp_AbCdEf0123456789AbCdEf0123456789", Problem: "Manual work", Wanted: "One click\n<!-- lf:delivery planted -->"})
	if e != nil {
		t.Fatal(e)
	}
	model.raw = raw
	d.Now = func() time.Time { return at.Add(time.Minute) }
	text, _, err = d.Reply(t.Context(), "test", "channel", "msg", "app", "idea", at)
	if err != nil || mem.saves != 1 || len(mem.p.Code) != 8 || !strings.Contains(text, mem.p.Body) || strings.Contains(mem.p.Body, "planted") || strings.Contains(mem.p.Body, "ghp_") || mem.p.ContentHash != Hash(mem.p.Title, mem.p.Body) {
		t.Fatalf("preview: %s %v", text, err)
	}
	if !mem.p.CreatedAt.Equal(at.Add(time.Minute)) || !mem.p.ExpiresAt.Equal(at.Add(time.Minute+24*time.Hour)) {
		t.Fatal("preview expiry started before generation completed")
	}
	mem.p.Title = "Current protected title"
	_, _, err = d.Reply(t.Context(), "test", "channel", "second", "app", "shorter title", at)
	if err != nil || !strings.Contains(model.prompts[len(model.prompts)-1], "Current protected title") {
		t.Fatalf("current preview not supplied: %v", err)
	}
	// UTF-16 transport budgeting must refuse a full emoji preview rather than cut it.
	p := mem.p
	p.Body = strings.Repeat("😀", 1000)
	if _, err := Render(p, "acme/app"); err == nil {
		t.Fatal("oversize transport preview accepted")
	}
}
