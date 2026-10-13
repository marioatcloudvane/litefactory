package linear

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/ticket"
)

var creationIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

const userFields = "id displayName active app archivedAt"
const issueFields = "id identifier url title description createdAt updatedAt creator { " + userFields + " } assignee { " + userFields + " } team { id } project { id } parent { id } state { id type } labels(first: 100) { nodes { id name } pageInfo { hasNextPage } }"
const commentFields = "id body url createdAt updatedAt user { " + userFields + " } issue { id team { id } project { id } }"

type user struct {
	ID          string
	DisplayName string
	Active      bool
	App         bool
	ArchivedAt  *time.Time
}
type objectID struct{ ID string }
type issue struct {
	ID, Identifier, URL, Title, Description string
	CreatedAt, UpdatedAt                    time.Time
	Creator, Assignee                       *user
	Team, Project, Parent                   *objectID
	State                                   struct{ ID, Type string }
	Labels                                  struct {
		Nodes    []struct{ ID, Name string }
		PageInfo pageInfo
	}
}
type comment struct {
	ID, Body, URL        string
	CreatedAt, UpdatedAt time.Time
	User                 *user
	Issue                *issue
}
type pageInfo struct {
	HasNextPage bool
	EndCursor   string
	observed    bool
}

func (p *pageInfo) UnmarshalJSON(raw []byte) error {
	var fields struct {
		HasNextPage *bool
		EndCursor   string
	}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	if fields.HasNextPage == nil {
		return errors.New("linear: provider pagination result missing")
	}
	p.HasNextPage, p.EndCursor, p.observed = *fields.HasNextPage, fields.EndCursor, true
	return nil
}

func (c *Client) actor(u *user) ticket.Actor {
	if u == nil {
		return ticket.Actor{}
	}
	return ticket.Actor{Identity: c.identity(u.ID), Handle: u.DisplayName, IsBot: u.App || !u.Active || u.ArchivedAt != nil}
}
func (c *Client) locator(id string) ticket.Locator {
	return ticket.Locator{Provider: Provider, Origin: c.origin, Container: c.Container(), Key: id}
}
func (c *Client) address(l ticket.Locator) error {
	if l.Validate() != nil || l.Provider != Provider || l.Origin != c.origin || l.Container != c.Container() {
		return failure("address", ticket.Refused, "ticket belongs to another connection")
	}
	return nil
}
func (c *Client) snapshot(i issue) (ticket.Snapshot, error) {
	if i.ID == "" || i.Identifier == "" || i.URL == "" || i.Team == nil || i.Project == nil || i.Team.ID != c.config.TeamID || i.Project.ID != c.config.ProjectID || i.State.ID == "" || !i.Labels.PageInfo.observed || i.Labels.PageInfo.HasNextPage {
		return ticket.Snapshot{}, failure("read", ticket.Conflict, "ticket identity, scope or bounded labels differ")
	}
	body, err := DecodeContent(i.Description)
	if err != nil {
		return ticket.Snapshot{}, failure("read", ticket.Conflict, "provider content representation differs")
	}
	raw, _ := json.Marshal([2]string{i.Title, i.Description})
	sum := sha256.Sum256(raw)
	out := ticket.Snapshot{Ref: ticket.Ref{Identity: c.identity(i.ID), Locator: c.locator(i.ID)}, DisplayKey: i.Identifier, BrowserURL: i.URL, Title: i.Title, Body: body, ProviderBody: i.Description, Representation: Representation, Author: c.actor(i.Creator), CreatedAt: i.CreatedAt, UpdatedAt: i.UpdatedAt, Revision: hex.EncodeToString(sum[:]), State: "open"}
	if i.State.Type == "completed" || i.State.Type == "canceled" {
		out.State = "closed"
		if i.State.Type == "canceled" {
			out.StateReason = "not_planned"
		} else {
			out.StateReason = "completed"
		}
	}
	if i.Assignee != nil {
		out.Assignees = []ticket.Actor{c.actor(i.Assignee)}
	}
	for _, label := range i.Labels.Nodes {
		name := label.Name
		for mode, id := range c.config.ModeLabels {
			if label.ID == id {
				name = canonicalModeLabel(mode)
			}
		}
		out.Labels = append(out.Labels, name)
	}
	return out, nil
}
func (c *Client) rawIssue(ctx context.Context, id string) (issue, error) {
	var data struct{ Issue issue }
	err := c.query(ctx, "read", "query($id: String!) { issue(id: $id) { "+issueFields+" } }", map[string]any{"id": id}, false, &data)
	if err != nil {
		return issue{}, err
	}
	if _, err := c.snapshot(data.Issue); err != nil {
		return issue{}, err
	}
	return data.Issue, nil
}

