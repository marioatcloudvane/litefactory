package logevidence

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/logreader"
)

const digestPageItems = 200
const digestRecordMaximum = 16 << 20
const digestScanMaximum = 64 << 20
const messageBytes = 1500
const commandBytes = 300
const errorTailBytes = 300

// DigestRequest pages through the condensed view of one run.
type DigestRequest struct {
	RunID  int64  `json:"run_id"`
	Limit  int    `json:"limit,omitempty"`
	Cursor string `json:"cursor,omitempty"`
}

// DigestEntry is one condensed step. Offset is the source record start, usable
// with run_logs_read for an exact citation.
type DigestEntry struct {
	Offset int64    `json:"offset"`
	Kind   string   `json:"kind"`
	Text   string   `json:"text"`
	Exit   *int     `json:"exit,omitempty"`
	Failed bool     `json:"failed,omitempty"`
	Error  string   `json:"error,omitempty"`
	Files  []string `json:"files,omitempty"`
}

// DigestCounts summarizes the whole run on every page.
type DigestCounts struct {
	Entries  int `json:"entries"`
	Messages int `json:"messages"`
	Commands int `json:"commands"`
	Tools    int `json:"tool_calls"`
	Edits    int `json:"edits"`
	Failed   int `json:"failed"`
	Errors   int `json:"errors"`
	// Reasoning counts provider thinking summaries (settings.thinking_logs).
	Reasoning int `json:"reasoning,omitempty"`
	Withheld  int `json:"withheld_records,omitempty"`
	// Subagents counts helpers the agent started for its own work: Codex
	// collab spawn_agent receivers and Claude Task/Agent tool calls.
	Subagents int `json:"subagents,omitempty"`
	// ParallelBatches counts assistant turns that issued two or more tool
	// calls at once. Only Claude streams show this (tool_use blocks sharing
	// one message id); Codex items do not mark a shared turn.
	ParallelBatches int `json:"parallel_tool_batches,omitempty"`
}

func positionAt(n int) logreader.Position { return logreader.Position{Offset: int64(n)} }

func (s *Service) digest(ctx context.Context, r DigestRequest) Result {
	if r.Limit == 0 {
		r.Limit = 80
	}
	row, err := s.db.GetPriorRun(ctx, s.query(), r.RunID)
	if err != nil {
		return failureResult("unavailable")
	}
	src, status := s.openSource(row)
	if status != "" {
		if status == "unavailable" {
			return sourceUnavailable()
		}
		return failureResult(status)
	}
	defer src.close()
	fp := fileFingerprint(src.info)
	start := 0
	if r.Cursor != "" {
		c, ok := s.unsign(r.Cursor)
		if !ok || c.Kind != "digest" || c.RunID != r.RunID {
			return failureResult("expired_cursor")
		}
		if c.Fingerprint != fp {
			return failureResult("source_changed")
		}
		start = int(c.Position.Offset)
	}
	d := digestBuilder{calls: map[string]int{}, turnCalls: map[string]int{}}
	pos := logreader.Position{}
	scanned, eof := 0, false
	for !eof && scanned < digestScanMaximum {
		page, e := logreader.Scan(ctx, src.file, pos, 256, digestRecordMaximum, digestScanMaximum-scanned)
		if e != nil {
			return failureResult("unavailable")
		}
		scanned += int(page.Scanned)
		for _, rec := range page.Records {
			if rec.Omission != "" || !d.add(rec.Start, rec.Data) {
				d.counts.Withheld++
			}
		}
		pos, eof = page.Next, page.EOF || page.Next.Offset >= src.info.Size()
	}
	after, e := src.file.Stat()
	if e != nil || fileFingerprint(after) != fp {
		return failureResult("source_changed")
	}
	summary := d.counts
	summary.Entries = len(d.entries)
	out := Result{Status: "ok", Policy: PolicyVersion, DigestSummary: &summary, Coverage: Coverage{RetentionOnly: true, Fingerprint: fp, ScannedEnd: pos.Offset, RequestedEnd: src.info.Size()}}
	if summary.Withheld > 0 || !eof {
		out.Coverage.Incomplete = true
		out.Coverage.Omissions = []Omission{{Reason: fmt.Sprintf("%d records withheld or unsupported", summary.Withheld)}}
	}
	out.PageLimit = r.Limit
	next := start
	for ; next < len(d.entries) && len(out.Digest) < r.Limit; next++ {
		entry, e := s.redactEntry(ctx, d.entries[next])
		if e != nil {
			return failureResult("redaction_failure")
		}
		out.Digest = append(out.Digest, entry)
		if encodedSize(out, strings.Repeat("x", 128)) > outcomePageTarget && len(out.Digest) > 1 {
			out.Digest = out.Digest[:len(out.Digest)-1]
			break
		}
	}
	if next < len(d.entries) {
		out.Coverage.Incomplete = true
		out.NextCursor = s.sign(cursor{Kind: "digest", RunID: r.RunID, Fingerprint: fp, Position: positionAt(next)})
	} else {
		out.Coverage.EndOfSource = eof
	}
	return out
}

