package connector

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/cloudvane/litefactory/internal/meterview"
	"github.com/cloudvane/litefactory/internal/store"
)

// UsageProvider materializes an allowlisted, sanitized, immutable local read.
// It has no credential, workflow or current-price capabilities.
type UsageProvider interface {
	Usage(context.Context, UsageRequest) (UsageProjection, error)
	PolicyID() string
}

// UsageRequest contains the server-authorized filter and resolved time bounds.
type UsageRequest struct {
	Repository string
	Filter     store.MeterFilter
	View       string
	By         []string
	Empty      bool
}

// UsageProjection is fully sanitized before it can enter the view cache.
type UsageProjection struct {
	Facts    map[string]any
	Items    []map[string]any
	Revision string
	Policy   string
}

type usageArguments struct {
	Scope      string   `json:"scope"`
	Repository string   `json:"repository,omitempty"`
	Issue      int64    `json:"issue,omitempty"`
	Period     string   `json:"period,omitempty"`
	From       string   `json:"from,omitempty"`
	To         string   `json:"to,omitempty"`
	Mode       string   `json:"mode,omitempty"`
	Agent      string   `json:"agent,omitempty"`
	Model      string   `json:"model,omitempty"`
	View       string   `json:"view,omitempty"`
	By         []string `json:"by,omitempty"`
	Limit      int      `json:"limit,omitempty"`
	Cursor     string   `json:"cursor,omitempty"`
}

type usageBounds struct {
	From          *time.Time `json:"from"`
	To            *time.Time `json:"to"`
	Zone          string     `json:"zone"`
	FromInclusive bool       `json:"from_inclusive"`
	ToExclusive   bool       `json:"to_exclusive"`
}