// Resolve verifies an issue locator in the configured team/project.
func (c *Client) Resolve(ctx context.Context, l ticket.Locator) (ticket.Snapshot, error) {
	if err := c.address(l); err != nil {
		return ticket.Snapshot{}, err
	}
	i, err := c.rawIssue(ctx, l.Key)
	if err != nil {
		return ticket.Snapshot{}, err
	}
	return c.snapshot(i)
}

// Read requires the locator to retain the recorded immutable identity.
func (c *Client) Read(ctx context.Context, r ticket.Ref) (ticket.Snapshot, error) {
	if r.Validate() != nil {
		return ticket.Snapshot{}, failure("read", ticket.Refused, "invalid ticket reference")
	}
	s, err := c.Resolve(ctx, r.Locator)
	if err == nil && !s.Ref.Equal(r) {
		return ticket.Snapshot{}, failure("read", ticket.Conflict, "recorded ticket identity changed")
	}
	return s, err
}

// ReplyCursor starts bounded traversal on an exact ticket.
func ReplyCursor(ref ticket.Ref) ticket.Cursor {
	return ticket.Cursor{Ticket: ref.Identity, Kind: "linear-comments/v1"}
}
func (c *Client) reply(ref ticket.Ref, item comment) (ticket.Reply, error) {
	if item.ID == "" || item.User == nil || item.User.ID == "" || item.Issue == nil || item.Issue.ID != ref.Identity.RemoteID || item.Issue.Team == nil || item.Issue.Project == nil || item.Issue.Team.ID != c.config.TeamID || item.Issue.Project.ID != c.config.ProjectID {
		return ticket.Reply{}, failure("reply", ticket.Conflict, "reply identity or scope differs")
	}
	body, err := DecodeContent(item.Body)
	if err != nil {
		return ticket.Reply{}, failure("reply", ticket.Conflict, "provider reply representation differs")
	}
	return ticket.Reply{Ticket: ref, Identity: c.identity(item.ID), Author: c.actor(item.User), Body: body, ProviderBody: item.Body, Representation: Representation, BrowserURL: item.URL, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt}, nil
}

// RepliesAfter reads a bounded reply page with a ticket-bound opaque continuation.
func (c *Client) RepliesAfter(ctx context.Context, ref ticket.Ref, cursor ticket.Cursor) (ticket.ReplyPage, error) {
	if cursor == (ticket.Cursor{}) {
		cursor = ReplyCursor(ref)
	}
	out := ticket.ReplyPage{}
	for range 100 {
		page, err := c.repliesPageCursor(ctx, ref, cursor, 100)
		if err != nil {
			return ticket.ReplyPage{}, err
		}
		out.Replies = append(out.Replies, page.Replies...)
		out.Next = page.Next
		if page.Complete {
			out.Complete = true
			return out, nil
		}
		cursor = page.Next
	}
	return ticket.ReplyPage{}, failure("replies", ticket.Unavailable, "reply page bound exceeded")
}

