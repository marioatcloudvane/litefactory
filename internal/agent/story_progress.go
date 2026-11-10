package agent

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

// StoryEventsDirectory is reserved transient transport, never repository content.
const StoryEventsDirectory = ".litefactory-story-events"

// MaxStoryEventBytes bounds each immutable checkpoint including snapshot bytes.
const MaxStoryEventBytes = 4 << 20

// MaxStoryEvents bounds the per-run journal and directory scan.
const MaxStoryEvents = 4096

// MaxStoryRunBytes bounds total transport read work during a daemon tick.
const MaxStoryRunBytes = 64 << 20

// StoryEvent is incremental evidence, never an instruction or a forge mutation.
// Snapshot bytes and every check are retained together in the accepted journal.
type StoryEvent struct {
	Version    int            `json:"version"`
	Parent     string         `json:"parent"`
	RunID      int64          `json:"run_id"`
	Generation int            `json:"generation"`
	Story      string         `json:"story"`
	IssueID    int64          `json:"issue_id"`
	Revision   string         `json:"revision"`
	Attempt    int            `json:"attempt"`
	Sequence   int            `json:"sequence"`
	Kind       string         `json:"kind"`
	Reason     string         `json:"reason,omitempty"`
	Evidence   *StoryEvidence `json:"evidence,omitempty"`
}

// StoryEvidence binds scoped checks to the exact retained implementation bytes.
type StoryEvidence struct {
	ImplementationDigest string              `json:"implementation_digest"`
	Files                []StorySnapshotFile `json:"files"`
	Acceptance           []StoryCheck        `json:"acceptance"`
	Review               []StoryCheck        `json:"review"`
	Tests                []StoryCheck        `json:"tests"`
}

// StorySnapshotFile preserves content rather than a mutable evidence pathname.
type StorySnapshotFile struct {
	Path    string `json:"path"`
	Content []byte `json:"content"`
	SHA256  string `json:"sha256"`
	Deleted bool   `json:"deleted,omitempty"`
}

// StoryCheck records one named result against a specific implementation digest.
type StoryCheck struct {
	Name                 string `json:"name"`
	Passed               bool   `json:"passed"`
	Evidence             string `json:"evidence"`
	ImplementationDigest string `json:"implementation_digest"`
}

// StoryDigest is the canonical lowercase SHA256 used by the event protocol.
func StoryDigest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func validStoryDigest(s string) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == sha256.Size && s == strings.ToLower(s)
}

// SnapshotDigest is SHA256 of the ordered path, NUL, hash, NUL, deletion flag,
// newline manifest. Paths must be strictly sorted. Content itself is also checked.
func SnapshotDigest(files []StorySnapshotFile) string {
	var b strings.Builder
	for _, f := range files {
		fmt.Fprintf(&b, "%s\x00%s\x00%t\n", f.Path, f.SHA256, f.Deleted)
	}
	return StoryDigest([]byte(b.String()))
}

// ParseStoryEvent rejects oversized, ambiguous or incomplete JSON checkpoints.
func ParseStoryEvent(data []byte) (StoryEvent, error) {
	var e StoryEvent
	if len(data) > MaxStoryEventBytes {
		return e, fmt.Errorf("story event exceeds size limit")
	}
	if err := uniqueStoryJSON(json.NewDecoder(bytes.NewReader(data)), 0); err != nil {
		return e, err
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&e); err != nil {
		return e, fmt.Errorf("decode story event: %w", err)
	}
	var extra json.RawMessage
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		return e, fmt.Errorf("story event has trailing data")
	}
	if err := e.Validate(); err != nil {
		return e, err
	}
	return e, nil
}

