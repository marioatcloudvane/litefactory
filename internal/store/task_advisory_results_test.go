package store

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func taskAdvisoryResultFixture(t *testing.T) (*DB, AdvisoryResultBinding) {
	t.Helper()
	db, _, a := discussionFixture(t)
	owner := connectorTestOwner(t, db, 42)
	a.OwnerGeneration = owner.Generation
	if _, err := db.sql.ExecContext(t.Context(), `UPDATE task_discussions SET owner_generation=?`, owner.Generation); err != nil {
		t.Fatal(err)
	}
	connectorTestClient(t, db, "client")
	now := time.Now().UTC()
	a.CreatedAt = now
	repo, err := db.UpsertRepo(t.Context(), Repo{Forge: "github", Owner: "acme", Name: "api", Path: t.TempDir()})
	if err != nil {
		t.Fatalf("repo: %v", err)
	}
	if _, err = db.sql.ExecContext(t.Context(), `INSERT INTO connector_grants(id,client_id,resource,scope,owner_id,generation,created_at,expires_at) VALUES('grant','client',?,'task:advisory',42,?,?,?)`, connectorTestResource, owner.Generation, FormatTime(now), FormatTime(now.Add(time.Hour))); err != nil {
		t.Fatalf("grant: %v", err)
	}
	if _, err = db.sql.ExecContext(t.Context(), `INSERT INTO connector_grant_repositories(grant_id,repo_id,repository_ref) VALUES('grant',?,'github:acme/api')`, repo.ID); err != nil {
		t.Fatalf("coverage: %v", err)
	}
	if _, err = db.AdmitAdvisory(t.Context(), a); err != nil {
		t.Fatalf("admission: %v", err)
	}
	if _, _, err = db.ClaimWorkflowAttempt(t.Context(), now, time.Minute); err != nil {
		t.Fatalf("claim: %v", err)
	}
	b, err := db.BindAdvisoryResultAttempt(t.Context(), a.ExecutionID)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	return db, b
}
func TestTaskAdvisoryResultReplayAndLateWorkerFence(t *testing.T) {
	db, b := taskAdvisoryResultFixture(t)
	raw := []byte(`{"answer":"answer","sources":["task.json"],"gaps":[]}`)
	if replay, err := db.SubmitAdvisoryResult(t.Context(), b, "receipt", raw); err != nil || replay {
		t.Fatalf("first submit: %v %v", replay, err)
	}
	if replay, err := db.SubmitAdvisoryResult(t.Context(), b, "receipt", raw); err != nil || !replay {
		t.Fatalf("replay: %v %v", replay, err)
	}
	if _, err := db.SubmitAdvisoryResult(t.Context(), b, "receipt", []byte(`{}`)); !errors.Is(err, ErrResultCorrelation) {
		t.Fatalf("changed receipt: %v", err)
	}
	if _, err := db.sql.ExecContext(t.Context(), `UPDATE durable_workflow_worker SET generation=generation+1`); err != nil {
		t.Fatal(err)
	}
	if err := db.CheckAdvisoryResultWriter(t.Context(), b); !errors.Is(err, ErrResultAuthority) {
		t.Fatalf("late writer: %v", err)
	}
	if id, saved, err := db.ReadAdvisoryResult(t.Context(), b, "receipt"); err != nil || id != "receipt" || string(saved) != string(raw) {
		t.Fatalf("receipt recovery: %s %s %v", id, saved, err)
	}
}
func TestTaskAdvisoryResultRevokedOriginalConsent(t *testing.T) {
	db, b := taskAdvisoryResultFixture(t)
	if err := db.RevokeConnectorGrant(t.Context(), 42, "grant", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SubmitAdvisoryResult(t.Context(), b, "receipt", []byte(`{}`)); err == nil {
		t.Fatal("revoked consent admitted result")
	}
}

func TestTaskAdvisoryInvocationCannotLaunchTwice(t *testing.T) {
	db, b := taskAdvisoryResultFixture(t)
	if err := db.StartAdvisoryInvocation(t.Context(), b, "codex", "test-model", "high", time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := db.StartAdvisoryInvocation(t.Context(), b, "codex", "test-model", "high", time.Minute); err == nil {
		t.Fatal("second paid invocation admitted")
	}
	if err := db.RecordAdvisoryUsage(t.Context(), b, []byte(`{"kind":"observation"}`)); err != nil {
		t.Fatal(err)
	}
	if err := db.FinishAdvisoryInvocation(t.Context(), b, "observed-model", "provider-version", "failed", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordAdvisoryUsage(t.Context(), b, []byte(`{}`)); !errors.Is(err, ErrResultAuthority) {
		t.Fatalf("late usage modified terminal execution: %v", err)
	}
}

func TestTaskAdvisoryStopBeforeLaunchPreventsPaidInvocation(t *testing.T) {
	db, b := taskAdvisoryResultFixture(t)
	a, err := db.ReadAdvisoryByExecution(t.Context(), b.ExecutionID)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = db.CancelAdvisory(t.Context(), a.DiscussionScope, a.JobID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err = db.StartAdvisoryInvocation(t.Context(), b, "codex", "model", "high", time.Minute); !errors.Is(err, ErrResultAuthority) {
		t.Fatalf("stopped work launched: %v", err)
	}
	if err = db.CheckAdvisoryResultWriter(t.Context(), b); err != nil {
		t.Fatalf("stop prevented completion receipt: %v", err)
	}
}

func TestTaskAdvisoryInvocationPinsCatalogAndBoundsUsageBytes(t *testing.T) {
	db, b := taskAdvisoryResultFixture(t)
	catalog, err := db.ListMeterPrices(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err = db.StartAdvisoryInvocation(t.Context(), b, "codex", "model", "high", time.Minute); err != nil {
		t.Fatal(err)
	}
	later, err := db.SetMeterPrices(t.Context(), "codex", "model", rateEntry("model", "1.25"))
	if err != nil {
		t.Fatal(err)
	}
	var pinned int64
	if err = db.sql.QueryRowContext(t.Context(), `SELECT price_revision_id FROM task_advisory_invocations WHERE execution_id=?`, b.ExecutionID).Scan(&pinned); err != nil || pinned != catalog.RevisionID || pinned == later {
		t.Fatalf("catalog pin changed: pinned %d original %d latest %d: %v", pinned, catalog.RevisionID, later, err)
	}
	// UTF-8 text proves the cap measures bytes, rather than SQLite text characters.
	observation := []byte(strings.Repeat("é", 64<<10))
	for range 80 {
		if err = db.RecordAdvisoryUsage(t.Context(), b, observation); err != nil {
			t.Fatal(err)
		}
	}
	if err = db.RecordAdvisoryUsage(t.Context(), b, []byte(`{}`)); !errors.Is(err, ErrConnectorCapacity) {
		t.Fatalf("usage exceeded ten MiB: %v", err)
	}
	var count, bytes int64
	if err = db.sql.QueryRowContext(t.Context(), `SELECT COUNT(*),SUM(length(CAST(event AS BLOB))) FROM task_advisory_usage_events WHERE execution_id=?`, b.ExecutionID).Scan(&count, &bytes); err != nil || count != 80 || bytes != 10<<20 {
		t.Fatalf("usage retention changed on refusal: rows %d bytes %d error %v", count, bytes, err)
	}
}
