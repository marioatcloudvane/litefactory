package analystschedule

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/drift"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/loganalysis"
	"github.com/cloudvane/litefactory/internal/logevidence"
	"github.com/cloudvane/litefactory/internal/prevention"
	"github.com/cloudvane/litefactory/internal/store"
)

func scheduleServiceFixture(t *testing.T) (*Service, store.AnalystOccurrence, time.Time) {
	t.Helper()
	return typedScheduleFixture(t, Config{Name: "Review", Kind: "logs", RunLimit: 1}, t.TempDir(), []store.AnalystSourceWork{{Kind: "run", SourceKey: "run:42", RunID: 42, State: "selected"}})
}

func typedScheduleFixture(t *testing.T, c Config, clonePath string, works []store.AnalystSourceWork) (*Service, store.AnalystOccurrence, time.Time) {
	t.Helper()
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "schedule.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: clonePath, Forge: store.ForgeGitHub, Owner: "fixture", Name: "scheduled", RemoteURL: "git@github.com:fixture/scheduled.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	svc := &Service{DB: db}
	c.RepositoryID = repo.ID
	a, err := svc.Create(t.Context(), "service-fixture", "github:71", c, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Enable(t.Context(), a.ID, a.Version, "github:71", now)
	if err != nil {
		t.Fatal(err)
	}
	o, ok, err := svc.Reserve(t.Context(), now)
	if err != nil || !ok {
		t.Fatalf("reserve=%v %v", ok, err)
	}
	if len(works) == 0 {
		return svc, o, now
	}
	o, err = db.SetAnalystSources(t.Context(), o.ID, o.Fence, works, now)
	if err != nil {
		t.Fatal(err)
	}
	return svc, o, now
}

func allocateScheduledLog(t *testing.T, svc *Service, o store.AnalystOccurrence, now time.Time) store.LogAnalysis {
	t.Helper()
	a, err := svc.DB.CreateLogAnalysis(store.WithAnalystOccurrence(t.Context(), o.ID, o.Fence, now), store.LogAnalysis{ID: "scheduled-fixture-analysis", StartKey: o.StartKey, RequestDigest: "fixture-request", GrantDigest: "fixture-grant", Scope: raw(loganalysis.Scope{}), Limits: raw(loganalysis.Limits{})})
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.DB.ReserveLogAnalysisBudget(t.Context(), a.ID, a.Generation, 7, 120, 40, 256<<10); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestRecoverCommittedLogReportBeforeNewWork(t *testing.T) {
	svc, o, now := scheduleServiceFixture(t)
	a := allocateScheduledLog(t, svc, o, now)
	_, err := svc.DB.RecordLogAnalysisEvidence(t.Context(), store.LogAnalysisEvidence{AnalysisID: a.ID, Generation: a.Generation, CallKey: "source-eof", Metadata: raw(loganalysis.Evidence{Tool: "run_logs_read", Selector: raw(logevidence.ReadRequest{RunID: 42}), Status: "ok", Coverage: logevidence.Coverage{EndOfSource: true}})})
	if err != nil {
		t.Fatal(err)
	}
	candidate := store.LogAnalysisCandidate{Key: "practice", Proposal: store.StandardsProposalVersion{ProposalID: "recovered-proposal", SchemaVersion: 1, Author: "log analyst", ChangeDigest: "original-change", ResultDigest: "original-result", Operations: []store.StandardsOperation{{Kind: "standard_add", Standard: &store.StandardsStandard{ID: "checks", Rule: "MUST Checks", Description: "Run relevant checks.", Evidence: "Evidence.", Scope: "All repositories.", Check: "manual", AppliesTo: []string{"story-worker"}}}}}}
	original, err := svc.DB.FinalizeLogAnalysis(t.Context(), store.LogAnalysisFinalization{AnalysisID: a.ID, Generation: a.Generation, RequestID: "committed-report", RequestDigest: "committed-digest", Report: raw(loganalysis.Report{Summary: "One cited practice supports scoped guidance."}), State: "completed", Disposition: "proposals", Candidates: []store.LogAnalysisCandidate{candidate}})
	if err != nil {
		t.Fatal(err)
	}
	// Receipt committed, but the process stopped before scheduler bookkeeping.
	notices, err := svc.DB.ListAnalystProposalNotices(t.Context(), o.ScheduleID, 0, 10)
	if err != nil || len(notices) != 0 {
		t.Fatalf("notice existed before reconciliation: %+v %v", notices, err)
	}
	reopen := func() {
		t.Helper()
		path := svc.DB.Path()
		if err := svc.DB.Close(); err != nil {
			t.Fatal(err)
		}
		reopened, err := store.Open(t.Context(), path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = reopened.Close() })
		svc.DB = reopened
	}
	reopen()
	recoveredAt := now.Add(leaseDuration + time.Minute)
	if err = svc.Recover(t.Context(), recoveredAt); err != nil {
		t.Fatal(err)
	}
	if err = svc.Recover(t.Context(), recoveredAt.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	current, err := svc.DB.GetAnalystOccurrence(t.Context(), o.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.State != "completed" || current.Disposition != "proposals" || current.AnalysisID != a.ID || len(current.Works) != 1 || !current.Works[0].Covered {
		t.Fatalf("recovered occurrence=%+v", current)
	}
	retained, err := svc.DB.GetLogAnalysisReceipt(t.Context(), a.ID, original.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	if retained.RequestDigest != original.RequestDigest || retained.AnalysisID != original.AnalysisID {
		t.Fatalf("receipt changed: %+v", retained)
	}
	analyses, err := svc.DB.ListLogAnalyses(t.Context(), "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(analyses) != 1 || analyses[0].Calls != 7 || analyses[0].OutboundBytes != 120 {
		t.Fatalf("recovery relaunched or reset budget: %+v", analyses)
	}
	notices, err = svc.DB.ListAnalystProposalNotices(t.Context(), o.ScheduleID, 0, 10)
	if err != nil || len(notices) != 1 {
		t.Fatalf("crash recovery notice=%+v %v", notices, err)
	}
	originalNotice := notices[0]
	link := original.Proposals[0]
	if originalNotice.ID < 1 || originalNotice.ScheduleID != o.ScheduleID || originalNotice.OccurrenceID != o.ID || originalNotice.AnalysisID != a.ID || originalNotice.ProposalID != link.ProposalID || originalNotice.ProposalVersion != link.Version || originalNotice.BaseRevision != link.BaseRevision || originalNotice.ChangeDigest != link.ChangeDigest || originalNotice.ResultDigest != link.ResultDigest || originalNotice.OriginStatus != "unapproved" {
		t.Fatalf("notice lost exact committed proposal: %+v link=%+v", originalNotice, link)
	}
	proposal, err := svc.DB.GetStandardsProposal(t.Context(), link.ProposalID, link.Version)
	if err != nil {
		t.Fatal(err)
	}
	proposal.Operations[0].Standard.Description = "Human review revised this guidance."
	proposal.ChangeDigest = "human-change"
	proposal.ResultDigest = "human-result"
	proposal.Author = "human reviewer"
	edited, err := svc.DB.SaveStandardsProposal(t.Context(), proposal, link.Version)
	if err != nil || edited.Version != 2 {
		t.Fatalf("later proposal edit=%+v %v", edited, err)
	}
	reopen()
	if err = svc.Recover(t.Context(), recoveredAt.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	notices, err = svc.DB.ListAnalystProposalNotices(t.Context(), o.ScheduleID, 0, 10)
	if err != nil || len(notices) != 1 || !reflect.DeepEqual(notices[0], originalNotice) {
		t.Fatalf("later version changed or duplicated original notice: %+v %v", notices, err)
	}
	shown, err := svc.DB.GetAnalystProposalNotice(t.Context(), originalNotice.ID)
	if err != nil || !reflect.DeepEqual(shown, originalNotice) {
		t.Fatalf("stable notice lookup changed: %+v %v", shown, err)
	}
	produced, err := svc.DB.GetStandardsProposal(t.Context(), originalNotice.ProposalID, originalNotice.ProposalVersion)
	if err != nil || produced.Version != 1 || produced.ChangeDigest != "original-change" || produced.ResultDigest != "original-result" {
		t.Fatalf("original proposal reference followed edit: %+v %v", produced, err)
	}
	if _, err = svc.DB.GetStandardsApproval(t.Context(), link.ProposalID, link.Version); !errors.Is(err, store.ErrStandardsApprovalRequired) {
		t.Fatalf("notice recovery approved proposal: %v", err)
	}
}

func TestRecoverLaunchedOrphanFencesWithoutRenewingBudget(t *testing.T) {
	svc, o, now := scheduleServiceFixture(t)
	a := allocateScheduledLog(t, svc, o, now)
	if err := svc.Recover(t.Context(), now.Add(leaseDuration+time.Minute)); err != nil {
		t.Fatal(err)
	}
	current, err := svc.DB.GetAnalystOccurrence(t.Context(), o.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.State != "incomplete" || current.AnalysisID != a.ID || current.Fence <= o.Fence || current.Works[0].Covered || current.Works[0].State != "unresolved" {
		t.Fatalf("orphan was not fenced as unresolved: %+v", current)
	}
	saved, err := svc.DB.GetLogAnalysis(t.Context(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.State != "incomplete" || saved.Generation <= a.Generation || saved.Calls != 7 || saved.OutboundBytes != 120 {
		t.Fatalf("orphan budget or identity changed: %+v", saved)
	}
	if err = svc.DB.CheckLogAnalysisWriter(t.Context(), a.ID, a.Generation); !errors.Is(err, store.ErrLogAnalysisConflict) {
		t.Fatalf("stale upstream writer remained usable: %v", err)
	}
	if err = svc.Recover(t.Context(), now.Add(2*leaseDuration)); err != nil {
		t.Fatal(err)
	}
	analyses, err := svc.DB.ListLogAnalyses(t.Context(), "", 10)
	if err != nil || len(analyses) != 1 {
		t.Fatalf("orphan recovery allocated another analysis: %+v %v", analyses, err)
	}
}

func TestAuthorityHoldDoesNotAllocateAnalyst(t *testing.T) {
	for _, holdAt := range []int{1, 2} {
		name := "initial admission"
		if holdAt == 2 {
			name = "launch recheck"
		}
		t.Run(name, func(t *testing.T) {
			svc, o, now := scheduleServiceFixture(t)
			checks := 0
			svc.Authorize = func(context.Context, store.Repo, string) error {
				checks++
				if checks == holdAt {
					return errors.New("fixture provider held")
				}
				return nil
			}
			if err := svc.Run(t.Context(), o, now); err != nil {
				t.Fatal(err)
			}
			current, err := svc.DB.GetAnalystOccurrence(t.Context(), o.ID)
			if err != nil {
				t.Fatal(err)
			}
			if checks != holdAt || current.State != "blocked" || current.Disposition != "not_evaluated" || current.AnalysisID != "" || !strings.Contains(current.Reason, "admission") {
				t.Fatalf("authority hold=%+v checks=%d", current, checks)
			}
			analyses, err := svc.DB.ListLogAnalyses(t.Context(), "", 10)
			if err != nil || len(analyses) != 0 {
				t.Fatalf("hold allocated upstream work: %+v %v", analyses, err)
			}
		})
	}
}

func TestNormalizePreservesLowerOnlyUpstreamLimits(t *testing.T) {
	c, err := normalize(Config{Name: "Logs", Kind: "logs", RepositoryID: 1, LogLimits: loganalysis.Limits{Evidence: logevidence.Limits{Runs: 2}}})
	if err != nil {
		t.Fatal(err)
	}
	if c.IntervalSeconds != 86400 || c.RunLimit != 2 || c.LogLimits.Evidence.Runs != 2 || c.LogLimits.Evidence.Records != 100 || c.LogLimits.Calls != 120 {
		t.Fatalf("normalized finite defaults=%+v", c)
	}
	for _, c := range []Config{
		{Name: "Logs", Kind: "logs", RepositoryID: 1, LogLimits: loganalysis.Limits{Evidence: logevidence.Limits{Runs: logevidence.AnalystLimits().Runs + 1}}},
		{Name: "Logs", Kind: "logs", RepositoryID: 1, LogLimits: loganalysis.Limits{Calls: 121}},
		{Name: "Logs", Kind: "logs", RepositoryID: 1, IntervalSeconds: 3599},
		{Name: "Bugs", Kind: "bugs", RepositoryID: 1, Modes: []string{"implement"}},
		{Name: "Drift", Kind: "drift", RepositoryID: 1, Ref: "main"},
	} {
		if _, err = normalize(c); err == nil {
			t.Fatalf("accepted broadened or ambiguous scope: %+v", c)
		}
	}
}

func TestBugReconciliationKeepsDeliveredCoverageAndRunMapping(t *testing.T) {
	svc, o, now := typedScheduleFixture(t, Config{Name: "Bug review", Kind: "bugs"}, t.TempDir(), []store.AnalystSourceWork{{Kind: "run", SourceKey: "run:42", RunID: 42, TaskRef: "github:fixture/scheduled#17", IssueNumber: 17, State: "selected"}, {Kind: "run", SourceKey: "run:43", RunID: 43, State: "selected"}})
	a, _, err := svc.DB.CreatePreventionAnalysis(store.WithAnalystOccurrence(t.Context(), o.ID, o.Fence, now), store.PreventionAnalysis{ID: "bug-fixture", RequestKey: o.StartKey, ScopeDigest: "bug-scope", Repository: "fixture/scheduled", AttemptID: "bug-attempt", Scope: raw(prevention.Scope{Repository: "fixture/scheduled", Issues: []int{17}, Limits: prevention.DefaultLimits()}), Limits: raw(prevention.DefaultLimits())})
	if err != nil {
		t.Fatal(err)
	}
	manifest := prevention.Manifest{Frozen: true, Evidence: []prevention.Evidence{{ID: "issue-17", Issue: 17, Kind: "issue", Digest: "fixture-evidence"}}, Coverage: []prevention.Coverage{{Issue: 17, Source: "issue", Status: "read"}}, StartedAt: now, FinishedAt: now}
	if err = svc.DB.FreezePreventionAnalysis(t.Context(), a.ID, a.AttemptID, raw(manifest)); err != nil {
		t.Fatal(err)
	}
	if err = svc.DB.AppendPreventionDelivery(t.Context(), a.ID, a.AttemptID, "issue-17"); err != nil {
		t.Fatal(err)
	}
	result := prevention.Result{SchemaVersion: 1, Findings: []prevention.Finding{{Issue: 17, Outcome: "no_warranted_change", BugEvidence: []string{"issue-17"}, CauseEvidence: []string{"issue-17"}}}}
	if _, err = svc.DB.FinalizePreventionAnalysis(t.Context(), a.ID, a.AttemptID, "bug-result", raw(map[string]any{"result": result}), nil); err != nil {
		t.Fatal(err)
	}
	if err = svc.Recover(t.Context(), now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	current, err := svc.DB.GetAnalystOccurrence(t.Context(), o.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.State != "incomplete" || current.Disposition != "no_warranted_change" || !current.Works[0].Covered || !current.Works[0].Offered || !current.Works[0].Cited || current.Works[0].IssueNumber != 17 || current.Works[0].RunID != 42 {
		t.Fatalf("bug coverage and originating run lost: %+v", current)
	}
	if current.Works[1].Covered || current.Works[1].State != "unresolved" || current.Works[1].IssueNumber != 0 {
		t.Fatalf("unmapped run gained coverage: %+v", current.Works[1])
	}
}

func TestDriftReconciliationAdvancesOnlyCoveredPinnedWindow(t *testing.T) {
	for _, partial := range []bool{false, true} {
		name := "covered"
		if partial {
			name = "partial"
		}
		t.Run(name, func(t *testing.T) {
			clone := gittest.New(t)
			base := clone.Head()
			clone.WriteFile("architecture.md", "Bounded evidence collection.\n")
			firstHead := clone.Commit("first drift")
			svc, o, now := typedScheduleFixture(t, Config{Name: "Drift review", Kind: "drift", Base: base, Ref: firstHead}, clone.Dir, nil)
			repo, err := svc.DB.GetRepo(t.Context(), o.RepositoryID)
			if err != nil {
				t.Fatal(err)
			}
			var config Config
			if err = json.Unmarshal(o.Config, &config); err != nil {
				t.Fatal(err)
			}
			o, err = svc.driftSources(t.Context(), o, config, repo, now)
			if err != nil {
				t.Fatal(err)
			}
			if len(o.Works) != 1 {
				t.Fatalf("pinned drift selection=%+v", o)
			}
			var pinned struct{ Base, Head string }
			if err = json.Unmarshal(o.Works[0].Metadata, &pinned); err != nil {
				t.Fatal(err)
			}
			if pinned.Base != base || pinned.Head != firstHead {
				t.Fatalf("unpinned endpoints: %+v", pinned)
			}
			a, _, err := svc.DB.CreateDriftAnalysis(store.WithAnalystOccurrence(t.Context(), o.ID, o.Fence, now), store.DriftAnalysis{ID: "drift-fixture", Repository: repo.Slug(), RequestKey: o.StartKey, RequestDigest: "drift-scope", Scope: raw(drift.Request{Repository: repo.Slug(), Base: base, Head: firstHead}), Limits: drift.DefaultLimits()})
			if err != nil {
				t.Fatal(err)
			}
			coverage := drift.Coverage{Enumerated: 1, Retained: 1}
			if partial {
				coverage.Gaps = []string{"one selected source was unavailable"}
			}
			a.Coverage = raw(coverage)
			a, err = svc.DB.UpdateDriftAnalysis(t.Context(), a, a.Generation, a.Version)
			if err != nil {
				t.Fatal(err)
			}
			evidenceDigest := store.DriftDigest([]byte("code range"))
			if err = svc.DB.AppendDriftEvidence(t.Context(), a.ID, a.Generation, store.DriftEvidence{ID: "code-range", Kind: "code", Text: "code range", Digest: evidenceDigest}); err != nil {
				t.Fatal(err)
			}
			a, err = svc.DB.ChargeDriftBudget(t.Context(), a.ID, a.Generation, store.DriftUsage{}, []store.DriftOffer{{EvidenceID: "code-range", Digest: evidenceDigest, Start: 0, End: 10}}, false)
			if err != nil {
				t.Fatal(err)
			}
			disposition := "no_warranted_change"
			if partial {
				disposition = "insufficient_evidence"
			}
			report := drift.Report{Disposition: disposition, Findings: []drift.Finding{{Citations: []drift.Citation{{EvidenceID: "code-range", Digest: evidenceDigest, Start: 0, End: 10}}}}}
			if _, err = svc.DB.FinalizeDriftAnalysis(t.Context(), a.ID, a.Generation, a.Version, "drift-report", "drift-report-digest", raw(map[string]any{"report": report, "coverage": coverage}), "completed", disposition, nil); err != nil {
				t.Fatal(err)
			}
			if err = svc.Recover(t.Context(), now.Add(time.Minute)); err != nil {
				t.Fatal(err)
			}
			current, err := svc.DB.GetAnalystOccurrence(t.Context(), o.ID)
			if err != nil {
				t.Fatal(err)
			}
			if current.Works[0].Covered == partial || !current.Works[0].Offered || !current.Works[0].Cited {
				t.Fatalf("drift host coverage/citations=%+v", current)
			}
			clone.WriteFile("architecture.md", "Bounded evidence and explicit context.\n")
			secondHead := clone.Commit("second drift")
			schedule, err := svc.DB.GetAnalystSchedule(t.Context(), o.ScheduleID)
			if err != nil {
				t.Fatal(err)
			}
			config.Ref = secondHead
			schedule, err = svc.Change(t.Context(), schedule.ID, schedule.Version, "github:71", config, now.Add(time.Hour))
			if err != nil {
				t.Fatal(err)
			}
			next, err := svc.DB.ReserveAnalystOccurrence(t.Context(), schedule.ID, schedule.Version, now.Add(24*time.Hour), leaseDuration)
			if err != nil {
				t.Fatal(err)
			}
			next, err = svc.driftSources(t.Context(), next, config, repo, now.Add(24*time.Hour))
			if partial {
				if err == nil {
					t.Fatal("partial pinned window should require explicit reanalysis")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(next.Works) != 1 {
				t.Fatalf("next drift selection=%+v", next)
			}
			if err = json.Unmarshal(next.Works[0].Metadata, &pinned); err != nil {
				t.Fatal(err)
			}
			expectedBase := firstHead
			if partial {
				expectedBase = base
			}
			if pinned.Base != expectedBase || pinned.Head != secondHead {
				t.Fatalf("baseline advanced without complete coverage: got=%+v expected base=%s", pinned, expectedBase)
			}
		})
	}
}

func TestStalePreparationCannotAdoptNewReservationFence(t *testing.T) {
	svc, old, now := scheduleServiceFixture(t)
	current, e := svc.DB.RecoverAnalystOccurrence(t.Context(), old.ID, now.Add(leaseDuration+time.Minute), leaseDuration)
	if e != nil {
		t.Fatal(e)
	}
	svc.Authorize = func(context.Context, store.Repo, string) error { return nil }
	if e = svc.Run(t.Context(), old, now.Add(leaseDuration+time.Minute)); !errors.Is(e, store.ErrAnalystScheduleConflict) {
		t.Fatalf("stale preparation adopted new fence: %v", e)
	}
	saved, e := svc.DB.GetAnalystOccurrence(t.Context(), old.ID)
	if e != nil {
		t.Fatal(e)
	}
	if saved.Fence != current.Fence || saved.State != "reserved" {
		t.Fatalf("stale writer changed recovered reservation: %+v", saved)
	}
}
