package messaging_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/messaging/presentation"
	"github.com/cloudvane/litefactory/internal/store"
)

type replyModel struct {
	calls  int
	cancel context.CancelFunc
}

func TestRegistryRepliesAllCrossPresentationBoundary(t *testing.T) {
	db := database(t)
	at := time.Date(2026, 10, 11, 10, 0, 0, 0, time.UTC)
	model := &replyModel{}
	send := &attachmentSender{sender: sender{outcome: messaging.Delivery{Confirmed: true}}}
	s := messaging.New(db, "discord", "2", []string{"3"}, send, messaging.FactoryStatus{DB: db})
	s.EnablePresentation(model, nil)
	s.EnableAsk(nil)
	s.EnableFeatures(nil, nil)
	s.EnableResearch(nil, nil)
	names := s.Commands()
	if len(names) != 11 {
		t.Fatalf("registry coverage changed: %v", names)
	}
	for _, command := range names {
		before := send.richCalls
		message := messaging.Message{Transport: "discord", ID: command, Channel: "3", Sender: "2", Command: command}
		if _, inserted, err := s.Admit(t.Context(), message, at); err != nil || !inserted {
			t.Fatalf("%s admission: %v", command, err)
		}
		if more, err := s.Step(t.Context(), at); err != nil || !more {
			t.Fatalf("%s reply: %v", command, err)
		}
		if send.richCalls != before+1 {
			t.Fatalf("%s bypassed presentation", command)
		}
	}
}

type pendingDiagnosis struct {
	db    *store.DB
	at    time.Time
	calls int
}

func (d *pendingDiagnosis) Queue(ctx context.Context, request messaging.DiagnosisRequest) error {
	d.calls++
	id := strings.TrimPrefix(request.SourceID, "chat:discord:")
	return d.db.CompleteChatDiagnosisResult(ctx, "discord", id, 0, "Diagnosis result: 12 findings.", d.at, "success")
}

