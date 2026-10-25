package projectread

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/retainedevidence"
)

const preparationBytes = 256 << 10

// Views owns private preparation state for one authorized invocation. It must
// not be shared with a later invocation to replenish its call allowance.
type Views struct {
	mu      sync.Mutex
	entries map[string]*repoView
	closed  bool
}
type repoView struct {
	project              int64
	commit, path, policy string
	preparationID        string
	stream               io.ReadCloser
	file                 *os.File
	directory            bool
	sourceBytes          int64
	sourceDigest         string
	resume               *repoLocator
	hash                 hash.Hash
	view                 redact.View
	offset, start, end   int
	line                 int
	ready                bool
}

// NewViews allocates an invocation's private repository view cache.
func NewViews() *Views { return &Views{entries: make(map[string]*repoView)} }

// Close removes private source spills and stops any unfinished Git reader.
func (v *Views) Close() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.closed = true
	for token, entry := range v.entries {
		entry.close()
		delete(v.entries, token)
	}
	return nil
}
func (v *repoView) close() {
	if v.stream != nil {
		_ = v.stream.Close()
		v.stream = nil
	}
	if v.file != nil {
		name := v.file.Name()
		_ = v.file.Close()
		_ = os.Remove(name)
		v.file = nil
	}
}

type repoLocator struct {
	Version       int    `json:"version"`
	Project       int64  `json:"project"`
	Commit        string `json:"commit"`
	Path          string `json:"path"`
	Policy        string `json:"policy"`
	Digest        string `json:"digest"`
	Offset        int    `json:"offset"`
	End           int    `json:"end"`
	Prepared      int64  `json:"prepared,omitempty"`
	Prefix        string `json:"prefix,omitempty"`
	PreparationID string `json:"preparation_id,omitempty"`
}

func (e *repoView) locator(offset int) string {
	preparationID := ""
	if !e.ready {
		preparationID = e.preparationID
	}
	b, _ := json.Marshal(repoLocator{PreparationID: preparationID, Version: 1, Project: e.project, Commit: e.commit, Path: e.path, Policy: e.policy, Digest: e.view.Digest(), Offset: offset, End: e.end, Prepared: e.sourceBytes, Prefix: hex.EncodeToString(e.hash.Sum(nil))})
	return base64.RawURLEncoding.EncodeToString(b)
}
func pageError(status, reason string) (json.RawMessage, bool, error) {
	return raw(map[string]string{"status": status, "reason": reason}), false, nil
}

