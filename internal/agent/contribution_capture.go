package agent

import (
	"encoding/base64"
	"fmt"
	"maps"
	"strings"
)

// SnapshotEvidence converts a capture without granting trust to its execution
// claims. The factory must compare these bytes with the worktree before submit.
func (c ContributionCapture) SnapshotEvidence() (StoryEvidence, error) {
	e := StoryEvidence{ImplementationDigest: c.ImplementationDigest, Acceptance: []StoryCheck{}, Review: []StoryCheck{}, Tests: []StoryCheck{}}
	for _, f := range c.Files {
		content, err := base64.StdEncoding.DecodeString(f.Content)
		if err != nil {
			return e, fmt.Errorf("invalid captured base64 bytes")
		}
		e.Files = append(e.Files, StorySnapshotFile{Path: f.Path, Content: content, SHA256: f.SHA256, Deleted: f.Deleted})
	}
	for _, check := range c.Checks {
		e.Tests = append(e.Tests, StoryCheck{Name: check.Name, Passed: check.Passed, Evidence: check.Evidence, ImplementationDigest: check.ImplementationDigest})
	}
	return e, e.ValidateEngineering()
}

func scopeWithCapture(root *contributionNode, key string, s ContributionStory, d *ContributionDiagnostics) ContributionStory {
	if root.field("capture") == nil {
		return s
	}
	var capture ContributionCapture
	if !root.decode("capture", &capture) {
		return s
	}
	evidence, err := capture.SnapshotEvidence()
	if err != nil {
		d.add("$.capture", "snapshot", "complete sorted snapshot and passing attributed checks", "invalid capture", err.Error())
		return s
	}
	var engineering EngineeringContribution
	if !root.decode("engineering", &engineering) {
		return s
	}
	if engineering.SnapshotID == "" {
		d.add("$.engineering.snapshot_id", "blank", "snapshot reference", "empty", "Name the captured snapshot.")
	}
	s.Checks = maps.Clone(s.Checks)
	if s.Checks == nil {
		s.Checks = map[string]RetainedContributionCheck{}
	}
	if s.SnapshotRetained && (s.SnapshotID != engineering.SnapshotID || s.ImplementationDigest != evidence.ImplementationDigest) {
		d.add("$.capture", "attribution", "existing immutable snapshot", "conflicting capture", "Use a new authorized snapshot reference for changed work.")
	}
	s.SnapshotID = engineering.SnapshotID
	s.ImplementationDigest = evidence.ImplementationDigest
	s.SnapshotRetained = true
	seen := map[string]bool{}
	for i, check := range capture.Checks {
		if strings.TrimSpace(check.ID) == "" || len(check.ID) > 256 || seen[check.ID] {
			d.add(fmt.Sprintf("$.capture.checks[%d].id", i), "identity", "unique nonblank check ID up to 256 bytes", "invalid ID", "Name each captured check once.")
		}
		seen[check.ID] = true
		facts := RetainedContributionCheck{Story: key, Revision: s.Revision, SnapshotID: s.SnapshotID, ImplementationDigest: s.ImplementationDigest, Executed: true, Passed: check.Passed, Retained: true}
		if old, ok := s.Checks[check.ID]; ok && old != facts {
			d.add(fmt.Sprintf("$.capture.checks[%d].id", i), "attribution", "unchanged retained check", "conflicting ID", "Use a new ID for changed execution.")
		}
		s.Checks[check.ID] = facts
	}
	return s
}