// worktreePrefix only names the private checkout of a task.
var worktreePrefix = regexp.MustCompile(`[^\s"'=]*/task-[0-9]+/`)

// redactEntry redacts complete decoded values before bounding them.
func (s *Service) redactEntry(ctx context.Context, e DigestEntry) (DigestEntry, error) {
	e.Text = worktreePrefix.ReplaceAllString(e.Text, "")
	limit := commandBytes
	if e.Kind == "message" || e.Kind == "error" {
		limit = messageBytes
	}
	// Reasoning is the analyst's primary evidence for why an agent acted, so
	// it keeps the whole redacted thinking, bounded only like the record. The
	// analysis budget and page size bound the cost.
	if e.Kind == "reasoning" {
		limit = digestRecordMaximum
	}
	text, err := s.redactor.Apply(ctx, e.Text, digestRecordMaximum)
	if err != nil {
		return e, err
	}
	failure, err := s.redactor.Apply(ctx, e.Error, digestRecordMaximum)
	if err != nil {
		return e, err
	}
	e.Text, e.Error = text.String(), failure.String()
	if len(e.Text) > limit {
		e.Text = truncateUTF8(e.Text, limit) + "…"
	}
	// An error keeps its end, where the failure is reported.
	if len(e.Error) > errorTailBytes {
		e.Error = "…" + tail(e.Error, errorTailBytes)
	}
	files := make([]string, 0, len(e.Files))
	for _, f := range e.Files {
		body, err := s.redactor.Apply(ctx, f, 4096)
		if err != nil {
			return e, err
		}
		files = append(files, body.String())
	}
	e.Files = files
	return e, nil
}

// digestBuilder condenses Codex and Claude stream records in source order.
type digestBuilder struct {
	entries []DigestEntry
	counts  DigestCounts
	calls   map[string]int // Claude tool_use id -> entry index
	// turnCalls counts tool_use blocks per Claude message id; the stream can
	// split one assistant turn over several records.
	turnCalls map[string]int
}

func (d *digestBuilder) push(e DigestEntry) int {
	switch e.Kind {
	case "message":
		d.counts.Messages++
	case "command":
		d.counts.Commands++
	case "tool":
		d.counts.Tools++
	case "edit":
		d.counts.Edits++
	case "error":
		d.counts.Errors++
	case "reasoning":
		d.counts.Reasoning++
	}
	if e.Failed {
		d.counts.Failed++
	}
	d.entries = append(d.entries, e)
	return len(d.entries) - 1
}

func (d *digestBuilder) fail(i int, tail string) {
	if !d.entries[i].Failed {
		d.counts.Failed++
	}
	d.entries[i].Failed = true
	d.entries[i].Error = tail
}

