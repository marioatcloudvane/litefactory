package standards

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/store"
)

// RuntimeStore exposes only retained run data and published immutable reads.
type RuntimeStore interface {
	GetStandardsRevision(context.Context, int64) (store.StandardsRevision, error)
	GetStandardsProposal(context.Context, string, int64) (store.StandardsProposalVersion, error)
	GetStandardsRunBinding(context.Context, string) (store.StandardsRunBinding, error)
	SaveStandardsRunBinding(context.Context, store.StandardsRunBinding) (store.StandardsRunBinding, error)
	ListStandardsRunSelections(context.Context, string, string) ([]store.StandardsRunSelection, error)
	SaveStandardsRunSelection(context.Context, store.StandardsRunSelection, int64) (store.StandardsRunSelection, error)
	AppendStandardsRunObservation(context.Context, store.StandardsRunObservation) error
	ListStandardsRunObservations(context.Context, string, string) ([]store.StandardsRunObservation, error)
}

// Runtime is constructed with factory-owned invocation and consumer identities.
type Runtime struct {
	OutputLimit                       int // Optional lower encoded MCP page limit for standalone consumers.
	Store                             RuntimeStore
	RootRunID, ConsumerKey, AttemptID string
}

// RuntimeStatus separates retained integrity, choice and delivery.
type RuntimeStatus struct {
	Binding                  store.StandardsRunBinding    `json:"binding"`
	Integrity                string                       `json:"integrity"`
	Reason                   string                       `json:"reason,omitempty"`
	SelectionReason          string                       `json:"selection_reason,omitempty"`
	SelectionStatus          string                       `json:"selection_status"`
	RetrievalStatus          string                       `json:"retrieval_status"`
	Selection                *store.StandardsRunSelection `json:"selection,omitempty"`
	CategoryCount            int                          `json:"category_count"`
	StandardCount            int                          `json:"standard_count"`
	StatusPersistenceFailure string                       `json:"status_persistence_failure,omitempty"`
	VerifiedAt               time.Time                    `json:"verified_at"`
}

// RunInspection is read-only fresh integrity plus all retained history.
type RunInspection struct {
	Status       RuntimeStatus                   `json:"status"`
	Selections   []store.StandardsRunSelection   `json:"selections"`
	Observations []store.StandardsRunObservation `json:"observations"`
	Consumers    []ConsumerInspection            `json:"consumers"`
}

// CategoryPage offers a bounded discovery page from the pinned book.
type CategoryPage struct {
	Status              RuntimeStatus  `json:"status"`
	Categories          []Category     `json:"categories"`
	SelectedCategoryIDs []string       `json:"selected_category_ids"`
	StandardCounts      map[string]int `json:"standard_counts"`
	Load                Load           `json:"load"`
	NextCursor          string         `json:"next_cursor,omitempty"`
}

// StandardManifest describes one entry in the streamed selected manifest.
type StandardManifest struct {
	ID     string `json:"id"`
	Rule   string `json:"rule"`
	Digest string `json:"digest"`
	Bytes  int    `json:"bytes"`
}

// ReadPage carries exact bytes of rendered standards and explicit continuation markers.
type ReadPage struct {
	Manifest         []StandardManifest `json:"manifest,omitempty"`
	Status           RuntimeStatus      `json:"status"`
	SelectionVersion int64              `json:"selection_version"`
	SelectionDigest  string             `json:"selection_digest"`
	StandardID       string             `json:"standard_id,omitempty"`
	Rule             string             `json:"rule,omitempty"`
	Body             string             `json:"body"`
	BodyDigest       string             `json:"body_digest,omitempty"`
	Start            int                `json:"start"`
	End              int                `json:"end"`
	Total            int                `json:"total"`
	Complete         bool               `json:"complete"`
	NextCursor       string             `json:"next_cursor,omitempty"`
}
type runtimeCursor struct {
	Root, Consumer, Digest, Selection string
	Index, Offset                     int
}

