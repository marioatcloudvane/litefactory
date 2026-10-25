package standards_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
)

func runtimeFixture(t *testing.T) (*standards.Runtime, *store.DB) {
	t.Helper()
	svc, _, db := publicationServices(t)
	p, e := svc.Create(t.Context(), "runtime", 0, []standards.Operation{
		{Kind: "category.add", Category: &standards.Category{ID: "go", Name: "Go"}},
		{Kind: "category.add", Category: &standards.Category{ID: "testing", Name: "Testing"}},
		{Kind: "standard.add", Standard: &standards.Standard{ID: "s", Rule: "MUST Rule", Description: strings.Repeat("é", 6000), Evidence: "Evidence.", Scope: "All repositories.", Check: "manual", CategoryID: "go", AppliesTo: []string{"story-worker"}}},
	})
	if e != nil {
		t.Fatal(e)
	}
	approvePublication(t, svc, p)
	if _, e = svc.Publish(t.Context(), p.ProposalID, p.Version, p.ChangeDigest); e != nil {
		t.Fatal(e)
	}
	r := &standards.Runtime{Store: db, RootRunID: "1", ConsumerKey: "worker", AttemptID: "attempt-1"}
	s, e := r.Bind(t.Context(), true)
	if e != nil || s.Integrity != "verified" {
		t.Fatalf("bind: %+v %v", s, e)
	}
	return r, db
}
func TestRuntimeSelectionRangesAndExplicitEmpty(t *testing.T) {
	r, _ := runtimeFixture(t)
	ctx := t.Context()
	s, e := r.Status(ctx)
	if e != nil || s.SelectionStatus != "not-selected" {
		t.Fatalf("status %+v %v", s, e)
	}
	inspectionBeforeChoice, e := r.Inspect(ctx)
	if e != nil || len(inspectionBeforeChoice.Consumers) != 1 || inspectionBeforeChoice.Consumers[0].Status.SelectionStatus != "not-selected" {
		t.Fatalf("unselected consumer missing: %+v %v", inspectionBeforeChoice, e)
	}
	s, e = r.Select(ctx, 0, []string{}, "no guidance yet")
	if e != nil || s.SelectionStatus != "selected-empty" {
		t.Fatalf("empty: %+v %v", s, e)
	}
	s, e = r.Select(ctx, 1, []string{"go", "testing"}, "implementation")
	if e != nil || len(s.Selection.StandardIDs) != 1 || s.StandardCount != 1 || s.CategoryCount != 2 {
		t.Fatalf("select %+v %v", s, e)
	}
	version, d := s.Selection.Version, s.Selection.Digest
	if _, e = r.Read(ctx, version, d, "", true); e == nil {
		t.Fatal("ack accepted before offering body")
	}
	cursor := ""
	body := ""
	for {
		page, e := r.Read(ctx, version, d, cursor, false)
		if e != nil {
			t.Fatal(e)
		}
		if !standards.FitsMCPResult(page) {
			t.Fatal("oversized page")
		}
		body += page.Body
		if page.Complete {
			break
		}
		cursor = page.NextCursor
	}
	if !strings.Contains(body, strings.Repeat("é", 6000)) {
		t.Fatal("UTF-8 description changed")
	}
	if _, e = r.Read(ctx, version, d, "", true); e != nil {
		t.Fatal(e)
	}
	s, e = r.Status(ctx)
	if e != nil || s.RetrievalStatus != "retrieved" {
		t.Fatalf("ack status %+v %v", s, e)
	}
	retry, e := r.Select(ctx, 0, []string{"go"}, "retry")
	if e != nil || retry.Selection.Version != version {
		t.Fatalf("idempotent union %+v %v", retry, e)
	}
	other := *r
	other.ConsumerKey = "reviewer"
	os, e := other.Status(ctx)
	if e != nil || os.Selection != nil {
		t.Fatal("consumer selection leaked")
	}
	inspection, e := other.Inspect(ctx)
	if e != nil || len(inspection.Consumers) != 1 || len(inspection.Consumers[0].RetrievalGaps) != 0 {
		t.Fatalf("inspection %+v %v", inspection, e)
	}
}

