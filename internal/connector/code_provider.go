package connector

import (
	"context"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

// CodeEvidenceProvider exposes sanitized committed and published evidence only.
type CodeEvidenceProvider interface {
	Evidence(context.Context, store.ConnectorCodeTarget, CodeEvidenceRequest) (CodeEvidencePage, error)
}

// CodeEvidencePin identifies one locally observed, immutable evidence view.
type CodeEvidencePin struct {
	TaskCommit   string    `json:"task_commit,omitempty"`
	BaseCommit   string    `json:"base_commit,omitempty"`
	TaskRef      string    `json:"task_ref,omitempty"`
	BaseRef      string    `json:"base_ref,omitempty"`
	BookDigest   string    `json:"book_digest,omitempty"`
	BookRevision int64     `json:"book_revision,omitempty"`
	ObservedAt   time.Time `json:"observed_at"`
	Policy       string    `json:"redaction_policy"`
}

// CodeEvidenceRequest contains validated selectors and a trusted retained pin.
type CodeEvidenceRequest struct {
	Operation, Branch, Path, Pattern, Query, Mode, Commit, Role string
	Categories                                                  []string
	IgnoreCase                                                  bool
	StartLine, Lines, Limit, Offset                             int
	Pin                                                         CodeEvidencePin
}

// CodeEvidencePage contains sanitized facts with advancing finite pagination.
type CodeEvidencePage struct {
	Pin        CodeEvidencePin
	Items      []map[string]any
	Sources    []map[string]any
	Gaps       []string
	HasMore    bool
	NextOffset int
}
