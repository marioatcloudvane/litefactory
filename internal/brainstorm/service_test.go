package brainstorm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

type serviceTeam struct{ err error }

func (f serviceTeam) Capture(context.Context, string) (TeamCapture, error) {
	if f.err != nil {
		return TeamCapture{}, f.err
	}
	return TeamCapture{Version: "v1", Role: "research role", Files: map[string]string{"skills/brainstorm/SKILL.md": "fixture"}}, nil
}

type serviceClock struct {
	mu sync.Mutex
	at time.Time
}

func (c *serviceClock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.at }
func (c *serviceClock) add(d time.Duration) {
	c.mu.Lock()
	c.at = c.at.Add(d)
	c.mu.Unlock()
}

var servicePrincipal = Principal{OwnerID: "7", OwnerGeneration: 2, Transport: "mcp", ClientID: "client-1", GrantID: "grant-1"}

const serviceRepo = "acme/app"

func serviceDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "litefactory.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	return db
}

func newServiceForTest(t *testing.T, db *store.DB, clock *serviceClock, key []byte) *Service {
	t.Helper()
	s, err := New(Options{DB: db, Now: clock.now, Team: serviceTeam{}, CursorKey: key})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func newServiceClock() *serviceClock {
	return &serviceClock{at: time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)}
}

func TestBrainstormAllKindsRecoveredByFreshService(t *testing.T) {
	db, clock := serviceDB(t), newServiceClock()
	s := newServiceForTest(t, db, clock, nil)
	v, err := s.Open(t.Context(), servicePrincipal, serviceRepo)
	if err != nil {
		t.Fatal(err)
	}
	id := v.Brainstorm.ID
	saved := []AddRequest{
		{Kind: KindIdea, Text: "voice capture of ideas"},
		{Kind: KindDecision, Text: "keep one brainstorm per repository"},
		{Kind: KindOpenQuestion, Text: "how long do scans take?"},
		{Kind: KindRelatedIssue, Text: "similar request", IssueReference: "acme/app#12"},
		{Kind: KindDocumentNote, Text: "the spec asks for offline mode", DocumentName: "product-spec.pdf"},
	}
	for i, r := range saved {
		r.BrainstormID, r.RequestKey, r.ExpectedRevision = id, fmt.Sprintf("add-%d", i), int64(i)
		if _, err = s.Add(t.Context(), servicePrincipal, r); err != nil {
			t.Fatalf("add %s: %v", r.Kind, err)
		}
	}
	fresh := newServiceForTest(t, db, clock, nil)
	resumed, err := fresh.Open(t.Context(), servicePrincipal, serviceRepo)
	if err != nil || resumed.Brainstorm.ID != id || resumed.Brainstorm.Revision != 5 {
		t.Fatalf("resume = %+v %v", resumed.Brainstorm, err)
	}
	page, err := fresh.Entries(t.Context(), servicePrincipal, id, "", "", 50)
	if err != nil || len(page.Entries) != len(saved) || page.NextCursor != "" {
		t.Fatalf("entries = %+v %v", page, err)
	}
	for i, e := range page.Entries {
		want := saved[i]
		if e.Seq != int64(i+1) || e.Kind != want.Kind || e.Text != want.Text || e.DocumentName != want.DocumentName || e.IssueReference != want.IssueReference {
			t.Fatalf("entry %d = %+v, want %+v", i, e, want)
		}
	}
	for _, k := range []string{KindIdea, KindDecision, KindOpenQuestion, KindRelatedIssue, KindDocumentNote} {
		if resumed.EntryCounts[k] != 1 {
			t.Fatalf("count %s = %d", k, resumed.EntryCounts[k])
		}
	}
	notes, err := fresh.Entries(t.Context(), servicePrincipal, id, KindDocumentNote, "", 10)
	if err != nil || len(notes.Entries) != 1 || notes.Entries[0].DocumentName != "product-spec.pdf" {
		t.Fatalf("document notes = %+v %v", notes, err)
	}
	other := servicePrincipal
	other.OwnerGeneration = 3
	if _, err = fresh.Get(t.Context(), other, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other generation get = %v", err)
	}
}

