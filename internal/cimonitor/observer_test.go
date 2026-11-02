package cimonitor

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/alerts"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
)

type observerReader struct {
	repository github.ActionsRepository
	workflow   github.ActionsWorkflow
	runs       []github.ActionsRun
	err        error
	pages      func(string, int) (github.ActionsRunPage, error)
	refreshed  func(int64) (github.ActionsRun, error)
	requests   int
	timing     github.ActionsTiming
}

func (r *observerReader) InspectActionsRepository(context.Context, github.Repo) (github.ActionsRepository, github.ActionsTiming, error) {
	r.requests++
	return r.repository, r.timing, r.err
}
func (r *observerReader) InspectActionsWorkflow(context.Context, github.Repo, string) (github.ActionsWorkflow, github.ActionsTiming, error) {
	r.requests++
	return r.workflow, github.ActionsTiming{}, nil
}
func (r *observerReader) ListActionsRuns(_ context.Context, _ github.Repo, _ int64, _ string, created string, page int) (github.ActionsRunPage, github.ActionsTiming, error) {
	r.requests++
	if r.pages != nil {
		p, e := r.pages(created, page)
		return p, github.ActionsTiming{}, e
	}
	return github.ActionsRunPage{Runs: r.runs, Total: len(r.runs)}, github.ActionsTiming{}, nil
}
func (r *observerReader) GetActionsRun(_ context.Context, _ github.Repo, id int64) (github.ActionsRun, github.ActionsTiming, error) {
	r.requests++
	if r.refreshed != nil {
		v, e := r.refreshed(id)
		return v, github.ActionsTiming{}, e
	}
	for _, v := range r.runs {
		if v.ID == id {
			return v, github.ActionsTiming{}, nil
		}
	}
	return github.ActionsRun{}, github.ActionsTiming{}, errors.New("missing run")
}

