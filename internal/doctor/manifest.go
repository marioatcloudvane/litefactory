package doctor

import (
	"context"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// PathManifest stores locations, never pre-read contents. Live distinguishes
// these attempts from retained legacy evidence during result recovery.
type PathManifest struct {
	SandboxProfile string    `json:"sandbox_profile"`
	Isolation      string    `json:"isolation"`
	Live           bool      `json:"live"`
	TaskRef        string    `json:"task_ref"`
	CapturedAt     time.Time `json:"captured_at"`
	FactoryHome    string    `json:"factory_home"`
	DaemonLog      string    `json:"daemon_log"`
	Database       string    `json:"database"`
	RunLogs        []string  `json:"run_logs"`
	Worktree       string    `json:"worktree"`
	Tools          []string  `json:"tools"`
	Gaps           []string  `json:"gaps"`
	// Records holds only the branch synchronization record, captured as the
	// repair recheck captures it, so sync_branch proposals bind.
	Records []store.DoctorEvidenceRecord `json:"records,omitempty"`
}

func (s *Service) pathManifest(ctx context.Context, ref naming.Ref) (PathManifest, string, error) {
	sources, err := s.DB.ReadDoctorPathSources(ctx, ref)
	if err != nil {
		return PathManifest{}, "", err
	}
	m := PathManifest{Live: true, TaskRef: ref.String(), CapturedAt: time.Now().UTC(), FactoryHome: s.Paths.Home(), DaemonLog: s.Paths.DaemonLogPath(), Database: s.DB.Path(), RunLogs: sources.RunLogs, Worktree: sources.Worktree, Tools: append([]string{"shell"}, agent.DoctorToolNames()...), Gaps: []string{}}
	if sources.SessionMissing {
		m.Gaps = append(m.Gaps, "No workflow session exists for this issue")
	}
	if m.Worktree == "" {
		m.Gaps = append(m.Gaps, "Task worktree path is not recorded; inspect database routing metadata")
	}
	if sources.LogsTruncated {
		m.Gaps = append(m.Gaps, "Run log manifest includes only the latest 100 runs; query runs.log_path for earlier logs")
	}
	if !sources.SessionMissing && m.Worktree != "" {
		branch := store.DoctorEvidence{}
		s.collectBranchRecord(ctx, ref, &branch)
		m.Records = branch.Records
		m.Gaps = append(m.Gaps, branch.Gaps...)
	}
	return m, sources.Provider, nil
}

func (m PathManifest) evidenceIdentity() store.DoctorEvidence {
	return store.DoctorEvidence{Live: m.Live, TaskRef: m.TaskRef, Records: m.Records, Gaps: m.Gaps}
}
