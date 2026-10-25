package linear

import (
	"net/http"
	"testing"

	"github.com/cloudvane/litefactory/internal/ticket"
)

func TestReplyParentIdentityStaysOnExactTicket(t *testing.T) {
	c := testClient(t, func(http.ResponseWriter, *http.Request) { t.Fatal("unexpected request") })
	ref := ticket.Ref{Identity: c.identity("ticket"), Locator: c.locator("ticket")}
	item := comment{ID: "reply", User: &user{ID: "owner"}, Issue: &issue{ID: "ticket", Team: &objectID{ID: "team"}, Project: &objectID{ID: "project"}}, Parent: &comment{ID: "display", Issue: &issue{ID: "ticket"}}}
	r, err := c.reply(ref, item)
	if err != nil || r.ReplyTo.RemoteID != "display" || r.ReplyTo.Origin != ref.Identity.Origin {
		t.Fatalf("parent: %+v %v", r, err)
	}
	item.Parent.Issue.ID = "another-ticket"
	if _, err = c.reply(ref, item); err == nil {
		t.Fatal("foreign parent was admitted")
	}
	item.Parent.Issue = nil
	if _, err = c.reply(ref, item); err == nil {
		t.Fatal("unscoped parent was admitted")
	}
	item.Parent = nil
	r, err = c.reply(ref, item)
	if err != nil || r.ReplyTo.RemoteID != "" {
		t.Fatalf("legacy comment changed: %+v %v", r, err)
	}
}