// RepliesPage reads one bounded reply page, including threaded issue comments.
func (c *Client) repliesPageCursor(ctx context.Context, ref ticket.Ref, cursor ticket.Cursor, limit int) (ticket.ReplyPage, error) {
	if ref.Validate() != nil {
		return ticket.ReplyPage{}, failure("read", ticket.Refused, "invalid ticket reference")
	}
	if err := c.address(ref.Locator); err != nil {
		return ticket.ReplyPage{}, err
	}
	if limit < 1 || limit > 100 || !cursor.Ticket.Equal(ref.Identity) || cursor.Kind != "linear-comments/v1" {
		return ticket.ReplyPage{}, failure("replies", ticket.Refused, "invalid reply continuation")
	}
	var data struct {
		Issue    issue
		Comments struct {
			Nodes    []comment
			PageInfo pageInfo
		}
	}
	err := c.query(ctx, "replies", "query($id: String!, $filter: CommentFilter!, $after: String, $first: Int!) { issue(id: $id) { "+issueFields+" } comments(filter: $filter, after: $after, first: $first, includeArchived: true) { nodes { "+commentFields+" } pageInfo { hasNextPage endCursor } } }", map[string]any{"id": ref.Locator.Key, "filter": map[string]any{"issue": map[string]any{"id": map[string]string{"eq": ref.Identity.RemoteID}}}, "after": nullable(cursor.Value), "first": limit}, false, &data)
	if err != nil {
		return ticket.ReplyPage{}, err
	}
	snapshot, err := c.snapshot(data.Issue)
	if err != nil {
		return ticket.ReplyPage{}, err
	}
	if !snapshot.Ref.Equal(ref) {
		return ticket.ReplyPage{}, failure("read", ticket.Conflict, "recorded ticket identity changed")
	}
	if len(data.Comments.Nodes) > limit {
		return ticket.ReplyPage{}, failure("replies", ticket.Unavailable, "provider exceeded page bound")
	}
	out := ticket.ReplyPage{Next: cursor, Complete: !data.Comments.PageInfo.HasNextPage}
	if !out.Complete {
		if data.Comments.PageInfo.EndCursor == "" || data.Comments.PageInfo.EndCursor == cursor.Value {
			return ticket.ReplyPage{}, failure("replies", ticket.Unavailable, "provider continuation missing or repeated")
		}
		out.Next.Value = data.Comments.PageInfo.EndCursor
	}
	for _, item := range data.Comments.Nodes {
		reply, err := c.reply(ref, item)
		if err != nil {
			return ticket.ReplyPage{}, err
		}
		out.Replies = append(out.Replies, reply)
	}
	return out, nil
}
func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// PublishReplyWithID uses the immutable UUID persisted by the caller's ledger.
func (c *Client) PublishReplyWithID(ctx context.Context, ref ticket.Ref, id string, body redact.Body) (ticket.Reply, error) {
	if !creationIDPattern.MatchString(id) || !body.Valid() {
		return ticket.Reply{}, failure("publish reply", ticket.Refused, "creation UUID and redacted body are required")
	}
	if _, err := c.Read(ctx, ref); err != nil {
		return ticket.Reply{}, err
	}
	var data struct {
		CommentCreate struct {
			Success bool
			Comment comment
		}
	}
	err := c.query(ctx, "publish reply", "mutation($input: CommentCreateInput!) { commentCreate(input: $input) { success comment { "+commentFields+" } } }", map[string]any{"input": map[string]any{"id": id, "issueId": ref.Identity.RemoteID, "body": EncodeContent(body.String())}}, true, &data)
	if err != nil {
		return ticket.Reply{}, err
	}
	if !data.CommentCreate.Success || data.CommentCreate.Comment.ID != id {
		return ticket.Reply{}, failure("publish reply", ticket.Uncertain, "mutation confirmation missing")
	}
	reply, err := c.reply(ref, data.CommentCreate.Comment)
	if err != nil {
		return ticket.Reply{}, failure("publish reply", ticket.Uncertain, "mutation receipt scope differs")
	}
	return reply, nil
}

// PublishReply refuses publication without a persisted creation UUID.
func (c *Client) PublishReply(ctx context.Context, ref ticket.Ref, body redact.Body) (ticket.Reply, error) {
	return ticket.Reply{}, failure("publish reply", ticket.Refused, "durable creation UUID is required")
}

// ReadReply reads an exact UUID and verifies its ticket membership.
func (c *Client) ReadReply(ctx context.Context, ref ticket.Ref, uuid string) (ticket.Reply, error) {
	id := c.identity(uuid)
	if id.Validate() != nil {
		return ticket.Reply{}, failure("read reply", ticket.Refused, "reply belongs to another connection")
	}
	if _, err := c.Read(ctx, ref); err != nil {
		return ticket.Reply{}, err
	}
	var data struct{ Comment comment }
	if err := c.query(ctx, "read reply", "query($id: String!) { comment(id: $id) { "+commentFields+" } }", map[string]string{"id": id.RemoteID}, false, &data); err != nil {
		return ticket.Reply{}, err
	}
	if data.Comment.ID != id.RemoteID {
		return ticket.Reply{}, failure("read reply", ticket.Conflict, "reply identity differs")
	}
	return c.reply(ref, data.Comment)
}

