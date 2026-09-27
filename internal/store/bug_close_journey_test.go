package store_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

type closeJourneyForge struct {
	issue                        github.Issue
	writes, reads                int
	lost, unapplied, unavailable bool
	onClose                      func()
}

func (f *closeJourneyForge) BaseURL() string { return "https://api.github.com/" }
func (f *closeJourneyForge) GetIssue(context.Context, github.Repo, int) (github.Issue, error) {
	f.reads++
	if f.unavailable {
		return github.Issue{}, errors.New("controlled unavailable issue")
	}
	return f.issue, nil
}
func (f *closeJourneyForge) CloseDeliveredParent(_ context.Context, _ github.Repo, n int, id int64) error {
	f.writes++
	if f.onClose != nil {
		f.onClose()
	}
	if n != f.issue.Number || id != f.issue.ID {
		return errors.New("wrong close identity")
	}
	if !f.unapplied {
		f.issue.State = "closed"
	}
	if f.lost {
		return errors.New("controlled lost closure response")
	}
	return nil
}
func closeJourneySetup(t *testing.T) (*store.DB, string, *closeJourneyForge, *turn.BugCloseActivity, turn.BugCloseInput) {
	t.Helper()
	db, path, _, h, in := mergeJourneySetup(t)
	out, e := h.Merge(t.Context(), db, in)
	if e != nil || out.Outcome != "confirmed" {
		t.Fatalf("integration: %+v %v", out, e)
	}
	id := out.Merge.Binding.Review.Evidence.Identity
	f := &closeJourneyForge{issue: github.Issue{ID: id.IssueID, Number: int(id.IssueNumber), State: "open", APIURL: fmt.Sprintf("https://api.github.com/repos/%s/issues/%d", id.Repository, id.IssueNumber)}}
	ci := turn.BugCloseInput{SessionID: in.SessionID, Operation: "close-1", MergeOperation: in.Operation, At: in.At}
	f.onClose = func() {
		c, e := db.GetBugClosure(t.Context(), in.SessionID)
		if e != nil || !c.Dispatched {
			t.Errorf("close preceded durable claim: %+v %v", c, e)
		}
	}
	return db, path, f, &turn.BugCloseActivity{Forge: f, Redactor: h.Redactor}, ci
}
func TestBugCloseJourneyConfirmationRecoveryAndReopen(t *testing.T) {
	for _, kind := range []string{"normal", "lost", "external"} {
		t.Run(kind, func(t *testing.T) {
			db, path, f, h, in := closeJourneySetup(t)
			if kind == "lost" {
				f.lost = true
			}
			if kind == "external" {
				f.issue.State = "closed"
			}
			out, e := h.Close(t.Context(), db, in)
			if e != nil || out.Outcome != "confirmed" {
				t.Fatalf("close: %+v %v", out, e)
			}
			if _, e = db.ConfirmedBugClosure(t.Context(), in.SessionID); e != nil {
				t.Fatal(e)
			}
			writes := f.writes
			if kind == "external" && writes != 0 {
				t.Fatal("rewrote external closure")
			}
			if e = db.Close(); e != nil {
				t.Fatal(e)
			}
			db, e = store.Open(t.Context(), path)
			if e != nil {
				t.Fatal(e)
			}
			defer func() { _ = db.Close() }()
			out, e = h.Close(t.Context(), db, in)
			if e != nil || out.Outcome != "confirmed" || f.writes != writes {
				t.Fatalf("replay: %+v %v writes=%d", out, e, f.writes)
			}
			f.issue.State = "open"
			out, e = h.Close(t.Context(), db, in)
			if e != nil || out.Outcome != "pending" || !out.Closure.Confirmed || !out.Closure.Conflict || f.writes != writes {
				t.Fatalf("reopen: %+v %v", out, e)
			}
			if _, e = db.ConfirmedBugClosure(t.Context(), in.SessionID); e == nil {
				t.Fatal("reopen allowed lifecycle completion")
			}
		})
	}
}
func TestBugCloseJourneyRefusesIdentityAndCancelledAuthority(t *testing.T) {
	for _, kind := range []string{"id", "number", "endpoint", "null", "cancel-before", "cancel-after", "external-no-authority"} {
		t.Run(kind, func(t *testing.T) {
			db, _, f, h, in := closeJourneySetup(t)
			switch kind {
			case "id":
				f.issue.ID++
			case "number":
				f.issue.Number++
			case "endpoint":
				f.issue.APIURL = "https://other.example/repos/acme/api/issues/385"
			case "null":
				f.issue = github.Issue{}
			case "cancel-before", "external-no-authority":
				if e := db.CancelBugDelivery(t.Context(), in.SessionID); e != nil {
					t.Fatal(e)
				}
				if kind == "external-no-authority" {
					f.issue.State = "closed"
				}
			case "cancel-after":
				f.onClose = func() {
					if e := db.CancelBugDelivery(t.Context(), in.SessionID); e != nil {
						t.Fatal(e)
					}
				}
			}
			out, e := h.Close(t.Context(), db, in)
			if e != nil || out.Outcome != "blocked" {
				t.Fatalf("refusal: %+v %v", out, e)
			}
			want := 0
			if kind == "cancel-after" {
				want = 1
			}
			if f.writes != want {
				t.Fatalf("writes=%d", f.writes)
			}
			if _, e = db.ConfirmedBugClosure(t.Context(), in.SessionID); e == nil {
				t.Fatal("invalid closure supplied completion")
			}
		})
	}
}
func TestBugCloseJourneyUnknownNeverRetries(t *testing.T) {
	db, _, f, h, in := closeJourneySetup(t)
	f.lost = true
	f.unapplied = true
	for range 2 {
		out, e := h.Close(t.Context(), db, in)
		if e != nil || out.Outcome != "pending" {
			t.Fatalf("uncertain: %+v %v", out, e)
		}
	}
	if f.writes != 1 {
		t.Fatal("duplicated uncertain close")
	}
	f.unavailable = true
	out, e := h.Close(t.Context(), db, in)
	if e != nil || out.Outcome != "pending" || f.writes != 1 {
		t.Fatalf("unknown: %+v %v", out, e)
	}
	f.unavailable = false
	f.issue.State = "closed"
	out, e = h.Close(t.Context(), db, in)
	if e != nil || out.Outcome != "confirmed" || f.writes != 1 {
		t.Fatalf("late closure: %+v %v", out, e)
	}
}

