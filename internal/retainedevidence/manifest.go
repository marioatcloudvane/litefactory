package retainedevidence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Version is the metadata contract version, independent of a source revision.
const Version = 1

// Coverage describes captured content or accounted-for source references. It
// never describes facts that were not captured or confers disclosure authority.
type Coverage string

// Complete, Partial, Unknown and Empty distinguish captured coverage.
const (
	Complete Coverage = "complete"
	Partial  Coverage = "partial"
	Unknown  Coverage = "unknown"
	Empty    Coverage = "empty"
)

// Availability records whether retained content can be disclosed in this view.
type Availability string

// Available, Unavailable and Restricted distinguish missing content from access limits.
const (
	Available   Availability = "available"
	Unavailable Availability = "unavailable"
	Restricted  Availability = "restricted"
)

// Scope is supplied by the owning factory adapter, never by retrieved text.
// Repository and Task bind identity but do not serve as an access grant.
type Scope struct {
	Kind       string `json:"kind"`
	ID         string `json:"id"`
	Repository string `json:"repository"`
	Task       string `json:"task"`
}

// Entry refers to one exact owning record. Digest and Bytes describe retained
// bytes before redaction; a derived view must identify its own digest separately.
// Bytes is -1 when historical capture metadata cannot establish the extent.
// Channel preserves distinctions such as stdout, stderr, and provider events;
// an unknown ordering must remain unknown rather than inferred from this list.
type Entry struct {
	Kind         string       `json:"kind"`
	OwnerID      string       `json:"owner_id"`
	Revision     string       `json:"revision"`
	Digest       string       `json:"digest,omitempty"`
	Channel      string       `json:"channel,omitempty"`
	Bytes        int64        `json:"bytes"`
	Capture      Coverage     `json:"capture"`
	Availability Availability `json:"availability"`
	Reason       string       `json:"reason,omitempty"`
	Omissions    []string     `json:"omissions,omitempty"`
}

// Manifest accounts for captured sources in its declared scope. Complete means
// every captured source is indexed, including entries explicitly unavailable;
// it does not assert that each source was captured completely or is disclosed.
// Revision is immutable: a changed set or changed metadata needs a new revision.
type Manifest struct {
	Version  int      `json:"version"`
	Scope    Scope    `json:"scope"`
	Revision string   `json:"revision"`
	Coverage Coverage `json:"coverage"`
	Entries  []Entry  `json:"entries"`
}

// ErrInvalid, ErrIntegrity and ErrConflict distinguish malformed metadata,
// changed retained bytes and attempts to rewrite an immutable revision.
var (
	ErrInvalid   = errors.New("invalid retained evidence metadata")
	ErrIntegrity = errors.New("retained evidence integrity mismatch")
	ErrConflict  = errors.New("retained evidence revision conflict")
)

func validCoverage(c Coverage) bool {
	return c == Complete || c == Partial || c == Unknown || c == Empty
}

func validText(s string, required bool) bool {
	return len(s) <= 2048 && (!required || strings.TrimSpace(s) != "")
}

