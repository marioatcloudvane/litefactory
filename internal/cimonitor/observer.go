// Package cimonitor observes one read-only Actions target independently of work.
package cimonitor

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/alerts"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
)

// Cadence is the interval between scheduled CI observations.
const Cadence = 5 * time.Minute

// ScanTimeout bounds a complete metadata and history scan.
const ScanTimeout = 2 * time.Minute

// RequestBudget limits HTTP calls in one complete observation.
const RequestBudget = 100

// Reader exposes only the configured Actions input, without CI write methods.
type Reader interface {
	InspectActionsRepository(context.Context, github.Repo) (github.ActionsRepository, github.ActionsTiming, error)
	InspectActionsWorkflow(context.Context, github.Repo, string) (github.ActionsWorkflow, github.ActionsTiming, error)
	ListActionsRuns(context.Context, github.Repo, int64, string, string, int) (github.ActionsRunPage, github.ActionsTiming, error)
	GetActionsRun(context.Context, github.Repo, int64) (github.ActionsRun, github.ActionsTiming, error)
}

// Observer shares incident policy and durable source position with factory alerts.
type Observer struct {
	Reader Reader
	Gate   *Gate
	Engine *alerts.Engine
	Target config.CITarget
	Origin string
	Now    func() time.Time
}

// ProbeResult is live access evidence, not accepted CI or recovery evidence.
type ProbeResult struct {
	RepositoryID  int64      `json:"repository_id"`
	WorkflowID    int64      `json:"workflow_id"`
	Ready         bool       `json:"ready"`
	Reason        string     `json:"reason"`
	Repository    string     `json:"repository"`
	Workflow      string     `json:"workflow"`
	WorkflowState string     `json:"workflow_state"`
	NextRetry     *time.Time `json:"next_retry,omitempty"`
}

func (o *Observer) now() time.Time {
	if o.Now != nil {
		return o.Now().UTC()
	}
	return time.Now().UTC()
}
func (o *Observer) origin() string {
	if o.Origin == "" {
		return "https://api.github.com"
	}
	return strings.TrimRight(o.Origin, "/")
}
func (o *Observer) configuration() string {
	return o.origin() + "|" + o.Target.Repository + "|" + o.Target.Branch + "|" + o.Target.Workflow
}
func (o *Observer) gate() *Gate {
	if o.Gate != nil {
		return o.Gate
	}
	if o.Engine == nil {
		return nil
	}
	return &Gate{Store: o.Engine.DB, Key: "ci:" + o.origin(), Now: o.Now}
}

// Reset invalidates in-flight scans and clean streaks at daemon startup.
func (o *Observer) Reset(ctx context.Context, at time.Time) error { return o.Engine.ResetCI(ctx, at) }

type scan struct {
	o          *Observer
	ctx        context.Context
	repo       github.Repo
	requests   int
	repository github.ActionsRepository
	workflow   github.ActionsWorkflow
}

func (s *scan) request(fn func(context.Context) (github.ActionsTiming, error)) error {
	if err := s.ctx.Err(); err != nil {
		return err
	}
	if s.requests >= RequestBudget {
		return fmt.Errorf("request_budget_exhausted")
	}
	s.requests++
	return s.o.gate().Do(s.ctx, fn)
}
func (s *scan) metadata() error {
	if s.o.Reader == nil {
		return fmt.Errorf("credentials_unavailable")
	}
	parts := strings.Split(s.o.Target.Repository, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("target_invalid")
	}
	s.repo = github.Repo{Owner: parts[0], Name: parts[1]}
	if err := s.request(func(ctx context.Context) (github.ActionsTiming, error) {
		v, t, e := s.o.Reader.InspectActionsRepository(ctx, s.repo)
		s.repository = v
		return t, e
	}); err != nil {
		return err
	}
	if s.repository.ID <= 0 || !strings.EqualFold(s.repository.FullName, s.o.Target.Repository) {
		return fmt.Errorf("repository_identity_changed")
	}
	if err := s.request(func(ctx context.Context) (github.ActionsTiming, error) {
		v, t, e := s.o.Reader.InspectActionsWorkflow(ctx, s.repo, s.o.Target.Workflow)
		s.workflow = v
		return t, e
	}); err != nil {
		return err
	}
	if s.workflow.ID <= 0 || s.workflow.Path != s.o.Target.Workflow {
		return fmt.Errorf("workflow_identity_changed")
	}
	if s.workflow.State != "active" {
		return fmt.Errorf("workflow_inactive")
	}
	return nil
}
func safeReason(err error) string {
	var uncertain *TransportUncertainError
	if errors.As(err, &uncertain) {
		return "transport_uncertain"
	}
	if err == nil {
		return ""
	}
	if errors.Is(err, github.ErrRateLimited) {
		return "rate_limited"
	}
	if errors.Is(err, github.ErrUnauthorized) {
		return "credentials_rejected"
	}
	if errors.Is(err, github.ErrForbidden) {
		return "actions_access_forbidden"
	}
	if errors.Is(err, github.ErrNotFound) {
		return "target_missing_or_inaccessible"
	}
	switch err.Error() {
	case "credentials_unavailable", "target_invalid", "repository_identity_changed", "workflow_identity_changed", "workflow_inactive", "request_budget_exhausted", "history_empty", "history_incomplete", "history_inconsistent", "history_capped", "run_identity_changed", "run_changed_during_scan", "source_regressed":
		return err.Error()
	}
	var deferred *DeferredError
	if errors.As(err, &deferred) {
		return "rate_limited"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "observation_timeout"
	}
	return "github_read_unavailable"
}

