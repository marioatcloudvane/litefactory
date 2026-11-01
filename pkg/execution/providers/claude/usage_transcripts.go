package claude

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"

	"github.com/cloudvane/litefactory/pkg/execution/agent"
)

// These reads are restricted to the caller-selected session under its isolated
// CLAUDE_CONFIG_DIR. No operator transcript search or historical import occurs.
// The CLI's 2.1.266 source uses projects/<escaped cwd>/<session>.jsonl and
// <session>/subagents[/<subdirectory>]/agent-<id>.jsonl. Only transcript records
// proving the selected session and (for a child) the filename agent id enter
// accounting. No source-supplied filesystem path is ever opened.
const (
	usageTranscriptBytes = 128 << 20
	usageTranscriptFiles = 4096
	usageTranscriptLine  = 8 << 20
)

var agentFileID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)

func transcriptAgentID(id string) bool { return agentFileID.MatchString(id) }

type usageTranscripts struct {
	base           string
	project        string
	offsets        map[string]int64
	parents        map[string]string
	identities     map[string]os.FileInfo
	entries        int
	incomplete     bool
	finalRemaining int64
}

type transcriptRecord struct {
	Type       string          `json:"type"`
	Session    string          `json:"sessionId"`
	AgentID    string          `json:"agentId"`
	Version    string          `json:"version"`
	Parent     string          `json:"parentToolUseID"`
	Message    json.RawMessage `json:"message"`
	ToolResult json.RawMessage `json:"toolUseResult"`
	Data       struct {
		Type    string          `json:"type"`
		AgentID string          `json:"agentId"`
		Message json.RawMessage `json:"message"`
	} `json:"data"`
}

func (c *UsageCollector) prepareTranscripts(configDir, cwd string) {
	if configDir == "" || !filepath.IsAbs(configDir) {
		c.diagnostic("session_source_unavailable")
		return
	}
	project := transcriptProjectKey(cwd)
	c.transcripts = &usageTranscripts{base: configDir, project: filepath.Join("projects", project), offsets: make(map[string]int64), parents: make(map[string]string), identities: make(map[string]os.FileInfo)}
	c.collectTranscripts(true)
	// The baseline is source evidence, not work performed by this execution.
	c.baselineSeen = maps.Clone(c.seen)
	c.transcripts.parents = c.invocations
	c.invocations = make(map[string]string)
	c.pending = nil
	c.pendingBytes = 0
	c.observed = false
	c.measured = make(map[string]bool)
	c.completed = make(map[string]bool)
	c.coverage.Status = agent.CollectionUnknown
}

func (c *UsageCollector) associateAgent(id, invocation, parent string) {
	if !transcriptAgentID(id) || !usageIdentity(invocation) {
		c.diagnostic("invalid_child_identity")
		return
	}
	if old, ok := c.agents[id]; ok && old != invocation && c.invocations[old] != "" && !c.completed[old] {
		c.diagnostic("conflicting_child_identity")
		return
	}
	if len(c.agents) >= maxUsageIdentities {
		c.diagnostic("usage_identity_limit")
		return
	}
	// The link must name an Agent/Task call already observed in its parent.
	knownParent, ok := c.invocations[invocation]
	if !ok && c.transcripts != nil {
		knownParent, ok = c.transcripts.parents[invocation]
	}
	if !ok || knownParent != parent {
		c.diagnostic("unresolved_parent")
		return
	}
	c.agents[id] = invocation
}

