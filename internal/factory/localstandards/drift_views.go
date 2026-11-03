package localstandards

import (
	"encoding/json"
	"time"
)

// DriftLimits holds immutable source, delivery and analyst work ceilings.
type DriftLimits struct {
	Paths              int   `json:"paths"`
	Issues             int   `json:"issues"`
	Comments           int   `json:"comments"`
	Requests           int   `json:"requests"`
	SourceBytes        int64 `json:"source_bytes"`
	ItemBytes          int   `json:"item_bytes"`
	AcquisitionSeconds int   `json:"acquisition_seconds"`
	AgentSeconds       int   `json:"agent_seconds"`
	Calls              int   `json:"calls"`
	OutboundBytes      int64 `json:"outbound_bytes"`
	ResultBytes        int   `json:"result_bytes"`
	Proposals          int   `json:"proposals"`
}

// DriftUsage records cumulative charges against an analysis's retained limits.
type DriftUsage struct {
	Paths         int   `json:"paths"`
	Comments      int   `json:"comments"`
	Requests      int   `json:"requests"`
	SourceBytes   int64 `json:"source_bytes"`
	Calls         int   `json:"calls"`
	OutboundBytes int64 `json:"outbound_bytes"`
	Invocations   int   `json:"invocations"`
}

// DriftRange identifies a half-open byte range in a sanitized evidence excerpt.
type DriftRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// DriftEvidence retains sanitized source content and its provenance or expiry metadata.
type DriftEvidence struct {
	ID              string          `json:"id"`
	AnalysisID      string          `json:"analysis_id"`
	Kind            string          `json:"kind"`
	Locator         string          `json:"locator"`
	Path            string          `json:"path,omitempty"`
	Commit          string          `json:"commit,omitempty"`
	Blob            string          `json:"blob,omitempty"`
	Issue           int             `json:"issue,omitempty"`
	CommentID       int64           `json:"comment_id,omitempty"`
	URL             string          `json:"url,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
	CollectedAt     time.Time       `json:"collected_at"`
	Text            string          `json:"text"`
	Digest          string          `json:"digest"`
	RedactionPolicy string          `json:"redaction_policy"`
	Omission        string          `json:"omission,omitempty"`
	ExcerptExpired  bool            `json:"excerpt_expired,omitempty"`
	ExcerptBytes    int             `json:"excerpt_bytes,omitempty"`
	Metadata        json.RawMessage `json:"metadata,omitempty"`
}

// DriftOffer records an evidence range reserved for delivery to the analyst.
type DriftOffer struct {
	EvidenceID string `json:"evidence_id"`
	Digest     string `json:"digest"`
	Start      int    `json:"start"`
	End        int    `json:"end"`
}

// DriftProposalLink identifies the exact unapproved version created for a candidate.
type DriftProposalLink struct {
	CandidateKey string `json:"candidate_key"`
	ProposalID   string `json:"proposal_id"`
	Version      int64  `json:"version"`
	Digest       string `json:"digest"`
}

// DriftAnalysis is a persisted invocation with immutable scope and fenced progress.
type DriftAnalysis struct {
	ID            string              `json:"id"`
	RunID         string              `json:"run_id"`
	Repository    string              `json:"repository"`
	RequestKey    string              `json:"request_key,omitempty"`
	RequestDigest string              `json:"request_digest"`
	Scope         json.RawMessage     `json:"scope"`
	Pinned        json.RawMessage     `json:"pinned,omitempty"`
	Limits        DriftLimits         `json:"limits"`
	Usage         DriftUsage          `json:"usage"`
	Generation    int64               `json:"generation"`
	Version       int64               `json:"version"`
	Execution     string              `json:"execution"`
	Disposition   string              `json:"disposition"`
	Coverage      json.RawMessage     `json:"coverage,omitempty"`
	Standards     json.RawMessage     `json:"standards,omitempty"`
	Provenance    json.RawMessage     `json:"provenance,omitempty"`
	Report        json.RawMessage     `json:"report,omitempty"`
	Offers        []DriftOffer        `json:"offers"`
	ProposalLinks []DriftProposalLink `json:"proposal_links"`
	Reason        string              `json:"reason,omitempty"`
	CreatedAt     time.Time           `json:"created_at"`
	UpdatedAt     time.Time           `json:"updated_at"`
}
