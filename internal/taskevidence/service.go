// Package taskevidence projects exact-task local evidence through a read-only boundary.
package taskevidence

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/connector"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

// Reader has no workflow, filesystem or mutation capability.
type Reader interface {
	ReadTaskEvidence(context.Context, int64, string, int, int) (store.TaskEvidencePage, error)
}

// Service builds sanitized, bounded evidence pages from local records.
type Service struct {
	reader    Reader
	redactor  *redact.Redactor
	now       func() time.Time
	hostRoots []string
}

// New composes the read boundary, shared redactor and clock.
func New(reader Reader, redactor *redact.Redactor, now func() time.Time, hostRoots ...string) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{reader, redactor, now, append([]string{"/Users", "/home", "/private", "/var", "/tmp"}, hostRoots...)}
}

// Evidence sanitizes complete units before applying the byte-page bound. A
// refused unit is represented explicitly and still advances the source offset.
func (s *Service) Evidence(ctx context.Context, target store.ConnectorGrantTarget, req connector.EvidenceRequest) (connector.EvidencePage, error) {
	if s.reader == nil || s.redactor == nil {
		return connector.EvidencePage{}, errors.New("task evidence unavailable")
	}
	raw, err := s.reader.ReadTaskEvidence(ctx, target.SessionID, req.Section, req.Offset, req.Limit)
	if err != nil {
		return connector.EvidencePage{}, errors.New("task evidence unavailable")
	}
	out := connector.EvidencePage{Revision: raw.Revision + ":" + s.redactor.PolicyID(), Items: []map[string]any{}, Sources: []map[string]any{}, Gaps: []string{}, HasMore: raw.HasMore, NextOffset: raw.NextOffset}
	for _, gap := range raw.Gaps {
		clean, err := s.clean(ctx, gap, "")
		if err != nil {
			return connector.EvidencePage{}, errors.New("task evidence redaction failed")
		}
		text, ok := clean.(string)
		if !ok {
			return connector.EvidencePage{}, errors.New("task evidence unavailable")
		}
		out.Gaps = append(out.Gaps, text)
	}
	for _, source := range raw.Sources {
		observed := make(map[string]any, len(source)+1)
		for key, value := range source {
			observed[key] = value
		}
		observed["read_at"] = s.now().UTC()
		clean, err := s.cleanMap(ctx, observed)
		if err != nil {
			return connector.EvidencePage{}, errors.New("task evidence redaction failed")
		}
		out.Sources = append(out.Sources, clean)
	}
	for i, item := range raw.Items {
		var clean map[string]any
		encoded, err := json.Marshal(item)
		if err != nil {
			return connector.EvidencePage{}, errors.New("task evidence unavailable")
		}
		if len(encoded) > 256<<10 {
			clean = map[string]any{"unavailable": true, "reason": "source_unit_exceeds_budget", "source_offset": req.Offset + i}
			out.Gaps = append(out.Gaps, "oversized_source_unit_unavailable")
		} else {
			clean, err = s.cleanMap(ctx, item)
			if err != nil {
				return connector.EvidencePage{}, errors.New("task evidence redaction failed")
			}
		}
		out.Items = append(out.Items, clean)
		encoded, err = json.Marshal(out)
		if err != nil {
			return connector.EvidencePage{}, errors.New("task evidence unavailable")
		}
		escaped, _ := json.Marshal(string(encoded))
		if len(encoded)+len(escaped) > 56<<10 {
			out.Items = out.Items[:len(out.Items)-1]
			if len(out.Items) == 0 {
				out.Items = append(out.Items, map[string]any{"unavailable": true, "reason": "source_unit_exceeds_page_budget", "source_offset": req.Offset + i})
				out.Gaps = append(out.Gaps, "oversized_source_unit_unavailable")
				out.NextOffset = req.Offset + i + 1
				out.HasMore = raw.HasMore || i+1 < len(raw.Items)
			} else {
				out.NextOffset = req.Offset + i
				out.HasMore = true
			}
			break
		}
	}
	if len(out.Items) == 0 && out.HasMore && out.NextOffset <= req.Offset {
		return connector.EvidencePage{}, errors.New("task evidence cannot advance")
	}
	return out, nil
}

