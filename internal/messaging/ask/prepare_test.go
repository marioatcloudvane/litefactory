package ask_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging/ask"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestExplicitBugAnswerScopesEvidence(t *testing.T) {
	at := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	question := "What happened with bug 726, is it delivered?"
	repo := store.Repo{ID: 1, Forge: "github", Owner: "owner", Name: "app"}
	for _, present := range []bool{false, true} {
		f := &factory{tasks: []ask.Task{{IssueNumber: 723, Reference: "github:owner/app#723"}}}
		if present {
			f.tasks = append(f.tasks, ask.Task{IssueNumber: 726, Reference: "github:owner/app#726", LatestRun: &store.Run{Ordinal: 1, Status: store.RunSucceeded}})
		}
		s := &ask.Service{Repositories: catalog{repo}, Collector: ask.Collector{Factory: f, Issues: &forge{}}, Redactor: redact.New(redact.Options{})}
		prepared, err := s.Prepare(t.Context(), question, "", "3", at)
		if err != nil {
			t.Fatal(err)
		}
		if prepared.Bundle.TargetIssue != 726 || len(prepared.History.Turns) != 0 {
			t.Fatalf("unscoped: %+v", prepared)
		}
		for _, fact := range prepared.Bundle.Facts {
			if strings.Contains(fact.Handle, "723") {
				t.Fatalf("unrelated evidence: %+v", fact)
			}
		}
		// Closed tasks remain available for an explicitly named issue even after a
		// complete open-issue read omits them.
		if want := map[bool]int{false: 0, true: 2}[present]; len(prepared.Bundle.Facts) != want {
			t.Fatalf("closed evidence: %+v", prepared.Bundle)
		}
	}
}

type exactIssueFactory struct {
	factory
	selected []ask.Task
}

func (f *exactIssueFactory) ReadIssueTasks(_ context.Context, repo store.Repo, issue int) ([]ask.Task, error) {
	if repo.ID != 1 || issue != 726 {
		return nil, errors.New("unexpected target")
	}
	return f.selected, nil
}

func TestNamedIssueOutsideBoundedProjectList(t *testing.T) {
	repo := store.Repo{ID: 1, Forge: "github", Owner: "owner", Name: "app"}
	f := &exactIssueFactory{factory: factory{tasks: []ask.Task{{IssueNumber: 723}}}, selected: []ask.Task{{IssueNumber: 726, Reference: "github:owner/app#726"}}}
	service := ask.Service{Repositories: catalog{repo}, Collector: ask.Collector{Factory: f, Issues: &forge{}}, Redactor: redact.New(redact.Options{})}
	prepared, err := service.Prepare(t.Context(), "What about #726?", "owner/app", "channel", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(prepared.Bundle.Facts) != 1 || prepared.Bundle.Facts[0].Handle != "task:726" {
		t.Fatalf("target evidence: %+v", prepared.Bundle)
	}
	if !strings.Contains(strings.Join(prepared.Bundle.Gaps, " "), "named issue text was not captured") {
		t.Fatalf("missing issue text not explained: %+v", prepared.Bundle.Gaps)
	}
}
