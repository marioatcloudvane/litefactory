package analystschedule

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/cloudvane/litefactory/internal/drift"
	"github.com/cloudvane/litefactory/internal/loganalysis"
	"github.com/cloudvane/litefactory/internal/logevidence"
	"github.com/cloudvane/litefactory/internal/prevention"
	"github.com/cloudvane/litefactory/internal/store"
)

func notice(p store.StandardsProposalVersion) store.AnalystProposalNotice {
	return store.AnalystProposalNotice{ProposalID: p.ProposalID, ProposalVersion: p.Version, BaseRevision: p.BaseRevision, ChangeDigest: p.ChangeDigest, ResultDigest: p.ResultDigest}
}

func (s *Service) reconcile(ctx context.Context, o store.AnalystOccurrence, now time.Time, orphan bool) (bool, error) {
	notices := []store.AnalystProposalNotice{}
	switch o.Kind {
	case "logs":
		a, e := s.DB.GetLogAnalysis(ctx, o.AnalysisID)
		if e != nil {
			return false, e
		}
		if a.State == "running" {
			if !orphan {
				return false, nil
			}
			if e = s.DB.InterruptLogAnalysis(ctx, a.ID, a.Generation, "incomplete", "scheduled invocation interrupted; no automatic relaunch"); e != nil {
				return false, e
			}
			a, e = s.DB.GetLogAnalysis(ctx, a.ID)
			if e != nil {
				return false, e
			}
		}
		o.FinishedAt = a.UpdatedAt
		o.State = a.State
		o.Disposition = a.Disposition
		o.Reason = a.Reason
		view, e := (&loganalysis.Service{DB: s.DB}).Inspect(ctx, a.ID, 0, 100)
		if e != nil {
			return false, e
		}
		o.Standards = raw(view.Standards)
		o.Coverage = raw(view.Evidence)
		var report loganalysis.Report
		if len(view.Report) > 0 {
			if e = json.Unmarshal(view.Report, &report); e != nil {
				return false, e
			}
		}
		for i := range o.Works {
			w := &o.Works[i]
			pending := false
			failed := false
			for _, row := range view.Evidence {
				var ev loganalysis.Evidence
				if e = json.Unmarshal(row.Metadata, &ev); e != nil {
					return false, e
				}
				if ev.Tool != "run_logs_read" {
					continue
				}
				var selector logevidence.ReadRequest
				if e = json.Unmarshal(ev.Selector, &selector); e != nil {
					return false, e
				}
				if selector.RunID != w.RunID {
					continue
				}
				if ev.Status != "ok" || (ev.Coverage.Incomplete && !ev.Coverage.RetentionOnly) || len(ev.Coverage.Omissions) > 0 {
					failed = true
				}
				if len(ev.Citations) > 0 {
					w.Offered = true
				}
				if ev.Coverage.EndOfSource && ev.Status == "ok" && selector.Citation == nil {
					w.Covered = true
				}
				pending = ev.Next
			}
			w.Covered = len(view.Report) > 0 && w.Covered && !pending && !failed
			for _, f := range report.Findings {
				for _, citation := range f.Citations {
					if citation.RunID == w.RunID {
						w.Cited = true
					}
				}
			}
			disposition(w)
		}
		for _, p := range view.Proposals {
			notices = append(notices, store.AnalystProposalNotice{ProposalID: p.ProposalID, ProposalVersion: p.Version, BaseRevision: p.BaseRevision, ChangeDigest: p.ChangeDigest, ResultDigest: p.ResultDigest})
		}
	case "bugs":
		a, e := s.DB.GetPreventionAnalysis(ctx, o.AnalysisID)
		if e != nil {
			return false, e
		}
		if a.Status == "collecting" || a.Status == "analyzing" {
			if !orphan {
				return false, nil
			}
			a, e = s.DB.RecoverPreventionAnalysis(ctx, a.ID)
			if e != nil {
				return false, e
			}
			if a.Status == "collecting" || a.Status == "analyzing" {
				return false, errors.New("upstream bug invocation is still active")
			}
		}
		view, e := (&prevention.Service{DB: s.DB}).Inspect(ctx, a.ID)
		if e != nil {
			return false, e
		}
		o.FinishedAt = a.UpdatedAt
		o.State = a.Status
		o.Disposition = "not_evaluated"
		o.Standards = raw(view.Standards)
		o.Coverage = raw(view.Manifest.Coverage)
		if len(a.Result) > 0 {
			o.Disposition = "insufficient_evidence"
			if len(a.Proposals) > 0 {
				o.Disposition = "proposals"
			} else if view.Result != nil {
				allNoChange := len(view.Result.Findings) > 0
				for _, f := range view.Result.Findings {
					if f.Outcome != "no_warranted_change" {
						allNoChange = false
					}
				}
				if allNoChange {
					o.Disposition = "no_warranted_change"
				}
			}
		}
		for i := range o.Works {
			w := &o.Works[i]
			gap := false
			seen := false
			for _, cv := range view.Manifest.Coverage {
				if cv.Issue == w.IssueNumber {
					seen = true
					if cv.Status != "read" && cv.Status != "excluded" {
						gap = true
					}
				}
			}
			for _, ev := range view.Manifest.Evidence {
				if ev.Issue != w.IssueNumber {
					continue
				}
				delivered := false
				for _, id := range a.DeliveredEvidence {
					if ev.ID == id {
						delivered = true
						w.Offered = true
					}
				}
				if !delivered {
					gap = true
				}
				if view.Result != nil {
					for _, f := range view.Result.Findings {
						for _, id := range append(append([]string(nil), f.BugEvidence...), f.CauseEvidence...) {
							if id == ev.ID {
								w.Cited = true
							}
						}
					}
				}
			}
			if view.Manifest.CoverageOmitted > 0 {
				gap = true
			}
			if w.IssueNumber < 1 {
				w.Reason = "terminal Bug run has no authorized issue mapping"
			}
			w.Covered = w.IssueNumber > 0 && len(a.Result) > 0 && seen && !gap && w.Offered
			disposition(w)
		}
		for _, p := range a.Proposals {
			notices = append(notices, notice(p))
		}
	case "drift":
		a, e := s.DB.GetDriftAnalysis(ctx, o.AnalysisID)
		if e != nil {
			return false, e
		}
		if a.Execution == "running" {
			if !orphan {
				return false, nil
			}
			a, e = s.DB.InterruptDriftAnalysis(ctx, a.ID, a.Generation, "scheduled invocation interrupted; no automatic relaunch")
			if e != nil {
				return false, e
			}
		}
		o.FinishedAt = a.UpdatedAt
		o.State = a.Execution
		o.Disposition = a.Disposition
		o.Reason = a.Reason
		o.Coverage = a.Coverage
		view, e := (&drift.Service{Store: s.DB}).Inspect(ctx, a.ID)
		if e != nil {
			return false, e
		}
		o.Standards = raw(view.Standards)
		var cv drift.Coverage
		if len(a.Coverage) > 0 {
			if e = json.Unmarshal(a.Coverage, &cv); e != nil {
				return false, e
			}
		}
		for i := range o.Works {
			w := &o.Works[i]
			w.Offered = len(a.Offers) > 0
			var report struct {
				Report drift.Report `json:"report"`
			}
			if len(a.Report) > 0 {
				if e = json.Unmarshal(a.Report, &report); e != nil {
					return false, e
				}
				for _, f := range report.Report.Findings {
					if len(f.Citations) > 0 {
						w.Cited = true
					}
				}
			}
			w.Covered = len(a.Report) > 0 && a.Execution == "completed" && len(cv.Gaps) == 0 && !cv.UnknownRemainder
			disposition(w)
		}
		for _, link := range a.ProposalLinks {
			p, e := s.DB.GetStandardsProposal(ctx, link.ProposalID, link.Version)
			if e != nil {
				return false, e
			}
			notices = append(notices, notice(p))
		}
	default:
		return false, errors.New("unknown saved analyst")
	}
	if o.State == "completed" || o.State == "complete" {
		o.State = "completed"
	}
	for _, w := range o.Works {
		if !w.Covered && o.State == "completed" {
			o.State = "incomplete"
		}
	}
	return true, s.DB.FinalizeAnalystOccurrence(ctx, o, notices, now)
}
func disposition(w *store.AnalystSourceWork) {
	if w.Covered {
		w.State = "covered"
		w.Reason = ""
	} else {
		w.State = "unresolved"
		if w.Reason == "" {
			w.Reason = "upstream host coverage does not establish complete examination"
		}
	}
}
