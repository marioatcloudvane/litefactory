package linear

import (
	"context"
	"maps"
	"slices"
	"time"

	"github.com/cloudvane/litefactory/internal/ticket"
)

// Inspection is qualified read evidence; remote effects still confirm writes.
type Inspection struct {
	WorkspaceID, TeamID, ProjectID string
	Publisher                      ticket.Actor
	WebhookAdministration          bool
}

// Publisher reads the authenticated publisher in the selected workspace.
func (c *Client) Publisher(ctx context.Context) (ticket.Actor, error) {
	var data struct {
		Viewer struct {
			user
			Organization objectID
		}
	}
	if err := c.query(ctx, "publisher", "query { viewer { "+userFields+" organization { id } } }", nil, false, &data); err != nil {
		return ticket.Actor{}, err
	}
	if data.Viewer.ID == "" || data.Viewer.Organization.ID != c.config.WorkspaceID || !data.Viewer.Active || data.Viewer.ArchivedAt != nil {
		return ticket.Actor{}, failure("publisher", ticket.Refused, "publisher identity or workspace differs")
	}
	return c.actor(&data.Viewer.user), nil
}

// ResolveActor reads an immutable workspace account without inferring authority.
func (c *Client) ResolveActor(ctx context.Context, id string) (ticket.Actor, error) {
	if id == "" {
		return ticket.Actor{}, failure("identity", ticket.Refused, "immutable actor ID is required")
	}
	var data struct {
		User struct {
			user
			Organization objectID
		}
	}
	if err := c.query(ctx, "identity", "query($id: String!) { user(id: $id) { "+userFields+" organization { id } } }", map[string]string{"id": id}, false, &data); err != nil {
		return ticket.Actor{}, err
	}
	if data.User.ID != id || data.User.Organization.ID != c.config.WorkspaceID {
		return ticket.Actor{}, failure("identity", ticket.Refused, "actor identity or workspace differs")
	}
	return c.actor(&data.User.user), nil
}

// Inspect verifies destination and mapping IDs and reports webhook capability separately.
func (c *Client) Inspect(ctx context.Context) (Inspection, error) {
	publisher, err := c.Publisher(ctx)
	if err != nil {
		return Inspection{}, err
	}
	var data struct {
		Team struct {
			ID           string
			ArchivedAt   *time.Time
			Organization objectID
		}
		Project struct {
			ID         string
			ArchivedAt *time.Time
			Teams      struct {
				Nodes    []objectID
				PageInfo pageInfo
			}
		}
	}
	err = c.query(ctx, "inspect connection", "query($team: String!, $project: String!) { team(id: $team) { id archivedAt organization { id } } project(id: $project) { id archivedAt teams(first: 100) { nodes { id } pageInfo { hasNextPage } } } }", map[string]string{"team": c.config.TeamID, "project": c.config.ProjectID}, false, &data)
	if err != nil {
		return Inspection{}, err
	}
	if data.Team.ID != c.config.TeamID || data.Team.Organization.ID != c.config.WorkspaceID || data.Team.ArchivedAt != nil || data.Project.ID != c.config.ProjectID || data.Project.ArchivedAt != nil || !data.Project.Teams.PageInfo.observed || data.Project.Teams.PageInfo.HasNextPage {
		return Inspection{}, failure("inspect connection", ticket.Refused, "workspace, team or project is inaccessible or stale")
	}
	found := false
	for _, team := range data.Project.Teams.Nodes {
		if team.ID == c.config.TeamID {
			found = true
		}
	}
	if !found {
		return Inspection{}, failure("inspect connection", ticket.Refused, "project is not in selected team")
	}
	seen := map[string]bool{}
	for _, name := range slices.Sorted(maps.Keys(c.config.ModeLabels)) {
		id := c.config.ModeLabels[name]
		if id == "" || seen[id] {
			return Inspection{}, failure("inspect mappings", ticket.Refused, "mode mappings are missing or ambiguous")
		}
		seen[id] = true
		var label struct {
			IssueLabel struct {
				ID           string
				ArchivedAt   *time.Time
				Team         *objectID
				Organization objectID
				IsGroup      bool
			}
		}
		if err := c.query(ctx, "inspect label", "query($id: String!) { issueLabel(id: $id) { id archivedAt isGroup team { id } organization { id } } }", map[string]string{"id": id}, false, &label); err != nil {
			return Inspection{}, err
		}
		if label.IssueLabel.ID != id || label.IssueLabel.ArchivedAt != nil || label.IssueLabel.IsGroup || label.IssueLabel.Organization.ID != c.config.WorkspaceID || (label.IssueLabel.Team != nil && label.IssueLabel.Team.ID != c.config.TeamID) {
			return Inspection{}, failure("inspect label", ticket.Refused, "mode label does not belong to selected workspace/team")
		}
	}
	seen = map[string]bool{}
	for _, name := range slices.Sorted(maps.Keys(c.config.States)) {
		id := c.config.States[name]
		if id == "" || seen[id] {
			return Inspection{}, failure("inspect mappings", ticket.Refused, "state mappings are missing or ambiguous")
		}
		seen[id] = true
		var state struct {
			WorkflowState struct {
				ID, Type   string
				ArchivedAt *time.Time
				Team       objectID
			}
		}
		if err := c.query(ctx, "inspect state", "query($id: String!) { workflowState(id: $id) { id type archivedAt team { id } } }", map[string]string{"id": id}, false, &state); err != nil {
			return Inspection{}, err
		}
		i := state.WorkflowState
		if i.ID != id || i.Team.ID != c.config.TeamID || i.ArchivedAt != nil || (name == "completed" && i.Type != "completed") || (name == "retired" && i.Type != "canceled") || (name == "open" && (i.Type == "completed" || i.Type == "canceled")) {
			return Inspection{}, failure("inspect state", ticket.Refused, "lifecycle state mapping is incompatible")
		}
	}
	out := Inspection{WorkspaceID: c.config.WorkspaceID, TeamID: c.config.TeamID, ProjectID: c.config.ProjectID, Publisher: publisher}
	_, err = c.ListSubscriptions(ctx, c.Container())
	out.WebhookAdministration = err == nil
	return out, nil
}

// ContainerIdentity verifies and returns the configured project identity.
func (c *Client) ContainerIdentity(ctx context.Context, container string) (ticket.Identity, error) {
	if container != c.Container() {
		return ticket.Identity{}, failure("container", ticket.Refused, "container differs")
	}
	inspection, err := c.Inspect(ctx)
	if err != nil {
		return ticket.Identity{}, err
	}
	return c.identity(inspection.ProjectID), nil
}