// Validate checks reference shape only. Source ownership and current grants
// remain the responsibility of the owning adapter before every disclosure.
func (m Manifest) Validate() error {
	var errs []error
	if m.Version != Version || !validCoverage(m.Coverage) || !validText(m.Revision, true) {
		errs = append(errs, fmt.Errorf("manifest version, coverage or revision: %w", ErrInvalid))
	}
	if !validText(m.Scope.Kind, true) || !validText(m.Scope.ID, true) || !validText(m.Scope.Repository, true) || !validText(m.Scope.Task, false) {
		errs = append(errs, fmt.Errorf("manifest scope: %w", ErrInvalid))
	}
	if m.Coverage == Empty && len(m.Entries) != 0 {
		errs = append(errs, fmt.Errorf("nonempty manifest declares empty coverage: %w", ErrInvalid))
	}
	seen := make(map[[4]string]bool, len(m.Entries))
	for i, e := range m.Entries {
		key := [4]string{e.Kind, e.OwnerID, e.Revision, e.Channel}
		if seen[key] {
			errs = append(errs, fmt.Errorf("entry %d duplicate identity: %w", i, ErrInvalid))
		}
		seen[key] = true
		if !validText(e.Kind, true) || !validText(e.OwnerID, true) || !validText(e.Revision, true) || !validText(e.Channel, false) || !validText(e.Reason, false) || e.Bytes < -1 || !validCoverage(e.Capture) {
			errs = append(errs, fmt.Errorf("entry %d reference: %w", i, ErrInvalid))
		}
		if e.Availability != Available && e.Availability != Unavailable && e.Availability != Restricted {
			errs = append(errs, fmt.Errorf("entry %d availability: %w", i, ErrInvalid))
		}
		if e.Availability != Available && e.Reason == "" {
			errs = append(errs, fmt.Errorf("entry %d missing availability reason: %w", i, ErrInvalid))
		}
		if e.Capture == Empty && e.Bytes != 0 {
			errs = append(errs, fmt.Errorf("entry %d empty extent: %w", i, ErrInvalid))
		}
		if e.Digest != "" {
			decoded, err := hex.DecodeString(e.Digest)
			if err != nil || len(decoded) != sha256.Size || e.Digest != strings.ToLower(e.Digest) {
				errs = append(errs, fmt.Errorf("entry %d digest: %w", i, ErrInvalid))
			}
		}
		if len(e.Omissions) > 16 {
			errs = append(errs, fmt.Errorf("entry %d omissions count: %w", i, ErrInvalid))
		}
		for _, omission := range e.Omissions {
			if !validText(omission, true) {
				errs = append(errs, fmt.Errorf("entry %d omission: %w", i, ErrInvalid))
			}
		}
	}
	return errors.Join(errs...)
}

// Digest identifies exact retained bytes without asserting their truth.
func Digest(content []byte) string {
	digest := sha256.Sum256(content)
	return hex.EncodeToString(digest[:])
}

// Verify compares retained bytes before redaction. A missing legacy digest
// remains unknown; it must not be filled from current content silently.
func Verify(entry Entry, content []byte) error {
	if entry.Bytes >= 0 && entry.Bytes != int64(len(content)) {
		return fmt.Errorf("byte extent: %w", ErrIntegrity)
	}
	if entry.Digest != "" && entry.Digest != Digest(content) {
		return fmt.Errorf("source digest: %w", ErrIntegrity)
	}
	return nil
}

// MaxIndexEntries bounds the number of source references in one index page.
const MaxIndexEntries = 64

// MaxIndexBytes bounds even entries containing long omission descriptions.
const MaxIndexBytes = 64 << 10

// Page retains the manifest revision and explicit continuation position. A page
// is an excerpt, never a second complete manifest. Next is -1 at the end.
type Page struct {
	Version  int      `json:"version"`
	Scope    Scope    `json:"scope"`
	Revision string   `json:"revision"`
	Coverage Coverage `json:"coverage"`
	Offset   int      `json:"offset"`
	Total    int      `json:"total"`
	Next     int      `json:"next"`
	Entries  []Entry  `json:"entries"`
}

// Index emits a bounded page without changing capture or disclosure budgets.
// The caller must authorize/redact metadata before putting it in an artifact.
func Index(m Manifest, offset, limit int) (Page, error) {
	if err := m.Validate(); err != nil {
		return Page{}, err
	}
	if offset < 0 || offset > len(m.Entries) || limit < 1 || limit > MaxIndexEntries {
		return Page{}, fmt.Errorf("index range: %w", ErrInvalid)
	}
	end := min(offset+limit, len(m.Entries))
	next := -1
	if end < len(m.Entries) {
		next = end
	}
	entries := make([]Entry, end-offset)
	for i, entry := range m.Entries[offset:end] {
		entries[i] = entry
		entries[i].Omissions = append([]string(nil), entry.Omissions...)
	}
	page := Page{Version: m.Version, Scope: m.Scope, Revision: m.Revision, Coverage: m.Coverage, Offset: offset, Total: len(m.Entries), Next: next, Entries: entries}
	for {
		raw, err := json.Marshal(page)
		if err != nil {
			return Page{}, err
		}
		if len(raw) <= MaxIndexBytes {
			return page, nil
		}
		if len(page.Entries) <= 1 {
			return Page{}, fmt.Errorf("index metadata disclosure limit: %w", ErrInvalid)
		}
		page.Entries = page.Entries[:len(page.Entries)-1]
		page.Next = offset + len(page.Entries)
	}
}
