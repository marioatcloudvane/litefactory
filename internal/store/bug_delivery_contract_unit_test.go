package store_test

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
)

func TestParseBugDeliveryControl_ExactOwnerControls(t *testing.T) {
	t.Parallel()
	r := store.BugDeliveryReview{Digest: strings.Repeat("d", 64), PR: store.BugPullRequest{Number: 42, Head: strings.Repeat("a", 40)}, Evidence: store.BugDeliveryEvidence{Child: store.BugTestCall{Request: store.BugTestRequest{Binding: store.BugCheckBinding{TargetCommit: strings.Repeat("b", 40)}}}}}
	approval := r.ApprovalCommand()
	prefix := "/bug-review " + r.Digest + " "
	cases := []struct {
		name, body, want string
		bad              bool
	}{
		{"exact approval", approval, "approved", false},
		{"clarification", prefix + "clarify Explain AC-001 evidence", "clarify", false},
		{"in-scope repair", prefix + "repair Fix the missing edge case", "repair", false},
		{"material remedy", prefix + "reshape A different remedy is needed", "reshape", false},
		{"free text", "Looks fine to me", "discussion", false},
		{"mixed approval", approval + " but change the tests", "", true},
		{"two commands", approval + "\n" + prefix + "repair Change tests", "", true},
		{"stale review", strings.Replace(approval, r.Digest, strings.Repeat("e", 64), 1), "", true},
		{"wrong PR", strings.Replace(approval, " 42 ", " 43 ", 1), "", true},
		{"wrong target", strings.Replace(approval, " main ", " release ", 1), "", true},
		{"incomplete approval", "/approve-bug delivery " + r.Digest, "", true},
		{"missing feedback", prefix + "repair", "", true},
		{"unknown control", prefix + "merge now", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := store.ParseBugDeliveryControl(r, tc.body)
			if tc.bad {
				if err == nil {
					t.Fatalf("invalid control accepted as %q", got)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestBugDeliveryReview_ApprovalCommandExactBinding(t *testing.T) {
	t.Parallel()
	r := store.BugDeliveryReview{
		Digest: strings.Repeat("d", 64),
		PR:     store.BugPullRequest{Number: 395, Head: strings.Repeat("a", 40)},
		Evidence: store.BugDeliveryEvidence{Child: store.BugTestCall{Request: store.BugTestRequest{
			Binding: store.BugCheckBinding{TargetBranch: "main", TargetCommit: strings.Repeat("b", 40)},
		}}},
	}
	want := "/approve-bug delivery " + strings.Repeat("d", 64) + " 395 " + strings.Repeat("a", 40) + " main " + strings.Repeat("b", 40)
	if got := r.ApprovalCommand(); got != want {
		t.Fatalf("approval control = %q, want %q", got, want)
	}
}

func TestBugDeliveryReviewDigest_BindsReviewedEvidence(t *testing.T) {
	t.Parallel()
	e := store.BugDeliveryEvidence{
		Identity: store.BugIdentity{SessionID: 1, Repository: "owner/repo", IssueID: 385, IssueNumber: 385, OwnerID: 7},
		Analysis: store.BugAnalysisRevision{Revision: 2, Digest: "analysis"},
		Approval: store.BugAnalysisApproval{Review: "review", AnalysisRevision: 2, AnalysisDigest: "analysis", ReplyID: 9, OwnerID: 7},
		Repair:   store.BugRepairCompletion{SessionID: 1, Request: "repair", SourceCommit: "head", SourceTree: "tree"},
		Child: store.BugTestCall{ID: 3, Generation: 1, Request: store.BugTestRequest{Binding: store.BugCheckBinding{
			SourceCommit: "head", SourceTree: "tree", TargetRepository: "owner/repo", TargetBranch: "main", TargetCommit: "target",
			RecipeDigest: "recipe", EnvironmentRevision: "env", PermissionRevision: "permission",
		}}},
	}
	p := store.BugPullRequest{ID: 5, Number: 6, CreatorID: 8, Head: "head", Branch: "lf/385", Target: "main", HeadRepo: "owner/repo", BaseRepo: "owner/repo", State: "open"}
	baseline, err := store.BugDeliveryReviewDigest(e, p)
	if err != nil {
		t.Fatal(err)
	}
	if raw, err := hex.DecodeString(baseline); err != nil || len(raw) != 32 {
		t.Fatalf("review digest is not SHA256: %q", baseline)
	}
	again, err := store.BugDeliveryReviewDigest(e, p)
	if err != nil || again != baseline {
		t.Fatalf("identical review changed digest: %q, %v", again, err)
	}
	cases := []struct {
		name   string
		change func(*store.BugDeliveryEvidence, *store.BugPullRequest)
	}{
		{"task", func(e *store.BugDeliveryEvidence, _ *store.BugPullRequest) { e.Identity.IssueID++ }},
		{"analysis revision", func(e *store.BugDeliveryEvidence, _ *store.BugPullRequest) { e.Analysis.Revision++ }},
		{"analysis approval", func(e *store.BugDeliveryEvidence, _ *store.BugPullRequest) { e.Approval.ReplyID++ }},
		{"repair", func(e *store.BugDeliveryEvidence, _ *store.BugPullRequest) { e.Repair.Request = "other" }},
		{"child", func(e *store.BugDeliveryEvidence, _ *store.BugPullRequest) { e.Child.ID++ }},
		{"generation", func(e *store.BugDeliveryEvidence, _ *store.BugPullRequest) { e.Child.Generation++ }},
		{"source tree", func(e *store.BugDeliveryEvidence, _ *store.BugPullRequest) {
			e.Child.Request.Binding.SourceTree = "other"
		}},
		{"recipe", func(e *store.BugDeliveryEvidence, _ *store.BugPullRequest) {
			e.Child.Request.Binding.RecipeDigest = "other"
		}},
		{"target commit", func(e *store.BugDeliveryEvidence, _ *store.BugPullRequest) {
			e.Child.Request.Binding.TargetCommit = "other"
		}},
		{"target repository", func(e *store.BugDeliveryEvidence, _ *store.BugPullRequest) {
			e.Child.Request.Binding.TargetRepository = "other/repo"
		}},
		{"environment", func(e *store.BugDeliveryEvidence, _ *store.BugPullRequest) {
			e.Child.Request.Binding.EnvironmentRevision = "other"
		}},
		{"permissions", func(e *store.BugDeliveryEvidence, _ *store.BugPullRequest) {
			e.Child.Request.Binding.PermissionRevision = "other"
		}},
		{"PR identity", func(_ *store.BugDeliveryEvidence, p *store.BugPullRequest) { p.ID++ }},
		{"PR head", func(_ *store.BugDeliveryEvidence, p *store.BugPullRequest) { p.Head = "other" }},
		{"PR target", func(_ *store.BugDeliveryEvidence, p *store.BugPullRequest) { p.Target = "other" }},
	}
	for _, tc := range cases {
		t.Run(tc.name+"/changes review digest", func(t *testing.T) {
			t.Parallel()
			changedEvidence, changedPR := e, p
			tc.change(&changedEvidence, &changedPR)
			got, err := store.BugDeliveryReviewDigest(changedEvidence, changedPR)
			if err != nil {
				t.Fatal(err)
			}
			if got == baseline {
				t.Fatal("changed reviewed evidence retained old approval digest")
			}
		})
	}
}