func parseUsageArguments(raw json.RawMessage) (usageArguments, error) {
	var a usageArguments
	d := json.NewDecoder(bytes.NewReader(raw))
	t, e := d.Token()
	if e != nil || t != json.Delim('{') {
		return a, errors.New("invalid usage arguments")
	}
	seen := map[string]bool{}
	for d.More() {
		t, e = d.Token()
		if e != nil {
			return a, e
		}
		k, ok := t.(string)
		if !ok || seen[k] || !slices.Contains([]string{"scope", "repository", "issue", "period", "from", "to", "mode", "agent", "model", "view", "by", "limit", "cursor"}, k) {
			return a, errors.New("duplicate argument")
		}
		seen[k] = true
		var v json.RawMessage
		if e = d.Decode(&v); e != nil || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return a, errors.New("invalid usage argument")
		}
	}
	if _, e = d.Token(); e != nil {
		return a, e
	}
	if _, e = d.Token(); !errors.Is(e, io.EOF) {
		return a, errors.New("trailing arguments")
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if e = d.Decode(&a); e != nil {
		return a, e
	}
	if seen["period"] && a.Period == "" || seen["from"] && a.From == "" || seen["to"] && a.To == "" || seen["view"] && a.View == "" {
		return a, errors.New("invalid empty selector")
	}
	if !slices.Contains([]string{"factory", "repository", "task"}, a.Scope) || len(a.Repository) > 512 || a.Issue < 0 || a.Limit < 0 || a.Limit > 50 || seen["limit"] && a.Limit == 0 || len(a.Cursor) > 4096 {
		return a, errors.New("invalid usage selector")
	}
	if a.Scope == "factory" && (a.Repository != "" || a.Issue != 0) || a.Scope == "repository" && (a.Repository == "" || a.Issue != 0) || a.Scope == "task" && (a.Repository == "" || a.Issue <= 0) {
		return a, errors.New("invalid scope selector")
	}
	if a.Repository != "" && !canonicalUsageRepository(a.Repository) {
		return a, errors.New("invalid repository")
	}
	if a.Period != "" && (a.From != "" || a.To != "") {
		return a, errors.New("mixed periods")
	}
	if a.Period != "" && !slices.Contains([]string{"today", "rolling_7_days", "rolling_30_days", "all_history"}, a.Period) {
		return a, errors.New("invalid period")
	}
	if a.View == "" {
		a.View = "summary"
	}
	if !slices.Contains([]string{"summary", "breakdown", "details"}, a.View) {
		return a, errors.New("invalid view")
	}
	if len(a.By) > 4 || a.View != "breakdown" && len(a.By) > 0 {
		return a, errors.New("invalid dimensions")
	}
	dims := map[string]bool{}
	for _, v := range a.By {
		if !slices.Contains(meterview.BreakdownDimensions, v) || dims[v] {
			return a, errors.New("invalid dimension")
		}
		dims[v] = true
	}
	if a.View == "breakdown" && len(a.By) == 0 {
		a.By = []string{"run"}
	}
	if a.Limit == 0 {
		a.Limit = 50
	}
	_, e = meterview.Validate(meterview.Request{Project: a.Repository, Task: a.Issue, Mode: a.Mode, Agent: a.Agent, Model: a.Model, From: a.From, To: a.To}, false)
	return a, e
}
func canonicalUsageRepository(v string) bool {
	forge, slug, ok := strings.Cut(v, ":")
	if !ok || forge == "" {
		return false
	}
	parts := strings.Split(slug, "/")
	if len(parts) != 2 {
		return false
	}
	for _, part := range append([]string{forge}, parts...) {
		if part == "" || part == "." || part == ".." {
			return false
		}
		for _, r := range part {
			if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && !strings.ContainsRune("._-", r) {
				return false
			}
		}
	}
	return true
}
func resolveUsagePeriod(a usageArguments, t time.Time) (store.MeterFilter, usageBounds, error) {
	f, e := meterview.Validate(meterview.Request{Project: a.Repository, Task: a.Issue, Mode: a.Mode, Agent: a.Agent, Model: a.Model, From: a.From, To: a.To}, false)
	if e != nil {
		return f, usageBounds{}, e
	}
	t = t.UTC()
	if a.From == "" && a.To == "" {
		p := a.Period
		if p == "" {
			p = "rolling_30_days"
		}
		var start time.Time
		switch p {
		case "today":
			start = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
		case "rolling_7_days":
			start = t.Add(-168 * time.Hour)
		case "rolling_30_days":
			start = t.Add(-720 * time.Hour)
		case "all_history":
		default:
			return f, usageBounds{}, errors.New("invalid period")
		}
		if p != "all_history" {
			f.TimeFrom = &start
			f.TimeTo = &t
		}
	}
	return f, usageBounds{f.TimeFrom, f.TimeTo, "UTC", true, true}, nil
}
func (a usageArguments) permission() string { return a.Scope + ":usage" }
func (a usageArguments) taskRef() string {
	if a.Scope != "task" {
		return ""
	}
	return fmt.Sprintf("%s#%d", a.Repository, a.Issue)
}
func (a usageArguments) identity() string {
	a.Cursor = ""
	b, _ := json.Marshal(a)
	sum := sha256.Sum256(b)
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

type usageCursor struct {
	Grant, View, Revision, Selection, Policy string
	Offset                                   int
	Expires                                  int64
}
type retainedUsage struct {
	id, grant, selection string
	captured, expires    time.Time
	projection           UsageProjection
	bounds               usageBounds
	arguments            usageArguments
	bytes                int
	busy                 bool
	order                uint64
}
type usageCache struct {
	mu           sync.Mutex
	views        map[string]*retainedUsage
	reservations map[string]int
	serial       uint64
}

func (c *usageCache) reserve(grant string, now time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.views == nil {
		c.views = map[string]*retainedUsage{}
		c.reservations = map[string]int{}
	}
	for id, v := range c.views {
		if !v.busy && !now.Before(v.expires) {
			delete(c.views, id)
		}
	}
	for {
		own := c.reservations[grant]
		reserved := 0
		for _, n := range c.reservations {
			reserved += n
		}
		for _, v := range c.views {
			if v.grant == grant {
				own++
			}
		}
		if len(c.views)+reserved < 4 && own < 2 {
			c.reservations[grant]++
			return nil
		}
		var oldest *retainedUsage
		for _, v := range c.views {
			if !v.busy && (own < 2 || v.grant == grant) && (oldest == nil || v.order < oldest.order) {
				oldest = v
			}
		}
		if oldest == nil {
			return errors.New("usage capacity unavailable")
		}
		delete(c.views, oldest.id)
	}
}
func (c *usageCache) release(grant string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reservations[grant]--
	if c.reservations[grant] == 0 {
		delete(c.reservations, grant)
	}
}
func (c *usageCache) retain(v *retainedUsage) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.serial++
	v.order = c.serial
	c.views[v.id] = v
}
func (c *usageCache) acquire(id, grant, selection string, now time.Time) (*retainedUsage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v := c.views[id]
	if v == nil || v.grant != grant || v.selection != selection || !now.Before(v.expires) || v.busy {
		return nil, errors.New("usage continuation unavailable")
	}
	v.busy = true
	return v, nil
}
func (c *usageCache) finish(v *retainedUsage, retire bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v.busy = false
	if retire {
		delete(c.views, v.id)
	}
}
func (s *Server) sealUsage(c usageCursor) (string, error) {
	b, e := json.Marshal(c)
	if e != nil {
		return "", e
	}
	n := make([]byte, s.cookie.NonceSize())
	if _, e = rand.Read(n); e != nil {
		return "", e
	}
	return base64.RawURLEncoding.EncodeToString(s.cookie.Seal(n, n, b, []byte("usage-v1"))), nil
}
func (s *Server) openUsage(raw string) (usageCursor, error) {
	var c usageCursor
	b, e := base64.RawURLEncoding.DecodeString(raw)
	if e != nil || len(b) < s.cookie.NonceSize() {
		return c, errors.New("invalid cursor")
	}
	b, e = s.cookie.Open(nil, b[:s.cookie.NonceSize()], b[s.cookie.NonceSize():], []byte("usage-v1"))
	if e != nil || json.Unmarshal(b, &c) != nil || c.Offset < 1 {
		return c, errors.New("invalid cursor")
	}
	return c, nil
}

