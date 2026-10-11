package daemon

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/doctor"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/messaging/discord"
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

// The owner journey uses signed Discord intake, the durable sender, the real
// wait claim and dispatch allocation, and a read-only doctor seam.
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
	var replyMu sync.Mutex
	var delivered []string
	replies := func() []string { replyMu.Lock(); defer replyMu.Unlock(); return append([]string(nil), delivered...) }
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		replyMu.Lock()
		delivered = append(delivered, body.Content)
		replyMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"id":"900"}`))
		if err != nil {
			t.Error(err)
		}
	}))
	defer api.Close()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	researchRecoveryCheck(t, err)
	at := time.Now().UTC()
	a := discord.New(discord.Options{ApplicationID: "app", PublicKey: pub, APIURL: api.URL, Client: api.Client(), Now: func() time.Time { return at }})
	s := messaging.New(f.db, "discord", "7", []string{"2"}, a, messaging.FactoryStatus{DB: f.db})
	fake := &chatDoctorFixture{}
	d.chat = &chatRuntime{service: s, adapter: a, diagnoser: fake, redactor: redact.New(redact.Options{Environ: []string{"API_SECRET=chat-owner-secret"}}), notes: map[string]string{}}
	s.EnableResearch(chatResearch{d}, chatResearch{d})
	send := func(id, command string, options map[string]string) string {
		t.Helper()
		var typed []map[string]any
		for name, value := range options {
			var v any = value
			kind := 3
			if name == "issue" {
				kind = 4
				number, e := strconv.Atoi(value)
				researchRecoveryCheck(t, e)
				v = number
			}
			typed = append(typed, map[string]any{"name": name, "type": kind, "value": v})
		}
		body, e := json.Marshal(map[string]any{"id": id, "application_id": "app", "type": 2, "token": "token-" + id, "channel_id": "2", "user": map[string]string{"id": "7"}, "data": map[string]any{"name": command, "type": 1, "options": typed}})
		researchRecoveryCheck(t, e)
		timestamp := strconv.FormatInt(at.Unix(), 10)
		r := httptest.NewRequest(http.MethodPost, discord.Path, bytes.NewReader(body))
		r.Header.Set("X-Signature-Timestamp", timestamp)
		r.Header.Set("X-Signature-Ed25519", hex.EncodeToString(ed25519.Sign(priv, append([]byte(timestamp), body...))))
		w := httptest.NewRecorder()
		a.Handler(s).ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("intake: %d %s", w.Code, w.Body.String())
		}
		return w.Body.String()
	}
	base := map[string]string{"project": "research", "issue": "342"}
	send("10", "diagnose", base)
	_, err = s.Step(ctx, at)
	researchRecoveryCheck(t, err)
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
	_, err = s.Step(ctx, time.Now().Add(time.Second))
	researchRecoveryCheck(t, err)
	if len(forge.posts) != 0 || len(replies()) != 1 || !strings.Contains(replies()[0], "Coverage gaps") || !strings.Contains(replies()[0], "Open questions") || !strings.Contains(replies()[0], "only be reviewed on the issue") || strings.Contains(replies()[0], "private repair details") {
		t.Fatalf("diagnosis writes or rendering: %v %v", forge.posts, replies())
	}
	send("11", "question", base)
	_, err = s.Step(ctx, at)
	researchRecoveryCheck(t, err)
	token := questionToken("chat-question")
	if len(token) != 12 || !strings.Contains(replies()[len(replies())-1], "Which boundary?\nToken: "+token) {
		t.Fatalf("question: %v", replies())
	}
	answer := map[string]string{"project": "research", "issue": "342", "revision": "old-token", "text": "Use the existing boundary. chat-owner-secret"}
	send("12", "answer", answer)
	_, err = s.Step(ctx, at)
	researchRecoveryCheck(t, err)
	if !strings.Contains(replies()[len(replies())-1], "does not match") || !strings.Contains(replies()[len(replies())-1], token) {
		t.Fatalf("stale answer: %v", replies())
	}
	answer["revision"] = token
	forge.issue.State = "closed"
	send("15", "answer", answer)
	_, err = s.Step(ctx, at)
	researchRecoveryCheck(t, err)
	if !strings.Contains(replies()[len(replies())-1], "issue is closed") || !strings.Contains(replies()[len(replies())-1], token) {
		t.Fatalf("closed issue: %v", replies())
	}
	forge.issue.State = "open"
	watermark, found, err := f.db.GetWatermark(ctx, f.session.ID)
	researchRecoveryCheck(t, err)
	send("13", "answer", answer)
	_, err = s.Step(ctx, at)
	researchRecoveryCheck(t, err)
	if replies()[len(replies())-1] != "answer recorded" {
		t.Fatalf("answer: %v", replies())
	}
	if response := send("13", "answer", answer); !strings.Contains(response, "already answered") {
		t.Fatalf("duplicate: %s", response)
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
	// note or interaction token. A replacement service uses one restart retry.
	send("14", "diagnose", base)
	_, err = s.Step(ctx, at)
	researchRecoveryCheck(t, err)
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
	restartedAdapter := discord.New(discord.Options{ApplicationID: "app", PublicKey: pub, APIURL: api.URL, Client: api.Client(), Now: func() time.Time { return at }})
	restarted := messaging.New(f.db, "discord", "7", []string{"2"}, restartedAdapter, messaging.FactoryStatus{DB: f.db})
	d.chat.service, d.chat.adapter = restarted, restartedAdapter
	restarted.EnableResearch(chatResearch{d}, chatResearch{d})
	researchRecoveryCheck(t, f.db.RecoverChatDiagnoses(ctx, "discord", at))
	d.dispatchChatDiagnosis(ctx, at)
	d.doctor.pool.wait()
	_, err = restarted.Step(ctx, time.Now().Add(time.Second))
	researchRecoveryCheck(t, err)
	if fake.sources[len(fake.sources)-1] != "chat:discord:14:retry" || len(forge.posts) != 1 {
		t.Fatalf("restart duplicated an effect: %v %v", fake.sources, forge.posts)
	}
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
