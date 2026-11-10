package daemon

import (
	"errors"
	"testing"

	"github.com/cloudvane/litefactory/internal/brainstorm"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/loganalysis"
	"github.com/cloudvane/litefactory/internal/projectconnection"
)

// The brainstorm issue search picks the project's ticket tracker by the log
// analysis rule, and a project without a ticket connection is refused plainly.
func TestLogAnalysisTrackerRuleSelectsBrainstormIssueTracker(t *testing.T) {
	f := newProjectFixture(t, false)
	ref := "github:" + f.repo.Owner + "/" + f.repo.Name
	tracker, err := f.d.brainstormIssueSelector()(t.Context(), ref)
	if err != nil {
		t.Fatal(err)
	}
	gt, ok := tracker.(loganalysis.GitHubTracker)
	if !ok || gt.Repo != (github.Repo{Owner: "team", Name: "tickets"}) || gt.Client == nil {
		t.Fatalf("tracker %#v", tracker)
	}

	d, _, repo := daemonFixture(t)
	d.connections = projectconnection.NewFactory(projectconnection.Options{})
	if _, err = d.brainstormIssueSelector()(t.Context(), "github:"+repo.Owner+"/"+repo.Name); !errors.Is(err, brainstorm.ErrNoTracker) {
		t.Fatalf("no ticket connection: %v", err)
	}
	if _, err = d.brainstormIssueSelector()(t.Context(), "github:acme/unregistered"); !errors.Is(err, brainstorm.ErrNotFound) {
		t.Fatalf("unregistered repository: %v", err)
	}
	if _, err = d.brainstormIssueSelector()(t.Context(), "github:broken"); !errors.Is(err, brainstorm.ErrInvalid) {
		t.Fatalf("malformed reference: %v", err)
	}
}
