package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

// CommentReader grants only paged read access; the attempt supplies the target.
type CommentReader interface {
	ListCommentsPage(context.Context, github.Repo, int, int, int) (github.CommentPage, error)
}

type readComment struct {
	ID     int64     `json:"id"`
	Author string    `json:"author"`
	Time   time.Time `json:"time"`
	Body   string    `json:"body"`
}

type commentPage struct {
	Comments  []readComment `json:"comments"`
	NextPage  int           `json:"next_page"`
	Truncated bool          `json:"truncated"`
}

// Read failures are MCP tool errors, not transport/receipt failures. The shared
// adapter recognizes valid:false; the broker otherwise hides handler errors.
func (s *ResultService) readResponse(ctx context.Context, read func(context.Context, []byte) (json.RawMessage, error), payload []byte) (json.RawMessage, error) {
	data, err := read(ctx, payload)
	if err == nil {
		return data, nil
	}
	body, redactErr := s.redactor.Apply(ctx, err.Error(), 4096)
	message := "doctor read failed; error redaction unavailable"
	if redactErr == nil {
		message = body.String()
	}
	return json.Marshal(struct {
		Valid bool   `json:"valid"`
		Error string `json:"error"`
	}{false, message})
}

func (s *ResultService) query(ctx context.Context, payload []byte) (json.RawMessage, error) {
	var input struct {
		SQL string `json:"sql"`
	}
	if err := submission.DecodeJSON(payload, &input); err != nil {
		return nil, errors.New("database query requires sql only")
	}
	out, err := s.db.QueryDoctor(ctx, input.SQL)
	if err != nil {
		return nil, err
	}
	for i := range out.Columns {
		if err := s.clean(ctx, &out.Columns[i]); err != nil {
			return nil, errors.New("database output redaction failed")
		}
	}
	for _, row := range out.Rows {
		for i, v := range row {
			if v != nil {
				text, isText := v.(string)
				if !isText {
					encoded, err := json.Marshal(v)
					if err != nil {
						return nil, errors.New("database output encoding failed")
					}
					text = string(encoded)
				}
				body, err := s.redactor.Apply(ctx, text, store.DoctorQueryBytes)
				if err != nil {
					return nil, errors.New("database output redaction failed")
				}
				out.Truncated = out.Truncated || body.Truncated()
				if isText || body.String() != text {
					row[i] = body.String()
				}
			}
		}
	}
	for {
		data, err := json.Marshal(out)
		if err != nil {
			return nil, err
		}
		if len(data) <= store.DoctorQueryBytes {
			return data, nil
		}
		out.Truncated = true
		if len(out.Rows) == 0 {
			return nil, errors.New("database column metadata exceeds disclosure limit")
		}
		out.Rows = out.Rows[:len(out.Rows)-1]
	}
}

func (s *ResultService) commentsPage(ctx context.Context, payload []byte) (json.RawMessage, error) {
	var input struct {
		Page int `json:"page,omitempty"`
	}
	if err := submission.DecodeJSON(payload, &input); err != nil {
		return nil, errors.New("issue comments accepts a page only; target is fixed")
	}
	if input.Page == 0 {
		input.Page = 1
	}
	if input.Page < 1 || input.Page > 10000 {
		return nil, errors.New("invalid issue comments page")
	}
	if s.comments == nil {
		return nil, errors.New("issue comments reader unavailable")
	}
	ref, err := naming.ParseRef(s.binding.Task)
	if err != nil {
		return nil, errors.New("issue comments target unavailable")
	}
	page, err := s.comments.ListCommentsPage(ctx, github.Repo{Owner: ref.Owner, Name: ref.Repo}, ref.Issue, input.Page, 20)
	if err != nil {
		return nil, errors.New("issue comments fetch failed: unavailable, refused or rate limited")
	}
	if len(page.Comments) > 20 {
		return nil, errors.New("issue comments page exceeds bound")
	}
	out := commentPage{Comments: []readComment{}, NextPage: page.NextPage}
	budget := 60000
	for _, c := range page.Comments {
		body, err := s.redactor.Apply(ctx, c.Body, 8192)
		if err != nil {
			return nil, errors.New("issue comments redaction failed")
		}
		author, err := s.redactor.Apply(ctx, c.Author.Login, 256)
		if err != nil {
			return nil, errors.New("issue comments redaction failed")
		}
		// Keep every comment identity in this API page. Body truncation is explicit,
		// so advancing next_page cannot silently skip a comment after a byte limit.
		text := body.String()
		if len(text) > budget {
			text = ""
			out.Truncated = true
		}
		budget -= len(text)
		out.Truncated = out.Truncated || body.Truncated() || author.Truncated()
		out.Comments = append(out.Comments, readComment{ID: c.ID, Author: author.String(), Time: c.CreatedAt, Body: text})
	}
	for {
		data, err := json.Marshal(out)
		if err != nil {
			return nil, err
		}
		if len(data) <= store.DoctorQueryBytes {
			return data, nil
		}
		out.Truncated = true
		for i := len(out.Comments) - 1; i >= 0; i-- {
			if out.Comments[i].Body != "" {
				out.Comments[i].Body = ""
				break
			}
		}
	}
}
