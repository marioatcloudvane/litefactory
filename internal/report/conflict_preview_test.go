package report_test

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
)

func previewProposal() report.ConflictProposal {
	p := report.ConflictProposal{ID: 42, Format: 1, Model: "codex", BranchSHA: strings.Repeat("a", 40)}
	for _, path := range []string{"internal/config/config.go", "internal/daemon/daemon.go", "internal/daemon/setup.go", "internal/turn/execute.go"} {
		p.Files = append(p.Files, report.ConflictProposalFile{Path: path, Metrics: report.ConflictPreviewMetrics{BeforeBytes: 30000, AfterBytes: 1039, BeforeLines: 836, AfterLines: 16, Additions: 10, Deletions: 610}, Diff: strings.Repeat("- removed line\n+ replacement line\n", 120)})
	}
	return p
}

func TestRegionPreviewKeepsAllMetadataWhenDiffOmitted(t *testing.T) {
	t.Parallel()
	rep, err := report.New(report.Options{Redactor: redact.New(redact.Options{Environ: []string{}}), Limit: 3500})
	if err != nil {
		t.Fatal(err)
	}
	proposal := previewProposal()
	prepared, err := rep.PrepareConflictPreview(t.Context(), testRef, proposal)
	if err != nil {
		t.Fatal(err)
	}
	body, err := rep.RenderReply(t.Context(), report.Reply{Ref: testRef, Kind: report.ReplySuggestsResolution, ConflictPaths: []string{"source"}, ConflictProposal: &prepared})
	if err != nil {
		t.Fatal(err)
	}
	text := body.String()
	for _, f := range proposal.Files {
		if !strings.Contains(text, f.Path) {
			t.Fatalf("metadata omitted %s", f.Path)
		}
	}
	if strings.Count(text, "bytes 30000 -> 1039") != 4 || strings.Count(text, "logical lines 836 -> 16") != 4 || strings.Count(text, "+10 / -610") != 4 {
		t.Fatal("raw metrics lost")
	}
	for _, want := range []string{proposal.BranchSHA, "Diff content omitted", "sanitized and possibly incomplete", "no type checking", "parsing of other languages"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q", want)
		}
	}
	if len(text) > 3500 {
		t.Fatal("outbound limit exceeded")
	}
}

func TestRegionPreviewRefusesMetadataOverflowBeforeConfirmation(t *testing.T) {
	t.Parallel()
	rep, err := report.New(report.Options{Redactor: redact.New(redact.Options{Environ: []string{}}), Limit: 800})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rep.PrepareConflictPreview(t.Context(), testRef, previewProposal()); err == nil {
		t.Fatal("unreviewable proposal prepared")
	}
}

func TestRegionPreviewRedactionFencingAndRawMetrics(t *testing.T) {
	t.Parallel()
	token := "ghp_abcdefghijklmnopqrstuvwxyz1234567890"
	rep, err := report.New(report.Options{Redactor: redact.New(redact.Options{Environ: []string{"API_TOKEN=" + token}}), Limit: 16000})
	if err != nil {
		t.Fatal(err)
	}
	proposal := previewProposal()
	proposal.Files = proposal.Files[:1]
	proposal.Files[0].Path = "source`\n```\n" + token + "\x1b[31m.go"
	proposal.Files[0].Diff = "+ " + token + "\r\n+ ```\n+ \x1b[31mfield bool\n"
	prepared, err := rep.PrepareConflictPreview(t.Context(), testRef, proposal)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Files[0].Metrics != proposal.Files[0].Metrics {
		t.Fatal("sanitization changed raw statistics")
	}
	if strings.Contains(prepared.Files[0].Diff, token) || strings.ContainsAny(prepared.Files[0].Diff, "\r\x1b") {
		t.Fatal("unsanitized preview persisted")
	}
	body, err := rep.RenderReply(t.Context(), report.Reply{Ref: testRef, Kind: report.ReplySuggestsResolution, ConflictPaths: []string{"source"}, ConflictProposal: &prepared})
	if err != nil {
		t.Fatal(err)
	}
	text := body.String()
	if strings.Contains(text, token) || strings.ContainsAny(text, "\r\x1b") {
		t.Fatal("credential/control escaped")
	}
	if !strings.Contains(text, "````") || !strings.Contains(text, "bytes 30000 -> 1039") {
		t.Fatal("fencing or raw metrics lost")
	}
}
