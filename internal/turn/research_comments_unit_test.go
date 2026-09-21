package turn_test

import (
	"fmt"
	"testing"

	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/turn"
)

// Publication is an explicit graph operation. Private progress and delivery
// uncertainty must not become accepted public kinds or successful graph edges.
func TestResearchCommentDeclaration(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, action, kind, outcome string
		valid                       bool
	}{
		{"question/accepted", "lf_ask", "question", "published", true},
		{"review/accepted", "lf_ask", "review", "published", true},
		{"question comment/rejected", "lf_comment", "question", "published", false},
		{"final ask/rejected", "lf_ask", "final", "published", false},
		{"final/accepted", "lf_comment", "final", "published", true},
		{"failure/accepted", "lf_comment", "failure", "published", true},
		{"private progress/rejected", "lf_comment", "progress", "published", false},
		{"missing kind/rejected", "lf_comment", "", "published", false},
		{"unknown kind/rejected", "lf_comment", "arbitrary", "published", false},
		{"pending delivery/rejected", "lf_comment", "final", "pending", false},
		{"uncertain delivery/rejected", "lf_comment", "final", "uncertain", false},
		{"failed delivery/rejected", "lf_comment", "final", "failed", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var registry graphexec.Registry
			if err := registry.Register(turn.ResearchCommentSpec()); err != nil {
				t.Fatal(err)
			}
			if err := registry.Register(turn.ResearchAskSpec()); err != nil {
				t.Fatal(err)
			}
			kind := ""
			if tt.kind != "" {
				kind = fmt.Sprintf(",lf_kind=%q", tt.kind)
			}
			dot := fmt.Sprintf(`digraph {team=builtin;mode=research;profile_version=1;start=comment;
comment [lf_action=%s,lf_outcomes=%q%s];
end [lf_terminal=true];comment -> end [on=%q];}`, tt.action, tt.outcome, kind, tt.outcome)
			profile, err := graphexec.ParseProfile([]byte(dot))
			if err != nil {
				t.Fatal(err)
			}

			err = graphexec.Validate(profile, registry)

			if (err == nil) != tt.valid {
				t.Fatalf("validation error = %v, want valid = %v", err, tt.valid)
			}
		})
	}
}
