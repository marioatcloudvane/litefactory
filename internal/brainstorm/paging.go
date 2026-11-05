package brainstorm

import (
	"encoding/json"
	"time"

	"github.com/cloudvane/litefactory/internal/signedcursor"
)

// pageBytes bounds the encoded items of one paged reply below the 128 KiB transport bound.
const pageBytes = 96 << 10

// viewBytes bounds each newest page carried by a View.
const viewBytes = 32 << 10

// viewItems is the item count of each newest page carried by a View.
const viewItems = 20

// pageCursor is one signed paging position bound to its reader.
type pageCursor struct {
	Type       string `json:"t"`
	Owner      string `json:"o"`
	Generation int64  `json:"g"`
	Grant      string `json:"gr"`
	Client     string `json:"cl"`
	Target     string `json:"b"`
	Kind       string `json:"k,omitempty"`
	State      string `json:"st,omitempty"`
	Seq        int64  `json:"s,omitempty"`
	Created    string `json:"c,omitempty"`
	ID         string `json:"i,omitempty"`
	Offsets    [3]int `json:"e,omitzero"`
}

func (s *Service) codec() signedcursor.Codec { return signedcursor.Codec{Key: s.cursorKey} }

func boundCursor(p Principal, kind, target string) pageCursor {
	return pageCursor{Type: kind, Owner: p.OwnerID, Generation: p.OwnerGeneration, Grant: p.GrantID, Client: p.ClientID, Target: target}
}

// openCursor returns the zero position for an empty cursor, or the position
// sealed in raw when it was issued to the same reader for the same target.
func (s *Service) openCursor(raw string, want pageCursor) (pageCursor, error) {
	if raw == "" {
		return want, nil
	}
	var c pageCursor
	if s.codec().Decode(raw, &c) != nil {
		return pageCursor{}, ErrCursor
	}
	if c.Type != want.Type || c.Owner != want.Owner || c.Generation != want.Generation || c.Grant != want.Grant || c.Client != want.Client || c.Target != want.Target || c.Kind != want.Kind || c.State != want.State {
		return pageCursor{}, ErrCursor
	}
	return c, nil
}

func (s *Service) sealCursor(c pageCursor) (string, error) { return s.codec().Encode(c) }

// pageLimit accepts 1 to 50 and treats 0 as the maximum.
func pageLimit(limit int, cursor string) (int, error) {
	if limit == 0 {
		limit = MaxPageLimit
	}
	if !signedcursor.ValidPage(limit, cursor) || limit > MaxPageLimit {
		return 0, ErrInvalid
	}
	return limit, nil
}

func encodedSize(v any) int {
	raw, err := json.Marshal(v)
	if err != nil {
		return 1 << 30
	}
	return len(raw) + 1
}

// fitPrefix returns how many leading items fit within budget encoded bytes,
// at least one when any exist so paging always advances.
func fitPrefix[T any](items []T, budget int) int {
	size := 0
	for i, item := range items {
		size += encodedSize(item)
		if size > budget && i > 0 {
			return i
		}
	}
	return len(items)
}

// fitSuffix returns the start of the longest tail within budget encoded bytes.
func fitSuffix[T any](items []T, budget int) int {
	size := 0
	for i := len(items) - 1; i >= 0; i-- {
		size += encodedSize(items[i])
		if size > budget && i < len(items)-1 {
			return i + 1
		}
	}
	return 0
}

// evidence is the paged research payload of one scan.
type evidence struct {
	Findings    []json.RawMessage `json:"findings"`
	Decisions   []json.RawMessage `json:"decisions"`
	Assumptions []json.RawMessage `json:"assumptions"`
}

// evidencePage takes up to limit items from the offsets on, in findings,
// decisions, assumptions order, within pageBytes. It returns the page, the
// next offsets and whether everything was returned.
func evidencePage(e evidence, at [3]int, limit int) (evidence, [3]int, bool) {
	lists := [3][]json.RawMessage{e.Findings, e.Decisions, e.Assumptions}
	var out [3][]json.RawMessage
	next := at
	taken, size := 0, 0
	for i := range lists {
		next[i] = min(max(next[i], 0), len(lists[i]))
		for next[i] < len(lists[i]) && taken < limit {
			n := len(lists[i][next[i]]) + 1
			if taken > 0 && size+n > pageBytes {
				return evidence{out[0], out[1], out[2]}, next, false
			}
			out[i] = append(out[i], lists[i][next[i]])
			next[i]++
			taken++
			size += n
		}
	}
	done := next[0] == len(lists[0]) && next[1] == len(lists[1]) && next[2] == len(lists[2])
	return evidence{out[0], out[1], out[2]}, next, done
}

func cursorTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }
