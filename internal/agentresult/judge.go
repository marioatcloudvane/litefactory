package agentresult

import (
	"fmt"
	"path"
	"strings"
)

// JudgeCitation names a file the judge read: a path relative to the repository
// root at the pinned commit, or the file name of one of the factory context
// files, optionally narrowed to a line range. It is provenance for the saved
// report, never a link the factory follows.
type JudgeCitation struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
}

// JudgeFinding explains a concern, the files it rests on, impact and correction.
// Title is a short name for the finding; reports saved before it existed omit it.
type JudgeFinding struct {
	Title      string          `json:"title,omitempty"`
	Severity   string          `json:"severity"`
	Concern    string          `json:"concern"`
	Evidence   []JudgeCitation `json:"evidence"`
	Impact     string          `json:"impact"`
	Correction string          `json:"correction"`
}

// JudgeReport is advisory content without source or workflow authority.
// Coverage optionally lists the files the judge read, as citation paths.
type JudgeReport struct {
	Version     int            `json:"version"`
	Verdict     string         `json:"verdict"`
	Summary     string         `json:"summary"`
	Coverage    []string       `json:"coverage,omitempty"`
	Uncertainty []string       `json:"uncertainty"`
	Findings    []JudgeFinding `json:"findings"`
}

// JudgeInput is the dedicated shared-result contribution envelope.
type JudgeInput struct {
	Report JudgeReport `json:"report"`
}

// DecodeJudge rejects unknown fields, duplicate fields and malformed citations.
func DecodeJudge(data []byte) (JudgeReport, Diagnostics) {
	var input JudgeInput
	d := Decode(data, &input)
	if !d.Valid {
		return JudgeReport{}, d
	}
	d = ValidateJudge(input.Report)
	if !d.Valid {
		return JudgeReport{}, d
	}
	return input.Report, d
}

// judgePath accepts a slash-separated path inside the repository or a context
// file name. Absolute paths, parent traversal and backslashes are refused.
func judgePath(p string) bool {
	if strings.TrimSpace(p) != p || p == "" || len(p) > 1024 || strings.ContainsAny(p, "\\\x00\r\n") || strings.HasPrefix(p, "/") {
		return false
	}
	clean := path.Clean(p)
	return clean == p && clean != "." && clean != ".." && !strings.HasPrefix(clean, "../")
}

// ValidateJudge checks the report shape; what the judge read is its own call.
func ValidateJudge(r JudgeReport) Diagnostics {
	d := newDiagnostics()
	fail := func(path, message string) { add(&d, path, "judge", "valid advisory assessment", "invalid", message) }
	if r.Version != 1 {
		fail("$.report.version", "Use judge report version 1.")
	}
	if strings.TrimSpace(r.Summary) == "" {
		fail("$.report.summary", "Explain the assessment.")
	}
	switch r.Verdict {
	case "no_material_concerns":
		if len(r.Findings) != 0 {
			fail("$.report.findings", "A clean assessment cannot contain concerns.")
		}
	case "concerns_found":
		if len(r.Findings) == 0 {
			fail("$.report.findings", "Explain at least one concern.")
		}
	case "unable_to_assess":
		if len(r.Uncertainty) == 0 {
			fail("$.report.uncertainty", "Explain why assessment is unavailable.")
		}
	default:
		fail("$.report.verdict", "Choose no_material_concerns, concerns_found or unable_to_assess.")
	}
	for i, p := range r.Coverage {
		if !judgePath(p) {
			fail(fmt.Sprintf("$.report.coverage[%d]", i), "Name a file by its path relative to the repository root, or a context file by its file name.")
		}
	}
	for i, f := range r.Findings {
		p := fmt.Sprintf("$.report.findings[%d]", i)
		switch f.Severity {
		case "low", "medium", "high", "critical":
		default:
			fail(p+".severity", "Choose low, medium, high or critical.")
		}
		if len(f.Title) > 120 || strings.ContainsAny(f.Title, "\n\r") {
			fail(p+".title", "Use a one-line title of at most 120 characters.")
		}
		if strings.TrimSpace(f.Concern) == "" || strings.TrimSpace(f.Impact) == "" || strings.TrimSpace(f.Correction) == "" {
			fail(p, "Each concern needs an explanation, impact and suggested correction.")
		}
		if len(f.Evidence) == 0 {
			fail(p+".evidence", "Cite the files each concern rests on.")
		}
		for j, c := range f.Evidence {
			if !judgePath(c.Path) {
				fail(fmt.Sprintf("%s.evidence[%d].path", p, j), "Name a file by its path relative to the repository root, or a context file by its file name.")
			}
			if c.StartLine < 0 || c.EndLine < 0 || (c.StartLine == 0 && c.EndLine != 0) || (c.StartLine > 0 && c.EndLine != 0 && c.EndLine < c.StartLine) {
				fail(fmt.Sprintf("%s.evidence[%d]", p, j), "Use a valid line range: start_line, then an end_line that is not before it.")
			}
		}
	}
	finish(&d)
	return d
}