func (o *Observer) nextRetry(ctx context.Context, err error) *time.Time {
	if err == nil {
		return nil
	}
	var d *DeferredError
	if errors.As(err, &d) {
		v := d.Until
		return &v
	}
	g := o.gate()
	if g == nil || g.Store == nil {
		return nil
	}
	state, e := g.Store.CITransport(ctx, g.Key)
	if e == nil && state.NotBefore.After(o.now()) {
		v := state.NotBefore
		return &v
	}
	return nil
}

// Probe validates access and active metadata through the same durable transport
// gate. It has no incident, accepted-observation or recovery write capability.
func (o *Observer) Probe(ctx context.Context) (ProbeResult, error) {
	ctx, cancel := context.WithTimeout(ctx, ScanTimeout)
	defer cancel()
	s := scan{o: o, ctx: ctx}
	err := s.metadata()
	p := ProbeResult{RepositoryID: s.repository.ID, WorkflowID: s.workflow.ID, Ready: err == nil, Reason: safeReason(err), Repository: o.Target.Repository, Workflow: o.Target.Workflow, WorkflowState: s.workflow.State}
	p.NextRetry = o.nextRetry(ctx, err)
	return p, err
}
func targetKey(origin string, repository int64, branch string, workflow int64) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%s|%d", origin, repository, branch, workflow))))
}
func retain(r github.ActionsRun) *store.CIRun {
	return &store.CIRun{ID: r.ID, WorkflowID: r.WorkflowID, RunNumber: r.RunNumber, Attempt: r.Attempt, HeadSHA: r.HeadSHA, Branch: r.Branch, Status: r.Status, Conclusion: r.Conclusion, URL: r.URL, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
}
func restore(r *store.CIRun) github.ActionsRun {
	return github.ActionsRun{ID: r.ID, WorkflowID: r.WorkflowID, RunNumber: r.RunNumber, Attempt: r.Attempt, HeadSHA: r.HeadSHA, Branch: r.Branch, Status: r.Status, Conclusion: r.Conclusion, URL: r.URL, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
}
func identity(a, b github.ActionsRun) bool {
	return a.ID == b.ID && a.RunNumber == b.RunNumber && a.WorkflowID == b.WorkflowID && a.HeadSHA == b.HeadSHA && a.CreatedAt.Equal(b.CreatedAt) && a.Branch == b.Branch
}
func same(a, b github.ActionsRun) bool {
	return identity(a, b) && a.Attempt == b.Attempt && a.Status == b.Status && a.Conclusion == b.Conclusion
}
func newer(a, b github.ActionsRun) bool {
	return a.RunNumber > b.RunNumber || (a.RunNumber == b.RunNumber && a.ID == b.ID && a.Attempt > b.Attempt)
}
func decisive(r github.ActionsRun) bool {
	return r.Status == "completed" && (r.Conclusion == "success" || failed(r))
}
func failed(r github.ActionsRun) bool {
	return r.Status == "completed" && (r.Conclusion == "failure" || r.Conclusion == "timed_out" || r.Conclusion == "action_required")
}
func (s *scan) valid(r github.ActionsRun) bool {
	u, e := url.Parse(r.URL)
	origin, _ := url.Parse(s.o.origin())
	host := origin.Host
	if strings.EqualFold(origin.Hostname(), "api.github.com") {
		host = "github.com"
	}
	safeURL := e == nil && u.Scheme == "https" && strings.EqualFold(u.Host, host) && u.User == nil && u.RawQuery == "" && u.Fragment == "" && u.Path == "/"+s.o.Target.Repository+"/actions/runs/"+fmt.Sprint(r.ID)
	return r.ID > 0 && r.RunNumber > 0 && r.Attempt > 0 && r.WorkflowID == s.workflow.ID && r.Branch == s.o.Target.Branch && !r.CreatedAt.IsZero() && r.HeadSHA != "" && safeURL
}
func (s *scan) get(id int64) (github.ActionsRun, error) {
	var r github.ActionsRun
	err := s.request(func(ctx context.Context) (github.ActionsTiming, error) {
		v, t, e := s.o.Reader.GetActionsRun(ctx, s.repo, id)
		r = v
		return t, e
	})
	if err == nil && (!s.valid(r) || r.ID != id) {
		err = fmt.Errorf("run_identity_changed")
	}
	return r, err
}

// window fetches complete pages or splits capped searches into bounded creation
// windows. Inclusive boundary duplicates are checked and collapsed by the caller.
func (s *scan) window(start, end time.Time) ([]github.ActionsRun, error) {
	var out []github.ActionsRun
	total := -1
	seen := map[int64]bool{}
	for page := 1; ; page++ {
		var p github.ActionsRunPage
		created := start.UTC().Format(time.RFC3339) + ".." + end.UTC().Format(time.RFC3339)
		err := s.request(func(ctx context.Context) (github.ActionsTiming, error) {
			v, t, e := s.o.Reader.ListActionsRuns(ctx, s.repo, s.workflow.ID, s.o.Target.Branch, created, page)
			p = v
			return t, e
		})
		if err != nil {
			return nil, err
		}
		if p.Total < 0 {
			return nil, fmt.Errorf("history_incomplete")
		}
		if p.Total >= 1000 {
			if end.Sub(start) <= time.Second {
				return nil, fmt.Errorf("history_capped")
			}
			mid := start.Add(end.Sub(start) / 2).Truncate(time.Second)
			if !mid.After(start) || !mid.Before(end) {
				return nil, fmt.Errorf("history_capped")
			}
			a, e := s.window(start, mid)
			if e != nil {
				return nil, e
			}
			b, e := s.window(mid, end)
			return append(a, b...), e
		}
		if total < 0 {
			total = p.Total
		} else if total != p.Total {
			return nil, fmt.Errorf("history_inconsistent")
		}
		for _, r := range p.Runs {
			if seen[r.ID] {
				return nil, fmt.Errorf("history_inconsistent")
			}
			seen[r.ID] = true
			if !s.valid(r) || r.CreatedAt.Before(start) || r.CreatedAt.After(end) {
				return nil, fmt.Errorf("history_inconsistent")
			}
		}
		out = append(out, p.Runs...)
		if len(out) > total {
			return nil, fmt.Errorf("history_inconsistent")
		}
		if p.NextPage == 0 {
			if len(out) != total {
				return nil, fmt.Errorf("history_incomplete")
			}
			return out, nil
		}
		if p.NextPage != page+1 || len(p.Runs) == 0 {
			return nil, fmt.Errorf("history_incomplete")
		}
	}
}
func (s *scan) runs(previous store.CIObservation, cutoff time.Time) ([]github.ActionsRun, error) {
	start := time.Unix(0, 0).UTC()
	if previous.Decisive != nil {
		start = previous.Decisive.CreatedAt.Truncate(time.Second)
	}
	if previous.Failure != nil && previous.Failure.CreatedAt.Before(start) {
		start = previous.Failure.CreatedAt.Truncate(time.Second)
	}
	runs, err := s.window(start, cutoff.Truncate(time.Second))
	if err != nil {
		return nil, err
	}
	byID := map[int64]github.ActionsRun{}
	byNumber := map[int64]int64{}
	for _, r := range runs {
		if old, ok := byID[r.ID]; ok {
			if !same(old, r) {
				return nil, fmt.Errorf("history_inconsistent")
			}
			continue
		}
		if id, ok := byNumber[r.RunNumber]; ok && id != r.ID {
			return nil, fmt.Errorf("run_identity_changed")
		}
		byID[r.ID] = r
		byNumber[r.RunNumber] = r.ID
	}
	if previous.Decisive != nil {
		old := restore(previous.Decisive)
		fresh, e := s.get(old.ID)
		if e != nil {
			return nil, e
		}
		if !identity(old, fresh) || fresh.Attempt < old.Attempt {
			return nil, fmt.Errorf("source_regressed")
		}
		if fresh.Attempt == old.Attempt && !same(old, fresh) {
			return nil, fmt.Errorf("run_identity_changed")
		}
		if listed, ok := byID[old.ID]; !ok {
			return nil, fmt.Errorf("history_incomplete")
		} else if !same(listed, fresh) {
			return nil, fmt.Errorf("run_changed_during_scan")
		}
		byID[old.ID] = fresh
	}
	if previous.Failure != nil && (previous.Decisive == nil || previous.Failure.ID != previous.Decisive.ID) {
		old := restore(previous.Failure)
		fresh, e := s.get(old.ID)
		if e != nil {
			return nil, e
		}
		if !identity(old, fresh) || fresh.Attempt < old.Attempt {
			return nil, fmt.Errorf("source_regressed")
		}
		if fresh.Attempt == old.Attempt && !same(old, fresh) {
			return nil, fmt.Errorf("run_identity_changed")
		}
		listed, ok := byID[old.ID]
		if !ok {
			return nil, fmt.Errorf("history_incomplete")
		}
		if !same(listed, fresh) {
			return nil, fmt.Errorf("run_changed_during_scan")
		}
		byID[old.ID] = fresh
	}
	if len(byID) == 0 {
		return nil, fmt.Errorf("history_empty")
	}
	out := make([]github.ActionsRun, 0, len(byID))
	for _, r := range byID {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return newer(out[i], out[j]) })
	if previous.Latest != nil {
		old := restore(previous.Latest)
		if newer(old, out[0]) {
			return nil, fmt.Errorf("source_regressed")
		}
		if old.RunNumber == out[0].RunNumber && (!identity(old, out[0]) || out[0].Attempt < old.Attempt) {
			return nil, fmt.Errorf("source_regressed")
		}
	}
	fresh, e := s.get(out[0].ID)
	if e != nil {
		return nil, e
	}
	if !same(fresh, out[0]) {
		return nil, fmt.Errorf("run_changed_during_scan")
	}
	return out, nil
}

// Poll accepts one scheduled observation. Duplicate or out-of-order poll times
// cannot count twice, and a generation fence rejects scans invalidated by reset.
func (o *Observer) Poll(ctx context.Context, at time.Time) error {
	if o.Engine == nil || o.Engine.DB == nil {
		return fmt.Errorf("CI store unavailable")
	}
	l, err := o.Engine.DB.ReadAlerts(ctx)
	if err != nil {
		return err
	}
	old := l.CI
	at = at.UTC()
	if !old.LastAttempt.IsZero() && !at.After(old.LastAttempt) {
		return nil
	}
	state := old
	if old.Configuration != o.configuration() {
		state = store.CIObservation{Generation: old.Generation, Configuration: o.configuration()}
	}
	state.Origin = o.origin()
	state.Repository = o.Target.Repository
	state.Branch = o.Target.Branch
	state.Workflow = o.Target.Workflow
	state.LastAttempt = at
	state.NextDue = at.Add(Cadence)
	state.Cutoff = at
	scanCtx, cancel := context.WithTimeout(ctx, ScanTimeout)
	defer cancel()
	s := scan{o: o, ctx: scanCtx}
	readErr := s.metadata()
	if s.workflow.ID > 0 {
		state.WorkflowState = s.workflow.State
	}
	if readErr == nil {
		state.WorkflowName = s.workflow.Name
		state.WorkflowState = s.workflow.State
		key := targetKey(o.origin(), s.repository.ID, o.Target.Branch, s.workflow.ID)
		if state.Target != "" && state.Target != key {
			readErr = fmt.Errorf("workflow_identity_changed")
		} else {
			if state.Target == "" {
				retained, exists := l.CITargets[key]
				if old.Target == key {
					retained = old
					exists = true
				}
				if exists {
					state.Latest = retained.Latest
					state.Decisive = retained.Decisive
					state.Failure = retained.Failure
					state.LastSuccess = retained.LastSuccess
					state.Conclusion = retained.Conclusion
					state.Pending = retained.Pending
				}
			}
			state.Target = key
			state.RepositoryID = s.repository.ID
			state.WorkflowID = s.workflow.ID
		}
	}
	var runs []github.ActionsRun
	if readErr == nil {
		runs, readErr = s.runs(state, at)
	}
	observations := []alerts.Observation{}
	key := state.Target
	if key == "" {
		key = targetKey(o.origin()+o.configuration(), 0, o.Target.Branch, 0)
	}
	readinessKey := targetKey(o.configuration(), 0, "readiness", 0)
	observe := func(rule, condition, recovery, summary, action string) {
		subjectKey := key
		if rule == "ci_monitoring_unavailable" {
			subjectKey = readinessKey
		}
		observations = append(observations, alerts.Observation{Rule: rule, Kind: "ci", Key: subjectKey, At: at, ID: "snapshot:ci:" + rule + ":" + key + ":" + at.Format(time.RFC3339Nano), Source: "github_actions", Condition: condition, Severity: "warning", RecoveryEvidence: recovery, EvidenceAt: at, Summary: summary, Action: action})
	}
	// A missed scheduled scan is missing evidence, even if the next request
	// succeeds. Record that gap before this observation so silence cannot bridge
	// two clean confirmations or hide a readiness episode.
	if old.Configuration == o.configuration() && !old.NextDue.IsZero() && at.After(old.NextDue.Add(ScanTimeout)) {
		gapAt := old.NextDue.Add(ScanTimeout)
		observations = append(observations,
			alerts.Observation{Rule: "main_red", Kind: "ci", Key: key, At: gapAt, Condition: "unknown", Severity: "warning"},
			alerts.Observation{Rule: "ci_monitoring_unavailable", Kind: "ci", Key: readinessKey, At: gapAt, Condition: "failing", Severity: "warning", ID: "snapshot:ci:gap:" + gapAt.Format(time.RFC3339Nano), Summary: "A scheduled main CI observation was missed for " + o.Target.Repository + ".", Action: "Inspect CI monitoring and the daemon's availability in doctor."},
		)
	}
	if readErr != nil {
		state.Ready = false
		state.Reason = safeReason(readErr)
		if retry := o.nextRetry(ctx, readErr); retry != nil {
			state.NextDue = *retry
			if state.NextDue.Before(at.Add(Cadence)) {
				state.NextDue = at.Add(Cadence)
			}
		}
		observe("main_red", "unknown", "", "", "")
		observe("ci_monitoring_unavailable", "failing", "", "Main CI monitoring is unavailable for "+o.Target.Repository+" ("+state.Reason+").", "Check the configured repository, active workflow and GitHub Actions read access with doctor.")
	} else {
		state.Ready = true
		state.Reason = ""
		state.LastSuccess = at
		state.Latest = retain(runs[0])
		state.Pending = !decisive(runs[0])
		state.Conclusion = runs[0].Conclusion
		var latestDecisive *github.ActionsRun
		for i := range runs {
			if decisive(runs[i]) {
				latestDecisive = &runs[i]
				break
			}
		}
		if latestDecisive != nil {
			state.Decisive = retain(*latestDecisive)
		}
		observe("ci_monitoring_unavailable", "clean", "complete:"+key, "", "")
		switch {
		case latestDecisive != nil && failed(*latestDecisive):
			state.Failure = retain(*latestDecisive)
			observe("main_red", "failing", "", "Main CI "+o.Target.Workflow+" on "+o.Target.Repository+"/"+o.Target.Branch+" finished with "+latestDecisive.Conclusion+". "+latestDecisive.URL, "Open the workflow run, inspect the failed check and correct the cause.")
		case decisive(runs[0]) && runs[0].Conclusion == "success" && (state.Failure == nil || newer(runs[0], restore(state.Failure))):
			observe("main_red", "clean", fmt.Sprintf("run:%d:%d", runs[0].ID, runs[0].Attempt), "", "")
		default:
			observe("main_red", "unknown", "", "", "")
		}
	}
	if err := o.Engine.CommitCI(ctx, old.Generation, state, observations); err != nil {
		return err
	}
	return readErr
}
