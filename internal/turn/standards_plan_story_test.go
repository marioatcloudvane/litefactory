package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
)

// planStoryTechnicalStandards is a published book with two plan-technical
// standards and one for another role.
var planStoryTechnicalStandards = []standards.Operation{
	{Kind: "category.add", Category: &standards.Category{ID: "go", Name: "Go"}},
	{Kind: "standard.add", Standard: &standards.Standard{ID: "design", Rule: "MUST name the seams", Description: "Name the seams.\n", Evidence: "Evidence.", Scope: "All repositories.", Check: "manual", CategoryID: "go", AppliesTo: []string{"plan-technical"}}},
	{Kind: "standard.add", Standard: &standards.Standard{ID: "errors", Rule: "MUST wrap errors", Description: "Wrap errors.\n", Evidence: "Evidence.", Scope: "All repositories.", Check: "manual", CategoryID: "go", AppliesTo: []string{"story-worker"}}},
	{Kind: "category.add", Category: &standards.Category{ID: "tests", Name: "Testing"}},
	{Kind: "standard.add", Standard: &standards.Standard{ID: "table", Rule: "MUST plan table tests", Description: "Plan table tests.\n", Evidence: "Evidence.", Scope: "All repositories.", Check: "manual", CategoryID: "tests", AppliesTo: []string{"plan-technical"}}},
}

// C6: a scheduled graph Plan story technical stage reads the published book
// through the real MCP family dispatch, and inspecting its run (the path of
// `litefactory standards run`) shows the retained revision and digest.
func TestPlanStoryTechnicalStageReadsPublishedStandards(t *testing.T) {
	f, _, base, _, u := upstreamFixture(t)
	revision := publishRunStandards(t, f.db, "plan-story-book", 0, planStoryTechnicalStandards)
	var categories standards.RoleCategories
	var page standards.RolePage
	var runID int64
	var categoriesRaw, readRaw string
	u.instead = func(s planAgentStep) (*agent.RunResult, bool) {
		if s.story != "story-001" || s.stage != "technical" {
			return nil, false
		}
		runID = s.req.Graph.RunID
		reply, err := s.client.Call(s.t.Context(), "standards_categories", json.RawMessage(`{}`))
		if err != nil || reply.Error != "" {
			s.t.Fatalf("standards_categories: %v %s", err, reply.Error)
		}
		categoriesRaw = string(reply.Result)
		mustProgress(s.t, json.Unmarshal(reply.Result, &categories))
		reply, err = s.client.Call(s.t.Context(), "standards_read", json.RawMessage(`{}`))
		if err != nil || reply.Error != "" {
			s.t.Fatalf("standards_read: %v %s", err, reply.Error)
		}
		readRaw = string(reply.Result)
		mustProgress(s.t, json.Unmarshal(reply.Result, &page))
		return nil, false
	}
	if _, err := executePlanStoryWork(t, f, base, "story-001"); err != nil {
		t.Fatalf("technical stage: %v logs=%s", err, f.logs.String())
	}
	if runID == 0 {
		t.Fatal("story-001's technical stage did not run")
	}
	if page.Status.Integrity != "verified" || !slices.Equal(page.StandardIDs, []string{"design", "table"}) {
		t.Fatalf("standards_read did not return the published book: %s", readRaw)
	}
	if !slices.Equal(categories.Categories, []standards.RoleCategory{{ID: "go", Name: "Go", Count: 1}, {ID: "tests", Name: "Testing", Count: 1}}) {
		t.Fatalf("standards_categories: %s", categoriesRaw)
	}
	root, err := f.db.ResolveStandardsRoot(t.Context(), runID)
	mustProgress(t, err)
	t.Logf("run %d root %d", runID, root)
	view, err := (&standards.Runtime{Store: f.db, RootRunID: fmt.Sprint(root)}).Inspect(t.Context())
	mustProgress(t, err)
	if view.Status.Binding.RevisionID != revision.ID || view.Status.Binding.Digest != revision.Digest || view.Status.Integrity != "verified" {
		t.Fatalf("inspection of run %d (root %d) shows %+v, want revision %d digest %s", runID, root, view, revision.ID, revision.Digest)
	}
}

// executePlanStoryWork runs the factory's scheduled Plan work for one story.
func executePlanStoryWork(t *testing.T, f *fixture, base Request, key string) (Result, error) {
	t.Helper()
	return Execute(t.Context(), f.opts, planWorkRun(t, f, base, key))
}