// add reports whether the record was a supported event (kept or deliberately skipped).
func (d *digestBuilder) add(offset int64, raw []byte) bool {
	if !utf8.Valid(raw) {
		return false
	}
	var event struct {
		Type    string          `json:"type"`
		Subtype string          `json:"subtype"`
		IsError bool            `json:"is_error"`
		Result  string          `json:"result"`
		Message json.RawMessage `json:"message"`
		Error   json.RawMessage `json:"error"`
		Item    *codexItem      `json:"item"`
	}
	if json.Unmarshal(raw, &event) != nil {
		return false
	}
	switch event.Type {
	case "item.completed":
		if event.Item != nil {
			d.codex(offset, *event.Item)
		}
	case "turn.failed", "error":
		d.push(DigestEntry{Offset: offset, Kind: "error", Text: messageText(event.Error, event.Message), Failed: true})
	case "assistant", "user":
		d.claude(offset, event.Type, event.Message)
	case "result":
		if event.IsError || event.Subtype != "" && event.Subtype != "success" {
			d.push(DigestEntry{Offset: offset, Kind: "error", Text: strings.TrimSpace(event.Subtype + ": " + tail(event.Result, errorTailBytes*4)), Failed: true})
		}
	}
	return true
}

type codexItem struct {
	Type    string          `json:"type"`
	Text    string          `json:"text"`
	Message string          `json:"message"`
	Command string          `json:"command"`
	Exit    *int            `json:"exit_code"`
	Status  string          `json:"status"`
	Output  string          `json:"aggregated_output"`
	Server  string          `json:"server"`
	Tool    string          `json:"tool"`
	Args    json.RawMessage `json:"arguments"`
	Error   json.RawMessage `json:"error"`
	Query   string          `json:"query"`
	Summary json.RawMessage `json:"summary"`
	// Receivers names the threads a Codex collab_tool_call addressed.
	Receivers []string `json:"receiver_thread_ids"`
	Changes   []struct {
		Path string `json:"path"`
		Kind string `json:"kind"`
	} `json:"changes"`
}

func (d *digestBuilder) codex(offset int64, it codexItem) {
	switch it.Type {
	case "agent_message":
		d.push(DigestEntry{Offset: offset, Kind: "message", Text: it.Text})
	case "reasoning":
		d.reasoning(offset, append([]string{it.Text}, summaryParts(it.Summary)...)...)
	case "command_execution":
		e := DigestEntry{Offset: offset, Kind: "command", Text: it.Command, Exit: it.Exit}
		if it.Status == "failed" || it.Exit != nil && *it.Exit != 0 {
			e.Failed, e.Error = true, tail(it.Output, errorTailBytes*4)
		}
		d.push(e)
	case "file_change":
		e := DigestEntry{Offset: offset, Kind: "edit", Failed: it.Status == "failed"}
		for _, c := range it.Changes {
			e.Files = append(e.Files, c.Kind+" "+shortPath(c.Path))
		}
		e.Text = fmt.Sprintf("%d file changes", len(e.Files))
		d.push(e)
	case "mcp_tool_call":
		e := DigestEntry{Offset: offset, Kind: "tool", Text: it.Server + "/" + it.Tool + " " + string(it.Args)}
		if it.Status == "failed" || len(it.Error) > 0 && string(it.Error) != "null" {
			e.Failed, e.Error = true, tail(messageText(it.Error, nil), errorTailBytes*4)
		}
		d.push(e)
	case "collab_tool_call":
		e := DigestEntry{Offset: offset, Kind: "tool", Text: "collab/" + it.Tool, Failed: it.Status == "failed"}
		if it.Tool == "spawn_agent" {
			d.counts.Subagents += max(1, len(it.Receivers))
			e.Text = fmt.Sprintf("collab/spawn_agent %d agents", max(1, len(it.Receivers)))
		}
		d.push(e)
	case "web_search":
		d.push(DigestEntry{Offset: offset, Kind: "tool", Text: "web_search: " + it.Query})
	case "error":
		d.push(DigestEntry{Offset: offset, Kind: "error", Text: it.Message, Failed: true})
	}
}

