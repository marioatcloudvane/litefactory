// Command factory-pending-state creates and reads an offline compatibility
// fixture. It uses public store operations and never contacts a provider.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

var at = time.Date(2026, 11, 2, 22, 0, 0, 0, time.UTC)

// The manifest records values returned by the baseline implementation, including
// its serializers, rather than rebuilding expected values in the reader.
type manifest struct {
	Baseline      string
	Session       store.Session
	Attempt       store.GraphNodeAttempt
	Capture       store.WorkflowCapture
	Code, Tickets store.ConnectionVersion
	Owner         store.ConnectorOwner
	Grants        []store.ConnectorGrantSummary
	Proposal      store.OwnerAction
	Delivery      store.ReportDelivery
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
func main() {
	ctx := context.Background() //nolint:forbidigo // This standalone offline fixture command creates its root context in main.
	if len(os.Args) != 5 {
		panic("usage: factory-pending-state create|verify database manifest baseline-commit")
	}
	db, err := store.Open(ctx, os.Args[2])
	must(err)
	defer func() { must(db.Close()) }()
	must(db.Migrate(ctx))
	switch os.Args[1] {
	case "create":
		m := create(ctx, db)
		m.Baseline = os.Args[4]
		data, err := json.MarshalIndent(m, "", "  ")
		must(err)
		must(os.WriteFile(os.Args[3], append(data, '\n'), 0600)) //nolint:gosec // G703: explicit local fixture destination supplied by the trusted operator script.
	case "verify":
		data, err := os.ReadFile(os.Args[3]) //nolint:gosec // G703: trusted local fixture paths supplied by the operator script; no server input.
		must(err)
		var expected manifest
		must(json.Unmarshal(data, &expected))
		if expected.Baseline != os.Args[4] {
			panic("fixture baseline mismatch")
		}
		actual := read(ctx, db, expected.Session.ID, expected.Attempt.ID, expected.Code.ID, expected.Tickets.ID)
		actual.Baseline = expected.Baseline
		want, err := json.Marshal(expected)
		must(err)
		got, err := json.Marshal(actual)
		must(err)
		if string(want) != string(got) {
			panic(fmt.Sprintf("pending state changed\nexpected: %s\nactual: %s", want, got))
		}
	default:
		panic("unknown operation")
	}
	_, err = fmt.Fprintln(os.Stdout, "offline pending-state "+os.Args[1]+" passed")
	must(err)
}

func create(ctx context.Context, db *store.DB) manifest {
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: "/offline-fixture/acme/api", Forge: store.ForgeGitHub, Owner: "acme", Name: "api", RemoteURL: "git@github.com:acme/api.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: "/offline-fixture/worktrees"})
	must(err)
	session, err := db.GetOrCreateSession(ctx, repo, 9516)
	must(err)
	_, err = db.AdmitWorkflowPath(ctx, session.ID, "graph")
	must(err)
	definition, err := db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: "builtin", Mode: "research", ProfileVersion: 2, Path: "research/research.dot", Digest: strings.Repeat("a", 64), CreatedAt: at})
	must(err)
	config := []byte("name: standard\nversion: 1.0.0\n")
	sum := sha256.Sum256(config)
	launch, err := db.AdmitWorkflowLaunch(ctx, store.WorkflowLaunch{SessionID: session.ID, TriggerID: 1, DefinitionID: definition.ID, Mode: "research", Start: "work", ContentDigest: strings.Repeat("b", 64), TopologyDigest: strings.Repeat("c", 64), Files: []byte(`{"research.dot":"eA=="}`), Provenance: store.WorkflowProvenance{Kind: "fetched", Source: "https://example.test/teams/index.json", Slug: "standard", Version: "1.0.0", ReleaseDigest: strings.Repeat("d", 64), GraphPath: "research/research.dot", GraphFile: "research.dot", Team: "builtin", TeamConfig: config, ConfigDigest: hex.EncodeToString(sum[:])}})
	must(err)
	attempt, err := db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: launch.Instance.ID, LaunchID: launch.LaunchID, Node: "work", StartedAt: at})
	must(err)
	connection := store.ConnectionVersion{RepoID: repo.ID, Role: store.CodeConnection, Provider: ticket.GitHub, Origin: "https://api.github.com", Destination: repo.Slug(), CredentialSource: "env", CredentialEnv: "OFFLINE_FIXTURE_TOKEN", Enabled: true, Provenance: "explicit", ClonePath: repo.Path, RemoteURL: repo.RemoteURL, BaseBranch: repo.BaseBranch, FeaturePrefix: repo.FeaturePrefix, WorktreeRoot: repo.WorktreeRoot}
	code, err := db.SetConnection(ctx, connection, 0)
	must(err)
	connection.Role = store.TicketConnection
	tickets, err := db.SetConnection(ctx, connection, 0)
	must(err)
	connection.CredentialEnv = "REPLACEMENT_OFFLINE_FIXTURE_TOKEN"
	_, err = db.SetConnection(ctx, connection, tickets.Revision)
	must(err)
	_, err = db.UpsertIdentity(ctx, store.Identity{Forge: store.ForgeGitHub, Role: store.RoleOwner, UserID: 42, Handle: "owner", Source: store.SourceGH})
	must(err)
	owner, err := db.ConnectorOwner(ctx, 42)
	must(err)
	must(db.RegisterConnectorClient(ctx, store.ConnectorClient{ID: "pending-client", Name: "Offline fixture", AuthMethod: "none", Redirects: []string{"https://client.test/callback"}, CreatedAt: at, ExpiresAt: at.Add(24 * time.Hour)}, at))
	browser := store.ConnectorBrowser{ID: "pending-browser", ClientID: "pending-client", RedirectURI: "https://client.test/callback", State: "state", Resource: "https://example.test/litefactory/mcp", Scope: "standards:management", PKCEChallenge: "challenge", NonceDigest: "nonce", UpstreamStateDigest: "upstream", OwnerGeneration: owner.Generation, ExpiresAt: at.Add(10 * time.Minute)}
	must(db.CreateConnectorBrowser(ctx, browser, 42, at))
	must(db.VerifyConnectorBrowser(ctx, browser.ID, "nonce", "upstream", 42, at))
	grant := store.ConnectorGrant{ID: "pending-grant", ClientID: browser.ClientID, Resource: browser.Resource, Scope: browser.Scope, OwnerID: 42, OwnerGeneration: owner.Generation, CreatedAt: at, ExpiresAt: at.Add(time.Hour)}
	authCode := store.ConnectorCode{Digest: "offline-code", GrantID: grant.ID, ClientID: browser.ClientID, RedirectURI: browser.RedirectURI, Resource: browser.Resource, Scope: browser.Scope, PKCEChallenge: browser.PKCEChallenge, ExpiresAt: at.Add(5 * time.Minute)}
	must(db.ConsentConnectorBrowser(ctx, browser.ID, "nonce", 42, at, grant, authCode, true))
	must(db.CreateOwnerAction(ctx, store.OwnerAction{ID: "pending-proposal", PolicyVersion: 1, Principal: store.OwnerActionPrincipal{OwnerID: "42", OwnerGeneration: owner.Generation, Transport: "mcp", ClientID: browser.ClientID, GrantID: grant.ID}, RequestKey: "pending-key", RequestDigest: "pending-request", Facts: `{"target_revision":"reviewed","connection_version":1}`, Payload: `{"exact":"protected pending payload"}`, Digest: "pending-digest", Scope: browser.Scope, Tier: "conversational", State: "awaiting_confirmation", CreatedAt: at, AvailableAt: at, ConfirmNotBefore: at.Add(5 * time.Second), ExpiresAt: at.Add(15 * time.Minute), ExecutionID: "owner-action:pending-proposal"}))
	segment, err := db.GetOrCreateSegment(ctx, session.ID, store.FirstSegmentOrdinal, "offline-attempt", store.AgentClaude)
	must(err)
	run, err := db.AllocateRun(ctx, store.AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: store.ModeSpike, Agent: store.AgentClaude, TriggerCommentID: 55, LogPath: func(int) string { return "/offline-fixture/log.jsonl" }})
	must(err)
	_, err = db.FinishRun(ctx, run.ID, store.RunOutcome{Status: store.RunFailed, Error: "interrupted after result"})
	must(err)
	_, err = db.EnsureReportDelivery(ctx, store.ReportDelivery{Key: "pending-report", SessionID: session.ID, TriggerID: 55, RunID: run.ID, Task: session.TaskRef, Destination: session.TaskRef, TriggerNamespace: "comment", Purpose: "run", BotUserID: 9, Body: "redacted offline result marker", Marker: "marker"})
	must(err)
	_, err = db.MarkReportDeliverySent(ctx, "pending-report")
	must(err)
	return read(ctx, db, session.ID, attempt.ID, code.ID, tickets.ID)
}

func read(ctx context.Context, db *store.DB, sessionID, attemptID, codeID, ticketsID int64) (m manifest) {
	var err error
	m.Session, err = db.GetSession(ctx, sessionID)
	must(err)
	m.Attempt, err = db.GetGraphNodeAttempt(ctx, attemptID)
	must(err)
	m.Capture, err = db.WorkflowCaptureForAttempt(ctx, attemptID)
	must(err)
	m.Code, err = db.GetConnectionVersion(ctx, codeID)
	must(err)
	m.Tickets, err = db.GetConnectionVersion(ctx, ticketsID)
	must(err)
	m.Owner, err = db.ConnectorOwner(ctx, 42)
	must(err)
	m.Grants, err = db.ListConnectorGrants(ctx, 42, at)
	must(err)
	m.Proposal, err = db.ReadOwnerAction(ctx, "pending-proposal")
	must(err)
	m.Delivery, err = db.GetReportDelivery(ctx, "pending-report")
	must(err)
	return m
}
