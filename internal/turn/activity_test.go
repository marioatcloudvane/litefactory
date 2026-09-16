package turn

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

func activitySocketFixture(t *testing.T) (*ActivityService, submission.Client, *store.DB, *AttemptGuard) {
	t.Helper()
	db, _, opts := agentResultFixture(t, "research")
	app, err := BindAgentResultService(t.Context(), opts)
	mustProgress(t, err)
	o := familyResearchOptions(t, db, app)
	activity, err := NewActivityService(ActivityOptions{Receipts: o.Receipts, Now: time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)})
	mustProgress(t, err)
	o.Registrations = []FamilyRegistration{{Name: "activity_update", Owner: activity, Handle: activity.HandleUpdate}, {Name: "activity_get", Owner: activity, Handle: activity.HandleGet}}
	client, guard, _ := familySocketFixture(t, db, o)
	return activity, client, db, guard
}

func activityArgs(sequence int, message string) []byte {
	raw, _ := json.Marshal(map[string]any{"sequence": sequence, "message": message})
	return raw
}

func activityResponse(t *testing.T, r submission.Response) ActivityUpdateResponse {
	t.Helper()
	if r.Error != "" {
		t.Fatal(r.Error)
	}
	var value ActivityUpdateResponse
	mustProgress(t, json.Unmarshal(r.Result, &value))
	return value
}

func TestActivitySocketBoundsAndOrdering(t *testing.T) {
	s, client, _, _ := activitySocketFixture(t)
	for _, tc := range []struct {
		name                       string
		sequence                   int
		message, disposition, code string
	}{
		{"zero", 0, "work", "rejected", "invalid_progress"},
		{"blank", 1, " \t", "rejected", "invalid_progress"},
		{"oversize", 1, strings.Repeat("a", 2049), "rejected", "message_limit"},
		{"max bytes", 3, strings.Repeat("é", 1024), "accepted", ""},
		{"conflict", 3, "different", "rejected", "sequence_conflict"},
		{"older", 2, "old", "stale", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := mcpToolCall(t, client, "activity_update", activityArgs(tc.sequence, tc.message))
			got := activityResponse(t, r)
			if got.Disposition != tc.disposition || got.Code != tc.code {
				t.Fatalf("got %+v", got)
			}
			receipt, err := s.receipts.Get(t.Context(), r.RequestID)
			mustProgress(t, err)
			want := "confirmed"
			if tc.disposition == "rejected" {
				want = "rejected"
			}
			if receipt.Disposition != want {
				t.Fatalf("receipt %+v", receipt)
			}
		})
	}
	for _, raw := range [][]byte{[]byte(`{"sequence":4,"message":"ok","unknown":true}`), append(append([]byte(`{"sequence":4,"message":"`), 0xff), []byte(`"}`)...)} {
		r, err := client.Call(t.Context(), "activity_update", raw)
		mustProgress(t, err)
		if r.Error == "" {
			t.Fatalf("invalid arguments accepted: %q", raw)
		}
	}
	view, err := s.Lifecycle(ActivityLifecycle{Now: s.now})
	mustProgress(t, err)
	if view.AcceptedUpdates != 1 || view.HighestSequence != 3 {
		t.Fatalf("refusals changed display: %+v", view)
	}
}

func TestActivitySocketDurableRetryAndLifecycle(t *testing.T) {
	s, client, db, guard := activitySocketFixture(t)
	initial := mcpToolCall(t, client, "activity_get", []byte(`{}`))
	var initialView ActivityView
	mustProgress(t, json.Unmarshal(initial.Result, &initialView))
	if initialView.Status != "unavailable" {
		t.Fatalf("initial display: %+v", initialView)
	}
	lease := guard.Lease()
	run, err := db.GetRun(t.Context(), s.Binding().RunID)
	mustProgress(t, err)
	first := mcpToolCall(t, client, "activity_update", activityArgs(1, "checking"))
	if got := activityResponse(t, first); got.Disposition != "accepted" {
		t.Fatalf("%+v", got)
	}
	terminal := s.now
	_, err = s.Lifecycle(ActivityLifecycle{Now: terminal, Terminal: true})
	mustProgress(t, err)
	for i := 1; i <= 3; i++ {
		r := mcpToolCall(t, client, "activity_update", activityArgs(1, "checking"))
		if r.RequestID != first.RequestID {
			t.Fatal("retry lost stable identity")
		}
		var receipt FamilyReceipt
		mustProgress(t, json.Unmarshal(r.Result, &receipt))
		if receipt.Disposition != "confirmed" {
			t.Fatalf("retry %+v", receipt)
		}
		v, err := s.Lifecycle(ActivityLifecycle{Now: terminal.Add(time.Duration(i) * time.Hour), Terminal: true})
		mustProgress(t, err)
		if v.TerminalAt == nil || !v.TerminalAt.Equal(terminal) || v.AcceptedUpdates != 1 {
			t.Fatalf("retry renewed state: %+v", v)
		}
	}
	denied := activityResponse(t, mcpToolCall(t, client, "activity_update", activityArgs(2, "late")))
	if denied.Disposition != "rejected" {
		t.Fatalf("terminal accepted: %+v", denied)
	}
	v, err := s.Lifecycle(ActivityLifecycle{Now: terminal.Add(24 * time.Hour)})
	mustProgress(t, err)
	if v.Status != "cleared" || v.Latest != nil {
		t.Fatalf("retention %+v", v)
	}
	// Open the actual database again: retained outcomes survive, display does not.
	reopened, err := store.Open(t.Context(), db.Path())
	mustProgress(t, err)
	defer reopened.Close()
	mustProgress(t, reopened.Migrate(t.Context()))
	receipts, err := NewReceiptService(reopened, s.Binding(), nil, nil)
	mustProgress(t, err)
	replacement, err := NewActivityService(ActivityOptions{Receipts: receipts, Now: terminal.Add(25 * time.Hour)})
	mustProgress(t, err)
	data, err := replacement.HandleGet(t.Context(), "", []byte(`{}`))
	mustProgress(t, err)
	var empty ActivityView
	mustProgress(t, json.Unmarshal(data, &empty))
	if empty.Status != "unavailable" || empty.Latest != nil {
		t.Fatalf("restart %+v", empty)
	}
	recovered, err := receipts.Get(t.Context(), first.RequestID)
	mustProgress(t, err)
	if recovered.Disposition != "confirmed" {
		t.Fatalf("reopen %+v", recovered)
	}
	revisions, err := db.ResultRevisionNumbers(t.Context(), s.Binding())
	mustProgress(t, err)
	after, err := db.GetRun(t.Context(), s.Binding().RunID)
	mustProgress(t, err)
	if len(revisions) != 0 || !reflect.DeepEqual(run, after) || !reflect.DeepEqual(lease, guard.Lease()) {
		t.Fatal("activity changed workflow or authority")
	}
}