func encodeCursor(c runtimeCursor) string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}
func (r *Runtime) decodeCursor(raw, selection string) (runtimeCursor, error) {
	c := runtimeCursor{Root: r.RootRunID, Consumer: r.ConsumerKey, Selection: selection}
	if raw == "" {
		return c, nil
	}
	b, e := base64.RawURLEncoding.DecodeString(raw)
	if e != nil {
		return c, ErrInvalid
	}
	if e = json.Unmarshal(b, &c); e != nil || c.Root != r.RootRunID || c.Consumer != r.ConsumerKey || c.Selection != selection || c.Index < 0 || c.Offset < 0 {
		return c, ErrInvalid
	}
	return c, nil
}

// Bind initializes only a new invocation; resumed missing bindings stay unavailable.
func (r *Runtime) Bind(ctx context.Context, allowInitial bool) (RuntimeStatus, error) {
	_, err := r.Store.GetStandardsRunBinding(ctx, r.RootRunID)
	switch {
	case errors.Is(err, store.ErrNotFound) && allowInitial:
		b := store.StandardsRunBinding{RootRunID: r.RootRunID, Source: "local-published-book", Outcome: "unavailable"}
		rev, e := r.Store.GetStandardsRevision(ctx, 0)
		if e == nil {
			if rev.Publication == nil {
				e = store.ErrStandardsCorrupt
			} else {
				p, pe := r.Store.GetStandardsProposal(ctx, rev.Publication.ProposalID, rev.Publication.Version)
				if pe != nil {
					e = pe
				} else if p.SchemaVersion != 1 || changeDigest(p.BaseRevision, p.Operations) != p.ChangeDigest {
					e = store.ErrStandardsCorrupt
				}
			}
		}

		b.RevisionID = rev.ID
		b.SchemaVersion = rev.SchemaVersion
		b.Digest = rev.Digest
		if e != nil {
			b.Reason = boundedReason(e.Error())
		} else {
			b.RevisionID = rev.ID
			b.SchemaVersion = rev.SchemaVersion
			b.Digest = rev.Digest
			raw, _ := json.Marshal(canonical(rev.Snapshot))
			b.SnapshotBytes = raw
			b.Outcome = "verified"
			if len(raw) > 16<<20 {
				b.SnapshotBytes = nil
				b.Outcome = "unavailable"
				b.Reason = "canonical snapshot exceeds 16 MiB capture limit"
			} else if _, state, reason := verifyBinding(b); state != "verified" {
				b.Outcome = state
				b.Reason = reason
			}
		}
		if _, err = r.Store.SaveStandardsRunBinding(ctx, b); err != nil {
			return RuntimeStatus{Binding: store.StandardsRunBinding{RootRunID: r.RootRunID}, Integrity: "unavailable", Reason: "binding status persistence failure", StatusPersistenceFailure: boundedReason(err.Error())}, nil
		}

	case errors.Is(err, store.ErrNotFound):
		unavailable := store.StandardsRunBinding{RootRunID: r.RootRunID, Source: "local-published-book", Outcome: "unavailable", Reason: "retained binding is missing; current book will not be substituted"}
		if _, err = r.Store.SaveStandardsRunBinding(ctx, unavailable); err != nil {
			return RuntimeStatus{Binding: store.StandardsRunBinding{RootRunID: r.RootRunID}, Integrity: "unavailable", Reason: "binding status persistence failure", StatusPersistenceFailure: boundedReason(err.Error())}, nil
		}
	case err != nil && !errors.Is(err, store.ErrStandardsCorrupt):
		return RuntimeStatus{}, err
	}
	s, err := r.Status(ctx)
	if err == nil {
		e := r.observe(ctx, store.StandardsRunObservation{Kind: "verification", Outcome: s.Integrity, Reason: s.Reason})
		if e != nil {
			s.StatusPersistenceFailure = boundedReason(e.Error())
		}
	}
	return s, err
}
func boundedReason(s string) string {
	if len(s) > 512 {
		s = s[:512]
		for !utf8.ValidString(s) {
			s = s[:len(s)-1]
		}
	}
	return s
}
func verifyBinding(b store.StandardsRunBinding) (Snapshot, string, string) {
	if b.Outcome != "verified" {
		return Snapshot{}, b.Outcome, b.Reason
	}
	if b.SchemaVersion != 1 {
		return Snapshot{}, "unsupported-schema", "retained book schema is unsupported"
	}
	var snap Snapshot
	raw := b.SnapshotBytes
	if raw == nil {
		raw = []byte(b.Canonical)
	}
	if err := json.Unmarshal(raw, &snap); err != nil {
		return Snapshot{}, "corrupt", "retained canonical bytes cannot be decoded"
	}
	canonicalBytes, _ := json.Marshal(canonical(snap))
	if string(raw) != string(canonicalBytes) || snapshotDigest(snap) != b.Digest {
		return Snapshot{}, "corrupt", "retained book digest or canonical bytes mismatch"
	}
	if err := validateSnapshot(snap); err != nil {
		return Snapshot{}, "corrupt", boundedReason(err.Error())
	}
	return snap, "verified", ""
}
func (r *Runtime) status(ctx context.Context) (RuntimeStatus, Snapshot, error) {
	s := RuntimeStatus{Binding: store.StandardsRunBinding{RootRunID: r.RootRunID}, Integrity: "unavailable", SelectionStatus: "not-selected", RetrievalStatus: "pending", VerifiedAt: time.Now().UTC()}
	b, e := r.Store.GetStandardsRunBinding(ctx, r.RootRunID)
	if errors.Is(e, store.ErrNotFound) {
		s.Reason = "retained binding is missing; current book will not be substituted"
		return s, Snapshot{}, nil
	}
	if e != nil {
		if errors.Is(e, store.ErrStandardsCorrupt) {
			s.Integrity = "corrupt"
			s.Reason = boundedReason(e.Error())
			return s, Snapshot{}, nil
		}
		return s, Snapshot{}, e
	}
	snap, state, reason := verifyBinding(b)
	b.Canonical = ""
	b.SnapshotBytes = nil
	s.Binding = b
	s.Integrity = state
	s.Reason = reason
	all, e := r.Store.ListStandardsRunSelections(ctx, r.RootRunID, r.ConsumerKey)
	if e != nil {
		if errors.Is(e, store.ErrStandardsCorrupt) {
			s.SelectionStatus = "corrupt"
			s.SelectionReason = boundedReason(e.Error())
			return s, snap, nil
		}
		return s, snap, e
	}
	if len(all) > 0 && r.ConsumerKey != "" {
		s.Selection = &all[len(all)-1]
		s.CategoryCount = len(s.Selection.CategoryIDs)
		s.StandardCount = len(s.Selection.StandardIDs)
		if s.Integrity == "verified" {
			if reason := verifyRunSelection(*s.Selection, snap, s.Binding.Digest, r.RootRunID, r.ConsumerKey); reason != "" {
				s.SelectionStatus = "corrupt"
				s.SelectionReason = reason
				return s, snap, nil
			}
		}
		s.SelectionStatus = "selected-nonempty"
		if len(s.Selection.StandardIDs) == 0 {
			s.SelectionStatus = "selected-empty"
		}
		obs, e := r.Store.ListStandardsRunObservations(ctx, r.RootRunID, r.ConsumerKey)
		if e != nil {
			return s, snap, e
		}
		for _, o := range obs {
			if o.SelectionVersion == s.Selection.Version {
				if o.Kind == "offered" && s.RetrievalStatus == "pending" {
					s.RetrievalStatus = "partial"
				}
				if o.Kind == "acknowledgment" {
					s.RetrievalStatus = "retrieved"
				}
			}
		}
	}
	if s.Integrity != "verified" {
		s.RetrievalStatus = "pending"
	}
	return s, snap, nil
}

