package turn

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/buganalysis"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

func TestBugParentPackageClosedRoutes(t *testing.T) {
	specs, e := BugParentSpecs()
	if e != nil {
		t.Fatal(e)
	}
	def, e := teamsource.CaptureBugParent(context.Background(), specs)
	if e != nil {
		t.Fatal(e)
	}
	for _, p := range []string{"bug.dot", "bug_tests.dot", "bug-analyst.md", "bug-fixer.md", "skills/bug-analysis-result/SKILL.md", "skills/bug-repair-result/SKILL.md"} {
		if len(def.Files[p]) == 0 {
			t.Errorf("missing %s", p)
		}
	}
	for path, raw := range def.Files {
		source, e := os.ReadFile(filepath.Join("../../teams/source/bug", path))
		if e != nil || string(source) != string(raw) {
			t.Errorf("source parity %s: %v", path, e)
		}
	}
	for _, n := range def.Profile.Nodes {
		if n.Terminal {
			continue
		}
		if n.Limit <= 0 {
			t.Errorf("unbounded %s", n.Name)
		}
		for _, out := range n.Outcomes {
			if _, ok := graphexec.Decide(def.Profile, n.Name, out); !ok {
				t.Errorf("missing %s/%s", n.Name, out)
			}
		}
	}
	for _, bad := range []string{strings.Replace(string(def.Files["bug.dot"]), "bug-fixer", "arbitrary-role", 1), strings.Replace(string(def.Files["bug.dot"]), "lf_limit=\"4\"", "lf_limit=\"999\"", 1)} {
		p, e := graphexec.ParseProfile([]byte(bad))
		if e == nil && teamsource.ValidateBugParent(p) == nil {
			t.Fatal("altered factory parent accepted")
		}
	}
	for _, route := range []struct{ node, out, next string }{{"test", "failed", "attempt"}, {"pr", "repair", "attempt"}, {"attempt", "reserved", "fixer"}, {"merge", "confirmed", "close"}, {"close", "pending", "report"}, {"record_analysis", "approved", "attempt"}} {
		got, ok := graphexec.Decide(def.Profile, route.node, route.out)
		if !ok || got != route.next {
			t.Errorf("route %+v: %s", route, got)
		}
	}
}
func TestBugRepairTypedBoundsObservation(t *testing.T) {
	a := store.BugAnalysisRevision{Digest: strings.Repeat("a", 64), Analysis: buganalysis.BugAnalysis{Boundaries: &buganalysis.BugRepairBoundaries{AllowedPaths: []string{"src/search.go", "src/search_test.go"}, ExcludedPaths: []string{"src/ranking.go"}}}}
	s := git.BugRepairSnapshot{DiffDigest: strings.Repeat("b", 64), Paths: []string{"src/search.go", "src/search_test.go"}}
	if d := BugRepairBoundedScope(a, s); d.Disposition != "within" || d.AnalysisDigest != a.Digest || d.DiffDigest != s.DiffDigest {
		t.Fatalf("matching actual diff: %+v", d)
	}
	s.Paths = append(s.Paths, "src/ranking.go")
	if d := BugRepairBoundedScope(a, s); d.Disposition != "outside" {
		t.Fatalf("excluded change: %+v", d)
	}
	s.Paths = []string{"other.go"}
	if d := BugRepairBoundedScope(a, s); d.Disposition != "outside" {
		t.Fatalf("unlisted change: %+v", d)
	}
	a.Analysis.Boundaries = nil
	if d := BugRepairBoundedScope(a, s); d.Disposition != "indeterminate" {
		t.Fatalf("historical scope: %+v", d)
	}
}
