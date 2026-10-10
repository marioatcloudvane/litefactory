package ask

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/redact"
)

// Point is one claim with factory-owned fact handles, never model-written URLs.
type Point struct {
	Text    string   `json:"text"`
	Handles []string `json:"handles"`
}

// Answer is the only model output consumed by the answer service.
type Answer struct {
	Points        []Point  `json:"points"`
	Gaps          []string `json:"gaps"`
	Clarification string   `json:"needs_clarification,omitempty"`
}

// DecodeAnswer refuses excessive, unknown and trailing input before inspecting
// citations. A caller may supply one correction after this validation fails.
func DecodeAnswer(raw []byte, bundle Bundle) (Answer, error) {
	var answer Answer
	if len(raw) > 32<<10 || !utf8.Valid(raw) {
		return answer, errors.New("ask: invalid answer encoding or size")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&answer); err != nil {
		return answer, fmt.Errorf("ask: decode answer: %w", err)
	}
	var trailing json.RawMessage
	if err := d.Decode(&trailing); !errors.Is(err, io.EOF) {
		return answer, errors.New("ask: trailing answer input")
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return answer, errors.New("ask: answer must be an object")
	}
	return answer, ValidateAnswer(answer, bundle)
}

// ValidateAnswer checks every point, including ones the renderer may omit.
// An empty answer represents no supportable point, not a provider failure.
func ValidateAnswer(answer Answer, bundle Bundle) error {
	if len(answer.Points) > 20 || len(answer.Gaps) > 8 || len(answer.Clarification) > 512 || (answer.Clarification != "" && len(answer.Points) > 0) {
		return errors.New("ask: invalid answer shape")
	}
	known := make(map[string]bool, len(bundle.Facts))
	for _, fact := range bundle.Facts {
		known[fact.Handle] = true
	}
	var failures []error
	for i, point := range answer.Points {
		if strings.TrimSpace(point.Text) == "" || len(point.Text) > 8192 || len(point.Handles) < 1 || len(point.Handles) > 8 {
			failures = append(failures, fmt.Errorf("ask: invalid point %d", i+1))
		}
		for _, handle := range point.Handles {
			if !known[handle] {
				failures = append(failures, fmt.Errorf("ask: point %d cites unknown fact %q", i+1, handle))
			}
		}
	}
	for _, gap := range answer.Gaps {
		if strings.TrimSpace(gap) == "" || len(gap) > 512 {
			failures = append(failures, errors.New("ask: invalid answer gap"))
		}
	}
	return errors.Join(failures...)
}

// RenderOptions carries only ephemeral turn metadata and a mandatory redactor.
type RenderOptions struct {
	Question           string
	InheritedProject   bool
	HistoryUnavailable bool
	OlderContextCut    bool
	Redactor           *redact.Redactor
}

// RenderAnswer emits one bounded message with citations on every displayed
// point. It reserves source references and the as-of timestamp before shortening
// claim text; sources are never lost to a blind final text cut.
func RenderAnswer(ctx context.Context, answer Answer, bundle Bundle, options RenderOptions) (string, error) {
	if err := ValidateAnswer(answer, bundle); err != nil {
		return "", err
	}
	if options.Redactor == nil || bundle.CapturedAt.IsZero() {
		return "", errors.New("ask: reply protection or capture time is missing")
	}
	// Single-line quoting prevents a question from supplying a forged marker.
	question := strings.Join(strings.Fields(options.Question), " ")
	prefix := "> " + runeExcerpt(question, 160) + "\n> Project: " + runeExcerpt(bundle.Project, 100) + "\n[lf:ask:v1]\n"
	notes := "\nAs of " + bundle.CapturedAt.UTC().Format("2006-01-02T15:04:05Z") + "."
	if options.InheritedProject {
		notes += "\nProject carried over from the newest /ask turn."
	}
	if options.HistoryUnavailable {
		notes += "\nHistory unavailable; answered without it."
	}
	if options.OlderContextCut {
		notes += "\nOlder context left out."
	}
	gaps := append(append([]string(nil), bundle.Gaps...), answer.Gaps...)
	if len(gaps) > 0 {
		notes += "\nGaps: " + runeExcerpt(strings.Join(gaps, "; "), 220)
	}
	budget := min(1500, 1900-runeCount(prefix)-runeCount(notes)-len("\n(shortened)"))
	if budget < 64 {
		return "", errors.New("ask: reply metadata exceeds message budget")
	}
	text, shortened := "", false
	switch {
	case answer.Clarification != "":
		text = "Please rephrase your question: " + answer.Clarification
		shortened = runeCount(text) > budget
		text = runeExcerpt(text, budget)
	case len(answer.Points) == 0:
		text = "No source in the current project data supports an answer."
	default:
		facts := make(map[string]Fact, len(bundle.Facts))
		for _, fact := range bundle.Facts {
			facts[fact.Handle] = fact
		}
		for i, point := range answer.Points {
			seen := map[string]bool{}
			var sources []string
			for _, handle := range point.Handles {
				if seen[handle] {
					continue
				}
				seen[handle] = true
				f := facts[handle]
				ref := handle
				if f.Link != "" {
					ref = "[" + handle + "](" + f.Link + ")"
				}
				sources = append(sources, ref)
			}
			citation := " (" + strings.Join(sources, ", ") + ")"
			remaining := budget - runeCount(text) - runeCount(citation) - 3
			if remaining < 16 {
				shortened = true
				break
			}
			claim := strings.Join(strings.Fields(point.Text), " ")
			if runeCount(claim) > remaining {
				claim = runeExcerpt(claim, remaining)
				shortened = true
			}
			text += "- " + claim + citation + "\n"
			if shortened {
				break
			}
			if i < len(answer.Points)-1 && budget-runeCount(text) < 16 {
				shortened = true
			}
		}
		if text == "" {
			text = "The supported answer could not fit in one reply. Please ask a narrower question."
		}
	}
	if shortened {
		text += "\n(shortened)"
	}
	// Redactor limits are in bytes and reserve comment headroom. Apply with a
	// generous bound to prevent that generic truncation from removing sources;
	// the complete protected chat message has its own character check below.
	body, err := options.Redactor.Apply(ctx, prefix+text+notes, 64<<10)
	if err != nil {
		return "", err
	}
	// Protection may expand replacements. Refuse rather than cut citations away.
	safe := body.String()
	if runeCount(safe) > 2000 {
		return "", errors.New("ask: protected reply exceeds message limit")
	}
	return safe, nil
}

func runeCount(s string) int { return utf8.RuneCountInString(s) }

func runeExcerpt(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	if limit < 1 {
		return ""
	}
	return string(runes[:limit-1]) + "…"
}
