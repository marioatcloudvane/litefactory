package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Turning a run's stream-json into something a person reads.
//
// A run log is a JSONL file of claude's events. Printing it verbatim is what
// --raw is for; it is unreadable at any length. What a person following a run
// actually wants is the handful of things the agent is *doing*: what it said,
// which tool it called with which argument, what came back, and how it ended.
//
// Every rendered line is one output line prefixed with its kind, so the output
// stays line-oriented and greppable — by --grep here, or by grep after a pipe.
// That is the whole reason a multi-line assistant message repeats its label on
// each line rather than indenting continuations: a matched line that lost its
// label would be a line you cannot place.
const (
	// kindSystem is a system event: the init line, and any other session-level
	// notice claude emits.
	kindSystem = "system"
	// kindText is an assistant message's text — what claude says.
	kindText = "text"
	// kindThink is an assistant message's thinking block. Off by default: it is
	// the bulkiest kind and the least often what you are looking for.
	kindThink = "think"
	// kindTool is a tool call, rendered as the tool's name and its primary
	// argument.
	kindTool = "tool"
	// kindOutput is what a tool call returned.
	kindOutput = "output"
	// kindResult is the terminal result event and its final text.
	kindResult = "result"
	// kindUser is a user-role message that is not a tool result — the prompt,
	// and anything injected into the conversation.
	kindUser = "user"
	// kindOther is every line that does not fit the above, including lines that
	// are not JSON at all. Nothing in a run log is ever silently dropped.
	kindOther = "other"
)

// Event type and subtype values from the stream-json protocol. The terminal
// event's type lives in cmd_logs.go as terminalEventType, because following a
// run needs it whether or not anything is being rendered.
const (
	eventTypeSystem    = "system"
	eventTypeAssistant = "assistant"
	eventTypeUser      = "user"
	subtypeInit        = "init"
)

// kindLabelWidth is the column the rendered text starts in. It is the width of
// the longest kind name, so no label is ever truncated and every line's text
// starts at the same column.
const kindLabelWidth = 6

// Caps on the parts of a run that are big enough to bury the parts that are
// not. Both are lifted by --full.
const (
	// toolInputCap is how much of a tool's argument is shown. Long enough for a
	// path or an ordinary shell command, short enough that a heredoc does not
	// take the screen.
	toolInputCap = 160
	// toolOutputCap is how much of a tool's output is previewed on its summary
	// line. A tool result runs to tens of kilobytes; the summary says how big it
	// was and shows the start.
	toolOutputCap = 120
)

// allKinds is every kind --only accepts, in the order they are listed back in
// an error message.
var allKinds = []string{kindSystem, kindText, kindThink, kindTool, kindOutput, kindResult, kindUser, kindOther}

// defaultKinds is what is shown when --only is omitted: everything except
// thinking.
var defaultKinds = []string{kindSystem, kindText, kindTool, kindOutput, kindResult, kindUser, kindOther}

// agentKinds is the "--only agent" alias: what the agent said, thought and
// did, without the tool output it read. This is the "show me what the agents
// are doing" view.
var agentKinds = []string{kindText, kindThink, kindTool}

// kindSet is the set of kinds to print.
type kindSet map[string]bool

// newKindSet builds a set from a list of kind names.
func newKindSet(names []string) kindSet {
	set := make(kindSet, len(names))
	for _, name := range names {
		set[name] = true
	}
	return set
}

// parseKinds parses the --only value: a comma-separated list of kind names,
// plus the aliases "agent" and "all".
//
// An unknown name is a usage error rather than an empty view. Silently
// accepting "--only tools" and then printing nothing is the kind of thing that
// costs an hour.
func parseKinds(spec string) (kindSet, error) {
	set := kindSet{}
	for _, field := range strings.Split(spec, ",") {
		name := strings.ToLower(strings.TrimSpace(field))
		switch {
		case name == "":
			continue
		case name == "all":
			for _, kind := range allKinds {
				set[kind] = true
			}
		case name == "agent":
			for _, kind := range agentKinds {
				set[kind] = true
			}
		case newKindSet(allKinds)[name]:
			set[name] = true
		default:
			return nil, fmt.Errorf("unknown kind %q: --only takes %s, or the aliases agent and all",
				name, strings.Join(allKinds, ", "))
		}
	}
	if len(set) == 0 {
		return nil, fmt.Errorf("--only needs at least one kind: %s, or the aliases agent and all",
			strings.Join(allKinds, ", "))
	}
	return set, nil
}

