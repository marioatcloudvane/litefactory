package ask_test

import (
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
