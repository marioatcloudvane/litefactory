package turn

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/buganalysis"
)

func TestRenderBugAnalysisShowsPrevention(t *testing.T) {
	t.Parallel()
	a := buganalysis.BugAnalysis{Expected: "e", Actual: "a", Cause: "c", Remedy: "r", Reproduction: buganalysis.BugReproduction{Observation: "observed"}}
	if got := renderBugAnalysis(1, "digest", a); strings.Contains(got, "Prevention") {
		t.Fatalf("analysis without a note renders a prevention line:\n%s", got)
	}
	cases := []struct {
		p    buganalysis.BugPrevention
		want []string
	}{
		{buganalysis.BugPrevention{Kind: "missing_standard", Note: "Check lengths before indexing."}, []string{"Check lengths before indexing."}},
		{buganalysis.BugPrevention{Kind: "one_off", Note: "A typo\nin a fixture."}, []string{"A typo in a fixture."}},
	}
	for _, c := range cases {
		p := c.p
		a.Prevention = &p
		got := renderBugAnalysis(1, "digest", a)
		line := ""
		for _, l := range strings.Split(got, "\n") {
			if strings.HasPrefix(l, "**Prevention:**") {
				line = l
			}
		}
		if line == "" {
			t.Fatalf("%s: no prevention line:\n%s", p.Kind, got)
		}
		for _, w := range c.want {
			if !strings.Contains(line, w) {
				t.Fatalf("%s: prevention line %q lacks %q", p.Kind, line, w)
			}
		}
	}
}