// EditReply requires a qualified matching author and redacted outbound text.
func (c *Client) EditReply(ctx context.Context, r ticket.Reply, body redact.Body) error {
	if !r.Identity.Equal(c.identity(r.Identity.RemoteID)) {
		return failure("edit reply", ticket.Refused, "reply belongs to another connection")
	}
	observed, err := c.ReadReply(ctx, r.Ticket, r.Identity.RemoteID)
	if err != nil {
		return err
	}
	if !body.Valid() || !observed.Author.Identity.Equal(r.Author.Identity) {
		return failure("edit reply", ticket.Refused, "redacted body and matching author required")
	}
	var data struct {
		CommentUpdate struct {
			Success bool
			Comment comment
		}
	}
	err = c.query(ctx, "edit reply", "mutation($id: String!, $input: CommentUpdateInput!) { commentUpdate(id: $id, input: $input) { success comment { "+commentFields+" } } }", map[string]any{"id": r.Identity.RemoteID, "input": map[string]string{"body": EncodeContent(body.String())}}, true, &data)
	if err != nil {
		return err
	}
	if !data.CommentUpdate.Success || data.CommentUpdate.Comment.ID != r.Identity.RemoteID || data.CommentUpdate.Comment.Body != EncodeContent(body.String()) {
		return failure("edit reply", ticket.Uncertain, "mutation confirmation differs")
	}
	return nil
}

// React verifies the reply membership and author before sending a reaction.
func (c *Client) React(ctx context.Context, r ticket.Reply, reaction string) error {
	if !r.Identity.Equal(c.identity(r.Identity.RemoteID)) {
		return failure("react", ticket.Refused, "reply belongs to another connection")
	}
	observed, err := c.ReadReply(ctx, r.Ticket, r.Identity.RemoteID)
	if err != nil {
		return err
	}
	if !observed.Author.Identity.Equal(r.Author.Identity) || reaction == "" {
		return failure("react", ticket.Refused, "reply author or reaction differs")
	}
	var data struct{ ReactionCreate struct{ Success bool } }
	err = c.query(ctx, "react", "mutation($input: ReactionCreateInput!) { reactionCreate(input: $input) { success } }", map[string]any{"input": map[string]string{"commentId": r.Identity.RemoteID, "emoji": reaction}}, true, &data)
	if err == nil && !data.ReactionCreate.Success {
		return failure("react", ticket.Uncertain, "mutation confirmation missing")
	}
	return err
}
func (c *Client) mutateIssue(ctx context.Context, ref ticket.Ref, input map[string]any) (ticket.Snapshot, error) {
	var data struct {
		IssueUpdate struct {
			Success bool
			Issue   issue
		}
	}
	err := c.query(ctx, "update ticket", "mutation($id: String!, $input: IssueUpdateInput!) { issueUpdate(id: $id, input: $input) { success issue { "+issueFields+" } } }", map[string]any{"id": ref.Identity.RemoteID, "input": input}, true, &data)
	if err != nil {
		return ticket.Snapshot{}, err
	}
	if !data.IssueUpdate.Success || data.IssueUpdate.Issue.ID != ref.Identity.RemoteID {
		return ticket.Snapshot{}, failure("update ticket", ticket.Uncertain, "mutation confirmation missing")
	}
	observed, err := c.Read(ctx, ref)
	if err != nil {
		return ticket.Snapshot{}, failure("update ticket", ticket.Uncertain, "authoritative readback unavailable")
	}
	return observed, nil
}

// UpdateContent checks a fresh raw revision and equivalent Markdown readback; writes are not atomic CAS.
func (c *Client) UpdateContent(ctx context.Context, ref ticket.Ref, revision string, title, body redact.Body) (ticket.Snapshot, error) {
	current, err := c.Read(ctx, ref)
	if err != nil {
		return ticket.Snapshot{}, err
	}
	if !title.Valid() || !body.Valid() || revision == "" || revision != current.Revision {
		return ticket.Snapshot{}, failure("update content", ticket.Conflict, "content base changed or redaction missing")
	}
	observed, err := c.mutateIssue(ctx, ref, map[string]any{"title": title.String(), "description": EncodeContent(body.String())})
	if err != nil {
		return ticket.Snapshot{}, err
	}
	if observed.Title != title.String() || !ContentEquivalent(body.String(), observed.Body) {
		c.logContentDifference(ctx, ref, title.String(), body.String(), observed)
		return ticket.Snapshot{}, failure("update content", ticket.Conflict, "content readback differs")
	}
	return observed, nil
}