func TestBrainstormReplayRevisionCapsAndClose(t *testing.T) {
	db, clock := serviceDB(t), newServiceClock()
	s := newServiceForTest(t, db, clock, nil)
	v, err := s.Open(t.Context(), servicePrincipal, serviceRepo)
	if err != nil {
		t.Fatal(err)
	}
	r := AddRequest{BrainstormID: v.Brainstorm.ID, Kind: KindIdea, Text: "first", RequestKey: "k1"}
	if v, err = s.Add(t.Context(), servicePrincipal, r); err != nil || v.Replayed || v.Brainstorm.Revision != 1 {
		t.Fatalf("add = %+v %v", v.Brainstorm, err)
	}
	if v, err = s.Add(t.Context(), servicePrincipal, r); err != nil || !v.Replayed || v.EntryCounts[KindIdea] != 1 {
		t.Fatalf("replay = %+v %v", v, err)
	}
	r.Text = "changed"
	if _, err = s.Add(t.Context(), servicePrincipal, r); !errors.Is(err, ErrRequestReuse) {
		t.Fatalf("reuse = %v", err)
	}
	r.RequestKey = "k2"
	if _, err = s.Add(t.Context(), servicePrincipal, r); !errors.Is(err, ErrStaleRevision) || !strings.Contains(err.Error(), "current revision 1") {
		t.Fatalf("stale = %v", err)
	}
	r.ExpectedRevision = 1
	r.Text = strings.Repeat("x", MaxEntryBytes+1)
	if _, err = s.Add(t.Context(), servicePrincipal, r); !errors.Is(err, ErrCapReached) || !strings.Contains(err.Error(), "4 KB") {
		t.Fatalf("entry cap = %v", err)
	}
	if _, err = s.Add(t.Context(), servicePrincipal, AddRequest{BrainstormID: v.Brainstorm.ID, Kind: KindDocumentNote, Text: "x", RequestKey: "k3", ExpectedRevision: 1}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("note without document = %v", err)
	}
	if v, err = s.Close(t.Context(), servicePrincipal, v.Brainstorm.ID, "close-1", 1); err != nil || v.Brainstorm.State != "closed" || v.Brainstorm.CloseReason != "owner" {
		t.Fatalf("close = %+v %v", v.Brainstorm, err)
	}
	if _, err = s.Add(t.Context(), servicePrincipal, AddRequest{BrainstormID: v.Brainstorm.ID, Kind: KindIdea, Text: "late", RequestKey: "k4", ExpectedRevision: 2}); !errors.Is(err, ErrClosed) {
		t.Fatalf("add closed = %v", err)
	}
	if _, err = s.Ask(t.Context(), servicePrincipal, AskRequest{BrainstormID: v.Brainstorm.ID, Question: "what exists?", RequestKey: "a1"}); !errors.Is(err, ErrClosed) {
		t.Fatalf("ask closed = %v", err)
	}
	if page, err := s.Entries(t.Context(), servicePrincipal, v.Brainstorm.ID, "", "", 10); err != nil || len(page.Entries) != 1 {
		t.Fatalf("closed read = %+v %v", page, err)
	}
	list, err := s.List(t.Context(), servicePrincipal, serviceRepo, "", 10)
	if err != nil || len(list.Brainstorms) != 1 || list.Brainstorms[0].State != "closed" {
		t.Fatalf("list = %+v %v", list, err)
	}
}

