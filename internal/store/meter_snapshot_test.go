package store

import (
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/archive"
)

// meterPricedTotal sums PricedSubtotalUSD over every meter row, the project's
// observed lower bound.
func meterPricedTotal(t *testing.T, db *DB) *big.Rat {
	t.Helper()
	ctx := t.Context()
	snap, err := db.NewMeterSnapshot(ctx, MeterFilter{}, MeterSnapshotOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := snap.Close(ctx); err != nil {
			t.Error(err)
		}
	}()
	total := new(big.Rat)
	for {
		rows, err := snap.Next(ctx, 256)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) == 0 {
			return total
		}
		for _, r := range rows {
			if r.PricedSubtotalUSD == nil {
				continue
			}
			v, ok := new(big.Rat).SetString(*r.PricedSubtotalUSD)
			if !ok {
				t.Fatalf("subtotal %q", *r.PricedSubtotalUSD)
			}
			total.Add(total, v)
		}
	}
}

// TestMeterLowerBoundSurvivesReclassificationAndArchive seeds a closed family
// with a priced, graph-linked meter attempt, reclassifies its collection status
// and archives the family; the project's lower bound never drops.
func TestMeterLowerBoundSurvivesReclassificationAndArchive(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	s := testSession(t, db, 727)
	r := testRun(t, db, s.ID, 727)
	model := "priced-model"
	var rates []MeterPriceRate
	for _, dimension := range []string{"input_uncached", "output", "cache_read", "cache_write"} {
		rates = append(rates, MeterPriceRate{CodingAgent: "claude", Model: model, Dimension: dimension, USDPerMillion: "1"})
	}
	if _, err := db.SetMeterPrices(ctx, "claude", model, rates); err != nil {
		t.Fatal(err)
	}
	a, err := db.StartMeterAttempt(ctx, r.ID, "claude")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.RecordMeterEvent(ctx, a.ID, "claude", agent.UsageEvent{Invocation: &agent.UsageInvocation{AttemptID: a.ID, ID: "root", SourceID: "s", SourceInvocationID: "root", Coverage: agent.UsageCoverage{Status: agent.CollectionUnknown}}}); err != nil {
		t.Fatal(err)
	}
	n, zero := int64(1000), int64(0)
	o := agent.UsageObservation{AttemptID: a.ID, SourceID: "s", EventKey: "one", InvocationID: "root", ActualModel: &model, ModelProvenance: "provider", Mode: agent.CounterIncremental, Scope: agent.UsageExclusive, Tokens: agent.TokenUsage{InputUncached: &n, Output: &zero, CacheRead: &zero, CacheWrite: &zero}, Coverage: agent.UsageCoverage{Status: agent.CollectionPartial}}
	if err := db.RecordMeterEvent(ctx, a.ID, "claude", agent.UsageEvent{Observation: &o}); err != nil {
		t.Fatal(err)
	}

	// Link the meter attempt to a finished graph node attempt of the family.
	definition := graphTestDefinition(t, db)
	instance, err := db.CreateGraphInstance(ctx, GraphInstance{SessionID: s.ID, DefinitionID: definition.ID, CurrentNode: "work", StartedAt: graphTestTime(), NextDueAt: graphTestTime()})
	if err != nil {
		t.Fatal(err)
	}
	var nodeAttempt int64
	if err := db.sql.QueryRowContext(ctx, `INSERT INTO graph_node_attempts(instance_id,node,attempt,run_id,status,started_at,finished_at) VALUES(?,'work',1,?,'succeeded',?,?) RETURNING id`, instance.ID, r.ID, FormatTime(graphTestTime()), FormatTime(graphTestTime())).Scan(&nodeAttempt); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.ExecContext(ctx, `UPDATE meter_attempts SET graph_node_attempt_id=? WHERE id=?`, nodeAttempt, a.ID); err != nil {
		t.Fatal(err)
	}

	before := meterPricedTotal(t, db)
	if before.Sign() <= 0 {
		t.Fatalf("seeded lower bound %s, want positive", before.FloatString(12))
	}

	// c-monotonic-after-reclassification.
	if err := db.FinishMeterAttempt(ctx, a.ID, "success", "complete", "v", "stream-json", nil); err != nil {
		t.Fatal(err)
	}
	if got := meterPricedTotal(t, db); got.Cmp(before) != 0 {
		t.Fatalf("lower bound after reclassification %s, want %s", got.FloatString(12), before.FloatString(12))
	}

	// c-monotonic-after-archive and c-archive-graph-linked-attempt.
	if _, err := db.FinishRun(ctx, r.ID, RunOutcome{Status: RunSucceeded}); err != nil {
		t.Fatal(err)
	}
	f := ArchiveFamily{SessionID: s.ID, RepoID: s.RepoID, TaskRef: s.TaskRef, IssueClosed: true}
	d := archive.NewDigester()
	if err := db.StreamArchiveRows(ctx, f, func(row archive.Row) error {
		if strings.HasPrefix(row.Table, "meter_") {
			t.Errorf("archive file would hold %s row", row.Table)
		}
		_, err := d.Add(row)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	deleted, err := db.DeleteArchivedFamily(ctx, f, d.Trailer(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if deleted["graph_node_attempts"] != 1 {
		t.Fatalf("deleted %v, want the graph node attempt", deleted)
	}
	for table := range deleted {
		if strings.HasPrefix(table, "meter_") {
			t.Fatalf("archive deleted %s", table)
		}
	}
	var link *int64
	if err := db.sql.QueryRowContext(ctx, `SELECT graph_node_attempt_id FROM meter_attempts WHERE id=?`, a.ID).Scan(&link); err != nil {
		t.Fatal(err)
	}
	if link != nil {
		t.Fatalf("graph link %d kept after archive", *link)
	}
	if got := meterPricedTotal(t, db); got.Cmp(before) != 0 {
		t.Fatalf("lower bound after archive %s, want %s", got.FloatString(12), before.FloatString(12))
	}
}

func TestMeterSnapshotManagedFamilySelection(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	repo := testRepo(t, db, "acme", "api")
	other := testRepo(t, db, "foreign", "api")
	for _, project := range []Repo{repo, other} {
		for _, issue := range []int{614, 809, 810, 811} {
			session, err := db.GetOrCreateSession(ctx, project, issue)
			if err != nil {
				t.Fatal(err)
			}
			run := testRun(t, db, session.ID, int64(issue))
			a, err := db.StartMeterAttempt(ctx, run.ID, "claude")
			if err != nil {
				t.Fatal(err)
			}
			if err := db.FinishMeterAttempt(ctx, a.ID, "success", "unknown", "fixture", "fixture", nil); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, query := range []string{
		`INSERT INTO plan_story_drafts(parent,story_key,ordinal,issue_id,issue_number,retired) VALUES('github:acme/api#614','one',1,809,809,1)`,
		`INSERT INTO managed_features(parent_task,generation,state) VALUES('github:acme/api#614',2,'completed')`,
		`INSERT INTO managed_stories(parent_task,generation,story_key,issue_number,issue_id,body_hash,dependencies,state) VALUES('github:acme/api#614',1,'one',809,809,'x','[]','completed'),('github:acme/api#614',2,'one',809,809,'x','[]','completed'),('github:acme/api#614',2,'two',810,810,'x','[]','completed')`,
		`INSERT INTO plan_story_drafts(parent,story_key,ordinal,issue_id,issue_number) VALUES('github:foreign/api#614','other',1,811,811)`,
	} {
		if _, err := db.sql.ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name string
		f    MeterFilter
		want string
	}{
		{"family", MeterFilter{RepoID: repo.ID, Issue: 614, IncludeStories: true}, "614,809,810"},
		{"exact export and recovery", MeterFilter{RepoID: repo.ID, Issue: 614}, "614"},
		{"child", MeterFilter{RepoID: repo.ID, Issue: 809, IncludeStories: true}, "809"},
		{"foreign", MeterFilter{RepoID: other.ID, Issue: 614, IncludeStories: true}, "614,811"},
		{"agent filter", MeterFilter{RepoID: repo.ID, Issue: 614, IncludeStories: true, CodingAgent: "codex"}, ""},
		{"mode filter", MeterFilter{RepoID: repo.ID, Issue: 614, IncludeStories: true, Mode: "implement"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snap, err := db.NewMeterSnapshot(ctx, tc.f, MeterSnapshotOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := snap.Close(ctx); err != nil {
					t.Error(err)
				}
			}()
			rows, err := snap.Next(ctx, 256)
			if err != nil {
				t.Fatal(err)
			}
			var issues []string
			for _, r := range rows {
				issues = append(issues, fmt.Sprint(r.Issue))
			}
			if got := strings.Join(issues, ","); got != tc.want {
				t.Fatalf("issues=%s want=%s", got, tc.want)
			}
		})
	}
}
