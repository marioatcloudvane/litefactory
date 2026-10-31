package taskdiscussion

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

const resultPageBytes = 24 << 10

type resultPosition struct {
	AnswerOffset int  `json:"answer"`
	SourceOffset int  `json:"sources"`
	GapOffset    int  `json:"gaps"`
	SummaryDone  bool `json:"summary"`
}
type resultCursor struct {
	Position  resultPosition `json:"position"`
	Principal string         `json:"principal"`
	Job       string         `json:"job"`
	Result    string         `json:"result"`
	Expires   int64          `json:"expires"`
}

func (s *Service) sealResultCursor(position resultPosition, p Principal, job, result string) (string, error) {
	data, err := json.Marshal(resultCursor{position, hash(p), job, result, s.now().Add(15 * time.Minute).Unix()})
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, s.cursorKey[:])
	_, _ = mac.Write(data)
	return base64.RawURLEncoding.EncodeToString(data) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}
func (s *Service) openResultCursor(raw string, p Principal, job, result string) (resultPosition, error) {
	invalid := errors.New("invalid or expired advisory cursor; restart without cursor")
	if raw == "" {
		return resultPosition{}, nil
	}
	if len(raw) > 4096 {
		return resultPosition{}, invalid
	}
	left, right, ok := strings.Cut(raw, ".")
	if !ok {
		return resultPosition{}, invalid
	}
	data, err := base64.RawURLEncoding.DecodeString(left)
	if err != nil {
		return resultPosition{}, invalid
	}
	signature, err := base64.RawURLEncoding.DecodeString(right)
	if err != nil {
		return resultPosition{}, invalid
	}
	mac := hmac.New(sha256.New, s.cursorKey[:])
	_, _ = mac.Write(data)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return resultPosition{}, invalid
	}
	var c resultCursor
	if json.Unmarshal(data, &c) != nil || c.Principal != hash(p) || c.Job != job || c.Result != result || c.Expires <= s.now().Unix() {
		return resultPosition{}, invalid
	}
	return c.Position, nil
}

func (s *Service) publicBasis(ctx context.Context, b Basis) (Basis, error) {
	// Full artifacts and role captures remain private durable execution inputs.
	b.StoreGuard = ""
	b.Evidence = json.RawMessage(`{}`)
	b.Guidance = json.RawMessage(`{}`)
	raw, err := json.Marshal(b)
	if err != nil {
		return Basis{}, err
	}
	safe, err := s.protect(ctx, string(raw))
	if err != nil {
		return Basis{}, err
	}
	if err = json.Unmarshal([]byte(safe), &b); err != nil {
		return Basis{}, err
	}
	bounded := func(values []string) ([]string, bool) {
		out := []string{}
		size := 0
		omitted := false
		for _, value := range values {
			encoded, _ := json.Marshal(value)
			n := len(encoded) + 1
			if size+n > 4096 {
				omitted = true
				continue
			}
			size += n
			out = append(out, value)
		}
		return out, omitted
	}
	var omittedSources, omittedGaps bool
	b.Sources, omittedSources = bounded(b.Sources)
	b.Gaps, omittedGaps = bounded(b.Gaps)
	b.Gaps = append(b.Gaps, "private_capture_not_exported")
	if omittedSources || omittedGaps {
		b.Gaps = append(b.Gaps, "captured_basis_metadata_omitted")
	}
	raw, err = json.Marshal(b)
	if err != nil || len(raw) > 12<<10 {
		return Basis{}, errors.New("captured basis metadata exceeds client budget")
	}
	return b, nil
}

func resultPage(answer Answer, position resultPosition) (Answer, resultPosition, bool, error) {
	if position.AnswerOffset < 0 || position.AnswerOffset > len(answer.Answer) || position.SourceOffset < 0 || position.SourceOffset > len(answer.Sources) || position.GapOffset < 0 || position.GapOffset > len(answer.Gaps) || !utf8.ValidString(answer.Answer) || position.AnswerOffset < len(answer.Answer) && !utf8.RuneStart(answer.Answer[position.AnswerOffset]) {
		return Answer{}, position, false, errors.New("invalid advisory page position")
	}
	next := position
	page := Answer{Sources: []string{}, Gaps: []string{}}
	fits := func() bool { raw, err := json.Marshal(page); return err == nil && len(raw) <= resultPageBytes }
	end := min(len(answer.Answer), position.AnswerOffset+(8<<10))
	for end < len(answer.Answer) && !utf8.RuneStart(answer.Answer[end]) {
		end--
	}
	page.Answer = answer.Answer[position.AnswerOffset:end]
	next.AnswerOffset = end
	// JSON escaping can expand an otherwise bounded text chunk by six times.
	for !fits() && end > position.AnswerOffset {
		end = position.AnswerOffset + (end-position.AnswerOffset)/2
		for end > position.AnswerOffset && !utf8.RuneStart(answer.Answer[end]) {
			end--
		}
		page.Answer = answer.Answer[position.AnswerOffset:end]
		next.AnswerOffset = end
	}
	for next.SourceOffset < len(answer.Sources) {
		value := answer.Sources[next.SourceOffset]
		page.Sources = append(page.Sources, value)
		if !fits() {
			page.Sources = page.Sources[:len(page.Sources)-1]
			if encoded, _ := json.Marshal(value); len(encoded) > resultPageBytes/2 {
				page.Gaps = append(page.Gaps, "oversized_result_source_omitted")
				if !fits() {
					page.Gaps = page.Gaps[:len(page.Gaps)-1]
					break
				}
				next.SourceOffset++
				continue
			}
			break
		}
		next.SourceOffset++
	}
	for next.GapOffset < len(answer.Gaps) {
		value := answer.Gaps[next.GapOffset]
		page.Gaps = append(page.Gaps, value)
		if !fits() {
			page.Gaps = page.Gaps[:len(page.Gaps)-1]
			if encoded, _ := json.Marshal(value); len(encoded) > resultPageBytes/2 {
				page.Gaps = append(page.Gaps, "oversized_result_gap_omitted")
				if !fits() {
					page.Gaps = page.Gaps[:len(page.Gaps)-1]
					break
				}
				next.GapOffset++
				continue
			}
			break
		}
		next.GapOffset++
	}
	if !next.SummaryDone && answer.SummarySuggestion != nil {
		page.SummarySuggestion = answer.SummarySuggestion
		if fits() {
			next.SummaryDone = true
		} else {
			page.SummarySuggestion = nil
			raw, _ := json.Marshal(answer.SummarySuggestion)
			if len(raw) > resultPageBytes/2 {
				page.Gaps = append(page.Gaps, "oversized_summary_suggestion_omitted")
				if fits() {
					next.SummaryDone = true
				} else {
					page.Gaps = page.Gaps[:len(page.Gaps)-1]
				}
			}
		}
	} else if answer.SummarySuggestion == nil {
		next.SummaryDone = true
	}
	if !fits() {
		return Answer{}, position, false, errors.New("advisory page exceeds transport budget")
	}
	complete := next.AnswerOffset == len(answer.Answer) && next.SourceOffset == len(answer.Sources) && next.GapOffset == len(answer.Gaps) && next.SummaryDone
	if !complete && next == position {
		return Answer{}, position, false, errors.New("advisory page did not advance")
	}
	return page, next, complete, nil
}