// Status revalidates retained bytes without writing or resolving current.
func (r *Runtime) Status(ctx context.Context) (RuntimeStatus, error) {
	s, _, e := r.status(ctx)
	return s, e
}

// ConsumerInspection exposes fresh state and standard IDs whose bytes were not fully offered.
type ConsumerInspection struct {
	ConsumerKey   string        `json:"consumer_key"`
	Status        RuntimeStatus `json:"status"`
	RetrievalGaps []string      `json:"retrieval_gaps"`
}

// Inspect returns fresh verification and all consumer histories without writing.
func (r *Runtime) Inspect(ctx context.Context) (RunInspection, error) {
	s, snap, e := r.status(ctx)
	if e != nil {
		return RunInspection{}, e
	}
	all, e := r.Store.ListStandardsRunSelections(ctx, r.RootRunID, "")
	if e != nil {
		if !errors.Is(e, store.ErrStandardsCorrupt) {
			return RunInspection{}, e
		}
		s.SelectionStatus = "corrupt"
		s.SelectionReason = boundedReason(e.Error())
		all = []store.StandardsRunSelection{}
	}
	obs, e := r.Store.ListStandardsRunObservations(ctx, r.RootRunID, "")
	if e != nil {
		return RunInspection{}, e
	}
	out := RunInspection{Status: s, Selections: all, Observations: obs, Consumers: []ConsumerInspection{}}
	keys := map[string]bool{}
	for _, choice := range all {
		keys[choice.ConsumerKey] = true
	}
	for _, observation := range obs {
		if observation.ConsumerKey != "" {
			keys[observation.ConsumerKey] = true
		}
	}
	for _, o := range obs {
		keys[o.ConsumerKey] = true
	}
	ordered := []string{}
	for key := range keys {
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)
	for _, key := range ordered {
		consumer := *r
		consumer.ConsumerKey = key
		state, e := consumer.Status(ctx)
		if e != nil {
			return out, e
		}
		detail := ConsumerInspection{ConsumerKey: key, Status: state, RetrievalGaps: []string{}}
		if state.Selection != nil {
			for _, id := range state.Selection.StandardIDs {
				total := -1
				for _, item := range snap.Standards {
					if item.ID == id {
						total = len(standardText(snap, item))
					}
				}
				ranges := [][2]int{}
				for _, o := range obs {
					if o.ConsumerKey == key && o.Kind == "offered" && o.SelectionVersion == state.Selection.Version && o.StandardID == id {
						ranges = append(ranges, [2]int{o.Start, o.End})
					}
				}
				sort.Slice(ranges, func(i, j int) bool { return ranges[i][0] < ranges[j][0] })
				end := 0
				found := false
				for _, v := range ranges {
					if v[0] <= end {
						found = true
						if v[1] > end {
							end = v[1]
						}
					}
				}
				if state.Integrity != "verified" || !found || total < 0 || end < total {
					detail.RetrievalGaps = append(detail.RetrievalGaps, id)
				}
			}
		}
		out.Consumers = append(out.Consumers, detail)
	}
	if r.ConsumerKey == "" {
		out.Status.Selection = nil
		if out.Status.SelectionStatus != "corrupt" {
			out.Status.SelectionStatus = "per-consumer"
		}
		out.Status.RetrievalStatus = "per-consumer"
	}
	return out, nil
}
func (r *Runtime) observe(ctx context.Context, o store.StandardsRunObservation) error {
	o.RootRunID = r.RootRunID
	o.ConsumerKey = r.ConsumerKey
	o.AttemptID = r.AttemptID
	return r.Store.AppendStandardsRunObservation(ctx, o)
}

