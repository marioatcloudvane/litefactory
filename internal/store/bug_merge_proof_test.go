package store_test

import (
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

func TestBugIntegrationProofMatrix(t *testing.T) {
	h, tgt, merged := strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40)
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p := store.BugPullRequest{ID: 1, Number: 2, CreatorID: 3, Head: h, Branch: "task", Target: "main", HeadRepo: "a/b", BaseRepo: "a/b", State: "closed", Merged: true}
	m := store.BugMergeOperation{Binding: store.BugMergeBinding{Review: store.BugDeliveryReview{PR: p}, Approval: store.BugDeliveryDecision{At: at}}}
	m.Binding.Review.Evidence.Approval.ApprovedAt = at
	m.Binding.Review.Evidence.Child.Request.Binding.TargetCommit = tgt
	for _, tc := range []struct {
		name, want string
		change     func(*store.BugIntegrationObservation)
	}{
		{"complete", "confirmed", func(*store.BugIntegrationObservation) {}},
		{"response-only", "pending", func(o *store.BugIntegrationObservation) { o.PR.Merged = false }},
		{"missing-time", "blocked", func(o *store.BugIntegrationObservation) { o.MergedAt = time.Time{} }},
		{"late-approval", "blocked", func(o *store.BugIntegrationObservation) { o.MergedAt = at.Add(-time.Second) }},
		{"wrong-pr", "blocked", func(o *store.BugIntegrationObservation) { o.PR.ID++ }},
		{"wrong-head", "blocked", func(o *store.BugIntegrationObservation) { o.PR.Head = tgt }},
		{"wrong-base", "blocked", func(o *store.BugIntegrationObservation) { o.Parents = []string{h, tgt} }},
		{"squash", "blocked", func(o *store.BugIntegrationObservation) { o.Parents = []string{tgt} }},
		{"main-moved", "blocked", func(o *store.BugIntegrationObservation) { o.Main = tgt }},
		{"missing-main", "pending", func(o *store.BugIntegrationObservation) { o.Main = "" }},
		{"source-drift", "blocked", func(o *store.BugIntegrationObservation) { o.BindingError = "source changed" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := store.BugIntegrationObservation{PR: p, MergeCommit: merged, Parents: []string{tgt, h}, Main: merged, MergedAt: at.Add(time.Second)}
			tc.change(&o)
			got, _ := store.BugIntegrationProof(m, o)
			if got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
}
