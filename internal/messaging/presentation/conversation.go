package presentation

import (
	"errors"
	"strings"
	"unicode/utf8"
)

// ErrProtectedProposalTooLarge preserves the existing explicit proposal refusal.
var ErrProtectedProposalTooLarge = errors.New("protected proposal exceeds message budget")

// AnswerBlock is provider wording with internal current-evidence associations.
type AnswerBlock struct {
	Text     string   `json:"text,omitempty"`
	FactRefs []string `json:"fact_refs,omitempty"`
}

// CurrentFact is an entire fact from this answer's admitted evidence.
type CurrentFact struct {
	ID, Text, Source, Target, Caveats string
	Complete                          bool
}

// ConversationPart is immutable delivery content. Protected marks the proposal.
type ConversationPart struct {
	Content   string
	Protected bool
}

// RenderConversation keeps evidence internal; associations do not verify paraphrases.
func RenderConversation(answerID string, blocks []AnswerBlock, facts []CurrentFact, protected string, intent ...string) ([]ConversationPart, error) {
	if answerID == "" || len(answerID) > 128 || strings.ContainsAny(answerID, "\r\n") {
		return nil, errors.New("invalid answer reference")
	}
	type segment struct {
		text      string
		protected bool
	}
	var segments []segment
	byID := make(map[string]CurrentFact, len(facts))
	for _, fact := range facts {
		if _, exists := byID[fact.ID]; exists {
			return nil, errors.New("duplicate fact identity")
		}
		byID[fact.ID] = fact
	}
	missing := false
	for _, block := range blocks {
		if !utf8.ValidString(block.Text) {
			return nil, errors.New("invalid answer text")
		}
		if block.Text == "" && len(block.FactRefs) == 0 {
			return nil, errors.New("empty answer block")
		}
		text := block.Text
		for _, ref := range block.FactRefs {
			if _, ok := byID[ref]; !ok {
				text = "I couldn't get the latest information."
				break
			}
		}
		if strings.TrimSpace(text) == "" {
			text = "I couldn't get the latest information."
		}
		if text == "I couldn't get the latest information." {
			if missing {
				continue
			}
			missing = true
		}
		segments = append(segments, segment{text: plain(text)})
	}
	if protected != "" {
		segments = append(segments, segment{text: protected, protected: true})
	}
	var parts []ConversationPart
	for _, seg := range segments {
		budget := MessageLimit
		if seg.protected && messageSize(seg.text) > budget {
			return nil, ErrProtectedProposalTooLarge
		}
		remaining := seg.text
		for remaining != "" {
			end := splitEnd(remaining, budget)
			if end == 0 {
				return nil, errors.New("answer cannot fit message budget")
			}
			parts = append(parts, ConversationPart{Content: remaining[:end], Protected: seg.protected})
			remaining = remaining[end:]
		}
	}
	if len(parts) > 1024 {
		return nil, errors.New("answer exceeds saved part budget")
	}
	return parts, nil
}

// ConversationDiagnostics recognizes internal report syntax that needs rewriting.
func ConversationDiagnostics(text string) bool {
	lower := strings.ToLower(text)
	for _, marker := range []string{"source:", "target:", "evidence", "fact-", "page ", "lower bound", "collection", "helper overhead", "still unanswered", "remains unanswered", "the meter output:", "at recorded rates", "total unknown", "total is unknown", "stage:", "mode:", "story-", "recent runs (", "caveat:"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// plain escapes Discord formatting while preserving every source character.
func plain(text string) string {
	return strings.NewReplacer("\\", "\\\\", "*", "\\*", "_", "\\_", "~", "\\~", "`", "\\`", "|", "\\|", ">", "\\>", "#", "\\#", "[", "\\[", "]", "\\]", "@", "\\@").Replace(text)
}

func splitEnd(text string, budget int) int {
	units, end, boundary, sentence := 0, 0, 0, 0
	var previous rune
	for offset, r := range text {
		size := 1
		if r > 0xffff {
			size = 2
		}
		if units+size > budget {
			break
		}
		units += size
		end = offset + utf8.RuneLen(r)
		if r == '\n' || r == ' ' {
			boundary = end
			if r == '\n' || previous == '.' || previous == '!' || previous == '?' {
				sentence = end
			}
		}
		previous = r
	}
	if end < len(text) && boundary > 0 {
		end = boundary
		if sentence > 0 {
			end = sentence
		}
	}
	if end < len(text) {
		slashes := 0
		for i := end - 1; i >= 0 && text[i] == '\\'; i-- {
			slashes++
		}
		if slashes%2 == 1 {
			end--
		}
	}
	return end
}