func (c *UsageCollector) collectTranscripts(baseline bool) {
	if c == nil || c.transcripts == nil {
		return
	}
	if c.ctx.Err() != nil {
		c.diagnostic("session_finalization_cancelled")
		return
	}
	ctx, cancel := context.WithTimeout(c.ctx, 2*time.Second)
	defer cancel()
	originalCtx := c.ctx
	c.ctx = ctx
	defer func() { c.ctx = originalCtx }()
	c.transcripts.entries = 0
	c.transcripts.incomplete = false
	c.baselining = baseline
	defer func() { c.baselining = false }()
	base, err := os.OpenRoot(c.transcripts.base)
	if err != nil {
		c.diagnostic("session_source_unavailable")
		return
	}
	defer func() {
		if err := base.Close(); err != nil {
			c.diagnostic("session_source_close_failed")
		}
	}()
	for _, part := range []string{"projects", c.transcripts.project} {
		info, err := base.Lstat(part)
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			c.diagnostic("unsafe_session_source")
			return
		}
	}
	root, err := base.OpenRoot(c.transcripts.project)
	if err != nil {
		if baseline && !c.resumed && errors.Is(err, os.ErrNotExist) {
			c.baselineReady = true
			return
		}
		c.diagnostic("session_source_unavailable")
		return
	}
	defer func() {
		if err := root.Close(); err != nil {
			c.diagnostic("session_source_close_failed")
		}
	}()
	var files []string
	rootFile := c.session + ".jsonl"
	files = append(files, rootFile)
	remaining := int64(usageTranscriptBytes)
	if !baseline {
		remaining = c.transcripts.finalRemaining
		defer func() { c.transcripts.finalRemaining = remaining }()
	}
	complete := true
	// Root records reveal tool-result and progress links before child records.
	if !c.readTranscript(ctx, root, rootFile, "", baseline, &remaining) {
		complete = false
		c.transcripts.incomplete = true
	}
	directory := filepath.Join(c.session, "subagents")
	if err := c.listTranscripts(ctx, root, directory, 0, &files); err != nil && !errors.Is(err, os.ErrNotExist) {
		c.diagnostic("session_source_limit_or_read_failed")
		complete = false
		c.transcripts.incomplete = true
	}
	slices.Sort(files[1:])
	unresolved := files[1:]
	// A linked child can disclose its own children. Bounded passes permit any
	// lexical filename order without guessing an unresolved child's parent.
	for depth := 0; len(unresolved) > 0 && depth < 16; depth++ {
		next := make([]string, 0, len(unresolved))
		progress := false
		for _, name := range unresolved {
			id := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(name), "agent-"), ".jsonl")
			if _, ok := c.agents[id]; !ok {
				next = append(next, name)
				continue
			}
			if !c.readTranscript(ctx, root, name, id, baseline, &remaining) {
				complete = false
				c.transcripts.incomplete = true
			}
			progress = true
		}
		if !progress {
			unresolved = next
			break
		}
		unresolved = next
	}
	if len(unresolved) > 0 && baseline {
		// Unlinked old child transcripts still establish message baselines. Their
		// records must prove this session and their agent filename before use.
		for _, name := range unresolved {
			id := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(name), "agent-"), ".jsonl")
			if !c.readTranscript(ctx, root, name, id, true, &remaining) {
				complete = false
				c.transcripts.incomplete = true
			}
		}
	} else if len(unresolved) > 0 {
		c.diagnostic("unresolved_descendant")
		complete = false
		c.transcripts.incomplete = true
	}
	if baseline {
		c.baselineReady = complete && !c.baselineInvalid
		if !complete {
			c.diagnostic("resume_baseline_unverified")
		}
	}
	c.coverage.SourceFormat = "stream-json+session-jsonl"
}

