package store

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"
)

func conversationTestOperation(t *testing.T, db *DB, key string) StandardsConversationOperation {
	t.Helper()
	o, e := db.AdmitStandardsConversationOperation(t.Context(), StandardsConversationOperation{Key: key, RequestDigest: "digest", Kind: "author.start", Transport: "discord", OwnerID: "1", ChannelID: "2", SourceID: "3", RequestJSON: json.RawMessage(`{"input":"draft"}`)})
	if e != nil {
		t.Fatal(e)
	}
	return o
}
func TestStandardsConversationAtomicEffectAndReceiptRecovery(t *testing.T) {
	db := newTestDB(t)
	other, e := Open(t.Context(), db.path)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = other.Close() })
	o := conversationTestOperation(t, db, "source/action")
	ctx := WithStandardsConversationOperation(t.Context(), o.Key, o.RequestDigest)
	s, e := db.SaveStandardsAuthoring(ctx, StandardsAuthoringSession{ID: "draft", Mode: "create", TargetID: "standard", Status: "draft", Data: json.RawMessage(`{"questions":["why?"]}`)}, 0)
	if e != nil {
		t.Fatal(e)
	}
	retained, e := other.GetStandardsConversationOperation(t.Context(), o.Key)
	if e != nil || !retained.EffectCommitted {
		t.Fatalf("receipt not atomic: %#v %v", retained, e)
	}
	var effect StandardsConversationEffect
	if e = json.Unmarshal(retained.EffectJSON, &effect); e != nil || effect.ID != s.ID || effect.Version != 1 {
		t.Fatalf("effect %#v %v", effect, e)
	}
	if _, e = other.SaveStandardsAuthoring(ctx, s, 0); !errors.Is(e, ErrStandardsOperationCommitted) {
		t.Fatalf("retry reached stale-version logic: %v", e)
	}
	changed := o
	changed.RequestJSON = json.RawMessage(`{"input":"different"}`)
	if _, e = other.AdmitStandardsConversationOperation(t.Context(), changed); !errors.Is(e, ErrStandardsConflict) {
		t.Fatalf("changed admitted input accepted: %v", e)
	}
	result := json.RawMessage(`{"session":"draft","version":1}`)
	if _, e = db.FinishStandardsConversationOperation(t.Context(), o.Key, o.RequestDigest, "completed", result, ""); e != nil {
		t.Fatal(e)
	}
	again, e := other.AdmitStandardsConversationOperation(t.Context(), o)
	if e != nil || !reflect.DeepEqual(again.ResultJSON, result) {
		t.Fatalf("lost reply recovery: %#v %v", again, e)
	}
	bad := conversationTestOperation(t, db, "failed")
	badctx := WithStandardsConversationOperation(t.Context(), bad.Key, bad.RequestDigest)
	if _, e = db.SaveStandardsAuthoring(badctx, s, 0); !errors.Is(e, ErrStandardsConflict) {
		t.Fatalf("expected conflicting mutation: %v", e)
	}
	bad, e = db.GetStandardsConversationOperation(t.Context(), bad.Key)
	if e != nil || bad.EffectCommitted {
		t.Fatalf("failed effect recorded %#v %v", bad, e)
	}
}
func TestStandardsConversationContextAndGenerationFence(t *testing.T) {
	db := newTestDB(t)
	c, e := db.SaveStandardsConversationContext(t.Context(), StandardsConversationContext{Transport: "discord", OwnerID: "1", ChannelID: "2", SelectedJSON: json.RawMessage(`{"session":"s","question":"q"}`), Clarification: "which question?"}, 0)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.SaveStandardsConversationContext(t.Context(), c, 0); !errors.Is(e, ErrStandardsConflict) {
		t.Fatal(e)
	}
	got, e := db.GetStandardsConversationContext(t.Context(), "discord", "1", "2")
	if e != nil || !reflect.DeepEqual(got.SelectedJSON, c.SelectedJSON) || got.Clarification != c.Clarification {
		t.Fatalf("saved coordination: %#v %v", got, e)
	}
	o := conversationTestOperation(t, db, "generation")
	if _, e = db.StartStandardsConversationGeneration(t.Context(), o.Key, o.RequestDigest); e != nil {
		t.Fatal(e)
	}
	if _, e = db.StartStandardsConversationGeneration(t.Context(), o.Key, o.RequestDigest); !errors.Is(e, ErrStandardsOperationStarted) {
		t.Fatalf("uncertain generation replay: %v", e)
	}
}
func TestStandardsConversationReviewAtomicConsumptionAndDelivery(t *testing.T) {
	db := newTestDB(t)
	r, e := db.SaveStandardsConversationReview(t.Context(), StandardsConversationReview{ID: "review", Transport: "discord", OwnerID: "1", ChannelID: "2", Kind: "proposal", TargetID: "p", TargetVersion: 1, ChangeDigest: "change", ResultDigest: "result", Text: "before\nafter", Parts: []string{"before\n", "after"}, Code: "exact"})
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC()
	if _, e = db.ActivateStandardsConversationReview(t.Context(), r.ID, "1", "2", "prompt", now); !errors.Is(e, ErrStandardsApprovalRequired) {
		t.Fatalf("partial review enabled approval: %v", e)
	}
	if _, e = db.AcknowledgeStandardsConversationReviewPart(t.Context(), r.ID, "1", "2", 1, "part2"); !errors.Is(e, ErrStandardsConflict) {
		t.Fatalf("out of order review: %v", e)
	}
	for i := range r.Parts {
		if _, e = db.AcknowledgeStandardsConversationReviewPart(t.Context(), r.ID, "1", "2", i, r.Parts[i]); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = db.ActivateStandardsConversationReview(t.Context(), r.ID, "1", "2", "prompt", now); e != nil {
		t.Fatal(e)
	}
	if _, e = db.ConsumeStandardsConversationReview(t.Context(), r.ID, "other", "2", r.Code, now); !errors.Is(e, ErrStandardsApprovalRequired) {
		t.Fatalf("wrong owner: %v", e)
	}
	if _, e = db.ConsumeStandardsConversationReview(t.Context(), r.ID, "1", "2", r.Code, now.Add(15*time.Minute)); !errors.Is(e, ErrStandardsApprovalRequired) {
		t.Fatalf("expired: %v", e)
	}
	o := conversationTestOperation(t, db, "approval")
	ctx := WithStandardsConversationOperation(t.Context(), o.Key, o.RequestDigest)
	ctx = WithStandardsConversationReviewConsumption(ctx, r.ID, "1", "2", r.Code, now)
	// A failing domain mutation must roll back challenge consumption as well.
	if _, e = db.SaveStandardsAuthoring(ctx, StandardsAuthoringSession{}, 0); e == nil {
		t.Fatal("invalid state saved")
	}
	check, e := db.GetStandardsConversationReview(t.Context(), r.ID)
	if e != nil || !check.ConsumedAt.IsZero() {
		t.Fatalf("failed domain effect consumed gate: %#v %v", check, e)
	}
	if _, e = db.SaveStandardsAuthoring(ctx, StandardsAuthoringSession{ID: "saved", Mode: "create", TargetID: "std", Status: "draft", Data: json.RawMessage(`{}`)}, 0); e != nil {
		t.Fatal(e)
	}
	check, e = db.FindStandardsConversationReviewByCode(t.Context(), "discord", "1", "2", r.Code)
	if e != nil || check.ConsumedAt.IsZero() {
		t.Fatalf("successful effect did not consume gate: %#v %v", check, e)
	}
	if _, e = db.ConsumeStandardsConversationReview(t.Context(), r.ID, "1", "2", r.Code, now); !errors.Is(e, ErrStandardsApprovalRequired) {
		t.Fatalf("consumed twice: %v", e)
	}
	changed := r
	changed.Parts = []string{"altered"}
	changed.Text = "altered"
	if _, e = db.SaveStandardsConversationReview(t.Context(), changed); !errors.Is(e, ErrStandardsConflict) {
		t.Fatalf("manifest changed: %v", e)
	}
}
func TestStandardsConversationNoticeFencesAndRetainsFailures(t *testing.T) {
	db := newTestDB(t)
	d, e := db.SaveStandardsConversationNoticeDelivery(t.Context(), StandardsConversationNoticeDelivery{NoticeID: 9, Transport: "discord", OwnerID: "1", ChannelID: "2", Payload: "original proposal p v1; unapproved at origin"})
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC()
	d, e = db.ClaimStandardsConversationNoticeDelivery(t.Context(), d.NoticeID, d.Transport, d.OwnerID, d.ChannelID, now, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	newer, e := db.ClaimStandardsConversationNoticeDelivery(t.Context(), d.NoticeID, d.Transport, d.OwnerID, d.ChannelID, now.Add(time.Minute), time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	d.Status = "delivered"
	d.RemoteID = "late"
	if _, e = db.FinishStandardsConversationNoticeDelivery(t.Context(), d, now.Add(time.Minute)); !errors.Is(e, ErrStandardsConflict) {
		t.Fatalf("late delivery won fence: %v", e)
	}
	newer.PartsAcknowledged = 1
	newer.Status = "paused"
	newer.LastError = "authorization revoked"
	if _, e = db.FinishStandardsConversationNoticeDelivery(t.Context(), newer, now.Add(time.Minute)); e != nil {
		t.Fatal(e)
	}
	pending, e := db.CountStandardsConversationNoticeBacklog(t.Context(), d.Transport, d.OwnerID, d.ChannelID)
	if e != nil || pending != 1 {
		t.Fatalf("paused failure disappeared: %d %v", pending, e)
	}
	same, e := db.SaveStandardsConversationNoticeDelivery(t.Context(), StandardsConversationNoticeDelivery{NoticeID: 9, Transport: "discord", OwnerID: "1", ChannelID: "2", Payload: d.Payload})
	if e != nil || same.Status != "paused" || same.Attempts != 2 || same.PartsAcknowledged != 1 {
		t.Fatalf("duplicate changed delivery: %#v %v", same, e)
	}
	changed := same
	changed.Payload = "new proposal"
	if _, e = db.SaveStandardsConversationNoticeDelivery(t.Context(), changed); !errors.Is(e, ErrStandardsConflict) {
		t.Fatalf("notice terms changed: %v", e)
	}
}

func TestStandardsConversationTrustedProvenanceRetained(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	provenance := "discord-owner:123:456:789"
	p, e := db.SaveStandardsProposal(ctx, StandardsProposalVersion{ProposalID: "discord", SchemaVersion: 1, Operations: []StandardsOperation{}, ChangeDigest: "change", ResultDigest: "result", Author: "owner"}, 0)
	if e != nil {
		t.Fatal(e)
	}
	a := StandardsApproval{ProposalID: p.ProposalID, Version: p.Version, ChangeDigest: p.ChangeDigest, ResultDigest: p.ResultDigest, Owner: "owner", Provenance: provenance}
	if _, e = db.ApproveStandardsProposal(ctx, a); e != nil {
		t.Fatal(e)
	}
	rev, e := db.PublishStandardsProposalWithProvenance(ctx, p, StandardsSnapshot{Standards: []StandardsStandard{{ID: "s", Title: "Title", Body: "Body", CategoryIDs: []string{}}}}, "owner", "discord-owner:123:456:790")
	if e != nil {
		t.Fatal(e)
	}
	if rev.Publication.Approval.Provenance != provenance || rev.Publication.Provenance != "discord-owner:123:456:790" {
		t.Fatalf("lost attribution: %#v", rev.Publication)
	}
	repo, e := db.UpsertRepo(ctx, Repo{Path: t.TempDir(), Forge: "github", Owner: "owner", Name: "repo"})
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	ex := StandardsExceptionProposal{ID: "discord-exception", OperationKey: "ex", SchemaVersion: 1, Digest: "exact", TermsJSON: `{"terms":"departure"}`, CreatedAt: now, Owner: "owner", BookRevision: rev.ID, StandardID: "s", RepositoryID: repo.ID, ExpiresAt: now.Add(time.Hour)}
	validate := func(StandardsExceptionRecord, StandardsRevision, Repo) error { return nil }
	if _, e = db.CreateStandardsException(ctx, ex, validate); e != nil {
		t.Fatal(e)
	}
	remote := WithStandardsProvenance(ctx, provenance)
	approved, e := db.ApproveStandardsException(remote, ex.ID, ex.Digest, "owner", now, validate)
	if e != nil || approved.Approval == nil || approved.Approval.Provenance != provenance {
		t.Fatalf("approval provenance %#v %v", approved, e)
	}
	revoked, e := db.RevokeStandardsException(remote, ex.ID, "owner", "finished", now)
	if e != nil || revoked.Revocation == nil || revoked.Revocation.Provenance != provenance {
		t.Fatalf("revocation provenance %#v %v", revoked, e)
	}
	if StandardsProvenanceValid("discord-owner:provider:456:789") || StandardsProvenanceValid("discord-owner:123:456:") {
		t.Fatal("untrusted provenance accepted")
	}
}

func TestStandardsConversationFailedGenerationRetainsInterruptedEffect(t *testing.T) {
	db := newTestDB(t)
	s := authoringTestSession(t, db)
	o := conversationTestOperation(t, db, "failed-generation")
	if _, err := db.StartStandardsConversationGeneration(t.Context(), o.Key, o.RequestDigest); err != nil {
		t.Fatal(err)
	}
	a, err := db.BeginStandardsAuthoringAttempt(t.Context(), StandardsAuthoringAttempt{ID: "attempt-failed", SessionID: s.ID, InputVersion: s.Version, Token: "fence", LeaseUntil: time.Now().UTC().Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	a.Outcome = "failed"
	a.Diagnostic = "needs-context: missing evidence"
	s.Status = "needs-context"
	ctx := WithStandardsConversationOperation(t.Context(), o.Key, o.RequestDigest)
	if _, err = db.FinishStandardsAuthoringAttempt(ctx, a, &s); err != nil {
		t.Fatal(err)
	}
	retained, err := db.GetStandardsConversationOperation(t.Context(), o.Key)
	if err != nil {
		t.Fatal(err)
	}
	var effect StandardsConversationEffect
	if err = json.Unmarshal(retained.EffectJSON, &effect); err != nil {
		t.Fatal(err)
	}
	if !retained.EffectCommitted || effect.DomainKind != "author-interrupted" || effect.ID != s.ID || effect.Version != s.Version {
		t.Fatalf("failed generation can look successful: %#v %#v", retained, effect)
	}
	if _, err = db.FinishStandardsConversationOperation(t.Context(), o.Key, o.RequestDigest, "interrupted", nil, a.Diagnostic); err != nil {
		t.Fatal(err)
	}
}

func TestStandardsConversationExpiredChallengeRotationPreservesReview(t *testing.T) {
	db := newTestDB(t)
	r, err := db.SaveStandardsConversationReview(t.Context(), StandardsConversationReview{ID: "review-refresh", Transport: "discord", OwnerID: "1", ChannelID: "2", Kind: "proposal", TargetID: "proposal", TargetVersion: 7, ChangeDigest: "change", ResultDigest: "result", Text: "full before and after", Parts: []string{"full before and after"}, Code: "old-code"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err = db.AcknowledgeStandardsConversationReviewPart(t.Context(), r.ID, r.OwnerID, r.ChannelID, 0, "body"); err != nil {
		t.Fatal(err)
	}
	active, err := db.ActivateStandardsConversationReview(t.Context(), r.ID, r.OwnerID, r.ChannelID, "old-prompt", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.RefreshStandardsConversationReviewChallenge(t.Context(), r.ID, r.OwnerID, r.ChannelID, "new-code", now.Add(time.Minute)); !errors.Is(err, ErrStandardsApprovalRequired) {
		t.Fatalf("live challenge rotated: %v", err)
	}
	expired := now.Add(15 * time.Minute)
	refreshed, err := db.RefreshStandardsConversationReviewChallenge(t.Context(), r.ID, r.OwnerID, r.ChannelID, "new-code", expired)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.ManifestDigest != r.ManifestDigest || refreshed.Text != r.Text || !reflect.DeepEqual(refreshed.PartDigests, r.PartDigests) || !reflect.DeepEqual(refreshed.RemoteIDs, active.RemoteIDs) || !refreshed.ExpiresAt.IsZero() || refreshed.PromptRemoteID != "" {
		t.Fatalf("rotation changed review or kept old gate: %#v", refreshed)
	}
	if _, err = db.FindStandardsConversationReviewByCode(t.Context(), r.Transport, r.OwnerID, r.ChannelID, "old-code"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old challenge still indexed: %v", err)
	}
	found, err := db.FindStandardsConversationReviewByCode(t.Context(), r.Transport, r.OwnerID, r.ChannelID, "new-code")
	if err != nil || found.ManifestDigest != r.ManifestDigest {
		t.Fatalf("new code lost manifest: %#v %v", found, err)
	}
	if _, err = db.ConsumeStandardsConversationReview(t.Context(), r.ID, r.OwnerID, r.ChannelID, "new-code", expired); !errors.Is(err, ErrStandardsApprovalRequired) {
		t.Fatalf("rotated prompt not delivered: %v", err)
	}
	if _, err = db.ActivateStandardsConversationReview(t.Context(), r.ID, r.OwnerID, r.ChannelID, "new-prompt", expired); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ConsumeStandardsConversationReview(t.Context(), r.ID, r.OwnerID, r.ChannelID, "old-code", expired); !errors.Is(err, ErrStandardsApprovalRequired) {
		t.Fatalf("old code approved: %v", err)
	}
	if _, err = db.ConsumeStandardsConversationReview(t.Context(), r.ID, r.OwnerID, r.ChannelID, "new-code", expired); err != nil {
		t.Fatal(err)
	}
	if _, err = db.RefreshStandardsConversationReviewChallenge(t.Context(), r.ID, r.OwnerID, r.ChannelID, "third-code", expired.Add(time.Hour)); !errors.Is(err, ErrStandardsApprovalRequired) {
		t.Fatalf("consumed gate rotated: %v", err)
	}
}
