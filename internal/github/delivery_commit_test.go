package github_test

import (
	"fmt"
	"strings"
	"testing"
)

func TestDeliveryCommitRequiresExactSHAAndOrderedParents(t *testing.T) {
	m, a, b := strings.Repeat("c", 40), strings.Repeat("a", 40), strings.Repeat("b", 40)
	for _, tc := range []struct {
		name, body string
		valid      bool
	}{
		{"ordered", fmt.Sprintf(`{"sha":%q,"parents":[{"sha":%q},{"sha":%q}]}`, m, a, b), true},
		{"wrong-object", fmt.Sprintf(`{"sha":%q,"parents":[]}`, a), false},
		{"bad-parent", fmt.Sprintf(`{"sha":%q,"parents":[{"sha":"unknown"}]}`, m), false},
		{"null", "null", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, respondJSON(200, tc.body))
			out, err := f.Client.GetDeliveryCommit(t.Context(), testRepo, m)
			if (err == nil) != tc.valid {
				t.Fatalf("out=%+v err=%v", out, err)
			}
			if tc.valid && (len(out.Parents) != 2 || out.Parents[0] != a || out.Parents[1] != b) {
				t.Fatalf("lost parent order %+v", out)
			}
		})
	}
}