func TestBrainstormLazyExpiry(t *testing.T) {
	db, clock := serviceDB(t), newServiceClock()
	s := newServiceForTest(t, db, clock, nil)
	v, err := s.Open(t.Context(), servicePrincipal, serviceRepo)
	if err != nil {
		t.Fatal(err)
	}
	clock.add(ExpiryAfter - time.Second)
	if v, err = s.Get(t.Context(), servicePrincipal, v.Brainstorm.ID); err != nil || v.Brainstorm.State != "open" {
		t.Fatalf("before expiry = %+v %v", v.Brainstorm, err)
	}
	clock.add(time.Second)
	if v, err = s.Get(t.Context(), servicePrincipal, v.Brainstorm.ID); err != nil || v.Brainstorm.State != "closed" || v.Brainstorm.CloseReason != "expired" {
		t.Fatalf("after expiry = %+v %v", v.Brainstorm, err)
	}
	if _, err = s.Add(t.Context(), servicePrincipal, AddRequest{BrainstormID: v.Brainstorm.ID, Kind: KindIdea, Text: "x", RequestKey: "k", ExpectedRevision: v.Brainstorm.Revision}); !errors.Is(err, ErrClosed) {
		t.Fatalf("add expired = %v", err)
	}
	next, err := s.Open(t.Context(), servicePrincipal, serviceRepo)
	if err != nil || next.Brainstorm.ID == v.Brainstorm.ID || next.Brainstorm.State != "open" {
		t.Fatalf("open after expiry = %+v %v", next.Brainstorm, err)
	}
}

