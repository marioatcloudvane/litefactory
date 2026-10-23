package loganalysis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/projectread"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

func checkTools() []submission.ToolSpec {
	repository := `"repository":{"type":"string","maxLength":200,"description":"owner/name of an analysed repository; may be omitted when only one is analysed"}`
	return append([]submission.ToolSpec{
		{Name: "issues_search", Description: "Search existing issues, open and closed, by words in their title or text: in the analysed project's own ticket source (where: project) or in litefactory's upstream tracker when one is configured (where: upstream). Returns reference, title, state, labels and link. Use it before filing to find an issue that already covers a finding.", InputSchema: projectread.ObjectSchema(`"query":{"type":"string","minLength":1,"maxLength":200},"where":{"type":"string","enum":["project","upstream"]},`+repository+`,"limit":{"type":"integer","minimum":1,"maximum":20}`, `"query"`)},
	}, projectread.RepoTools(repository)...)
}

type issuesSearch struct {
	Query      string `json:"query"`
	Where      string `json:"where,omitempty"`
	Repository string `json:"repository,omitempty"`
	Limit      int    `json:"limit,omitempty"`
}

// selector is the redacted decoded request, retained in the evidence ledger.
func (h *invocation) selector(ctx context.Context, request any) (json.RawMessage, error) {
	var fields map[string]any
	if e := json.Unmarshal(raw(request), &fields); e != nil {
		return nil, e
	}
	for k, v := range fields {
		if text, ok := v.(string); ok {
			safe, e := h.service.Redactor.Apply(ctx, text, 1024)
			if e != nil {
				return nil, e
			}
			fields[k] = safe.String()
		}
	}
	return raw(fields), nil
}

// repository resolves a named, or the only, analysed repository.
func (h *invocation) repository(name string) (Repository, error) {
	if name == "" && len(h.scope.Repositories) == 1 {
		return h.scope.Repositories[0], nil
	}
	for _, r := range h.scope.Repositories {
		if strings.EqualFold(r.Name, name) {
			return r, nil
		}
	}
	return Repository{}, fmt.Errorf("repository %q is not analysed here", name)
}

// check runs one issue or repository tool. Its selector is the decoded request,
// so the ledger never stores arbitrary model output.
func (h *invocation) check(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, *Evidence, error) {
	if name == "issues_search" {
		var in issuesSearch
		if e := submission.DecodeJSON(args, &in); e != nil {
			return nil, nil, e
		}
		selector, e := h.selector(ctx, in)
		if e != nil {
			return nil, nil, e
		}
		evidence := &Evidence{Tool: name, Selector: selector, Status: "ok"}
		v, e := h.search(ctx, in)
		if e != nil {
			evidence.Status = "unavailable"
			return raw(map[string]string{"status": "unavailable", "reason": e.Error()}), evidence, nil
		}
		return v, evidence, nil
	}
	var in projectread.Request
	if e := submission.DecodeJSON(args, &in); e != nil {
		return nil, nil, e
	}
	selector, e := h.selector(ctx, in)
	if e != nil {
		return nil, nil, e
	}
	evidence := &Evidence{Tool: name, Selector: selector, Status: "ok"}
	r, e := h.repository(in.Repository)
	if e != nil {
		evidence.Status = "unavailable"
		return raw(map[string]string{"status": "unavailable", "reason": e.Error()}), evidence, nil
	}
	out, ok, e := h.service.reader().Call(ctx, projectread.Project{ID: r.ID, Name: r.Name, BaseBranch: r.BaseBranch, Commit: r.Commit, Gap: r.Gap}, name, in)
	if !ok {
		evidence.Status = "unavailable"
	}
	return out, evidence, e
}

func (h *invocation) search(ctx context.Context, in issuesSearch) (json.RawMessage, error) {
	limit := in.Limit
	if limit == 0 {
		limit = 10
	}
	if limit < 1 || limit > 20 {
		return nil, errors.New("limit is 1..20")
	}
	var tracker Tracker
	switch in.Where {
	case "", "project":
		r, e := h.repository(in.Repository)
		if e != nil {
			return nil, e
		}
		tracker = h.service.Projects[r.ID]
		if tracker == nil {
			return nil, fmt.Errorf("the ticket source of %s is unavailable", r.Name)
		}
	case "upstream":
		if h.scope.Upstream == "" {
			return nil, errors.New("no upstream tracker is configured")
		}
		if tracker = h.service.Upstream; tracker == nil {
			return nil, fmt.Errorf("the upstream tracker %s is unavailable", h.scope.Upstream)
		}
	default:
		return nil, errors.New("where is project or upstream")
	}
	hits, e := tracker.Search(ctx, in.Query, limit)
	if e != nil {
		return nil, fmt.Errorf("search in %s failed", tracker.Name())
	}
	for i := range hits {
		for _, s := range []*string{&hits[i].Title, &hits[i].Reference, &hits[i].URL} {
			safe, e := h.service.Redactor.Apply(ctx, *s, 512)
			if e != nil {
				return nil, e
			}
			*s = safe.String()
		}
	}
	return raw(map[string]any{"status": "ok", "where": tracker.Name(), "issues": hits}), nil
}

// pinRepositories records each analysed repository's base head, team and
// ticket source. Nothing is fetched; a gap is recorded, never fatal.
func (s *Service) pinRepositories(ctx context.Context, ids []int64) ([]Repository, error) {
	out := []Repository{}
	for _, id := range ids {
		repo, e := s.DB.GetRepo(ctx, id)
		if e != nil {
			return nil, e
		}
		r := Repository{ID: id, Name: repo.Slug(), BaseBranch: repo.BaseBranch}
		if t := s.Projects[id]; t != nil {
			r.Tickets = t.Name()
		}
		if b, e := s.DB.GetRepositoryTeamBinding(ctx, id); e == nil {
			r.Team, r.TeamOwned = b.Slug+"@"+b.Version, b.Kind == "local"
		} else if !errors.Is(e, store.ErrNotFound) {
			return nil, e
		}
		pinned, e := s.reader().Pin(ctx, id)
		if e != nil {
			return nil, e
		}
		r.Commit, r.Gap = pinned.Commit, pinned.Gap
		out = append(out, r)
	}
	return out, nil
}

func (s *Service) reader() projectread.Reader {
	return projectread.Reader{DB: s.DB, Git: s.Git, Redactor: s.Redactor}
}