// Categories returns default 25, maximum 100 categories with stable pinned cursors.
func (r *Runtime) Categories(ctx context.Context, size int, cursor string) (CategoryPage, error) {
	s, snap, e := r.status(ctx)
	out := CategoryPage{Status: BoundedStatus(s), Categories: []Category{}, StandardCounts: map[string]int{}}
	if e != nil || s.Integrity != "verified" {
		return out, e
	}
	out.Load = BookLoad(snap)
	if size == 0 {
		size = 25
	}
	if size < 1 || size > 100 {
		return out, ErrInvalid
	}
	c, e := r.decodeCursor(cursor, "")
	if e != nil {
		return out, e
	}
	if cursor != "" && c.Digest != s.Binding.Digest {
		return out, ErrInvalid
	}
	if c.Index > len(snap.Categories) {
		return out, ErrInvalid
	}
	for c.Index < len(snap.Categories) && len(out.Categories) < size {
		candidate := snap.Categories[c.Index]
		out.Categories = append(out.Categories, candidate)
		out.StandardCounts[candidate.ID] = out.Load.Categories[candidate.ID]
		selected := false
		if s.Selection != nil {
			for _, id := range s.Selection.CategoryIDs {
				if id == candidate.ID {
					selected = true
					out.SelectedCategoryIDs = append(out.SelectedCategoryIDs, id)
					break
				}
			}
		}
		c.Index++
		c.Digest = s.Binding.Digest
		if c.Index < len(snap.Categories) {
			out.NextCursor = encodeCursor(c)
		} else {
			out.NextCursor = ""
		}
		if !r.fitsResult(out) {
			out.Categories = out.Categories[:len(out.Categories)-1]
			delete(out.StandardCounts, candidate.ID)
			if selected {
				out.SelectedCategoryIDs = out.SelectedCategoryIDs[:len(out.SelectedCategoryIDs)-1]
			}
			c.Index--
			out.NextCursor = encodeCursor(c)
			if len(out.Categories) == 0 {
				return out, fmt.Errorf("category metadata exceeds encoded response limit")
			}
			break
		}
	}
	return out, nil
}

