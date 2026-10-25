package github_test

import (
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/ticket"
)

func ticketLocator(a *github.Tickets) ticket.Locator {
	return ticket.Locator{Provider: ticket.GitHub, Origin: a.Origin(), Container: testRepo.String(), Key: "7"}
}

func ticketJSON(base string, id int64, title, content string) string {
	return fmt.Sprintf(`{"id":%d,"number":7,"url":%q,"title":%q,"body":%q,"state":"open","user":{"id":42,"login":"owner"},"assignees":[{"id":42,"login":"renamed"}]}`, id, base+"/repos/"+testRepo.String()+"/issues/7", title, content)
}

func replyJSON(base string, id int64) string {
	return fmt.Sprintf(`{"id":%d,"url":%q,"issue_url":%q,"body":"/implement continue","user":{"id":42,"login":"renamed"}}`, id, base+"/repos/"+testRepo.String()+fmt.Sprintf("/issues/comments/%d", id), base+"/repos/"+testRepo.String()+"/issues/7")
}

func newTicketAdapter(t *testing.T, f *fixture) *github.Tickets {
	t.Helper()
	a, err := github.NewTickets(f.Client)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestTicketAdapterQualifiedResolutionAndNumericReplyOrder(t *testing.T) {
	t.Parallel()
	var base string
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/"+testRepo.String()+"/issues/7/comments" {
			respondJSON(http.StatusOK, "["+replyJSON(base, 100)+","+replyJSON(base, 9)+","+replyJSON(base, 10)+"]")(w, r)
			return
		}
		respondJSON(http.StatusOK, ticketJSON(base, 700, "Ticket", "body"))(w, r)
	})
	base = f.Server.URL
	a := newTicketAdapter(t, f)
	snapshot, err := a.Resolve(t.Context(), ticketLocator(a))
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Ref.Identity.RemoteID != "700" || snapshot.DisplayKey != "7" {
		t.Fatalf("identity conflates number and database ID: %+v", snapshot)
	}
	if !snapshot.AssignedTo(snapshot.Author.Identity) {
		t.Fatal("qualified owner assignment lost")
	}
	cursor, err := github.ReplyCursor(snapshot.Ref, 9)
	if err != nil {
		t.Fatal(err)
	}
	replies, err := a.RepliesAfter(t.Context(), snapshot.Ref, cursor)
	if err != nil {
		t.Fatal(err)
	}
	if !replies.Complete || len(replies.Replies) != 2 || replies.Replies[0].Identity.RemoteID != "10" || replies.Replies[1].Identity.RemoteID != "100" || replies.Next.Value != "100" {
		t.Fatalf("numeric order/watermark changed: %+v", replies)
	}
	for _, reply := range replies.Replies {
		if !reply.Ticket.Equal(snapshot.Ref) || !reply.Author.Identity.Equal(snapshot.Author.Identity) {
			t.Fatal("reply identity qualification lost")
		}
	}
}

func TestTicketAdapterPreservesObservedReplyParentQualification(t *testing.T) {
	t.Parallel()
	f := newFixture(t, func(http.ResponseWriter, *http.Request) { t.Fatal("unexpected request") })
	a := newTicketAdapter(t, f)
	ref := ticket.Ref{Identity: ticket.Identity{Provider: ticket.GitHub, Origin: a.Origin(), RemoteID: "700"}, Locator: ticketLocator(a)}
	observed := github.Comment{ID: 123, Author: github.User{ID: 42}, APIURL: f.Server.URL + "/repos/" + testRepo.String() + "/issues/comments/123", IssueAPIURL: f.Server.URL + "/repos/" + testRepo.String() + "/issues/7"}
	for _, parent := range []int64{0, 100} {
		observed.ReplyToCommentID = parent
		reply, err := a.QualifyReply(ref, observed)
		if err != nil {
			t.Fatal(err)
		}
		if parent == 0 {
			if reply.ReplyTo.RemoteID != "" {
				t.Fatal("unthreaded observation acquired a parent")
			}
		} else if !reply.ReplyTo.Equal(ticket.Identity{Provider: ticket.GitHub, Origin: a.Origin(), RemoteID: "100"}) {
			t.Fatalf("parent lost provider qualification: %+v", reply.ReplyTo)
		}
	}
}