func (s *Service) cleanMap(ctx context.Context, value map[string]any) (map[string]any, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var v any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err = d.Decode(&v); err != nil {
		return nil, err
	}
	clean, err := s.clean(ctx, v, "")
	if err != nil {
		return nil, err
	}
	result, ok := clean.(map[string]any)
	if !ok {
		return nil, errors.New("invalid sanitized evidence")
	}
	return result, nil
}
func (s *Service) clean(ctx context.Context, value any, key string) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch x := value.(type) {
	case string:
		if len(x) > 256<<10 {
			return nil, errors.New("source text exceeds budget")
		}
		// Links are display facts, never capabilities. Host-local and credentialed
		// URLs have no permitted outbound representation.
		if strings.Contains(strings.ToLower(key), "url") || strings.Contains(strings.ToLower(key), "link") {
			if x != "" && !permittedEvidenceLink(x) {
				return "", nil
			}
		}
		var linkErr error
		x, linkErr = s.protectLinks(ctx, x)
		if linkErr != nil {
			return nil, linkErr
		}
		if json.Valid([]byte(x)) {
			var inner any
			decoder := json.NewDecoder(strings.NewReader(x))
			decoder.UseNumber()
			if err := decoder.Decode(&inner); err != nil {
				return nil, err
			}
			safe, err := s.clean(ctx, inner, "")
			if err != nil {
				return nil, err
			}
			encoded, err := json.Marshal(safe)
			if err != nil {
				return nil, err
			}
			x = string(encoded)
		}
		b, err := s.redactor.ApplyReviewJSONDisplay(ctx, report.StripHostPaths(x, s.hostRoots), 512<<10)
		if err != nil || !b.Valid() || b.Truncated() {
			return nil, errors.New("redaction failed")
		}
		return b.String(), nil
	case map[string]any:
		out := map[string]any{}
		for k, v := range x {
			safeKey, e := s.clean(ctx, k, "")
			if e != nil {
				return nil, e
			}
			clean, e := s.clean(ctx, v, k)
			if e != nil {
				return nil, e
			}
			name, ok := safeKey.(string)
			if !ok {
				return nil, errors.New("invalid sanitized field")
			}
			if _, exists := out[name]; exists {
				return nil, fmt.Errorf("redacted key collision")
			}
			out[name] = clean
		}
		return out, nil
	case []any:
		out := make([]any, len(x))
		for i, v := range x {
			clean, e := s.clean(ctx, v, key)
			if e != nil {
				return nil, e
			}
			out[i] = clean
		}
		return out, nil
	default:
		return value, nil
	}
}

var evidenceURL = regexp.MustCompile("(?i)(?:https?://|file://)[^\\s<>()\"`]+")

func (s *Service) protectLinks(ctx context.Context, text string) (string, error) {
	var failed error
	out := evidenceURL.ReplaceAllStringFunc(text, func(raw string) string {
		u, err := url.Parse(raw)
		if err != nil || !permittedEvidenceLink(raw) {
			return "[link unavailable]"
		}
		decoded := raw
		for round := 0; round < 8; round++ {
			next, e := url.QueryUnescape(decoded)
			if e != nil {
				return "[link unavailable]"
			}
			protected, e := s.redactor.Prepare(ctx, next)
			if e != nil {
				failed = e
				return ""
			}
			if protected.String() != next {
				return "[link unavailable]"
			}
			if report.StripHostPaths(next, s.hostRoots) != next {
				return "[host path]"
			}
			if next == decoded {
				break
			}
			decoded = next
			if round == 7 {
				return "[link unavailable]"
			}
		}

		path, err := url.PathUnescape(u.EscapedPath())
		if err != nil {
			return "[link unavailable]"
		}
		if report.StripHostPaths(path, s.hostRoots) != path {
			return "[host path]"
		}
		for _, values := range u.Query() {
			for _, value := range values {
				if report.StripHostPaths(value, s.hostRoots) != value {
					return "[host path]"
				}
			}
		}
		return raw
	})
	return out, failed
}

func permittedEvidenceLink(text string) bool {
	u, err := url.Parse(text)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.RawQuery == "" && !u.ForceQuery
}