// FitsMCPResult accounts for both text and structured copies with envelope reserve.
func FitsMCPResult(v any) bool {
	b, e := json.Marshal(v)
	if e != nil {
		return false
	}
	text, _ := json.Marshal(string(b))
	return len(b)+len(text)+1024 <= 32<<10
}

// Select records an explicit initial choice or additive union with expected version.
func (r *Runtime) Select(ctx context.Context, expected int64, ids []string, reason string) (RuntimeStatus, error) {
	s, snap, e := r.status(ctx)
	if e != nil {
		return s, e
	}
	if s.Integrity != "verified" {
		return s, fmt.Errorf("standards %s: %s", s.Integrity, s.Reason)
	}
	if s.SelectionStatus == "corrupt" {
		return s, fmt.Errorf("%w: %s", store.ErrStandardsCorrupt, s.SelectionReason)
	}
	if expected < 0 || strings.TrimSpace(reason) == "" || len(reason) > 512 {
		return s, ErrInvalid
	}
	chosen := map[string]bool{}
	valid := map[string]bool{}
	for _, c := range snap.Categories {
		valid[c.ID] = true
	}
	if s.Selection != nil {
		for _, id := range s.Selection.CategoryIDs {
			chosen[id] = true
		}
	}
	for _, id := range ids {
		if !valid[id] {
			return s, fmt.Errorf("%w: unknown category", ErrInvalid)
		}
		chosen[id] = true
	}
	categories := []string{}
	standards := []string{}
	for id := range chosen {
		categories = append(categories, id)
	}
	sort.Strings(categories)
	for _, standard := range snap.Standards {
		if chosen[standard.CategoryID] {
			standards = append(standards, standard.ID)
		}
	}
	d := digest(struct {
		Book                  string
		Categories, Standards []string
	}{s.Binding.Digest, categories, standards})
	_, e = r.Store.SaveStandardsRunSelection(ctx, store.StandardsRunSelection{RootRunID: r.RootRunID, ConsumerKey: r.ConsumerKey, Digest: d, CategoryIDs: categories, StandardIDs: standards, Reason: reason, AttemptID: r.AttemptID}, expected)
	if e != nil {
		return s, e
	}
	return r.Status(ctx)
}

