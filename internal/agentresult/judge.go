package agentresult

import (
	"fmt"
	"strings"
)

// JudgeEvidence is factory-captured evidence. Authored output references only its ID.
type JudgeEvidence struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Reference string `json:"reference"`
	Digest    string `json:"digest"`
	Path      string `json:"path,omitempty"`
	Lines     int    `json:"lines,omitempty"`
	Available bool   `json:"available"`
	Essential bool   `json:"essential"`
	Gap       string `json:"gap,omitempty"`
}

// JudgeCitation names captured bytes, never an arbitrary URL or a live file.
type JudgeCitation struct {
	EvidenceID string `json:"evidence_id"`
	StartLine  int    `json:"start_line,omitempty"`
	EndLine    int    `json:"end_line,omitempty"`
}

// JudgeFinding explains a concern, its captured evidence, impact and correction.
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
type JudgeReport struct {
	Version     int            `json:"version"`
	Verdict     string         `json:"verdict"`
	Summary     string         `json:"summary"`
	Coverage    []string       `json:"coverage"`
	Uncertainty []string       `json:"uncertainty"`
	Findings    []JudgeFinding `json:"findings"`
}

// JudgeInput is the dedicated shared-result contribution envelope.
type JudgeInput struct {
	Report JudgeReport `json:"report"`
}

// DecodeJudge rejects unknown fields, duplicate fields and unchecked citations.
func DecodeJudge(data []byte, evidence []JudgeEvidence) (JudgeReport, Diagnostics) {
	var input JudgeInput
	d := Decode(data, &input)
	if !d.Valid {
		return JudgeReport{}, d
	}
	d = ValidateJudge(input.Report, evidence)
	if !d.Valid {
		return JudgeReport{}, d
	}
	return input.Report, d
}

// ValidateJudge keeps unavailable essential evidence and execution gaps from a clean verdict.
func ValidateJudge(r JudgeReport, evidence []JudgeEvidence) Diagnostics {
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
	captured := map[string]JudgeEvidence{}
	for _, e := range evidence {
		captured[e.ID] = e
		// Missing essential evidence rules out a clean verdict, not the concerns
		// that the available evidence already supports.
		if e.Essential && !e.Available && r.Verdict == "no_material_concerns" {
			fail("$.report.verdict", "Essential evidence is unavailable; report concerns_found or unable_to_assess.")
		}
		if e.Essential && !e.Available && r.Verdict == "concerns_found" && len(r.Uncertainty) == 0 {
			fail("$.report.uncertainty", "Essential evidence is unavailable; say what could not be checked.")
		}
	}
	if len(r.Coverage) == 0 {
		fail("$.report.coverage", "Name the captured evidence examined.")
	}
	sourceCovered := false
	for i, id := range r.Coverage {
		if id == "source" {
			sourceCovered = true
		}
		if e, ok := captured[id]; !ok || !e.Available {
			fail(fmt.Sprintf("$.report.coverage[%d]", i), "Coverage must identify available captured evidence.")
		}
	}
	if !sourceCovered && r.Verdict != "unable_to_assess" {
		fail("$.report.coverage", "Assess the pinned source before issuing an advisory verdict.")
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
			fail(p+".evidence", "Support each concern with captured evidence.")
		}
		for j, c := range f.Evidence {
			e, ok := captured[c.EvidenceID]
			if !ok || !e.Available {
				fail(fmt.Sprintf("%s.evidence[%d]", p, j), "Citations must identify available captured evidence.")
				continue
			}
			if c.StartLine < 0 || c.EndLine < 0 || (c.StartLine == 0 && c.EndLine != 0) || (c.StartLine > 0 && (c.EndLine < c.StartLine || c.EndLine > e.Lines)) {
				fail(fmt.Sprintf("%s.evidence[%d]", p, j), "Use a valid line range in the captured evidence.")
			}
		}
	}
	finish(&d)
	return d
}
