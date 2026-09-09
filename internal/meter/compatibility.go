package meter

import "github.com/cloudvane/litefactory/internal/agent"

// CapabilityEvidence is an adapter's review of an exact version/source pair.
// These flags require externally captured authentic provenance; synthetic parser
// success and configured defaults cannot establish actual model attribution.
// The caller owns the version allowlist and fixture/provenance verification.
type CapabilityEvidence struct {
	Version       string
	SourceFormat  string
	Supported     bool
	Authentic     bool
	Root          bool
	Parallel      bool
	Nested        bool
	ActualModels  bool
	ParentLinkage bool
	Provenance    string
}

// Coverage distinguishes unsupported contracts, missing authentic evidence and
// the fully evidenced capability. It is not a process-success predicate and
// does not assert any individual run has complete usage collection.
func (e CapabilityEvidence) Coverage() agent.UsageCoverage {
	c := agent.UsageCoverage{Version: e.Version, SourceFormat: e.SourceFormat, Status: agent.CollectionUnknown}
	if e.Version == "" || e.SourceFormat == "" || !e.Supported {
		c.Diagnostics = []string{"unsupported_source_version"}
		return c
	}
	if !e.Authentic || e.Provenance == "" {
		c.Diagnostics = []string{"missing_authentic_evidence"}
		return c
	}
	c.Status = agent.CollectionPartial
	if !e.Root {
		c.Diagnostics = append(c.Diagnostics, "missing_root_evidence")
	}
	if !e.Parallel {
		c.Diagnostics = append(c.Diagnostics, "missing_parallel_evidence")
	}
	if !e.Nested {
		c.Diagnostics = append(c.Diagnostics, "missing_nested_evidence")
	}
	if !e.ActualModels {
		c.Diagnostics = append(c.Diagnostics, "missing_actual_model_evidence")
	}
	if !e.ParentLinkage {
		c.Diagnostics = append(c.Diagnostics, "missing_parent_evidence")
	}
	if len(c.Diagnostics) == 0 {
		c.Status = agent.CollectionComplete
	}
	return c
}
