package brainstorm

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/agentresult"
)

var (
	answerURL      = regexp.MustCompile(`(?i)\b(?:[a-z][a-z0-9+.-]*://|www\.)\S+`)
	answerLink     = regexp.MustCompile(`\[[^\]]*\]\([^)]*\)`)
	answerTableRow = regexp.MustCompile(`(?m)^\s*\|.*\|\s*$`)
	answerHex      = regexp.MustCompile(`(?i)[0-9a-f]{7,}`)
)

// ValidateScanResult checks one authored research-shaped scan result: the
// shared research finding and statement checks, plus the spoken answer limits.
// It returns the decoded result only when every check passes.
func ValidateScanResult(data []byte) (RunResult, agentresult.Diagnostics) {
	if len(data) > MaxResultBytes {
		d := agentresult.Diagnostics{Complete: true, Diagnostics: []agentresult.Diagnostic{{Path: "$", Code: "limit", Expected: "at most 120 KiB", Actual: "oversized result", Message: "Keep the whole result within 120 KiB."}}, UncheckedPaths: []string{}}
		return RunResult{}, d
	}
	r, d := agentresult.Create(data, agentresult.Context{SchemaVersion: agentresult.SchemaVersion, WorkflowVersion: 1, Mode: "research", QuestionsAllowed: true})
	var authored struct {
		Result struct {
			Comment string `json:"comment"`
		} `json:"result"`
	}
	if json.Unmarshal(data, &authored) == nil {
		d.Diagnostics = append(d.Diagnostics, AnswerDiagnostics(authored.Result.Comment)...)
	}
	d.Valid = d.Complete && len(d.Diagnostics) == 0
	if !d.Valid {
		return RunResult{}, d
	}
	research, err := json.Marshal(r.Payload.Research)
	if err != nil {
		d.Valid = false
		return RunResult{}, d
	}
	out := RunResult{Answer: r.Comment, Research: research}
	for _, q := range r.Questions {
		out.Questions = append(out.Questions, q.Text)
	}
	return out, d
}

// AnswerDiagnostics reports one diagnostic per violated spoken answer rule.
func AnswerDiagnostics(answer string) []agentresult.Diagnostic {
	const path = "$.result.comment"
	var out []agentresult.Diagnostic
	add := func(code, expected, actual, message string) {
		out = append(out, agentresult.Diagnostic{Path: path, Code: code, Expected: expected, Actual: actual, Message: message})
	}
	if n := utf8.RuneCountInString(answer); n > MaxAnswerCodePoints {
		add("answer_length", "at most 1000 code points", "too long", "Shorten the spoken answer; the evidence belongs in the findings.")
	}
	if answerURL.MatchString(answer) {
		add("answer_url", "no URL", "URL", "Remove URLs from the spoken answer.")
	}
	if answerLink.MatchString(answer) {
		add("answer_link", "no markdown link", "markdown link", "Remove markdown links from the spoken answer.")
	}
	if answerTableRow.MatchString(answer) {
		add("answer_table", "no table row", "markdown table row", "Remove table rows from the spoken answer.")
	}
	if hexString(answer) {
		add("answer_hex", "no hex string of 7 or more characters", "hex string", "Remove commit hashes and other hex strings from the spoken answer.")
	}
	return out
}

// hexString finds a run of 7 or more hex characters mixing digits and letters,
// so plain spoken numbers and ordinary words are not mistaken for hashes.
func hexString(text string) bool {
	for _, run := range answerHex.FindAllString(text, -1) {
		if strings.ContainsAny(run, "0123456789") && strings.ContainsAny(strings.ToLower(run), "abcdef") {
			return true
		}
	}
	return false
}