func TestTicketAdapterRefusesCollidingDestinationBeforeMutation(t *testing.T) {
	t.Parallel()
	var base string
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("unauthorized mutation: %s", r.Method)
		}
		respondJSON(http.StatusOK, ticketJSON(base, 700, "Ticket", "body"))(w, r)
	})
	base = f.Server.URL
	a := newTicketAdapter(t, f)
	snapshot, err := a.Resolve(t.Context(), ticketLocator(a))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*ticket.Ref)
	}{
		{"other origin", func(ref *ticket.Ref) {
			ref.Identity.Origin = "https://other.example"
			ref.Locator.Origin = ref.Identity.Origin
		}},
		{"other provider", func(ref *ticket.Ref) { ref.Identity.Provider = "other"; ref.Locator.Provider = ref.Identity.Provider }},
		{"different immutable ID", func(ref *ticket.Ref) { ref.Identity.RemoteID = "701" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ref := snapshot.Ref
			tc.change(&ref)
			if _, err := a.PublishReply(t.Context(), ref, body(t, "prepared")); err == nil {
				t.Fatal("colliding identity authorized a mutation")
			}
		})
	}
}

func TestTicketAdapterRejectsResponseProvenance(t *testing.T) {
	t.Parallel()
	f := newFixture(t, respondJSON(http.StatusOK, ticketJSON("https://other.example", 700, "Ticket", "body")))
	a := newTicketAdapter(t, f)
	_, err := a.Resolve(t.Context(), ticketLocator(a))
	var failure *ticket.Failure
	if !errors.As(err, &failure) || failure.Kind != ticket.Conflict {
		t.Fatalf("wrong response origin: %v", err)
	}
}

func TestTicketAdapterLostWriteResponseRetainsUncertainty(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		status   int
		response string
		want     ticket.FailureKind
	}{
		{"server failure", http.StatusBadGateway, `{"message":"lost response"}`, ticket.Uncertain},
		{"invalid receipt", http.StatusCreated, `{"id":123,"url":"https://other.example/reply"}`, ticket.Uncertain},
		{"credential refusal", http.StatusForbidden, `{"message":"forbidden"}`, ticket.Refused},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var base string
			posts := 0
			f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					posts++
					respondJSON(tc.status, tc.response)(w, r)
					return
				}
				respondJSON(http.StatusOK, ticketJSON(base, 700, "Ticket", "body"))(w, r)
			})
			base = f.Server.URL
			a := newTicketAdapter(t, f)
			snapshot, err := a.Resolve(t.Context(), ticketLocator(a))
			if err != nil {
				t.Fatal(err)
			}
			_, err = a.PublishReply(t.Context(), snapshot.Ref, body(t, "prepared"))
			var failure *ticket.Failure
			if !errors.As(err, &failure) || failure.Kind != tc.want || posts != 1 {
				t.Fatalf("write failure %v; posts %d", err, posts)
			}
			if tc.status == http.StatusForbidden && !errors.Is(err, github.ErrForbidden) {
				t.Fatal("original error lost")
			}
		})
	}
}

func TestTicketAdapterContentConflictDoesNotDispatch(t *testing.T) {
	t.Parallel()
	var base string
	posts := 0
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			posts++
		}
		respondJSON(http.StatusOK, ticketJSON(base, 700, "Current", "current"))(w, r)
	})
	base = f.Server.URL
	a := newTicketAdapter(t, f)
	snapshot, err := a.Resolve(t.Context(), ticketLocator(a))
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.UpdateContent(t.Context(), snapshot.Ref, "old revision", body(t, "new title"), body(t, "new body"))
	var failure *ticket.Failure
	if !errors.As(err, &failure) || failure.Kind != ticket.Conflict || posts != 0 {
		t.Fatalf("conflict %v; mutations %d", err, posts)
	}
}

func TestTicketAdapterMutationBodiesAreRedacted(t *testing.T) {
	t.Parallel()
	typ := reflect.TypeOf((*github.Tickets)(nil))
	for _, tc := range []struct {
		name      string
		positions []int
	}{
		{"PublishReply", []int{3}}, {"EditReply", []int{3}}, {"Create", []int{3, 4}}, {"UpdateContent", []int{4, 5}},
	} {
		method, ok := typ.MethodByName(tc.name)
		if !ok {
			t.Fatalf("missing method %s", tc.name)
		}
		for _, position := range tc.positions {
			if method.Type.In(position) != reflect.TypeOf(redact.Body{}) {
				t.Fatalf("%s accepts unredacted body", tc.name)
			}
		}
	}
}

