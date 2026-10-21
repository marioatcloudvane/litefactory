package github

import (
	"context"
	"fmt"
	gogithub "github.com/google/go-github/v90/github"
	"time"
)

// AnalysisFile is a pinned file observation; an empty patch is a coverage gap.
type AnalysisFile struct{ Filename, Status, Patch, SHA string }

// AnalysisFilePage is one bounded page of PR file observations.
type AnalysisFilePage struct {
	Files    []AnalysisFile
	NextPage int
}

// AnalysisReviewComment preserves review identity and pinned commit context.
type AnalysisReviewComment struct {
	ID                   int64
	Body, URL, CommitSHA string
	UpdatedAt            time.Time
}

// AnalysisReviewPage is one bounded page of PR review discussion.
type AnalysisReviewPage struct {
	Comments []AnalysisReviewComment
	NextPage int
}

// AnalysisCommit contains an exact commit and one bounded file page.
type AnalysisCommit struct {
	SHA, Message, URL string
	Files             []AnalysisFile
	NextPage          int
}

func analysisPage(page, limit int) error {
	if page < 1 || limit < 1 || limit > 20 {
		return fmt.Errorf("github: invalid analysis page: %w", ErrInvalidArgument)
	}
	return nil
}
func analysisFiles(raw []*gogithub.CommitFile) []AnalysisFile {
	out := make([]AnalysisFile, 0, len(raw))
	for _, f := range raw {
		if f != nil {
			out = append(out, AnalysisFile{Filename: f.GetFilename(), Status: f.GetStatus(), Patch: f.GetPatch(), SHA: f.GetSHA()})
		}
	}
	return out
}

// ListAnalysisPRFiles reads one finite page; the caller rechecks the PR head.
func (c *Client) ListAnalysisPRFiles(ctx context.Context, repo Repo, number, page, limit int) (AnalysisFilePage, error) {
	var out AnalysisFilePage
	if err := repo.Validate(); err != nil {
		return out, err
	}
	if err := checkIssueNumber(number); err != nil {
		return out, err
	}
	if err := analysisPage(page, limit); err != nil {
		return out, err
	}
	raw, resp, err := do(ctx, c, func(ctx context.Context) ([]*gogithub.CommitFile, *gogithub.Response, error) {
		return c.api.PullRequests.ListFiles(ctx, repo.Owner, repo.Name, number, &gogithub.ListOptions{Page: page, PerPage: limit})
	})
	if err != nil {
		return out, err
	}
	if len(raw) > limit {
		return out, ErrAnalysisLimit
	}
	out.Files = analysisFiles(raw)
	if resp != nil {
		out.NextPage = resp.NextPage
	}
	return out, nil
}

// ListAnalysisReviewComments observes PR review discussion independently of issue comments.
func (c *Client) ListAnalysisReviewComments(ctx context.Context, repo Repo, number, page, limit int) (AnalysisReviewPage, error) {
	var out AnalysisReviewPage
	if err := repo.Validate(); err != nil {
		return out, err
	}
	if err := checkIssueNumber(number); err != nil {
		return out, err
	}
	if err := analysisPage(page, limit); err != nil {
		return out, err
	}
	raw, resp, err := do(ctx, c, func(ctx context.Context) ([]*gogithub.PullRequestComment, *gogithub.Response, error) {
		return c.api.PullRequests.ListComments(ctx, repo.Owner, repo.Name, number, &gogithub.PullRequestListCommentsOptions{ListOptions: gogithub.ListOptions{Page: page, PerPage: limit}})
	})
	if err != nil {
		return out, err
	}
	if len(raw) > limit {
		return out, ErrAnalysisLimit
	}
	for _, v := range raw {
		if v != nil {
			out.Comments = append(out.Comments, AnalysisReviewComment{ID: v.GetID(), Body: v.GetBody(), URL: v.GetHTMLURL(), CommitSHA: v.GetCommitID(), UpdatedAt: v.GetUpdatedAt().Time})
		}
	}
	if resp != nil {
		out.NextPage = resp.NextPage
	}
	return out, nil
}

// GetAnalysisCommit reads an exact SHA with one bounded file page.
func (c *Client) GetAnalysisCommit(ctx context.Context, repo Repo, sha string, page, limit int) (AnalysisCommit, error) {
	var out AnalysisCommit
	if err := repo.Validate(); err != nil {
		return out, err
	}
	if !validDeliverySHA(sha) {
		return out, fmt.Errorf("github: invalid analysis commit: %w", ErrInvalidArgument)
	}
	if err := analysisPage(page, limit); err != nil {
		return out, err
	}
	raw, resp, err := do(ctx, c, func(ctx context.Context) (*gogithub.RepositoryCommit, *gogithub.Response, error) {
		return c.api.Repositories.GetCommit(ctx, repo.Owner, repo.Name, sha, &gogithub.ListOptions{Page: page, PerPage: limit})
	})
	if err != nil {
		return out, err
	}
	if raw == nil || raw.GetSHA() != sha {
		return out, fmt.Errorf("github: commit identity mismatch")
	}
	if len(raw.Files) > limit {
		return out, ErrAnalysisLimit
	}
	out = AnalysisCommit{SHA: sha, Message: raw.GetCommit().GetMessage(), URL: raw.GetHTMLURL(), Files: analysisFiles(raw.Files)}
	if resp != nil {
		out.NextPage = resp.NextPage
	}
	return out, nil
}