func (r Reader) page(ctx context.Context, p Project, in Request) (json.RawMessage, bool, error) {
	if r.Views == nil {
		r.Views = NewViews()
		defer func() { _ = r.Views.Close() }()
	}
	if r.DB == nil || r.Redactor == nil || p.Commit == "" {
		return pageError("unavailable", "no authorized pinned repository")
	}
	// The owning repository must still exist on every call, including a cached page.
	repo, err := r.DB.GetRepo(ctx, p.ID)
	if err != nil {
		return pageError("unavailable", "repository grant is unavailable")
	}
	r.Views.mu.Lock()
	defer r.Views.mu.Unlock()
	if r.Views.closed {
		return pageError("expired_cursor", "repository invocation has ended")
	}
	var resume *repoLocator
	if in.Locator != "" {
		if in.Continuation != "" || in.Path != "" || in.StartLine != 0 || in.Lines != 0 || in.Query != "" || in.Pattern != "" || in.Limit != 0 || in.IgnoreCase {
			return pageError("invalid_request", "locator conflicts with selectors")
		}
		decoded, e := base64.RawURLEncoding.DecodeString(in.Locator)
		if e != nil || len(decoded) > 4096 {
			return pageError("invalid_request", "invalid repository locator")
		}
		resume = &repoLocator{}
		if json.Unmarshal(decoded, resume) != nil || resume.Version != 1 || resume.Offset < 0 || resume.End < resume.Offset {
			return pageError("invalid_request", "invalid repository locator")
		}
		if resume.Project != p.ID || resume.Commit != p.Commit {
			return pageError("source_changed", "locator names a different repository revision")
		}
		if resume.Policy != r.Redactor.PolicyID() {
			return pageError("policy_changed", "locator disclosure policy changed")
		}
		in.Path = resume.Path
	}
	headerView, e := r.Redactor.Prepare(ctx, fmt.Sprintf("%s at %s (%s)\n", p.Name, p.Commit, p.BaseBranch))
	if e != nil {
		return pageError("redaction_failure", "repository metadata redaction failed")
	}
	token := in.Continuation
	var entry *repoView
	if token != "" {
		if in.Path != "" || in.StartLine != 0 || in.Lines != 0 || in.Query != "" || in.Pattern != "" || in.Limit != 0 || in.IgnoreCase {
			return pageError("invalid_request", "continuation conflicts with selectors")
		}
		entry = r.Views.entries[token]
		if entry == nil {
			return pageError("expired_cursor", "repository continuation is unavailable")
		}
		if entry.project != p.ID || entry.commit != p.Commit {
			return pageError("source_changed", "continuation names a different repository revision")
		}
		if entry.policy != r.Redactor.PolicyID() {
			entry.close()
			delete(r.Views.entries, token)
			return pageError("policy_changed", "the disclosure policy changed")
		}
	} else {
		if in.StartLine < 0 || in.Lines < 0 || in.Lines > 400 {
			return pageError("invalid_request", "invalid line selector")
		}
		safePath, e := r.Redactor.Prepare(ctx, in.Path)
		if e != nil || safePath.String() != in.Path {
			return pageError("unavailable", "unsafe source selector withheld")
		}
		if resume != nil && resume.PreparationID != "" {
			var takeErr error
			entry, takeErr = takeRepositoryPreparation(resume, p.ID, p.Commit, in.Path, r.Redactor.PolicyID())
			if takeErr != nil {
				return pageError("unavailable", "private preparation expired or no longer available")
			}
			var nonce [24]byte
			if _, e = rand.Read(nonce[:]); e != nil {
				entry.close()
				return nil, false, e
			}
			token = base64.RawURLEncoding.EncodeToString(nonce[:])
			entry.resume = resume
			r.Views.entries[token] = entry
		} else {
			clone, e := r.open(ctx, repo.Path)
			if e != nil {
				return pageError("unavailable", "repository clone unavailable")
			}
			stream, directory, e := git.OpenPinnedRead(ctx, clone, p.Commit, in.Path)
			if e != nil {
				return pageError("unavailable", "pinned source unavailable")
			}
			file, e := os.CreateTemp("", "lf-repository-source-")
			if e != nil {
				_ = stream.Close()
				return pageError("unavailable", "private preparation storage unavailable")
			}
			var nonce [24]byte
			if _, e = rand.Read(nonce[:]); e != nil {
				_ = stream.Close()
				name := file.Name()
				_ = file.Close()
				_ = os.Remove(name)
				return nil, false, e
			}
			token = base64.RawURLEncoding.EncodeToString(nonce[:])
			entry = &repoView{preparationID: token, project: p.ID, commit: p.Commit, path: in.Path, policy: r.Redactor.PolicyID(), stream: stream, file: file, directory: directory, start: in.StartLine, end: in.Lines}
			entry.hash = sha256.New()
			entry.resume = resume
			r.Views.entries[token] = entry
		}
	}
	if in.Continuation != "" {
		var nonce [24]byte
		if _, err = rand.Read(nonce[:]); err != nil {
			return nil, false, err
		}
		delete(r.Views.entries, token)
		token = base64.RawURLEncoding.EncodeToString(nonce[:])
		r.Views.entries[token] = entry
	}
	if !entry.ready {
		// Read no more than one preparation allowance. Cancellation destroys the
		// unfinished source rather than presenting a partially redacted prefix.
		chunk := make([]byte, preparationBytes)
		type readResult struct {
			n   int
			err error
		}
		result := make(chan readResult, 1)
		go func(stream io.Reader) { n, e := io.ReadFull(stream, chunk); result <- readResult{n, e} }(entry.stream)
		var read readResult
		select {
		case <-ctx.Done():
			entry.close()
			delete(r.Views.entries, token)
			<-result
			return pageError("unavailable", "preparation cancelled")
		case read = <-result:
		}
		if read.n > 0 {
			if _, err = entry.file.Write(chunk[:read.n]); err != nil {
				entry.close()
				delete(r.Views.entries, token)
				return pageError("unavailable", "private preparation write failed")
			}
			_, _ = entry.hash.Write(chunk[:read.n])
			entry.sourceBytes += int64(read.n)
			if entry.resume != nil && entry.resume.Digest == "" && entry.sourceBytes == entry.resume.Prepared && hex.EncodeToString(entry.hash.Sum(nil)) != entry.resume.Prefix {
				entry.close()
				delete(r.Views.entries, token)
				return pageError("source_changed", "preparation prefix failed verification")
			}
		}
		if read.err == nil {
			return raw(map[string]any{"status": "preparing", "continuation": token, "source_bytes_prepared": entry.sourceBytes, "locator": entry.locator(0), "coverage": "incomplete; no content disclosed", "remaining": map[string]any{"source_offset": entry.sourceBytes, "view_offset": 0}}), true, nil
		}
		if !errors.Is(read.err, io.EOF) && !errors.Is(read.err, io.ErrUnexpectedEOF) {
			entry.close()
			delete(r.Views.entries, token)
			return pageError("unavailable", "source preparation failed")
		}
		if err = entry.stream.Close(); err != nil {
			entry.close()
			delete(r.Views.entries, token)
			return pageError("unavailable", "pinned source preparation failed")
		}
		entry.stream = nil
		if _, err = entry.file.Seek(0, io.SeekStart); err != nil {
			entry.close()
			return pageError("unavailable", "private preparation read failed")
		}
		data, e := io.ReadAll(entry.file)
		if e != nil {
			entry.close()
			return pageError("unavailable", "private preparation read failed")
		}
		entry.sourceDigest = retainedevidence.Digest(data)
		text := string(data)
		if entry.directory {
			var names []string
			for _, record := range strings.Split(text, "\x00") {
				meta, name, ok := strings.Cut(record, "\t")
				if ok {
					if strings.Contains(meta, " tree ") {
						name += "/"
					}
					names = append(names, name)
				}
			}
			text = strings.Join(names, "\n")
		}
		if !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
			entry.close()
			delete(r.Views.entries, token)
			return pageError("unavailable", "non-text source withheld")
		}
		entry.view, err = r.Redactor.Prepare(ctx, text)
		entry.close()
		if err != nil {
			delete(r.Views.entries, token)
			return pageError("redaction_failure", "complete safe view preparation failed")
		}
		entry.ready = true
		entry.offset, entry.end = lineRange(entry.view.String(), entry.start, entry.end, entry.directory)
		if entry.resume != nil {
			loc := entry.resume
			if (loc.Digest != "" && loc.Digest != entry.view.Digest()) || loc.End > len(entry.view.String()) || !utf8.ValidString(entry.view.String()[:loc.Offset]) {
				delete(r.Views.entries, token)
				return pageError("source_changed", "locator view failed verification")
			}
			if loc.Digest != "" {
				entry.offset, entry.end = loc.Offset, loc.End
			}
		}
	}
	text := entry.view.String()
	if entry.line == 0 {
		entry.line = 1 + strings.Count(text[:entry.offset], "\n")
	}
	start := entry.offset
	end := min(entry.end, start+OutputBytes)

	// The boundary must be between complete code points, not before a trailing
	// continuation byte. Validate the candidate and shrink as needed.
	for end > start && !utf8.ValidString(text[start:end]) {
		end--
	}
	for {
		eof := end == entry.end
		out := map[string]any{"status": "ok", "content": text[start:end], "text": headerView.String() + decoratePage(text[start:end], entry.line), "source": retainedevidence.Entry{Kind: "git", OwnerID: fmt.Sprintf("%d:%s", p.ID, entry.path), Revision: p.Commit, Digest: entry.sourceDigest, Bytes: entry.sourceBytes, Capture: retainedevidence.Complete, Availability: retainedevidence.Available}, "view": map[string]any{"digest": entry.view.Digest(), "policy": entry.view.PolicyID(), "bytes": len(text)}, "range": map[string]int{"start": start, "end": end}, "remaining": map[string]int{"view_offset": end}, "locator": entry.locator(end), "retained_eof": end == len(text), "selection_complete": eof, "directory": entry.directory, "coverage": map[string]string{"source": "complete pinned Git object", "view": "complete normalized redacted source", "page": "selected interval"}, "line": entry.line, "partial_line": start > 0 && text[start-1] != '\n'}
		if !eof {
			out["continuation"] = token
		} else if end < len(text) {
			out["limitation"] = "The requested line selection is complete; the retained view contains more content."
		}
		if len(text[start:end]) > 4096 {
			out["text"] = headerView.String()
		}
		encoded := raw(out)
		// MCP retains both JSON and its textual transport representation.
		transport, _ := json.Marshal(string(encoded))
		if len(encoded)+len(transport)+2048 <= OutputBytes {
			entry.line += strings.Count(text[start:end], "\n")
			entry.offset = end
			return encoded, true, nil
		}
		if end == start {
			return raw(map[string]any{"status": "budget_limited", "reason": "metadata exceeds the response allowance; position unchanged", "locator": entry.locator(start), "remaining": map[string]int{"view_offset": start}}), false, nil
		}
		end = start + (end-start)/2
		for end > start && !utf8.ValidString(text[start:end]) {
			end--
		}
	}
}

func lineRange(text string, start, count int, directory bool) (int, int) {
	if directory {
		return 0, len(text)
	}
	if start < 1 {
		start = 1
	}
	offset := 0
	for line := 1; line < start && offset < len(text); line++ {
		next := strings.IndexByte(text[offset:], '\n')
		if next < 0 {
			return len(text), len(text)
		}
		offset += next + 1
	}
	if count == 0 {
		return offset, len(text)
	}
	end := offset
	for n := 0; n < count && end < len(text); n++ {
		next := strings.IndexByte(text[end:], '\n')
		if next < 0 {
			end = len(text)
			break
		}
		end += next + 1
	}
	return offset, end
}
