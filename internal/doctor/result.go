package doctor

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

// Finding links a plain-language conclusion to factory-owned evidence handles.
type Finding struct {
	Explanation string   `json:"explanation"`
	Evidence    []string `json:"evidence"`
	Uncertainty string   `json:"uncertainty"`
}

// RepairStep is an inert proposal, never executable SQL or shell authority.
type RepairStep struct {
	Action        *store.DoctorRepairAction `json:"action,omitempty"`
	Number        int                       `json:"number"`
	Target        string                    `json:"target"`
	Precondition  string                    `json:"precondition"`
	Change        string                    `json:"change"`
	Reason        string                    `json:"reason"`
	Reversibility string                    `json:"reversibility"`
	Rollback      string                    `json:"rollback"`
}

// Diagnosis is bounded structured agent data. Only the factory may adopt it.
type Diagnosis struct {
	Repair       *RepairSummary `json:"repair,omitempty"`
	Findings     []Finding      `json:"findings"`
	CoverageGaps []string       `json:"coverage_gaps"`
	Questions    []string       `json:"questions"`
	Proposal     []RepairStep   `json:"proposal"`
}

// DecodeDiagnosis rejects unknown fields, trailing values and oversized input.
func DecodeDiagnosis(data []byte) (Diagnosis, error) {
	var d Diagnosis
	if len(data) > 65536 {
		return d, errors.New("doctor result exceeds 65536 bytes")
	}
	if err := submission.DecodeJSON(data, &d); err != nil {
		return d, fmt.Errorf("decode doctor diagnosis: %w", err)
	}
	return d, nil
}

// EvidenceHandle is derived from the scoped collector, never supplied by an agent.
func EvidenceHandle(r store.DoctorEvidenceRecord) string { return r.Kind + ":" + r.ID }

// ValidateDiagnosis refuses invented/foreign evidence and incomplete proposals.
// File proposals remain unavailable until the collector can prove exact ownership.
func ValidateDiagnosis(d Diagnosis, e store.DoctorEvidence) error {
	for _, p := range d.Proposal {
		if p.Action == nil || !store.DoctorRepairToolKind(p.Action.Kind) {
			return errors.New("new doctor proposals require an exact typed repair tool")
		}
	}
	return validateRetainedDiagnosis(d, e)
}

// validateRetainedDiagnosis reads previously accepted legacy proposals without
// granting them admission as a newly submitted proposal.
func validateRetainedDiagnosis(d Diagnosis, e store.DoctorEvidence) error {
	if len(d.Findings) == 0 || len(d.Findings) > 30 || len(d.Proposal) > 30 || len(d.Questions) > 30 || len(d.CoverageGaps) > 100 {
		return errors.New("doctor diagnosis exceeds collection bounds or has no findings")
	}
	known := make(map[string]bool, len(e.Records))
	targets := make(map[string]bool, len(e.Records))
	for _, r := range e.Records {
		known[EvidenceHandle(r)] = true
		switch r.Kind {
		case "session_file":
			if r.Identity != "" && (len(r.State) == 64 || r.State == absentFile) && repairableSessionPath(r.ID) {
				targets[EvidenceHandle(r)] = true
			}
		case "worktree_file":
			if r.Identity != "" && (len(r.State) == 64 || r.State == absentFile) && repairableWorktreePath(r.ID) {
				targets[EvidenceHandle(r)] = true
			}
		case "session_lock", "session", "run", "watermark", "schedule", "report", "report_recovery", "graph", "graph_attempt", "graph_dispatch", "owner_wait", "story", "comment":
			targets[EvidenceHandle(r)] = true
		}
	}
	for _, f := range d.Findings {
		if !boundedText(f.Explanation) || !boundedText(f.Uncertainty) || len(f.Evidence) > 30 {
			return errors.New("doctor finding requires bounded explanation and uncertainty")
		}
		if len(f.Evidence) == 0 && len(d.CoverageGaps) == 0 {
			return errors.New("doctor finding without evidence must name a coverage gap")
		}
		for _, handle := range f.Evidence {
			if e.Live {
				if !boundedText(handle) {
					return errors.New("doctor live source citation is empty or oversized")
				}
			} else if !known[handle] {
				return fmt.Errorf("doctor finding references unknown evidence handle %q", handle)
			}
		}
	}
	gaps := make(map[string]bool, len(d.CoverageGaps))
	for _, gap := range d.CoverageGaps {
		if !boundedText(gap) {
			return errors.New("invalid doctor coverage gap")
		}
		gaps[gap] = true
	}
	for _, gap := range e.Gaps {
		if !gaps[gap] {
			return fmt.Errorf("doctor result omitted collector coverage gap %q; copy it verbatim into coverage_gaps", gap)
		}
	}
	for _, q := range d.Questions {
		if !boundedText(q) {
			return errors.New("invalid doctor question")
		}
	}
	for i, p := range d.Proposal {
		if p.Number != i+1 || !targets[p.Target] {
			return errors.New("doctor proposal has invalid numbering or unresolved target")
		}
		if p.Action != nil {
			switch p.Action.Kind {
			case "session_file", "worktree_file":
				matched := false
				for _, r := range e.Records {
					if EvidenceHandle(r) == p.Target && r.Kind == p.Action.Kind && r.State == p.Action.Before && r.Identity != "" {
						matched = true
					}
				}
				if !matched || len(p.Action.After) > 32768 {
					return errors.New("invalid exact session file repair")
				}
			default:
				if !store.DoctorRowRepairTarget(p.Action.Kind, p.Target, p.Action.Before, p.Action.After) && !store.DoctorTypedRowRepairTarget(p.Action.Kind, p.Target, p.Action.Before, p.Action.After) {
					return errors.New("unsupported exact repair action")
				}
				observed := false
				for _, r := range e.Records {
					state := r.State
					if p.Action.Kind == "poll_flag" {
						state = strings.TrimPrefix(r.Content, "needs_poll=")
					}
					if (EvidenceHandle(r) == p.Target || (p.Action.Kind == "consume_comment" && r.Kind == "watermark")) && state == p.Action.Before {
						observed = true
					}
				}
				if !observed {
					return errors.New("repair row precondition was not observed")
				}

			}
		}
		for _, field := range []string{p.Precondition, p.Change, p.Reason, p.Reversibility, p.Rollback} {
			if !boundedText(field) {
				return errors.New("doctor proposal requires bounded precondition, change, reason, reversibility and rollback")
			}
		}
	}
	encoded, err := json.Marshal(d)
	if err != nil {
		return err
	}
	if len(encoded) > 65536 {
		return errors.New("doctor result exceeds 65536 bytes")
	}
	return nil
}
func boundedText(s string) bool { return strings.TrimSpace(s) != "" && len(s) <= 8192 }
