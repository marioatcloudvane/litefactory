package messaging

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

// A finished conversation diagnosis reaches the channel without a new owner
// message, exactly once across repeated sweeps and a restarted service, and
// stays available to the next turn as context that was already posted.
func TestConversationDiagnosisPushedOnceAndKeptAsContext(t *testing.T) {
	const code = "fedcba0987654321"
	provider := &factoryUnitProvider{outputs: []string{`{"version":1,"kind":"answer","text":"Anything else?"}`}}
	run, at := factoryUnitRun(t, provider, code)
	ctx := t.Context()
	run.s.EnableResearch(&factoryUnitDiagnosis{}, nil)
	repo, err := run.s.db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "app"})
	if err != nil {
		t.Fatal(err)
	}
	payload := naturalPayload{Invocation: Invocation{Command: "diagnose", Arguments: map[string]string{"project": "acme/app", "issue": "42"}}, Channel: "2", Project: "acme/app", RepoID: repo.ID}
	encoded, digest, err := naturalDigest(payload)
	if err != nil {
		t.Fatal(err)
	}
	dialogue := store.NaturalDialogue{Transport: "discord", OwnerID: "1", ChannelRef: "2", Revision: 1, State: "awaiting_confirmation", SourceID: "100", Payload: encoded, Digest: digest, Code: code, ExpiresAt: at.Add(time.Minute), UpdatedAt: at}
	if err = run.s.db.SaveNaturalDialogue(ctx, dialogue, 0); err != nil {
		t.Fatal(err)
	}
	if err = run.s.db.BindNaturalPrompt(ctx, dialogue, "delivered-proposal"); err != nil {
		t.Fatal(err)
	}
	if err = run.s.db.SaveConversationSession(ctx, run.t, "unit-session"); err != nil {
		t.Fatal(err)
	}
	run.c.SessionHandle = "unit-session"
	if handled, err := run.confirm(ctx); err != nil || !handled {
		t.Fatalf("confirmation handled=%v err=%v", handled, err)
	}
	queued, err := run.s.db.ClaimConversationReply(ctx, "discord", at)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(queued.ReplyText, "as soon as it's ready") {
		t.Fatalf("queued reply %q", queued.ReplyText)
	}
	if err = run.s.db.FinishConversationReply(ctx, queued, "queue-notice", at); err != nil {
		t.Fatal(err)
	}
	action, err := run.s.db.ClaimChatDiagnosis(ctx, "discord", at)
	if err != nil {
		t.Fatal(err)
	}
	const saved = "Findings: the current task is waiting on the owner."
	if err = run.s.db.CompleteChatDiagnosisResult(ctx, "discord", action.MessageID, action.AnswerAttempts, saved, at, "success"); err != nil {
		t.Fatal(err)
	}

	// Two sweeps, the second by a restarted service, find the same result.
	restarted := New(run.s.db, "discord", "1", []string{"2"}, nil, FactoryStatus{DB: run.s.db})
	for _, s := range []*Service{run.s, run.s, restarted} {
		results, err := s.db.ListConversationDiagnosisResults(ctx, "discord", 16)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range results {
			if err = s.PushConversationDiagnosis(ctx, r, "", at); err != nil {
				t.Fatal(err)
			}
		}
	}
	notice, err := run.s.db.ClaimChatNotice(ctx, "discord", at)
	if err != nil {
		t.Fatal(err)
	}
	if notice.ChannelRef != "2" || notice.MessageID != "diagnosis:"+action.MessageID+":0001" || !strings.Contains(notice.ReplyText, "the current task is waiting on the owner.") {
		t.Fatalf("pushed notice %+v", notice)
	}
	if err = run.s.db.FinishChatNotice(ctx, notice, false, at); err != nil {
		t.Fatal(err)
	}
	if extra, err := run.s.db.ClaimChatNotice(ctx, "discord", at.Add(time.Hour)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("result posted twice: %+v %v", extra, err)
	}
	row, err := run.s.db.ReadChatReply(ctx, "discord", action.MessageID)
	if err != nil {
		t.Fatal(err)
	}
	if row.State != "confirmed" || row.FinishedAt.IsZero() {
		t.Fatalf("inbox row not finished: %+v", row)
	}
	execution, err := run.s.db.ReadNaturalExecution(ctx, action.MessageID)
	if err != nil {
		t.Fatal(err)
	}
	if execution.State != "finished" || execution.Outcome != saved {
		t.Fatalf("execution %+v", execution)
	}

	// The owner's next message sees the result as context, once.
	scan, err := run.s.db.ReadNaturalScan(ctx, "2")
	if err != nil {
		t.Fatal(err)
	}
	if err = run.s.db.StageNaturalScan(ctx, scan, store.NaturalScan{ChannelRef: "2", Cursor: scan.Cursor, HighWater: "102"}, []store.NaturalMessage{{ChannelRef: "2", SourceID: "102", OwnerID: "1", Text: "Thanks", CreatedAt: at}}, true); err != nil {
		t.Fatal(err)
	}
	if _, err = run.s.db.AdmitConversationTurn(ctx, run.c.ID, "102", "protected followup", at); err != nil {
		t.Fatal(err)
	}
	next, err := run.s.db.ClaimConversationTurn(ctx, run.c.ID, "unit-worker", at, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err = run.s.runFactoryConversation(ctx, run.c, run.b, next, at); err != nil {
		t.Fatal(err)
	}
	if provider.calls != 1 || !strings.Contains(provider.prompts[0], saved) || !strings.Contains(provider.prompts[0], "Already posted") {
		t.Fatalf("next turn context calls=%d", provider.calls)
	}
	link, err := run.s.db.GetConversationActionLink(ctx, run.t.ID)
	if err != nil || link.Outcome != saved {
		t.Fatalf("saved link %+v %v", link, err)
	}
	pending, err := run.s.db.ListPendingConversationActionOutcomes(ctx, run.c.ID)
	if err != nil || len(pending) != 0 {
		t.Fatalf("outcome eligible again: %+v %v", pending, err)
	}
}
