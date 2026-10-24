package messaging

import (
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

func TestInboxNavigationOmitsTicketFacts(t *testing.T) {
	text := "private ticket description\nWaiting: secret\nOpen: opaque:0:0\nMore: opaque:1\nRefresh: opaque:0"
	got := inboxNavigation(text)
	if strings.Contains(got, "secret") || strings.Contains(got, "private") || !strings.Contains(got, "Open: opaque:0:0") {
		t.Fatal(got)
	}
	for _, bad := range []string{"", "id", "id:-1", "id:0:-2", "id:x", "id:0:0:-1", "id:0:0:x", "id:0:0:0:0"} {
		if _, _, _, err := inboxPosition(bad); err == nil {
			t.Fatalf("accepted malformed reference %q", bad)
		}
	}
}

func TestInboxPageReservesNavigationWithLongUnicode(t *testing.T) {
	long := strings.Repeat("🙂界", 1000)
	page := store.InboxPage{Snapshot: store.InboxSnapshot{ID: strings.Repeat("a", 32), CapturedAt: time.Now(), Filter: store.InboxFilter{Source: long, Project: long, Kind: long}}, Entries: []store.InboxDecision{{Ordinal: 4, Source: long, Project: long, Ticket: long, Summary: long, Action: long}}, Offset: 4, Next: 5}
	for i := 0; i < 100; i++ {
		page.Snapshot.Coverage = append(page.Snapshot.Coverage, store.InboxCoverage{Source: long, Project: long, Status: long, ObservedAt: time.Now()})
	}
	text := renderInboxPage(page)
	if replySize(text) > 1900 || !strings.Contains(text, "More:") || !strings.Contains(text, "Back:") || !strings.Contains(text, ":4:4") {
		t.Fatalf("budget/navigation failed (%d units): %s", replySize(text), text)
	}
}

func TestInboxCoverageRetainsEveryConnectionAndBrowsePosition(t *testing.T) {
	snap := store.InboxSnapshot{ID: "snapshot", CapturedAt: time.Now()}
	for i := 0; i < 9; i++ {
		snap.Coverage = append(snap.Coverage, store.InboxCoverage{Source: "github", Project: "project", Status: strings.Repeat("🙂", 500), ObservedAt: time.Now()})
	}
	for _, offset := range []int{0, 4, 8} {
		text := renderInboxCoverage(snap, offset, 17)
		if replySize(text) > 1900 || !strings.Contains(text, "Return: snapshot:17") {
			t.Fatal(text)
		}
		expected := min(4, 9-offset)
		if strings.Count(text, "last observed") != expected {
			t.Fatalf("coverage page at %d omitted connections", offset)
		}
		if offset < 8 && !strings.Contains(text, "Coverage more: snapshot:") {
			t.Fatal("missing continuation")
		}
	}
}