func (d *digestBuilder) claude(offset int64, role string, message json.RawMessage) {
	var m struct {
		ID      string          `json:"id"`
		Content json.RawMessage `json:"content"`
	}
	var blocks []struct {
		Type      string          `json:"type"`
		Text      string          `json:"text"`
		Thinking  string          `json:"thinking"`
		ID        string          `json:"id"`
		Name      string          `json:"name"`
		Input     json.RawMessage `json:"input"`
		ToolUseID string          `json:"tool_use_id"`
		IsError   bool            `json:"is_error"`
		Content   json.RawMessage `json:"content"`
	}
	if json.Unmarshal(message, &m) != nil || json.Unmarshal(m.Content, &blocks) != nil {
		return
	}
	for _, b := range blocks {
		switch {
		case role == "assistant" && b.Type == "text":
			d.push(DigestEntry{Offset: offset, Kind: "message", Text: b.Text})
		case role == "assistant" && b.Type == "thinking":
			d.reasoning(offset, b.Thinking)
		case role == "assistant" && b.Type == "tool_use":
			d.calls[b.ID] = d.push(claudeCall(offset, b.Name, b.Input))
			if b.Name == "Task" || b.Name == "Agent" {
				d.counts.Subagents++
			}
			turn := m.ID
			if turn == "" {
				turn = fmt.Sprintf("@%d", offset)
			}
			if d.turnCalls[turn]++; d.turnCalls[turn] == 2 {
				d.counts.ParallelBatches++
			}
		case role == "user" && b.Type == "tool_result" && b.IsError:
			if i, ok := d.calls[b.ToolUseID]; ok {
				d.fail(i, tail(messageText(b.Content, nil), errorTailBytes*4))
			}
		}
	}
}

// reasoning joins every non-empty thinking part of one item, in order, into
// one entry at the item's offset. A provider that withholds its thinking
// leaves empty parts, which are skipped; an item with none adds no entry.
func (d *digestBuilder) reasoning(offset int64, texts ...string) {
	parts := []string{}
	for _, text := range texts {
		if text = strings.TrimSpace(text); text != "" {
			parts = append(parts, text)
		}
	}
	if len(parts) > 0 {
		d.push(DigestEntry{Offset: offset, Kind: "reasoning", Text: strings.Join(parts, "\n\n")})
	}
}

// summaryParts reads every text of a Codex reasoning summary: an array of
// summary parts, or a plain string or {"message": ...} object.
func summaryParts(raw json.RawMessage) []string {
	var blocks []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) == nil {
		parts := make([]string, 0, len(blocks))
		for _, b := range blocks {
			parts = append(parts, b.Text)
		}
		return parts
	}
	return []string{messageText(raw)}
}

func claudeCall(offset int64, name string, raw json.RawMessage) DigestEntry {
	var input struct {
		Command  string `json:"command"`
		FilePath string `json:"file_path"`
		Path     string `json:"notebook_path"`
	}
	_ = json.Unmarshal(raw, &input)
	switch {
	case name == "Bash":
		return DigestEntry{Offset: offset, Kind: "command", Text: input.Command}
	case input.FilePath != "" && (name == "Edit" || name == "Write" || name == "MultiEdit"), name == "NotebookEdit":
		file := input.FilePath + input.Path
		return DigestEntry{Offset: offset, Kind: "edit", Text: name, Files: []string{shortPath(file)}}
	default:
		return DigestEntry{Offset: offset, Kind: "tool", Text: name + " " + string(raw)}
	}
}

// messageText reads an error string or {"message": ...} object.
func messageText(values ...json.RawMessage) string {
	for _, v := range values {
		var text string
		if json.Unmarshal(v, &text) == nil && text != "" {
			return text
		}
		var object struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(v, &object) == nil && object.Message != "" {
			return object.Message
		}
		var blocks []struct {
			Text string `json:"text"`
		}
		if json.Unmarshal(v, &blocks) == nil {
			parts := []string{}
			for _, b := range blocks {
				parts = append(parts, b.Text)
			}
			if joined := strings.Join(parts, "\n"); joined != "" {
				return joined
			}
		}
	}
	return ""
}

// tail keeps the end of output from a line start, so redaction sees whole lines.
func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	s = s[len(s)-n:]
	if i := strings.IndexByte(s, '\n'); i >= 0 && i < len(s)-1 {
		return strings.TrimSpace(s[i+1:])
	}
	for len(s) > 0 && !utf8.RuneStart(s[0]) {
		s = s[1:]
	}
	return s
}

// shortPath drops the private checkout prefix of an edited file.
func shortPath(p string) string {
	if short := worktreePrefix.ReplaceAllString(p, ""); short != p {
		return short
	}
	if path.IsAbs(p) {
		parts := strings.Split(p, "/")
		if len(parts) > 3 {
			return ".../" + strings.Join(parts[len(parts)-3:], "/")
		}
	}
	return p
}
