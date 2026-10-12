package messaging_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/store"
)

type researchPorts struct {
	calls    int
	fail     bool
	request  messaging.DiagnosisRequest
	question messaging.Question
}

func (p *researchPorts) Queue(_ context.Context, r messaging.DiagnosisRequest) error {
	p.calls++
	p.request = r
	return nil
}
func (p *researchPorts) Current(context.Context, string) (messaging.Question, error) {
	p.calls++
	if p.question.Text != "" || p.question.Reason != "" {
		return p.question, nil
	}
	return messaging.Question{Text: "Which boundary?", Token: "123456789abc"}, nil
}
func (p *researchPorts) Answer(context.Context, string, string, string, time.Time) (messaging.Question, error) {
	p.calls++
	if p.fail {
		return messaging.Question{}, errors.New("storage failed")
	}
	return messaging.Question{Reason: "answer recorded"}, nil
}

func TestResearchCommandResolutionAndPrivateInput(t *testing.T) {
	db := database(t)
	ctx, at := t.Context(), time.Now().UTC()
	for _, owner := range []string{"acme", "other"} {
		_, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: owner, Name: "app", BaseBranch: "main"})
		if err != nil {
			t.Fatal(err)
		}
	}
	send := &sender{outcome: messaging.Delivery{Confirmed: true}}
	s := messaging.New(db, "test", "owner", []string{"channel"}, send, messaging.FactoryStatus{DB: db})
	ports := &researchPorts{}
	s.EnableResearch(ports, ports)
	for _, tc := range []struct{ id, command, project, issue, want string }{
		{"bad", "diagnose", "acme/app", "0", "positive"},
		{"unknown", "diagnose", "unknown", "1", "Unknown"},
		{"ambiguous", "diagnose", "app", "1", "acme/app"},
		{"no-task", "question", "acme/app", "1", "no factory task"},
		{"no-answer-task", "answer", "acme/app", "1", "no factory task"},
	} {
		_, _, err := s.Admit(ctx, messaging.Message{Transport: "test", ID: tc.id, Channel: "channel", Sender: "owner", Command: tc.command, Arguments: map[string]string{"project": tc.project, "issue": tc.issue}}, at)
		if err != nil {
			t.Fatal(err)
		}
		step(t, s, at, true)
		if !strings.Contains(strings.ToLower(send.texts[len(send.texts)-1]), strings.ToLower(tc.want)) {
			t.Fatalf("%s: %v", tc.id, send.texts)
		}
	}
	if ports.calls != 0 {
		t.Fatal("refused command called a port")
	}
	repos, err := db.ListRepos(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, repo := range repos {
		if repo.Owner == "acme" {
			if _, err = db.GetOrCreateSession(ctx, repo, 1); err != nil {
				t.Fatal(err)
			}
		}
	}
	ports.question = messaging.Question{Text: strings.Repeat("Long question 🐧 ", 400), Token: "123456789abc"}
	_, _, err = s.Admit(ctx, messaging.Message{Transport: "test", ID: "long-question", Channel: "channel", Sender: "owner", Command: "question", Arguments: map[string]string{"project": "acme/app", "issue": "1"}}, at)
	if err != nil {
		t.Fatal(err)
	}
	step(t, s, at, true)
	text := send.texts[len(send.texts)-1]
	if len(utf16.Encode([]rune(text))) > 2000 || !strings.Contains(text, "[shortened]") || !strings.Contains(text, "Token: 123456789abc") {
		t.Fatalf("long question lost binding: %q", text)
	}
	ports.fail = true
	_, _, err = s.Admit(ctx, messaging.Message{Transport: "test", ID: "storage-error", Channel: "channel", Sender: "owner", Command: "answer", Arguments: map[string]string{"project": "acme/app", "issue": "1", "revision": "123456789abc", "text": "private answer"}}, at)
	if err != nil {
		t.Fatal(err)
	}
	answerRow, err := db.ReadChatReply(ctx, "test", "storage-error")
	if err != nil || answerRow.Arguments["text"] != "" {
		t.Fatalf("raw answer persisted: %+v %v", answerRow, err)
	}
	step(t, s, at, true)
	if send.texts[len(send.texts)-1] != "your answer was not recorded; nothing was applied" {
		t.Fatalf("storage error: %v", send.texts)
	}
	_, _, err = s.Admit(ctx, messaging.Message{Transport: "test", ID: "interrupted-answer", Channel: "channel", Sender: "owner", Command: "answer", Arguments: map[string]string{"project": "acme/app", "issue": "1", "revision": "old-token", "text": "private answer"}}, at)
	if err != nil {
		t.Fatal(err)
	}
	restarted := messaging.New(db, "test", "owner", []string{"channel"}, send, messaging.FactoryStatus{DB: db})
	restarted.EnableResearch(ports, ports)
	before := ports.calls
	step(t, restarted, at, true)
	if ports.calls != before+1 || !strings.Contains(send.texts[len(send.texts)-1], "could not be confirmed") || !strings.Contains(send.texts[len(send.texts)-1], "123456789abc") {
		t.Fatalf("lost input guessed a result: %v", send.texts)
	}
	_, _, err = s.Admit(ctx, messaging.Message{Transport: "test", ID: "diagnosis", Channel: "channel", Sender: "owner", Command: "diagnose", Arguments: map[string]string{"project": "acme/app", "issue": "1", "note": "private owner note"}}, at)
	if err != nil {
		t.Fatal(err)
	}
	r, err := db.ReadChatReply(ctx, "test", "diagnosis")
	if err != nil || r.Arguments["note"] != "" {
		t.Fatalf("persisted note: %+v %v", r, err)
	}
	step(t, s, at, true)
	r, err = db.ReadChatReply(ctx, "test", "diagnosis")
	if err != nil || r.State != "working" || r.TargetRef != "github:acme/app#1" || len(r.Arguments) != 0 {
		t.Fatalf("deferred diagnosis: %+v %v", r, err)
	}
	if ports.request.Note != "private owner note" || ports.request.SourceID != "chat:test:diagnosis" {
		t.Fatalf("diagnosis request: %+v", ports.request)
	}
}