func (s *Server) usageAccess(ctx context.Context, credential string, a usageArguments) (store.ConnectorGrant, error) {
	return s.db.CheckConnectorUsageAccess(ctx, digest(credential), s.cfg.OwnerID, s.cfg.ResourceURL, a.permission(), a.Repository, a.taskRef(), s.now())
}
func (s *Server) readUsage(ctx context.Context, credential string, a usageArguments) (result ToolResult, err error) {
	grant, err := s.usageAccess(ctx, credential, a)
	if err != nil {
		return result, err
	}
	observed := s.now().UTC()
	position := 0
	var v *retainedUsage
	if a.Cursor != "" {
		c, e := s.openUsage(a.Cursor)
		if e != nil || c.Grant != grant.ID || c.Selection != a.identity() || c.Expires <= observed.Unix() {
			return result, errors.New("usage continuation unavailable")
		}
		v, e = s.usageViews.acquire(c.View, grant.ID, a.identity(), observed)
		if e != nil {
			return result, e
		}
		defer func() { s.usageViews.finish(v, err != nil || !usageHasMore(result)) }()
		if c.Revision != v.projection.Revision || c.Policy != v.projection.Policy || c.Policy != s.usage.PolicyID() {
			return result, errors.New("usage continuation changed")
		}
		position = c.Offset
	} else {
		if e := s.usageViews.reserve(grant.ID, observed); e != nil {
			return result, e
		}
		defer s.usageViews.release(grant.ID)
		f, bounds, e := resolveUsagePeriod(a, observed)
		if e != nil {
			return result, e
		}
		if a.Scope != "factory" {
			if len(grant.UsageTargets) != 1 {
				return result, errors.New("usage target unavailable")
			}
			f.RepoID = grant.UsageTargets[0].RepoID
		}
		f.IncludeStories = a.Scope == "task"
		p, e := s.usage.Usage(ctx, UsageRequest{Repository: a.Repository, Filter: f, View: a.View, By: a.By, Empty: f.TimeFrom != nil && f.TimeTo != nil && f.TimeFrom.Equal(*f.TimeTo)})
		if e != nil {
			return result, e
		}
		b, e := json.Marshal(p)
		if e != nil || len(b) > 16<<20 || p.Revision == "" || p.Policy == "" {
			return result, errors.New("usage projection unavailable")
		}
		// JSON round-trip takes ownership of every nested map and slice.
		var cloned UsageProjection
		if e = json.Unmarshal(b, &cloned); e != nil {
			return result, e
		}
		p = cloned
		id, e := randomCredential()
		if e != nil {
			return result, e
		}
		v = &retainedUsage{id: id, grant: grant.ID, selection: a.identity(), captured: observed, expires: observed.Add(5 * time.Minute), projection: p, bounds: bounds, arguments: a, bytes: len(b)}
	}
	if position < 0 || position > len(v.projection.Items) {
		return result, errors.New("invalid continuation position")
	}
	end := min(position+a.Limit, len(v.projection.Items))
	if a.View == "summary" {
		end = position
	}
	for {
		more := end < len(v.projection.Items) && a.View != "summary"
		next := ""
		if more {
			next, err = s.sealUsage(usageCursor{Grant: grant.ID, View: v.id, Revision: v.projection.Revision, Selection: a.identity(), Policy: v.projection.Policy, Offset: end, Expires: v.expires.Unix()})
			if err != nil {
				return result, err
			}
		}
		facts := make(map[string]any, len(v.projection.Facts)+16)
		for k, x := range v.projection.Facts {
			facts[k] = x
		}
		facts["version"] = 1
		facts["scope"] = a.Scope
		// Canonical attribution was sanitized by the provider before buffering.
		if _, ok := facts["repository"]; !ok {
			facts["repository"] = ""
		}
		facts["issue"] = a.Issue
		facts["family_policy"] = "none"
		if a.Scope == "task" {
			facts["family_policy"] = "persisted_report_family_including_retired"
		}
		facts["bounds"] = v.bounds
		facts["generated_at"] = v.captured
		facts["captured_at"] = v.captured
		facts["revision"] = v.projection.Revision
		facts["items"] = v.projection.Items[position:end]
		facts["has_more"] = more
		facts["next_cursor"] = next
		result, err = DiscussionResult(usageSummary(facts), facts)
		if err == nil {
			encoded, _ := json.Marshal(result)
			if len(encoded) <= EvidencePageBytes {
				break
			}
		}
		if end <= position {
			return ToolResult{}, errors.New("usage page exceeds budget")
		}
		end--
		if end == position && a.View != "summary" && position < len(v.projection.Items) {
			return ToolResult{}, errors.New("usage item exceeds page budget")
		}
	}
	// Recheck immediately before returning the buffered projection; the transport
	// performs another check immediately before serializing the final envelope.
	if _, err = s.usageAccess(ctx, credential, a); err != nil || ctx.Err() != nil || v.projection.Policy != s.usage.PolicyID() || !s.now().Before(v.expires) {
		return ToolResult{}, errors.New("usage access unavailable")
	}
	if a.Cursor == "" && usageHasMore(result) {
		s.usageViews.retain(v)
	}
	return result, nil
}
func usageHasMore(r ToolResult) bool {
	var v struct {
		Evidence struct {
			More bool `json:"has_more"`
		} `json:"evidence"`
	}
	_ = json.Unmarshal(r.StructuredContent, &v)
	return v.Evidence.More
}
func usageSummary(facts map[string]any) string {
	raw, _ := json.Marshal(facts["totals"])
	var t struct {
		Estimated, Subtotal *string
		Collection, Pricing string
	}
	var m map[string]json.RawMessage
	_ = json.Unmarshal(raw, &m)
	_ = json.Unmarshal(m["estimated_usd"], &t.Estimated)
	_ = json.Unmarshal(m["priced_subtotal_usd"], &t.Subtotal)
	_ = json.Unmarshal(m["collection_status"], &t.Collection)
	_ = json.Unmarshal(m["pricing_status"], &t.Pricing)
	text := "Recorded usage for " + fmt.Sprint(facts["scope"])
	if r, _ := facts["repository"].(string); r != "" {
		text += " " + r
	}
	text += ". " + meterview.CostText(t.Estimated, t.Subtotal, t.Collection, t.Pricing) + ". Collection: " + t.Collection + "; pricing: " + t.Pricing + ". Helper overhead excluded."
	if count, ok := facts["records"].(float64); ok && count == 0 {
		text += " No records in this period; this is not measured zero."
	}
	if gaps, ok := facts["gaps"].([]any); ok && len(gaps) > 0 {
		text += " Recorded gaps: "
		for i, g := range gaps {
			if i > 0 {
				text += ", "
			}
			text += fmt.Sprint(g)
		}
	}
	return text
}

