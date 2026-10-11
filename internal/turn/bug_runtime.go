package turn

import (
	"context"
	"errors"
	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/bugchecks"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

// BugPRForge supplies shared PR observations and factory-owned creation.
type BugPRForge interface {
	GetDeliveryHead(context.Context, github.Repo, string) (string, error)
	ListDeliveryPRs(context.Context, github.Repo, string) ([]github.DeliveryPR, error)
	GetDeliveryPR(context.Context, github.Repo, int) (github.DeliveryPR, error)
	CreateDeliveryPR(context.Context, github.Repo, string, string, redact.Body, redact.Body) (github.DeliveryPR, error)
}

// BugMergeForge supplies guarded merge and independent integration observations.
type BugMergeForge interface {
	GetDeliveryPR(context.Context, github.Repo, int) (github.DeliveryPR, error)
	GetDeliveryHead(context.Context, github.Repo, string) (string, error)
	GetDeliveryCommit(context.Context, github.Repo, string) (github.DeliveryCommit, error)
	MergeDeliveryPR(context.Context, github.Repo, int, string) error
}

// BugCloseForge supplies numeric issue observation and confirmed closure.
type BugCloseForge interface {
	BaseURL() string
	GetIssue(context.Context, github.Repo, int) (github.Issue, error)
	CloseDeliveredParent(context.Context, github.Repo, int, int64) error
}

// BugCodeForge keeps repository and integration operations separate from tickets.
type BugCodeForge interface {
	BugPRForge
	BugMergeForge
}

// BugRuntimeForge is the explicit legacy combined dependency.
type BugRuntimeForge interface {
	BugPRForge
	BugMergeForge
	BugCloseForge
}

// BugRuntimeConfig supplies operator-selected identity, permissions and isolation.
type BugRuntimeConfig struct {
	Forge       BugRuntimeForge
	Code        BugCodeForge
	Tickets     BugCloseForge
	Permissions func(agent.Kind, string) (BugRolePermissions, error)
	Isolation   bugchecks.Isolation
	OwnerID     int64
}

// BugRolePermissions carries factory-selected tool ceilings and sandbox policy.
type BugRolePermissions struct {
	Tools, AllowedTools []string
	Sandbox             string
}

// BugRuntime supports only the pinned simplified graph contract.
type BugRuntime struct {
	opts       Options
	cfg        BugRuntimeConfig
	definition *teamsource.WorkflowDefinition
}

// Execute runs one admitted simplified Bug continuation.
func (r *BugRuntime) Execute(ctx context.Context, req Request, result Result) (Result, error) {
	return r.executeGraph(ctx, req, result)
}

// Reconcile recovers supported graph waits and settled orphan attempts.
func (r *BugRuntime) Reconcile(ctx context.Context) error { return r.reconcileGraphBug(ctx) }
func (r *BugRuntime) identity(ctx context.Context, session int64) (store.BugIdentity, error) {
	s, e := r.opts.DB.GetSession(ctx, session)
	if e != nil {
		return store.BugIdentity{}, e
	}
	ref, e := naming.ParseRef(s.TaskRef)
	if e != nil {
		return store.BugIdentity{}, e
	}
	issue, e := r.cfg.Tickets.GetIssue(ctx, github.Repo{Owner: ref.Owner, Name: ref.Repo}, s.IssueNumber)
	if e != nil {
		return store.BugIdentity{}, e
	}
	if issue.ID <= 0 || issue.Number != s.IssueNumber {
		return store.BugIdentity{}, errors.New("bug issue observation identity mismatch")
	}
	return store.BugIdentity{SessionID: session, Repository: ref.Owner + "/" + ref.Repo, IssueID: issue.ID, IssueNumber: int64(issue.Number), OwnerID: r.cfg.OwnerID}, nil
}

func (r *BugRuntime) pin(ctx context.Context, d *teamsource.WorkflowDefinition, path string) (store.GraphDefinition, error) {
	return r.opts.DB.CreateGraphDefinition(ctx, store.GraphDefinition{Workflow: "bug", Team: d.Profile.Team, ProfileVersion: d.Profile.ProfileVersion, Path: path, Digest: d.Profile.Digest, TeamSnapshotDigest: d.Digest})
}

// CheckExistingAdmission refuses unsupported old pins before factory scheduling,
// reporting, locking or source preparation can change their retained work.
func (r *BugRuntime) CheckExistingAdmission(ctx context.Context, sessionID int64) error {
	instance, err := r.opts.DB.LatestStandaloneBugInstance(ctx, sessionID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	definition, err := r.opts.DB.GetGraphDefinition(ctx, instance.DefinitionID)
	if err != nil {
		return err
	}
	if definition.ProfileVersion != 2 {
		return errors.New("unsupported pinned legacy Bug; existing work is preserved and dispatch is refused")
	}
	specs, err := SimplifiedBugSpecs()
	if err != nil {
		return err
	}
	saved, err := r.opts.DB.WorkflowCaptureForAdmission(ctx, sessionID, 0, "bug")
	if err != nil {
		return err
	}
	_, err = restoreWorkflow(ctx, r.opts.DB, saved, agent.WorkflowBug, specs)
	return err
}
