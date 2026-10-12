package turn

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

type publicationTicketsFixture struct {
	snapshot    ticket.Snapshot
	reply       ticket.Reply
	posts       int
	lost        bool
	complete    bool
	botID       string
	hidden      bool
	readErr     error
	returnedRef ticket.Ref
	postedRef   ticket.Ref
}

func (f *publicationTicketsFixture) Resolve(_ context.Context, l ticket.Locator) (ticket.Snapshot, error) {
	if l != f.snapshot.Ref.Locator {
		return ticket.Snapshot{}, store.ErrTicketIdentityConflict
	}
	if f.returnedRef != (ticket.Ref{}) {
		out := f.snapshot
		out.Ref = f.returnedRef
		return out, nil
	}
	return f.snapshot, nil
}
func (f *publicationTicketsFixture) Read(_ context.Context, r ticket.Ref) (ticket.Snapshot, error) {
	if !r.Equal(f.snapshot.Ref) {
		return ticket.Snapshot{}, store.ErrTicketIdentityConflict
	}
	if f.returnedRef != (ticket.Ref{}) {
		out := f.snapshot
		out.Ref = f.returnedRef
		return out, nil
	}
	return f.snapshot, nil
}
func (f *publicationTicketsFixture) PublishReply(_ context.Context, r ticket.Ref, body redact.Body) (ticket.Reply, error) {
	if !r.Equal(f.snapshot.Ref) {
		return ticket.Reply{}, store.ErrTicketIdentityConflict
	}
	f.posts++
	botID := f.botID
	if botID == "" {
		botID = "77"
	}
	f.reply = ticket.Reply{Ticket: r, Identity: ticket.Identity{Provider: r.Identity.Provider, Origin: r.Identity.Origin, RemoteID: "5001"}, Author: ticket.Actor{Identity: ticket.Identity{Provider: r.Identity.Provider, Origin: r.Identity.Origin, RemoteID: botID}}, Body: body.String(), BrowserURL: "https://enterprise.example/owner/repo/issues/123#issuecomment-5001"}
	if f.postedRef != (ticket.Ref{}) {
		f.reply.Ticket = f.postedRef
	}
	if f.lost {
		return ticket.Reply{}, errors.New("reply lost after dispatch")
	}
	return f.reply, nil
}
func (f *publicationTicketsFixture) RepliesAfter(_ context.Context, r ticket.Ref, _ ticket.Cursor) (ticket.ReplyPage, error) {
	if f.readErr != nil {
		return ticket.ReplyPage{}, f.readErr
	}
	if !r.Equal(f.snapshot.Ref) {
		return ticket.ReplyPage{}, store.ErrTicketIdentityConflict
	}
	out := ticket.ReplyPage{Complete: f.complete}
	if f.reply.Identity.RemoteID != "" && !f.hidden {
		out.Replies = []ticket.Reply{f.reply}
	}
	return out, nil
}
func (f *publicationTicketsFixture) RepliesPage(ctx context.Context, r ticket.Ref, _ int, _ int) (ticket.ReplyPage, error) {
	return f.RepliesAfter(ctx, r, ticket.Cursor{})
}
func (f *publicationTicketsFixture) EditReply(_ context.Context, r ticket.Reply, _ redact.Body) error {
	if !r.Identity.Equal(f.reply.Identity) || !r.Ticket.Equal(f.reply.Ticket) {
		return store.ErrTicketIdentityConflict
	}
	return nil
}
func (f *publicationTicketsFixture) React(_ context.Context, r ticket.Reply, _ string) error {
	if !r.Identity.Equal(f.reply.Identity) || !r.Ticket.Equal(f.reply.Ticket) {
		return store.ErrTicketIdentityConflict
	}
	return nil
}

