package turn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"

	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/git"
)

const judgeFileLimit = 1 << 20
const judgeSnapshotLimit = 64 << 20

// JudgeComment is a captured comment, identified by its provider-returned URL.
type JudgeComment struct{ ID, URL, Body string }

// JudgeEvidenceSnapshot retains captured bytes for bounded reads. Later edits cannot retarget reads.
type JudgeEvidenceSnapshot struct {
	Cutoff      time.Time                   `json:"cutoff"`
	Commit      string                      `json:"commit"`
	LocalDigest string                      `json:"local_digest"`
	Evidence    []agentresult.JudgeEvidence `json:"evidence"`
	Contents    map[string]string           `json:"contents"`
}

// Read returns only the admitted captured evidence, without opening a live path.
func (s JudgeEvidenceSnapshot) Read(id string, start, count int) (string, error) {
	body, ok := s.Contents[id]
	if !ok {
		return "", errors.New("judge evidence is unavailable")
	}
	if start < 1 || count < 1 || count > 200 {
		return "", errors.New("judge read requires a starting line and 1–200 lines")
	}
	lines := strings.Split(body, "\n")
	if start > len(lines) {
		return "", errors.New("judge read starts beyond captured evidence")
	}
	end := min(start-1+count, len(lines))
	return strings.Join(lines[start-1:end], "\n"), nil
}