// lineFilter is --grep: a regexp applied to each output line, optionally
// inverted.
//
// It is deliberately applied to the *rendered* line rather than to the JSON
// behind it. Grepping the JSON means writing patterns against escaped strings
// and matching a 70 KB tool-result line because the word appeared somewhere
// inside it; grepping what is printed means what you see is what you matched.
type lineFilter struct {
	re     *regexp.Regexp
	invert bool
}

// newLineFilter compiles the --grep pattern. Case folding is opt-in, as with
// grep itself.
func newLineFilter(pattern string, ignoreCase, invert bool) (*lineFilter, error) {
	if pattern == "" {
		return nil, nil
	}
	if ignoreCase {
		pattern = "(?i)" + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("--grep %w", err)
	}
	return &lineFilter{re: re, invert: invert}, nil
}

// match reports whether a line survives the filter. A nil filter passes
// everything, so callers never have to check.
func (f *lineFilter) match(line []byte) bool {
	if f == nil {
		return true
	}
	return f.re.Match(bytes.TrimRight(line, "\r\n")) != f.invert
}

// logSink consumes a run's event stream one complete line at a time. It is
// what lets the file reader and the follower share a single path through the
// four ways `logs` can present a run.
//
// done reports that the terminal event has been seen: the follower stops on
// it, and the file reader ignores it and keeps reading to the end.
type logSink interface {
	write(line []byte) (done bool, err error)
}

// rawSink writes the run's JSONL through, optionally filtered by --grep.
//
// With no filter it writes each line's bytes exactly as they came off the
// disk, which is what makes --raw byte-for-byte (AC-5.5).
type rawSink struct {
	out  io.Writer
	grep *lineFilter
}

func (s *rawSink) write(line []byte) (bool, error) {
	if s.grep.match(line) {
		if _, err := s.out.Write(line); err != nil {
			return false, fmt.Errorf("write run log: %w", err)
		}
	}
	return isTerminalEvent(line), nil
}

// renderSink turns each event into labelled lines.
type renderSink struct {
	out  io.Writer
	grep *lineFilter
	// kinds is which kinds to print. Filtering happens here, not at the event
	// level, so an event that renders several kinds — a message carrying both
	// text and a tool call — contributes only the lines that were asked for.
	kinds kindSet
	// full lifts the caps on tool arguments and tool output.
	full bool

	// tools maps a tool_use id to the tool's name, so a tool result can be
	// labelled with the tool it came from. claude does not repeat the name on
	// the result, and "output  24 lines" without it is nearly useless.
	tools map[string]string
	// marker prefixes every line of the event being rendered. It carries the
	// sub-agent tag, which is the only way to tell a Task sub-agent's activity
	// from the main agent's in an interleaved stream.
	marker string
	// sawResult records whether the run's terminal event was rendered, so the
	// caller can say so when it was not.
	sawResult bool
}

// newRenderSink builds a renderer writing to out.
func newRenderSink(out io.Writer, kinds kindSet, grep *lineFilter, full bool) *renderSink {
	return &renderSink{out: out, grep: grep, kinds: kinds, full: full, tools: map[string]string{}}
}