type corruptRuntimeStore struct{ standards.RuntimeStore }

func (s corruptRuntimeStore) GetStandardsRunBinding(ctx context.Context, root string) (store.StandardsRunBinding, error) {
	b, e := s.RuntimeStore.GetStandardsRunBinding(ctx, root)
	b.SnapshotBytes = []byte(`{}`)
	return b, e
}
func TestRuntimeCorruptAndMissingBindingsNeverResolveCurrent(t *testing.T) {
	r, _ := runtimeFixture(t)
	r.Store = corruptRuntimeStore{r.Store}
	s, e := r.Bind(t.Context(), true)
	if e != nil || s.Integrity != "corrupt" {
		t.Fatalf("corruption: %+v %v", s, e)
	}
	page, e := r.Categories(t.Context(), 25, "")
	if e != nil || len(page.Categories) != 0 {
		t.Fatal("corrupt categories served")
	}
	if _, e = r.Select(t.Context(), 0, []string{"go"}, "test"); e == nil {
		t.Fatal("corrupt selection allowed")
	}
	r.RootRunID = "missing"
	s, e = r.Bind(t.Context(), false)
	if e != nil || s.Integrity != "unavailable" || s.Binding.RevisionID != 0 {
		t.Fatalf("missing rebound %+v %v", s, e)
	}
}
func TestRuntimeStaleDifferentSelectionConflicts(t *testing.T) {
	r, _ := runtimeFixture(t)
	if _, e := r.Select(t.Context(), 0, []string{"go"}, "first"); e != nil {
		t.Fatal(e)
	}
	if _, e := r.Select(t.Context(), 0, []string{"testing"}, "stale"); !errors.Is(e, store.ErrStandardsConflict) {
		t.Fatalf("stale update: %v", e)
	}
}

type corruptSelectionStore struct {
	standards.RuntimeStore
	mutate func(*store.StandardsRunSelection)
}

func (s corruptSelectionStore) ListStandardsRunSelections(ctx context.Context, root, consumer string) ([]store.StandardsRunSelection, error) {
	choices, err := s.RuntimeStore.ListStandardsRunSelections(ctx, root, consumer)
	if len(choices) > 0 {
		s.mutate(&choices[len(choices)-1])
	}
	return choices, err
}
func TestRuntimeRejectsCorruptRetainedSelections(t *testing.T) {
	r, _ := runtimeFixture(t)
	ctx := t.Context()
	selected, err := r.Select(ctx, 0, []string{"go", "testing"}, "implementation and verification")
	if err != nil {
		t.Fatal(err)
	}
	version, d := selected.Selection.Version, selected.Selection.Digest
	cursor := ""
	for {
		page, e := r.Read(ctx, version, d, cursor, false)
		if e != nil {
			t.Fatal(e)
		}
		if page.Complete {
			break
		}
		cursor = page.NextCursor
	}
	if _, err = r.Read(ctx, version, d, "", true); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*store.StandardsRunSelection)
	}{
		{"omitted standard", func(s *store.StandardsRunSelection) { s.StandardIDs = []string{} }},
		{"unknown standard", func(s *store.StandardsRunSelection) { s.StandardIDs = []string{"unknown"} }},
		{"duplicate category", func(s *store.StandardsRunSelection) { s.CategoryIDs = []string{"go", "go", "testing"} }},
		{"unsorted category", func(s *store.StandardsRunSelection) { s.CategoryIDs = []string{"testing", "go"} }},
		{"unknown category", func(s *store.StandardsRunSelection) { s.CategoryIDs = []string{"unknown"} }},
		{"digest mismatch", func(s *store.StandardsRunSelection) { s.Digest = "bit-flipped" }},
		{"consumer mismatch", func(s *store.StandardsRunSelection) { s.ConsumerKey = "other" }},
		{"root mismatch", func(s *store.StandardsRunSelection) { s.RootRunID = "other" }},
		{"version mismatch", func(s *store.StandardsRunSelection) { s.Version = 0 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			consumer := *r
			consumer.Store = corruptSelectionStore{RuntimeStore: r.Store, mutate: tc.mutate}
			status, e := consumer.Status(ctx)
			if e != nil || status.Integrity != "verified" || status.SelectionStatus != "corrupt" || status.SelectionReason == "" || status.RetrievalStatus != "pending" {
				t.Fatalf("selection corruption status: %+v %v", status, e)
			}
			if page, e := consumer.Read(ctx, version, d, "", false); !errors.Is(e, store.ErrStandardsCorrupt) || page.Body != "" || page.Complete {
				t.Fatalf("corrupt selection served: %+v %v", page, e)
			}
			if _, e := consumer.Select(ctx, version, []string{}, "retry"); !errors.Is(e, store.ErrStandardsCorrupt) {
				t.Fatalf("corrupt selection rewritten: %v", e)
			}
		})
	}
}

