package doctor

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// openDoctorFile anchors every path component to a nofollow descriptor. A
// replacement of an ancestor cannot redirect a later child open to a sibling.
func openDoctorFile(path string, directory bool) (*os.File, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("doctor source path is not canonical")
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for i, part := range parts {
		flags := unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_CLOEXEC | unix.O_NONBLOCK
		if i < len(parts)-1 || directory {
			flags |= unix.O_DIRECTORY
		}
		next, e := unix.Openat(fd, part, flags, 0)
		closeErr := unix.Close(fd)
		if e != nil {
			return nil, errors.Join(e, closeErr)
		}
		if closeErr != nil {
			return nil, errors.Join(closeErr, unix.Close(next))
		}
		fd = next
	}
	return os.NewFile(uintptr(fd), path), nil
}

func (s *Service) collectLocal(ctx context.Context, ref naming.Ref, e *store.DoctorEvidence) {
	if ctx.Err() != nil {
		return
	}
	s.collectLiveness(e)
	s.collectLogs(ref, e)
	sources, err := s.DB.DoctorSources(ctx, ref)
	if err != nil {
		e.Gaps = append(e.Gaps, "Worktree and session inventory unavailable: no trusted local roots")
		return
	}
	expected, err := naming.WorktreePath(sources.WorktreeRoot, ref.Issue)
	if err != nil || sources.WorktreePath == "" || sources.WorktreePath != expected {
		e.Gaps = append(e.Gaps, "Worktree and session inventory refused: root identity is missing or mismatched")
		return
	}
	s.collectInventory(sources, e)
	binary, err := git.Resolve(ctx)
	if err != nil {
		e.Gaps = append(e.Gaps, "Git observation unavailable")
		return
	}
	observation, err := binary.ObserveDoctor(ctx, sources.WorktreePath, sources.Branch)
	if err != nil {
		e.Gaps = append(e.Gaps, "Worktree git observation refused or unavailable")
		return
	}
	s.collectBranchInputs(ctx, ref, sources, binary, observation.Head, e)
	s.collectWorktreeFiles(ctx, ref, sources, observation.Status, e)
	e.Records = append(e.Records, store.DoctorEvidenceRecord{CapturedAt: time.Now().UTC(), Kind: "worktree", ID: ref.String(), State: observation.Head, Content: observation.Unpushed + "\n" + observation.Status})
}

func (s *Service) collectLogs(ref naming.Ref, e *store.DoctorEvidence) {
	f, err := openDoctorFile(s.Paths.DaemonLogPath(), false)
	if err != nil {
		e.Gaps = append(e.Gaps, "Issue logs unavailable or unsafe")
		return
	}
	defer func() {
		if err := f.Close(); err != nil {
			e.Gaps = append(e.Gaps, "Issue log descriptor close failed")
		}
	}()
	before, err := f.Stat()
	if err != nil || !before.Mode().IsRegular() {
		e.Gaps = append(e.Gaps, "Issue logs refused: nonregular source")
		return
	}
	stat, ok := before.Sys().(*syscall.Stat_t)
	if ok && stat.Nlink != 1 {
		e.Gaps = append(e.Gaps, "Issue logs refused: aliased source")
		return
	}
	offset := int64(0)
	if before.Size() > 1<<20 {
		offset = before.Size() - (1 << 20)
		e.Gaps = append(e.Gaps, "Issue log scan limited to latest 1 MiB")
	}
	if _, err = f.Seek(offset, io.SeekStart); err != nil {
		e.Gaps = append(e.Gaps, "Issue logs could not seek to bounded window")
		return
	}
	data, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		e.Gaps = append(e.Gaps, "Issue log source exceeded bounded window")
		return
	}
	after, err := f.Stat()
	if err != nil || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		e.Gaps = append(e.Gaps, "Issue logs changed during read; snapshot omitted")
		return
	}
	if offset > 0 {
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			data = data[i+1:]
		} else {
			return
		}
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 4096), 65536)
	var selected []string
	for scanner.Scan() {
		var identity struct {
			Task    string `json:"task"`
			TaskRef string `json:"task_ref"`
		}
		line := scanner.Bytes()
		if json.Unmarshal(line, &identity) != nil {
			continue
		}
		if !strings.EqualFold(identity.Task, ref.String()) && !strings.EqualFold(identity.TaskRef, ref.String()) {
			continue
		}
		if len(selected) == 200 {
			selected = selected[1:]
			e.Gaps = appendUnique(e.Gaps, "Issue logs limited to latest 200 matching lines")
		}
		selected = append(selected, string(line))
	}
	if scanner.Err() != nil {
		e.Gaps = append(e.Gaps, "Issue log scan truncated at an oversized line")
	}
	for _, line := range selected {
		digest := sha256.Sum256([]byte(line))
		e.Records = append(e.Records, store.DoctorEvidenceRecord{CapturedAt: time.Now().UTC(), Kind: "log", ID: hex.EncodeToString(digest[:]), State: "captured", Content: line})
	}
}

