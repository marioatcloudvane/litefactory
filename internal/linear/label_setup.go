package linear

import (
	"context"
	"errors"
	"time"

	"github.com/cloudvane/litefactory/internal/ticket"
)

// SetupLabel is one non-archived issue label in the connection's team or at workspace level.
type SetupLabel struct {
	ID, Name   string
	IsGroup    bool
	TeamID     string // empty: workspace level
	ParentID   string // empty: not in a group
	ParentName string
}

// NewLabel is one label to create in the connection's team. Color is six hex digits without '#'.
type NewLabel struct {
	Name, Color, Description string
	IsGroup                  bool
	ParentID                 string // a group ID; empty for a flat label or a group
}

// TeamID returns the configured team ID.
func (c *Client) TeamID() string { return c.config.TeamID }

// CanonicalModeLabel returns the canonical label for a mode key, such as "plan" -> "Mode: Plan".
func CanonicalModeLabel(key string) string { return canonicalModeLabel(key) }

// ListSetupLabels reads every non-archived issue label visible to the token
// and keeps those of the connection's team or of the workspace. Any failed or
// inconsistent page returns an error and no list.
func (c *Client) ListSetupLabels(ctx context.Context) ([]SetupLabel, error) {
	out := []SetupLabel{}
	after := ""
	for range 100 {
		var data struct {
			IssueLabels struct {
				Nodes []struct {
					ID, Name   string
					IsGroup    bool
					ArchivedAt *time.Time
					Team       *objectID
					Parent     *struct{ ID, Name string }
				}
				PageInfo pageInfo
			}
		}
		if err := c.query(ctx, "labels", "query($after: String) { issueLabels(first: 100, after: $after) { nodes { id name isGroup archivedAt team { id } parent { id name } } pageInfo { hasNextPage endCursor } } }", map[string]any{"after": nullable(after)}, false, &data); err != nil {
			return nil, err
		}
		if len(data.IssueLabels.Nodes) > 100 {
			return nil, failure("labels", ticket.Unavailable, "provider exceeded page bound")
		}
		for _, n := range data.IssueLabels.Nodes {
			if n.ID == "" || n.ArchivedAt != nil || (n.Team != nil && n.Team.ID != c.config.TeamID) {
				continue
			}
			l := SetupLabel{ID: n.ID, Name: n.Name, IsGroup: n.IsGroup}
			if n.Team != nil {
				l.TeamID = n.Team.ID
			}
			if n.Parent != nil {
				l.ParentID, l.ParentName = n.Parent.ID, n.Parent.Name
			}
			out = append(out, l)
		}
		if !data.IssueLabels.PageInfo.HasNextPage {
			return out, nil
		}
		if data.IssueLabels.PageInfo.EndCursor == "" || data.IssueLabels.PageInfo.EndCursor == after {
			return nil, failure("labels", ticket.Unavailable, "provider continuation missing or repeated")
		}
		after = data.IssueLabels.PageInfo.EndCursor
	}
	return nil, failure("labels", ticket.Unavailable, "label page bound exceeded")
}

// CreateLabel creates one label in the connection's team and returns its ID.
func (c *Client) CreateLabel(ctx context.Context, l NewLabel) (string, error) {
	if l.Name == "" || len(l.Color) != 6 {
		return "", failure("create label", ticket.Refused, "label name and six-digit color are required")
	}
	input := map[string]any{"name": l.Name, "color": "#" + l.Color, "description": l.Description, "teamId": c.config.TeamID}
	if l.IsGroup {
		input["isGroup"] = true
	}
	if l.ParentID != "" {
		input["parentId"] = l.ParentID
	}
	var data struct {
		IssueLabelCreate struct {
			Success    bool
			IssueLabel objectID
		}
	}
	if err := c.query(ctx, "create label", "mutation($input: IssueLabelCreateInput!) { issueLabelCreate(input: $input) { success issueLabel { id } } }", map[string]any{"input": input}, true, &data); err != nil {
		return "", err
	}
	if !data.IssueLabelCreate.Success || data.IssueLabelCreate.IssueLabel.ID == "" {
		return "", failure("create label", ticket.Refused, "label creation was not confirmed")
	}
	return data.IssueLabelCreate.IssueLabel.ID, nil
}

// VerifyForSave checks the publisher, the destination, every lifecycle state
// mapping and only the given label IDs, with the same rules as Inspect.
func (c *Client) VerifyForSave(ctx context.Context, labelIDs []string) error {
	if _, err := c.Publisher(ctx); err != nil {
		return err
	}
	if _, err := c.ContainerIdentity(ctx, c.Container()); err != nil {
		return err
	}
	if err := c.inspectLabels(ctx, labelIDs); err != nil {
		return err
	}
	return c.inspectStates(ctx)
}

// IsMappingFailure reports whether err came from a stored label or state ID lookup.
func IsMappingFailure(err error) bool {
	var f *ticket.Failure
	if !errors.As(err, &f) {
		return false
	}
	switch f.Operation {
	case "inspect mappings", "inspect label", "inspect state":
		return true
	}
	return false
}
