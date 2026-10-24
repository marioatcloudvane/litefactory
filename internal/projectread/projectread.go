// Package projectread gives a read-only agent one registered project's code at
// a pinned commit and its recent incidents. The log analyst and the standards
// author share it; nothing here writes, fetches or files.
package projectread

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

// OutputBytes bounds one repository tool result before encoding.
const OutputBytes = 32 << 10

// Project is one registered repository pinned to its base branch head.
type Project struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	BaseBranch string `json:"base_branch"`
	// Commit is the base branch head the repository tools read; Gap says why
	// there is none.
	Commit string `json:"commit,omitempty"`
	Gap    string `json:"gap,omitempty"`
}

// Reader reads registered projects at their pinned commits.
type Reader struct {
	DB       *store.DB
	Git      *git.Binary
	Redactor *redact.Redactor
	Views    *Views
}

// Pin records a project's base branch head. Nothing is fetched; a missing
// clone or head is recorded as a gap, never an error.
func (r Reader) Pin(ctx context.Context, id int64) (Project, error) {
	repo, e := r.DB.GetRepo(ctx, id)
	if e != nil {
		return Project{}, e
	}
	p := Project{ID: id, Name: repo.Slug(), BaseBranch: repo.BaseBranch}
	clone, e := r.open(ctx, repo.Path)
	if e != nil {
		p.Gap = "The repository clone is unavailable."
		return p, nil //nolint:nilerr // a missing clone is recorded as a gap, never an error
	}
	for _, rev := range []string{"refs/remotes/origin/" + repo.BaseBranch, "refs/heads/" + repo.BaseBranch} {
		if p.Commit, e = clone.RevParse(ctx, rev, git.RevParseOptions{}); e == nil {
			break
		}
	}
	if p.Commit == "" {
		p.Gap = "The base branch head could not be read."
	}
	return p, nil
}

func (r Reader) open(ctx context.Context, path string) (*git.Clone, error) {
	if r.Git == nil {
		return nil, errors.New("git unavailable")
	}
	return r.Git.Open(ctx, path)
}

// Request is the decoded argument of one repository tool.
type Request struct {
	Repository   string `json:"repository,omitempty"`
	Path         string `json:"path,omitempty"`
	Query        string `json:"query,omitempty"`
	Pattern      string `json:"pattern,omitempty"`
	IgnoreCase   bool   `json:"ignore_case,omitempty"`
	Limit        int    `json:"limit,omitempty"`
	StartLine    int    `json:"start_line,omitempty"`
	Lines        int    `json:"lines,omitempty"`
	Continuation string `json:"continuation,omitempty"`
	Locator      string `json:"locator,omitempty"`
}

// RepoToolNames are the repository tools in catalog order.
var RepoToolNames = []string{"repo_log", "repo_grep", "repo_read"}

// RepoTools describes repo_log, repo_grep and repo_read. A non-empty
// repository is the JSON schema of a parameter naming one of several projects.
func RepoTools(repository string) []submission.ToolSpec {
	if repository != "" {
		repository += ","
	}
	return []submission.ToolSpec{
		{Name: "repo_log", Description: "List recent commits of the project's base branch, newest first, as of the commit pinned for you: short hash, date, subject and changed files. Narrow by a path or by words in the commit message. Read-only.", InputSchema: ObjectSchema(repository+`"path":{"type":"string","maxLength":500},"query":{"type":"string","maxLength":200},"limit":{"type":"integer","minimum":1,"maximum":50}`, ``)},
		{Name: "repo_grep", Description: "Search the project's files at the pinned commit for an extended regular expression. Returns path:line:text, at most 20 matches per file; narrow by path when there are too many. Read-only.", InputSchema: ObjectSchema(repository+`"pattern":{"type":"string","minLength":1,"maxLength":500},"path":{"type":"string","maxLength":500},"ignore_case":{"type":"boolean"}`, `"pattern"`)},
		{Name: "repo_read", Description: "Read one file, or list one directory, of the project at the pinned commit. Files return exact content separately from line decoration. Follow continuation with no selectors until retained_eof; preparing pages disclose no text. Read-only.", InputSchema: ObjectSchema(repository+`"path":{"type":"string","maxLength":500},"start_line":{"type":"integer","minimum":1},"lines":{"type":"integer","minimum":1,"maximum":400},"continuation":{"type":"string","maxLength":4096},"locator":{"type":"string","maxLength":4096}`, ``)},
	}
}

// ObjectSchema is a closed JSON object schema.
func ObjectSchema(properties string, required string) json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{` + properties + `},"required":[` + required + `],"additionalProperties":false}`)
}

// Call runs one repository tool at p's pinned commit and returns its redacted,
// bounded result. ok is false when the read was unavailable; the result then
// says why. err is only a redaction failure.
func (r Reader) Call(ctx context.Context, p Project, name string, in Request) (_ json.RawMessage, ok bool, err error) {
	if name == "repo_read" {
		return r.page(ctx, p, in)
	}
	text, e := r.read(ctx, p, name, in)
	if e != nil {
		return unavailable(e), false, nil
	}
	safe, e := r.Redactor.Apply(ctx, text, OutputBytes)
	if e != nil {
		return nil, false, e
	}
	out := map[string]any{"status": "ok", "text": safe.String()}
	if safe.Truncated() {
		out["truncated"] = true
	}
	return raw(out), true, nil
}

func (r Reader) read(ctx context.Context, p Project, name string, in Request) (string, error) {
	if p.Commit == "" || r.Git == nil {
		return "", fmt.Errorf("%s cannot be read: %s", p.Name, strings.TrimSpace(p.Gap+" no pinned commit"))
	}
	repo, e := r.DB.GetRepo(ctx, p.ID)
	if e != nil {
		return "", e
	}
	clone, e := r.open(ctx, repo.Path)
	if e != nil {
		return "", fmt.Errorf("%s cannot be opened", p.Name)
	}
	header := fmt.Sprintf("%s at %s (%s)\n", p.Name, p.Commit, p.BaseBranch)
	var text string
	switch name {
	case "repo_log":
		limit := in.Limit
		if limit == 0 {
			limit = 20
		}
		text, e = clone.PinnedLog(ctx, p.Commit, in.Path, in.Query, min(limit, 50), 256<<10)
	case "repo_grep":
		text, e = clone.PinnedGrep(ctx, p.Commit, in.Pattern, in.Path, in.IgnoreCase, 20, 256<<10)
		if e == nil && text == "" {
			text = "no match"
		}
	case "repo_read":
		var directory bool
		text, directory, e = clone.PinnedRead(ctx, p.Commit, in.Path, 1<<20)
		if e == nil && !directory {
			text = numbered(text, in.StartLine, in.Lines)
		}
	default:
		return "", fmt.Errorf("%s is not a repository tool", name)
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

func raw(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

// unavailable is the tool result for a read that could not be served. The
// failure is reported to the model as data, not returned as a Go error.
func unavailable(e error) json.RawMessage {
	return raw(map[string]string{"status": "unavailable", "reason": e.Error()})
}
