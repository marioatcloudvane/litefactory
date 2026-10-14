package turn

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/cloudvane/litefactory/internal/config"
)

// JudgeCommandOptions carries bounded owner intent, never shell instructions.
type JudgeCommandOptions struct {
	Target string `json:"target,omitempty"`
	Focus  string `json:"focus,omitempty"`
	config.JudgeOptions
}

// ParseJudgeOptions accepts quoted values without expansion or execution.
func ParseJudgeOptions(text string) (JudgeCommandOptions, error) {
	var out JudgeCommandOptions
	if len(text) > 16384 {
		return out, errors.New("judge settings exceed 16384 bytes")
	}
	var words []string
	var word strings.Builder
	var quote rune
	active := false
	for _, r := range text {
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				word.WriteRune(r)
			}
			active = true
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			active = true
			continue
		}
		if unicode.IsSpace(r) {
			if active {
				words = append(words, word.String())
				word.Reset()
				active = false
			}
			continue
		}
		word.WriteRune(r)
		active = true
	}
	if quote != 0 {
		return out, errors.New("judge settings contain an unclosed quote")
	}
	if active {
		words = append(words, word.String())
	}
	seen := map[string]bool{}
	for i := 0; i < len(words); i++ {
		name, value, equal := strings.Cut(words[i], "=")
		switch name {
		case "--target", "--agent", "--model", "--thinking", "--focus":
		default:
			return out, fmt.Errorf("unknown judge setting %q", name)
		}
		if seen[name] {
			return out, fmt.Errorf("duplicate judge setting %s", name)
		}
		seen[name] = true
		if !equal {
			i++
			if i >= len(words) || strings.HasPrefix(words[i], "--") {
				return out, fmt.Errorf("judge setting %s requires a value", name)
			}
			value = words[i]
		}
		if strings.TrimSpace(value) == "" {
			return out, fmt.Errorf("judge setting %s requires a value", name)
		}
		switch name {
		case "--target":
			out.Target = value
		case "--agent":
			out.Agent = value
		case "--model":
			out.Model = value
		case "--thinking":
			out.Thinking = config.Thinking(value)
		case "--focus":
			out.Focus = value
		}
	}
	if out.Agent != "" && out.Agent != "claude" && out.Agent != "codex" && out.Agent != "other" {
		return out, errors.New("judge agent must be claude, codex or other")
	}
	return out, nil
}