func TestDiagnosisSummaryKeepsEverySection(t *testing.T) {
	text := messaging.RenderDiagnosis(messaging.DiagnosisSummary{Findings: []string{strings.Repeat("Long finding 🐧 ", 400)}, CoverageGaps: []string{"Missing deployment evidence."}, OpenQuestions: []string{"Which boundary?"}, RepairProposed: true}, 1872)
	if len(utf16.Encode([]rune(text))) > 1872 || !strings.Contains(text, "Missing deployment evidence.") || !strings.Contains(text, "Which boundary?") || !strings.Contains(text, "only be reviewed on the issue") || !strings.Contains(text, "[shortened]") {
		t.Fatalf("summary: %q", text)
	}
}

// TestQuestionOutcomeStatus checks that /question keeps the status assessed
// for the current research question: refusal when none is open.
func TestQuestionOutcomeStatus(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		question messaging.Question
		want     string
	}{
		{"no open question", messaging.Question{Reason: "there is no open research question"}, "refusal"},
		{"open question", messaging.Question{Text: "Which boundary?", Token: "123456789abc"}, "success"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			db := database(t)
			ctx, at := t.Context(), time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
			repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "app", BaseBranch: "main"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.GetOrCreateSession(ctx, repo, 1); err != nil {
				t.Fatal(err)
			}
			// An unconfirmed delivery keeps the row and its outcome status.
			send := &sender{}
			s := messaging.New(db, "test", "owner", []string{"channel"}, send, messaging.FactoryStatus{DB: db})
			ports := &researchPorts{question: tc.question}
			s.EnableResearch(ports, ports)
			if _, _, err = s.Admit(ctx, messaging.Message{Transport: "test", ID: "q", Channel: "channel", Sender: "owner", Command: "question", Arguments: map[string]string{"project": "acme/app", "issue": "1"}}, at); err != nil {
				t.Fatal(err)
			}
			step(t, s, at, true)
			r, err := db.ReadChatReply(ctx, "test", "q")
			if err != nil || r.OutcomeStatus != tc.want {
				t.Fatalf("outcome status: %q %v; want %q (texts %v)", r.OutcomeStatus, err, tc.want, send.texts)
			}
		})
	}
}
