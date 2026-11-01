package store

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func discussionFixture(t *testing.T) (*DB, DiscussionConversation, AdvisoryAdmission) {
	t.Helper()
	db := newTestDB(t)
	at := time.Date(2026, 10, 31, 20, 0, 0, 0, time.UTC)
	scope := DiscussionScope{OwnerID: 42, OwnerGeneration: 1, RepositoryRef: "github:acme/api", TaskRef: "github:acme/api#1"}
	c, err := db.OpenDiscussion(t.Context(), DiscussionConversation{DiscussionScope: scope, ID: "conversation", ClientID: "client", RequestKey: "open", CreatedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	a := AdvisoryAdmission{DiscussionScope: scope, ID: "admission", ConversationID: c.ID, Operation: "ask", RequestKey: "ask", Digest: "digest", Basis: `{"stage":"research","revision":"one"}`, Context: `[]`, Question: "Why?", GrantID: "grant", ConsentRevision: "1", JobID: "advisory-job", ExecutionID: "advisory-execution", CreatedAt: at}
	return db, c, a
}
func TestDiscussionAdmissionIdentityScopeAndOrder(t *testing.T) {
	db, c, a := discussionFixture(t)
	got, err := db.AdmitAdvisory(t.Context(), a)
	if err != nil {
		t.Fatal(err)
	}
	retry := a
	retry.ID = "different"
	retry.JobID = "different"
	retry.Basis = `{"revision":"advanced"}`
	recovered, err := db.AdmitAdvisory(t.Context(), retry)
	if err != nil || recovered.ID != got.ID || recovered.Basis != a.Basis {
		t.Fatalf("retry: %+v %v", recovered, err)
	}
	retry.Digest = "changed"
	if _, err = db.AdmitAdvisory(t.Context(), retry); !errors.Is(err, ErrWorkflowConflict) {
		t.Fatalf("changed input: %v", err)
	}
	for _, mutate := range []func(*DiscussionScope){func(s *DiscussionScope) { s.OwnerID++ }, func(s *DiscussionScope) { s.OwnerGeneration++ }, func(s *DiscussionScope) { s.RepositoryRef = "github:other/repo" }, func(s *DiscussionScope) { s.TaskRef = "github:acme/api#2" }} {
		scope := c.DiscussionScope
		mutate(&scope)
		if _, err = db.ReadAdvisory(t.Context(), scope, a.JobID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("scope leaked result: %v", err)
		}
		if _, err = db.ReadDiscussion(t.Context(), scope, c.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("scope leaked history: %v", err)
		}
	}
	competing := a
	competing.ID = "next"
	competing.JobID = "next"
	competing.ExecutionID = "next"
	competing.RequestKey = "next"
	if _, err = db.AdmitAdvisory(t.Context(), competing); !errors.Is(err, ErrWorkflowRevision) {
		t.Fatalf("parallel conversation: %v", err)
	}
	after, err := db.ReadDiscussion(t.Context(), c.DiscussionScope, c.ID)
	if err != nil || after.Revision != 1 || after.ActiveJob != a.JobID {
		t.Fatalf("reservation: %+v %v", after, err)
	}
}
func TestDiscussionQueuedCancelAndFencedRunningCompletion(t *testing.T) {
	for _, queued := range []bool{true, false} {
		t.Run(strconv.FormatBool(queued), func(t *testing.T) {
			db, c, a := discussionFixture(t)
			if _, err := db.AdmitAdvisory(t.Context(), a); err != nil {
				t.Fatal(err)
			}
			var attempt WorkflowAttempt
			if !queued {
				var err error
				attempt, _, err = db.ClaimWorkflowAttempt(t.Context(), a.CreatedAt, time.Minute)
				if err != nil || attempt.AdmissionKind != "advisory" || attempt.ProposalID != "" {
					t.Fatalf("claim: %+v %v", attempt, err)
				}
			}
			stopped, won, err := db.CancelAdvisory(t.Context(), c.DiscussionScope, a.JobID, a.CreatedAt)
			if err != nil || won {
				t.Fatalf("cancel: %+v %v %v", stopped, won, err)
			}
			if queued {
				if stopped.Job.State != "cancelled" {
					t.Fatal(stopped.Job.State)
				}
				if _, _, err = db.ClaimWorkflowAttempt(t.Context(), a.CreatedAt, time.Minute); !errors.Is(err, ErrNotFound) {
					t.Fatalf("cancelled work launched: %v", err)
				}
			} else {
				if stopped.Job.State != "stop_requested" {
					t.Fatal(stopped.Job.State)
				}
				f := WorkflowFinish{State: "succeeded", Result: `{"answer":"Because of the evidence.","sources":["saved research"]}`, Questions: `[]`}
				stale := attempt
				stale.Fence++
				if err = db.FinishAdvisory(t.Context(), stale, f, a.CreatedAt); !errors.Is(err, ErrWorkflowFenced) {
					t.Fatalf("late fence: %v", err)
				}
				if err = db.FinishAdvisory(t.Context(), attempt, f, a.CreatedAt); err != nil {
					t.Fatal(err)
				}
				if err = db.FinishAdvisory(t.Context(), attempt, f, a.CreatedAt); !errors.Is(err, ErrWorkflowFenced) {
					t.Fatalf("duplicate finish: %v", err)
				}
				final, completionWon, err := db.CancelAdvisory(t.Context(), c.DiscussionScope, a.JobID, a.CreatedAt)
				if err != nil || !completionWon || final.Job.State != "succeeded" {
					t.Fatalf("completion race: %+v %v %v", final, completionWon, err)
				}
			}
			after, err := db.ReadDiscussion(t.Context(), c.DiscussionScope, c.ID)
			if err != nil || after.ActiveJob != "" || after.Revision != 2 {
				t.Fatalf("release: %+v %v", after, err)
			}
			if !queued && (len(after.Exchanges) != 1 || after.Exchanges[0].Basis != compactAdvisoryBasis(a.Basis)) {
				t.Fatalf("lost exchange: %+v", after)
			}
		})
	}
}
func TestDiscussionContextUTF8AndWholeExchangeLimits(t *testing.T) {
	history := make([]DiscussionExchange, 12)
	for i := range history {
		history[i] = DiscussionExchange{Question: strconv.Itoa(i), Answer: strings.Repeat("é", 100)}
	}
	got, omitted, err := BoundedDiscussionContext(history, "")
	if err != nil || omitted != 2 || len(got) != 10 || got[0].Question != "2" {
		t.Fatalf("count: %v %d %v", got, omitted, err)
	}
	got, omitted, err = BoundedDiscussionContext(history, strings.Repeat("é", 16300))
	if err != nil || omitted != 12 || len(got) != 0 {
		t.Fatalf("byte bound: %d %d %v", len(got), omitted, err)
	}
	if _, _, err = BoundedDiscussionContext(history, strings.Repeat("é", 16385)); !errors.Is(err, ErrConnectorInvalid) {
		t.Fatalf("oversized question: %v", err)
	}
	if _, _, err = BoundedDiscussionContext(nil, string([]byte{0xff})); !errors.Is(err, ErrConnectorInvalid) {
		t.Fatalf("invalid UTF8: %v", err)
	}
}
func TestDiscussionConclusionFreezeAndUncertainNoRetry(t *testing.T) {
	db, c, a := discussionFixture(t)
	conclusion := DiscussionConclusion{DiscussionScope: c.DiscussionScope, ID: "publication", ConversationID: c.ID, ConclusionKey: "conclusion-one", Digest: "summary-digest", Summary: `{"question":"Why?","conclusion":"Because."}`, Rendered: "Safe frozen bytes", GrantID: "publish-grant", ConsentRevision: "1", CreatedAt: a.CreatedAt}
	stored, err := db.ReserveDiscussionConclusion(t.Context(), conclusion)
	if err != nil || stored.State != "pending" {
		t.Fatalf("reserve: %+v %v", stored, err)
	}
	retry := conclusion
	retry.ID = "another"
	retry.Rendered = "Regenerated bytes"
	got, err := db.ReserveDiscussionConclusion(t.Context(), retry)
	if err != nil || got.Rendered != conclusion.Rendered || got.ID != conclusion.ID {
		t.Fatalf("freeze: %+v %v", got, err)
	}
	retry.Digest = "different"
	if _, err = db.ReserveDiscussionConclusion(t.Context(), retry); !errors.Is(err, ErrWorkflowConflict) {
		t.Fatalf("changed content: %v", err)
	}
	if err = db.UpdateDiscussionConclusion(t.Context(), c.DiscussionScope, stored.ID, "pending", "sending", "", "", a.CreatedAt); err != nil {
		t.Fatal(err)
	}
	due := a.CreatedAt.Add(time.Minute)
	if err = db.UpdateDiscussionConclusionRetry(t.Context(), c.DiscussionScope, stored.ID, "sending", "rate limited", due, a.CreatedAt); err != nil {
		t.Fatal(err)
	}
	if queued, err := db.ListPendingDiscussionConclusions(t.Context(), a.CreatedAt, 10); err != nil || len(queued) != 0 {
		t.Fatalf("early retry: %+v %v", queued, err)
	}
	if err = db.UpdateDiscussionConclusion(t.Context(), c.DiscussionScope, stored.ID, "pending", "sending", "", "", a.CreatedAt); !errors.Is(err, ErrWorkflowRevision) {
		t.Fatalf("early dispatch bypassed due: %v", err)
	}
	if queued, err := db.ListPendingDiscussionConclusions(t.Context(), due, 10); err != nil || len(queued) != 1 {
		t.Fatalf("due retry: %+v %v", queued, err)
	}
	if err = db.UpdateDiscussionConclusion(t.Context(), c.DiscussionScope, stored.ID, "pending", "sending", "", "", due); err != nil {
		t.Fatal(err)
	}
	if err = db.UpdateDiscussionConclusion(t.Context(), c.DiscussionScope, stored.ID, "sending", "uncertain", "response lost", "", due); err != nil {
		t.Fatal(err)
	}
	if err = db.UpdateDiscussionConclusion(t.Context(), c.DiscussionScope, stored.ID, "uncertain", "pending", "retry", "", a.CreatedAt); !errors.Is(err, ErrConnectorInvalid) {
		t.Fatalf("uncertain repost: %v", err)
	}
	if err = db.UpdateDiscussionConclusion(t.Context(), c.DiscussionScope, stored.ID, "uncertain", "delivered", "receipt", "https://github.com/acme/api/issues/1#issuecomment-1", a.CreatedAt); err != nil {
		t.Fatal(err)
	}
	scope := c.DiscussionScope
	scope.OwnerGeneration++
	if _, err = db.ReadDiscussionConclusion(t.Context(), scope, stored.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-generation publication: %v", err)
	}
}

func TestDiscussionMigrationPreservesLegacyJobAndProposal(t *testing.T) {
	db, err := Open(t.Context(), filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	var prior []migration
	var addition migration
	for _, m := range migrations {
		if m.name == "task_discussions" {
			addition = m
		} else if m.version < 20261031204000000 {
			prior = append(prior, m)
		}
	}
	if err = db.migrateAll(t.Context(), prior); err != nil {
		t.Fatal(err)
	}
	owner := connectorTestOwner(t, db, 42)
	at := time.Date(2026, 10, 31, 20, 0, 0, 0, time.UTC)
	p := OwnerAction{ID: "old", PolicyVersion: 1, Principal: OwnerActionPrincipal{OwnerID: "42", OwnerGeneration: owner.Generation, Transport: "discord", ChannelID: "channel", DiscordOwnerID: "owner"}, RequestKey: "old", RequestDigest: "digest", Facts: `{"old":true}`, Payload: `{"old":true}`, Digest: "digest", Scope: "standards:management", Tier: "conversational", State: "awaiting_confirmation", CreatedAt: at, AvailableAt: at, ConfirmNotBefore: at.Add(5 * time.Second), ExpiresAt: at.Add(15 * time.Minute), ExecutionID: "owner-action:old"}
	if err = db.CreateOwnerAction(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	r := WorkflowRequest{OwnerID: "42", OwnerGeneration: owner.Generation, Key: "key", Digest: "digest", ProposalID: p.ID, JobID: "old-job", Snapshot: `{"saved":"basis"}`, Inputs: `{"input":true}`, Kind: "start", CreatedAt: at}
	if _, err = db.ReserveWorkflowRequest(t.Context(), r); err != nil {
		t.Fatal(err)
	}
	// The old table has the original ten columns and no advisory metadata.
	if _, err = db.sql.ExecContext(t.Context(), `INSERT INTO durable_workflow_jobs(id,owner_id,owner_generation,snapshot,state,result,created_at,updated_at) VALUES('old-job','42',?,'{"saved":"basis"}','queued','{"retained":true}',?,?)`, owner.Generation, ownerActionTime(at), ownerActionTime(at)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.sql.ExecContext(t.Context(), `INSERT INTO durable_workflow_attempts(id,job_id,proposal_id,execution_id,state,fence,queued_at,started_at,deadline,finished_at) VALUES(77,'old-job','old','old-execution','succeeded',12,?,?,?,?)`, ownerActionTime(at), ownerActionTime(at), ownerActionTime(at.Add(time.Minute)), ownerActionTime(at)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.sql.ExecContext(t.Context(), `UPDATE sqlite_sequence SET seq=100 WHERE name='durable_workflow_attempts'`); err != nil {
		t.Fatal(err)
	}
	beforeJob, err := db.ReadWorkflowJob(t.Context(), r.JobID)
	if err != nil {
		t.Fatal(err)
	}
	var oldAttempt []any
	if err = db.sql.QueryRowContext(t.Context(), `SELECT json_array(id,job_id,proposal_id,execution_id,state,fence,queued_at,started_at,deadline,finished_at) FROM durable_workflow_attempts WHERE id=77`).Scan(newJSONScan(&oldAttempt)); err != nil {
		t.Fatal(err)
	}
	if err = db.applyMigration(t.Context(), addition); err != nil {
		t.Fatal(err)
	}
	afterJob, err := db.ReadWorkflowJob(t.Context(), r.JobID)
	if err != nil || !reflect.DeepEqual(beforeJob, afterJob) {
		t.Fatalf("changed job: %+v %+v %v", beforeJob, afterJob, err)
	}
	afterRequest, err := db.WorkflowRequestByKey(t.Context(), r.OwnerID, r.OwnerGeneration, r.Key)
	if err != nil || !reflect.DeepEqual(r, afterRequest) {
		t.Fatalf("changed request: %+v %v", afterRequest, err)
	}
	var sequence int64
	if err = db.sql.QueryRowContext(t.Context(), `SELECT seq FROM sqlite_sequence WHERE name='durable_workflow_attempts'`).Scan(&sequence); err != nil || sequence != 100 {
		t.Fatalf("changed sequence: %d %v", sequence, err)
	}
	afterProposal, err := db.ReadOwnerAction(t.Context(), p.ID)
	if err != nil || !reflect.DeepEqual(p, afterProposal) {
		t.Fatalf("changed proposal: %+v %v", afterProposal, err)
	}
	attempt, err := db.WorkflowAttemptByExecution(t.Context(), "old-execution")
	if err != nil || attempt.ID != 77 || attempt.Fence != 12 || attempt.AdmissionKind != "confirmed_workflow" || attempt.AdvisoryID != "" {
		t.Fatalf("changed attempt: %+v %v", attempt, err)
	}
	var afterAttempt []any
	if err = db.sql.QueryRowContext(t.Context(), `SELECT json_array(id,job_id,proposal_id,execution_id,state,fence,queued_at,started_at,deadline,finished_at) FROM durable_workflow_attempts WHERE id=77`).Scan(newJSONScan(&afterAttempt)); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(oldAttempt, afterAttempt) {
		t.Fatalf("changed historical bytes: %v %v", oldAttempt, afterAttempt)
	}
	if _, err = db.sql.ExecContext(t.Context(), `INSERT INTO durable_workflow_attempts(job_id,proposal_id,execution_id,state,queued_at,admission_kind) VALUES('old-job','old','invalid','queued',?,'advisory')`, ownerActionTime(at)); err == nil {
		t.Fatal("advisory admitted with proposal authority")
	}
}

type discussionJSONScanner struct{ out any }

func newJSONScan(out any) *discussionJSONScanner { return &discussionJSONScanner{out: out} }
func (s *discussionJSONScanner) Scan(value any) error {
	switch v := value.(type) {
	case string:
		return json.Unmarshal([]byte(v), s.out)
	case []byte:
		return json.Unmarshal(v, s.out)
	default:
		return errors.New("unexpected JSON SQL type")
	}
}

func discussionAuthorityFixture(t *testing.T) (*DB, DiscussionConversation, AdvisoryAdmission, OwnerActionPrincipal) {
	t.Helper()
	db, c, a := discussionFixture(t)
	owner := connectorTestOwner(t, db, 42)
	connectorTestClient(t, db, "client")
	c.OwnerGeneration = owner.Generation
	a.OwnerGeneration = owner.Generation
	if _, err := db.sql.ExecContext(t.Context(), `UPDATE task_discussions SET owner_generation=?`, owner.Generation); err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(t.Context(), Repo{Forge: "github", Owner: "acme", Name: "api", Path: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.GetOrCreateSession(t.Context(), repo, 1); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	a.CreatedAt = now
	if _, err = db.sql.ExecContext(t.Context(), `INSERT INTO connector_grants(id,client_id,resource,scope,owner_id,generation,created_at,expires_at) VALUES('grant','client',?,'task:advisory task:discussion-publish',42,?,?,?)`, connectorTestResource, owner.Generation, FormatTime(now), FormatTime(now.Add(time.Hour))); err != nil {
		t.Fatal(err)
	}
	if _, err = db.sql.ExecContext(t.Context(), `INSERT INTO connector_grant_repositories(grant_id,repo_id,repository_ref) VALUES('grant',?,'github:acme/api')`, repo.ID); err != nil {
		t.Fatal(err)
	}
	actor := OwnerActionPrincipal{OwnerID: "42", OwnerGeneration: owner.Generation, Transport: "mcp", ClientID: "client", GrantID: "grant"}
	return db, c, a, actor
}
func TestDiscussionAdmissionAndPublicationCheckConsentAtAtomicBoundary(t *testing.T) {
	t.Run("admission", func(t *testing.T) {
		db, _, a, actor := discussionAuthorityFixture(t)
		if _, err := db.AdmitAuthorizedAdvisory(t.Context(), actor, a); err != nil {
			t.Fatal(err)
		}
		if err := db.RevokeConnectorGrant(t.Context(), 42, "grant", a.CreatedAt); err != nil {
			t.Fatal(err)
		}
		if _, err := db.AdmitAuthorizedAdvisory(t.Context(), actor, a); !errors.Is(err, ErrConnectorInvalid) {
			t.Fatalf("revoked replay released private work: %v", err)
		}
	})
	t.Run("publication", func(t *testing.T) {
		db, c, a, _ := discussionAuthorityFixture(t)
		pub, err := db.ReserveDiscussionConclusion(t.Context(), DiscussionConclusion{DiscussionScope: c.DiscussionScope, ID: "publication", ConversationID: c.ID, ConclusionKey: "conclusion", Digest: "digest", Summary: `{"conclusion":"safe"}`, Rendered: "safe bytes", GrantID: "grant", CreatedAt: a.CreatedAt})
		if err != nil {
			t.Fatal(err)
		}
		if err = db.RevokeConnectorGrant(t.Context(), 42, "grant", a.CreatedAt); err != nil {
			t.Fatal(err)
		}
		if err = db.ClaimDiscussionPublication(t.Context(), pub, a.CreatedAt); !errors.Is(err, ErrConnectorInvalid) {
			t.Fatalf("revoked grant claimed possible send: %v", err)
		}
		retained, err := db.ReadDiscussionConclusion(t.Context(), c.DiscussionScope, pub.ID)
		if err != nil || retained.State != "pending" {
			t.Fatalf("failed auth changed publication: %+v %v", retained, err)
		}
	})
	t.Run("claim-once", func(t *testing.T) {
		db, c, a, _ := discussionAuthorityFixture(t)
		pub, err := db.ReserveDiscussionConclusion(t.Context(), DiscussionConclusion{DiscussionScope: c.DiscussionScope, ID: "publication", ConversationID: c.ID, ConclusionKey: "conclusion", Digest: "digest", Summary: `{"conclusion":"safe"}`, Rendered: "safe bytes", GrantID: "grant", CreatedAt: a.CreatedAt})
		if err != nil {
			t.Fatal(err)
		}
		tampered := pub
		tampered.Rendered = "different bytes"
		if err = db.ClaimDiscussionPublication(t.Context(), tampered, a.CreatedAt); !errors.Is(err, ErrWorkflowConflict) {
			t.Fatalf("changed bytes claimed: %v", err)
		}
		if err = db.ClaimDiscussionPublication(t.Context(), pub, a.CreatedAt); err != nil {
			t.Fatal(err)
		}
		if err = db.ClaimDiscussionPublication(t.Context(), pub, a.CreatedAt); !errors.Is(err, ErrWorkflowRevision) {
			t.Fatalf("duplicate send claim: %v", err)
		}
	})
}

func TestDiscussionUncertainStopDoesNotClaimCompletionWon(t *testing.T) {
	db, c, a := discussionFixture(t)
	if _, err := db.AdmitAdvisory(t.Context(), a); err != nil {
		t.Fatal(err)
	}
	attempt, _, err := db.ClaimWorkflowAttempt(t.Context(), a.CreatedAt, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.FinishAdvisory(t.Context(), attempt, WorkflowFinish{State: "uncertain", Result: `{"answer":"Provider completion is unknown"}`, Questions: `[]`}, a.CreatedAt); err != nil {
		t.Fatal(err)
	}
	stopped, won, err := db.CancelAdvisory(t.Context(), c.DiscussionScope, a.JobID, a.CreatedAt)
	if err != nil || won || stopped.Job.State != "uncertain" {
		t.Fatalf("uncertainty became confirmed completion: %+v %v %v", stopped, won, err)
	}
}

func TestDiscussionConsentProvenanceSurvivesGrantExpiryCleanup(t *testing.T) {
	db, c, a, actor := discussionAuthorityFixture(t)
	principal, err := json.Marshal(actor)
	if err != nil {
		t.Fatal(err)
	}
	a.ConsentRevision = string(principal)
	admitted, err := db.AdmitAuthorizedAdvisory(t.Context(), actor, a)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := db.ReserveDiscussionConclusion(t.Context(), DiscussionConclusion{DiscussionScope: c.DiscussionScope, ID: "publication", ConversationID: c.ID, ConclusionKey: "conclusion", Digest: "digest", Summary: `{"conclusion":"safe"}`, Rendered: "safe bytes", GrantID: "grant", ConsentRevision: string(principal), CreatedAt: a.CreatedAt})
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range []struct{ raw, scope string }{{admitted.ConsentRevision, "task:advisory"}, {pub.ConsentRevision, "task:discussion-publish"}} {
		var evidence struct {
			Principal OwnerActionPrincipal
			Evidence  discussionConsentEvidence `json:"_consent_evidence"`
		}
		if err = json.Unmarshal([]byte(record.raw), &evidence); err != nil {
			t.Fatal(err)
		}
		var recovered OwnerActionPrincipal
		if err = json.Unmarshal([]byte(record.raw), &recovered); err != nil || recovered != actor {
			t.Fatalf("principal contract changed: %+v %v", recovered, err)
		}
		e := evidence.Evidence
		if e.RequiredScope != record.scope || e.Scope != "task:advisory task:discussion-publish" || e.OwnerID != 42 || e.OwnerGeneration != actor.OwnerGeneration || e.GrantID != "grant" || e.ClientID != "client" || e.Resource != connectorTestResource || e.CreatedAt == "" || e.ExpiresAt == "" || e.CoverageDigest == "" || len(e.Repositories) != 1 || e.Repositories[0].RepositoryRef != "github:acme/api" {
			t.Fatalf("missing historical consent evidence: %+v", e)
		}
	}
	// A reconnect keeps the exact original consent snapshot rather than capturing
	// a new timestamp for an already admitted job or conclusion.
	retry := a
	retry.CreatedAt = retry.CreatedAt.Add(time.Minute)
	replay, err := db.AdmitAuthorizedAdvisory(t.Context(), actor, retry)
	if err != nil || replay.ConsentRevision != admitted.ConsentRevision {
		t.Fatalf("admission provenance changed on retry: %+v %v", replay, err)
	}
	retryPub := pub
	retryPub.CreatedAt = retryPub.CreatedAt.Add(time.Minute)
	replayPub, err := db.ReserveDiscussionConclusion(t.Context(), retryPub)
	if err != nil || replayPub.ConsentRevision != pub.ConsentRevision {
		t.Fatalf("publication provenance changed on retry: %+v %v", replayPub, err)
	}
	if err = db.CleanupConnectorState(t.Context(), a.CreatedAt.Add(2*time.Hour), 100); err != nil {
		t.Fatal(err)
	}
	var grants int
	if err = db.sql.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM connector_grants WHERE id='grant'`).Scan(&grants); err != nil || grants != 0 {
		t.Fatalf("expiry cleanup did not remove grant: %d %v", grants, err)
	}
	saved, err := db.ReadAdvisory(t.Context(), c.DiscussionScope, a.JobID)
	if err != nil || saved.ConsentRevision != admitted.ConsentRevision {
		t.Fatalf("lost advisory consent history: %+v %v", saved, err)
	}
	savedPub, err := db.ReadDiscussionConclusion(t.Context(), c.DiscussionScope, pub.ID)
	if err != nil || savedPub.ConsentRevision != pub.ConsentRevision {
		t.Fatalf("lost publication consent history: %+v %v", savedPub, err)
	}
	if err = db.ClaimDiscussionPublication(t.Context(), savedPub, a.CreatedAt.Add(2*time.Hour)); !errors.Is(err, ErrConnectorInvalid) {
		t.Fatalf("historical evidence revived expired consent: %v", err)
	}
}