func TestActivitySocketConcurrentRetry(t *testing.T) {
	s, client, _, _ := activitySocketFixture(t)
	const count = 16
	start := make(chan struct{})
	results := make(chan submission.Response, count)
	errs := make(chan error, count)
	var wg sync.WaitGroup
	for range count {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			r, err := client.Call(t.Context(), "activity_update", activityArgs(1, "same"))
			results <- r
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		mustProgress(t, err)
	}
	id := ""
	for r := range results {
		if r.Error != "" {
			t.Fatal(r.Error)
		}
		if id == "" {
			id = r.RequestID
		}
		if id != r.RequestID {
			t.Fatal("concurrent retries got distinct identities")
		}
	}
	v, err := s.Lifecycle(ActivityLifecycle{Now: s.now})
	mustProgress(t, err)
	if v.AcceptedUpdates != 1 || v.RetainedIdentities != 1 {
		t.Fatalf("duplicate applied: %+v", v)
	}
	receipt, err := s.receipts.Get(t.Context(), id)
	mustProgress(t, err)
	if receipt.Disposition != "confirmed" {
		t.Fatalf("receipt %+v", receipt)
	}
}

func TestActivitySQLiteUpdateBudgetAndReplayWindow(t *testing.T) {
	s, _, _, _ := activitySocketFixture(t)
	call := func(id string, seq int, message string) ActivityUpdateResponse {
		t.Helper()
		raw := activityArgs(seq, message)
		_, first, err := s.receipts.Begin(t.Context(), id, "activity_update", raw)
		mustProgress(t, err)
		if !first {
			t.Fatal("fixture requires fresh transport identity")
		}
		data, err := s.HandleUpdate(t.Context(), id, raw)
		mustProgress(t, err)
		var got ActivityUpdateResponse
		mustProgress(t, json.Unmarshal(data, &got))
		return got
	}
	for i := 1; i <= 1024; i++ {
		got := call(fmt.Sprintf("update-%d", i), i, "work")
		if got.Disposition != "accepted" {
			t.Fatalf("update %d: %+v", i, got)
		}
	}
	for _, tc := range []struct {
		id                         string
		seq                        int
		message, disposition, code string
	}{
		{"retained-replay", 1024, "work", "replayed", ""},
		{"evicted-replay", 1, "work", "stale", ""},
		{"budget", 1025, "work", "rejected", "update_limit"},
		{"retained-conflict", 1024, "changed", "rejected", "sequence_conflict"},
	} {
		got := call(tc.id, tc.seq, tc.message)
		if got.Disposition != tc.disposition || got.Code != tc.code || got.Activity.AcceptedUpdates != 1024 || got.Activity.RetainedIdentities != 32 {
			t.Fatalf("%s: %+v", tc.id, got)
		}
	}
}

func TestActivitySocketRevokedCallsHaveNoEffect(t *testing.T) {
	s, client, _, guard := activitySocketFixture(t)
	first := mcpToolCall(t, client, "activity_update", activityArgs(1, "before revocation"))
	activityResponse(t, first)
	before, err := s.Lifecycle(ActivityLifecycle{Now: s.now})
	mustProgress(t, err)
	mustProgress(t, guard.Revoke(t.Context(), "cancelled"))
	for _, tc := range []struct {
		name string
		args []byte
	}{
		{"activity_update", activityArgs(2, "after revocation")},
		{"activity_update", activityArgs(1, "before revocation")},
		{"activity_get", []byte(`{}`)},
	} {
		r, err := client.Call(t.Context(), tc.name, tc.args)
		mustProgress(t, err)
		if r.Error == "" {
			t.Fatalf("revoked %s admitted", tc.name)
		}
	}
	after, err := s.Lifecycle(ActivityLifecycle{Now: s.now})
	mustProgress(t, err)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("revoked request altered display")
	}
}