func (s *Service) collectInventory(src store.DoctorLocalSources, e *store.DoctorEvidence) {
	if src.Branch == "" || strings.Contains(src.Branch, "..") || strings.Contains(src.Branch, "\\") || strings.HasPrefix(src.Branch, "/") {
		e.Gaps = append(e.Gaps, "Session inventory refused: branch identity unavailable")
		return
	}
	root := filepath.Join(src.WorktreePath, ".aiteam", strings.ReplaceAll(src.Branch, "/", "-"))
	f, err := openDoctorFile(root, true)
	if err != nil {
		e.Gaps = append(e.Gaps, "Issue session inventory unavailable or unsafe")
		return
	}
	defer func() {
		if err := f.Close(); err != nil {
			e.Gaps = append(e.Gaps, "Session inventory descriptor close failed")
		}
	}()
	remaining := 100
	info, err := f.Stat()
	if err != nil {
		e.Gaps = append(e.Gaps, "Session root identity unavailable")
		return
	}
	rootIdentity := fileIdentity(info)
	inventoryDirectory(f, "", rootIdentity, 0, &remaining, e)
	if remaining == 0 {
		e.Gaps = append(e.Gaps, "Session inventory truncated at 100 entries")
	}
}
func inventoryDirectory(dir *os.File, prefix, rootIdentity string, depth int, remaining *int, e *store.DoctorEvidence) {
	if depth > 3 || *remaining == 0 {
		return
	}
	entries, err := dir.ReadDir(*remaining + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		e.Gaps = appendUnique(e.Gaps, "Session inventory read failed")
		return
	}
	for _, entry := range entries {
		if *remaining == 0 {
			return
		}
		*remaining--
		name := entry.Name()
		lower := strings.ToLower(name)
		if strings.HasPrefix(name, ".") || strings.Contains(lower, "secret") || strings.Contains(lower, "credential") || strings.Contains(lower, "token") || len(name) > 256 {
			continue
		}
		fd, err := unix.Openat(int(dir.Fd()), name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
		if err != nil {
			e.Gaps = appendUnique(e.Gaps, "Unsafe or unavailable session entries omitted")
			continue
		}
		f := os.NewFile(uintptr(fd), name)
		info, err := f.Stat()
		switch {
		case err != nil:
			e.Gaps = appendUnique(e.Gaps, "Session entry stat failed")
		case info.IsDir():
			inventoryDirectory(f, prefix+name+"/", rootIdentity, depth+1, remaining, e)
		case info.Mode().IsRegular():
			stat, ok := info.Sys().(*syscall.Stat_t)
			if !ok || stat.Nlink != 1 {
				e.Gaps = appendUnique(e.Gaps, "Aliased session file omitted")
			} else {
				record := store.DoctorEvidenceRecord{CapturedAt: time.Now().UTC(), Kind: "session_file", ID: prefix + name, State: "inventory_only", Content: fmt.Sprintf("%d bytes; contents not read", info.Size())}
				if repairableSessionPath(record.ID) && info.Size() <= 32768 && rootIdentity != "" {
					data, readErr := io.ReadAll(io.LimitReader(f, 32769))
					after, statErr := f.Stat()
					if readErr == nil && statErr == nil && len(data) <= 32768 && os.SameFile(info, after) && info.ModTime().Equal(after.ModTime()) && info.Size() == after.Size() {
						record.State = digestBytes(data)
						record.Identity = rootIdentity + ":" + fileIdentity(info)
						record.Content = string(data)
					} else {
						e.Gaps = appendUnique(e.Gaps, "Changing or unreadable session file omitted")
					}
				}
				e.Records = append(e.Records, record)
			}
		default:
			e.Gaps = appendUnique(e.Gaps, "Nonregular session entry omitted")
		}
		if err := f.Close(); err != nil {
			e.Gaps = appendUnique(e.Gaps, "Session entry descriptor close failed")
		}
	}
}
func appendUnique(values []string, v string) []string {
	for _, s := range values {
		if s == v {
			return values
		}
	}
	return append(values, v)
}

func (s *Service) collectLiveness(e *store.DoctorEvidence) {
	f, err := openDoctorFile(s.Paths.LockPath(), false)
	if err != nil {
		e.Gaps = append(e.Gaps, "Daemon liveness unavailable: singleton file missing or unsafe")
		return
	}
	err = unix.Flock(int(f.Fd()), unix.LOCK_SH|unix.LOCK_NB)
	state := "singleton_unlocked_at_capture"
	if errors.Is(err, unix.EWOULDBLOCK) {
		state = "singleton_held_at_capture; not proof of workflow health"
	} else if err != nil {
		e.Gaps = append(e.Gaps, "Daemon singleton observation unavailable")
	}
	if err == nil || errors.Is(err, unix.EWOULDBLOCK) {
		e.Records = append(e.Records, store.DoctorEvidenceRecord{CapturedAt: time.Now().UTC(), Kind: "daemon", ID: "singleton", State: state})
	}
	if err := f.Close(); err != nil {
		e.Gaps = append(e.Gaps, "Daemon singleton observation close failed")
	}
}

// collectBranchInputs binds synchronization to the locally observed base/parent
// commit. No fetch is performed during diagnosis.
func (s *Service) collectBranchInputs(ctx context.Context, ref naming.Ref, sources store.DoctorLocalSources, binary *git.Binary, head string, e *store.DoctorEvidence) {
	session, err := s.DB.GetSessionByTaskRef(ctx, ref.String())
	if err != nil {
		e.Gaps = append(e.Gaps, "Branch synchronization session unavailable")
		return
	}
	repo, err := s.DB.GetRepo(ctx, session.RepoID)
	if err != nil {
		e.Gaps = append(e.Gaps, "Branch synchronization repository unavailable")
		return
	}
	base := repo.BaseBranch
	if story, err := s.DB.ImplementStoryByWorkerSession(ctx, session.ID); err == nil {
		parent, err := s.DB.GetSessionByTaskRef(ctx, story.Parent)
		if err != nil || parent.Branch == "" {
			e.Gaps = append(e.Gaps, "Branch synchronization parent unavailable")
			return
		}
		base = parent.Branch
	} else if !errors.Is(err, store.ErrNotFound) {
		e.Gaps = append(e.Gaps, "Branch synchronization parent lookup failed")
		return
	}
	clone, err := binary.Open(ctx, sources.ClonePath)
	if err != nil {
		e.Gaps = append(e.Gaps, "Branch synchronization clone unavailable")
		return
	}
	commit, err := clone.RevParse(ctx, "refs/remotes/origin/"+base, git.RevParseOptions{})
	if err != nil {
		e.Gaps = append(e.Gaps, "Branch synchronization base unavailable; no fetch performed")
		return
	}
	e.Records = append(e.Records, store.DoctorEvidenceRecord{CapturedAt: time.Now().UTC(), Kind: "branch", ID: sources.Branch, State: head, Identity: commit, Content: "Observed base/parent: " + base + "; no fetch performed"})
}