// CaptureJudgeEvidence captures source, comments and repository bytes before invoking a model.
// Repository enumeration uses tracked and nonignored files; symlinks and credential paths are excluded.
func CaptureJudgeEvidence(ctx context.Context, binary *git.Binary, repositoryRoot, source string, comments []JudgeComment) (JudgeEvidenceSnapshot, error) {
	s := JudgeEvidenceSnapshot{Contents: map[string]string{}}
	add := func(id, kind, ref, path, body string, essential bool) {
		digest := sha256.Sum256([]byte(body))
		s.Evidence = append(s.Evidence, agentresult.JudgeEvidence{ID: id, Kind: kind, Reference: ref, Path: path, Digest: hex.EncodeToString(digest[:]), Lines: len(strings.Split(body, "\n")), Available: true, Essential: essential})
		s.Contents[id] = body
	}
	add("source", "source", "pinned saved source", "", source, true)
	commentBytes := 0
	for _, c := range comments {
		if len(c.Body) > 1<<20 || commentBytes+len(c.Body) > 8<<20 {
			s.Evidence = append(s.Evidence, agentresult.JudgeEvidence{ID: "comment:" + c.ID, Kind: "comment", Reference: c.URL, Essential: true, Gap: "Comment exceeds the captured discussion budget; bytes were not clipped."})
			continue
		}
		commentBytes += len(c.Body)
		add("comment:"+c.ID, "comment", c.URL, "", c.Body, false)
	}
	if binary == nil || repositoryRoot == "" {
		s.Evidence = append(s.Evidence, agentresult.JudgeEvidence{ID: "repository", Kind: "code", Essential: true, Gap: "Repository snapshot is unavailable."})
		return s, nil
	}
	state, err := git.CaptureJudgeRepository(ctx, binary, repositoryRoot)
	if err != nil {
		return s, fmt.Errorf("capture judge repository: %w", err)
	}
	s.Commit = state.Commit
	names := state.Paths
	root, err := os.OpenRoot(state.Root)
	if err != nil {
		return s, fmt.Errorf("open judge evidence root: %w", err)
	}
	defer func() { _ = root.Close() }()
	sort.Strings(names)
	hash := sha256.New()
	total := 0
	seen := map[string]bool{}
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return s, err
		}
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		id := "code:" + name
		entry := agentresult.JudgeEvidence{ID: id, Kind: "code", Path: name, Reference: s.Commit + ":" + name}
		switch {
		case !filepath.IsLocal(name) || judgeSensitivePath(name):
			entry.Gap = "Path is outside the authorized evidence ceiling."
		default:
			info, statErr := root.Lstat(name)
			if statErr == nil {
				parts := strings.Split(filepath.ToSlash(name), "/")
				for i := 1; i < len(parts); i++ {
					ancestor, ancestorErr := root.Lstat(strings.Join(parts[:i], "/"))
					if ancestorErr != nil || !ancestor.IsDir() || ancestor.Mode()&os.ModeSymlink != 0 {
						statErr = errors.New("symlink or inaccessible ancestor")
						break
					}
				}
			}
			switch {
			case statErr != nil:
				entry.Gap = "File is missing or inaccessible."
			case !info.Mode().IsRegular():
				entry.Gap = "Only regular repository files are captured."
			case info.Size() > judgeFileLimit || total+int(info.Size()) > judgeSnapshotLimit:
				entry.Gap = "File exceeds the bounded evidence snapshot; no bytes were clipped."
			default:
				f, openErr := root.Open(name)
				if openErr != nil {
					entry.Gap = "File is inaccessible."
					break
				}
				opened, descriptorErr := f.Stat()
				if descriptorErr != nil || !os.SameFile(info, opened) {
					_ = f.Close()
					entry.Gap = "File identity changed during capture."
					break
				}
				raw, readErr := io.ReadAll(io.LimitReader(f, judgeFileLimit+1))
				after, afterErr := f.Stat()
				closeErr := f.Close()
				if readErr != nil || closeErr != nil || afterErr != nil || after.Size() != opened.Size() || after.ModTime() != opened.ModTime() || len(raw) > judgeFileLimit {
					entry.Gap = "File could not be captured within the evidence bound."
					break
				}
				total += len(raw)
				digest := sha256.Sum256(raw)
				entry.Digest = hex.EncodeToString(digest[:])
				entry.Lines = len(strings.Split(string(raw), "\n"))
				entry.Available = true
				s.Contents[id] = string(raw)
				_, _ = fmt.Fprintf(hash, "%s\x00%s\x00", name, entry.Digest)
			}
		}
		s.Evidence = append(s.Evidence, entry)
	}
	s.LocalDigest = hex.EncodeToString(hash.Sum(nil))
	final, err := git.CaptureJudgeRepository(ctx, binary, repositoryRoot)
	if err != nil {
		return s, err
	}
	if state.StateDigest != final.StateDigest {
		return s, errors.New("repository changed during judge evidence capture")
	}
	for i := range s.Evidence {
		e := &s.Evidence[i]
		if e.Kind == "code" {
			e.Reference = "local-snapshot:" + s.LocalDigest + ":" + e.Path
			if e.Available {
				f, openErr := root.Open(e.Path)
				if openErr != nil {
					return s, errors.New("captured repository file became inaccessible")
				}
				raw, readErr := io.ReadAll(io.LimitReader(f, judgeFileLimit+1))
				closeErr := f.Close()
				digest := sha256.Sum256(raw)
				if readErr != nil || closeErr != nil || hex.EncodeToString(digest[:]) != e.Digest {
					return s, errors.New("repository bytes changed during judge evidence capture")
				}
				if !state.ChangedPaths[e.Path] && git.JudgeBlobMatches(state.CommittedBlobs[e.Path], raw) {
					e.Reference = s.Commit + ":" + e.Path
				}
			}
			if !e.Available && e.Gap != "Path is outside the authorized evidence ceiling." {
				e.Essential = true
			}
		}
	}
	return s, nil
}

// judgeSensitivePath keeps credentials and repository internals out of judge
// evidence. Factory artifacts under .aiteam (research and plan reports) stay
// readable: judging a Plan against its research needs them.
func judgeSensitivePath(name string) bool {
	for _, part := range strings.Split(filepath.ToSlash(name), "/") {
		lower := strings.ToLower(part)
		if lower == ".git" || lower == ".env" || strings.HasPrefix(lower, ".env.") || lower == "credentials" || lower == ".aws" || lower == ".ssh" || strings.HasSuffix(lower, ".pem") || strings.HasSuffix(lower, ".key") {
			return true
		}
	}
	return false
}
