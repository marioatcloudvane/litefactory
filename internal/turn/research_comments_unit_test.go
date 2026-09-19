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
		name, kind, outcome string
		valid               bool
	}{
		{"question/accepted", "question", "published", true},
		{"final/accepted", "final", "published", true},
		{"failure/accepted", "failure", "published", true},
		{"private progress/rejected", "progress", "published", false},
		{"missing kind/rejected", "", "published", false},
		{"unknown kind/rejected", "arbitrary", "published", false},
		{"pending delivery/rejected", "final", "pending", false},
		{"uncertain delivery/rejected", "final", "uncertain", false},
		{"failed delivery/rejected", "final", "failed", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var registry graphexec.Registry
			if err := registry.Register(turn.ResearchCommentSpec()); err != nil {
				t.Fatal(err)
			}
			kind := ""
			if tt.kind != "" {
				kind = fmt.Sprintf(",lf_kind=%q", tt.kind)
			}
			dot := fmt.Sprintf(`digraph {team=builtin;mode=research;profile_version=1;start=comment;
comment [lf_action=lf_research_comment,lf_outcomes=%q%s];
end [lf_terminal=true];comment -> end [on=%q];}`, tt.outcome, kind, tt.outcome)
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