// SetMode replaces mapped mode labels while preserving unrelated labels.
func (c *Client) SetMode(ctx context.Context, ref ticket.Ref, mode string) error {
	if _, err := c.Read(ctx, ref); err != nil {
		return err
	}
	id, ok := c.config.ModeLabels[mode]
	if !ok || id == "" {
		return failure("set mode", ticket.Refused, "mode mapping is missing")
	}
	i, err := c.rawIssue(ctx, ref.Identity.RemoteID)
	if err != nil {
		return err
	}
	labels := []string{id}
	for _, label := range i.Labels.Nodes {
		managed := false
		for _, mapped := range c.config.ModeLabels {
			if label.ID == mapped {
				managed = true
			}
		}
		if !managed {
			labels = append(labels, label.ID)
		}
	}
	s, err := c.mutateIssue(ctx, ref, map[string]any{"labelIds": labels})
	if err != nil {
		return err
	}
	if !slices.Contains(s.Labels, canonicalModeLabel(mode)) {
		return failure("set mode", ticket.Conflict, "mode readback differs")
	}
	return nil
}
func (c *Client) setState(ctx context.Context, ref ticket.Ref, state string) error {
	if _, err := c.Read(ctx, ref); err != nil {
		return err
	}
	id := c.config.States[state]
	if id == "" {
		return failure("set state", ticket.Refused, "lifecycle mapping missing")
	}
	if _, err := c.mutateIssue(ctx, ref, map[string]any{"stateId": id}); err != nil {
		return err
	}
	i, err := c.rawIssue(ctx, ref.Identity.RemoteID)
	if err != nil {
		return failure("set state", ticket.Uncertain, "readback unavailable")
	}
	if i.State.ID != id {
		return failure("set state", ticket.Conflict, "state readback differs")
	}
	return nil
}

// Close applies the configured completed lifecycle state.
func (c *Client) Close(ctx context.Context, ref ticket.Ref) error {
	return c.setState(ctx, ref, "completed")
}

// Create refuses issue creation without a persisted creation UUID.
func (c *Client) Create(ctx context.Context, container string, title, body redact.Body) (ticket.Snapshot, error) {
	return ticket.Snapshot{}, failure("create", ticket.Refused, "durable creation UUID required")
}

// CreateWithID dispatches creation with the immutable UUID retained by the effect owner.
func (c *Client) CreateWithID(ctx context.Context, container, id string, title, body redact.Body) (ticket.Snapshot, error) {
	return c.CreateChildWithID(ctx, container, id, nil, title, body)
}

// CreateChildWithID creates an issue with a verified optional direct parent.
func (c *Client) CreateChildWithID(ctx context.Context, container, id string, parent *ticket.Ref, title, body redact.Body) (ticket.Snapshot, error) {
	if container != c.Container() || !creationIDPattern.MatchString(id) || !title.Valid() || !body.Valid() {
		return ticket.Snapshot{}, failure("create", ticket.Refused, "scope, creation UUID and redaction required")
	}
	input := map[string]any{"id": id, "teamId": c.config.TeamID, "projectId": c.config.ProjectID, "title": title.String(), "description": EncodeContent(body.String())}
	if parent != nil {
		if _, err := c.Read(ctx, *parent); err != nil {
			return ticket.Snapshot{}, err
		}
		input["parentId"] = parent.Identity.RemoteID
	}
	var data struct {
		IssueCreate struct {
			Success bool
			Issue   issue
		}
	}
	err := c.query(ctx, "create", "mutation($input: IssueCreateInput!) { issueCreate(input: $input) { success issue { "+issueFields+" } } }", map[string]any{"input": input}, true, &data)
	if err != nil {
		return ticket.Snapshot{}, err
	}
	if !data.IssueCreate.Success || data.IssueCreate.Issue.ID != id {
		return ticket.Snapshot{}, failure("create", ticket.Uncertain, "mutation confirmation missing")
	}
	s, err := c.snapshot(data.IssueCreate.Issue)
	if err != nil {
		return ticket.Snapshot{}, failure("create", ticket.Uncertain, "created issue scope differs")
	}
	if parent != nil && (data.IssueCreate.Issue.Parent == nil || data.IssueCreate.Issue.Parent.ID != parent.Identity.RemoteID) {
		return ticket.Snapshot{}, failure("create", ticket.Uncertain, "created issue parent differs")
	}
	return s, nil
}

