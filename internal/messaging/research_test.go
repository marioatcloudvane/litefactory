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
	s := messaging.New(db, "test", "owner", []string{"channel"}, nil, messaging.FactoryStatus{DB: db})
	ports := &researchPorts{}
	s.EnableResearch(ports, ports)
	run := func(id, command string, args map[string]string) string {
		t.Helper()
		text, _, err := s.RunCommand(ctx, messaging.Message{Transport: "test", ID: id, Channel: "channel", Sender: "owner", Command: command, Arguments: args}, at)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		return text
	}
	for _, tc := range []struct{ id, command, project, issue, want string }{
		{"bad", "diagnose", "acme/app", "0", "positive"},
		{"unknown", "diagnose", "unknown", "1", "Unknown"},
		{"ambiguous", "diagnose", "app", "1", "acme/app"},
		{"no-task", "question", "acme/app", "1", "no factory task"},
		{"no-answer-task", "answer", "acme/app", "1", "no factory task"},
	} {
		if text := run(tc.id, tc.command, map[string]string{"project": tc.project, "issue": tc.issue}); !strings.Contains(strings.ToLower(text), strings.ToLower(tc.want)) {
			t.Fatalf("%s: %q", tc.id, text)
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
	text := run("long-question", "question", map[string]string{"project": "acme/app", "issue": "1"})
	if len(utf16.Encode([]rune(text))) > 2000 || !strings.Contains(text, "[shortened]") || !strings.Contains(text, "Token: 123456789abc") {
		t.Fatalf("long question lost binding: %q", text)
	}
	ports.fail = true
	if text := run("storage-error", "answer", map[string]string{"project": "acme/app", "issue": "1", "revision": "123456789abc", "text": "private answer"}); text != "your answer was not recorded; nothing was applied" {
		t.Fatalf("storage error: %q", text)
	}
	before := ports.calls
	if text := run("interrupted-answer", "answer", map[string]string{"project": "acme/app", "issue": "1", "revision": "old-token"}); ports.calls != before+1 || !strings.Contains(text, "could not be confirmed") || !strings.Contains(text, "123456789abc") {
		t.Fatalf("lost input guessed a result: %q", text)
	}
}

func TestDiagnosisSummaryKeepsEverySection(t *testing.T) {
	text := messaging.RenderDiagnosis(messaging.DiagnosisSummary{Findings: []string{strings.Repeat("Long finding 🐧 ", 400)}, CoverageGaps: []string{"Missing deployment evidence."}, OpenQuestions: []string{"Which boundary?"}, RepairProposed: true}, 1872)
	if len(utf16.Encode([]rune(text))) > 1872 || !strings.Contains(text, "Missing deployment evidence.") || !strings.Contains(text, "Which boundary?") || !strings.Contains(text, "only be reviewed on the issue") || !strings.Contains(text, "[shortened]") {
		t.Fatalf("summary: %q", text)
	}
}

// TestQuestionOutcomeStatus checks that the question reader assesses the status
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
			s := messaging.New(db, "test", "owner", []string{"channel"}, nil, messaging.FactoryStatus{DB: db})
			ports := &researchPorts{question: tc.question}
			s.EnableResearch(ports, ports)
			text, status, err := s.RunCommand(ctx, messaging.Message{Transport: "test", ID: "q", Channel: "channel", Sender: "owner", Command: "question", Arguments: map[string]string{"project": "acme/app", "issue": "1"}}, at)
			if err != nil || string(status) != tc.want {
				t.Fatalf("outcome status: %q %v; want %q (text %q)", status, err, tc.want, text)
			}
		})
	}
}
