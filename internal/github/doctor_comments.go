package github

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	gogithub "github.com/google/go-github/v90/github"
)

// RepositoryComment retains verified routing identity without reading another
// issue's history. The daemon admits only owner doctor envelopes from this page.
type RepositoryComment struct {
	IssueNumber int
	Comment     Comment
}

// RepositoryCommentPage is one bounded page of routing metadata.
type RepositoryCommentPage struct {
	Comments []RepositoryComment
	NextPage int
}

// ListRepositoryCommentsPage performs one bounded request; callers persist the
// fixed scan window and page before progressing. No retries happen here.
func (c *Client) ListRepositoryCommentsPage(ctx context.Context, repo Repo, since time.Time, page, limit int) (RepositoryCommentPage, error) {
	var out RepositoryCommentPage
	if err := repo.Validate(); err != nil {
		return out, err
	}
	if page < 1 || limit < 1 || limit > 100 {
		return out, fmt.Errorf("github: invalid repository comment page")
	}
	payloads, response, err := do(ctx, c, func(ctx context.Context) ([]*gogithub.IssueComment, *gogithub.Response, error) {
		return c.api.Issues.ListComments(ctx, repo.Owner, repo.Name, 0, &gogithub.IssueListCommentsOptions{Sort: gogithub.Ptr("updated"), Direction: gogithub.Ptr("asc"), Since: gogithub.Ptr(since.UTC()), ListOptions: gogithub.ListOptions{Page: page, PerPage: limit}})
	})
	if err != nil {
		return out, fmt.Errorf("github: doctor comment discovery: %w", err)
	}
	if len(payloads) > limit {
		return out, fmt.Errorf("github: repository comment page exceeds bound")
	}
	for _, p := range payloads {
		if p == nil {
			continue
		}
		u, err := url.Parse(p.GetIssueURL())
		if err != nil {
			return out, fmt.Errorf("github: invalid issue routing URL")
		}
		prefix := "/repos/" + repo.Owner + "/" + repo.Name + "/issues/"
		if !strings.HasPrefix(strings.ToLower(u.Path), strings.ToLower(prefix)) {
			return out, fmt.Errorf("github: foreign repository comment routing")
		}
		number, ok := issueNumberFromURL(p.GetIssueURL())
		if !ok {
			return out, fmt.Errorf("github: invalid issue comment target")
		}
		out.Comments = append(out.Comments, RepositoryComment{IssueNumber: number, Comment: convertComment(p)})
	}
	if response != nil {
		out.NextPage = response.NextPage
	}
	return out, nil
}
