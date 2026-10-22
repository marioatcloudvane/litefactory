package daemon

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/standardsexceptions"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestConversationStandardsExceptionReviewRemainsExactAcrossClock(t *testing.T) {
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "review.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: "owner", Name: "repo"})
	if err != nil {
		t.Fatal(err)
	}
	book := &standards.Service{Store: db, OwnerID: 42}
	p, err := book.Create(ctx, "review-seed", 0, []standards.Operation{{Kind: "category.add", Category: &standards.Category{ID: "quality", Name: "Quality"}}, {Kind: "standard.add", Standard: &standards.Standard{ID: "standard", Title: "Standard", Body: "Exact guidance", CategoryIDs: []string{"quality"}, ExceptionPolicy: &standards.ExceptionPolicy{Version: 1, Mode: "allow-task", AllRegisteredRepositories: true, MaxLifetimeSeconds: 3600}}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = book.Approve(ctx, p.ProposalID, p.Version, p.ChangeDigest); err != nil {
		t.Fatal(err)
	}
	revision, err := book.Publish(ctx, p.ProposalID, p.Version, p.ChangeDigest)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	exceptions := &standardsexceptions.Service{Store: db, OwnerID: 42, Now: func() time.Time { return now }}
	proposal, err := exceptions.Propose(ctx, "review-exception", standardsexceptions.Request{BookRevision: revision.ID, StandardID: "standard", RepositoryID: repo.ID, TaskRef: "github:owner/repo#42", Departure: "Temporary alternative", Reason: "Migration", ExpiresAt: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	adapter := &ConversationStandards{DB: db, Book: book, Exceptions: exceptions, OwnerID: "7", Authorized: func(context.Context, messaging.StandardsRequest) error { return nil }}
	request := messaging.StandardsRequest{Operation: "exception.review", Arguments: map[string]string{"id": proposal.Proposal.ID}, Transport: "discord", OwnerID: "7", ChannelID: "3", MessageID: "101"}
	first, err := adapter.Review(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	second, err := adapter.Review(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("unchanged exception terms produced a changed review")
	}
	if !strings.Contains(first.Text, "allow-task") || !strings.Contains(first.Text, "Exact guidance") || strings.Contains(first.Text, "evaluated_at") {
		t.Fatalf("review omitted immutable terms or included volatile state: %s", first.Text)
	}
}

func TestConversationStandardsCompactAuthorOutput(t *testing.T) {
	session := standards.AuthoringSession{ID: "draft", Version: 2, Status: "draft", AuthoringState: standards.AuthoringState{Intent: "Intent", Draft: standards.Standard{ID: "s", Title: "Title", Body: "Draft guidance"}, Questions: []standards.AuthoringQuestion{{ID: "q", Text: "Which checks?", State: "pending"}}, Diagnostics: []standards.Problem{{Field: "draft", Message: "Answer required"}}, RoleCapture: json.RawMessage(`{"private":"captured instructions"}`), Base: standards.Snapshot{Standards: []standards.Standard{{ID: "old", Body: "Whole published book"}}}}}
	result, err := standardsResult(session)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Which checks?", "Draft guidance", "Answer required"} {
		if !strings.Contains(result.Text, want) {
			t.Errorf("missing %s", want)
		}
	}
	for _, unwanted := range []string{"captured instructions", "Whole published book", "role_capture"} {
		if strings.Contains(result.Text, unwanted) {
			t.Errorf("exposed %s", unwanted)
		}
	}
	if result.References["question_id"] != "q" || result.References["version"] != "2" {
		t.Fatalf("lost exact references: %+v", result.References)
	}
}

func TestConversationStandardsRecoveryRendersTerminalAndStartedOutcomes(t *testing.T) {
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "recovery.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	adapter := &ConversationStandards{DB: db, OwnerID: "7", Authorized: func(context.Context, messaging.StandardsRequest) error { return nil }}
	for _, status := range []string{"failed", "conflict", "interrupted", "started"} {
		t.Run(status, func(t *testing.T) {
			request := messaging.StandardsRequest{Operation: "author.continue", Arguments: map[string]string{"id": "draft", "version": "2"}, Transport: "discord", OwnerID: "7", ChannelID: "3", MessageID: "101", OperationKey: status}
			raw, err := json.Marshal(struct {
				Operation                                string
				Arguments                                map[string]string
				Transport, OwnerID, ChannelID, MessageID string
			}{request.Operation, request.Arguments, request.Transport, request.OwnerID, request.ChannelID, request.MessageID})
			if err != nil {
				t.Fatal(err)
			}
			_, err = db.AdmitStandardsConversationOperation(ctx, store.StandardsConversationOperation{Key: status, RequestDigest: "digest", Kind: request.Operation, Transport: request.Transport, OwnerID: request.OwnerID, ChannelID: request.ChannelID, SourceID: request.MessageID, RequestJSON: raw})
			if err != nil {
				t.Fatal(err)
			}
			if status == "started" {
				_, err = db.StartStandardsConversationGeneration(ctx, status, "digest")
			} else {
				_, err = db.FinishStandardsConversationOperation(ctx, status, "digest", status, nil, "saved diagnostic")
			}
			if err != nil {
				t.Fatal(err)
			}
			result, found, err := adapter.Recover(ctx, request)
			if err != nil {
				t.Fatal(err)
			}
			if !found || !result.Interrupted || result.Text == "" {
				t.Fatalf("terminal recovery cannot be delivered: %+v found=%v", result, found)
			}
		})
	}
}

func TestConversationStandardsRecoveryRendersFirstDiagnosticEffect(t *testing.T) {
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "diagnostics.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	adapter := &ConversationStandards{DB: db, OwnerID: "7", Authorized: func(context.Context, messaging.StandardsRequest) error { return nil }}
	request := messaging.StandardsRequest{Operation: "author.complete", Arguments: map[string]string{"id": "draft", "version": "1"}, Transport: "discord", OwnerID: "7", ChannelID: "3", MessageID: "101", OperationKey: "diagnostic-effect"}
	raw, err := json.Marshal(struct {
		Operation                                string
		Arguments                                map[string]string
		Transport, OwnerID, ChannelID, MessageID string
	}{request.Operation, request.Arguments, request.Transport, request.OwnerID, request.ChannelID, request.MessageID})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.AdmitStandardsConversationOperation(ctx, store.StandardsConversationOperation{Key: request.OperationKey, RequestDigest: "digest", Kind: request.Operation, Transport: request.Transport, OwnerID: request.OwnerID, ChannelID: request.ChannelID, SourceID: request.MessageID, RequestJSON: raw})
	if err != nil {
		t.Fatal(err)
	}
	effect := store.WithStandardsConversationOperation(ctx, request.OperationKey, "digest")
	if _, err = db.SaveStandardsAuthoring(effect, store.StandardsAuthoringSession{ID: "draft", Mode: "create", TargetID: "s", Status: "draft", Data: json.RawMessage(`{}`)}, 0); err != nil {
		t.Fatal(err)
	}
	result, found, err := adapter.Recover(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if !found || !result.Interrupted || !strings.Contains(result.Text, "diagnostics") {
		t.Fatalf("diagnostic effect misreported: %+v", result)
	}
	receipt, err := db.GetStandardsConversationOperation(ctx, request.OperationKey)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Status != "failed" {
		t.Fatalf("diagnostics claimed completion: %+v", receipt)
	}
}