// Read offers bounded exact UTF-8 ranges; acknowledgment requires all ranges offered.
func (r *Runtime) Read(ctx context.Context, version int64, selectionDigest, cursor string, ack bool) (ReadPage, error) {
	s, snap, e := r.status(ctx)
	out := ReadPage{Status: BoundedStatus(s), SelectionVersion: version, SelectionDigest: selectionDigest}
	if e != nil {
		return out, e
	}
	if s.Integrity != "verified" {
		return out, fmt.Errorf("standards %s: %s", s.Integrity, s.Reason)
	}
	if s.SelectionStatus == "corrupt" {
		return out, fmt.Errorf("%w: %s", store.ErrStandardsCorrupt, s.SelectionReason)
	}
	if s.Selection == nil || s.Selection.Version != version || s.Selection.Digest != selectionDigest {
		return out, store.ErrStandardsConflict
	}
	c, e := r.decodeCursor(cursor, selectionDigest)
	if e != nil {
		return out, e
	}
	if cursor != "" && c.Digest != s.Binding.Digest {
		return out, ErrInvalid
	}
	selected := map[string]bool{}
	for _, id := range s.Selection.StandardIDs {
		selected[id] = true
	}
	items := []Standard{}
	texts := []string{}
	for _, item := range snap.Standards {
		if selected[item.ID] {
			items = append(items, item)
			texts = append(texts, standardText(snap, item))
		}
	}
	if ack {
		obs, e := r.Store.ListStandardsRunObservations(ctx, r.RootRunID, r.ConsumerKey)
		if e != nil {
			return out, e
		}
		for i, item := range items {
			ranges := [][2]int{}
			for _, o := range obs {
				if o.Kind == "offered" && o.SelectionVersion == version && o.StandardID == item.ID {
					ranges = append(ranges, [2]int{o.Start, o.End})
				}
			}
			sort.Slice(ranges, func(i, j int) bool { return ranges[i][0] < ranges[j][0] })
			end := 0
			found := false
			for _, v := range ranges {
				if v[0] <= end {
					if v[1] > end {
						end = v[1]
					}
					found = true
				}
			}
			if !found || end < len(texts[i]) {
				return out, fmt.Errorf("%w: selected ranges have not all been offered", ErrInvalid)
			}
		}
		e = r.observe(ctx, store.StandardsRunObservation{Kind: "acknowledgment", Outcome: "agent-reported-complete", SelectionVersion: version})
		if e != nil {
			return out, e
		}
		out.Complete = true
		out.Status, e = r.Status(ctx)
		if e != nil {
			return out, e
		}
		out.Status = BoundedStatus(out.Status)
		return out, nil
	}
	if c.Index > len(items) {
		return out, ErrInvalid
	}
	if c.Index == len(items) {
		if cursor != "" {
			return out, ErrInvalid
		}
		out.Complete = true
		return out, nil
	}
	item, text := items[c.Index], texts[c.Index]
	if c.Offset > len(text) || !utf8.ValidString(text[:c.Offset]) {
		return out, ErrInvalid
	}
	end := c.Offset + 4096
	if end > len(text) {
		end = len(text)
	}
	for !utf8.ValidString(text[c.Offset:end]) {
		end--
	}
	out.StandardID = item.ID
	out.Rule = item.Rule
	out.Start = c.Offset
	out.Total = len(text)
	out.BodyDigest = digestBody(text)
	out.Manifest = []StandardManifest{{ID: item.ID, Rule: item.Rule, Digest: out.BodyDigest, Bytes: len(text)}}
	for {
		out.End = end
		out.Body = text[c.Offset:end]
		next := c
		next.Digest = s.Binding.Digest
		if end == len(text) {
			next.Index++
			next.Offset = 0
		} else {
			next.Offset = end
		}
		out.Complete = next.Index == len(items)
		if !out.Complete {
			out.NextCursor = encodeCursor(next)
		} else {
			out.NextCursor = ""
		}
		if r.fitsResult(out) {
			break
		}
		if end == c.Offset {
			return out, fmt.Errorf("standard metadata exceeds encoded response limit")
		}
		end = c.Offset + (end-c.Offset)/2
		for !utf8.ValidString(text[c.Offset:end]) {
			end--
		}
	}
	e = r.observe(ctx, store.StandardsRunObservation{Kind: "offered", Outcome: "offered", SelectionVersion: version, StandardID: item.ID, Start: out.Start, End: out.End, Total: out.Total})
	if e != nil {
		return out, e
	}
	return out, nil
}