func TestTicketPublicationRetainsPendingReportAcrossRestartAndIdentityConflicts(t *testing.T) {
	f := newFixture(t, newStubRunner(nil))
	origin := ticket.Origin("https://enterprise.example/api/v3")
	ref := f.ref()
	neutral := &publicationTicketsFixture{lost: true, complete: true, snapshot: ticket.Snapshot{Ref: ticket.Ref{Identity: ticket.Identity{Provider: ticket.GitHub, Origin: origin, RemoteID: "700"}, Locator: ticket.Locator{Provider: ticket.GitHub, Origin: origin, Container: ref.Owner + "/" + ref.Repo, Key: "123"}}}}
	bridge, err := NewTicketPublicationBridge(f.db, neutral, origin)
	if err != nil {
		t.Fatal(err)
	}
	reporter := newReporter(t, f, newFakeForge(t), nil)
	reporter.forge = bridge
	request := ReplyRequest{Ref: ref, SessionID: f.session.ID, CommentID: 22, Command: ParseCommand("/unknown command")}
	if _, err = reporter.ReportReply(t.Context(), request); err == nil {
		t.Fatal("lost response claimed success")
	}
	original, found, err := f.db.FindPendingReportDelivery(t.Context(), f.session.ID, 22)
	if err != nil || !found || !original.PossiblySent {
		t.Fatalf("pending intent: %+v %v", original, err)
	}
	if watermark(t, f) != 0 || neutral.posts != 1 {
		t.Fatal("uncertain response consumed source or repeated send")
	}
	// Reopening the store and reconstructing dependencies must recover prepared
	// bytes rather than rerendering or trusting current display/account handles.
	if err = f.db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(t.Context(), f.paths.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	*f.db = *reopened
	if err = f.db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	bridge, err = NewTicketPublicationBridge(f.db, neutral, origin)
	if err != nil {
		t.Fatal(err)
	}
	reporter.forge = bridge
	reporter.renderer = stubRenderer{err: errors.New("retained bytes must not rerender")}
	retained, err := f.db.GetReportDelivery(t.Context(), original.Key)
	if err != nil || !reflect.DeepEqual(retained, original) {
		t.Fatalf("pending payload changed: %v", err)
	}
	neutral.complete = false
	if _, _, err = reporter.ReportPending(t.Context(), f.session.ID, 22); err == nil {
		t.Fatal("partial observation confirmed pending reply")
	}
	neutral.complete = true
	originalOrigin := neutral.reply.Author.Identity.Origin
	neutral.reply.Author.Identity.Origin = "https://other.example/api/v3"
	if _, _, err = reporter.ReportPending(t.Context(), f.session.ID, 22); err == nil {
		t.Fatal("colliding foreign actor granted receipt")
	}
	neutral.reply.Author.Identity.Origin = originalOrigin
	neutral.snapshot.Ref.Identity.RemoteID = "701"
	if _, _, err = reporter.ReportPending(t.Context(), f.session.ID, 22); !errors.Is(err, store.ErrTicketIdentityConflict) {
		t.Fatalf("changed destination not refused: %v", err)
	}
	neutral.snapshot.Ref.Identity.RemoteID = "700"
	foreignBridge, err := NewTicketPublicationBridge(f.db, neutral, "https://other.example/api/v3")
	if err != nil {
		t.Fatal(err)
	}
	reporter.forge = foreignBridge
	if _, _, err = reporter.ReportPending(t.Context(), f.session.ID, 22); !errors.Is(err, store.ErrTicketIdentityConflict) {
		t.Fatalf("configured origin rebound retained effect: %v", err)
	}
	reporter.forge = bridge
	posted, pending, err := reporter.ReportPending(t.Context(), f.session.ID, 22)
	if err != nil || !pending || posted.CommentID != 5001 {
		t.Fatalf("qualified recovery: %+v %v", posted, err)
	}
	if neutral.posts != 1 || watermark(t, f) != 22 || neutral.reply.Body != original.Body {
		t.Fatal("restart duplicated dispatch, changed bytes or missed confirmation")
	}
	if err = bridge.EditComment(t.Context(), github.RepoFromRef(ref), 5001, redact.Body{}); err != nil {
		t.Fatal(err)
	}
	if err = bridge.EditComment(t.Context(), github.RepoFromRef(ref), 5002, redact.Body{}); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("unknown numeric reply selected an edit destination")
	}
}

