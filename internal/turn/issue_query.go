package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// IssueQueryForge must exclude PRs in both operations, as github.Client does.
// The page operation performs one bounded page read, including PR-only pages.
type IssueQueryForge interface {
	ListIssuesPage(context.Context, github.Repo, string, int, int) (github.IssuePage, error)
	GetIssue(context.Context, github.Repo, int) (github.Issue, error)
}

// IssueQueryService projects minimal repository-local summaries. It owns no
// comment grants; discovery cannot enlarge an attempt's publication targets.
type IssueQueryService struct {
	receipts *ReceiptService
	forge    IssueQueryForge
	repo     github.Repo
}

// NewIssueQueryService derives repository scope from the immutable receipt binding.
func NewIssueQueryService(receipts *ReceiptService, forge IssueQueryForge) (*IssueQueryService, error) {
	if receipts == nil || forge == nil {
		return nil, errors.New("issue queries require immutable factory dependencies")
	}
	ref, err := naming.ParseRef(receipts.Binding().Task)
	if err != nil {
		return nil, fmt.Errorf("issue query binding: %w", err)
	}
	repo := github.Repo{Owner: ref.Owner, Name: ref.Repo}
	if err := repo.Validate(); err != nil {
		return nil, err
	}
	return &IssueQueryService{receipts: receipts, forge: forge, repo: repo}, nil
}

// Binding returns the immutable attempt scope used by the family dispatcher.
func (s *IssueQueryService) Binding() store.ResultBinding { return s.receipts.Binding() }

// PermissionMaterial binds the repository used for issue discovery into the catalog digest.
func (s *IssueQueryService) PermissionMaterial() ([]byte, error) { return json.Marshal(s.repo) }

// Registrations supplies bounded list and repository-local get handlers.
func (s *IssueQueryService) Registrations() []FamilyRegistration {
	return []FamilyRegistration{
		{Name: "issues_list", Owner: s, Handle: s.HandleList},
		{Name: "issues_get", Owner: s, Handle: s.HandleGet},
	}
}

// IssueListRequest selects one bounded issue page. Defaults are assigned before
// decoding, so explicit zero values are refused.
type IssueListRequest struct {
	State             string `json:"state"`
	Page              int    `json:"page"`
	PerPage           int    `json:"per_page"`
	IncludeModeLabels bool   `json:"include_mode_labels"`
}

// IssueGetRequest selects one local issue in the requested state.
type IssueGetRequest struct {
	IssueNumber       int    `json:"issue_number"`
	State             string `json:"state"`
	IncludeModeLabels bool   `json:"include_mode_labels"`
}

// IssueSummary exposes only the local number, title and optional recognized mode labels.
type IssueSummary struct {
	Number     int      `json:"number"`
	Title      string   `json:"title"`
	ModeLabels []string `json:"mode_labels,omitempty"`
}

// IssueListResponse carries one bounded page and explicit continuation metadata.
type IssueListResponse struct {
	Items    []IssueSummary `json:"items"`
	Page     int            `json:"page"`
	PerPage  int            `json:"per_page"`
	NextPage int            `json:"next_page"`
}

// HandleList validates pagination and projects one page without scanning past pull requests.
func (s *IssueQueryService) HandleList(ctx context.Context, _ string, arguments []byte) (json.RawMessage, error) {
	q := IssueListRequest{State: github.StateOpen, Page: 1, PerPage: 30}
	if err := decodeIssueQueryArguments(arguments, &q); err != nil {
		return nil, err
	}
	if !issueQueryState(q.State) || q.Page < 1 || q.PerPage < 1 || q.PerPage > 100 {
		return nil, errors.New("invalid issue state or page bounds")
	}
	page, err := s.forge.ListIssuesPage(ctx, s.repo, q.State, q.Page, q.PerPage)
	if err != nil {
		return nil, fmt.Errorf("query issue page: %w", err)
	}
	if len(page.Items) > q.PerPage || page.NextPage < 0 || page.NextPage != 0 && page.NextPage <= q.Page {
		return nil, errors.New("issue page exceeds bounds or has invalid pagination")
	}
	result := IssueListResponse{Items: []IssueSummary{}, Page: q.Page, PerPage: q.PerPage, NextPage: page.NextPage}
	for _, issue := range page.Items {
		if issue.State != q.State {
			continue
		}
		summary, err := projectIssueSummary(issue, q.IncludeModeLabels)
		if err != nil {
			return nil, err
		}
		result.Items = append(result.Items, summary)
	}
	return json.Marshal(result)
}

// HandleGet validates the local target and state before projecting a minimal summary.
func (s *IssueQueryService) HandleGet(ctx context.Context, _ string, arguments []byte) (json.RawMessage, error) {
	q := IssueGetRequest{State: github.StateOpen}
	if err := decodeIssueQueryArguments(arguments, &q); err != nil {
		return nil, err
	}
	if !issueQueryState(q.State) || q.IssueNumber < 1 {
		return nil, errors.New("invalid issue state or local number")
	}
	issue, err := s.forge.GetIssue(ctx, s.repo, q.IssueNumber)
	if err != nil {
		return nil, fmt.Errorf("query issue: %w", err)
	}
	if issue.Number != q.IssueNumber || issue.State != q.State {
		return nil, errors.New("issue target or state mismatch")
	}
	summary, err := projectIssueSummary(issue, q.IncludeModeLabels)
	if err != nil {
		return nil, err
	}
	return json.Marshal(summary)
}

func issueQueryState(state string) bool { return state == github.StateOpen || state == "closed" }

func projectIssueSummary(issue github.Issue, labels bool) (IssueSummary, error) {
	// Reject oversized upstream titles rather than returning unbounded output or
	// silently presenting a truncated title as the issue's complete title.
	if issue.Number < 1 || !utf8.ValidString(issue.Title) || len(issue.Title) > github.IssueTitleSizeLimit {
		return IssueSummary{}, errors.New("invalid or oversized issue summary")
	}
	result := IssueSummary{Number: issue.Number, Title: issue.Title}
	if labels {
		for _, label := range issue.Labels {
			if mode, ok := agent.ParseModeLabel(label); ok && !slices.Contains(result.ModeLabels, mode.Label()) {
				result.ModeLabels = append(result.ModeLabels, mode.Label())
			}
		}
		slices.Sort(result.ModeLabels)
	}
	return result, nil
}

// JSON null is not a state, page number or boolean. Reject it explicitly because
// encoding/json otherwise leaves a defaulted scalar unchanged.
func decodeIssueQueryArguments(arguments []byte, out interface{}) error {
	if err := decodeMCPArguments(arguments, out); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(arguments, &fields); err != nil {
		return err
	}
	if fields == nil {
		return errors.New("issue query arguments must be an object")
	}
	for _, value := range fields {
		if string(value) == "null" {
			return errors.New("issue query fields cannot be null")
		}
	}
	return nil
}