// Validate checks the envelope before any identity can acquire authority.
func (e StoryEvent) Validate() error {
	if e.Version != 1 || e.Parent == "" || e.RunID < 1 || e.Generation < 1 || e.IssueID < 1 || e.Story == "" || len(e.Story) > 128 || !validStoryDigest(e.Revision) || e.Attempt < 1 || e.Sequence < 1 || e.Sequence > MaxStoryEvents || len(e.Reason) > 4096 {
		return fmt.Errorf("invalid story event identity")
	}
	switch e.Kind {
	case "engineering_complete":
		if e.Evidence == nil {
			return fmt.Errorf("engineering completion needs immutable evidence")
		}
		return e.Evidence.ValidateEngineering()
	case "verified":
		if e.Evidence == nil {
			return fmt.Errorf("verification needs immutable evidence")
		}
		return e.Evidence.Validate()
	case "invalidated", "blocked":
		if strings.TrimSpace(e.Reason) == "" || e.Evidence != nil {
			return fmt.Errorf("non-verification event needs reason and no evidence")
		}
	default:
		return fmt.Errorf("unsupported story event kind %q", e.Kind)
	}
	return nil
}

// Validate requires every scoped check to pass against the same content manifest.
func (e StoryEvidence) Validate() error {
	return e.validate([][]StoryCheck{e.Acceptance, e.Review, e.Tests})
}

// ValidateEngineering requires executed passing tests and snapshot facts without
// claiming acceptance or review. Those groups are forbidden, not ignored.
func (e StoryEvidence) ValidateEngineering() error {
	if len(e.Acceptance) != 0 || len(e.Review) != 0 {
		return fmt.Errorf("engineering cannot claim acceptance or review")
	}
	return e.validate([][]StoryCheck{e.Tests})
}

func (e StoryEvidence) validate(groups [][]StoryCheck) error {
	if len(e.Files) == 0 || len(e.Files) > 256 || !validStoryDigest(e.ImplementationDigest) || SnapshotDigest(e.Files) != e.ImplementationDigest {
		return fmt.Errorf("invalid implementation manifest")
	}
	previous := ""
	for _, f := range e.Files {
		if f.Path <= previous || !SafeStoryPath(f.Path) || StoryDigest(f.Content) != f.SHA256 || (f.Deleted && len(f.Content) != 0) {
			return fmt.Errorf("invalid snapshot file %q", f.Path)
		}
		previous = f.Path
	}
	for _, group := range groups {
		if len(group) == 0 || len(group) > 256 {
			return fmt.Errorf("missing or oversized acceptance, review or test checks")
		}
		seen := map[string]bool{}
		for _, check := range group {
			if strings.TrimSpace(check.Name) == "" || len(check.Name) > 256 || seen[check.Name] || !check.Passed || strings.TrimSpace(check.Evidence) == "" || len(check.Evidence) > 16384 || check.ImplementationDigest != e.ImplementationDigest {
				return fmt.Errorf("check does not verify the scoped snapshot")
			}
			seen[check.Name] = true
		}
	}
	return nil
}

// SafeStoryPath excludes traversal and factory-owned data from implementation snapshots.
func SafeStoryPath(p string) bool {
	if p == "" || len(p) > 1024 || strings.ContainsAny(p, "\x00\r\n\\") || path.IsAbs(p) || path.Clean(p) != p || p == ".." || strings.HasPrefix(p, "../") {
		return false
	}
	for _, c := range strings.Split(p, "/") {
		if c == ".git" || c == StoryEventsDirectory {
			return false
		}
	}
	return true
}

// uniqueStoryJSON rejects duplicate keys recursively before typed decoding. A
// last-key-wins parser could otherwise reinterpret a previously reviewed check.
func uniqueStoryJSON(d *json.Decoder, depth int) error {
	if depth > 32 {
		return fmt.Errorf("story event nesting exceeds limit")
	}
	token, err := d.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			keyToken, err := d.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok || seen[key] {
				return fmt.Errorf("duplicate story event key")
			}
			seen[key] = true
			if err := uniqueStoryJSON(d, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for d.More() {
			if err := uniqueStoryJSON(d, depth+1); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unexpected story event delimiter")
	}
	_, err = d.Token()
	return err
}
