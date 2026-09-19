package daemon

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

func TestGraphPollDueReadOnlySelection(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: store.ForgeGitHub, Owner: "acme", Name: "graphs", RemoteURL: "git@github.com:acme/graphs.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.GetOrCreateSession(ctx, repo, 315)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC)
	definition, err := db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: "core", Mode: "implement", ProfileVersion: 1, Path: "core/implement.dot", Digest: strings.Repeat("a", 64), CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	var instances []store.GraphInstance
	for _, tt := range []struct {
		due    time.Time
		status string
	}{
		{now, store.GraphRunning},
		{now.Add(-time.Second), store.GraphRunning},
		{now, store.GraphRunning},
		{now.Add(time.Millisecond), store.GraphRunning},
		{time.Time{}, store.GraphRunning},
		{now, store.GraphCancelled},
		{now, store.GraphSucceeded},
		{now, store.GraphFailed},
	} {
		instance, err := db.CreateGraphInstance(ctx, store.GraphInstance{SessionID: session.ID, DefinitionID: definition.ID, CurrentNode: "work", StartedAt: now, NextDueAt: tt.due})
		if err != nil {
			t.Fatal(err)
		}
		if tt.status != store.GraphRunning {
			if err := db.SetGraphInstanceStatus(ctx, instance.ID, tt.status, time.Time{}, now); err != nil {
				t.Fatal(err)
			}
			instance, err = db.GetGraphInstance(ctx, instance.ID)
			if err != nil {
				t.Fatal(err)
			}
		}
		instances = append(instances, instance)
	}
	for _, limit := range []int{1, 2, 20} {
		selected, err := pollDueGraphInstances(ctx, db, now, limit)
		if err != nil {
			t.Fatal(err)
		}
		expected := []store.GraphInstance{instances[1], instances[0], instances[2]}
		if limit < len(expected) {
			expected = expected[:limit]
		}
		if !reflect.DeepEqual(selected, expected) {
			t.Fatalf("limit %d: got %+v want %+v", limit, selected, expected)
		}
		direct, err := db.ListDueGraphInstances(ctx, now, limit)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(selected, direct) {
			t.Fatal("poller differs from store rows")
		}
	}
	for _, before := range instances {
		after, err := db.GetGraphInstance(ctx, before.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, after) {
			t.Fatalf("poll mutated instance: before=%+v after=%+v", before, after)
		}
		attempts, err := db.ListGraphNodeAttempts(ctx, before.ID)
		if err != nil || len(attempts) != 0 {
			t.Fatalf("poll dispatched work: attempts=%+v error=%v", attempts, err)
		}
		transitions, err := db.ListGraphTransitions(ctx, before.ID)
		if err != nil || len(transitions) != 0 {
			t.Fatalf("poll advanced graph: transitions=%+v error=%v", transitions, err)
		}
	}
	if _, err := pollDueGraphInstances(ctx, db, now, 0); !errors.Is(err, store.ErrGraphInvariant) {
		t.Fatalf("limit error lost: %v", err)
	}
	if _, err := pollDueGraphInstances(ctx, nil, now, 1); err == nil {
		t.Fatal("nil store accepted")
	}
}