type pausedInitialBookStore struct {
	standards.RuntimeStore
	started chan struct{}
	release chan struct{}
}

func (s pausedInitialBookStore) GetStandardsRevision(ctx context.Context, id int64) (store.StandardsRevision, error) {
	revision, err := s.RuntimeStore.GetStandardsRevision(ctx, id)
	close(s.started)
	select {
	case <-s.release:
		return revision, err
	case <-ctx.Done():
		return revision, ctx.Err()
	}
}
func TestRuntimeMissingSiblingBindingWinsAgainstSlowInitialCapture(t *testing.T) {
	fixture, db := runtimeFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	started := make(chan struct{})
	release := make(chan struct{})
	unpause := sync.OnceFunc(func() { close(release) })
	initial := *fixture
	initial.RootRunID = "binding-race"
	initial.Store = pausedInitialBookStore{RuntimeStore: db, started: started, release: release}
	type bindResult struct {
		status standards.RuntimeStatus
		err    error
	}
	finished := make(chan bindResult, 1)
	done := make(chan struct{})
	t.Cleanup(func() {
		unpause()
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("initial binding goroutine did not stop")
		}
	})
	// This one capture goroutine ends with the bounded context and is joined by cleanup.
	go func() { defer close(done); status, err := initial.Bind(ctx, true); finished <- bindResult{status, err} }()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatalf("waiting for initial source read: %v", ctx.Err())
	}
	sibling := *fixture
	sibling.RootRunID = initial.RootRunID
	sibling.ConsumerKey = "sibling"
	sibling.AttemptID = "child-attempt"
	unavailable, err := sibling.Bind(ctx, false)
	if err != nil || unavailable.Integrity != "unavailable" || unavailable.Binding.RevisionID != 0 {
		t.Fatalf("sibling binding: %+v %v", unavailable, err)
	}
	retained, err := db.GetStandardsRunBinding(ctx, initial.RootRunID)
	if err != nil || retained.Outcome != "unavailable" || retained.RevisionID != 0 || len(retained.SnapshotBytes) != 0 {
		t.Fatalf("missing binding outcome was not durable: %+v %v", retained, err)
	}
	unpause()
	var result bindResult
	select {
	case result = <-finished:
	case <-ctx.Done():
		t.Fatalf("waiting for initial capture to reuse winner: %v", ctx.Err())
	}
	if result.err != nil || result.status.Integrity != "unavailable" || result.status.Binding.RevisionID != 0 || result.status.Reason != unavailable.Reason {
		t.Fatalf("slow capture replaced winner: %+v %v", result.status, result.err)
	}
	resumed, err := initial.Bind(ctx, true)
	if err != nil || resumed.Integrity != "unavailable" || resumed.Binding.RevisionID != 0 {
		t.Fatalf("retry replaced unavailable binding: %+v %v", resumed, err)
	}
}

