package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const cacheSliceEntries = 100000
const cacheSliceDuration = 5 * time.Second
const cacheSampleInterval = 5 * time.Minute
const cacheScanDepth = 64

// cacheSizeObservation describes logical regular-file bytes, never allocated or
// reclaimable space. PartialBytes belongs to the current pass only.
type cacheSizeObservation struct {
	Path              string
	State             string
	PartialBytes      int64
	EntriesExamined   int64
	PassStartedAt     time.Time
	ProgressAt        time.Time
	CompleteBytes     *int64
	CompleteStale     bool
	CompleteStartedAt time.Time
	CompleteAt        time.Time
	Error             string
}

type cachePendingDirectory struct {
	path string
	info os.FileInfo
}

type cacheDirectoryCursor struct {
	file     *os.File
	path     string
	batch    []os.DirEntry
	readErr  error
	expected os.FileInfo
	verified bool
	pending  *cachePendingDirectory
}
type cacheScan struct {
	path        string
	root        os.FileInfo
	probedRoot  os.FileInfo
	rootChecked bool
	stack       []cacheDirectoryCursor
	observation cacheSizeObservation
}

// cacheSampler has exactly one scanning worker. Its mutex protects only published
// values: filesystem calls never hold it, so Snapshot cannot wait for a scan.
type cacheSampler struct {
	running        atomic.Bool
	mu             sync.RWMutex
	published      map[string]cacheSizeObservation
	scans          map[string]*cacheScan
	entryBudget    int
	durationBudget time.Duration
	now            func() time.Time
	// beforeRead is a test seam for a slow filesystem; it never holds mu.
	device     func(os.FileInfo) (string, bool)
	beforeRead func()
	afterLstat func()
	afterOpen  func()
	afterStat  func()
}

func newCacheSampler(main, reviewer string) *cacheSampler {
	s := &cacheSampler{published: make(map[string]cacheSizeObservation), scans: make(map[string]*cacheScan), entryBudget: cacheSliceEntries, durationBudget: cacheSliceDuration, now: time.Now, device: cacheDevice}
	for role, path := range map[string]string{"main": main, "reviewer": reviewer} {
		scan := &cacheScan{path: path, observation: cacheSizeObservation{Path: path, State: "unavailable"}}
		s.scans[role] = scan
		s.published[role] = scan.observation
	}
	return s
}

// Restore seeds display observations before Run. Traversal state is always new;
// persisted facts never authorize cache access or admission.
func (s *cacheSampler) Restore(observations map[string]cacheSizeObservation) {
	for role, previous := range observations {
		scan, ok := s.scans[role]
		if !ok || previous.Path != scan.path {
			continue
		}
		if previous.CompleteBytes != nil {
			n := *previous.CompleteBytes
			previous.CompleteBytes = &n
		}
		previous.State = "interrupted"
		previous.CompleteStale = true
		scan.observation = previous
		s.publish(role, scan)
	}
}

func (s *cacheSampler) Snapshot() map[string]cacheSizeObservation {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]cacheSizeObservation, len(s.published))
	for role, o := range s.published {
		if o.CompleteBytes != nil {
			n := *o.CompleteBytes
			o.CompleteBytes = &n
		}
		out[role] = o
	}
	return out
}

func (s *cacheSampler) publish(role string, scan *cacheScan) {
	s.mu.Lock()
	s.published[role] = scan.observation
	s.mu.Unlock()
}

func (s *cacheSampler) Run(ctx context.Context) {
	if !s.running.CompareAndSwap(false, true) {
		return
	}
	defer s.running.Store(false)
	defer s.close()
	timer := time.NewTimer(cacheSampleInterval)
	defer timer.Stop()
	for {
		s.round(ctx)
		timer.Reset(cacheSampleInterval)
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
	}
}

func (s *cacheSampler) round(ctx context.Context) {
	for _, role := range []string{"main", "reviewer"} {
		if ctx.Err() != nil {
			return
		}
		scan := s.scans[role]
		s.slice(ctx, scan)
		s.publish(role, scan)
	}
}

func (s *cacheSampler) close() {
	for role, scan := range s.scans {
		if len(scan.stack) > 0 {
			scan.observation.State = "interrupted"
		}
		closeCacheScan(scan)
		s.publish(role, scan)
	}
}
func closeCacheScan(scan *cacheScan) {
	for _, cursor := range scan.stack {
		_ = cursor.file.Close()
	}
	scan.stack = nil
	scan.rootChecked = false
	scan.probedRoot = nil
}
func cacheDevice(info os.FileInfo) (string, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", false
	}
	return fmt.Sprint(stat.Dev), true
}
func scanError(scan *cacheScan, err error) {
	if err != nil && scan.observation.Error == "" {
		scan.observation.Error = err.Error()
	}
}

func (s *cacheSampler) start(scan *cacheScan, root os.FileInfo) bool {
	complete, started, at := scan.observation.CompleteBytes, scan.observation.CompleteStartedAt, scan.observation.CompleteAt
	stale := scan.observation.CompleteStale
	scan.observation = cacheSizeObservation{CompleteStale: stale, Path: scan.path, State: "partial", PassStartedAt: s.now(), CompleteBytes: complete, CompleteStartedAt: started, CompleteAt: at}
	scan.root = root
	file, err := os.Open(scan.path)
	if err != nil {
		scanError(scan, err)
		scan.observation.State = "incomplete"
		return false
	}
	if s.afterOpen != nil {
		s.afterOpen()
	}
	scan.stack = []cacheDirectoryCursor{{file: file, path: scan.path, expected: root}}
	return true
}

