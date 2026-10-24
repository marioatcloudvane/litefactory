package config

import "testing"

func TestLogAnalysisUpstreamIsAGitHubRepositoryOrEmpty(t *testing.T) {
	for _, value := range []string{"", "github:marioatcloudvane/litefactory"} {
		c := Defaults()
		c.Standards.LogAnalysis.Upstream = value
		if errs := c.Standards.validate(); len(errs) != 0 {
			t.Fatalf("%q refused: %v", value, errs)
		}
	}
	owner, name, ok := LogAnalysisSettings{Upstream: "github:marioatcloudvane/litefactory"}.UpstreamRepository()
	if !ok || owner != "marioatcloudvane" || name != "litefactory" {
		t.Fatalf("upstream = %s/%s %v", owner, name, ok)
	}
	if _, _, ok = (LogAnalysisSettings{}).UpstreamRepository(); ok {
		t.Fatal("empty upstream configured")
	}
	for _, value := range []string{"marioatcloudvane/litefactory", "gitlab:a/b", "github:a", "github:a/b/c", "github:a/b.git"} {
		c := Defaults()
		c.Standards.LogAnalysis.Upstream = value
		if errs := c.Standards.validate(); len(errs) == 0 {
			t.Fatalf("%q accepted", value)
		}
	}
}
