package daemon

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/doctor"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

type chatDoctorFixture struct {
	calls   int
	busy    bool
	sources []string
	started chan struct{}
}

func (f *chatDoctorFixture) Diagnose(ctx context.Context, r doctor.DiagnoseRequest) (doctor.Conversation, error) {
	f.calls++
	f.sources = append(f.sources, r.SourceID)
	if f.started != nil {
		close(f.started)
		<-ctx.Done()
		return doctor.Conversation{}, ctx.Err()
	}
	if f.busy {
		return doctor.Conversation{}, store.ErrDoctorBusy
	}
	return doctor.Conversation{Diagnosis: &doctor.Diagnosis{Findings: []doctor.Finding{{Explanation: "The question is waiting for its owner."}}, CoverageGaps: []string{"No deployment evidence."}, Questions: []string{"Which boundary?"}, Proposal: []doctor.RepairStep{{Change: "private repair details"}}}}, nil
}

// The owner journey drives the research ports the way the conversation's
// confirmed diagnose, question and answer actions do, with the real wait claim,
// dispatch allocation and a read-only doctor seam.
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
	forge := &doctorTestForge{issue: github.Issue{Number: 342, State: "open"}}
	d.forge, d.log = forge, slog.New(slog.DiscardHandler)
	d.doctor.pool = newPool(1)
	at := time.Now().UTC()
	s := messaging.New(f.db, "discord", "7", []string{"2"}, nil, messaging.FactoryStatus{DB: f.db})
	fake := &chatDoctorFixture{}
	d.chat = &chatRuntime{service: s, diagnoser: fake, redactor: redact.New(redact.Options{Environ: []string{"API_SECRET=chat-owner-secret"}}), notes: map[string]string{}}
	research := chatResearch{d}
	ref := f.session.TaskRef
	// diagnose defers a claimed request row and queues its private note, as the
	// diagnose handler does; deliver confirms the saved completion.
	diagnose := func(id string) {
		t.Helper()
		_, e := f.db.AdmitChatReply(ctx, store.ChatReply{Transport: "discord", MessageID: id, ChannelRef: "2", Command: "diagnose", CreatedAt: at})
		researchRecoveryCheck(t, e)
		_, e = f.db.ClaimChatReply(ctx, "discord", at)
		researchRecoveryCheck(t, e)
		researchRecoveryCheck(t, f.db.DeferChatDiagnosis(ctx, "discord", id, ref, at))
		researchRecoveryCheck(t, research.Queue(ctx, messaging.DiagnosisRequest{Target: ref, Note: "private owner note", SourceID: "chat:discord:" + id}))
	}
	deliver := func(id string) store.ChatReply {
		t.Helper()
		claim, e := f.db.ClaimChatReply(ctx, "discord", time.Now().Add(time.Second))
		researchRecoveryCheck(t, e)
		if claim.MessageID != id {
			t.Fatalf("claimed %s, want %s", claim.MessageID, id)
		}
		researchRecoveryCheck(t, f.db.FinishChatReply(ctx, claim, false, time.Now()))
		return claim
	}
	diagnose("10")
	if !d.doctor.pool.try() {
		t.Fatal("slot unexpectedly busy")
	}
	d.dispatchChatDiagnosis(ctx, at)
	if fake.calls != 0 {
		t.Fatal("diagnosis ignored doctor slot")
	}
	d.doctor.pool.done()
	fake.busy = true
	d.dispatchChatDiagnosis(ctx, at)
	d.doctor.pool.wait()
	r, err := f.db.ReadChatReply(ctx, "discord", "10")
	researchRecoveryCheck(t, err)
	if r.State != "working" || r.AnswerAttempts != 0 {
		t.Fatalf("busy diagnosis: %+v", r)
	}
	fake.busy = false
	d.dispatchChatDiagnosis(ctx, at)
	d.doctor.pool.wait()
	result := deliver("10").ReplyText
	if len(forge.posts) != 0 || !strings.Contains(result, "Coverage gaps") || !strings.Contains(result, "Open questions") || !strings.Contains(result, "only be reviewed on the issue") || strings.Contains(result, "private repair details") {
		t.Fatalf("diagnosis writes or rendering: %v %q", forge.posts, result)
	}
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
	researchRecoveryCheck(t, d.claimGraphReply(ctx, task, turn.Trigger{CommentID: dispatch.TriggerID()}, "research"))
	_, err = f.db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: f.instance.ID, Node: dispatch.Node, RunID: f.run.ID, Limit: 3, StartedAt: at})
	researchRecoveryCheck(t, err)
	wait, err = f.db.GetGraphOwnerWait(ctx, f.instance.ID)
	researchRecoveryCheck(t, err)
	if wait.State != "allocated" || wait.ContinuationAttemptID == 0 {
		t.Fatalf("continuation: %+v", wait)
	}
	// A stopped provider invocation keeps its durable target, but not its raw
	// note. A restarted daemon uses one restart retry.
	diagnose("14")
	fake.started = make(chan struct{})
	interrupted, cancel := context.WithCancel(ctx)
	d.dispatchChatDiagnosis(interrupted, at)
	<-fake.started
	cancel()
	d.doctor.pool.wait()
	r, err = f.db.ReadChatReply(ctx, "discord", "14")
	researchRecoveryCheck(t, err)
	if r.State != "working" || r.AnswerAttempts != 1 {
		t.Fatalf("interrupted diagnosis: %+v", r)
	}
	fake.started = nil
	d.chat.service = messaging.New(f.db, "discord", "7", []string{"2"}, nil, messaging.FactoryStatus{DB: f.db})
	researchRecoveryCheck(t, f.db.RecoverChatDiagnoses(ctx, "discord", at))
	d.dispatchChatDiagnosis(ctx, at)
	d.doctor.pool.wait()
	if fake.sources[len(fake.sources)-1] != "chat:discord:14:retry" || len(forge.posts) != 1 {
		t.Fatalf("restart duplicated an effect: %v %v", fake.sources, forge.posts)
	}
	deliver("14")
	r, err = f.db.ReadChatReply(ctx, "discord", "14")
	researchRecoveryCheck(t, err)
	if r.State != "confirmed" || r.TargetRef != "" {
		t.Fatalf("restart delivery: %+v", r)
	}
}

func TestRecoveredChatDiagnosisWithoutDoctor(t *testing.T) {
	d, _, _ := doctorDaemonFixture(t)
	d.doctor.service = nil
	ctx, at := t.Context(), time.Now().UTC()
	s := messaging.New(d.db, "discord", "7", []string{"2"}, nil, messaging.FactoryStatus{DB: d.db})
	d.chat = &chatRuntime{service: s, redactor: redact.New(redact.Options{}), notes: map[string]string{}}
	_, err := d.db.AdmitChatReply(ctx, store.ChatReply{Transport: "discord", MessageID: "1", ChannelRef: "2", Command: "diagnose", CreatedAt: at})
	researchRecoveryCheck(t, err)
	_, err = d.db.ClaimChatReply(ctx, "discord", at)
	researchRecoveryCheck(t, err)
	researchRecoveryCheck(t, d.db.DeferChatDiagnosis(ctx, "discord", "1", "github:acme/api#406", at))
	d.dispatchChatDiagnosis(ctx, at)
	d.doctor.pool.wait()
	r, err := d.db.ReadChatReply(ctx, "discord", "1")
	researchRecoveryCheck(t, err)
	if r.State != "pending" || r.ReplyText != "diagnosis failed" {
		t.Fatalf("unavailable diagnosis: %+v", r)
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
