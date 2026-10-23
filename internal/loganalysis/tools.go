package loganalysis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

// toolOutputBytes bounds one repository or issue tool result before encoding.
const toolOutputBytes = 32 << 10

func checkTools() []submission.ToolSpec {
	repository := `"repository":{"type":"string","maxLength":200,"description":"owner/name of an analysed repository; may be omitted when only one is analysed"}`
	return []submission.ToolSpec{
		{Name: "issues_search", Description: "Search existing issues, open and closed, by words in their title or text: in the analysed project's own ticket source (where: project) or in litefactory's upstream tracker when one is configured (where: upstream). Returns reference, title, state, labels and link. Use it before filing to find an issue that already covers a finding.", InputSchema: objectSchema(`"query":{"type":"string","minLength":1,"maxLength":200},"where":{"type":"string","enum":["project","upstream"]},`+repository+`,"limit":{"type":"integer","minimum":1,"maximum":20}`, `"query"`)},
		{Name: "repo_log", Description: "List recent commits of an analysed repository's base branch, newest first, as of the commit pinned when this analysis started: short hash, date, subject and changed files. Narrow by a path or by words in the commit message. Use it to see whether a cause was already fixed. Read-only.", InputSchema: objectSchema(repository+`,"path":{"type":"string","maxLength":500},"query":{"type":"string","maxLength":200},"limit":{"type":"integer","minimum":1,"maximum":50}`, ``)},
		{Name: "repo_grep", Description: "Search the files of an analysed repository at the pinned base-branch commit for an extended regular expression. Returns path:line:text, at most 20 matches per file; narrow by path when there are too many. Read-only.", InputSchema: objectSchema(repository+`,"pattern":{"type":"string","minLength":1,"maxLength":500},"path":{"type":"string","maxLength":500},"ignore_case":{"type":"boolean"}`, `"pattern"`)},
		{Name: "repo_read", Description: "Read one file, or list one directory, of an analysed repository at the pinned base-branch commit. Files come back with line numbers; use start_line and lines for long files. Read-only.", InputSchema: objectSchema(repository+`,"path":{"type":"string","maxLength":500},"start_line":{"type":"integer","minimum":1},"lines":{"type":"integer","minimum":1,"maximum":400}`, ``)},
	}
}

type issuesSearch struct {
	Query      string `json:"query"`
	Where      string `json:"where,omitempty"`
	Repository string `json:"repository,omitempty"`
	Limit      int    `json:"limit,omitempty"`
}

type repoRequest struct {
	Repository string `json:"repository,omitempty"`
	Path       string `json:"path,omitempty"`
	Query      string `json:"query,omitempty"`
	Pattern    string `json:"pattern,omitempty"`
	IgnoreCase bool   `json:"ignore_case,omitempty"`
	Limit      int    `json:"limit,omitempty"`
	StartLine  int    `json:"start_line,omitempty"`
	Lines      int    `json:"lines,omitempty"`
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
	var in repoRequest
	if e := submission.DecodeJSON(args, &in); e != nil {
		return nil, nil, e
	}
	selector, e := h.selector(ctx, in)
	if e != nil {
		return nil, nil, e
	}
	evidence := &Evidence{Tool: name, Selector: selector, Status: "ok"}
	text, e := h.readRepository(ctx, name, in)
	if e != nil {
		evidence.Status = "unavailable"
		return raw(map[string]string{"status": "unavailable", "reason": e.Error()}), evidence, nil
	}
	safe, e := h.service.Redactor.Apply(ctx, text, toolOutputBytes)
	if e != nil {
		return nil, nil, e
	}
	out := map[string]any{"status": "ok", "text": safe.String()}
	if safe.Truncated() {
		out["truncated"] = true
	}
	return raw(out), evidence, nil
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

func (h *invocation) readRepository(ctx context.Context, name string, in repoRequest) (string, error) {
	r, e := h.repository(in.Repository)
	if e != nil {
		return "", e
	}
	if r.Commit == "" || h.service.Git == nil {
		return "", fmt.Errorf("%s cannot be read: %s", r.Name, strings.TrimSpace(r.Gap+" no pinned commit"))
	}
	repo, e := h.service.DB.GetRepo(ctx, r.ID)
	if e != nil {
		return "", e
	}
	clone, e := h.service.Git.Open(ctx, repo.Path)
	if e != nil {
		return "", fmt.Errorf("%s cannot be opened", r.Name)
	}
	header := fmt.Sprintf("%s at %s (%s)\n", r.Name, r.Commit, r.BaseBranch)
	var text string
	switch name {
	case "repo_log":
		limit := in.Limit
		if limit == 0 {
			limit = 20
		}
		text, e = clone.PinnedLog(ctx, r.Commit, in.Path, in.Query, min(limit, 50), 256<<10)
	case "repo_grep":
		text, e = clone.PinnedGrep(ctx, r.Commit, in.Pattern, in.Path, in.IgnoreCase, 20, 256<<10)
		if e == nil && text == "" {
			text = "no match"
		}
	case "repo_read":
		var directory bool
		text, directory, e = clone.PinnedRead(ctx, r.Commit, in.Path, 1<<20)
		if e == nil && !directory {
			text = numbered(text, in.StartLine, in.Lines)
		}
	}
	if errors.Is(e, git.ErrPinnedReadTooLarge) {
		return "", errors.New("the result is too large; narrow the path, pattern or line range")
	}
	if errors.Is(e, git.ErrRevisionNotFound) {
		return "", fmt.Errorf("%q does not exist at the pinned commit", in.Path)
	}
	if e != nil {
		return "", errors.New("the repository read failed; check the arguments")
	}
	return header + text, nil
}

// numbered returns a window of lines prefixed with their line numbers.
func numbered(text string, start, count int) string {
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if start < 1 {
		start = 1
	}
	if count < 1 {
		count = 400
	}
	var b strings.Builder
	end := min(len(lines), start-1+count)
	for i := start - 1; i < end; i++ {
		fmt.Fprintf(&b, "%d\t%s\n", i+1, lines[i])
	}
	if end < len(lines) {
		fmt.Fprintf(&b, "... %d more lines; continue with start_line %d\n", len(lines)-end, end+1)
	}
	return b.String()
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
		switch clone, e := s.openClone(ctx, repo.Path); {
		case e != nil:
			r.Gap = "The repository clone is unavailable."
		default:
			for _, rev := range []string{"refs/remotes/origin/" + repo.BaseBranch, "refs/heads/" + repo.BaseBranch} {
				if r.Commit, e = clone.RevParse(ctx, rev, git.RevParseOptions{}); e == nil {
					break
				}
			}
			if r.Commit == "" {
				r.Gap = "The base branch head could not be read."
			}
		}
		out = append(out, r)
	}
	return out, nil
}

func (s *Service) openClone(ctx context.Context, path string) (*git.Clone, error) {
	if s.Git == nil {
		return nil, errors.New("git unavailable")
	}
	return s.Git.Open(ctx, path)
}
