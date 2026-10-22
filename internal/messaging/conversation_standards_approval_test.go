package messaging

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

type standardsGateOperations struct {
	review     StandardsReview
	executions int
}

func (f *standardsGateOperations) Review(context.Context, StandardsRequest) (StandardsReview, error) {
	return f.review, nil
}
func (f *standardsGateOperations) Execute(context.Context, StandardsRequest) (StandardsResult, error) {
	f.executions++
	return StandardsResult{Text: "approved"}, nil
}
func (*standardsGateOperations) Read(context.Context, StandardsRequest) (StandardsResult, error) {
	return StandardsResult{}, nil
}

func standardsGateFixture(t *testing.T) (*Service, *standardsGateOperations, func(string, time.Time) *factoryConversationRun) {
	t.Helper()
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "factory.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	c, err := db.EnsureConversation(ctx, store.Conversation{Transport: "discord", ChannelRef: "3", OwnerID: "7", Name: "standards-gates", Provider: "claude", StateDir: t.TempDir()}, at)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.ActivateNaturalScan(ctx, "3", "100"); err != nil {
		t.Fatal(err)
	}
	sender := &standardsDeliverySender{result: Delivery{Confirmed: true, PromptID: "remote-prompt"}}
	service := New(db, "discord", "7", []string{"3"}, sender, FactoryStatus{DB: db})
	service.Protect = func(_ context.Context, text string) (string, error) { return text, nil }
	service.conversations.Store(&conversationRuntime{bindings: map[string]ConversationBinding{"3": {Provider: "claude"}}, check: func(context.Context, string) error { return nil }, accessAfter: map[string]time.Time{}})
	ops := &standardsGateOperations{review: StandardsReview{Kind: "proposal", TargetID: "proposal-1", TargetVersion: 1, BaseRevision: 1, ChangeDigest: "change", ResultDigest: "result", Text: "Full exact original review"}}
	service.EnableStandards(ops)
	next := 100
	run := func(text string, at time.Time) *factoryConversationRun {
		t.Helper()
		next++
		id := fmt.Sprint(next)
		scan, e := db.ReadNaturalScan(ctx, "3")
		if e != nil {
			t.Fatal(e)
		}
		following := scan
		following.HighWater = id
		if e = db.StageNaturalScan(ctx, scan, following, []store.NaturalMessage{{ChannelRef: "3", SourceID: id, OwnerID: "7", Text: text, CreatedAt: at}}, true); e != nil {
			t.Fatal(e)
		}
		if _, e = db.AdmitConversationTurn(ctx, c.ID, id, "fixture-prompt", at); e != nil {
			t.Fatal(e)
		}
		turn, e := db.ClaimConversationTurn(ctx, c.ID, "gate-worker", at, time.Minute)
		if e != nil {
			t.Fatal(e)
		}
		if e = db.SaveConversationSession(ctx, turn, "gate-native-session"); e != nil {
			t.Fatal(e)
		}
		return &factoryConversationRun{s: service, c: c, t: turn, at: at}
	}
	return service, ops, run
}

func TestStandardsHostApprovalRejectsIncompleteExpiredAndChangedReview(t *testing.T) {
	for _, scenario := range []string{"bare yes", "unknown code", "partial delivery", "expired", "CLI changed terms"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := t.Context()
			s, ops, start := standardsGateFixture(t)
			now := time.Now().UTC()
			text := "approve standards review-code"
			if scenario == "bare yes" {
				text = "yes"
			}
			if scenario != "unknown code" && scenario != "bare yes" {
				m, err := s.db.SaveStandardsConversationReview(ctx, store.StandardsConversationReview{ID: "review-1", Transport: "discord", OwnerID: "7", ChannelID: "3", Kind: "proposal", TargetID: ops.review.TargetID, TargetVersion: 1, BaseRevision: 1, ChangeDigest: ops.review.ChangeDigest, ResultDigest: ops.review.ResultDigest, Text: ops.review.Text, Parts: []string{ops.review.Text}, Code: "review-code"})
				if err != nil {
					t.Fatal(err)
				}
				if scenario != "partial delivery" {
					if _, err = s.db.AcknowledgeStandardsConversationReviewPart(ctx, m.ID, "7", "3", 0, "review-part"); err != nil {
						t.Fatal(err)
					}
					when := now
					if scenario == "expired" {
						when = now.Add(-16 * time.Minute)
					}
					if _, err = s.db.ActivateStandardsConversationReview(ctx, m.ID, "7", "3", "final-prompt", when); err != nil {
						t.Fatal(err)
					}
				}
				if scenario == "CLI changed terms" {
					ops.review.Text = "Different CLI terms"
					ops.review.ChangeDigest = "changed"
				}
			}
			run := start(text, now)
			handled, err := run.approveStandards(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if handled == (scenario == "bare yes") {
				t.Fatalf("unexpected host recognition: %t", handled)
			}
			if ops.executions != 0 {
				t.Fatal("unsafe approval reached shared mutation")
			}
		})
	}
}

