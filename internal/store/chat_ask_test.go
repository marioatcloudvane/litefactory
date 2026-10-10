package store

import (
	"math/big"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
)

func TestChatMigrationPreservesExistingInboxAndLedger(t *testing.T) {
	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "upgrade.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	var prior []migration
	for _, m := range migrations {
		if strings.Contains(m.name, "_chat_ask.sql") || strings.Contains(m.name, "_chat_answer_meter.sql") || strings.Contains(m.name, "_chat_research.sql") {
			continue
		}
		prior = append(prior, m)
	}
	if err := db.migrateAll(t.Context(), prior); err != nil {
		t.Fatal(err)
	}
	session := testSession(t, db, 7)
	run := testRun(t, db, session.ID, 100)
	legacy, err := db.StartMeterAttempt(t.Context(), run.ID, "claude")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.BeginDoctorAttempt(t.Context(), session.TaskRef, DoctorAttempt{ID: "doctor-old", SourceID: "fixture", OwnerText: "read", PromptVersion: "v1", GuideVersion: "v1"}); err != nil {
		t.Fatal(err)
	}
	if err := db.PrepareDoctorAttempt(t.Context(), DoctorAttempt{ID: "doctor-old", Provider: "claude", ConfiguredModel: "fixture", Evidence: "{}"}); err != nil {
		t.Fatal(err)
	}
	diagnosis, err := db.StartDoctorMeterAttempt(t.Context(), "doctor-old", "claude")
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range []MeterAttempt{legacy, diagnosis} {
		if err := db.PutMeterInvocation(t.Context(), MeterInvocation{AttemptID: a.ID, ID: "root", SourceID: "s", SourceInvocationID: "root", Status: "complete"}); err != nil {
			t.Fatal(err)
		}
		if err := addMeterFixture(t.Context(), db, meterObservation(a, "one"), 7); err != nil {
			t.Fatal(err)
		}
	}
	at := time.Date(2026, 10, 10, 18, 0, 0, 0, time.UTC)
	if _, err := db.AdmitChatReply(t.Context(), ChatReply{Transport: "test", MessageID: "old", ChannelRef: "channel", Command: "help", CreatedAt: at}); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, a := range []MeterAttempt{legacy, diagnosis} {
		stored, err := db.GetMeterAttempt(t.Context(), a.ID)
		if err != nil || stored.RunID != a.RunID || stored.DoctorAttemptID != a.DoctorAttemptID {
			t.Fatalf("lost attempt owner: %+v %v", stored, err)
		}
		var quantity int64
		if err := db.sql.QueryRowContext(t.Context(), `SELECT output_tokens FROM meter_measurements WHERE attempt_id=?`, a.ID).Scan(&quantity); err != nil || quantity != 7 {
			t.Fatalf("lost measurement: %d %v", quantity, err)
		}
	}
	row, err := db.ReadChatReply(t.Context(), "test", "old")
	if err != nil || row.State != "pending" || row.Question != "" {
		t.Fatalf("lost old inbox: %+v %v", row, err)
	}
}

func TestChatMeterKnownAndUnknownCostAndReadOnlyTasks(t *testing.T) {
	db := newTestDB(t)
	repo := testRepo(t, db, "acme", "app")
	for i := 1; i <= 3; i++ {
		if _, err := db.GetOrCreateSession(t.Context(), repo, i); err != nil {
			t.Fatal(err)
		}
	}
	tasks, more, err := db.ListTaskSessionsByRepo(t.Context(), repo.ID, 2)
	if err != nil || len(tasks) != 2 || !more {
		t.Fatalf("task bound: %v %t %v", tasks, more, err)
	}
	model := "observed-model"
	var rates []MeterPriceRate
	for _, dimension := range []string{"input_uncached", "output", "cache_read", "cache_write"} {
		rates = append(rates, MeterPriceRate{CodingAgent: "claude", Model: model, Dimension: dimension, USDPerMillion: "1"})
	}
	if _, err := db.SetMeterPrices(t.Context(), "claude", model, rates); err != nil {
		t.Fatal(err)
	}
	known, err := db.StartChatMeterAttempt(t.Context(), repo.ID, "claude")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.PutMeterInvocation(t.Context(), MeterInvocation{AttemptID: known.ID, ID: "root", SourceID: "fixture", SourceInvocationID: "root", Status: "complete"}); err != nil {
		t.Fatal(err)
	}
	n, zero := int64(10), int64(0)
	observation := agent.UsageObservation{AttemptID: known.ID, SourceID: "fixture", EventKey: "one", InvocationID: "root", ActualModel: &model, ModelProvenance: "provider", Mode: agent.CounterIncremental, Scope: agent.UsageExclusive, Tokens: agent.TokenUsage{InputUncached: &n, Output: &zero, CacheRead: &zero, CacheWrite: &zero}, Coverage: agent.UsageCoverage{Status: agent.CollectionComplete}}
	if err := db.RecordMeterEvent(t.Context(), known.ID, "claude", agent.UsageEvent{Observation: &observation}); err != nil {
		t.Fatal(err)
	}
	if err := db.FinishMeterAttempt(t.Context(), known.ID, "success", "complete", "fixture", "fixture", nil); err != nil {
		t.Fatal(err)
	}
	unknown, err := db.StartChatMeterAttempt(t.Context(), repo.ID, "claude")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.FinishMeterAttempt(t.Context(), unknown.ID, "failure", "unknown", "", "", nil); err != nil {
		t.Fatal(err)
	}
	snapshot, err := db.NewMeterSnapshot(t.Context(), MeterFilter{RepoID: repo.ID}, MeterSnapshotOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := snapshot.Close(t.Context()); err != nil {
			t.Error(err)
		}
	}()
	rows, err := snapshot.Next(t.Context(), 100)
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows: %v %v", rows, err)
	}
	for _, row := range rows {
		if row.ExecutionPath != "chat_answer" || row.RunID != 0 || row.Issue != 0 {
			t.Fatalf("wrong chat scope: %+v", row)
		}
		if row.AttemptID == known.ID {
			if row.EstimatedUSD == nil {
				t.Fatal("known cost absent")
			}
			value, ok := new(big.Rat).SetString(*row.EstimatedUSD)
			if !ok || value.Cmp(big.NewRat(10, 1000000)) != 0 {
				t.Fatalf("known cost not valued: %s", *row.EstimatedUSD)
			}
		}
		if row.AttemptID == unknown.ID && row.EstimatedUSD != nil {
			t.Fatalf("unknown cost became zero: %+v", row)
		}
	}
}