func TestAsynchronousSavedResultUsesPresentationWithoutRequeue(t *testing.T) {
	db := database(t)
	at := time.Date(2026, 10, 11, 10, 0, 0, 0, time.UTC)
	if _, err := db.UpsertRepo(t.Context(), store.Repo{Path: t.TempDir(), Forge: store.ForgeGitHub, Owner: "acme", Name: "app", BaseBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	diagnosis := &pendingDiagnosis{db: db, at: at}
	model := &replyModel{}
	send := &attachmentSender{sender: sender{outcome: messaging.Delivery{Confirmed: true}}}
	s := messaging.New(db, "discord", "2", []string{"3"}, send, nil)
	s.EnablePresentation(model, nil)
	s.EnableResearch(diagnosis, nil)
	if _, _, err := s.Admit(t.Context(), messaging.Message{Transport: "discord", ID: "100", Channel: "3", Sender: "2", Command: "diagnose", Arguments: map[string]string{"project": "acme/app", "issue": "12"}}, at); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Step(t.Context(), at); err != nil {
		t.Fatal(err)
	}
	if model.calls != 0 {
		t.Fatal("deferred work narrated before completion")
	}
	if _, err := s.Step(t.Context(), at); err != nil {
		t.Fatal(err)
	}
	if diagnosis.calls != 1 || model.calls != 2 || send.richCalls != 1 {
		t.Fatal("saved diagnosis bypassed presentation or repeated command")
	}
}

type timedOutModel struct {
	t     *testing.T
	calls int
}

func (m *timedOutModel) Present(ctx context.Context, _ int64, _, _, prompt string) ([]byte, error) {
	m.calls++
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > time.Second {
		m.t.Error("earlier caller deadline lost")
	}
	if !strings.Contains(prompt, `"language":"English"`) {
		m.t.Error("missing English default")
	}
	return nil, context.DeadlineExceeded
}

func TestProviderDeadlineFailureSelectsFixedRawReply(t *testing.T) {
	db := database(t)
	at := time.Date(2026, 10, 11, 10, 0, 0, 0, time.UTC)
	model := &timedOutModel{t: t}
	send := &attachmentSender{sender: sender{outcome: messaging.Delivery{}}}
	s := messaging.New(db, "discord", "2", []string{"3"}, send, messaging.FactoryStatus{DB: db})
	s.EnablePresentation(model, nil)
	if _, _, err := s.Admit(t.Context(), messaging.Message{Transport: "discord", ID: "100", Channel: "3", Sender: "2", Command: "health"}, at); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if _, err := s.Step(ctx, at); err != nil {
		t.Fatal(err)
	}
	row, err := db.ReadChatReply(t.Context(), "discord", "100")
	if err != nil {
		t.Fatal(err)
	}
	var source presentation.Outcome
	if err = json.Unmarshal([]byte(row.PresentationOutcome), &source); err != nil {
		t.Fatal(err)
	}
	if model.calls != 1 || row.PresentationCheck || row.PresentationState != "raw_fallback" || row.ReplyAttachment != source.RawText || !strings.Contains(row.ReplyText, "Raw result.") {
		t.Fatalf("deadline lost result: %+v", row)
	}
}

func (m *replyModel) Present(_ context.Context, _ int64, _, phase, _ string) ([]byte, error) {
	m.calls++
	if m.cancel != nil {
		m.cancel()
		return nil, context.Canceled
	}
	if phase == "generation" {
		return []byte(`{"claims":[{"text":"Here is the result.","facts":["result"],"protected":[]}]}`), nil
	}
	return []byte(`{"findings":[{"claim":0,"support":"supported"}]}`), nil
}

type attachmentSender struct {
	sender
	richCalls int
	rejected  bool
}

func (s *attachmentSender) DeliverReply(_ context.Context, _ messaging.Target, p presentation.Payload) messaging.Delivery {
	s.richCalls++
	s.texts = append(s.texts, p.Content)
	if s.rejected {
		return messaging.Delivery{AttachmentRejected: true}
	}
	return s.outcome
}

func TestInterruptedPresentationNeverRepeatsCommandOrGeneration(t *testing.T) {
	db := database(t)
	at := time.Date(2026, 10, 11, 10, 0, 0, 0, time.UTC)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	model := &replyModel{cancel: cancel}
	send := &attachmentSender{sender: sender{outcome: messaging.Delivery{Confirmed: true}}}
	effects := 0
	service := messaging.New(db, "discord", "2", []string{"3"}, send, nil)
	service.EnablePresentation(model, nil)
	service.Register("custom", func(context.Context, messaging.Message, time.Time) (string, error) {
		effects++
		return "Saved result.", nil
	})
	if _, inserted, err := service.Admit(t.Context(), messaging.Message{Transport: "discord", ID: "100", Channel: "3", Sender: "2", Command: "custom"}, at); err != nil || !inserted {
		t.Fatalf("admit: %t %v", inserted, err)
	}
	if _, err := service.Step(ctx, at); err == nil {
		t.Fatal("shutdown did not preserve pending work")
	}
	row, err := db.ReadChatReply(t.Context(), "discord", "100")
	if err != nil {
		t.Fatal(err)
	}
	if row.PresentationOutcome == "" || !row.PresentationGeneration || row.ReplyText != "Saved result." {
		t.Fatalf("source lost: %+v", row)
	}
	model.cancel = nil
	restarted := messaging.New(db, "discord", "2", []string{"3"}, send, nil)
	restarted.EnablePresentation(model, nil)
	restarted.Register("custom", func(context.Context, messaging.Message, time.Time) (string, error) {
		effects++
		return "Repeated effect!", nil
	})
	if _, err := restarted.Step(t.Context(), at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if effects != 1 || model.calls != 1 || send.richCalls != 1 {
		t.Fatalf("repeated work: effects=%d calls=%d sends=%d", effects, model.calls, send.richCalls)
	}
}

func TestAttachmentRejectionPersistsInlineFallback(t *testing.T) {
	db := database(t)
	at := time.Date(2026, 10, 11, 10, 0, 0, 0, time.UTC)
	model := &replyModel{}
	send := &attachmentSender{sender: sender{outcome: messaging.Delivery{Confirmed: true}}, rejected: true}
	service := messaging.New(db, "discord", "2", []string{"3"}, send, nil)
	service.EnablePresentation(model, nil)
	effects := 0
	service.Register("custom", func(context.Context, messaging.Message, time.Time) (string, error) {
		effects++
		return "Saved result.", nil
	})
	if _, _, err := service.Admit(t.Context(), messaging.Message{Transport: "discord", ID: "100", Channel: "3", Sender: "2", Command: "custom"}, at); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Step(t.Context(), at); err != nil {
		t.Fatal(err)
	}
	row, err := db.ReadChatReply(t.Context(), "discord", "100")
	if err != nil {
		t.Fatal(err)
	}
	if row.PresentationState != "inline" || row.ReplyAttachment != "" || row.ReplyText != "Saved result.\n\nRaw result." {
		t.Fatalf("fallback not durable: %+v", row)
	}
	if _, err := service.Step(t.Context(), row.NextAttemptAt); err != nil {
		t.Fatal(err)
	}
	if effects != 1 || model.calls != 2 || send.richCalls != 1 || send.texts[len(send.texts)-1] != row.ReplyText {
		t.Fatal("fallback repeated effects or narration")
	}
}
