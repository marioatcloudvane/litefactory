package daemon

import (
	"errors"
	"github.com/cloudvane/litefactory/internal/factory"
	"github.com/cloudvane/litefactory/internal/messagingstorage"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// The owner journey drives the research ports the way the conversation's
// confirmed question and answer actions do, with the real wait claim and
// dispatch allocation.
func TestChatResearchOwnerJourney(t *testing.T) {
	f := newResearchRecoveryFixture(t)
	ctx := t.Context()
	researchRecoveryCheck(t, f.db.CompleteGraphNodeAttempt(ctx, f.attempt.ID, store.GraphCompletion{Status: store.GraphSucceeded, Outcome: "success", FinishedAt: f.at, Outputs: []store.GraphOutput{{Kind: store.GraphOutputJSON, Payload: []byte(`{"response":"ask"}`)}}}))
	researchRecoveryCheck(t, f.db.RetainGraphComment(ctx, store.GraphCommentIntent{Key: "chat-question", InstanceID: f.instance.ID, SourceAttemptID: f.attempt.ID, Node: "comment", Kind: "question", Candidate: []byte(`{"outcome":"incomplete","questions":[{"id":"Q-1","text":"Which boundary?"}]}`)}))
	_, err := f.db.EnsureReportDelivery(ctx, store.ReportDelivery{Key: "chat-question", SessionID: f.session.ID, RunID: f.run.ID, TriggerID: 20, Task: f.session.TaskRef, Destination: f.session.TaskRef, TriggerNamespace: "issue-comment", Purpose: "research-question", BotUserID: 99, Body: "Which boundary?", Marker: "chat-question"})
	researchRecoveryCheck(t, err)
	_, err = f.db.MarkReportDeliverySent(ctx, "chat-question")
	researchRecoveryCheck(t, err)
	_, err = f.db.ConfirmReportDelivery(ctx, "chat-question", 101)
	researchRecoveryCheck(t, err)
	_, err = f.db.CompleteReportDelivery(ctx, "chat-question")
	researchRecoveryCheck(t, err)
	d, task := researchClaimDaemon(t, f)
	forge := &fixtureForge{issue: github.Issue{Number: 342, State: "open"}}
	d.forge, d.log = forge, slog.New(slog.DiscardHandler)
	at := time.Now().UTC()
	s := messaging.NewWithServices(messagingstorage.New(f.db), factory.NewMessaging(f.db), "discord", "7", []string{"2"}, nil, factory.NewHealth(f.db))
	d.chat = &chatRuntime{service: s, redactor: redact.New(redact.Options{Environ: []string{"API_SECRET=chat-owner-secret"}})}
	research := chatResearch{d}
	ref := f.session.TaskRef
	question, err := research.Current(ctx, ref)
	researchRecoveryCheck(t, err)
	token := questionToken("chat-question")
	if len(token) != 12 || question.Text != "Which boundary?" || question.Token != token {
		t.Fatalf("question: %+v", question)
	}
	text := "Use the existing boundary. chat-owner-secret"
	answer, err := research.Answer(ctx, ref, "old-token", text, at)
	researchRecoveryCheck(t, err)
	if !strings.Contains(answer.Reason, "does not match") || answer.Token != token {
		t.Fatalf("stale answer: %+v", answer)
	}
	forge.issue.State = "closed"
	answer, err = research.Answer(ctx, ref, token, text, at)
	researchRecoveryCheck(t, err)
	if !strings.Contains(answer.Reason, "issue is closed") || answer.Token != token {
		t.Fatalf("closed issue: %+v", answer)
	}
	forge.issue.State = "open"
	watermark, found, err := f.db.GetWatermark(ctx, f.session.ID)
	researchRecoveryCheck(t, err)
	answer, err = research.Answer(ctx, ref, token, text, at)
	researchRecoveryCheck(t, err)
	if answer.Reason != "answer recorded" {
		t.Fatalf("answer: %+v", answer)
	}
	forge.postErr = errors.New("lost acknowledgment")
	if err := d.chatResearchNotes(ctx); err == nil {
		t.Fatal("lost acknowledgment was reported as confirmed")
	}
	if err := d.chatResearchNotes(ctx); err == nil || len(forge.posts) != 1 {
		t.Fatal("uncertain publication repeated the write")
	}
	forge.seen = append(forge.seen, forge.posts[0])
	researchRecoveryCheck(t, d.chatResearchNotes(ctx))
	researchRecoveryCheck(t, d.chatResearchNotes(ctx))
	if len(forge.posts) != 1 || !strings.Contains(forge.posts[0].Body, "Owner answered from chat: Use the existing boundary.") {
		t.Fatalf("notes: %v", forge.posts)
	}
	wait, err := f.db.GetGraphOwnerWait(ctx, f.instance.ID)
	researchRecoveryCheck(t, err)
	if wait.ReplySource != "chat" || !store.IsChatTrigger(wait.ReplyID) || wait.State != "claimed" {
		t.Fatalf("claim: %+v", wait)
	}
	if strings.Contains(wait.ReplyBody, "chat-owner-secret") || strings.Contains(forge.posts[0].Body, "chat-owner-secret") {
		t.Fatal("chat answer leaked a secret")
	}
	afterWatermark, afterFound, err := f.db.GetWatermark(ctx, f.session.ID)
	researchRecoveryCheck(t, err)
	if found != afterFound || watermark != afterWatermark {
		t.Fatalf("chat moved comment watermark: %+v -> %+v", watermark, afterWatermark)
	}
	dispatches, err := f.db.ListDueGraphDispatches(ctx, at, 10)
	researchRecoveryCheck(t, err)
	if len(dispatches) != 1 {
		t.Fatalf("dispatches: %v", dispatches)
	}
	dispatch, ok, err := f.db.ReserveGraphDispatch(ctx, dispatches[0].ID, at)
	researchRecoveryCheck(t, err)
	if !ok {
		t.Fatal("dispatch not reserved")
	}
	researchRecoveryCheck(t, claimErr(d.claimGraphReply(ctx, task, turn.Trigger{CommentID: dispatch.TriggerID()}, "research")))
	_, err = f.db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: f.instance.ID, Node: dispatch.Node, RunID: f.run.ID, StartedAt: at})
	researchRecoveryCheck(t, err)
	wait, err = f.db.GetGraphOwnerWait(ctx, f.instance.ID)
	researchRecoveryCheck(t, err)
	if wait.State != "allocated" || wait.ContinuationAttemptID == 0 {
		t.Fatalf("continuation: %+v", wait)
	}
}

func TestChatQuestionBeforePublicationHasNoToken(t *testing.T) {
	f := newResearchRecoveryFixture(t)
	researchRecoveryCheck(t, f.db.CompleteGraphNodeAttempt(t.Context(), f.attempt.ID, store.GraphCompletion{Status: store.GraphSucceeded, Outcome: "success", FinishedAt: f.at, Outputs: []store.GraphOutput{{Kind: store.GraphOutputJSON, Payload: []byte(`{"response":"ask"}`)}}}))
	researchRecoveryCheck(t, f.db.RetainGraphComment(t.Context(), store.GraphCommentIntent{Key: "not-published", InstanceID: f.instance.ID, SourceAttemptID: f.attempt.ID, Node: "comment", Kind: "question", Candidate: []byte(`{"questions":[{"text":"Which boundary?"}]}`)}))
	question, err := (chatResearch{&Daemon{db: f.db}}).Current(t.Context(), f.session.TaskRef)
	researchRecoveryCheck(t, err)
	if question.Token != "" || question.Text != "" || question.Reason != "there is no open research question" {
		t.Fatalf("unpublished question: %+v", question)
	}
}
