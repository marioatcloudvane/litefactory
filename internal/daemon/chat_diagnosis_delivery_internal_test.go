package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/doctor"
	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

type chatDeliveryProvider struct{}

func (chatDeliveryProvider) RunConversation(context.Context, agent.ConversationRequest) (agent.ConversationResult, error) {
	return agent.ConversationResult{}, errors.New("the conversation provider must not run")
}

// chatProposalDoctor records a real repair proposal under the chat source, as
// the doctor service does for a diagnosis that proposes a repair.
type chatProposalDoctor struct {
	t        *testing.T
	db       *store.DB
	evidence store.DoctorEvidence
	calls    int
}

func (f *chatProposalDoctor) Diagnose(_ context.Context, r doctor.DiagnoseRequest) (doctor.Conversation, error) {
	f.calls++
	conversation, proposal := doctorProposalFixture(f.t, f.db, r.Target.String(), "chat-attempt", r.SourceID, store.DoctorRepairStep{Number: 1, Kind: "consume_comment", Target: "comment:11", Before: "10", After: "11"}, f.evidence)
	return doctor.Conversation{ProposalID: proposal, Conversation: conversation, ApplyAvailable: true, Diagnosis: &doctor.Diagnosis{Findings: []doctor.Finding{{Explanation: "The reply was never consumed."}}, Proposal: []doctor.RepairStep{{Change: "consume the reply"}}}, CurrentProposal: []doctor.RepairStep{{Number: 1, Target: "comment:11", Change: "consume the reply"}}}, nil
}

