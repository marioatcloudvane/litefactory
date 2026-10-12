package turn

import (
	"errors"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/ticket"
	"testing"
)

func TestTicketNoticeLostReceiptNeverRepeatsDispatch(t *testing.T) {
	f := newFixture(t, newStubRunner(nil))
	origin := ticket.Origin("https://enterprise.example/api/v3")
	neutral := &publicationTicketsFixture{lost: true, complete: true, snapshot: ticket.Snapshot{Ref: ticket.Ref{Identity: ticket.Identity{Provider: ticket.GitHub, Origin: origin, RemoteID: "700"}, Locator: ticket.Locator{Provider: ticket.GitHub, Origin: origin, Container: f.ref().Owner + "/" + f.ref().Repo, Key: "123"}}}}
	bridge, err := NewTicketPublicationBridge(f.db, neutral, origin)
	if err != nil {
		t.Fatal(err)
	}
	marker := "<!-- original-notice -->"
	body, err := redact.New(redact.Options{}).Apply(t.Context(), "prepared\n"+marker, github.CommentSizeLimit)
	if err != nil {
		t.Fatal(err)
	}
	post := func() error {
		_, err := bridge.PostNotice(t.Context(), marker, f.ref().String(), 77, github.RepoFromRef(f.ref()), 123, body)
		return err
	}
	if err = post(); err == nil || neutral.posts != 1 {
		t.Fatalf("lost receipt: posts=%d err=%v", neutral.posts, err)
	}
	neutral.hidden = true
	if err = post(); err == nil || neutral.posts != 1 {
		t.Fatalf("hidden reply repeated: posts=%d err=%v", neutral.posts, err)
	}
	neutral.hidden = false
	originalActor := neutral.reply.Author.Identity
	neutral.reply.Author.Identity.Origin = "https://collision.example/api"
	if err = post(); err == nil || neutral.posts != 1 {
		t.Fatalf("colliding bot accepted: posts=%d err=%v", neutral.posts, err)
	}
	neutral.reply.Author.Identity = originalActor
	if err = post(); err != nil || neutral.posts != 1 {
		t.Fatalf("receipt recovery: posts=%d err=%v", neutral.posts, err)
	}
	if watermark(t, f) != 0 {
		t.Fatal("notice companion advanced original source")
	}
	changed, err := redact.New(redact.Options{}).Apply(t.Context(), "changed\n"+marker, github.CommentSizeLimit)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = bridge.PostNotice(t.Context(), marker, f.ref().String(), 77, github.RepoFromRef(f.ref()), 123, changed); err == nil || neutral.posts != 1 {
		t.Fatalf("changed prepared body accepted: %v", err)
	}
}

func TestTicketNoticeHistoricalReceiptAndPredispatchFailure(t *testing.T) {
	for _, historical := range []bool{true, false} {
		t.Run(map[bool]string{true: "known receipt", false: "failed initial observation"}[historical], func(t *testing.T) {
			f := newFixture(t, newStubRunner(nil))
			origin := ticket.Origin("https://enterprise.example/api/v3")
			ref := ticket.Ref{Identity: ticket.Identity{Provider: ticket.GitHub, Origin: origin, RemoteID: "700"}, Locator: ticket.Locator{Provider: ticket.GitHub, Origin: origin, Container: f.ref().Owner + "/" + f.ref().Repo, Key: "123"}}
			neutral := &publicationTicketsFixture{complete: true, snapshot: ticket.Snapshot{Ref: ref}}
			bridge, err := NewTicketPublicationBridge(f.db, neutral, origin)
			if err != nil {
				t.Fatal(err)
			}
			marker := "<!-- preserved-notice -->"
			body, err := redact.New(redact.Options{}).Apply(t.Context(), "prepared\n"+marker, github.CommentSizeLimit)
			if err != nil {
				t.Fatal(err)
			}
			if historical {
				neutral.reply = ticket.Reply{Ticket: ref, Identity: ticket.Identity{Provider: ticket.GitHub, Origin: origin, RemoteID: "5001"}, Author: ticket.Actor{Identity: ticket.Identity{Provider: ticket.GitHub, Origin: origin, RemoteID: "77"}}, Body: body.String()}
			} else {
				neutral.readErr = errors.New("observation unavailable")
			}
			post := func() error {
				_, err := bridge.PostNotice(t.Context(), marker, f.ref().String(), 77, github.RepoFromRef(f.ref()), 123, body)
				return err
			}
			err = post()
			if historical {
				if err != nil || neutral.posts != 0 {
					t.Fatalf("historical receipt resent: %d %v", neutral.posts, err)
				}
			} else {
				if err == nil || neutral.posts != 0 {
					t.Fatalf("failed observation sent: %d %v", neutral.posts, err)
				}
				neutral.readErr = nil
				if err = post(); err != nil || neutral.posts != 1 {
					t.Fatalf("retry lost unsent dispatch: %d %v", neutral.posts, err)
				}
			}
			neutral.reply.Body = "changed remote content\n" + marker
			if err = post(); err == nil {
				t.Fatal("altered notice accepted")
			}
		})
	}
}