func TestBrainstormMissingSkillAndConsent(t *testing.T) {
	db, clock := serviceDB(t), newServiceClock()
	s, err := New(Options{DB: db, Now: clock.now, Team: serviceTeam{err: ErrSkillMissing}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Open(t.Context(), servicePrincipal, serviceRepo); !errors.Is(err, ErrSkillMissing) {
		t.Fatalf("missing skill = %v", err)
	}
	denied, err := New(Options{DB: db, Now: clock.now, Team: serviceTeam{}, Consent: func(context.Context, Principal, string) error { return ErrForbidden }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = denied.Open(t.Context(), servicePrincipal, serviceRepo); !errors.Is(err, ErrForbidden) {
		t.Fatalf("consent = %v", err)
	}
}

func TestBrainstormAskAdmissionReplayQueueAndCancel(t *testing.T) {
	db, clock := serviceDB(t), newServiceClock()
	s := newServiceForTest(t, db, clock, nil)
	v, err := s.Open(t.Context(), servicePrincipal, serviceRepo)
	if err != nil {
		t.Fatal(err)
	}
	id := v.Brainstorm.ID
	first, err := s.Ask(t.Context(), servicePrincipal, AskRequest{BrainstormID: id, Question: "is there an export?", RequestKey: "a0"})
	if err != nil || first.Scan.State != ScanWaiting || first.Cost != CostEstimateUnknown || first.Scan.Seq != 1 {
		t.Fatalf("ask = %+v %v", first, err)
	}
	fresh := newServiceForTest(t, db, clock, nil)
	scans, err := fresh.Scans(t.Context(), servicePrincipal, id, "", 50)
	if err != nil || len(scans.Scans) != 1 || scans.Scans[0].ScanID != first.Scan.ScanID || scans.Scans[0].State != ScanWaiting {
		t.Fatalf("fresh scans = %+v %v", scans, err)
	}
	got, err := fresh.Get(t.Context(), servicePrincipal, id)
	if err != nil || got.ScanCounts[ScanWaiting] != 1 || len(got.Scans) != 1 {
		t.Fatalf("fresh get = %+v %v", got, err)
	}
	again, err := fresh.Ask(t.Context(), servicePrincipal, AskRequest{BrainstormID: id, Question: "is there an export?", RequestKey: "a0"})
	if err != nil || !again.Replayed || again.Scan.ScanID != first.Scan.ScanID {
		t.Fatalf("replay = %+v %v", again, err)
	}
	if _, err = fresh.Ask(t.Context(), servicePrincipal, AskRequest{BrainstormID: id, Question: "other", RequestKey: "a0"}); !errors.Is(err, ErrRequestReuse) {
		t.Fatalf("reuse = %v", err)
	}
	for i := 1; i < MaxQueuedScans; i++ {
		if _, err = s.Ask(t.Context(), servicePrincipal, AskRequest{BrainstormID: id, Question: fmt.Sprintf("q%d", i), RequestKey: fmt.Sprintf("a%d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.Ask(t.Context(), servicePrincipal, AskRequest{BrainstormID: id, Question: "one too many", RequestKey: "full"}); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("queue = %v", err)
	}
	if scans, err = s.Scans(t.Context(), servicePrincipal, id, "", 50); err != nil || len(scans.Scans) != MaxQueuedScans {
		t.Fatalf("refused admission listed: %d %v", len(scans.Scans), err)
	}
	cancelled, err := s.ScanCancel(t.Context(), servicePrincipal, first.Scan.ScanID)
	if err != nil || cancelled.Scan.State != ScanCancelled || cancelled.Cost != CostUnknown {
		t.Fatalf("cancel = %+v %v", cancelled, err)
	}
	if got, err := s.ScanGet(t.Context(), servicePrincipal, first.Scan.ScanID, "", 10); err != nil || got.Scan.State != ScanCancelled {
		t.Fatalf("scan get = %+v %v", got, err)
	}
	if _, err = s.Ask(t.Context(), servicePrincipal, AskRequest{BrainstormID: id, Question: "now fits", RequestKey: "full"}); err != nil {
		t.Fatalf("ask after cancel = %v", err)
	}
}

func TestBrainstormPagingAcrossAppendsAndCursorBinding(t *testing.T) {
	db, clock := serviceDB(t), newServiceClock()
	key := []byte("fixture-cursor-key")
	s := newServiceForTest(t, db, clock, key)
	v, err := s.Open(t.Context(), servicePrincipal, serviceRepo)
	if err != nil {
		t.Fatal(err)
	}
	id := v.Brainstorm.ID
	revision := int64(0)
	add := func(n int) {
		for range n {
			kind := KindIdea
			if revision%3 == 0 {
				kind = KindDecision
			}
			if _, err := s.Add(t.Context(), servicePrincipal, AddRequest{BrainstormID: id, Kind: kind, Text: fmt.Sprintf("entry %d", revision), RequestKey: fmt.Sprintf("k%d", revision), ExpectedRevision: revision}); err != nil {
				t.Fatal(err)
			}
			revision++
		}
	}
	add(60)
	seen := map[int64]int{}
	last := int64(0)
	cursor := ""
	pages := 0
	for {
		fresh := newServiceForTest(t, db, clock, key)
		page, err := fresh.Entries(t.Context(), servicePrincipal, id, "", cursor, 50)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range page.Entries {
			if e.Seq <= last {
				t.Fatalf("seq %d after %d", e.Seq, last)
			}
			last = e.Seq
			seen[e.Seq]++
		}
		pages++
		if pages == 1 {
			add(7)
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if len(seen) != 67 || pages != 2 {
		t.Fatalf("seen %d entries in %d pages", len(seen), pages)
	}
	decisions, err := s.Entries(t.Context(), servicePrincipal, id, KindDecision, "", 5)
	if err != nil || decisions.NextCursor == "" {
		t.Fatalf("decisions = %+v %v", decisions, err)
	}
	for _, e := range decisions.Entries {
		if e.Kind != KindDecision {
			t.Fatalf("filter returned %s", e.Kind)
		}
	}
	if _, err = s.Entries(t.Context(), servicePrincipal, id, KindIdea, decisions.NextCursor, 5); !errors.Is(err, ErrCursor) {
		t.Fatalf("kind-swapped cursor = %v", err)
	}
	tampered := []byte(decisions.NextCursor)
	tampered[3] ^= 1
	if _, err = s.Entries(t.Context(), servicePrincipal, id, KindDecision, string(tampered), 5); !errors.Is(err, ErrCursor) {
		t.Fatalf("tampered cursor = %v", err)
	}
	foreign := servicePrincipal
	foreign.GrantID = "grant-2"
	if _, err = s.Entries(t.Context(), foreign, id, KindDecision, decisions.NextCursor, 5); !errors.Is(err, ErrCursor) {
		t.Fatalf("foreign grant cursor = %v", err)
	}
	if _, err = s.Scans(t.Context(), servicePrincipal, id, decisions.NextCursor, 5); !errors.Is(err, ErrCursor) {
		t.Fatalf("entry cursor on scans = %v", err)
	}
	if _, err = s.Entries(t.Context(), servicePrincipal, id, "", "", MaxPageLimit+1); !errors.Is(err, ErrInvalid) {
		t.Fatalf("limit = %v", err)
	}

	for i := range 55 {
		sv, err := s.Ask(t.Context(), servicePrincipal, AskRequest{BrainstormID: id, Question: fmt.Sprintf("question %d", i), RequestKey: fmt.Sprintf("ask-%d", i)})
		if err != nil {
			t.Fatal(err)
		}
		if i%2 == 0 {
			if _, err = s.ScanCancel(t.Context(), servicePrincipal, sv.Scan.ScanID); err != nil {
				t.Fatal(err)
			}
		}
		if i >= 3 && i%2 == 1 {
			if _, err = s.ScanCancel(t.Context(), servicePrincipal, sv.Scan.ScanID); err != nil {
				t.Fatal(err)
			}
		}
	}
	first, err := s.Scans(t.Context(), servicePrincipal, id, "", 50)
	if err != nil || len(first.Scans) != 50 || first.NextCursor == "" || first.Scans[1].State != ScanWaiting {
		t.Fatalf("scan page 1 = %d %q %v", len(first.Scans), first.NextCursor, err)
	}
	if _, err = s.ScanCancel(t.Context(), servicePrincipal, first.Scans[1].ScanID); err != nil {
		t.Fatal(err)
	}
	second, err := s.Scans(t.Context(), servicePrincipal, id, first.NextCursor, 50)
	if err != nil || len(second.Scans) != 5 || second.NextCursor != "" || second.Scans[0].Seq != 51 {
		t.Fatalf("scan page 2 = %+v %v", second, err)
	}
	reread, err := s.ScanGet(t.Context(), servicePrincipal, first.Scans[1].ScanID, "", 10)
	if err != nil || reread.Scan.State != ScanCancelled {
		t.Fatalf("reread = %+v %v", reread, err)
	}
}

func TestBrainstormEvidencePagesEveryItemOnce(t *testing.T) {
	var e evidence
	for i := range 30 {
		e.Findings = append(e.Findings, json.RawMessage(fmt.Sprintf(`{"id":"f%d"}`, i)))
		e.Decisions = append(e.Decisions, json.RawMessage(fmt.Sprintf(`{"id":"d%d"}`, i)))
		e.Assumptions = append(e.Assumptions, json.RawMessage(fmt.Sprintf(`{"id":"a%d","text":%q}`, i, strings.Repeat("x", 5000))))
	}
	var at [3]int
	total := 0
	for n := 0; ; n++ {
		page, next, done := evidencePage(e, at, MaxPageLimit)
		size := 0
		for _, list := range [][]json.RawMessage{page.Findings, page.Decisions, page.Assumptions} {
			total += len(list)
			for _, item := range list {
				size += len(item)
			}
		}
		if size > pageBytes {
			t.Fatalf("page %d is %d bytes", n, size)
		}
		at = next
		if done {
			break
		}
		if n > 20 {
			t.Fatal("evidence paging did not finish")
		}
	}
	if total != 90 {
		t.Fatalf("paged %d items", total)
	}
}

func TestBrainstormPlainStates(t *testing.T) {
	for job, want := range map[string]string{"queued": ScanWaiting, "running": ScanRunning, "stop_requested": ScanStopping, "succeeded": ScanAnswered, "failed": ScanFailed, "cancelled": ScanCancelled, "uncertain": ScanUnsure} {
		got, reason := plainState(store.BrainstormScan{JobState: job})
		if got != want || (job == "failed" || job == "uncertain") == (reason == "") {
			t.Fatalf("%s -> %s %q", job, got, reason)
		}
	}
}