func (c *UsageCollector) listTranscripts(ctx context.Context, root *os.Root, name string, depth int, files *[]string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if depth > 8 || len(*files) >= usageTranscriptFiles {
		return errors.New("transcript enumeration limit")
	}
	info, err := root.Lstat(name)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("unsafe transcript directory")
	}
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	defer func() {
		if err := f.Close(); err != nil {
			c.diagnostic("session_source_close_failed")
		}
	}()
	opened, err := f.Stat()
	if err != nil || !opened.IsDir() {
		return errors.New("unsafe transcript directory")
	}
	for {
		entries, err := f.ReadDir(64)
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		for _, e := range entries {
			c.transcripts.entries++
			if c.transcripts.entries > 8192 {
				return errors.New("transcript entry limit")
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if e.Type()&os.ModeSymlink != 0 {
				return errors.New("symlinked transcript entry")
			}
			child := filepath.Join(name, e.Name())
			if e.IsDir() {
				if err := c.listTranscripts(ctx, root, child, depth+1, files); err != nil {
					return err
				}
				continue
			}
			id := strings.TrimSuffix(strings.TrimPrefix(e.Name(), "agent-"), ".jsonl")
			if strings.HasPrefix(e.Name(), "agent-") && strings.HasSuffix(e.Name(), ".jsonl") && transcriptAgentID(id) {
				*files = append(*files, child)
				if len(*files) >= usageTranscriptFiles {
					return errors.New("transcript file limit")
				}
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
	}
}

func (c *UsageCollector) readTranscript(ctx context.Context, root *os.Root, name, id string, baseline bool, remaining *int64) bool {
	// Lstat rejects symlinks, and Root confines races to the selected project.
	// O_NONBLOCK prevents a swapped FIFO from wedging the runner; O_NOFOLLOW
	// rejects a swapped final symlink. The descriptor itself must be regular.
	for part := name; part != "."; part = filepath.Dir(part) {
		info, err := root.Lstat(part)
		if err != nil {
			if baseline && !c.resumed && errors.Is(err, os.ErrNotExist) {
				return true
			}
			c.diagnostic("session_source_unavailable")
			return false
		}
		if info.Mode()&os.ModeSymlink != 0 {
			c.diagnostic("unsafe_session_source")
			return false
		}
	}
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		c.diagnostic("session_source_unavailable")
		return false
	}
	defer func() {
		if err := f.Close(); err != nil {
			c.diagnostic("session_source_close_failed")
		}
	}()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		c.diagnostic("unsafe_session_source")
		return false
	}
	if previous, ok := c.transcripts.identities[name]; ok && !os.SameFile(previous, info) {
		c.diagnostic("session_source_replaced")
		return false
	}
	c.transcripts.identities[name] = info
	offset := int64(0)
	if !baseline {
		offset = c.transcripts.offsets[name]
	}
	if info.Size() < offset {
		c.diagnostic("session_source_regressed")
		return false
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		c.diagnostic("session_source_unavailable")
		return false
	}
	reader := bufio.NewReaderSize(f, 64<<10)
	for {
		if ctx.Err() != nil || *remaining <= 0 {
			c.diagnostic("session_source_limit_or_read_failed")
			return false
		}
		var line []byte
		for {
			part, err := reader.ReadSlice('\n')
			line = append(line, part...)
			*remaining -= int64(len(part))
			if len(line) > usageTranscriptLine || *remaining < 0 {
				c.diagnostic("session_source_limit_or_read_failed")
				return false
			}
			if errors.Is(err, bufio.ErrBufferFull) {
				continue
			}
			if errors.Is(err, io.EOF) {
				if len(line) > 0 {
					c.diagnostic("incomplete_session_record")
					return false
				}
				return true
			}
			if err != nil {
				c.diagnostic("session_source_unavailable")
				return false
			}
			break
		}
		offset += int64(len(line))
		c.transcripts.offsets[name] = offset
		var record transcriptRecord
		if json.Unmarshal(line, &record) != nil {
			c.diagnostic("malformed_session_record")
			return false
		}
		if record.Type != "assistant" && record.Type != "user" && record.Type != "progress" {
			continue
		}
		if record.Session != c.session || (id != "" && record.AgentID != id) {
			c.diagnostic("unassociated_session_record")
			continue
		}
		if usageIdentity(record.Version) {
			c.coverage.Version = record.Version
		}
		invocation := "root"
		if id != "" {
			invocation = c.agents[id]
			if invocation == "" {
				if baseline {
					c.seedMessage(record.Message)
					continue
				}
				c.diagnostic("unresolved_descendant")
				continue
			}
		}
		parent := ""
		if invocation != "root" {
			parent = strings.TrimPrefix(invocation, "tool:")
		}
		if record.Type == "progress" && record.Data.Type == "agent_progress" {
			c.associateAgent(record.Data.AgentID, "tool:"+record.Parent, invocation)
			continue
		}
		if record.Type != "assistant" && record.Type != "user" {
			continue
		}
		event := struct {
			Type       string          `json:"type"`
			Session    string          `json:"session_id"`
			Parent     string          `json:"parent_tool_use_id"`
			Message    json.RawMessage `json:"message"`
			ToolResult json.RawMessage `json:"tool_use_result"`
		}{record.Type, c.session, parent, record.Message, record.ToolResult}
		encoded, err := json.Marshal(event)
		if err != nil {
			c.diagnostic("malformed_session_record")
			continue
		}
		c.observe(encoded)
	}
}