// Parent reads a direct parent through verified issue scope.
func (c *Client) Parent(ctx context.Context, child ticket.Ref) (ticket.Snapshot, error) {
	if _, err := c.Read(ctx, child); err != nil {
		return ticket.Snapshot{}, err
	}
	i, err := c.rawIssue(ctx, child.Identity.RemoteID)
	if err != nil {
		return ticket.Snapshot{}, err
	}
	if i.Parent == nil {
		return ticket.Snapshot{}, failure("parent", ticket.Refused, "ticket has no direct parent")
	}
	return c.Resolve(ctx, c.locator(i.Parent.ID))
}

// Attach refuses replacement of a different existing direct parent.
func (c *Client) Attach(ctx context.Context, parent, child ticket.Ref) error {
	if parent.Equal(child) {
		return failure("attach", ticket.Refused, "ticket cannot parent itself")
	}
	if _, err := c.Read(ctx, parent); err != nil {
		return err
	}
	if _, err := c.Read(ctx, child); err != nil {
		return err
	}
	i, err := c.rawIssue(ctx, child.Identity.RemoteID)
	if err != nil {
		return err
	}
	if i.Parent != nil {
		if i.Parent.ID == parent.Identity.RemoteID {
			return nil
		}
		return failure("attach", ticket.Conflict, "child already has another parent")
	}
	if _, err := c.mutateIssue(ctx, child, map[string]any{"parentId": parent.Identity.RemoteID}); err != nil {
		return err
	}
	p, err := c.Parent(ctx, child)
	if err != nil {
		return failure("attach", ticket.Uncertain, "parent readback unavailable")
	}
	if !p.Ref.Equal(parent) {
		return failure("attach", ticket.Conflict, "parent readback differs")
	}
	return nil
}
func (c *Client) relationship(ctx context.Context, parent, child ticket.Ref) error {
	if _, err := c.Read(ctx, parent); err != nil {
		return err
	}
	p, err := c.Parent(ctx, child)
	if err != nil {
		return err
	}
	if !p.Ref.Equal(parent) {
		return failure("relationship", ticket.Conflict, "direct parent changed")
	}
	return nil
}

// SetStoryState applies lifecycle state only after verifying direct parentage.
func (c *Client) SetStoryState(ctx context.Context, parent, child ticket.Ref, completed bool) error {
	if err := c.relationship(ctx, parent, child); err != nil {
		return err
	}
	state := "open"
	if completed {
		state = "completed"
	}
	return c.setState(ctx, child, state)
}

// RetireStory applies the separately mapped canceled state to a verified child.
func (c *Client) RetireStory(ctx context.Context, parent, child ticket.Ref) error {
	if err := c.relationship(ctx, parent, child); err != nil {
		return err
	}
	return c.setState(ctx, child, "retired")
}

// RepliesPage traverses at most the requested bounded page count using opaque cursors.
func (c *Client) RepliesPage(ctx context.Context, ref ticket.Ref, page, limit int) (ticket.ReplyPage, error) {
	if page < 1 || page > 100 {
		return ticket.ReplyPage{}, failure("reply page", ticket.Refused, "invalid page")
	}
	cursor := ReplyCursor(ref)
	for index := 1; index <= page; index++ {
		observed, err := c.repliesPageCursor(ctx, ref, cursor, limit)
		if err != nil {
			return ticket.ReplyPage{}, err
		}
		if index == page {
			return observed, nil
		}
		if observed.Complete {
			return ticket.ReplyPage{Complete: true}, nil
		}
		cursor = observed.Next
	}
	return ticket.ReplyPage{}, failure("reply page", ticket.Unavailable, "page bound exceeded")
}

