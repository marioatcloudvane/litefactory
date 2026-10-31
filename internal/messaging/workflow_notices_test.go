package messaging_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/store"
)

func workflowNoticeFixture(t *testing.T) (*store.DB, time.Time) {
	t.Helper()
	db := database(t)
	ctx := t.Context()
	at := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	if _, err := db.UpsertIdentity(ctx, store.Identity{Forge: "github", Role: store.RoleOwner, UserID: 42, Handle: "owner", Source: store.SourceGH}); err != nil {
		t.Fatal(err)
	}
	owner, err := db.ConnectorOwner(ctx, 42)
	if err != nil {
		t.Fatal(err)
	}
	p := store.OwnerAction{ID: "workflow", PolicyVersion: 1, Principal: store.OwnerActionPrincipal{OwnerID: "42", OwnerGeneration: owner.Generation, Transport: "discord", ChannelID: "channel", DiscordOwnerID: "owner"}, RequestKey: "workflow", RequestDigest: "digest", Facts: `{}`, Payload: `{}`, Digest: "digest", Scope: "standards:management", Tier: "conversational", State: "awaiting_confirmation", CreatedAt: at, AvailableAt: at, ConfirmNotBefore: at.Add(5 * time.Second), ExpiresAt: at.Add(15 * time.Minute), ExecutionID: "owner-action:workflow"}
	if err = db.CreateOwnerAction(ctx, p); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ReserveWorkflowRequest(ctx, store.WorkflowRequest{OwnerID: "42", OwnerGeneration: owner.Generation, Key: "workflow", Digest: "digest", ProposalID: p.ID, JobID: "job", Snapshot: `{}`, Inputs: `{}`, Kind: "start", CreatedAt: at}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.MutateOwnerAction(ctx, p.Principal, p.ID, "confirm", "", p.ConfirmNotBefore); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ClaimOwnerActionExecution(ctx, p.ID, p.ConfirmNotBefore); err != nil {
		t.Fatal(err)
	}
	a, _, err := db.ClaimWorkflowAttempt(ctx, p.ConfirmNotBefore, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.FinishWorkflowAttempt(ctx, a, store.WorkflowFinish{State: "succeeded", Result: `{}`, Questions: `[]`, Notice: &store.WorkflowNotice{Event: "terminal", Destination: "channel", Body: "Saved result"}}, p.ConfirmNotBefore); err != nil {
		t.Fatal(err)
	}
	return db, p.ConfirmNotBefore
}
func TestWorkflowNoticeProviderOutcomesRemainSeparate(t *testing.T) {
	for _, tc := range []struct {
		name     string
		delivery messaging.Delivery
		state    string
	}{{"confirmed", messaging.Delivery{Confirmed: true, PromptID: "remote-message"}, "delivered"}, {"missing receipt", messaging.Delivery{Confirmed: true}, "failed"}, {"ambiguous", messaging.Delivery{}, "failed"}, {"refused", messaging.Delivery{Permanent: true}, "failed"}} {
		t.Run(tc.name, func(t *testing.T) {
			db, at := workflowNoticeFixture(t)
			sender := &noticeTransport{delivery: tc.delivery}
			service := messaging.New(db, "discord", "owner", []string{"channel"}, sender, nil)
			service.SetWorkflowNoticeAuthorizer(func(context.Context, string, string) error { return nil })
			if worked, err := service.NoticeStep(t.Context(), at); err != nil || !worked {
				t.Fatalf("delivery: %v %v", worked, err)
			}
			notices, err := db.WorkflowNotices(t.Context(), "job")
			if err != nil || len(notices) != 1 || notices[0].State != tc.state {
				t.Fatalf("notice: %+v %v", notices, err)
			}
			if worked, err := service.NoticeStep(t.Context(), at.Add(time.Minute)); err != nil || worked {
				t.Fatalf("replayed notice: %v %v", worked, err)
			}
			job, err := db.ReadWorkflowJob(t.Context(), "job")
			if err != nil || job.State != "succeeded" {
				t.Fatalf("delivery changed outcome: %+v %v", job, err)
			}
		})
	}
}
func TestWorkflowNoticeRateLimitRetriesOnlyConfirmedRefusal(t *testing.T) {
	db, at := workflowNoticeFixture(t)
	sender := &noticeTransport{delivery: messaging.Delivery{RetryAfter: 10 * time.Second}}
	service := messaging.New(db, "discord", "owner", []string{"channel"}, sender, nil)
	service.SetWorkflowNoticeAuthorizer(func(context.Context, string, string) error { return nil })
	if _, err := service.NoticeStep(t.Context(), at); err != nil {
		t.Fatal(err)
	}
	notices, err := db.WorkflowNotices(t.Context(), "job")
	if err != nil || notices[0].State != "pending" || !notices[0].RetryAt.Equal(at.Add(10*time.Second)) {
		t.Fatalf("rate limit: %+v %v", notices, err)
	}
	if _, err = db.ClaimChatNotice(t.Context(), "discord", at.Add(time.Second)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("rate limit ignored: %v", err)
	}
	sender.delivery = messaging.Delivery{Confirmed: true, PromptID: "remote-message"}
	if _, err = service.NoticeStep(t.Context(), at.Add(10*time.Second)); err != nil {
		t.Fatal(err)
	}
	notices, err = db.WorkflowNotices(t.Context(), "job")
	if err != nil || notices[0].State != "delivered" || notices[0].MessageID != "remote-message" {
		t.Fatalf("positive receipt: %+v %v", notices, err)
	}
}

func TestWorkflowNoticeRevokedBeforeRetryDoesNotSend(t *testing.T) {
	db, at := workflowNoticeFixture(t)
	sender := &noticeTransport{delivery: messaging.Delivery{RetryAfter: 10 * time.Second}}
	service := messaging.New(db, "discord", "owner", []string{"channel"}, sender, nil)
	service.SetWorkflowNoticeAuthorizer(func(_ context.Context, key, channel string) error {
		if key != "workflow-notice:1" || channel != "channel" {
			t.Fatalf("wrong notice authority binding %q %q", key, channel)
		}
		return nil
	})
	if _, err := service.NoticeStep(t.Context(), at); err != nil {
		t.Fatal(err)
	}
	if sender.calls != 1 {
		t.Fatal("first authorized delivery not attempted")
	}
	service.SetWorkflowNoticeAuthorizer(func(context.Context, string, string) error { return errors.New("current grant or destination revoked") })
	if worked, err := service.NoticeStep(t.Context(), at.Add(10*time.Second)); err != nil || !worked {
		t.Fatalf("revoked notice not settled %v %v", worked, err)
	}
	if sender.calls != 1 {
		t.Fatal("revoked notice sent to provider")
	}
	notices, err := db.WorkflowNotices(t.Context(), "job")
	if err != nil || len(notices) != 1 || notices[0].State != "failed" || notices[0].Reason != "delivery_authority_unavailable" {
		t.Fatalf("revoked notice status %+v %v", notices, err)
	}
	job, err := db.ReadWorkflowJob(t.Context(), "job")
	if err != nil || job.State != "succeeded" {
		t.Fatalf("notice revocation changed job outcome %+v %v", job, err)
	}
}
func TestWorkflowNoticeWithoutAuthorizerFailsClosed(t *testing.T) {
	db, at := workflowNoticeFixture(t)
	sender := &noticeTransport{delivery: messaging.Delivery{Confirmed: true, PromptID: "remote"}}
	service := messaging.New(db, "discord", "owner", []string{"channel"}, sender, nil)
	if worked, err := service.NoticeStep(t.Context(), at); err != nil || !worked {
		t.Fatalf("missing authorizer %v %v", worked, err)
	}
	if sender.calls != 0 {
		t.Fatal("notice sent without current authorization")
	}
	notices, err := db.WorkflowNotices(t.Context(), "job")
	if err != nil || len(notices) != 1 || notices[0].State != "failed" {
		t.Fatalf("missing authorization not retained %+v %v", notices, err)
	}
}