// logEvent is one line of claude's stream-json, decoded down to the fields
// that are rendered.
//
// Every field is optional. This is a wire format from another program, read
// from a file that may have been written by an older version of it, so a
// missing field is normal and never an error: the renderer falls back through
// what is present and, in the last resort, prints the line as "other".
type logEvent struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	// ParentToolUseID is set on every event produced inside a Task sub-agent.
	ParentToolUseID string `json:"parent_tool_use_id"`
	SessionID       string `json:"session_id"`

	// System init fields.
	Model string   `json:"model"`
	Tools []string `json:"tools"`
	CWD   string   `json:"cwd"`

	// Message carries assistant and user events.
	Message *logMessage `json:"message"`
	// Text is a flat fallback for events that carry their text at the top
	// level rather than in a message.
	Text string `json:"text"`

	// Result event fields. Note the absence of Subtype's opinion: it reads
	// "success" even on a failed run, so is_error is the only trustworthy
	// signal (SD-3).
	IsError      bool    `json:"is_error"`
	Result       string  `json:"result"`
	Error        string  `json:"error"`
	TotalCostUSD float64 `json:"total_cost_usd"`
	DurationMS   int64   `json:"duration_ms"`
	NumTurns     int     `json:"num_turns"`
}

// logMessage is the message an assistant or user event wraps.
type logMessage struct {
	Role string `json:"role"`
	// Content is either an array of blocks or a bare string, so it is kept raw
	// and decoded by [contentBlocks].
	Content json.RawMessage `json:"content"`
}