func TestRuntimeForRoleOffersOnlyStandardsThatApply(t *testing.T) {
	svc, _, db := publicationServices(t)
	standard := func(id, category string, roles ...string) standards.Operation {
		return standards.Operation{Kind: "standard.add", Standard: &standards.Standard{ID: id, CategoryID: category, Rule: "MUST rule " + id, Description: strings.Repeat("d", 9000), Evidence: "Evidence.", Scope: "All repositories.", Check: "automated", AppliesTo: roles}}
	}
	p, e := svc.Create(t.Context(), "roles", 0, []standards.Operation{
		{Kind: "category.add", Category: &standards.Category{ID: "sec", Name: "Security"}},
		{Kind: "category.add", Category: &standards.Category{ID: "tst", Name: "Testing"}},
		standard("SEC-001", "sec", "story-worker", "plan-technical"),
		standard("SEC-002", "sec", "validate-architecture"),
		standard("TST-001", "tst", "story-worker"),
	})
	if e != nil {
		t.Fatal(e)
	}
	approvePublication(t, svc, p)
	if _, e = svc.Publish(t.Context(), p.ProposalID, p.Version, p.ChangeDigest); e != nil {
		t.Fatal(e)
	}
	r := &standards.Runtime{Store: db, RootRunID: "7", ConsumerKey: "worker", AttemptID: "attempt-7"}
	if s, e := r.Bind(t.Context(), true); e != nil || s.Integrity != "verified" {
		t.Fatalf("bind: %+v %v", s, e)
	}
	text, cursor := "", ""
	var ids []string
	for {
		page, e := r.ForRole(t.Context(), "story-worker", nil, cursor)
		if e != nil {
			t.Fatal(e)
		}
		if !standards.FitsMCPResult(page) || page.Start != len(text) {
			t.Fatalf("page out of bounds or order: %d %d", page.Start, len(text))
		}
		ids = page.StandardIDs
		text += page.Text
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if strings.Join(ids, ",") != "SEC-001,TST-001" || !strings.Contains(text, "(SEC-001)") || !strings.Contains(text, "(TST-001)") || strings.Contains(text, "SEC-002") {
		t.Fatalf("role read offered the wrong standards: %v", ids)
	}
	if _, e = r.ForRole(t.Context(), "plan-technical", nil, cursor); e == nil {
		t.Fatal("a cursor of another role was accepted")
	}
	none, e := r.ForRole(t.Context(), "validate-code", nil, "")
	if e != nil || len(none.StandardIDs) != 0 || none.Text != "" {
		t.Fatalf("a role without standards got some: %+v %v", none, e)
	}
	page, e := r.Categories(t.Context(), 25, "")
	if e != nil {
		t.Fatal(e)
	}
	want := standards.Load{Roles: map[string]int{"story-worker": 2, "plan-technical": 1, "validate-architecture": 1}, Categories: map[string]int{"sec": 2, "tst": 1}}
	if !reflect.DeepEqual(page.Load, want) {
		t.Fatalf("load %+v, want %+v", page.Load, want)
	}
}

// A role sees only the categories that hold standards for it, with its own
// counts, and reads just the categories it picks; a category outside its
// standards is refused and a cursor stays bound to the picked categories.
func TestRuntimeRoleCategoriesNarrowTheRoleRead(t *testing.T) {
	svc, _, db := publicationServices(t)
	standard := func(id, category string, roles ...string) standards.Operation {
		return standards.Operation{Kind: "standard.add", Standard: &standards.Standard{ID: id, CategoryID: category, Rule: "MUST rule " + id, Description: strings.Repeat("d", 9000), Evidence: "Evidence.", Scope: "All repositories.", Check: "manual", AppliesTo: roles}}
	}
	p, e := svc.Create(t.Context(), "role-categories", 0, []standards.Operation{
		{Kind: "category.add", Category: &standards.Category{ID: "agents", Name: "Agents", Description: "Agent Markdown"}},
		{Kind: "category.add", Category: &standards.Category{ID: "sec", Name: "Security"}},
		{Kind: "category.add", Category: &standards.Category{ID: "tst", Name: "Testing"}},
		{Kind: "category.add", Category: &standards.Category{ID: "empty", Name: "Empty"}},
		standard("AGT-001", "agents", "story-worker"),
		standard("AGT-002", "agents", "story-worker", "plan-technical"),
		standard("SEC-001", "sec", "validate-architecture"),
		standard("TST-001", "tst", "story-worker"),
	})
	if e != nil {
		t.Fatal(e)
	}
	approvePublication(t, svc, p)
	if _, e = svc.Publish(t.Context(), p.ProposalID, p.Version, p.ChangeDigest); e != nil {
		t.Fatal(e)
	}
	r := &standards.Runtime{Store: db, RootRunID: "8", ConsumerKey: "worker", AttemptID: "attempt-8"}
	if s, e := r.Bind(t.Context(), true); e != nil || s.Integrity != "verified" {
		t.Fatalf("bind: %+v %v", s, e)
	}
	list, e := r.CategoriesForRole(t.Context(), "story-worker")
	if e != nil {
		t.Fatal(e)
	}
	want := []standards.RoleCategory{{ID: "agents", Name: "Agents", Description: "Agent Markdown", Count: 2}, {ID: "tst", Name: "Testing", Count: 1}}
	if !reflect.DeepEqual(list.Categories, want) || list.Note != "" {
		t.Fatalf("story-worker categories %+v, want %+v", list, want)
	}
	if plan, e := r.CategoriesForRole(t.Context(), "plan-technical"); e != nil || !reflect.DeepEqual(plan.Categories, []standards.RoleCategory{{ID: "agents", Name: "Agents", Description: "Agent Markdown", Count: 1}}) {
		t.Fatalf("plan-technical categories %+v %v", plan, e)
	}
	if none, e := r.CategoriesForRole(t.Context(), "validate-code"); e != nil || len(none.Categories) != 0 || none.Note == "" {
		t.Fatalf("role without standards: %+v %v", none, e)
	}
	readAll := func(categories []string) ([]string, string, string) {
		t.Helper()
		text, cursor, first := "", "", ""
		var ids []string
		for {
			page, e := r.ForRole(t.Context(), "story-worker", categories, cursor)
			if e != nil {
				t.Fatal(e)
			}
			ids = page.StandardIDs
			text += page.Text
			if page.NextCursor == "" {
				return ids, text, first
			}
			if first == "" {
				first = page.NextCursor
			}
			cursor = page.NextCursor
		}
	}
	ids, text, cursor := readAll([]string{"agents", "agents"})
	if !slices.Equal(ids, []string{"AGT-001", "AGT-002"}) || strings.Contains(text, "TST-001") || cursor == "" {
		t.Fatalf("agents read offered %v", ids)
	}
	if ids, _, _ := readAll([]string{"tst"}); !slices.Equal(ids, []string{"TST-001"}) {
		t.Fatalf("testing read offered %v", ids)
	}
	if ids, _, _ := readAll(nil); !slices.Equal(ids, []string{"AGT-001", "AGT-002", "TST-001"}) {
		t.Fatalf("unfiltered read offered %v", ids)
	}
	for _, foreign := range [][]string{{"sec"}, {"empty"}, {"nope"}, {"agents", "sec"}} {
		_, e := r.ForRole(t.Context(), "story-worker", foreign, "")
		if !errors.Is(e, standards.ErrUnknownCategory) || !strings.Contains(e.Error(), "agents, tst") {
			t.Fatalf("category %v outside the role was not refused with the valid ones: %v", foreign, e)
		}
	}
	for _, other := range [][]string{nil, {"tst"}, {"agents", "tst"}} {
		if _, e := r.ForRole(t.Context(), "story-worker", other, cursor); e == nil {
			t.Fatalf("a cursor for [agents] was accepted with categories %v", other)
		}
	}
	if _, e := r.ForRole(t.Context(), "story-worker", []string{"agents"}, cursor); e != nil {
		t.Fatalf("cursor rejected with its own categories: %v", e)
	}
}
