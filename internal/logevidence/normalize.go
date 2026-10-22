package logevidence

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/submission"
)

var (
	// ErrMalformedRecord identifies invalid or ambiguous JSON source records.
	ErrMalformedRecord = errors.New("malformed log record")
	// ErrUnsupportedRecord identifies formats that cannot safely become evidence.
	ErrUnsupportedRecord = errors.New("unsupported log record")
)

// NormalizeRecord decodes a complete supported JSONL record before redaction.
// Every field is included as decoded text, including tool input and metadata,
// so JSON string escaping cannot hide a registered credential. It never returns
// a raw fallback. The caller must redact the complete text before excerpting it.
func NormalizeRecord(raw []byte) (text string, kind string, err error) {
	if !utf8.Valid(raw) {
		return "", "", ErrMalformedRecord
	}
	var event map[string]any
	if submission.DecodeJSON(raw, &event) != nil || event == nil {
		return "", "", ErrMalformedRecord
	}
	kind, _ = event["type"].(string)
	switch kind {
	case "system", "result", "thread.started", "turn.started", "turn.completed", "turn.failed", "error":
	case "assistant", "user":
		if message, exists := event["message"]; exists {
			m, ok := message.(map[string]any)
			if !ok {
				return "", kind, ErrMalformedRecord
			}
			if !supportedContent(m["content"]) {
				return "", kind, ErrUnsupportedRecord
			}
		} else if _, ok := event["text"].(string); !ok {
			return "", kind, ErrMalformedRecord
		}
	case "item.started", "item.updated", "item.completed":
		item, ok := event["item"].(map[string]any)
		if !ok {
			return "", kind, ErrMalformedRecord
		}
		switch item["type"] {
		case "agent_message", "command_execution", "reasoning", "todo_list", "mcp_tool_call", "web_search", "file_change", "collab_tool_call", "error":
		default:
			return "", kind, ErrUnsupportedRecord
		}
	default:
		return "", kind, ErrUnsupportedRecord
	}
	var out strings.Builder
	decodedText(&out, event)
	return out.String(), kind, nil
}

func supportedContent(value any) bool {
	if _, ok := value.(string); ok {
		return true
	}
	blocks, ok := value.([]any)
	if !ok {
		return false
	}
	for _, value := range blocks {
		block, ok := value.(map[string]any)
		if !ok {
			return false
		}
		switch block["type"] {
		case "text", "thinking", "redacted_thinking", "tool_use":
		case "tool_result":
			if !supportedContent(block["content"]) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func decodedText(out *strings.Builder, value any) {
	switch value := value.(type) {
	case string:
		out.WriteString(value)
	case map[string]any:
		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		out.WriteByte('{')
		for i, key := range keys {
			if i != 0 {
				out.WriteByte('\n')
			}
			out.WriteString(key)
			out.WriteString(": ")
			decodedText(out, value[key])
		}
		out.WriteByte('}')
	case []any:
		out.WriteByte('[')
		for i, item := range value {
			if i != 0 {
				out.WriteByte('\n')
			}
			decodedText(out, item)
		}
		out.WriteByte(']')
	default:
		data, _ := json.Marshal(value)
		out.Write(data)
	}
}
