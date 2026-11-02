package localstandards

import (
	"github.com/cloudvane/litefactory/internal/standards"
	"time"
)

// RunBinding contains retained standards evidence for local inspection.
type RunBinding struct {
	RootRunID     string    `json:"root_run_id"`
	RevisionID    int64     `json:"revision_id"`
	Source        string    `json:"source"`
	SchemaVersion int       `json:"schema_version"`
	Digest        string    `json:"digest"`
	Canonical     string    `json:"canonical,omitempty"`
	Outcome       string    `json:"outcome"`
	Reason        string    `json:"reason,omitempty"`
	CapturedAt    time.Time `json:"captured_at"`
}

// RunSelection contains retained standards evidence for local inspection.
type RunSelection struct {
	RootRunID   string    `json:"root_run_id"`
	ConsumerKey string    `json:"consumer_key"`
	Version     int64     `json:"version"`
	Digest      string    `json:"digest"`
	CategoryIDs []string  `json:"category_ids"`
	StandardIDs []string  `json:"standard_ids"`
	Reason      string    `json:"reason"`
	AttemptID   string    `json:"attempt_id"`
	CreatedAt   time.Time `json:"created_at"`
}

// RunObservation contains retained standards evidence for local inspection.
type RunObservation struct {
	RootRunID        string    `json:"root_run_id"`
	ConsumerKey      string    `json:"consumer_key"`
	AttemptID        string    `json:"attempt_id"`
	Kind             string    `json:"kind"`
	Outcome          string    `json:"outcome"`
	Reason           string    `json:"reason,omitempty"`
	SelectionVersion int64     `json:"selection_version"`
	StandardID       string    `json:"standard_id,omitempty"`
	Start            int       `json:"start"`
	End              int       `json:"end"`
	Total            int       `json:"total"`
	CreatedAt        time.Time `json:"created_at"`
}

// RunStatus is an owned local standards inspection view.
type RunStatus struct {
	Binding                  RunBinding    `json:"binding"`
	Integrity                string        `json:"integrity"`
	Reason                   string        `json:"reason,omitempty"`
	SelectionReason          string        `json:"selection_reason,omitempty"`
	SelectionStatus          string        `json:"selection_status"`
	RetrievalStatus          string        `json:"retrieval_status"`
	Selection                *RunSelection `json:"selection,omitempty"`
	CategoryCount            int           `json:"category_count"`
	StandardCount            int           `json:"standard_count"`
	StatusPersistenceFailure string        `json:"status_persistence_failure,omitempty"`
	VerifiedAt               time.Time     `json:"verified_at"`
}

// ConsumerInspection is an owned local standards inspection view.
type ConsumerInspection struct {
	ConsumerKey   string    `json:"consumer_key"`
	Status        RunStatus `json:"status"`
	RetrievalGaps []string  `json:"retrieval_gaps"`
}

// RunInspection is an owned local standards inspection view.
type RunInspection struct {
	Status       RunStatus            `json:"status"`
	Selections   []RunSelection       `json:"selections"`
	Observations []RunObservation     `json:"observations"`
	Consumers    []ConsumerInspection `json:"consumers"`
}

func runStatus(source standards.RuntimeStatus) RunStatus {
	b := source.Binding
	view := RunStatus{Binding: RunBinding{RootRunID: b.RootRunID, RevisionID: b.RevisionID, Source: b.Source, SchemaVersion: b.SchemaVersion, Digest: b.Digest, Canonical: b.Canonical, Outcome: b.Outcome, Reason: b.Reason, CapturedAt: b.CapturedAt}, Integrity: source.Integrity, Reason: source.Reason, SelectionReason: source.SelectionReason, SelectionStatus: source.SelectionStatus, RetrievalStatus: source.RetrievalStatus, CategoryCount: source.CategoryCount, StandardCount: source.StandardCount, StatusPersistenceFailure: source.StatusPersistenceFailure, VerifiedAt: source.VerifiedAt}
	if source.Selection != nil {
		selection := RunSelection(*source.Selection)
		view.Selection = &selection
	}
	return view
}

// InspectRuntime captures domain inspection without exposing persisted records.
func InspectRuntime(source standards.RunInspection) RunInspection {
	view := RunInspection{Status: runStatus(source.Status)}
	if source.Selections != nil {
		view.Selections = make([]RunSelection, len(source.Selections))
		for i, v := range source.Selections {
			view.Selections[i] = RunSelection(v)
		}
	}
	if source.Observations != nil {
		view.Observations = make([]RunObservation, len(source.Observations))
		for i, v := range source.Observations {
			view.Observations[i] = RunObservation(v)
		}
	}
	if source.Consumers != nil {
		view.Consumers = make([]ConsumerInspection, len(source.Consumers))
		for i, v := range source.Consumers {
			view.Consumers[i] = ConsumerInspection{ConsumerKey: v.ConsumerKey, Status: runStatus(v.Status), RetrievalGaps: v.RetrievalGaps}
		}
	}
	return view
}
