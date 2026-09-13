package codex

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/meter"
)

// Rollouts are read only within the subprocess's existing HOME. No config,
// credentials, transcript bodies or paths cross the usage sink. This collector
// runs synchronously on the stdout reader and after Wait; no worker outlives Run.
// Bounds cover each discovery and the total bytes consumed over the attempt.
const (
	rolloutEntries   = 100000
	rolloutFiles     = 4096
	rolloutBytes     = 256 << 20
	rolloutLineBytes = 2 << 20
)

type rolloutBaseline struct {
	size int64
	info fs.FileInfo
}
type rolloutUsage struct {
	root      *os.Root
	home      string
	baseline  map[string]rolloutBaseline
	files     map[string]*rolloutFile
	lastScan  time.Time
	bytes     int64
	disabled  bool
	responses int
}
type rolloutFile struct {
	path                string
	info                fs.FileInfo
	id, parent, version string
	baseline, offset    int64
	model               string
	modelTurn           string
	terminal            bool
	observations        int
	associated          bool
	seen                map[string][32]byte
	digest              hash.Hash
}
type rolloutRecord struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}
type rolloutMeta struct {
	ID      string `json:"id"`
	Parent  string `json:"parent_thread_id"`
	Version string `json:"cli_version"`
	Fork    string `json:"forked_from_id"`
	Source  struct {
		SubAgent struct {
			ThreadSpawn struct {
				Parent string `json:"parent_thread_id"`
			} `json:"thread_spawn"`
		} `json:"subagent"`
	} `json:"source"`
}
type rolloutTokens struct {
	Input     *int64 `json:"input_tokens"`
	Read      *int64 `json:"cached_input_tokens"`
	Write     *int64 `json:"cache_write_input_tokens"`
	Output    *int64 `json:"output_tokens"`
	Reasoning *int64 `json:"reasoning_output_tokens"`
}

func (u rolloutTokens) normalized() (agent.TokenUsage, error) {
	return meter.Normalize(agent.TokenUsage{InputUncached: u.Input, CacheRead: u.Read, CacheWrite: u.Write, Output: u.Output, Reasoning: u.Reasoning}, true)
}

