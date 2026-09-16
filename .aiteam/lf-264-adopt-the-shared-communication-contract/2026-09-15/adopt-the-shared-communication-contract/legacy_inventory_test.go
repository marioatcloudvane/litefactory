package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
)

func inventoryFixture(t *testing.T) (*store.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "inventory.db")
	db, err := store.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	return db, path
}

func assertRetainedReaders(t *testing.T, r report) {
	t.Helper()
	if len(r.Gates) != 3 {
		t.Fatalf("want three reader dispositions, got %+v", r.Gates)
	}
	for _, g := range r.Gates {
		if !strings.Contains(g.State, "retain") {
			t.Errorf("inventory granted reader removal: %+v", g)
		}
	}
	if len(r.Coverage) == 0 {
		t.Error("missing observation bounds")
	}
	if r.Started.IsZero() || r.Finished.Before(r.Started) {
		t.Errorf("invalid observation interval: %v - %v", r.Started, r.Finished)
	}
}

func TestInventoryMissingDatabase(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "missing.db")
	r := collect(t.Context(), path)
	if len(r.Errors) == 0 {
		t.Fatal("missing database reported as successful inventory")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("inventory created database: %v", err)
	}
	assertRetainedReaders(t, r)
}

func TestInventoryEmptyDatabase(t *testing.T) {
	t.Parallel()
	_, path := inventoryFixture(t)
	r := collect(t.Context(), path)
	if len(r.Errors) != 0 {
		t.Fatalf("inventory errors: %v", r.Errors)
	}
	if len(r.Cohorts) != 0 || len(r.Records) != 0 {
		t.Fatalf("empty fixture invented records: %+v", r)
	}
	assertRetainedReaders(t, r)
}

func TestInventoryLegacyAndPendingDelivery(t *testing.T) {
	t.Parallel()
	db, path := inventoryFixture(t)
	repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: t.TempDir(), Forge: store.ForgeGitHub, Owner: "fixture", Name: "inventory", RemoteURL: "https://github.com/fixture/inventory", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.GetOrCreateSession(t.Context(), repo, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.PutManagedFeature(t.Context(), store.ManagedFeature{ParentTask: session.TaskRef, Generation: 1, State: "planning", WorkflowVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if err := db.BindFeatureContract(t.Context(), store.FeatureContract{Parent: session.TaskRef, Generation: 1, ContentVersion: 1, SubmissionVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if err := db.PutManagedStory(t.Context(), store.ManagedStory{ParentTask: session.TaskRef, Generation: 1, Key: "US-001", IssueNumber: 2, IssueID: 20, BodyHash: "fixture-hash", State: "planning"}); err != nil {
		t.Fatal(err)
	}
	intent := store.ReportDelivery{Key: "pending-fixture", SessionID: session.ID, TriggerID: 1, Task: session.TaskRef, Destination: session.TaskRef, TriggerNamespace: "comment", Purpose: "reply", BotUserID: 99, Body: "private fixture body must not appear", Marker: "private fixture marker must not appear"}
	if _, err := db.EnsureReportDelivery(t.Context(), intent); err != nil {
		t.Fatal(err)
	}
	if _, err := db.MarkReportDeliverySent(t.Context(), intent.Key); err != nil {
		t.Fatal(err)
	}
	r := collect(t.Context(), path)
	if len(r.Errors) != 0 {
		t.Fatalf("inventory errors: %v", r.Errors)
	}
	if len(r.Cohorts) != 1 {
		t.Fatalf("want legacy owner: %+v", r.Cohorts)
	}
	c := r.Cohorts[0]
	if c.Task != session.TaskRef || !c.Managed || c.Generation != 1 || c.Workflow != 1 || c.Content != 1 || c.Submission != 1 || len(c.Unfinished) != 1 || c.Unfinished[0] != "US-001" {
		t.Fatalf("legacy cohort lost identity/version/story: %+v", c)
	}
	found := false
	for _, p := range r.Records {
		if p.Kind == "report_delivery" && p.Reference == intent.Key && p.Task == session.TaskRef && strings.Contains(p.State, "possibly_sent=true") {
			found = true
		}
	}
	if !found {
		t.Fatalf("uncertain delivery omitted: %+v", r.Records)
	}
	encoded, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), intent.Body) || strings.Contains(string(encoded), intent.Marker) {
		t.Fatal("inventory leaked retained report content")
	}
	retained, err := db.GetReportDelivery(t.Context(), intent.Key)
	if err != nil || retained.Completed || !retained.PossiblySent {
		t.Fatalf("inventory changed pending delivery: %+v %v", retained, err)
	}
	contract, err := db.GetFeatureContract(t.Context(), session.TaskRef)
	if err != nil || contract.ContentVersion != 1 || contract.SubmissionVersion != 1 {
		t.Fatalf("inventory changed legacy contract: %+v %v", contract, err)
	}
	assertRetainedReaders(t, r)
}

func TestInventoryUnmigratedDatabase(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "unmigrated.db")
	db, err := store.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	r := collect(t.Context(), path)
	if len(r.Errors) == 0 {
		t.Fatal("unmigrated database reported successful inventory")
	}
	assertRetainedReaders(t, r)
}