func observerFixture(t *testing.T) (*Observer, *observerReader, *time.Time) {
	t.Helper()
	at := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	db, e := store.Open(t.Context(), filepath.Join(t.TempDir(), "ci.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = db.Close() })
	if e = db.Migrate(t.Context()); e != nil {
		t.Fatal(e)
	}
	settings := config.DefaultAlerts()
	settings.QuietHours.Enabled = false
	r := &observerReader{repository: github.ActionsRepository{ID: 10, FullName: settings.CI.Repository}, workflow: github.ActionsWorkflow{ID: 20, Path: settings.CI.Workflow, Name: "Check", State: "active"}}
	o := &Observer{Reader: r, Engine: &alerts.Engine{DB: db, Settings: settings}, Target: settings.CI, Now: func() time.Time { return at }}
	o.Gate = &Gate{Store: db, Key: "test", Now: func() time.Time { return at }}
	return o, r, &at
}
func observerRun(at time.Time, id, number, attempt int64, conclusion string) github.ActionsRun {
	return github.ActionsRun{ID: id, WorkflowID: 20, RunNumber: number, Attempt: attempt, HeadSHA: fmt.Sprint("sha", id), Branch: "main", Status: "completed", Conclusion: conclusion, URL: fmt.Sprintf("https://github.com/marioatcloudvane/litefactory/actions/runs/%d", id), CreatedAt: at.Add(-time.Hour), UpdatedAt: at.Add(-time.Minute)}
}
func ledger(t *testing.T, o *Observer) store.AlertLedger {
	t.Helper()
	l, e := o.Engine.DB.ReadAlerts(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	return l
}
func active(l store.AlertLedger, rule string) bool {
	for _, s := range l.Subjects {
		if s.Rule == rule && s.Active {
			return true
		}
	}
	return false
}
func cleanCount(l store.AlertLedger, rule string) int {
	for _, s := range l.Subjects {
		if s.Rule == rule && s.Active {
			return s.CleanCount
		}
	}
	return 0
}
func poll(t *testing.T, o *Observer, at *time.Time) {
	t.Helper()
	*at = at.Add(Cadence)
	if e := o.Poll(t.Context(), *at); e != nil {
		t.Fatal(e)
	}
}

func TestObserverOrderingRerunsAndRecovery(t *testing.T) {
	for _, failure := range []string{"failure", "timed_out", "action_required"} {
		t.Run(failure, func(t *testing.T) {
			o, r, at := observerFixture(t)
			bad := observerRun(*at, 1, 1, 1, failure)
			r.runs = []github.ActionsRun{bad}
			poll(t, o, at)
			if !active(ledger(t, o), "main_red") {
				t.Fatal("failure did not open")
			}
			good := bad
			good.Attempt = 2
			good.Conclusion = "success"
			r.runs = []github.ActionsRun{good}
			poll(t, o, at)
			if cleanCount(ledger(t, o), "main_red") != 1 {
				t.Fatal("successful rerun did not start recovery")
			}
			if e := o.Poll(t.Context(), *at); e != nil {
				t.Fatal(e)
			}
			if !active(ledger(t, o), "main_red") {
				t.Fatal("duplicate poll cleared")
			}
			poll(t, o, at)
			if active(ledger(t, o), "main_red") {
				t.Fatal("two successful observations did not clear")
			}
		})
	}
}
func TestObserverNondecisiveResultsCannotClear(t *testing.T) {
	for _, conclusion := range []string{"cancelled", "skipped", "neutral", "stale", "unknown", ""} {
		t.Run(conclusion, func(t *testing.T) {
			o, r, at := observerFixture(t)
			bad := observerRun(*at, 1, 1, 1, "failure")
			r.runs = []github.ActionsRun{bad}
			poll(t, o, at)
			newer := observerRun(*at, 2, 2, 1, conclusion)
			if conclusion == "" {
				newer.Status = "in_progress"
			}
			r.runs = []github.ActionsRun{newer, bad}
			poll(t, o, at)
			poll(t, o, at)
			if !active(ledger(t, o), "main_red") {
				t.Fatal("nondecisive result cleared failure")
			}
			if !ledger(t, o).CI.Ready {
				t.Fatal("complete pending observation should retain monitoring readiness")
			}
		})
	}
}
func TestObserverUnavailableKeepsFailureAndBreaksRecovery(t *testing.T) {
	o, r, at := observerFixture(t)
	bad := observerRun(*at, 1, 1, 1, "failure")
	r.runs = []github.ActionsRun{bad}
	poll(t, o, at)
	good := observerRun(*at, 2, 2, 1, "success")
	r.runs = []github.ActionsRun{good, bad}
	poll(t, o, at)
	r.err = errors.New("401 token secret")
	*at = at.Add(Cadence)
	if o.Poll(t.Context(), *at) == nil {
		t.Fatal("expected access failure")
	}
	l := ledger(t, o)
	if !active(l, "main_red") || !active(l, "ci_monitoring_unavailable") || l.CI.Ready || cleanCount(l, "main_red") != 0 {
		t.Fatal("read failure changed known CI failure or recovery")
	}
	if strings.Contains(l.CI.Reason, "secret") {
		t.Fatal("raw error exposed")
	}
	r.err = nil
	poll(t, o, at)
	if !active(ledger(t, o), "main_red") || !active(ledger(t, o), "ci_monitoring_unavailable") {
		t.Fatal("first complete observation cleared")
	}
	poll(t, o, at)
	if active(ledger(t, o), "main_red") || active(ledger(t, o), "ci_monitoring_unavailable") {
		t.Fatal("second complete observation did not recover")
	}
}
func TestObserverHealthyWorkflowBecomesDisabled(t *testing.T) {
	o, r, at := observerFixture(t)
	r.runs = []github.ActionsRun{observerRun(*at, 1, 1, 1, "success")}
	poll(t, o, at)
	r.workflow.State = "disabled_manually"
	for i := 0; i < 2; i++ {
		*at = at.Add(Cadence)
		if o.Poll(t.Context(), *at) == nil {
			t.Fatal("disabled workflow accepted")
		}
	}
	l := ledger(t, o)
	if l.CI.Ready || !active(l, "ci_monitoring_unavailable") || l.CI.Conclusion != "success" {
		t.Fatal("disabled workflow history established readiness or lost retained result")
	}
	r.workflow.State = "active"
	poll(t, o, at)
	if !active(ledger(t, o), "ci_monitoring_unavailable") {
		t.Fatal("readiness recovered after only one observation")
	}
	poll(t, o, at)
	if active(ledger(t, o), "ci_monitoring_unavailable") {
		t.Fatal("readiness failed to recover")
	}
}
func TestObserverRestartAndProbeCannotRecover(t *testing.T) {
	o, r, at := observerFixture(t)
	r.runs = []github.ActionsRun{observerRun(*at, 1, 1, 1, "failure")}
	poll(t, o, at)
	r.runs = []github.ActionsRun{observerRun(*at, 2, 2, 1, "success"), r.runs[0]}
	poll(t, o, at)
	before := ledger(t, o).CI
	probe, e := o.Probe(t.Context())
	if e != nil || !probe.Ready {
		t.Fatal(e)
	}
	after := ledger(t, o).CI
	if !reflect.DeepEqual(before, after) || cleanCount(ledger(t, o), "main_red") != 1 {
		t.Fatal("doctor changed source evidence")
	}
	if e = o.Reset(t.Context(), *at); e != nil {
		t.Fatal(e)
	}
	if cleanCount(ledger(t, o), "main_red") != 0 || !active(ledger(t, o), "main_red") {
		t.Fatal("restart lost incident or retained streak")
	}
	poll(t, o, at)
	if !active(ledger(t, o), "main_red") {
		t.Fatal("restart counted old recovery")
	}
	poll(t, o, at)
	if active(ledger(t, o), "main_red") {
		t.Fatal("recovery did not finish")
	}
}
func TestObserverInvalidPagesAndRaces(t *testing.T) {
	for _, mode := range []string{"missing_page", "inconsistent_total", "duplicate", "race", "lower_attempt", "identity", "empty"} {
		t.Run(mode, func(t *testing.T) {
			o, r, at := observerFixture(t)
			bad := observerRun(*at, 1, 1, 2, "failure")
			r.runs = []github.ActionsRun{bad}
			poll(t, o, at)
			good := observerRun(*at, 2, 2, 1, "success")
			r.runs = []github.ActionsRun{good, bad}
			switch mode {
			case "missing_page":
				r.pages = func(_ string, page int) (github.ActionsRunPage, error) {
					if page == 1 {
						return github.ActionsRunPage{Runs: []github.ActionsRun{good}, Total: 2, NextPage: 2}, nil
					}
					return github.ActionsRunPage{}, errors.New("missing")
				}
			case "inconsistent_total":
				r.pages = func(_ string, page int) (github.ActionsRunPage, error) {
					if page == 1 {
						return github.ActionsRunPage{Runs: []github.ActionsRun{good}, Total: 2, NextPage: 2}, nil
					}
					return github.ActionsRunPage{Runs: []github.ActionsRun{bad}, Total: 3}, nil
				}
			case "duplicate":
				r.runs = []github.ActionsRun{good, good, bad}
			case "race":
				r.refreshed = func(id int64) (github.ActionsRun, error) {
					if id == bad.ID {
						return bad, nil
					}
					changed := good
					changed.Attempt++
					return changed, nil
				}
			case "lower_attempt":
				bad.Attempt = 1
				r.runs = []github.ActionsRun{bad}
			case "identity":
				r.workflow.ID = 21
			case "empty":
				r.runs = nil
			}
			*at = at.Add(Cadence)
			if o.Poll(t.Context(), *at) == nil {
				t.Fatal("incomplete source accepted")
			}
			l := ledger(t, o)
			if l.CI.Ready || !active(l, "main_red") || cleanCount(l, "main_red") != 0 {
				t.Fatal("invalid source recovered CI")
			}
		})
	}
}
func TestObserverTargetReplacementDoesNotTransferRecovery(t *testing.T) {
	o, r, at := observerFixture(t)
	r.runs = []github.ActionsRun{observerRun(*at, 1, 1, 1, "failure")}
	poll(t, o, at)
	o.Target.Branch = "replacement"
	good := observerRun(*at, 2, 2, 1, "success")
	good.Branch = "replacement"
	r.runs = []github.ActionsRun{good}
	poll(t, o, at)
	poll(t, o, at)
	if !active(ledger(t, o), "main_red") {
		t.Fatal("replacement target cleared previous target incident")
	}
}
func TestObserverCompletePaginationAndCapSplitting(t *testing.T) {
	o, r, at := observerFixture(t)
	a := observerRun(*at, 1, 1, 1, "failure")
	b := observerRun(*at, 2, 2, 1, "success")
	b.CreatedAt = b.CreatedAt.Add(time.Minute)
	r.runs = []github.ActionsRun{a, b}
	capped := false
	r.pages = func(created string, page int) (github.ActionsRunPage, error) {
		parts := strings.Split(created, "..")
		start, _ := time.Parse(time.RFC3339, parts[0])
		end, _ := time.Parse(time.RFC3339, parts[1])
		if !capped {
			capped = true
			return github.ActionsRunPage{Total: 1000}, nil
		}
		var matches []github.ActionsRun
		for _, v := range r.runs {
			if !v.CreatedAt.Before(start) && !v.CreatedAt.After(end) {
				matches = append(matches, v)
			}
		}
		if len(matches) == 2 && page == 1 {
			return github.ActionsRunPage{Runs: matches[:1], Total: 2, NextPage: 2}, nil
		}
		if len(matches) == 2 && page == 2 {
			return github.ActionsRunPage{Runs: matches[1:], Total: 2}, nil
		}
		return github.ActionsRunPage{Runs: matches, Total: len(matches)}, nil
	}
	poll(t, o, at)
	if !ledger(t, o).CI.Ready || ledger(t, o).CI.Latest.ID != b.ID {
		t.Fatal("complete split pages rejected")
	}
}
func TestObserverRequestBudgetExhaustionIsUnavailable(t *testing.T) {
	o, r, at := observerFixture(t)
	r.pages = func(string, int) (github.ActionsRunPage, error) { return github.ActionsRunPage{Total: 1000}, nil }
	*at = at.Add(Cadence)
	if o.Poll(t.Context(), *at) == nil {
		t.Fatal("capped history accepted")
	}
	l := ledger(t, o)
	if l.CI.Ready || !active(l, "ci_monitoring_unavailable") || r.requests > RequestBudget {
		t.Fatal("budget failed closed incorrectly")
	}
}

func TestObserverDoctorWaitSurvivesRestartWithoutRecoveryWrites(t *testing.T) {
	o, r, at := observerFixture(t)
	r.runs = []github.ActionsRun{observerRun(*at, 1, 1, 1, "failure")}
	poll(t, o, at)
	before := ledger(t, o)
	until := at.Add(95 * time.Minute)
	r.err = github.ErrRateLimited
	r.timing = github.ActionsTiming{RateLimited: true, RetryAfter: until}
	p, e := o.Probe(t.Context())
	if e == nil || p.Ready {
		t.Fatal("rate-limited probe reported ready")
	}
	after := ledger(t, o)
	if !reflect.DeepEqual(before.CI, after.CI) || !reflect.DeepEqual(before.Subjects, after.Subjects) || !reflect.DeepEqual(before.Notifications, after.Notifications) {
		t.Fatal("diagnostic mutated observation or incident state")
	}
	requests := r.requests
	// Reconstruct the observer and gate as a standalone doctor process would.
	replacement := *o
	replacement.Gate = &Gate{Store: o.Engine.DB, Key: "test", Now: o.Now}
	o = &replacement
	r.err = nil
	r.timing = github.ActionsTiming{}
	*at = at.Add(70 * time.Minute)
	p, e = o.Probe(t.Context())
	if e == nil || p.Ready || p.NextRetry == nil || !p.NextRetry.Equal(until) {
		t.Fatal("restart shortened doctor-learned wait")
	}
	if e = o.Poll(t.Context(), *at); e == nil {
		t.Fatal("poll ignored doctor-learned wait")
	}
	if r.requests != requests {
		t.Fatal("deferred callers made network requests")
	}
	if !active(ledger(t, o), "main_red") || !active(ledger(t, o), "ci_monitoring_unavailable") {
		t.Fatal("deferred observation lost incidents")
	}
	*at = until.Add(time.Second)
	p, e = o.Probe(t.Context())
	if e != nil || !p.Ready {
		t.Fatalf("probe did not resume after full wait: %v", e)
	}
}

func TestObserverRecoveryRequiresSameCandidateAndCompleteRetainedRun(t *testing.T) {
	o, r, at := observerFixture(t)
	bad := observerRun(*at, 1, 1, 1, "failure")
	r.runs = []github.ActionsRun{bad}
	poll(t, o, at)
	good := observerRun(*at, 2, 2, 1, "success")
	r.runs = []github.ActionsRun{good, bad}
	poll(t, o, at)
	newer := observerRun(*at, 3, 3, 1, "success")
	r.runs = []github.ActionsRun{newer, good, bad}
	poll(t, o, at)
	if !active(ledger(t, o), "main_red") || cleanCount(ledger(t, o), "main_red") != 1 {
		t.Fatal("different candidate reused clean streak")
	}
	r.runs = []github.ActionsRun{newer, bad}
	r.refreshed = func(id int64) (github.ActionsRun, error) {
		switch id {
		case bad.ID:
			return bad, nil
		case good.ID:
			return good, nil
		default:
			return newer, nil
		}
	}
	*at = at.Add(Cadence)
	// Failure remains directly refreshed even after a later decisive success.
	before := r.requests
	if e := o.Poll(t.Context(), *at); e != nil {
		t.Fatal(e)
	}
	if r.requests-before < 5 {
		t.Fatal("did not refresh both failure and selected retained run")
	}
}

func TestObserverUntrustedRunURLIsUnavailable(t *testing.T) {
	for _, address := range []string{"https://attacker.invalid/run", "https://github.com/other/repo/actions/runs/1", "https://github.com/marioatcloudvane/litefactory/actions/runs/1?token=secret", "https://user:secret@github.com/marioatcloudvane/litefactory/actions/runs/1"} {
		t.Run(address, func(t *testing.T) {
			o, r, at := observerFixture(t)
			v := observerRun(*at, 1, 1, 1, "failure")
			v.URL = address
			r.runs = []github.ActionsRun{v}
			*at = at.Add(Cadence)
			if o.Poll(t.Context(), *at) == nil || ledger(t, o).CI.Ready {
				t.Fatal("untrusted run link accepted")
			}
		})
	}
}

func TestObserverReturningTargetRetainsFailureHighwater(t *testing.T) {
	for _, replacement := range []string{"branch", "path"} {
		t.Run(replacement, func(t *testing.T) {
			o, r, at := observerFixture(t)
			failedRun := observerRun(*at, 10, 10, 1, "failure")
			r.runs = []github.ActionsRun{failedRun}
			poll(t, o, at)
			original := o.Target
			oldKey := ledger(t, o).CI.Target
			if replacement == "branch" {
				o.Target.Branch = "other"
				other := observerRun(*at, 1, 1, 1, "success")
				other.Branch = "other"
				r.runs = []github.ActionsRun{other}
				poll(t, o, at)
				if _, ok := ledger(t, o).CITargets[oldKey]; !ok {
					t.Fatal("reconfiguration discarded prior source position")
				}
				o.Target = original
			} else {
				o.Target.Workflow = ".github/workflows/renamed.yml"
				r.workflow.Path = o.Target.Workflow
			}
			stale := observerRun(*at, 9, 9, 1, "success")
			r.runs = []github.ActionsRun{stale}
			r.refreshed = func(id int64) (github.ActionsRun, error) {
				if id == failedRun.ID {
					return failedRun, nil
				}
				return stale, nil
			}
			for i := 0; i < 2; i++ {
				*at = at.Add(Cadence)
				if o.Poll(t.Context(), *at) == nil {
					t.Fatal("older success accepted after target reconfiguration")
				}
			}
			l := ledger(t, o)
			if !active(l, "main_red") || l.CI.Failure == nil || l.CI.Failure.ID != failedRun.ID || l.CI.Latest == nil || l.CI.Latest.ID != failedRun.ID {
				t.Fatal("reconfiguration lost triggering failure or accepted highwater")
			}
		})
	}
}

func TestObserverPathRenameRequiresFreshRecoveryObservations(t *testing.T) {
	o, r, at := observerFixture(t)
	bad := observerRun(*at, 10, 10, 1, "failure")
	r.runs = []github.ActionsRun{bad}
	poll(t, o, at)
	good := observerRun(*at, 11, 11, 1, "success")
	r.runs = []github.ActionsRun{good, bad}
	poll(t, o, at)
	if cleanCount(ledger(t, o), "main_red") != 1 {
		t.Fatal("initial recovery did not start")
	}
	o.Target.Workflow = ".github/workflows/renamed.yml"
	r.workflow.Path = o.Target.Workflow
	poll(t, o, at)
	if !active(ledger(t, o), "main_red") || cleanCount(ledger(t, o), "main_red") != 1 {
		t.Fatal("path change inherited clean streak")
	}
	poll(t, o, at)
	if active(ledger(t, o), "main_red") {
		t.Fatal("fresh qualifying observations did not recover renamed target")
	}
}

func TestObserverMissingCadenceBreaksCleanConfirmation(t *testing.T) {
	o, r, at := observerFixture(t)
	bad := observerRun(*at, 1, 1, 1, "failure")
	r.runs = []github.ActionsRun{bad}
	poll(t, o, at)
	good := observerRun(*at, 2, 2, 1, "success")
	r.runs = []github.ActionsRun{good, bad}
	poll(t, o, at)
	if cleanCount(ledger(t, o), "main_red") != 1 {
		t.Fatal("first clean observation missing")
	}
	*at = at.Add(3 * Cadence)
	if err := o.Poll(t.Context(), *at); err != nil {
		t.Fatal(err)
	}
	l := ledger(t, o)
	if !active(l, "main_red") || cleanCount(l, "main_red") != 1 || !active(l, "ci_monitoring_unavailable") || cleanCount(l, "ci_monitoring_unavailable") != 1 {
		t.Fatal("missing scheduled evidence bridged recovery or hid readiness loss")
	}
	poll(t, o, at)
	if active(ledger(t, o), "main_red") || active(ledger(t, o), "ci_monitoring_unavailable") {
		t.Fatal("fresh complete observations failed to recover")
	}
}