func usageInputSchema() map[string]any {
	properties := map[string]any{
		"scope":      map[string]any{"type": "string", "enum": []string{"factory", "repository", "task"}},
		"repository": map[string]any{"type": "string", "maxLength": 512}, "issue": map[string]any{"type": "integer", "minimum": 1},
		"period": map[string]any{"type": "string", "enum": []string{"today", "rolling_7_days", "rolling_30_days", "all_history"}},
		"from":   map[string]any{"type": "string", "format": "date-time"}, "to": map[string]any{"type": "string", "format": "date-time"},
		"mode": map[string]any{"type": "string", "enum": []string{"research", "plan", "implement", "spike"}}, "agent": map[string]any{"type": "string", "enum": []string{"claude", "codex"}}, "model": map[string]any{"type": "string", "maxLength": 256},
		"view": map[string]any{"type": "string", "enum": []string{"summary", "breakdown", "details"}}, "by": map[string]any{"type": "array", "maxItems": 4, "uniqueItems": true, "items": map[string]any{"type": "string", "enum": meterview.BreakdownDimensions}}, "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 50}, "cursor": map[string]any{"type": "string", "maxLength": 4096},
	}
	return map[string]any{"type": "object", "required": []string{"scope"}, "additionalProperties": false, "properties": properties}
}
func usageOutputSchema() map[string]any {
	return map[string]any{"type": "object", "required": []string{"summary", "evidence"}, "additionalProperties": false, "properties": map[string]any{"summary": map[string]any{"type": "string", "maxLength": 1000}, "evidence": map[string]any{"type": "object", "required": []string{"version", "scope", "bounds", "revision", "totals", "records", "price_basis", "gaps", "items", "has_more", "next_cursor"}, "properties": map[string]any{"items": map[string]any{"type": "array", "maxItems": 50, "items": map[string]any{"type": "object"}}, "has_more": map[string]any{"type": "boolean"}, "next_cursor": map[string]any{"type": "string"}}}}}
}

func usageFailureResult() ToolResult {
	r, _ := DiscussionResult("Recorded usage is unavailable. Narrow the selection, or restart without a cursor if the previous view has ended.", map[string]any{"version": 1, "scope": "unavailable", "repository": "", "issue": 0, "bounds": usageBounds{Zone: "UTC", FromInclusive: true, ToExclusive: true}, "revision": "", "totals": map[string]any{"estimated_usd": nil, "priced_subtotal_usd": nil, "collection_status": "unknown", "pricing_status": "unknown"}, "records": 0, "price_basis": []any{}, "gaps": []string{"usage_unavailable_restart_or_narrow"}, "items": []any{}, "has_more": false, "next_cursor": ""})
	r.IsError = true
	return r
}
