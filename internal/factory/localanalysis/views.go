package localanalysis

import (
	"encoding/json"
	"github.com/cloudvane/litefactory/internal/factory/localstandards"
	"github.com/cloudvane/litefactory/internal/loganalysis"
	"github.com/cloudvane/litefactory/internal/store"
	"time"
)

// Evidence is retained local analysis evidence.
type Evidence struct {
	ID         int64           `json:"id"`
	AnalysisID string          `json:"analysis_id"`
	Generation int64           `json:"generation"`
	CallKey    string          `json:"call_key"`
	Metadata   json.RawMessage `json:"metadata"`
	CreatedAt  time.Time       `json:"created_at"`
}

// ProposalLink is retained local analysis evidence.
type ProposalLink struct {
	CandidateKey string `json:"candidate_key"`
	ProposalID   string `json:"proposal_id"`
	Version      int64  `json:"version"`
	BaseRevision int64  `json:"base_revision"`
	ChangeDigest string `json:"change_digest"`
	ResultDigest string `json:"result_digest"`
}

// Filing is retained local analysis evidence.
type Filing struct {
	AnalysisID  string    `json:"analysis_id"`
	FindingKey  string    `json:"finding_key"`
	Destination string    `json:"destination"`
	State       string    `json:"state"`
	Reference   string    `json:"reference,omitempty"`
	URL         string    `json:"url,omitempty"`
	Reason      string    `json:"reason,omitempty"`
	Title       string    `json:"title"`
	Body        string    `json:"body"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Inspection contains only the owned local analysis presentation.
type Inspection struct {
	Invocation   AnalysisView                 `json:"invocation"`
	Standards    localstandards.RunInspection `json:"standards"`
	Read         loganalysis.Reading          `json:"read"`
	Evidence     []Evidence                   `json:"evidence"`
	EvidenceNext int64                        `json:"evidence_next,omitempty"`
	Report       json.RawMessage              `json:"report,omitempty"`
	Proposals    []ProposalLink               `json:"proposals"`
	Filings      []Filing                     `json:"filings"`
}

func inspection(source loganalysis.Inspection) Inspection {
	view := Inspection{Invocation: AnalysisView(source.Invocation), Standards: localstandards.InspectRuntime(source.Standards), Read: source.Read, EvidenceNext: source.EvidenceNext, Report: source.Report}
	if source.Evidence != nil {
		view.Evidence = make([]Evidence, len(source.Evidence))
		for i, v := range source.Evidence {
			view.Evidence[i] = Evidence(v)
		}
	}
	if source.Proposals != nil {
		view.Proposals = make([]ProposalLink, len(source.Proposals))
		for i, v := range source.Proposals {
			view.Proposals[i] = ProposalLink(v)
		}
	}
	if source.Filings != nil {
		view.Filings = make([]Filing, len(source.Filings))
		for i, v := range source.Filings {
			view.Filings[i] = Filing(v)
		}
	}
	return view
}

// FindingStatus renders the existing filing outcome using its retained facts.
func FindingStatus(id string, scope loganalysis.Scope, finding loganalysis.Finding, filing Filing, recorded bool) string {
	return loganalysis.FindingStatus(id, scope, finding, store.LogAnalysisFiling(filing), recorded)
}