func TestTicketCommentDispatchRetainsOneRightAndQualifiedReceipt(t *testing.T) {
	f := commentHTTPSetup(t)
	origin := ticket.Origin("https://enterprise.example/api/v3")
	container := f.receipts.scope.Repository
	neutral := &publicationTicketsFixture{lost: true, complete: true, botID: "9", snapshot: ticket.Snapshot{Ref: ticket.Ref{Identity: ticket.Identity{Provider: ticket.GitHub, Origin: origin, RemoteID: "28400"}, Locator: ticket.Locator{Provider: ticket.GitHub, Origin: origin, Container: container, Key: "284"}}, Author: ticket.Actor{Identity: ticket.Identity{Provider: ticket.GitHub, Origin: origin, RemoteID: "44"}}}}
	bridge, err := NewTicketPublicationBridge(f.db, neutral, origin)
	if err != nil {
		t.Fatal(err)
	}
	f.service.forge = bridge
	args := []byte(`{"issue_number":284,"message":"prepared status"}`)
	first, err := f.client.Call(t.Context(), "issues_comment", args)
	if err != nil {
		t.Fatal(err)
	}
	if first.Error == "" {
		t.Fatal("lost response confirmed publication")
	}
	original, err := f.db.GetCommentOperation(t.Context(), f.receipts.scope, first.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.client.Call(t.Context(), "issues_comment", args)
	if err != nil {
		t.Fatal(err)
	}
	if second.RequestID != first.RequestID || neutral.posts != 1 {
		t.Fatal("retry recovered another dispatch right")
	}
	retained, err := f.db.GetCommentOperation(t.Context(), f.receipts.scope, first.RequestID)
	if err != nil || !reflect.DeepEqual(original, retained) {
		t.Fatal("retry changed prepared bytes or pending intent")
	}
	authorize := func(_ context.Context, op store.CommentOperation) error {
		if op.Scope != original.Scope || op.RequestID != original.RequestID || op.Body != original.Body || op.BotID != original.BotID {
			return store.ErrResultAuthority
		}
		return nil
	}
	neutral.reply.Author.Identity.Origin = "https://other.example/api/v3"
	if _, err := f.service.ReconcileComment(t.Context(), original.Scope, original.RequestID, 1, authorize); !errors.Is(err, store.ErrTicketIdentityConflict) {
		t.Fatalf("foreign bot receipt granted confirmation: %v", err)
	}
	neutral.reply.Author.Identity.Origin = origin
	confirmed, err := f.service.ReconcileComment(t.Context(), original.Scope, original.RequestID, 1, authorize)
	if err != nil || confirmed.State != "confirmed" || confirmed.CommentID != 5001 || neutral.posts != 1 || confirmed.Body != original.Body {
		t.Fatalf("qualified receipt recovery: %+v %v", confirmed, err)
	}
}

func TestStoryAcknowledgementRetainsUncertainDispatchAcrossHiddenObservationAndRestart(t *testing.T) {
	f := newFixture(t, newStubRunner(nil))
	ref := f.ref()
	const source = 401
	if err := f.db.AdmitStoryRefinement(t.Context(), store.StoryRefinement{SessionID: f.session.ID, CommentID: source, ChildNumber: 200, ChildID: 2000, Generation: 1, ActorID: 42, Body: "refine"}); err != nil {
		t.Fatal(err)
	}
	origin := ticket.Origin("https://enterprise.example/api/v3")
	neutral := &publicationTicketsFixture{lost: true, complete: true, snapshot: ticket.Snapshot{Ref: ticket.Ref{Identity: ticket.Identity{Provider: ticket.GitHub, Origin: origin, RemoteID: "2000"}, Locator: ticket.Locator{Provider: ticket.GitHub, Origin: origin, Container: ref.Owner + "/" + ref.Repo, Key: "200"}}}}
	bridge, err := NewTicketPublicationBridge(f.db, neutral, origin)
	if err != nil {
		t.Fatal(err)
	}
	reporter := newReporter(t, f, newFakeForge(t), nil)
	reporter.forge = bridge
	reporter.storyLockDir = f.paths.StoryProgressLocksDir()
	if _, err := NewReporter(ReporterOptions{DB: f.db, Forge: bridge, Renderer: reporter.renderer, Logger: f.opts.Logger, BotUserID: 77, StoryLockDir: reporter.storyLockDir}); err != nil {
		t.Fatal(err)
	}
	if err := reporter.AcknowledgeStoryRefinement(t.Context(), ref, f.session.ID, source); err == nil {
		t.Fatal("lost response claimed acknowledged")
	}
	original, err := f.db.GetStoryRefinementACK(t.Context(), f.session.ID, source)
	if err != nil || !original.DispatchAttempted || original.AcknowledgementID != 0 || original.PreparedBody != neutral.reply.Body {
		t.Fatalf("uncertain ACK lost: %+v %v", original, err)
	}
	neutral.hidden = true
	if err := reporter.AcknowledgeStoryRefinement(t.Context(), ref, f.session.ID, source); !errors.Is(err, ErrReportReceiptUncertain) {
		t.Fatalf("empty observation reclaimed dispatch: %v", err)
	}
	if err = f.db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(t.Context(), f.paths.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	*f.db = *reopened
	if err = f.db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	bridge, err = NewTicketPublicationBridge(f.db, neutral, origin)
	if err != nil {
		t.Fatal(err)
	}
	reporter.forge = bridge
	reporter.renderer = stubRenderer{err: errors.New("must not rerender retained ACK")}
	neutral.readErr = errors.New("bounded observation failed")
	if err = reporter.AcknowledgeStoryRefinement(t.Context(), ref, f.session.ID, source); err == nil {
		t.Fatal("failed read confirmed ACK")
	}
	neutral.readErr = nil
	if err = reporter.AcknowledgeStoryRefinement(t.Context(), ref, f.session.ID, source); !errors.Is(err, ErrReportReceiptUncertain) {
		t.Fatalf("restart reclaimed dispatch: %v", err)
	}
	retained, err := f.db.GetStoryRefinementACK(t.Context(), f.session.ID, source)
	if err != nil || !reflect.DeepEqual(original, retained) {
		t.Fatal("restart changed retained body/dispatch intent")
	}
	pending, err := f.db.GetStoryRefinement(t.Context(), f.session.ID, source)
	if err != nil || pending.Consumed || watermark(t, f) != 0 || neutral.posts != 1 {
		t.Fatal("uncertainty advanced source or sent duplicate")
	}
	neutral.hidden = false
	neutral.reply.Author.Identity.Origin = "https://other.example/api/v3"
	if err = reporter.AcknowledgeStoryRefinement(t.Context(), ref, f.session.ID, source); err == nil {
		t.Fatal("colliding bot from foreign origin confirmed ACK")
	}
	neutral.reply.Author.Identity.Origin = origin
	if err = reporter.AcknowledgeStoryRefinement(t.Context(), ref, f.session.ID, source); err != nil {
		t.Fatal(err)
	}
	confirmed, err := f.db.GetStoryRefinementACK(t.Context(), f.session.ID, source)
	if err != nil || confirmed.AcknowledgementID != 5001 || confirmed.PreparedBody != original.PreparedBody || neutral.posts != 1 {
		t.Fatalf("qualified ACK recovery failed: %+v %v", confirmed, err)
	}
	pending, err = f.db.GetStoryRefinement(t.Context(), f.session.ID, source)
	if err != nil || pending.Consumed || watermark(t, f) != 0 {
		t.Fatal("acknowledgement consumed refinement discussion")
	}
	if err = reporter.AcknowledgeStoryRefinement(t.Context(), ref, f.session.ID, source); err != nil {
		t.Fatal("confirmed ACK rerendered or retried")
	}
}

func TestTicketPublicationRefusesForeignReceiptBeforeRetainingReplyMetadata(t *testing.T) {
	f := newFixture(t, newStubRunner(nil))
	origin := ticket.Origin("https://enterprise.example/api/v3")
	ref := f.ref()
	good := ticket.Ref{Identity: ticket.Identity{Provider: ticket.GitHub, Origin: origin, RemoteID: "700"}, Locator: ticket.Locator{Provider: ticket.GitHub, Origin: origin, Container: ref.Owner + "/" + ref.Repo, Key: "123"}}
	neutral := &publicationTicketsFixture{complete: true, snapshot: ticket.Snapshot{Ref: good}}
	bridge, err := NewTicketPublicationBridge(f.db, neutral, origin)
	if err != nil {
		t.Fatal(err)
	}
	foreign := good
	foreign.Locator.Key = "200"
	neutral.reply = ticket.Reply{Ticket: foreign, Identity: ticket.Identity{Provider: ticket.GitHub, Origin: origin, RemoteID: "5001"}, Author: ticket.Actor{Identity: ticket.Identity{Provider: ticket.GitHub, Origin: origin, RemoteID: "77"}}, Body: "foreign"}
	if _, err = bridge.ListCommentsAfter(t.Context(), github.RepoFromRef(ref), 123, 0); !errors.Is(err, store.ErrTicketIdentityConflict) {
		t.Fatalf("foreign reply read accepted: %v", err)
	}
	if _, err = bridge.ListCommentsPage(t.Context(), github.RepoFromRef(ref), 123, 1, 100); !errors.Is(err, store.ErrTicketIdentityConflict) {
		t.Fatalf("foreign page reply accepted: %v", err)
	}
	if _, err = f.db.GetTicketEffect(t.Context(), publicationReplyKey(good.Locator.Container, "5001")); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("refused reply observation left mutation metadata")
	}
	neutral.postedRef = foreign
	if _, err = bridge.PostComment(t.Context(), github.RepoFromRef(ref), 123, redact.Body{}); !errors.Is(err, store.ErrTicketIdentityConflict) {
		t.Fatalf("foreign POST receipt accepted: %v", err)
	}
	if _, err = f.db.GetTicketEffect(t.Context(), publicationReplyKey(good.Locator.Container, "5001")); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("refused POST receipt left mutation metadata")
	}
}

