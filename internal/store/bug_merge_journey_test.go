package store_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

type mergeJourneyForge struct {
	*prJourneyForge
	merges                 int
	commit                 github.DeliveryCommit
	lost, unapplied, drift bool
	afterClaim             func()
	assertClaim            func()
}

func (f *mergeJourneyForge) MergeDeliveryPR(_ context.Context, _ github.Repo, number int, head string) error {
	f.merges++
	if f.assertClaim != nil {
		f.assertClaim()
	}
	if f.afterClaim != nil {
		f.afterClaim()
	}
	if number != f.p.Number || head != f.p.Head {
		return errors.New("unexpected merge arguments")
	}
	if !f.unapplied {
		f.p.Merged = true
		f.p.State = "closed"
		f.p.MergeSHA = f.commit.SHA
		f.p.MergedAt = time.Date(2026, 9, 27, 1, 0, 0, 0, time.UTC)
		f.target = f.commit.SHA
		if f.drift {
			f.target = strings.Repeat("e", 40)
		}
	}
	if f.lost {
		return errors.New("controlled lost merge response")
	}
	return nil
}
func (f *mergeJourneyForge) GetDeliveryCommit(context.Context, github.Repo, string) (github.DeliveryCommit, error) {
	return f.commit, nil
}
func mergeJourneySetup(t *testing.T) (*store.DB, string, *mergeJourneyForge, *turn.BugMergeActivity, turn.BugMergeInput) {
	t.Helper()
	db, path, _, f, h, in := prJourneySetup(t)
	out, err := h.Prepare(t.Context(), db, in)
	if err != nil || out.Review == nil {
		t.Fatalf("review: %+v %v", out, err)
	}
	r := *out.Review
	prJourneyPublish(t, db, r)
	in.Operation = "approval"
	in.ReviewDigest = r.Digest
	in.Reply = prJourneyReply(r, in, r.ApprovalCommand())
	out, err = h.Prepare(t.Context(), db, in)
	if err != nil || out.Outcome != "approved" {
		t.Fatalf("approval: %+v %v", out, err)
	}
	mf := &mergeJourneyForge{prJourneyForge: f, commit: github.DeliveryCommit{SHA: strings.Repeat("d", 40), Parents: []string{f.target, f.p.Head}}}
	mh := &turn.BugMergeActivity{Forge: mf, Redactor: h.Redactor, Observe: h.Observe}
	mi := turn.BugMergeInput{SessionID: in.SessionID, Operation: "merge-1", ReviewDigest: r.Digest, At: in.At.Add(48 * time.Hour)}
	mf.assertClaim = func() {
		m, e := db.GetBugMerge(t.Context(), mi.SessionID, mi.Operation)
		if e != nil || !m.Dispatched {
			t.Errorf("mutation precedes durable claim: %+v %v", m, e)
		}
		claimed, e := db.ClaimBugWork(t.Context(), mi.SessionID, "other-save", "save")
		if e != nil || claimed {
			t.Errorf("merge did not exclude conflicting work: %v %v", claimed, e)
		}
	}
	return db, path, mf, mh, mi
}
func TestBugMergeJourneyConfirmedAndLostReplyReplay(t *testing.T) {
	for _, lost := range []bool{false, true} {
		t.Run(map[bool]string{false: "normal", true: "lost"}[lost], func(t *testing.T) {
			db, path, f, h, in := mergeJourneySetup(t)
			f.lost = lost
			out, err := h.Merge(t.Context(), db, in)
			if err != nil || out.Outcome != "confirmed" {
				t.Fatalf("merge: %+v %v", out, err)
			}
			if _, err = db.ConfirmedBugIntegration(t.Context(), in.SessionID, in.Operation); err != nil {
				t.Fatal(err)
			}
			if err = db.Close(); err != nil {
				t.Fatal(err)
			}
			db, err = store.Open(t.Context(), path)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = db.Close() }()
			out, err = h.Merge(t.Context(), db, in)
			if err != nil || out.Outcome != "confirmed" || f.merges != 1 {
				t.Fatalf("replay: %+v %v merges=%d", out, err, f.merges)
			}
		})
	}
}
func TestBugMergeJourneyUncertaintyRetainsGuard(t *testing.T) {
	db, _, f, h, in := mergeJourneySetup(t)
	f.lost = true
	f.unapplied = true
	for range 2 {
		out, err := h.Merge(t.Context(), db, in)
		if err != nil || out.Outcome != "pending" {
			t.Fatalf("uncertain: %+v %v", out, err)
		}
	}
	if f.merges != 1 {
		t.Fatalf("repeated uncertain merge %d", f.merges)
	}
	if _, err := db.ConfirmedBugIntegration(t.Context(), in.SessionID, in.Operation); err == nil {
		t.Fatal("uncertainty allowed closure")
	}
	if _, err := db.EnsureBugMerge(t.Context(), in.SessionID, "replacement", in.ReviewDigest); err == nil {
		t.Fatal("replacement hid uncertain operation")
	}
}
func TestBugMergeJourneyTargetExceptionAndCancellation(t *testing.T) {
	for _, kind := range []string{"drift", "parents", "cancel", "source"} {
		t.Run(kind, func(t *testing.T) {
			db, _, f, h, in := mergeJourneySetup(t)
			switch kind {
			case "drift":
				f.drift = true
			case "parents":
				f.commit.Parents[0] = strings.Repeat("e", 40)
			case "cancel":
				f.afterClaim = func() {
					if err := db.CancelBugDelivery(t.Context(), in.SessionID); err != nil {
						t.Fatal(err)
					}
				}
			case "source":
				n := 0
				h.Observe = func(context.Context, store.BugCheckBinding) error {
					n++
					if n > 1 {
						return errors.New("source changed after dispatch")
					}
					return nil
				}
			}
			out, err := h.Merge(t.Context(), db, in)
			if err != nil || out.Outcome != "blocked" || out.Merge.Exception == "" {
				t.Fatalf("exception: %+v %v", out, err)
			}
			if _, err = db.ConfirmedBugIntegration(t.Context(), in.SessionID, in.Operation); err == nil {
				t.Fatal("exception allowed closure")
			}
			f.target = f.commit.SHA
			out, err = h.Merge(t.Context(), db, in)
			if err != nil || out.Outcome != "blocked" || f.merges != 1 {
				t.Fatalf("exception waived: %+v %v", out, err)
			}
		})
	}
}
func TestBugMergeJourneyExternalAndStaleGuards(t *testing.T) {
	for _, kind := range []string{"external", "late-approval", "target", "no-approval"} {
		t.Run(kind, func(t *testing.T) {
			db, _, f, h, in := mergeJourneySetup(t)
			switch kind {
			case "target":
				f.target = strings.Repeat("e", 40)
			case "no-approval":
				if err := db.CancelBugDelivery(t.Context(), in.SessionID); err != nil {
					t.Fatal(err)
				}
			default:
				f.p.Merged = true
				f.p.State = "closed"
				f.p.MergeSHA = f.commit.SHA
				f.target = f.commit.SHA
				f.p.MergedAt = in.At.Add(-time.Hour)
				if kind == "late-approval" {
					f.p.MergedAt = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
				}
			}
			out, err := h.Merge(t.Context(), db, in)
			if err != nil {
				t.Fatal(err)
			}
			want := "blocked"
			if kind == "external" {
				want = "confirmed"
			}
			if kind == "target" {
				want = "stale"
			}
			if out.Outcome != want || f.merges != 0 {
				t.Fatalf("guard: %+v mutations=%d", out, f.merges)
			}
		})
	}
}

func TestBugMergeAtomicClaimAndRetryRefusal(t *testing.T) {
	db, _, _, _, in := mergeJourneySetup(t)
	if _, e := db.EnsureBugMerge(t.Context(), in.SessionID, in.Operation, in.ReviewDigest); e != nil {
		t.Fatal(e)
	}
	_, revision, e := db.BugMergePrerequisites(t.Context(), in.SessionID, in.Operation)
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
			claimed, e := db.ClaimBugMerge(t.Context(), in.SessionID, in.Operation, revision)
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
	if e = db.PrepareBugMergeRetry(t.Context(), in.SessionID, in.Operation, "retry", "PR remains open", revision); e == nil {
		t.Fatal("uncertain dispatch authorized retry")
	}
}
