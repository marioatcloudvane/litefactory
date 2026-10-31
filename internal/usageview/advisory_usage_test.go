package usageview

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/connector"
	"github.com/cloudvane/litefactory/internal/meterview"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestPaidAdvisoryUsageAppearsInExistingViews(t *testing.T) {
	db := usageTestDB(t)
	ctx := t.Context()
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: filepath.Join(t.TempDir(), "repo"), Forge: "github", Owner: "owner", Name: "advisory-usage", BaseBranch: "main", FeaturePrefix: "lf"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.GetOrCreateSession(ctx, repo, 73)
	if err != nil {
		t.Fatal(err)
	}
	rates := []store.MeterPriceRate{}
	for _, dimension := range []string{"input_uncached", "output", "cache_read", "cache_write"} {
		rates = append(rates, store.MeterPriceRate{CodingAgent: "codex", Model: "advisory-meter-model", Dimension: dimension, USDPerMillion: "1"})
	}
	if _, err = db.SetMeterPrices(ctx, "codex", "advisory-meter-model", rates); err != nil {
		t.Fatal(err)
	}
	segment, err := db.GetOrCreateSegment(ctx, session.ID, store.FirstSegmentOrdinal, "usage", store.AgentCodex)
	if err != nil {
		t.Fatal(err)
	}
	run, err := db.AllocateRun(ctx, store.AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: "plan", Agent: store.AgentCodex, TriggerCommentID: 1, LogPath: func(int) string { return "/private/fixture/usage" }})
	if err != nil {
		t.Fatal(err)
	}
	workflow, err := db.StartMeterAttempt(ctx, run.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	recordAdvisoryUsageFixture(t, db, workflow, 1)
	admitUsageAdvisoryFixture(t, db, repo, session, "ask", 1, 2)
	admitUsageAdvisoryFixture(t, db, repo, session, "judge", 2, 3)
	service := New(db, redact.New(redact.Options{Environ: []string{}}))
	views := []struct {
		name       string
		filter     store.MeterFilter
		repository string
	}{
		{"factory", store.MeterFilter{}, ""},
		{"repository", store.MeterFilter{RepoID: repo.ID}, "github:owner/advisory-usage"},
		{"task", store.MeterFilter{RepoID: repo.ID, Issue: 73, IncludeStories: true}, "github:owner/advisory-usage"},
	}
	for _, scope := range views {
		t.Run(scope.name, func(t *testing.T) {
			projection, err := service.Usage(t.Context(), connector.UsageRequest{Repository: scope.repository, Filter: scope.filter, View: "details"})
			if err != nil {
				t.Fatal(err)
			}
			totals := usageAssert[map[string]any](t, projection.Facts["totals"])
			if totals["estimated_usd"] != "0.000006000000" || totals["collection_status"] != "complete" {
				t.Fatalf("paid advisory missing from complete total: %v", totals)
			}
			tokens := usageAssert[map[string]any](t, totals["tokens"])
			if tokens["output"] != json.Number("6") {
				t.Fatalf("advisory token usage omitted: %v", tokens)
			}
			paths := map[string]bool{}
			for _, item := range projection.Items {
				path := usageAssert[string](t, item["execution_path"])
				paths[path] = true
				if path == "advisory_ask" || path == "advisory_judge" {
					if item["run"] != json.Number("0") || item["issue"] != json.Number("73") || item["mode"] != "advisory" {
						t.Fatalf("advisory invented a workflow run or lost task ownership: %v", item)
					}
				}
			}
			if !paths["advisory_ask"] || !paths["advisory_judge"] {
				t.Fatalf("advisory attribution missing: %v", paths)
			}
		})
	}
	// The transport-neutral views and their mode filter consume the same canonical rows.
	view, err := meterview.Read(ctx, db, meterview.Request{Project: "github:owner/advisory-usage", Task: 73, Mode: "advisory", Details: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Details) != 2 || view.Summary.Tokens.Output == nil || *view.Summary.Tokens.Output != 5 || view.Summary.EstimatedUSD == nil || *view.Summary.EstimatedUSD != "0.000005000000" {
		t.Fatalf("advisory mode report lost paid work: %+v", view)
	}
	// An advisory launch without usage must prevent an all-in complete-cost claim.
	admitUsageAdvisoryFixture(t, db, repo, session, "ask", 3, -1)
	for _, scope := range views {
		projection, err := service.Usage(ctx, connector.UsageRequest{Repository: scope.repository, Filter: scope.filter, View: "summary"})
		if err != nil {
			t.Fatal(err)
		}
		totals := usageAssert[map[string]any](t, projection.Facts["totals"])
		if totals["estimated_usd"] != nil || totals["priced_subtotal_usd"] != "0.000006000000" || totals["collection_status"] == "complete" {
			t.Fatalf("%s claimed a complete total while advisory usage was missing: %v", scope.name, totals)
		}
	}
}

func recordAdvisoryUsageFixture(t *testing.T, db *store.DB, attempt store.MeterAttempt, output int64) {
	t.Helper()
	ctx := t.Context()
	if err := db.PutMeterInvocation(ctx, store.MeterInvocation{AttemptID: attempt.ID, ID: "root", SourceID: "fixture", SourceInvocationID: "root", Status: "complete"}); err != nil {
		t.Fatal(err)
	}
	zero := int64(0)
	model := "advisory-meter-model"
	observation := agent.UsageObservation{AttemptID: attempt.ID, SourceID: "fixture", EventKey: "one", InvocationID: "root", ActualModel: &model, ModelProvenance: "source", Mode: agent.CounterIncremental, Scope: agent.UsageExclusive, Tokens: agent.TokenUsage{InputUncached: &zero, Output: &output, CacheRead: &zero, CacheWrite: &zero}, Coverage: agent.UsageCoverage{Status: agent.CollectionComplete, SourceFormat: "fixture"}}
	if err := db.RecordMeterEvent(ctx, attempt.ID, "codex", agent.UsageEvent{Observation: &observation}); err != nil {
		t.Fatal(err)
	}
	if err := db.FinishMeterAttempt(ctx, attempt.ID, "success", "complete", "fixture", "fixture", nil); err != nil {
		t.Fatal(err)
	}
}

func admitUsageAdvisoryFixture(t *testing.T, db *store.DB, repo store.Repo, session store.Session, operation string, ordinal int, output int64) {
	t.Helper()
	ctx := t.Context()
	now := time.Now().UTC()
	scope := store.DiscussionScope{OwnerID: 42, OwnerGeneration: 1, RepositoryRef: repo.Forge + ":" + repo.Slug(), TaskRef: session.TaskRef}
	key := fmt.Sprintf("%s-%d", operation, ordinal)
	conversation, err := db.OpenDiscussion(ctx, store.DiscussionConversation{DiscussionScope: scope, ID: "conversation-" + key, ClientID: "meter-client", RequestKey: key, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	basis, _ := json.Marshal(map[string]any{"task_ref": session.TaskRef, "repository_ref": scope.RepositoryRef, "stage": "technical", "revision": "saved-review"})
	admission, err := db.AdmitAdvisory(ctx, store.AdvisoryAdmission{DiscussionScope: scope, ID: "admission-" + key, ConversationID: conversation.ID, Operation: operation, RequestKey: key, Digest: "input-" + key, Basis: string(basis), Context: `{}`, Question: "Explain the saved choice.", GrantID: "meter-grant", ConsentRevision: `{}`, ConversationRevision: conversation.Revision, JobID: "job-" + key, ExecutionID: "execution-" + key, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	claim, _, err := db.ClaimWorkflowAttempt(ctx, now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if claim.ExecutionID != admission.ExecutionID {
		t.Fatal("meter fixture claimed different work")
	}
	attempt, err := db.StartAdvisoryMeterAttempt(ctx, admission.ExecutionID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if output >= 0 {
		recordAdvisoryUsageFixture(t, db, attempt, output)
	} else if err = db.FinishMeterAttempt(ctx, attempt.ID, "success", "unknown", "fixture", "fixture", []string{"usage_unavailable"}); err != nil {
		t.Fatal(err)
	}
	if err = db.FinishAdvisory(ctx, claim, store.WorkflowFinish{State: "succeeded", Result: `{"answer":"Saved private answer","sources":["saved-review"],"gaps":[]}`, Questions: `[]`, Summary: "succeeded"}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
}