func (c *UsageCollector) seedMessage(raw json.RawMessage) {
	var m struct {
		ID    string        `json:"id"`
		Model string        `json:"model"`
		Usage *messageUsage `json:"usage"`
	}
	if json.Unmarshal(raw, &m) != nil || m.Usage == nil || !usageIdentity(m.ID) {
		return
	}
	// Unknown ancestry need not prevent deduplicating a source-owned message id.
	c.rememberBaseline(m.ID, m.Model, m.Usage.tokens())
}

func (c *UsageCollector) rememberBaseline(id, model string, tokens agent.TokenUsage) {
	if agent.Validate(tokens) != nil {
		c.diagnostic("invalid_usage_counters")
		return
	}
	if !usageIdentity(model) {
		model = ""
	}
	identity, _ := json.Marshal([]string{id, model})
	key := string(identity)
	if len(c.messages) >= maxUsageIdentities {
		c.diagnostic("usage_identity_limit")
		return
	}
	if prior, ok := c.messages[key]; ok {
		if _, err := agent.Delta(tokens, prior); err != nil {
			c.diagnostic("regressing_message_usage")
			return
		}
	}
	c.messages[key] = tokens.Clone()
	encoded, _ := json.Marshal(tokens)
	digest := sha256.Sum256(encoded)
	c.seen[key+":"+hex.EncodeToString(digest[:])] = true
}

func (c *UsageCollector) restoreInvocation(id string, depth int) bool {
	if _, ok := c.invocations[id]; ok {
		return true
	}
	if depth > 16 || c.transcripts == nil {
		return false
	}
	parent, ok := c.transcripts.parents[id]
	if !ok {
		return false
	}
	if parent != "" && !c.restoreInvocation(parent, depth+1) {
		return false
	}
	return c.invocation(id, parent)
}

// Finalization drains delayed transcript writes under one two-second deadline,
// including after subprocess cancellation. This is a collection drain, not an
// execution retry; it owns no goroutine and never changes the process outcome.
func (c *UsageCollector) finalizeTranscripts() {
	if c == nil || c.transcripts == nil {
		return
	}
	originalCtx := c.ctx
	evidence, stopEvidence := c.capture.CollectionContext(originalCtx)
	defer stopEvidence()
	ctx, cancel := context.WithTimeout(evidence, 2*time.Second)
	defer cancel()
	c.ctx = ctx
	c.transcripts.finalRemaining = usageTranscriptBytes
	defer func() { c.ctx = originalCtx }()
	for {
		c.collectTranscripts(false)
		missing := c.transcripts.incomplete
		for invocation := range c.invocations {
			if invocation != "root" && (!c.completed[invocation] || !c.measured[invocation]) {
				missing = true
			}
		}
		if !missing || c.transcripts.finalRemaining <= 0 {
			return
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			c.diagnostic("session_finalization_incomplete")
			return
		case <-timer.C:
		}
	}
}

// Match Claude 2.1.266's UTF-16 project escaping and signed 32-bit path hash,
// including long/non-ASCII worktree paths. This is a path key, not attribution.
func transcriptProjectKey(path string) string {
	units := utf16.Encode([]rune(path))
	escaped := make([]byte, len(units))
	var hash uint32
	for i, ch := range units {
		hash = hash*31 + uint32(ch)
		escaped[i] = '-'
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') {
			escaped[i] = byte(ch)
		}
	}
	if len(escaped) <= 200 {
		return string(escaped)
	}
	signed := int64(int32(hash))
	if signed < 0 {
		signed = -signed
	}
	return string(escaped[:200]) + "-" + strconv.FormatInt(signed, 36)
}