// A diagnosis confirmed in the conversation is posted to the channel when it
// finishes, without another owner message; its repair proposal is presented on
// the issue through the doctor's GitHub path and the channel post links to it.
// Repeated sweeps and a restarted service post nothing twice.
func TestConversationDiagnosisPostedWithIssueProposal(t *testing.T) {
	d, forge, repo := doctorDaemonFixture(t)
	ctx := t.Context()
	at := time.Now().UTC()
	session, err := d.db.GetOrCreateSession(ctx, repo, 406)
	researchRecoveryCheck(t, err)
	ref, err := naming.ParseRef(session.TaskRef)
	researchRecoveryCheck(t, err)
	evidence, err := d.db.ReadDoctorEvidence(ctx, ref)
	researchRecoveryCheck(t, err)

	newService := func() *messaging.Service {
		s := messaging.New(d.db, "discord", "1", []string{"2"}, nil, messaging.FactoryStatus{DB: d.db})
		s.Protect = func(_ context.Context, text string) (string, error) { return text, nil }
		binding := messaging.ConversationBinding{Channel: "2", Name: "owner", Provider: "claude", Model: "fixture"}
		researchRecoveryCheck(t, s.EnableConversations(ctx, messaging.ConversationOptions{Bindings: []messaging.ConversationBinding{binding}, StateRoot: filepath.Join(t.TempDir(), "sessions"), Providers: map[string]messaging.ConversationProvider{"claude": chatDeliveryProvider{}}, CheckAccess: func(context.Context, string) error { return nil }, Factory: true}, at))
		s.EnableResearch(chatResearch{d}, chatResearch{d})
		return s
	}
	fake := &chatProposalDoctor{t: t, db: d.db, evidence: evidence}
	d.chat = &chatRuntime{service: newService(), diagnoser: fake, redactor: redact.New(redact.Options{}), notes: map[string]string{}}

	// The owner confirmed "diagnose #406" in the conversation.
	c, err := d.db.ReadConversation(ctx, "discord", "2")
	researchRecoveryCheck(t, err)
	researchRecoveryCheck(t, d.db.ActivateNaturalScan(ctx, "2", "100"))
	scan, err := d.db.ReadNaturalScan(ctx, "2")
	researchRecoveryCheck(t, err)
	researchRecoveryCheck(t, d.db.StageNaturalScan(ctx, scan, store.NaturalScan{ChannelRef: "2", Cursor: "100", HighWater: "101"}, []store.NaturalMessage{{ChannelRef: "2", SourceID: "101", OwnerID: "1", Text: "yes", CreatedAt: at}}, true))
	_, err = d.db.AdmitConversationTurn(ctx, c.ID, "101", "confirmation", at)
	researchRecoveryCheck(t, err)
	turn, err := d.db.ClaimConversationTurn(ctx, c.ID, "worker", at, time.Minute)
	researchRecoveryCheck(t, err)
	payload := fmt.Sprintf(`{"invocation":{"Command":"diagnose","Arguments":{"issue":"406","project":"acme/api"}},"PreviewID":"","PreviewHash":"","Question":"","Revision":"","RepoID":%d,"Project":"acme/api","Channel":"2"}`, repo.ID)
	sum := sha256.Sum256([]byte(payload))
	const code = "0123456789abcdef"
	dialogue := store.NaturalDialogue{Transport: "discord", OwnerID: "1", ChannelRef: "2", Revision: 1, State: "awaiting_confirmation", SourceID: "100", Payload: payload, Digest: hex.EncodeToString(sum[:]), Code: code, ExpiresAt: at.Add(time.Hour), UpdatedAt: at}
	researchRecoveryCheck(t, d.db.SaveNaturalDialogue(ctx, dialogue, 0))
	researchRecoveryCheck(t, d.db.BindNaturalPrompt(ctx, dialogue, "proposal"))
	reply := store.ChatReply{Transport: "discord", ChannelRef: "2", MessageID: "nl-exec:" + code, Command: "diagnose", Arguments: map[string]string{"issue": "406", "project": "acme/api"}, CreatedAt: at}
	researchRecoveryCheck(t, d.db.AuthorizeConversationDialogue(ctx, turn, dialogue, "101", reply, at))
	_, err = d.db.StartConversationActionReply(ctx, turn, reply, at)
	researchRecoveryCheck(t, err)
	researchRecoveryCheck(t, d.db.StartNaturalExecution(ctx, reply.MessageID, at))
	researchRecoveryCheck(t, d.db.DeferChatDiagnosisRepository(ctx, "discord", reply.MessageID, session.TaskRef, at, repo.ID))

	d.dispatchChatDiagnosis(ctx, at)
	d.doctor.pool.wait()
	if fake.calls != 1 {
		t.Fatalf("diagnosis calls %d", fake.calls)
	}
	// The channel post waits briefly for the proposal's issue comment.
	researchRecoveryCheck(t, d.chatDiagnosisResults(ctx, time.Now()))
	if n, err := d.db.ClaimChatNotice(ctx, "discord", at.Add(2*time.Hour)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("posted before the proposal was presented: %+v %v", n, err)
	}
	researchRecoveryCheck(t, d.publishDoctor(ctx))
	researchRecoveryCheck(t, d.publishDoctor(ctx))
	if len(forge.posts) != 1 || !strings.Contains(forge.posts[0].Body, "Current repair proposal") || !strings.Contains(forge.posts[0].Body, "`/doctor apply`") {
		t.Fatalf("issue presentation: %+v", forge.posts)
	}
	presented, err := d.db.ReadChatDiagnosisPresentation(ctx, "chat:discord:"+reply.MessageID)
	researchRecoveryCheck(t, err)
	complete, err := d.db.DoctorPresentationComplete(ctx, presented.CommandID)
	researchRecoveryCheck(t, err)
	if !complete || presented.CommentID != forge.posts[0].ID || presented.TaskRef != session.TaskRef {
		t.Fatalf("presentation %+v complete=%v", presented, complete)
	}

	// A second sweep, and one by a restarted chat service, find nothing to post.
	researchRecoveryCheck(t, d.chatDiagnosisResults(ctx, time.Now()))
	researchRecoveryCheck(t, d.chatDiagnosisResults(ctx, time.Now()))
	d.chat.service = newService()
	researchRecoveryCheck(t, d.chatDiagnosisResults(ctx, time.Now()))
	// The queued confirmation was not delivered in this fixture, so the
	// notice is claimed once its confirmation hold has lapsed.
	var posted []string
	for range 2 {
		notice, err := d.db.ClaimChatNotice(ctx, "discord", at.Add(2*time.Hour))
		researchRecoveryCheck(t, err)
		if notice.ChannelRef != "2" {
			t.Fatalf("channel post %+v", notice)
		}
		posted = append(posted, notice.ReplyText)
		researchRecoveryCheck(t, d.db.FinishChatNotice(ctx, notice, false, at.Add(2*time.Hour)))
	}
	link := fmt.Sprintf("https://github.com/acme/api/issues/406#issuecomment-%d", forge.posts[0].ID)
	if !strings.Contains(posted[0], "The reply was never consumed.") || !strings.Contains(posted[1], link) || !strings.Contains(posted[1], "`/doctor apply`") {
		t.Fatalf("channel posts %q", posted)
	}
	if extra, err := d.db.ClaimChatNotice(ctx, "discord", at.Add(3*time.Hour)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("posted twice: %+v %v", extra, err)
	}
	row, err := d.db.ReadChatReply(ctx, "discord", reply.MessageID)
	researchRecoveryCheck(t, err)
	if row.State != "confirmed" || row.FinishedAt.IsZero() || row.OutcomeStatus != "success" {
		t.Fatalf("inbox row %+v", row)
	}
	execution, err := d.db.ReadNaturalExecution(ctx, reply.MessageID)
	researchRecoveryCheck(t, err)
	if execution.State != "finished" {
		t.Fatalf("execution %+v", execution)
	}
	if len(forge.posts) != 1 {
		t.Fatalf("issue posts %d", len(forge.posts))
	}
}
