package projectread

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/logevidence"
	"github.com/cloudvane/litefactory/internal/submission"
)

// IncidentWindow is how far back a session's incident tools look.
const IncidentWindow = 30 * 24 * time.Hour

// maxRepoCalls bounds the repository reads of one session.
const maxRepoCalls = 100

// ToolNames is the exact sorted catalog a session offers.
func ToolNames() []string {
	return []string{"incidents_list", "repo_grep", "repo_log", "repo_read", "run_digest_read"}
}

// Session serves one project's read tools to one agent invocation over MCP.
type Session struct {
	Reader
	RunsRoot   string
	Executable string
	// Prepare registers the project's credentials with the redactor before
	// anything is read; nil when they already are.
	Prepare func(ctx context.Context, projectID int64) error
}

// Start serves the tools for p, with incidents from the last IncidentWindow,
// until stop is called. attempt identifies the invocation.
func (s Session) Start(ctx context.Context, p Project, attempt string) (_ *agent.MCPLaunchSpec, stop func() error, err error) {
	if s.DB == nil || s.Redactor == nil {
		return nil, nil, errors.New("project reads unavailable: MCP is disabled or not configured")
	}
	if s.Prepare != nil {
		if err = s.Prepare(ctx, p.ID); err != nil {
			return nil, nil, err
		}
	}
	budget, err := logevidence.NewAnalystBudget(logevidence.AnalystLimits())
	if err != nil {
		return nil, nil, err
	}
	now := time.Now().UTC()
	incidents, err := logevidence.New(ctx, logevidence.Options{Store: s.DB, RunsRoot: s.RunsRoot, Redactor: s.Redactor, Budget: budget, Outcomes: true, Scope: logevidence.Scope{RepositoryIDs: []int64{p.ID}, StartedAfter: now.Add(-IncidentWindow), StartedBefore: now}})
	if err != nil {
		return nil, nil, err
	}
	var closers []func() error
	stop = func() error {
		var errs []error
		for i := len(closers) - 1; i >= 0; i-- {
			errs = append(errs, closers[i]())
		}
		return errors.Join(errs...)
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, stop())
		}
	}()
	closers = append(closers, incidents.Close)
	// Keep Unix socket paths short even when the system temp directory is long.
	directory, err := os.MkdirTemp("/tmp", "lf-pr-")
	if err != nil {
		return nil, nil, err
	}
	closers = append(closers, func() error { return os.RemoveAll(directory) })
	if directory, err = filepath.EvalSymlinks(directory); err != nil {
		return nil, nil, err
	}
	h := &handler{session: s, project: p, incidents: incidents}
	broker, err := submission.Start(ctx, submission.Options{Directory: directory, AllowTools: true, AttemptReference: attempt, MaxWorkers: 2, RequestTimeout: 30 * time.Second, Handler: h.handle})
	if err != nil {
		return nil, nil, err
	}
	closers = append(closers, broker.Close)
	descriptor, reference, err := broker.LaunchReference()
	if err != nil {
		return nil, nil, err
	}
	launch, err := agent.NewMCPLaunchSpec(s.Executable, descriptor, reference, ToolNames()...)
	if err != nil {
		return nil, nil, err
	}
	return launch, stop, nil
}

type handler struct {
	session   Session
	project   Project
	incidents *logevidence.Service
	mu        sync.Mutex
	repoCalls int
}

func (h *handler) handle(ctx context.Context, q submission.Request) (json.RawMessage, error) {
	if q.Method == "tools/list" {
		return h.catalog(ctx)
	}
	if q.Method != "tools/call" {
		return nil, errors.New("only read tools are available")
	}
	call, err := submission.DecodeToolCall(q.Payload)
	if err != nil {
		return nil, err
	}
	switch call.Name {
	case "incidents_list", "run_digest_read":
		return h.incidents.Handle(ctx, q)
	case "repo_log", "repo_grep", "repo_read":
		in, ok := repoRequest(call.Arguments)
		if !ok {
			return raw(map[string]string{"status": "unavailable", "reason": "invalid arguments"}), nil
		}
		h.mu.Lock()
		h.repoCalls++
		spent := h.repoCalls > maxRepoCalls
		h.mu.Unlock()
		if spent {
			return raw(map[string]string{"status": "unavailable", "reason": "repository read limit reached; work with what you have read"}), nil
		}
		out, _, err := h.session.Call(ctx, h.project, call.Name, in)
		return out, err
	}
	return nil, errors.New("tool unavailable")
}

// repoRequest decodes a repository tool's arguments. ok is false when they do
// not decode or name a repository, which the session fixes itself.
func repoRequest(args json.RawMessage) (in Request, ok bool) {
	if submission.DecodeJSON(args, &in) != nil || in.Repository != "" {
		return Request{}, false
	}
	return in, true
}

// catalog offers the incident tools of the reader and the repository tools.
func (h *handler) catalog(ctx context.Context) (json.RawMessage, error) {
	b, err := h.incidents.Handle(ctx, submission.Request{Method: "tools/list", RequestID: "catalog", Payload: json.RawMessage(`{}`)})
	if err != nil {
		return nil, err
	}
	var list struct {
		Tools []submission.ToolSpec `json:"tools"`
	}
	if err = json.Unmarshal(b, &list); err != nil {
		return nil, err
	}
	list.Tools = slices.DeleteFunc(list.Tools, func(t submission.ToolSpec) bool { return !slices.Contains(ToolNames(), t.Name) })
	list.Tools = append(list.Tools, RepoTools("")...)
	return raw(list), nil
}