// RolePage is one exact UTF-8 range of the rendered standards that apply to a role.
type RolePage struct {
	Status      RuntimeStatus `json:"status"`
	Role        string        `json:"role"`
	Categories  []string      `json:"categories,omitempty"`
	StandardIDs []string      `json:"standard_ids"`
	Text        string        `json:"text"`
	Start       int           `json:"start"`
	End         int           `json:"end"`
	Total       int           `json:"total"`
	NextCursor  string        `json:"next_cursor,omitempty"`
}

// RoleCategory is one category holding standards that apply to a role.
type RoleCategory struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Count       int    `json:"count"`
}

// RoleCategories lists the categories of the pinned book that hold at least
// one standard applying to a role, in book order.
type RoleCategories struct {
	Status     RuntimeStatus  `json:"status"`
	Role       string         `json:"role"`
	Categories []RoleCategory `json:"categories"`
	Note       string         `json:"note,omitempty"`
}

// ErrUnknownCategory refuses a category that holds no standard for the role.
var ErrUnknownCategory = errors.New("standards: unknown category")

func roleCategories(snap Snapshot, role string) []RoleCategory {
	counts := map[string]int{}
	for _, item := range snap.Standards {
		if slices.Contains(item.AppliesTo, role) {
			counts[item.CategoryID]++
		}
	}
	out := []RoleCategory{}
	for _, c := range snap.Categories {
		if counts[c.ID] > 0 {
			out = append(out, RoleCategory{ID: c.ID, Name: c.Name, Description: c.Description, Count: counts[c.ID]})
		}
	}
	return out
}

// CategoriesForRole lists the categories that hold standards applying to role.
// A book that is not available reads as empty, with the reason in the note.
func (r *Runtime) CategoriesForRole(ctx context.Context, role string) (RoleCategories, error) {
	s, snap, e := r.status(ctx)
	out := RoleCategories{Status: BoundedStatus(s), Role: role, Categories: []RoleCategory{}}
	if e != nil {
		return out, e
	}
	if strings.TrimSpace(role) == "" {
		return out, ErrInvalid
	}
	switch s.Integrity {
	case "verified":
	case "unavailable":
		out.Note = "No standards book is available for this run: " + s.Reason
		return out, nil
	default:
		return out, fmt.Errorf("standards %s: %s", s.Integrity, s.Reason)
	}
	out.Categories = roleCategories(snap, role)
	if len(out.Categories) == 0 {
		out.Note = "No published standards apply to you."
	}
	if !r.fitsResult(out) {
		return out, fmt.Errorf("category metadata exceeds encoded response limit")
	}
	return out, nil
}

