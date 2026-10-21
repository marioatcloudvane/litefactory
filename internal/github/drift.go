package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	gogithub "github.com/google/go-github/v90/github"
)

// DriftDiscussion is one bounded observation of an issue or pull request body.
type DriftDiscussion struct {
	Issue Issue
	Bytes int
}

// DriftComments is one bounded issue-comment page, excluding PR review threads.
type DriftComments struct {
	Comments        []Comment
	NextPage, Bytes int
}

type driftResponseKey struct{}
type driftResponseBound struct{ limit, bytes int }

// boundDriftResponse covers successful and error envelopes before SDK decoding.
func boundDriftResponse(resp *http.Response, bound *driftResponseBound) (*http.Response, error) {
	if resp == nil || resp.Body == nil {
		return resp, nil
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, int64(bound.limit)+1))
	closeErr := resp.Body.Close()
	bound.bytes = min(len(data), bound.limit)
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if len(data) > bound.limit {
		return nil, errors.New("discussion response exceeds byte allowance")
	}
	resp.Body = io.NopCloser(bytes.NewReader(data))
	return resp, nil
}

type driftBody struct {
	bytes.Buffer
	limit int
}

func (b *driftBody) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		return 0, errors.New("discussion response exceeds byte allowance")
	}
	return b.Buffer.Write(p)
}

func (c *Client) driftRead(ctx context.Context, route string, limit int) ([]byte, *gogithub.Response, error) {
	if limit < 1 || limit > 8<<20 {
		return nil, nil, errors.New("invalid discussion response bound")
	}
	bound := &driftResponseBound{limit: limit}
	ctx = context.WithValue(ctx, driftResponseKey{}, bound)
	req, err := c.api.NewRequest(ctx, http.MethodGet, route, nil)
	if err != nil {
		return nil, nil, err
	}
	body := &driftBody{limit: limit}
	_, response, err := do(ctx, c, func(ctx context.Context) (struct{}, *gogithub.Response, error) {
		resp, e := c.api.Do(req.WithContext(ctx), body)
		return struct{}{}, resp, e
	})
	if err != nil && bound.bytes > body.Len() {
		return make([]byte, bound.bytes), response, err
	}
	return body.Bytes(), response, err
}

// ReadDriftDiscussion permits an explicit PR selector without changing GetIssue's
// issue-only contract. The HTTP envelope is bounded before JSON decoding.
func (c *Client) ReadDriftDiscussion(ctx context.Context, repo Repo, number, limit int) (DriftDiscussion, error) {
	var out DriftDiscussion
	if err := repo.Validate(); err != nil {
		return out, err
	}
	if err := checkIssueNumber(number); err != nil {
		return out, err
	}
	data, _, err := c.driftRead(ctx, fmt.Sprintf("repos/%s/%s/issues/%d", repo.Owner, repo.Name, number), limit)
	out.Bytes = len(data)
	if err != nil {
		return out, err
	}
	var payload gogithub.Issue
	if err = json.Unmarshal(data, &payload); err != nil {
		return out, err
	}
	if payload.GetNumber() != number {
		return out, errors.New("discussion response does not match selected issue")
	}
	out.Issue = convertIssue(&payload)
	return out, nil
}

// ReadDriftComments performs exactly one request with no automatic retry.
func (c *Client) ReadDriftComments(ctx context.Context, repo Repo, number, page, count, limit int) (DriftComments, error) {
	var out DriftComments
	if err := repo.Validate(); err != nil {
		return out, err
	}
	if err := checkIssueNumber(number); err != nil {
		return out, err
	}
	if page < 1 || count < 1 || count > 100 {
		return out, errors.New("invalid drift comment page")
	}
	data, response, err := c.driftRead(ctx, fmt.Sprintf("repos/%s/%s/issues/%d/comments?page=%d&per_page=%d", repo.Owner, repo.Name, number, page, count), limit)
	out.Bytes = len(data)
	if err != nil {
		return out, err
	}
	var payload []*gogithub.IssueComment
	if err = json.Unmarshal(data, &payload); err != nil {
		return out, err
	}
	if len(payload) > count {
		return out, errors.New("discussion page exceeds item allowance")
	}
	for _, comment := range payload {
		if comment != nil {
			out.Comments = append(out.Comments, convertComment(comment))
		}
	}
	if response != nil {
		out.NextPage = response.NextPage
	}
	return out, nil
}
