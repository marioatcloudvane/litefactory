package turn

import (
	"context"
	"errors"

	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

// executionRunEvidence reads recorded provider/model facts. A missing observation
// stays unknown, including historical runs and unsuccessful provider launches.
func (r *Reporter) executionRunEvidence(ctx context.Context, run store.Run) (entries []report.Execution, err error) {
	if run.ID <= 0 {
		return nil, nil
	}
	entries, err = r.executionMeterEvidence(ctx, store.MeterFilter{RunID: run.ID}, run.Ordinal, 0)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		entries = append(entries, report.Execution{Run: run.Ordinal, Agent: run.Agent})
	}
	return entries, nil
}

// executionMeterEvidence preserves each provider launch and measured invocation.
func (r *Reporter) executionMeterEvidence(ctx context.Context, filter store.MeterFilter, ordinal int, attemptID int64) (entries []report.Execution, err error) {
	snapshot, err := r.db.NewMeterSnapshot(ctx, filter, store.MeterSnapshotOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, snapshot.Close(ctx)) }()
	seen := make(map[report.Execution]bool)
	for {
		rows, readErr := snapshot.Next(ctx, 256)
		if readErr != nil {
			return nil, readErr
		}
		if len(rows) == 0 {
			break
		}
		for _, row := range rows {
			if attemptID > 0 && (row.GraphAttemptID == nil || *row.GraphAttemptID != attemptID) {
				continue
			}
			entry := report.Execution{Run: ordinal, Step: row.GraphNode, Attempt: row.AttemptID, Invocation: row.InvocationID, Agent: row.CodingAgent, ModelProvenance: row.ModelProvenance}
			if row.ActualModel != nil {
				entry.Model = *row.ActualModel
			}
			if !seen[entry] {
				entries = append(entries, entry)
				seen[entry] = true
			}
		}
	}
	return entries, nil
}

func (r *Reporter) executionGraphEvidence(ctx context.Context, instanceID int64) ([]report.Execution, error) {
	attempts, err := r.db.ListGraphNodeAttempts(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	var entries []report.Execution
	seenRuns := make(map[int64]bool)
	for _, attempt := range attempts {
		if attempt.RunID == 0 || seenRuns[attempt.RunID] {
			continue
		}
		seenRuns[attempt.RunID] = true
		run, err := r.db.GetRun(ctx, attempt.RunID)
		if err != nil {
			return nil, err
		}
		evidence, err := r.executionRunEvidence(ctx, run)
		if err != nil {
			return nil, err
		}
		if len(evidence) == 1 && evidence[0].Attempt == "" {
			evidence[0].Step = "recorded run selection; invocation not reported"
		}
		entries = append(entries, evidence...)
	}
	judges, err := r.db.ListGraphJudgeAttempts(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	for _, judge := range judges {
		evidence, err := r.executionMeterEvidence(ctx, store.MeterFilter{JudgeAttemptID: judge.ID}, 0, 0)
		if err != nil {
			return nil, err
		}
		if len(evidence) == 0 {
			evidence = []report.Execution{{Step: "advisory judge", Attempt: judge.ID, Agent: judge.Provider, Model: judge.ObservedModel}}
		} else {
			observed := false
			for i := range evidence {
				evidence[i].Step = "advisory judge " + judge.ID
				if evidence[i].Model == judge.ObservedModel {
					observed = true
				}
				// Native launch metadata can fill a missing root observation, never a
				// child invocation whose model was not reported.
				if evidence[i].Invocation == "" && evidence[i].Model == "" && judge.ObservedModel != "" {
					evidence[i].Model = judge.ObservedModel
					evidence[i].ModelProvenance = "native launch"
					observed = true
				}
			}
			if judge.ObservedModel != "" && !observed {
				evidence = append(evidence, report.Execution{Step: "advisory judge", Attempt: judge.ID, Agent: judge.Provider, Model: judge.ObservedModel, ModelProvenance: "native launch"})
			}
		}
		entries = append(entries, evidence...)
	}
	return entries, nil
}