func (s *cacheSampler) slice(ctx context.Context, scan *cacheScan) {
	if ctx.Err() != nil {
		scan.observation.State = "interrupted"
		closeCacheScan(scan)
		return
	}
	deadline := s.now().Add(s.durationBudget)
	if !scan.rootChecked {
		root, err := os.Lstat(scan.path)
		if s.afterLstat != nil {
			s.afterLstat()
		}
		if err != nil || !root.IsDir() || root.Mode()&os.ModeSymlink != 0 {
			scan.observation.CompleteStale = true
			closeCacheScan(scan)
			if err == nil {
				err = errors.New("cache target is not a regular directory")
			}
			scanError(scan, err)
			scan.observation.State = "incomplete"
			scan.observation.ProgressAt = s.now()
			return
		}
		if scan.root != nil && !os.SameFile(scan.root, root) {
			closeCacheScan(scan)
			scan.observation.CompleteStale = true
			// Previous complete readings retain their original interval. The new pass
			// starts afresh and cannot combine bytes from the replaced directory.
		}
		scan.probedRoot = root
		scan.rootChecked = true
	}
	root := scan.probedRoot
	if ctx.Err() != nil {
		scan.observation.State = "interrupted"
		closeCacheScan(scan)
		return
	}
	if !s.now().Before(deadline) {
		return
	}
	if len(scan.stack) == 0 && !s.start(scan, root) {
		scan.observation.ProgressAt = s.now()
		return
	}
	// A slow root probe is consumed before probing again, so returning calls
	// cannot starve traversal. A later slice rechecks replacement.
	defer func() { scan.rootChecked = false }()
	examined := 0
	for len(scan.stack) > 0 && examined < s.entryBudget && s.now().Before(deadline) {
		if ctx.Err() != nil {
			scan.observation.State = "interrupted"
			closeCacheScan(scan)
			break
		}
		index := len(scan.stack) - 1
		cursor := &scan.stack[index]
		if !cursor.verified {
			actual, err := cursor.file.Stat()
			if s.afterStat != nil {
				s.afterStat()
			}
			if err != nil || !os.SameFile(cursor.expected, actual) {
				scanError(scan, errors.New("cache directory changed while opening"))
				_ = cursor.file.Close()
				scan.stack = scan.stack[:index]
			} else {
				cursor.verified = true
			}
			continue
		}
		if cursor.pending != nil {
			pending := cursor.pending
			file, err := os.Open(pending.path) //nolint:gosec // Retained metadata belongs to a validated non-symlink cache entry.
			if s.afterOpen != nil {
				s.afterOpen()
			}
			cursor.pending = nil
			if err != nil {
				scanError(scan, err)
				continue
			}
			scan.stack = append(scan.stack, cacheDirectoryCursor{file: file, path: pending.path, expected: pending.info})
			continue
		}
		if len(cursor.batch) == 0 {
			if cursor.readErr != nil {
				if !errors.Is(cursor.readErr, io.EOF) {
					scanError(scan, cursor.readErr)
				}
				_ = cursor.file.Close()
				scan.stack = scan.stack[:index]
				continue
			}
			if s.beforeRead != nil {
				s.beforeRead()
			}
			cursor.batch, cursor.readErr = cursor.file.ReadDir(256)
			// A filesystem call can exceed the deadline. Retain its returned batch,
			// and issue no further filesystem work until a later slice.
			if !s.now().Before(deadline) || ctx.Err() != nil {
				break
			}
			if len(cursor.batch) == 0 {
				continue
			}
		}
		entry := cursor.batch[0]
		cursor.batch = cursor.batch[1:]
		path := filepath.Join(cursor.path, entry.Name())
		examined++
		scan.observation.EntriesExamined++
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		info, err := os.Lstat(path)
		if s.afterLstat != nil {
			s.afterLstat()
		}
		if err != nil {
			scanError(scan, err)
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		rootDevice, rootOK := s.device(scan.root)
		device, ok := s.device(info)
		if !rootOK || !ok {
			scanError(scan, errors.New("cache filesystem identity unavailable"))
			continue
		}
		if device != rootDevice {
			scanError(scan, errors.New("cache scan reached another filesystem"))
			continue
		}
		if info.IsDir() {
			if len(scan.stack) >= cacheScanDepth {
				scanError(scan, errors.New("cache scan exceeds supported depth 64"))
				continue
			}
			// Metadata is a completed stage, even if its syscall used the whole
			// slice. Retain it rather than repeating a slow Lstat next round.
			cursor.pending = &cachePendingDirectory{path: path, info: info}
		} else if info.Mode().IsRegular() {
			scan.observation.PartialBytes += info.Size()
		}
	}
	scan.observation.ProgressAt = s.now()
	if ctx.Err() != nil {
		scan.observation.State = "interrupted"
		closeCacheScan(scan)
		return
	}
	switch {
	case len(scan.stack) == 0:
		if scan.observation.Error != "" {
			scan.observation.State = "incomplete"
			return
		}
		scan.observation.CompleteStale = false
		scan.observation.State = "complete"
		n := scan.observation.PartialBytes
		scan.observation.CompleteBytes = &n
		scan.observation.CompleteStartedAt = scan.observation.PassStartedAt
		scan.observation.CompleteAt = scan.observation.ProgressAt
	case scan.observation.Error != "":
		scan.observation.State = "incomplete"
	default:
		scan.observation.State = "partial"
	}
}