// logBlock is one content block of a message.
type logBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text"`
	Thinking string `json:"thinking"`

	// tool_use.
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`

	// tool_result.
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

// write renders one line of the run log.
func (s *renderSink) write(raw []byte) (bool, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return false, nil
	}

	var event logEvent
	if err := json.Unmarshal(trimmed, &event); err != nil {
		// Not JSON. It is still something the run wrote, so it is shown rather
		// than dropped — a stray diagnostic on claude's stdout is exactly the
		// sort of thing you are looking at the log to find.
		return false, s.emit(kindOther, s.clip(flattenSpace(string(trimmed)), toolInputCap))
	}

	done := event.Type == terminalEventType
	if done {
		s.sawResult = true
	}
	return done, s.render(event, trimmed)
}

// render dispatches one decoded event.
func (s *renderSink) render(event logEvent, raw []byte) error {
	s.marker = ""
	if event.ParentToolUseID != "" {
		s.marker = "[agent " + shortID(event.ParentToolUseID, agentIDWidth) + "] "
	}

	switch event.Type {
	case eventTypeSystem:
		return s.renderSystem(event)
	case eventTypeAssistant:
		return s.renderMessage(event, kindText, raw)
	case eventTypeUser:
		return s.renderMessage(event, kindUser, raw)
	case terminalEventType:
		return s.renderResult(event)
	default:
		return s.emit(kindOther, s.clip(flattenSpace(string(raw)), toolInputCap))
	}
}

// renderSystem prints the init line in full and every other system event by
// whatever names it.
func (s *renderSink) renderSystem(event logEvent) error {
	if event.Subtype == subtypeInit {
		parts := []string{subtypeInit}
		if event.Model != "" {
			parts = append(parts, "model="+event.Model)
		}
		if event.SessionID != "" {
			parts = append(parts, "session="+shortID(event.SessionID, sessionIDWidth))
		}
		if event.CWD != "" {
			parts = append(parts, "cwd="+event.CWD)
		}
		if len(event.Tools) > 0 {
			parts = append(parts, fmt.Sprintf("tools=%d", len(event.Tools)))
		}
		return s.emit(kindSystem, strings.Join(parts, "  "))
	}
	return s.emit(kindSystem, flattenSpace(firstNonEmpty(event.Subtype, event.Text, "system event")))
}

// renderMessage prints an assistant or user message block by block.
func (s *renderSink) renderMessage(event logEvent, textKind string, raw []byte) error {
	if event.Message == nil {
		if event.Text != "" {
			return s.emit(textKind, event.Text)
		}
		return s.emit(kindOther, s.clip(flattenSpace(string(raw)), toolInputCap))
	}

	blocks, plain := contentBlocks(event.Message.Content)
	if plain != "" {
		return s.emit(textKind, plain)
	}
	for _, block := range blocks {
		if err := s.renderBlock(block, textKind); err != nil {
			return err
		}
	}
	return nil
}

// renderBlock prints one content block.
func (s *renderSink) renderBlock(block logBlock, textKind string) error {
	switch block.Type {
	case "text":
		return s.emit(textKind, block.Text)
	case "thinking", "redacted_thinking":
		return s.emit(kindThink, firstNonEmpty(block.Thinking, block.Text, "(redacted)"))
	case "tool_use":
		// Remember the name even when tool lines are filtered out: the result
		// line may still be printed, and it is named from this map.
		if block.ID != "" {
			s.tools[block.ID] = block.Name
		}
		return s.emit(kindTool, s.toolLine(block))
	case "tool_result":
		return s.emit(kindOutput, s.outputLine(block))
	default:
		return s.emit(kindOther, block.Type)
	}
}

// renderResult prints how the run ended, then its final text.
func (s *renderSink) renderResult(event logEvent) error {
	status := "ok"
	if event.IsError {
		status = "error"
	}
	parts := []string{status}
	if event.DurationMS > 0 {
		parts = append(parts, (time.Duration(event.DurationMS) * time.Millisecond).Round(time.Second).String())
	}
	if event.TotalCostUSD > 0 {
		parts = append(parts, fmt.Sprintf("$%.4f", event.TotalCostUSD))
	}
	if event.NumTurns > 0 {
		parts = append(parts, fmt.Sprintf("%d turns", event.NumTurns))
	}
	if err := s.emit(kindResult, strings.Join(parts, "  ")); err != nil {
		return err
	}
	if text := firstNonEmpty(event.Result, event.Error); text != "" {
		return s.emit(kindResult, text)
	}
	return nil
}

// toolLine renders a tool call as its name and its primary argument.
func (s *renderSink) toolLine(block logBlock) string {
	name := firstNonEmpty(block.Name, "tool")
	summary := s.toolInput(block.Input)
	if summary == "" {
		return name
	}
	return name + "  " + summary
}

// toolInputKeys are the input fields that identify a call, most specific
// first. One table covers every tool because the interesting argument is
// almost always the thing being acted on: the command, the file, the pattern.
//
// description precedes prompt so a Task call reports what the sub-agent was
// asked to do rather than the first 160 bytes of its briefing.
var toolInputKeys = []string{
	"command", "file_path", "notebook_path", "path", "pattern",
	"url", "query", "description", "prompt",
}

// toolInput summarises a tool call's input on one line.
func (s *renderSink) toolInput(input json.RawMessage) string {
	if len(bytes.TrimSpace(input)) == 0 {
		return ""
	}
	var fields map[string]any
	if err := json.Unmarshal(input, &fields); err != nil {
		return s.clip(flattenSpace(string(input)), toolInputCap)
	}
	for _, key := range toolInputKeys {
		if value, ok := fields[key]; ok {
			if text := flattenSpace(valueString(value)); text != "" {
				return s.clip(text, toolInputCap)
			}
		}
	}
	// No recognised argument: show the input, with keys in a stable order so
	// two runs of the same call render the same way.
	return s.clip(flattenSpace(compactFields(fields)), toolInputCap)
}

// outputLine renders what a tool returned: which tool, how much came back, and
// the start of it. With --full the whole output follows.
func (s *renderSink) outputLine(block logBlock) string {
	name := firstNonEmpty(s.tools[block.ToolUseID], "tool")
	blocks, plain := contentBlocks(block.Content)
	text := plain
	if text == "" {
		text = blockText(blocks)
	}

	head := name
	if block.IsError {
		head += "  error"
	}
	head += "  " + sizeSummary(text)
	if text == "" {
		return head
	}
	if s.full {
		return head + "\n" + text
	}
	return head + "  " + s.clip(flattenSpace(firstLine(text)), toolOutputCap)
}

// emit writes one rendered value, which may span several lines. Each line is
// labelled with its kind and filtered on its own, so --grep behaves the way
// grep does.
func (s *renderSink) emit(kind, text string) error {
	if !s.kinds[kind] || text == "" {
		return nil
	}
	for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		rendered := kindLabel(kind) + s.marker + strings.TrimRight(line, "\r")
		if !s.grep.match([]byte(rendered)) {
			continue
		}
		if _, err := io.WriteString(s.out, rendered+"\n"); err != nil {
			return fmt.Errorf("write run log: %w", err)
		}
	}
	return nil
}

// clip shortens s to n runes unless --full was given, marking the cut so a
// truncated value is never mistaken for a short one.
func (s *renderSink) clip(text string, n int) string {
	if s.full {
		return text
	}
	return clip(text, n)
}

// kindLabel is the label column for a kind.
func kindLabel(kind string) string {
	if len(kind) < kindLabelWidth {
		kind += strings.Repeat(" ", kindLabelWidth-len(kind))
	}
	return kind + "  "
}

// contentBlocks decodes a message's content, which the protocol allows to be
// either an array of blocks or a bare string. The second result is the string
// form; the first is the array form. Both are empty for content that is
// neither.
func contentBlocks(raw json.RawMessage) ([]logBlock, string) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, ""
	}
	if trimmed[0] == '"' {
		var text string
		if err := json.Unmarshal(trimmed, &text); err != nil {
			return nil, ""
		}
		return nil, text
	}
	var blocks []logBlock
	if err := json.Unmarshal(trimmed, &blocks); err != nil {
		return nil, ""
	}
	return blocks, ""
}

// blockText joins the text of a decoded block array.
func blockText(blocks []logBlock) string {
	var parts []string
	for _, block := range blocks {
		if text := firstNonEmpty(block.Text, block.Thinking); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

// valueString renders a decoded JSON value for a one-line summary. Numbers go
// through the JSON encoder rather than %v so a large integer does not come out
// in scientific notation.
func valueString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case nil:
		return ""
	default:
		encoded, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(encoded)
	}
}

// compactFields renders an unrecognised tool input with its keys sorted, so
// the same call always renders the same way.
func compactFields(fields map[string]any) string {
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var b strings.Builder
	for i, key := range keys {
		if i > 0 {
			b.WriteString(" ")
		}
		fmt.Fprintf(&b, "%s=%s", key, valueString(fields[key]))
	}
	return b.String()
}

// sizeSummary reports how much a tool returned.
func sizeSummary(text string) string {
	if text == "" {
		return "no output"
	}
	lines := strings.Count(strings.TrimRight(text, "\n"), "\n") + 1
	unit := "lines"
	if lines == 1 {
		unit = "line"
	}
	return fmt.Sprintf("%d %s, %s", lines, unit, byteSize(len(text)))
}

// byteSize renders a byte count without pulling in a humanising dependency.
func byteSize(n int) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.1f kB", float64(n)/1024)
}

// firstLine is text up to its first newline.
func firstLine(text string) string {
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		return text[:i]
	}
	return text
}

// flattenSpace collapses all whitespace to single spaces so a value that spans
// lines still occupies one.
func flattenSpace(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// clip shortens text to n runes, marking the cut.
func clip(text string, n int) string {
	if n <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= n {
		return text
	}
	return string(runes[:n]) + "…"
}

// shortID abbreviates an identifier to its last width characters, which is
// enough to tell two of them apart on a line.
//
// The tail rather than the head: these identifiers share a fixed prefix —
// "toolu_", "sess_" — so the first characters are the ones that carry no
// information.
func shortID(id string, width int) string {
	runes := []rune(id)
	if len(runes) <= width {
		return id
	}
	return string(runes[len(runes)-width:])
}

// Widths for the two identifiers that appear in rendered output. A session id
// is printed once per run and is worth more characters; a sub-agent tag is
// printed on every line the sub-agent produces and only has to distinguish the
// handful of agents alive at one time.
const (
	sessionIDWidth = 12
	agentIDWidth   = 8
)