func TestBugCloseAtomicClaimAndRetryRefusal(t *testing.T) {
	db, _, f, _, in := closeJourneySetup(t)
	if _, e := db.EnsureBugClosure(t.Context(), in.SessionID, in.Operation, in.MergeOperation, f.issue.APIURL); e != nil {
		t.Fatal(e)
	}
	if _, e := db.ObserveBugClosure(t.Context(), in.SessionID, store.BugClosureObservation{Kind: "issue", Endpoint: f.issue.APIURL, ID: f.issue.ID, Number: f.issue.Number, State: "open", At: in.At}); e != nil {
		t.Fatal(e)
	}
	revision, e := db.BugClosurePrerequisites(t.Context(), in.SessionID)
	if e != nil {
		t.Fatal(e)
	}
	type result struct {
		claimed bool
		err     error
	}
	results := make(chan result, 2)
	for range 2 {
		go func() {
			claimed, e := db.ClaimBugClosure(t.Context(), in.SessionID, revision)
			results <- result{claimed, e}
		}()
	}
	claims := 0
	for range 2 {
		r := <-results
		if r.err != nil {
			t.Fatal(r.err)
		}
		if r.claimed {
			claims++
		}
	}
	if claims != 1 {
		t.Fatalf("claims=%d", claims)
	}
	if e = db.PrepareBugClosureRetry(t.Context(), in.SessionID, "retry", "issue remains open", revision); e == nil {
		t.Fatal("uncertain close authorized retry")
	}
}