func TestTicketPublicationRefusesForeignSnapshotBeforeBindingAnotherSession(t *testing.T) {
	f := newFixture(t, newStubRunner(nil))
	origin := ticket.Origin("https://enterprise.example/api/v3")
	ref := f.ref()
	child, err := f.db.GetOrCreateSession(t.Context(), f.storeRepo, 200)
	if err != nil {
		t.Fatal(err)
	}
	good := ticket.Ref{Identity: ticket.Identity{Provider: ticket.GitHub, Origin: origin, RemoteID: "700"}, Locator: ticket.Locator{Provider: ticket.GitHub, Origin: origin, Container: ref.Owner + "/" + ref.Repo, Key: "123"}}
	foreign := good
	foreign.Locator.Key = "200"
	neutral := &publicationTicketsFixture{complete: true, snapshot: ticket.Snapshot{Ref: good}, returnedRef: foreign}
	bridge, err := NewTicketPublicationBridge(f.db, neutral, origin)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = bridge.GetIssue(t.Context(), github.RepoFromRef(ref), 123); !errors.Is(err, store.ErrTicketIdentityConflict) {
		t.Fatalf("foreign snapshot accepted: %v", err)
	}
	if _, err = f.db.GetSessionTicket(t.Context(), child.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("refused snapshot bound unrelated session")
	}
}
