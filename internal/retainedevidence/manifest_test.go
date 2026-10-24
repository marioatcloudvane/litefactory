package retainedevidence

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestIndexPinsRevisionAndReportsContinuation(t *testing.T) {
	t.Parallel()
	m := Manifest{Version: Version, Scope: Scope{Kind: "workflow", ID: "attempt-1", Repository: "repo-1"}, Revision: "r1", Coverage: Complete}
	for i := range MaxIndexEntries + 1 {
		m.Entries = append(m.Entries, Entry{Kind: "command", OwnerID: fmt.Sprint(i), Revision: "r1", Bytes: -1, Capture: Unknown, Availability: Available, Omissions: []string{"ordering unknown"}})
	}
	page, err := Index(m, 0, MaxIndexEntries)
	if err != nil {
		t.Fatal(err)
	}
	if page.Revision != m.Revision || page.Total != len(m.Entries) || page.Next != MaxIndexEntries {
		t.Fatalf("wrong page identity or continuation: %+v", page)
	}
	page.Entries[0].Omissions[0] = "changed view"
	if m.Entries[0].Omissions[0] != "ordering unknown" {
		t.Fatal("index view changed retained manifest")
	}
	last, err := Index(m, page.Next, MaxIndexEntries)
	if err != nil {
		t.Fatal(err)
	}
	if last.Next != -1 || len(last.Entries) != 1 || last.Entries[0].OwnerID != fmt.Sprint(MaxIndexEntries) {
		t.Fatalf("wrong terminal page: %+v", last)
	}
	if _, err := Index(m, 0, MaxIndexEntries+1); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unbounded index error = %v", err)
	}
	for i := range m.Entries {
		m.Entries[i].Reason = strings.Repeat("x", 2048)
	}
	page, err = Index(m, 0, MaxIndexEntries)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > MaxIndexBytes || page.Next < 1 || page.Next >= MaxIndexEntries {
		t.Fatalf("byte-bounded page length=%d next=%d", len(raw), page.Next)
	}
}

func TestCaptureIntegrityAndUnknownExtentRemainDistinct(t *testing.T) {
	t.Parallel()
	entry := Entry{Kind: "command", OwnerID: "run-1", Revision: "1", Bytes: 0, Capture: Empty, Availability: Available, Digest: Digest(nil)}
	for _, tc := range []struct {
		name    string
		entry   Entry
		content []byte
		want    error
	}{
		{name: "retained empty", entry: entry},
		{name: "wrong empty extent", entry: entry, content: []byte("new"), want: ErrIntegrity},
		{name: "same extent different bytes", entry: Entry{Bytes: 3, Digest: Digest([]byte("old"))}, content: []byte("new"), want: ErrIntegrity},
		{name: "legacy unknown", entry: Entry{Bytes: -1, Capture: Unknown}, content: []byte("retained")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := Verify(tc.entry, tc.content); !errors.Is(err, tc.want) {
				t.Fatalf("Verify() = %v, want %v", err, tc.want)
			}
		})
	}
	m := Manifest{Version: Version, Scope: Scope{Kind: "workflow", ID: "1", Repository: "1"}, Revision: "1", Coverage: Complete, Entries: []Entry{entry}}
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	m.Entries[0].Availability = Unavailable
	if err := m.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unexplained unavailable entry error = %v", err)
	}
	m.Entries[0].Reason = "retained source deleted"
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	m.Entries = append(m.Entries, m.Entries[0])
	if err := m.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("duplicate source error = %v", err)
	}
}
