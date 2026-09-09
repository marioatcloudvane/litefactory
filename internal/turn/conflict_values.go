package turn

import (
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

func storedRegionFile(v git.ValidatedConflict) store.ConflictProposalFile {
	d := store.ConflictFileDetails{OriginalDigest: v.OriginalDigest, Mode: v.Mode, ResultDigest: v.ResultDigest,
		Metrics: store.ConflictFileMetrics(v.Metrics)}
	for _, r := range v.Regions {
		d.Regions = append(d.Regions, store.ConflictRegionResolution{ID: r.ID, Resolution: r.Resolution})
	}
	return store.ConflictProposalFile{Path: v.Path, Details: d}
}
func appliedRegionFile(p store.ConflictProposal, f store.ConflictProposalFile, limit int) git.ResolvedFile {
	v := git.ValidatedConflict{Path: f.Path, OriginalDigest: f.Details.OriginalDigest, Mode: f.Details.Mode, ResultDigest: f.Details.ResultDigest, Metrics: git.ConflictMetrics(f.Details.Metrics)}
	for _, r := range f.Details.Regions {
		v.Regions = append(v.Regions, git.RegionResolution{ID: r.ID, Resolution: r.Resolution})
	}
	return git.ResolvedFile{Path: f.Path, Format: p.Format, BranchSHA: p.BranchSHA, BaseSHA: p.BaseSHA, ContentLimit: limit, Validated: v}
}

func rawConflictPreview(values []git.ValidatedConflict, model, branchSHA string) report.ConflictProposal {
	p := report.ConflictProposal{Format: 1, Model: model, BranchSHA: branchSHA}
	for _, v := range values {
		p.Files = append(p.Files, report.ConflictProposalFile{Path: v.Path, Diff: v.Diff, Metrics: report.ConflictPreviewMetrics(v.Metrics)})
	}
	return p
}
