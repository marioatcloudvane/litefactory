package daemon

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestConversationStandardsAnswerReceiptSurvivesRestartAndStaleVersion(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "factory.db")
	db, err := store.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = db.UpsertIdentity(ctx, store.Identity{Forge: store.ForgeGitHub, Role: store.RoleOwner, Handle: "owner", UserID: 71, Source: store.SourceGH}); err != nil {
		t.Fatal(err)
	}
	book := &standards.Service{Store: db, OwnerID: 71}
	p, err := book.Create(ctx, "seed", 0, []standards.Operation{{Kind: "category.add", Category: &standards.Category{ID: "quality", Name: "Quality"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = book.Approve(ctx, p.ProposalID, p.Version, p.ChangeDigest); err != nil {
		t.Fatal(err)
	}
	if _, err = book.Publish(ctx, p.ProposalID, p.Version, p.ChangeDigest); err != nil {
		t.Fatal(err)
	}
	author := &standards.Authoring{Store: db, Book: book, Runner: &standardsJourneyAuthor{}}
	draft, err := author.Start(ctx, standards.AuthoringStart{ID: "receipt-draft", Mode: "create", Intent: "Relevant checks", CategoryIDs: []string{"quality"}})
	if err != nil {
		t.Fatal(err)
	}
	draft, err = author.Continue(ctx, draft.ID, draft.Version)
	if err != nil {
		t.Fatal(err)
	}
	adapter := &ConversationStandards{DB: db, Book: book, Authoring: author, OwnerID: "7", Authorized: func(context.Context, messaging.StandardsRequest) error { return nil }}
	request := messaging.StandardsRequest{Operation: "author.answer", Arguments: map[string]string{"id": draft.ID, "version": fmt.Sprint(draft.Version), "question_id": draft.Questions[0].ID, "text": "Relevant package checks"}, Transport: "discord", OwnerID: "7", ChannelID: "3", MessageID: "101", OperationKey: "discord:3:101", At: time.Now().UTC()}
	original, err := adapter.Execute(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	answered, err := author.Show(ctx, draft.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if answered.Version != draft.Version+1 {
		t.Fatal("answer did not advance exactly once")
	}
	// A later CLI edit makes both the answer's expected version and its result old.
	rule := "MUST later CLI rule"
	edited, err := author.Edit(ctx, answered.ID, answered.Version, standards.AuthoringEdit{Rule: &rule})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = store.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	book = &standards.Service{Store: db, OwnerID: 71}
	author = &standards.Authoring{Store: db, Book: book}
	adapter = &ConversationStandards{DB: db, Book: book, Authoring: author, OwnerID: "7", Authorized: func(context.Context, messaging.StandardsRequest) error { return nil }}
	recovered, err := adapter.Execute(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	// The first reply also says whether a next draft started; the retained
	// receipt itself is unchanged.
	if !reflect.DeepEqual(recovered.References, original.References) || recovered.Interrupted {
		t.Fatalf("retry replaced original receipt: got %+v want %+v", recovered, original)
	}
	current, err := author.Show(ctx, draft.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if current.Version != edited.Version || current.Draft.Rule != rule {
		t.Fatal("retry changed later shared CLI state")
	}
	request.Arguments["text"] = "All checks"
	if _, err = adapter.Execute(ctx, request); err == nil {
		t.Fatal("same admitted operation accepted changed content")
	}
}
