package daemon

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// ProjectCodeDelivery routes only through retained execution versions. Mutable
// project pointers never select a resumed or recovered code publication target.
type ProjectCodeDelivery struct {
	db        *store.DB
	factory   *projectconnection.Factory
	binary    *git.Binary
	sessionID int64
}

// NewProjectCodeDelivery constructs the code-only execution gateway. Supplying
// the startup git binary avoids repeating executable discovery before effects.
func NewProjectCodeDelivery(db *store.DB, factory *projectconnection.Factory, binary ...*git.Binary) *ProjectCodeDelivery {
	result := &ProjectCodeDelivery{db: db, factory: factory}
	if len(binary) > 0 {
		result.binary = binary[0]
	}
	return result
}

func (d *ProjectCodeDelivery) selectClient(ctx context.Context, repo github.Repo, mutation bool) (*github.Client, error) {
	scope, _ := ctx.Value(connectionScopeKey{}).(connectionScope)
	if scoped := turn.ExecutionSessionID(ctx); scoped > 0 {
		scope.sessionID = scoped
		scope.projectID = 0
	}
	if d.sessionID > 0 {
		scope.sessionID = d.sessionID
	}
	var selected store.ExecutionSnapshot
	if scope.sessionID > 0 {
		snapshot, err := d.db.GetExecutionSnapshot(ctx, scope.sessionID)
		if err != nil {
			return nil, err
		}
		selected = snapshot
	} else {
		projects, err := d.db.ListRepos(ctx)
		if err != nil {
			return nil, err
		}
		for _, project := range projects {
			if scope.projectID > 0 && project.ID != scope.projectID {
				continue
			}
			sessions, err := d.db.ListRepoSessions(ctx, project.ID)
			if err != nil {
				return nil, err
			}
			for _, session := range sessions {
				snapshot, err := d.db.GetExecutionSnapshot(ctx, session.ID)
				if errors.Is(err, store.ErrNotFound) {
					continue
				}
				if err != nil {
					return nil, err
				}
				if !strings.EqualFold(snapshot.CodeTarget.Slug(), repo.String()) {
					continue
				}
				if selected.SessionID > 0 && (selected.CodeVersionID != snapshot.CodeVersionID || selected.ActorID != snapshot.ActorID) {
					return nil, store.ErrConnectionConflict
				}
				selected = snapshot
			}
		}
	}
	if selected.SessionID == 0 {
		return nil, store.ErrNotFound
	}
	version, err := d.db.GetConnectionVersion(ctx, selected.CodeVersionID)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(version.Destination, repo.String()) {
		return nil, store.ErrTicketIdentityConflict
	}
	remoteID := selected.CodeRemoteID
	if remoteID == "" {
		remoteID = version.RemoteID
	}
	if remoteID == "" {
		return nil, errors.New("retained code destination has no verified remote identity")
	}
	clients, err := d.factory.Clients(ctx, version, selected.ActorID)
	if err != nil {
		return nil, err
	}
	observed, err := clients.Code.InspectRepository(ctx, repo)
	if err != nil {
		return nil, err
	}
	if remoteID != strconv.FormatInt(observed.ID, 10) || !strings.EqualFold(observed.FullName, version.Destination) {
		return nil, errors.New("original code repository identity changed")
	}
	if mutation {
		binary := d.binary
		if binary == nil {
			binary, err = git.Resolve(ctx)
			if err != nil {
				return nil, err
			}
		}
		clone, err := binary.Open(ctx, version.ClonePath)
		if err != nil {
			return nil, errors.New("original registered clone is unavailable")
		}
		if clone.Path() != version.ClonePath {
			return nil, errors.New("original registered clone ownership changed")
		}
		remote, err := clone.RemoteURL(ctx, "origin")
		if err != nil {
			return nil, err
		}
		if remote != version.RemoteURL {
			return nil, errors.New("original code clone remote changed")
		}
	}
	return clients.Code, nil
}

// GetDeliveryHead reads the branch at its original code destination.
func (d *ProjectCodeDelivery) GetDeliveryHead(ctx context.Context, repo github.Repo, branch string) (string, error) {
	client, err := d.selectClient(ctx, repo, false)
	if err != nil {
		return "", err
	}
	return client.GetDeliveryHead(ctx, repo, branch)
}

// ListDeliveryPRs observes original matching pull requests, including recovery history.
func (d *ProjectCodeDelivery) ListDeliveryPRs(ctx context.Context, repo github.Repo, branch string) ([]github.DeliveryPR, error) {
	client, err := d.selectClient(ctx, repo, false)
	if err != nil {
		return nil, err
	}
	return client.ListDeliveryPRs(ctx, repo, branch)
}

// GetDeliveryPR independently observes a retained pull request.
func (d *ProjectCodeDelivery) GetDeliveryPR(ctx context.Context, repo github.Repo, number int) (github.DeliveryPR, error) {
	client, err := d.selectClient(ctx, repo, false)
	if err != nil {
		return github.DeliveryPR{}, err
	}
	return client.GetDeliveryPR(ctx, repo, number)
}

// GetDeliveryCommit reads the exact commit at the retained code destination.
func (d *ProjectCodeDelivery) GetDeliveryCommit(ctx context.Context, repo github.Repo, sha string) (github.DeliveryCommit, error) {
	client, err := d.selectClient(ctx, repo, false)
	if err != nil {
		return github.DeliveryCommit{}, err
	}
	return client.GetDeliveryCommit(ctx, repo, sha)
}

// CreateDeliveryPR checks original clone ownership before a code effect.
func (d *ProjectCodeDelivery) CreateDeliveryPR(ctx context.Context, repo github.Repo, branch, target string, title, body redact.Body) (github.DeliveryPR, error) {
	client, err := d.selectClient(ctx, repo, true)
	if err != nil {
		return github.DeliveryPR{}, err
	}
	return client.CreateDeliveryPR(ctx, repo, branch, target, title, body)
}

// MergeDeliveryPR retains the original destination and publishing actor.
func (d *ProjectCodeDelivery) MergeDeliveryPR(ctx context.Context, repo github.Repo, number int, head string) error {
	client, err := d.selectClient(ctx, repo, true)
	if err != nil {
		return err
	}
	return client.MergeDeliveryPR(ctx, repo, number, head)
}

// ForSession binds parent delivery recovery to one immutable execution snapshot.
func (d *ProjectCodeDelivery) ForSession(sessionID int64) turn.CodeDelivery {
	bound := *d
	bound.sessionID = sessionID
	return &bound
}