// flakyBindingStore refuses the first failSaves binding saves, as a store that
// cannot write for a moment does, and counts the saves it lets through.
type flakyBindingStore struct {
	standards.RuntimeStore
	failSaves, saves int
}

func (s *flakyBindingStore) SaveStandardsRunBinding(ctx context.Context, b store.StandardsRunBinding) (store.StandardsRunBinding, error) {
	if s.failSaves > 0 {
		s.failSaves--
		return b, errors.New("injected binding write refusal")
	}
	s.saves++
	return s.RuntimeStore.SaveStandardsRunBinding(ctx, b)
}

func planTechnicalStandardsService(t *testing.T, flaky *flakyBindingStore, root string) *StandardsToolService {
	t.Helper()
	binding := store.ResultBinding{Attempt: "attempt-standards-" + root}
	svc, err := NewStandardsToolService(&standards.Runtime{Store: flaky, RootRunID: root, ConsumerKey: standardsConsumerKey("plan", "technical_agent"), AttemptID: binding.Attempt}, binding, "plan-technical")
	mustProgress(t, err)
	return svc
}

// C7: a failed initial binding save is named in the reply and retried on the
// next call, not latched for the rest of the run.
func TestStandardsBindingSaveFailureIsReportedAndRetried(t *testing.T) {
	_, db := lifecycleMCPPreparation(t, "plan")
	revision := publishRunStandards(t, db, "flaky-binding-book", 0, planStoryTechnicalStandards)
	flaky := &flakyBindingStore{RuntimeStore: db, failSaves: 1}
	svc := planTechnicalStandardsService(t, flaky, "7101")
	raw, err := svc.read(t.Context(), "read-1", []byte(`{}`))
	mustProgress(t, err)
	var refused struct {
		Valid  bool   `json:"valid"`
		Reason string `json:"reason"`
	}
	mustProgress(t, json.Unmarshal(raw, &refused))
	if refused.Valid || !strings.Contains(refused.Reason, "injected binding write refusal") {
		t.Fatalf("the first reply does not name the save failure: %s", raw)
	}
	if _, err := db.GetStandardsRunBinding(t.Context(), "7101"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("a binding was retained although its save failed: %v", err)
	}
	raw, err = svc.read(t.Context(), "read-2", []byte(`{}`))
	mustProgress(t, err)
	var page standards.RolePage
	mustProgress(t, json.Unmarshal(raw, &page))
	if page.Status.Integrity != "verified" || !slices.Equal(page.StandardIDs, []string{"design", "table"}) {
		t.Fatalf("the retry after the store recovered did not read the book: %s", raw)
	}
	retained, err := db.GetStandardsRunBinding(t.Context(), "7101")
	mustProgress(t, err)
	if retained.RevisionID != revision.ID || flaky.saves != 1 {
		t.Fatalf("retained %+v after %d saves, want revision %d and one save", retained, flaky.saves, revision.ID)
	}
}

// C8: later calls of one run keep the first retained book, even after a newer
// revision is published, and never write a second binding.
func TestStandardsRepeatedCallsKeepTheFirstBook(t *testing.T) {
	_, db := lifecycleMCPPreparation(t, "plan")
	first := publishRunStandards(t, db, "first-run-book", 0, planStoryTechnicalStandards)
	flaky := &flakyBindingStore{RuntimeStore: db}
	svc := planTechnicalStandardsService(t, flaky, "7102")
	read := func(id string) standards.RolePage {
		t.Helper()
		raw, err := svc.read(t.Context(), id, []byte(`{}`))
		mustProgress(t, err)
		var page standards.RolePage
		mustProgress(t, json.Unmarshal(raw, &page))
		if page.Status.Integrity != "verified" {
			t.Fatalf("read %s: %s", id, raw)
		}
		return page
	}
	before := read("read-1")
	second := publishRunStandards(t, db, "second-run-book", first.ID, []standards.Operation{{Kind: "category.add", Category: &standards.Category{ID: "later", Name: "Later"}}})
	after := read("read-2")
	if second.Digest == first.Digest || before.Status.Binding.Digest != first.Digest || after.Status.Binding.Digest != first.Digest {
		t.Fatalf("digests: first %s second %s, replies %s and %s", first.Digest, second.Digest, before.Status.Binding.Digest, after.Status.Binding.Digest)
	}
	if flaky.saves != 1 {
		t.Fatalf("%d binding saves, want one", flaky.saves)
	}
}