// prepareRollouts captures byte boundaries before process launch. Old rows are
// decoded only to seed model context, never charged again.
// Exact record format: openai/codex commit
// 41e22fee981a63b3698df7ed36bad393cda24715 (rust-v0.153.0),
// codex-rs/{protocol/src/protocol.rs,history/src/rollout_payload.rs}.
func (c *usageCollector) prepareRollouts(environ []string) {
	if c == nil {
		return
	}
	home := envValue(environ, "HOME")
	if !filepath.IsAbs(home) {
		c.diagnostic("usage_source_home_missing")
		return
	}
	root, err := os.OpenRoot(home)
	if err != nil {
		c.diagnostic("usage_source_unavailable")
		return
	}
	defer func() {
		if err := root.Close(); err != nil {
			c.diagnostic("usage_source_close_failed")
		}
	}()
	for _, path := range []string{".codex", ".codex/sessions"} {
		info, err := root.Lstat(path)
		if errors.Is(err, fs.ErrNotExist) {
			c.rollouts = &rolloutUsage{home: home, baseline: make(map[string]rolloutBaseline), files: make(map[string]*rolloutFile)}
			return
		}
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			c.diagnostic("usage_source_unavailable")
			return
		}
	}
	sessions, err := root.OpenRoot(".codex/sessions")
	if err != nil {
		c.diagnostic("usage_source_unavailable")
		return
	}
	c.rollouts = &rolloutUsage{root: sessions, baseline: make(map[string]rolloutBaseline), files: make(map[string]*rolloutFile)}
	if err := c.walkRollouts(func(path string, info fs.FileInfo) error {
		c.rollouts.baseline[path] = rolloutBaseline{info.Size(), info}
		return nil
	}); err != nil {
		c.diagnostic("usage_source_inventory_incomplete")
		c.rollouts.disabled = true
	}
}
func (c *usageCollector) walkRollouts(visit func(string, fs.FileInfo) error) error {
	count := 0
	started := time.Now()
	dirs := []string{"."}
	for len(dirs) > 0 {
		if err := c.ctx.Err(); err != nil {
			return err
		}
		if time.Since(started) > 2*time.Second {
			return fmt.Errorf("rollout discovery deadline")
		}
		path := dirs[len(dirs)-1]
		dirs = dirs[:len(dirs)-1]
		dir, err := c.rollouts.root.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
		if err != nil {
			return err
		}
		err = func() error {
			info, err := dir.Stat()
			if err != nil {
				return err
			}
			if !info.IsDir() {
				return fmt.Errorf("rollout directory changed")
			}
			for {
				if err := c.ctx.Err(); err != nil {
					return err
				}
				if time.Since(started) > 2*time.Second {
					return fmt.Errorf("rollout discovery deadline")
				}
				entries, readErr := dir.ReadDir(128)
				for _, entry := range entries {
					count++
					if count > rolloutEntries {
						return fmt.Errorf("rollout directory limit")
					}
					if entry.Type()&os.ModeSymlink != 0 {
						continue
					}
					child := filepath.Join(path, entry.Name())
					if entry.IsDir() {
						if strings.Count(child, string(filepath.Separator)) <= 3 {
							dirs = append(dirs, child)
						}
						continue
					}
					if !strings.HasPrefix(entry.Name(), "rollout-") || !strings.HasSuffix(entry.Name(), ".jsonl") {
						continue
					}
					info, err := entry.Info()
					if err != nil {
						return err
					}
					if !info.Mode().IsRegular() {
						continue
					}
					if err := visit(child, info); err != nil {
						return err
					}
				}
				if errors.Is(readErr, io.EOF) {
					return nil
				}
				if readErr != nil {
					return readErr
				}
			}
		}()
		closeErr := dir.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}
func (c *usageCollector) collectRollouts(final bool) {
	if c == nil || c.rollouts == nil || c.rollouts.disabled || c.thread == "" {
		return
	}
	r := c.rollouts
	if r.root == nil {
		home, err := os.OpenRoot(r.home)
		if err != nil {
			c.diagnostic("usage_source_unavailable")
			return
		}
		defer func() {
			if err := home.Close(); err != nil {
				c.diagnostic("usage_source_close_failed")
			}
		}()
		for _, path := range []string{".codex", ".codex/sessions"} {
			info, err := home.Lstat(path)
			if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				if final {
					c.diagnostic("usage_source_unavailable")
				}
				return
			}
		}
		r.root, err = home.OpenRoot(".codex/sessions")
		if err != nil {
			c.diagnostic("usage_source_unavailable")
			return
		}
	}
	if !final && time.Since(r.lastScan) < time.Second {
		return
	}
	r.lastScan = time.Now()
	deadline, cancel := context.WithTimeout(c.ctx, 2*time.Second)
	defer cancel()
	err := c.walkRollouts(func(path string, info fs.FileInfo) error {
		if err := deadline.Err(); err != nil {
			return err
		}
		if _, ok := r.files[path]; ok {
			return nil
		}
		// Metadata remains visible even when an ancestor has no new bytes:
		// active grandchildren still need its parent link. Byte baselines below
		// continue to exclude its historical response records.
		if len(r.files) >= rolloutFiles {
			return fmt.Errorf("rollout file limit")
		}
		file, err := r.root.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
		if err != nil {
			return err
		}
		opened, err := file.Stat()
		if err != nil {
			_ = file.Close()
			return err
		}
		if !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
			_ = file.Close()
			return fmt.Errorf("rollout changed")
		}
		reader := bufio.NewReaderSize(file, rolloutLineBytes)
		line, err := reader.ReadSlice('\n')
		closeErr := file.Close()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		r.bytes += int64(len(line))
		if r.bytes > rolloutBytes {
			return fmt.Errorf("rollout byte limit")
		}
		var record rolloutRecord
		// A line this collector cannot decode, or that is not session metadata,
		// is skipped rather than failed: its usage stays unknown, which is not
		// the same as measured zero.
		decoded := json.Unmarshal(line, &record) == nil
		if !decoded || record.Type != "session_meta" {
			c.diagnostic("unsupported_rollout_metadata")
			return nil
		}
		var meta rolloutMeta
		// source can be a string for root sessions; decode identity separately.
		if json.Unmarshal(record.Payload, &meta) != nil {
			var base struct {
				ID      string `json:"id"`
				Parent  string `json:"parent_thread_id"`
				Version string `json:"cli_version"`
				Fork    string `json:"forked_from_id"`
			}
			// Neither shape decodes: skip the record, leaving usage unknown.
			if decoded := json.Unmarshal(record.Payload, &base) == nil; !decoded {
				return nil
			}
			meta.ID, meta.Parent, meta.Version, meta.Fork = base.ID, base.Parent, base.Version, base.Fork
		}
		if !validUsageID(meta.ID) {
			return nil
		}
		if meta.Parent != "" && meta.Source.SubAgent.ThreadSpawn.Parent != "" && meta.Parent != meta.Source.SubAgent.ThreadSpawn.Parent {
			c.diagnostic("conflicting_parent")
			return nil
		}
		if meta.Parent == "" {
			meta.Parent = meta.Source.SubAgent.ThreadSpawn.Parent
		}
		if meta.Parent != "" && !validUsageID(meta.Parent) {
			return nil
		}
		baseline := int64(0)
		if old, ok := r.baseline[path]; ok {
			if !os.SameFile(old.info, opened) || opened.Size() < old.size {
				c.diagnostic("usage_source_replaced")
				return nil
			}
			baseline = old.size
		}
		for existingPath, existing := range r.files {
			if err := deadline.Err(); err != nil {
				return err
			}
			if existing.id == meta.ID && existingPath != path {
				c.diagnostic("duplicate_rollout_identity")
				return nil
			}
		}
		r.files[path] = &rolloutFile{path: path, info: opened, id: meta.ID, parent: meta.Parent, version: meta.Version, baseline: baseline}
		return nil
	})
	if err != nil {
		c.diagnostic("usage_source_scan_incomplete")
	}
	// Resolve parents before children even when filesystem order is unrelated.
	for pass := 0; pass < len(r.files); pass++ {
		if deadline.Err() != nil {
			c.diagnostic("usage_source_scan_incomplete")
			return
		}
		progress := false
		for _, file := range r.files {
			if deadline.Err() != nil {
				c.diagnostic("usage_source_scan_incomplete")
				return
			}
			if file.associated {
				continue
			}
			if file.id == c.thread {
				if file.parent != "" {
					c.diagnostic("conflicting_parent")
					continue
				}
			} else {
				if file.parent == "" {
					continue
				}
				if _, ok := c.parents[file.parent]; !ok {
					continue
				}
				if old, ok := c.parents[file.id]; ok && old != file.parent {
					c.diagnostic("conflicting_parent")
					continue
				}
			}
			file.associated = true
			progress = true
			c.invocation(file.id, file.parent)
		}
		if !progress {
			break
		}
	}
	for _, file := range r.files {
		if deadline.Err() != nil {
			c.diagnostic("usage_source_scan_incomplete")
			return
		}
		if !file.associated {
			continue
		}
		if err := c.readRollout(deadline, file); err != nil {
			c.diagnostic("usage_source_read_incomplete")
		}
	}

}
func (c *usageCollector) readRollout(ctx context.Context, state *rolloutFile) error {
	file, err := c.rollouts.root.OpenFile(state.path, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer func() {
		if err := file.Close(); err != nil {
			c.diagnostic("usage_source_close_failed")
		}
	}()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || !os.SameFile(state.info, info) || info.Size() < state.offset {
		return fmt.Errorf("rollout replaced or truncated")
	}
	if _, err = file.Seek(state.offset, io.SeekStart); err != nil {
		return err
	}
	if state.digest == nil {
		state.digest = sha256.New()
	}
	reader := bufio.NewReaderSize(file, rolloutLineBytes)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		line, err := reader.ReadSlice('\n')
		if errors.Is(err, io.EOF) {
			return nil
		} // incomplete final line is retried next scan
		if err != nil {
			return err
		}
		c.rollouts.bytes += int64(len(line))
		if c.rollouts.bytes > rolloutBytes {
			c.rollouts.disabled = true
			return fmt.Errorf("rollout byte limit")
		}
		_, _ = state.digest.Write(line)
		start := state.offset
		state.offset += int64(len(line))
		var record rolloutRecord
		if json.Unmarshal(line, &record) != nil {
			c.diagnostic("malformed_usage_event")
			continue
		}
		historical := start < state.baseline
		// The historical root already has a durable measurement. Read its
		// bytes only to attest identity/content; never reinterpret its totals.
		if c.window != nil && state.id == c.thread {
			continue
		}
		if c.window != nil && record.Type == "token_usage_record" {
			at, timeErr := time.Parse(time.RFC3339Nano, record.Timestamp)
			if timeErr != nil {
				c.diagnostic("historical_timestamp_missing")
				historical = true
			} else if at.Before(c.window.From) || !at.Before(c.window.To) {
				historical = true
			}
		}
		c.rolloutRecord(state, record, historical)
	}
}
func (c *usageCollector) rolloutRecord(state *rolloutFile, record rolloutRecord, historical bool) {
	if record.Type == "turn_context" {
		var turn struct {
			Model  string `json:"model"`
			TurnID string `json:"turn_id"`
		}
		if json.Unmarshal(record.Payload, &turn) != nil || !validUsageID(turn.Model) || !validUsageID(turn.TurnID) {
			state.model = ""
			state.modelTurn = ""
			c.diagnostic("model_attribution_unavailable")
			return
		}
		state.model = turn.Model
		state.modelTurn = turn.TurnID
		state.terminal = false
		return
	}
	if record.Type == "event_msg" {
		var event struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(record.Payload, &event) != nil {
			c.diagnostic("malformed_usage_event")
			return
		}
		if event.Type == "task_complete" || event.Type == "turn_complete" || event.Type == "turn_aborted" {
			state.terminal = true
		}
		return
	}
	if record.Type != "token_usage_record" {
		return
	}
	var response struct {
		Thread   string        `json:"thread_id"`
		Turn     string        `json:"turn_id"`
		Response string        `json:"response_id"`
		Usage    rolloutTokens `json:"usage"`
	}
	if json.Unmarshal(record.Payload, &response) != nil || !validUsageID(response.Response) || !validUsageID(response.Turn) {
		c.diagnostic("malformed_usage_event")
		return
	}
	// Forked/replayed parent response records are not work by this invocation.
	if response.Thread != state.id {
		c.diagnostic("inherited_usage_excluded")
		return
	}
	tokens, err := response.Usage.normalized()
	if err != nil {
		c.diagnostic("invalid_usage_counters")
		return
	}
	encoded, _ := json.Marshal(response)
	digest := sha256.Sum256(encoded)
	if previous, ok := state.seen[response.Response]; ok {
		if previous != digest {
			c.diagnostic("conflicting_response_identity")
		}
		return
	}
	if c.rollouts.responses >= 100000 {
		c.diagnostic("usage_identity_limit")
		return
	}
	if state.seen == nil {
		state.seen = make(map[string][32]byte)
	}
	state.seen[response.Response] = digest
	c.rollouts.responses++
	if historical {
		return
	}
	state.observations++
	model := state.model
	coverage := c.coverage.Clone()
	coverage.Status = agent.CollectionPartial
	coverage.SourceFormat = "rollout-token-usage-record-v1"
	coverage.Version = state.version
	var actual *string
	provenance := "unknown"
	if model != "" && response.Turn == state.modelTurn {
		actual = &model
		provenance = "runtime_turn_context"
	} else {
		c.diagnostic("model_attribution_unavailable")
	}
	c.coverage.Status = agent.CollectionPartial
	c.coverage.SourceFormat = "exec-jsonl+rollout-token-usage-record-v1"
	if state.id == c.thread {
		c.coverage.Version = state.version
	}
	c.emit(agent.UsageEvent{Observation: &agent.UsageObservation{AttemptID: c.capture.AttemptID, SourceID: state.id, EventKey: "response:" + response.Response, InvocationID: state.id, ActualModel: actual, ModelProvenance: provenance, Mode: agent.CounterIncremental, Scope: agent.UsageExclusive, Tokens: tokens, Coverage: coverage}})
}

func (c *usageCollector) diagnoseRollouts() {
	if c.rollouts == nil {
		return
	}
	found := make(map[string]bool, len(c.rollouts.files))
	for _, file := range c.rollouts.files {
		if !file.associated {
			continue
		}
		found[file.id] = true
		if file.observations == 0 {
			c.diagnostic("usage_response_records_missing")
		}
		if !file.terminal {
			c.diagnostic("descendant_finalization_incomplete")
		}
	}
	if !found[c.thread] {
		c.diagnostic("usage_source_unavailable")
	}
	for id := range c.parents {
		if !found[id] {
			c.diagnostic("descendant_usage_unavailable")
			break
		}
	}
}