func TestTicketAdapterCursorCannotCrossTickets(t *testing.T) {
	t.Parallel()
	var base string
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		respondJSON(http.StatusOK, ticketJSON(base, 700, "Ticket", "body"))(w, r)
	})
	base = f.Server.URL
	a := newTicketAdapter(t, f)
	snapshot, err := a.Resolve(t.Context(), ticketLocator(a))
	if err != nil {
		t.Fatal(err)
	}
	cursor, err := github.ReplyCursor(snapshot.Ref, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*ticket.Cursor)
	}{
		{"ticket", func(c *ticket.Cursor) { c.Ticket.RemoteID = "701" }},
		{"origin", func(c *ticket.Cursor) { c.Ticket.Origin = "https://other.example" }},
		{"unknown version", func(c *ticket.Cursor) { c.Kind = "github-comments-after/v2" }},
		{"opaque ID", func(c *ticket.Cursor) { c.Value = "opaque" }},
		{"noncanonical number", func(c *ticket.Cursor) { c.Value = "01" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := cursor
			tc.change(&changed)
			if _, err := a.RepliesAfter(t.Context(), snapshot.Ref, changed); err == nil {
				t.Fatal("accepted cursor from another identity/format")
			}
		})
	}
	for _, r := range f.Requests() {
		if r.Path == "/repos/"+testRepo.String()+"/issues/7/comments" {
			t.Fatal("invalid cursor reached reply endpoint")
		}
	}
}

func TestTicketAdapterContentReadbackFailureIsUncertain(t *testing.T) {
	t.Parallel()
	var base string
	patched := false
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			patched = true
			respondJSON(http.StatusOK, ticketJSON(base, 700, "New", "new body"))(w, r)
			return
		}
		if patched {
			respondJSON(http.StatusBadGateway, `{"message":"readback unavailable"}`)(w, r)
			return
		}
		respondJSON(http.StatusOK, ticketJSON(base, 700, "Old", "old body"))(w, r)
	})
	base = f.Server.URL
	a := newTicketAdapter(t, f)
	snapshot, err := a.Resolve(t.Context(), ticketLocator(a))
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.UpdateContent(t.Context(), snapshot.Ref, snapshot.Revision, body(t, "New"), body(t, "new body"))
	var failure *ticket.Failure
	if !errors.As(err, &failure) || failure.Kind != ticket.Uncertain || !patched {
		t.Fatalf("failed readback: %v; dispatched %v", err, patched)
	}
	count := 0
	for _, request := range f.Requests() {
		if request.Method == http.MethodPatch {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("write retried %d times", count)
	}
}

func TestTicketAdapterReplyMutationRejectsWrongMembership(t *testing.T) {
	t.Parallel()
	var base string
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("unverified reply mutation: %s", r.Method)
		}
		if r.URL.Path == "/repos/"+testRepo.String()+"/issues/7/comments" {
			respondJSON(http.StatusOK, "["+replyJSON(base, 10)+"]")(w, r)
			return
		}
		respondJSON(http.StatusOK, ticketJSON(base, 700, "Ticket", "body"))(w, r)
	})
	base = f.Server.URL
	a := newTicketAdapter(t, f)
	snapshot, err := a.Resolve(t.Context(), ticketLocator(a))
	if err != nil {
		t.Fatal(err)
	}
	cursor, err := github.ReplyCursor(snapshot.Ref, 0)
	if err != nil {
		t.Fatal(err)
	}
	page, err := a.RepliesAfter(t.Context(), snapshot.Ref, cursor)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*ticket.Reply)
	}{
		{"reply from another ticket", func(r *ticket.Reply) { r.Identity.RemoteID = "999" }},
		{"reply from another origin", func(r *ticket.Reply) { r.Identity.Origin = "https://other.example" }},
		{"author from another origin", func(r *ticket.Reply) { r.Author.Identity.Origin = "https://other.example" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reply := page.Replies[0]
			tc.change(&reply)
			if err := a.EditReply(t.Context(), reply, body(t, "replacement")); err == nil {
				t.Fatal("reply identity selected mutation destination")
			}
		})
	}
}

func TestTicketAdapterAcceptsCanonicalRepositoryCasing(t *testing.T) {
	t.Parallel()
	var base string
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		response := ticketJSON(base, 700, "Ticket", "body")
		response = strings.ReplaceAll(response, "/repos/"+testRepo.String()+"/", "/repos/"+strings.ToUpper(testRepo.String())+"/")
		respondJSON(http.StatusOK, response)(w, r)
	})
	base = f.Server.URL
	a := newTicketAdapter(t, f)
	snapshot, err := a.Resolve(t.Context(), ticketLocator(a))
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Ref.Locator.Container != testRepo.String() {
		t.Fatal("provider casing changed the historical locator")
	}
}
