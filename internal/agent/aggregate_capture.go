package agent

import (
	"encoding/base64"
	"fmt"
	"slices"
	"strings"
)

// AggregateCheckClaim describes a factual reported execution. Scope must be
// aggregate, and the caller supplies the digest checked at execution time. This
// serializer never runs a check or certifies provider execution provenance.
type AggregateCheckClaim struct {
	ID                   string `json:"id"`
	ExecutionID          string `json:"execution_id"`
	Scope                string `json:"scope"`
	Name                 string `json:"name"`
	Executed             bool   `json:"executed"`
	Passed               bool   `json:"passed"`
	Evidence             string `json:"evidence"`
	ImplementationDigest string `json:"implementation_digest"`
}

// AggregateCapture serializes complete bytes independently of story checkpoints.
// Failed or unexecuted facts remain factual and cannot pass completion validation.
type AggregateCapture struct {
	ImplementationDigest string                    `json:"implementation_digest"`
	Files                []ContributionCaptureFile `json:"files"`
	Checks               []AggregateCheckClaim     `json:"checks"`
}

// NewAggregateCapture sorts/copies complete files and validates supplied facts;
// it does not invent a successful execution or fill a missing execution digest.
func NewAggregateCapture(files []StorySnapshotFile, checks []AggregateCheckClaim) (AggregateCapture, error) {
	if len(files) == 0 || len(files) > 256 || len(checks) == 0 || len(checks) > 256 {
		return AggregateCapture{}, fmt.Errorf("aggregate capture requires bounded files and checks")
	}
	remaining := MaxStoryEventBytes
	for _, f := range files {
		if len(f.Content) > remaining {
			return AggregateCapture{}, fmt.Errorf("aggregate capture exceeds byte limit")
		}
		remaining -= len(f.Content)
	}
	copied := slices.Clone(files)
	slices.SortFunc(copied, func(a, b StorySnapshotFile) int { return strings.Compare(a.Path, b.Path) })
	c := AggregateCapture{ImplementationDigest: SnapshotDigest(copied), Checks: slices.Clone(checks), Files: []ContributionCaptureFile{}}
	for _, f := range copied {
		c.Files = append(c.Files, ContributionCaptureFile{Path: f.Path, Content: base64.StdEncoding.EncodeToString(f.Content), SHA256: f.SHA256, Deleted: f.Deleted})
	}
	_, err := c.SnapshotFiles()
	return c, err
}

// SnapshotFiles validates bounded sorted complete bytes and exact claim digests.
func (c AggregateCapture) SnapshotFiles() ([]StorySnapshotFile, error) {
	if len(c.Files) == 0 || len(c.Files) > 256 || len(c.Checks) == 0 || len(c.Checks) > 256 {
		return nil, fmt.Errorf("aggregate capture requires bounded files and checks")
	}
	files := make([]StorySnapshotFile, 0, len(c.Files))
	previous := ""
	remaining := MaxStoryEventBytes
	for _, f := range c.Files {
		if !SafeStoryPath(f.Path) || f.Path <= previous || len(f.Content) > base64.StdEncoding.EncodedLen(remaining) {
			return nil, fmt.Errorf("aggregate capture contains invalid paths or oversized bytes")
		}
		data, err := base64.StdEncoding.DecodeString(f.Content)
		if err != nil || len(data) > remaining {
			return nil, fmt.Errorf("aggregate capture contains invalid or oversized base64")
		}
		remaining -= len(data)
		if StoryDigest(data) != f.SHA256 || (f.Deleted && len(data) != 0) {
			return nil, fmt.Errorf("aggregate capture file hash or deletion conflicts")
		}
		files = append(files, StorySnapshotFile{Path: f.Path, Content: data, SHA256: f.SHA256, Deleted: f.Deleted})
		previous = f.Path
	}
	if !validStoryDigest(c.ImplementationDigest) || SnapshotDigest(files) != c.ImplementationDigest {
		return nil, fmt.Errorf("aggregate manifest digest mismatch")
	}
	ids := map[string]bool{}
	executions := map[string]bool{}
	for _, check := range c.Checks {
		if strings.TrimSpace(check.ID) == "" || len(check.ID) > 256 || ids[check.ID] || strings.TrimSpace(check.ExecutionID) == "" || len(check.ExecutionID) > 256 || executions[check.ExecutionID] || check.Scope != "aggregate" || strings.TrimSpace(check.Name) == "" || len(check.Name) > 256 || strings.TrimSpace(check.Evidence) == "" || len(check.Evidence) > 16384 || check.ImplementationDigest != c.ImplementationDigest || (!check.Executed && check.Passed) {
			return nil, fmt.Errorf("aggregate check requires unique attributed execution, digest and factual outcome")
		}
		ids[check.ID] = true
		executions[check.ExecutionID] = true
	}
	return files, nil
}

// AggregateReference creates a copied reference list without executing/capturing.
// Validate may use this helper with already retained facts.
func AggregateReference(snapshotID string, checkIDs []string) (EngineeringContribution, error) {
	if strings.TrimSpace(snapshotID) == "" || len(snapshotID) > 256 || len(checkIDs) == 0 || len(checkIDs) > 256 {
		return EngineeringContribution{}, fmt.Errorf("aggregate reference requires snapshot and checks")
	}
	ids := map[string]bool{}
	for _, id := range checkIDs {
		if strings.TrimSpace(id) == "" || len(id) > 256 || ids[id] {
			return EngineeringContribution{}, fmt.Errorf("aggregate reference contains invalid check IDs")
		}
		ids[id] = true
	}
	return EngineeringContribution{SnapshotID: snapshotID, CheckIDs: slices.Clone(checkIDs)}, nil
}
