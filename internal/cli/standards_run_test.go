package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/standards"
)

func TestStandardsRunInspectionPreservesBinding(t *testing.T) {
	f := newLogsHome(t)
	session := f.session(f.repo("acme", "api"), 123)
	run := f.allocateRun(session, 1, time.Now())
	root, err := f.db.ResolveStandardsRoot(t.Context(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	runtime := &standards.Runtime{Store: f.db, RootRunID: fmt.Sprint(root), ConsumerKey: "worker"}
	if _, err = runtime.Bind(t.Context(), true); err != nil {
		t.Fatal(err)
	}
	before, err := f.db.GetStandardsRunBinding(t.Context(), fmt.Sprint(root))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	env := &Env{Stdout: &out}
	if err = inspectRunStandards(t.Context(), env, f.db, run.ID, true); err != nil {
		t.Fatal(err)
	}
	var view standards.RunInspection
	if err = json.Unmarshal(out.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.Status.Integrity != "unavailable" || view.Status.Binding.RootRunID != fmt.Sprint(root) {
		t.Fatalf("incorrect inspection: %+v", view)
	}
	out.Reset()
	if err = inspectRunStandards(t.Context(), env, f.db, run.ID, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Fresh verification: unavailable") || !strings.Contains(out.String(), "local-published-book") {
		t.Fatal(out.String())
	}
	service := &standards.Service{Store: f.db, OwnerID: 71}
	proposal, err := service.Create(t.Context(), "first", 0, []standards.Operation{
		{Kind: "category.add", Category: &standards.Category{ID: "go", Name: "Go"}},
		{Kind: "standard.add", Standard: &standards.Standard{ID: "errors", Rule: "MUST Errors", Description: "Handle errors.", Evidence: "Evidence.", Scope: "All repositories.", Check: "manual", CategoryID: "go", AppliesTo: []string{"story-worker"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Approve(t.Context(), proposal.ProposalID, proposal.Version, proposal.ChangeDigest); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Publish(t.Context(), proposal.ProposalID, proposal.Version, proposal.ChangeDigest); err != nil {
		t.Fatal(err)
	}
	next := f.allocateRun(session, 2, time.Now())
	nextRuntime := &standards.Runtime{Store: f.db, RootRunID: fmt.Sprint(next.ID), ConsumerKey: "worker", AttemptID: "attempt"}
	if _, err = nextRuntime.Bind(t.Context(), true); err != nil {
		t.Fatal(err)
	}
	if _, err = nextRuntime.Select(t.Context(), 0, []string{"go"}, "Go task"); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err = inspectRunStandards(t.Context(), env, f.db, next.ID, true); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(out.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.Status.Integrity != "verified" || len(view.Consumers) != 1 || !reflect.DeepEqual(view.Consumers[0].RetrievalGaps, []string{"errors"}) {
		t.Fatalf("missing consumer gaps: %+v", view)
	}
	out.Reset()
	if err = inspectRunStandards(t.Context(), env, f.db, next.ID, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Retrieval gaps: [errors]") || !strings.Contains(out.String(), "Go task") {
		t.Fatal(out.String())
	}
	after, err := f.db.GetStandardsRunBinding(t.Context(), fmt.Sprint(root))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("inspection changed binding")
	}
}