func TestStandardsReviewProtectionChangeCannotIssueApproval(t *testing.T) {
	ctx := t.Context()
	s, ops, start := standardsGateFixture(t)
	s.Protect = func(_ context.Context, text string) (string, error) {
		return strings.ReplaceAll(text, "original", "redacted"), nil
	}
	run := start("Review proposal-1 version 1", time.Now().UTC())
	if err := run.reviewStandards(ctx, Invocation{Command: "standards", Arguments: map[string]string{"action": "proposal.review", "id": "proposal-1", "version": "1"}}); err != nil {
		t.Fatal(err)
	}
	reply, err := s.db.ClaimConversationReply(ctx, "discord", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply.ReplyText, "Approval is blocked") {
		t.Fatalf("changed review was offered as approvable: %s", reply.ReplyText)
	}
	if ops.executions != 0 {
		t.Fatal("protection change approved proposal")
	}
}

func TestStandardsLargeReviewOffersApprovalOnlyAfterFinalAcknowledgedBatch(t *testing.T) {
	ctx := t.Context()
	s, ops, start := standardsGateFixture(t)
	now := time.Now().UTC()
	ops.review.Text = strings.Repeat("Full exact before and after 界🙂\n", 4000)
	manifest, err := s.db.SaveStandardsConversationReview(ctx, store.StandardsConversationReview{ID: "large-review", Transport: "discord", OwnerID: "7", ChannelID: "3", Kind: "proposal", TargetID: "proposal-1", TargetVersion: 1, BaseRevision: 1, ChangeDigest: "change", ResultDigest: "result", Text: ops.review.Text, Parts: standardsReviewParts(ops.review.Text), Code: "large-code"})
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for batch := 0; batch < 10; batch++ {
		run := start("Continue exact review large-review", now.Add(time.Duration(batch)*time.Second))
		if err = run.queueStandardsReview(ctx, manifest); err != nil {
			t.Fatal(err)
		}
		turn, err := s.db.LatestConversationTurn(ctx, run.c.ID)
		if err != nil {
			t.Fatal(err)
		}
		reply, err := s.db.ReadChatReply(ctx, "discord", turn.ReplyID)
		if err != nil {
			t.Fatal(err)
		}
		answer, _, err := s.db.ConversationReplyAnswer(ctx, reply)
		if err != nil {
			t.Fatal(err)
		}
		parts, err := s.db.ReadConversationAnswerParts(ctx, answer.ID)
		if err != nil {
			t.Fatal(err)
		}
		sourceBytes := 0
		for _, p := range parts[:len(parts)-1] {
			sourceBytes += len(p.Text)
		}
		if sourceBytes > 64<<10 {
			t.Fatal("review batch exceeded source-byte budget")
		}
		total += sourceBytes
		final := strings.Contains(parts[len(parts)-1].Text, "approve standards large-code")
		if final != (total == len(ops.review.Text)) {
			t.Fatal("approval prompt appeared before complete exact review")
		}
		before, err := s.db.GetStandardsConversationReview(ctx, manifest.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !before.ExpiresAt.IsZero() {
			t.Fatal("approval activated before final prompt acknowledgment")
		}
		for range len(parts) {
			if _, err = s.ConversationDeliveryStep(ctx, run.at); err != nil {
				t.Fatal(err)
			}
		}
		manifest, err = s.db.GetStandardsConversationReview(ctx, manifest.ID)
		if err != nil {
			t.Fatal(err)
		}
		if final {
			if manifest.ExpiresAt.IsZero() {
				t.Fatal("final acknowledgment did not activate challenge")
			}
			return
		}
		if !manifest.ExpiresAt.IsZero() {
			t.Fatal("intermediate batch activated approval")
		}
	}
	t.Fatal("bounded batches failed to finish review")
}

type standardsGateRecovery struct {
	standardsGateOperations
	recovered []StandardsRequest
}

func (f *standardsGateRecovery) Recover(_ context.Context, r StandardsRequest) (StandardsResult, bool, error) {
	f.recovered = append(f.recovered, r)
	return StandardsResult{Text: "Committed author session saved-author version 2"}, true, nil
}

func TestStandardsConversationRecoversCommittedOutcomeBeforeUncertainChatRecovery(t *testing.T) {
	ctx := t.Context()
	s, _, start := standardsGateFixture(t)
	now := time.Now().UTC()
	run := start("confirm saved operation", now)
	p := naturalPayload{Invocation: Invocation{Command: "standards-author", Arguments: map[string]string{"action": "answer", "id": "saved-author", "version": "1", "question_id": "question-1", "text": "Relevant package checks"}}, Channel: "3"}
	payload, digest, err := naturalDigest(p)
	if err != nil {
		t.Fatal(err)
	}
	dialogue := store.NaturalDialogue{Transport: "discord", OwnerID: "7", ChannelRef: "3", Revision: 1, State: "awaiting_confirmation", SourceID: "100", Payload: payload, Digest: digest, Code: "saved-code", ExpiresAt: now.Add(time.Minute), UpdatedAt: now}
	if err = s.db.SaveNaturalDialogue(ctx, dialogue, 0); err != nil {
		t.Fatal(err)
	}
	if err = s.db.BindNaturalPrompt(ctx, dialogue, "preview-prompt"); err != nil {
		t.Fatal(err)
	}
	reply := store.ChatReply{Transport: "discord", MessageID: "nl-exec:saved-code", ChannelRef: "3", Command: "standards-author", Arguments: p.Invocation.Arguments}
	if err = s.db.AuthorizeConversationDialogue(ctx, run.t, dialogue, run.t.SourceID, reply, now); err != nil {
		t.Fatal(err)
	}
	if err = s.db.StartNaturalExecution(ctx, reply.MessageID, now); err != nil {
		t.Fatal(err)
	}
	// The response-only recovery fixture represents an upstream committed receipt;
	// the daemon adapter test separately verifies it against the shared domain store.
	recovery := &standardsGateRecovery{}
	path := s.db.Path()
	if err = s.db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	resumed := New(db, "discord", "7", []string{"3"}, nil, FactoryStatus{DB: db})
	resumed.Protect = func(_ context.Context, text string) (string, error) { return text, nil }
	resumed.EnableStandards(recovery)
	if err = resumed.recoverStandardsConversationAction(ctx, run.t, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	execution, err := db.ReadNaturalExecution(ctx, reply.MessageID)
	if err != nil {
		t.Fatal(err)
	}
	if execution.State != "finished" || execution.Outcome != "Committed author session saved-author version 2" {
		t.Fatalf("committed operation became uncertain: %+v", execution)
	}
	if recovery.executions != 0 || len(recovery.recovered) != 1 {
		t.Fatal("chat recovery repeated a mutation")
	}
	request := recovery.recovered[0]
	if request.OperationKey != "discord:3:"+reply.MessageID || request.MessageID != run.t.SourceID {
		t.Fatalf("recovery lost original operation identity: %+v", request)
	}
	if err = resumed.recoverStandardsConversationAction(ctx, run.t, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if len(recovery.recovered) != 1 {
		t.Fatal("finished outcome unnecessarily reconciled again")
	}
}

func standardsGateManifest(t *testing.T, s *Service, ops *standardsGateOperations, at time.Time) store.StandardsConversationReview {
	t.Helper()
	ctx := t.Context()
	m, err := s.db.SaveStandardsConversationReview(ctx, store.StandardsConversationReview{ID: "specialized-review", Transport: "discord", OwnerID: "7", ChannelID: "3", Kind: "proposal", TargetID: ops.review.TargetID, TargetVersion: ops.review.TargetVersion, BaseRevision: ops.review.BaseRevision, ChangeDigest: ops.review.ChangeDigest, ResultDigest: ops.review.ResultDigest, Text: ops.review.Text, Parts: []string{ops.review.Text}, Code: "specialized-code"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.AcknowledgeStandardsConversationReviewPart(ctx, m.ID, "7", "3", 0, "review-content"); err != nil {
		t.Fatal(err)
	}
	m, err = s.db.ActivateStandardsConversationReview(ctx, m.ID, "7", "3", "specialized-final-prompt", at)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestStandardsSpecializedApprovalRecoversConsumedReceiptAfterRestart(t *testing.T) {
	ctx := t.Context()
	s, ops, start := standardsGateFixture(t)
	now := time.Now().UTC()
	m := standardsGateManifest(t, s, ops, now)
	run := start("approve standards "+m.Code, now.Add(time.Second))
	// The domain committed approval and consumed its gate, but the chat process
	// stopped before it could retain an outcome. No generic dialogue exists.
	if _, err := s.db.ConsumeStandardsConversationReview(ctx, m.ID, "7", "3", m.Code, run.at); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.GetConversationActionLink(ctx, run.t.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("specialized approval unexpectedly has generic dialogue: %v", err)
	}
	path := s.db.Path()
	if err := s.db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	recovery := &standardsGateRecovery{}
	resumed := New(db, "discord", "7", []string{"3"}, nil, FactoryStatus{DB: db})
	resumed.Protect = func(_ context.Context, text string) (string, error) { return text, nil }
	resumed.EnableStandards(recovery)
	if err = resumed.recoverStandardsApproval(ctx, run.c, run.t, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	exchanges, err := db.ListConversationExchanges(ctx, run.t.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(exchanges) != 1 || exchanges[0].Kind != "saved_outcome" || !strings.Contains(exchanges[0].Result, "Committed author session saved-author version 2") {
		t.Fatalf("consumed approval lost its factual receipt: %+v", exchanges)
	}
	if len(recovery.recovered) != 1 || recovery.executions != 0 {
		t.Fatal("approval recovery repeated domain execution")
	}
	request := recovery.recovered[0]
	if request.Operation != "proposal.approve" || request.OperationKey != "discord:3:review:"+m.ID || request.MessageID != run.t.SourceID {
		t.Fatalf("approval recovery selected another operation: %+v", request)
	}
}

func TestStandardsExplicitApprovalCanReplyToExactFinalPrompt(t *testing.T) {
	ctx := t.Context()
	s, ops, start := standardsGateFixture(t)
	now := time.Now().UTC()
	m := standardsGateManifest(t, s, ops, now)
	run := start("approve standards", now.Add(time.Second))
	run.t.ReplyTo = m.PromptRemoteID
	handled, err := run.approveStandards(ctx)
	if err != nil || !handled {
		t.Fatalf("explicit reply approval failed: %t %v", handled, err)
	}
	if ops.executions != 1 {
		t.Fatal("explicit reply to exact final prompt did not reach approval")
	}
}

func TestStandardsDelayedOldPromptCannotActivateRotatedChallenge(t *testing.T) {
	ctx := t.Context()
	s, ops, start := standardsGateFixture(t)
	now := time.Now().UTC()
	// Queue an immutable old challenge delivery before its receipt is lost.
	m, err := s.db.SaveStandardsConversationReview(ctx, store.StandardsConversationReview{ID: "rotated-review", Transport: "discord", OwnerID: "7", ChannelID: "3", Kind: "proposal", TargetID: ops.review.TargetID, TargetVersion: 1, BaseRevision: 1, ChangeDigest: ops.review.ChangeDigest, ResultDigest: ops.review.ResultDigest, Text: ops.review.Text, Parts: []string{ops.review.Text}, Code: "old-code"})
	if err != nil {
		t.Fatal(err)
	}
	run := start("Review proposal-1", now.Add(-17*time.Minute))
	if err = run.queueStandardsReview(ctx, m); err != nil {
		t.Fatal(err)
	}
	turn, err := s.db.LatestConversationTurn(ctx, run.c.ID)
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.db.ReadChatReply(ctx, "discord", turn.ReplyID)
	if err != nil {
		t.Fatal(err)
	}
	answer, _, err := s.db.ConversationReplyAnswer(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	parts, err := s.db.ReadConversationAnswerParts(ctx, answer.ID)
	if err != nil {
		t.Fatal(err)
	}
	oldPrompt, err := s.db.ReadChatReply(ctx, "discord", parts[len(parts)-1].ReplyID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.AcknowledgeStandardsConversationReviewPart(ctx, m.ID, "7", "3", 0, "review-content"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.ActivateStandardsConversationReview(ctx, m.ID, "7", "3", "old-prompt", now.Add(-16*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.RefreshStandardsConversationReviewChallenge(ctx, m.ID, "7", "3", "new-code", now); err != nil {
		t.Fatal(err)
	}
	if err = s.acknowledgeStandardsReview(ctx, oldPrompt, "delayed-old-prompt", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	current, err := s.db.GetStandardsConversationReview(ctx, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Code != "new-code" || current.PromptRemoteID != "" || !current.ExpiresAt.IsZero() {
		t.Fatalf("old delivery activated replacement challenge: %+v", current)
	}
}
