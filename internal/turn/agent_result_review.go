package turn

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/store"
)

// AgentResultReview is an unaccepted display projection, never a candidate for
// adoption. Source and diagnostics remain the exact retained submission; Display
// is escaped text so submitted Markdown cannot become factory instructions.
// Owner delivery must still apply the normal outbound redactor.
type AgentResultReview struct {
	RequestID         string
	RequestHash       string
	Source            []byte
	Diagnostics       agentresult.UpdateDiagnostics
	Display           string
	DisplaySimplified bool
}

// Review reads only a rejected request belonging to this exact trusted attempt.
// Complete read authority is necessary because malformed input cannot be safely
// projected onto an individual field grant.
func (s *AgentResultService) Review(ctx context.Context, requestID string) (AgentResultReview, error) {
	if !s.completeReadGrant() {
		return AgentResultReview{}, store.ErrResultAuthority
	}
	r, err := s.db.GetResultReceipt(ctx, s.binding, s.binding.RunID, requestID)
	if err != nil {
		return AgentResultReview{}, err
	}
	return s.review(r)
}

func (s *AgentResultService) review(r store.ResultReceipt) (AgentResultReview, error) {
	if !s.completeReadGrant() || r.RunID != s.binding.RunID || r.Attempt != s.binding.Attempt || r.ResultID != s.binding.ResultID {
		return AgentResultReview{}, store.ErrResultAuthority
	}
	if r.InputState != "rejected" || r.AppliedRevision != 0 {
		return AgentResultReview{}, fmt.Errorf("review requires an unaccepted retained request")
	}
	if len(r.Payload) > store.MaxResultInputBytes {
		return AgentResultReview{}, fmt.Errorf("retained review exceeds contribution ceiling")
	}
	v := AgentResultReview{RequestID: r.RequestID, RequestHash: r.RequestHash, Source: bytes.Clone(r.Payload)}
	if err := json.Unmarshal(r.Diagnostics, &v.Diagnostics); err != nil {
		return AgentResultReview{}, fmt.Errorf("decode review diagnostics: %w", err)
	}
	text, simplified := reviewDisplayText(r.Payload)
	v.Display, v.DisplaySimplified = "<pre>"+html.EscapeString(text)+"</pre>", simplified
	return v, nil
}

// Only a single, exact enclosing fence is a display wrapper. Nested fences,
// extra trailing text and unknown language labels stay literal. No JSON member
// is decoded here, so duplicate keys and unknown content cannot disappear.
func reviewDisplayText(source []byte) (string, bool) {
	if !utf8.Valid(source) {
		return strconv.Quote(string(source)), false
	}
	original := string(source)
	text := strings.TrimSpace(original)
	if line, rest, ok := strings.Cut(text, "\n"); ok {
		opening := strings.TrimSuffix(line, "\r")
		for _, fence := range []string{"```", "~~~"} {
			if opening != fence && opening != fence+"json" {
				continue
			}
			body, closing, found := strings.Cut(rest, "\n"+fence)
			if found && (closing == "" || closing == "\r") && !strings.Contains(body, fence) {
				text = strings.TrimSpace(body)
			}
		}
	}
	return text, text != original
}

// Markdown keeps every validation limitation next to the complete submitted
// content. Neither receipt recovery nor successful delivery changes its status.
func (v AgentResultReview) Markdown() string {
	diagnostics, err := json.MarshalIndent(v.Diagnostics, "", "  ")
	if err != nil {
		return "Unaccepted retained result: diagnostics could not be rendered; review is incomplete."
	}
	return "## Retained result — unaccepted, for review only\n\nThis submission did not satisfy workflow validation. Review does not grant approval or advance workflow gates.\n\nRequest: <code>" + html.EscapeString(v.RequestID) + "</code>\n\n### Validation limitations\n\n<pre>" + html.EscapeString(string(diagnostics)) + "</pre>\n\n### Submitted content\n\n" + v.Display
}

func (s *AgentResultService) latestReview(ctx context.Context) (AgentResultReview, error) {
	if !s.completeReadGrant() {
		return AgentResultReview{}, store.ErrResultAuthority
	}
	receipt, err := s.db.LatestRejectedResultReceipt(ctx, s.binding)
	if err != nil {
		return AgentResultReview{}, err
	}
	return s.review(receipt)
}

// attachRejectedReview only supplies report text. Terminal text never becomes
// review input, and failure to read/render review leaves the existing result.
func (p *preparedMCP) attachRejectedReview(ctx context.Context, result *agent.RunResult, appendToAccepted bool) {
	if p.service == nil {
		return
	}
	review, err := p.service.latestReview(ctx)
	if errors.Is(err, store.ErrNotFound) {
		return
	}
	if err != nil {
		result.Diagnostic = strings.TrimSpace(result.Diagnostic + "\nRetained review is unavailable; complete review delivery cannot be claimed: " + err.Error())
		return
	}
	if appendToAccepted {
		result.ResultText = strings.TrimSpace(result.ResultText + "\n\n" + review.Markdown())
	} else {
		result.ResultText = review.Markdown()
	}
}