// ForRole offers the pinned book's standards whose applies_to names role,
// rendered in one document and paged by byte ranges. The book decides which
// standards apply to which role; categories, when given, narrow that to the
// categories the role chose, and must each hold standards for the role.
func (r *Runtime) ForRole(ctx context.Context, role string, categories []string, cursor string) (RolePage, error) {
	s, snap, e := r.status(ctx)
	out := RolePage{Status: BoundedStatus(s), Role: role, StandardIDs: []string{}}
	if e != nil {
		return out, e
	}
	if s.Integrity != "verified" {
		return out, fmt.Errorf("standards %s: %s", s.Integrity, s.Reason)
	}
	if strings.TrimSpace(role) == "" {
		return out, ErrInvalid
	}
	chosen := slices.Compact(slices.Sorted(slices.Values(categories)))
	selection := "role:" + role
	if len(chosen) > 0 {
		valid := []string{}
		for _, c := range roleCategories(snap, role) {
			valid = append(valid, c.ID)
		}
		var unknown []string
		for _, id := range chosen {
			if !slices.Contains(valid, id) {
				unknown = append(unknown, id)
			}
		}
		if len(unknown) > 0 {
			if len(valid) == 0 {
				valid = []string{"none"}
			}
			return out, fmt.Errorf("%w %s; categories that hold standards for you: %s", ErrUnknownCategory, strings.Join(unknown, ", "), strings.Join(valid, ", "))
		}
		out.Categories = chosen
		raw, _ := json.Marshal(chosen)
		selection += ":categories:" + string(raw)
	}
	c, e := r.decodeCursor(cursor, selection)
	if e != nil {
		return out, e
	}
	if cursor != "" && c.Digest != s.Binding.Digest {
		return out, ErrInvalid
	}
	items := []Standard{}
	for _, item := range snap.Standards {
		if slices.Contains(item.AppliesTo, role) && (len(chosen) == 0 || slices.Contains(chosen, item.CategoryID)) {
			items = append(items, item)
			out.StandardIDs = append(out.StandardIDs, item.ID)
		}
	}
	text := Render(snap.Categories, items)
	if c.Offset > len(text) || !utf8.ValidString(text[:c.Offset]) {
		return out, ErrInvalid
	}
	end := min(c.Offset+(16<<10), len(text))
	for !utf8.ValidString(text[c.Offset:end]) {
		end--
	}
	out.Start, out.Total = c.Offset, len(text)
	for {
		out.End = end
		out.Text = text[c.Offset:end]
		out.NextCursor = ""
		if end < len(text) {
			next := c
			next.Digest = s.Binding.Digest
			next.Offset = end
			out.NextCursor = encodeCursor(next)
		}
		if r.fitsResult(out) {
			break
		}
		if end == c.Offset {
			return out, fmt.Errorf("standards metadata exceeds encoded response limit")
		}
		end = c.Offset + (end-c.Offset)/2
		for !utf8.ValidString(text[c.Offset:end]) {
			end--
		}
	}
	reason := role
	if len(chosen) > 0 {
		reason += " categories=" + strings.Join(chosen, ",")
	}
	return out, r.observe(ctx, store.StandardsRunObservation{Kind: "role-read", Outcome: "offered", Reason: boundedReason(reason), Start: out.Start, End: out.End, Total: out.Total})
}

// standardText is the rendered text an analyst reads for one standard.
func standardText(snap Snapshot, item Standard) string {
	return Render(snap.Categories, []Standard{item})
}

func digestBody(body string) string {
	h := sha256.Sum256([]byte(body))
	return hex.EncodeToString(h[:])
}

// BoundedStatus retains references and counts while keeping manifests out of status.
func BoundedStatus(s RuntimeStatus) RuntimeStatus {
	if s.Selection != nil {
		selection := *s.Selection
		s.CategoryCount = len(selection.CategoryIDs)
		s.StandardCount = len(selection.StandardIDs)
		selection.CategoryIDs = nil
		selection.StandardIDs = nil
		selection.Reason = ""
		selection.RootRunID = ""
		selection.ConsumerKey = ""
		selection.AttemptID = ""
		s.Selection = &selection
	}
	return s
}

func verifyRunSelection(selection store.StandardsRunSelection, snapshot Snapshot, book, root, consumer string) string {
	if selection.RootRunID != root || selection.ConsumerKey != consumer || selection.Version < 1 {
		return "retained selection identity mismatch"
	}
	valid := map[string]bool{}
	for _, category := range snapshot.Categories {
		valid[category.ID] = true
	}
	chosen := map[string]bool{}
	for i, id := range selection.CategoryIDs {
		if !valid[id] || (i > 0 && selection.CategoryIDs[i-1] >= id) {
			return "retained selection categories are invalid or not canonical"
		}
		chosen[id] = true
	}
	expected := []string{}
	for _, standard := range snapshot.Standards {
		if chosen[standard.CategoryID] {
			expected = append(expected, standard.ID)
		}
	}
	if !slices.Equal(selection.StandardIDs, expected) {
		return "retained selection standards differ from selected category membership"
	}
	expectedDigest := digest(struct {
		Book                  string
		Categories, Standards []string
	}{book, selection.CategoryIDs, expected})
	if selection.Digest != expectedDigest {
		return "retained selection digest mismatch"
	}
	return ""
}

func (r *Runtime) fitsResult(v any) bool {
	if r.OutputLimit == 0 {
		return FitsMCPResult(v)
	}
	b, e := json.Marshal(v)
	if e != nil {
		return false
	}
	t, _ := json.Marshal(string(b))
	return len(b)+len(t)+1024 <= min(r.OutputLimit, 32<<10)
}
