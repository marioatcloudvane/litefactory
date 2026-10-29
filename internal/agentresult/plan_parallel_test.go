package agentresult_test

import (
	ar "github.com/cloudvane/litefactory/internal/agentresult"
	"testing"
)

func TestParallelContributionBinding(t *testing.T) {
	ctx := graphContext()
	ctx.PlanGraph.Member = &ar.PlanMemberBinding{Group: "g", Role: "technical", AttemptID: 7, Basis: "basis", ProposalDigest: "proposal"}
	cases := []struct {
		name, payload string
		valid         bool
	}{
		{"owned", `{"contribution":{"draft":{"key":"story-001"},"agreement":"proposal"}}`, true},
		{"wrong role", `{"contribution":{"draft":{"name":"changed"}}}`, false},
		{"stale proposal", `{"contribution":{"draft":{},"agreement":"old"}}`, false},
		{"wrong story", `{"contribution":{"draft":{"key":"story-002"}}}`, false},
		{"no contribution", `{}`, false},
		{"direct effect", `{"contribution":{"draft":{}},"storyset":{"entries":[{"id":"x","name":"x","intent":"x"}]}}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, d := ar.Create(graphDone(tc.payload), ctx)
			if d.Valid != tc.valid {
				t.Fatalf("valid=%v want %v: %v", d.Valid, tc.valid, d)
			}
		})
	}
	ctx.PlanGraph.Member = nil
	if _, d := ar.Create(graphDone(cases[0].payload), ctx); d.Valid {
		t.Fatal("ordinary agent supplied contribution")
	}
}