// AddLabel adds only an explicitly mapped mode label, preserving unrelated labels.
func (c *Client) AddLabel(ctx context.Context, ref ticket.Ref, label string) error {
	return c.changeLabel(ctx, ref, label, true)
}

// RemoveLabel removes only an explicitly mapped mode label, preserving unrelated labels.
func (c *Client) RemoveLabel(ctx context.Context, ref ticket.Ref, label string) error {
	return c.changeLabel(ctx, ref, label, false)
}
func (c *Client) changeLabel(ctx context.Context, ref ticket.Ref, label string, add bool) error {
	id := c.config.ModeLabels[label]
	if id == "" {
		for mode, mapped := range c.config.ModeLabels {
			if canonicalModeLabel(mode) == label {
				id = mapped
			}
		}
	}
	if id == "" {
		for _, mode := range MissingModeLabels(c.config.ModeLabels) {
			if label == mode || label == canonicalModeLabel(mode) {
				return &ticket.Failure{Kind: ticket.Refused, Operation: "label", Cause: &ticket.MissingModeMapping{Mode: mode}}
			}
		}
		return failure("label", ticket.Refused, "explicit mode label mapping is required")
	}
	if _, err := c.Read(ctx, ref); err != nil {
		return err
	}
	i, err := c.rawIssue(ctx, ref.Identity.RemoteID)
	if err != nil {
		return err
	}
	labels := []string{}
	for _, existing := range i.Labels.Nodes {
		if existing.ID != id {
			labels = append(labels, existing.ID)
		}
	}
	if add {
		labels = append(labels, id)
	}
	if _, err := c.mutateIssue(ctx, ref, map[string]any{"labelIds": labels}); err != nil {
		return err
	}
	observed, err := c.rawIssue(ctx, ref.Identity.RemoteID)
	if err != nil {
		return failure("label", ticket.Uncertain, "label readback unavailable")
	}
	found := false
	for _, existing := range observed.Labels.Nodes {
		if existing.ID == id {
			found = true
		}
	}
	if found != add {
		return failure("label", ticket.Conflict, "label readback differs")
	}
	return nil
}

func canonicalModeLabel(mode string) string {
	if strings.HasPrefix(mode, "Mode: ") {
		return mode
	}
	if mode == "" {
		return ""
	}
	return "Mode: " + strings.ToUpper(mode[:1]) + mode[1:]
}

// MissingModeLabels lists unmapped canonical modes in workflow order, accepting
// the same canonical label keys as the adapter's label operations.
func MissingModeLabels(labels map[string]string) []string {
	var missing []string
	for _, mode := range []string{"research", "plan", "implement", "validate", "spike"} {
		mapped := false
		for key, id := range labels {
			if id != "" && canonicalModeLabel(key) == canonicalModeLabel(mode) {
				mapped = true
				break
			}
		}
		if !mapped {
			missing = append(missing, mode)
		}
	}
	return missing
}

// Log only the first differing comparison line, redacting before bounding it.
// Raw provider revisions remain independent of this diagnostic.
func (c *Client) logContentDifference(ctx context.Context, ref ticket.Ref, title, body string, observed ticket.Snapshot) {
	if !slog.Default().Enabled(ctx, slog.LevelDebug) {
		return
	}
	field, expected, actual := "body", comparableContent(body), comparableContent(observed.Body)
	if title != observed.Title {
		field, expected, actual = "title", title, observed.Title
	}
	left, right := strings.Split(expected, "\n"), strings.Split(actual, "\n")
	line := 0
	for line < len(left) && line < len(right) && left[line] == right[line] {
		line++
	}
	a, b := "<end>", "<end>"
	if line < len(left) {
		a = left[line]
	}
	if line < len(right) {
		b = right[line]
	}
	redactor := redact.New(redact.Options{Token: c.token})
	safeA, err := redactor.Apply(ctx, a, 256)
	if err != nil {
		return
	}
	safeB, err := redactor.Apply(ctx, b, 256)
	if err != nil {
		return
	}
	safeID, err := redactor.Apply(ctx, ref.Identity.RemoteID, 128)
	if err != nil {
		return
	}
	slog.DebugContext(ctx, "linear content readback differs", "ticket", safeID.String(), "field", field, "line", line+1, "expected", safeA.String(), "observed", safeB.String())
}
