package messaging_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/messaging/ask"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

type answerModel struct {
	calls int
	err   error
	raw   string
}

func (m *answerModel) Answer(context.Context, int64, string) ([]byte, error) {
	m.calls++
	return []byte(m.raw), m.err
}

func TestAskFailureAndAttemptRules(t *testing.T) {
	for _, tc := range []struct {
		name, raw, want string
		failure         error
		calls           int
	}{
		{"provider failure", "", "Could not answer now", errors.New("private native failure"), 1},
		{"timeout", "", "Could not answer now", context.DeadlineExceeded, 1},
		{"unavailable", "", "Answers unavailable", ask.ErrUnavailable, 1},
		{"second invalid source", `{"points":[{"text":"Invented.","handles":["absent"]}]}`, "Could not answer now", nil, 2},
		{"no support", `{"points":[]}`, "No source in the current project data", nil, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := database(t)
			if _, err := db.UpsertRepo(t.Context(), store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "app"}); err != nil {
				t.Fatal(err)
			}
			model := &answerModel{raw: tc.raw, err: tc.failure}
			send := &sender{outcome: messaging.Delivery{Confirmed: true}}
			s := messaging.New(db, "test", "owner", []string{"channel"}, send, messaging.FactoryStatus{DB: db})
			s.EnableAsk(&ask.Service{Repositories: db, Collector: ask.Collector{Factory: ask.StoreReader{Reader: db}}, Model: model, Redactor: redact.New(redact.Options{})})
			at := time.Date(2026, 10, 10, 18, 0, 0, 0, time.UTC)
			m := messaging.Message{Transport: "test", ID: "one", Channel: "channel", Sender: "owner", Command: "ask", Arguments: map[string]string{"question": "What is open?", "project": "app"}}
			if _, _, err := s.Admit(t.Context(), m, at); err != nil {
				t.Fatal(err)
			}
			step(t, s, at, true)
			if model.calls != tc.calls || len(send.texts) != 1 || !strings.Contains(send.texts[0], tc.want) || strings.Contains(send.texts[0], "private native failure") {
				t.Fatalf("wrong failure handling: %v calls=%d", send.texts, model.calls)
			}
			row, err := db.ReadChatReply(t.Context(), "test", "one")
			// A delivered /ask turn keeps its question and project for local history.
			if err != nil || row.State != "confirmed" || row.Question != "What is open?" || row.ProjectOption != "app" || len(row.Arguments) != 0 {
				t.Fatalf("delivered ask turn not retained: %+v %v", row, err)
			}
			// Two abandoned attempts must never launch a third model call.
			m.ID = "abandoned"
			if _, _, err := s.Admit(t.Context(), m, at); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				claim, err := db.ClaimChatReply(t.Context(), "test", at)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := db.BeginChatAnswer(t.Context(), claim, 0, at); err != nil {
					t.Fatal(err)
				}
				if err := db.RecoverChatAnswers(t.Context(), "test", at); err != nil {
					t.Fatal(err)
				}
			}
			before := model.calls
			step(t, s, at, true)
			if model.calls != before || !strings.Contains(send.texts[len(send.texts)-1], "Could not answer now") {
				t.Fatal("exhausted row called model")
			}
		})
	}
}

func TestAskIsInHelpAndMissingQuestionNeverCallsModel(t *testing.T) {
	db := database(t)
	send := &sender{outcome: messaging.Delivery{Confirmed: true}}
	model := &answerModel{}
	s := messaging.New(db, "test", "owner", []string{"channel"}, send, messaging.FactoryStatus{DB: db})
	s.EnableAsk(&ask.Service{Model: model, Redactor: redact.New(redact.Options{})})
	at := time.Date(2026, 10, 10, 18, 0, 0, 0, time.UTC)
	for i, command := range []string{"help", "ask"} {
		if _, _, err := s.Admit(t.Context(), messaging.Message{Transport: "test", ID: command, Channel: "channel", Sender: "owner", Command: command}, at); err != nil {
			t.Fatal(err)
		}
		step(t, s, at, true)
		if (i == 0 && !strings.Contains(send.texts[i], "/ask")) || (i == 1 && !strings.Contains(send.texts[i], "8192 bytes")) {
			t.Fatal(send.texts)
		}
	}
	if model.calls != 0 {
		t.Fatal("missing question called model")
	}
}
