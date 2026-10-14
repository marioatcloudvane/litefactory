package presentation

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// ErrProtectedProposalTooLarge preserves the existing explicit proposal refusal.
var ErrProtectedProposalTooLarge = errors.New("protected proposal exceeds message budget")

// AnswerBlock is provider wording or a request to expand current factory facts.
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

// RenderConversation keeps unsupported wording readable and labels every
// continuation. Only exact complete current facts can remove the warning.
func RenderConversation(answerID string, blocks []AnswerBlock, facts []CurrentFact, protected string) ([]ConversationPart, error) {
	if answerID == "" || len(answerID) > 128 || strings.ContainsAny(answerID, "\r\n") {
		return nil, errors.New("invalid answer reference")
	}
	type segment struct {
		text, label string
		protected   bool
	}
	var segments []segment
	byID := make(map[string]CurrentFact, len(facts))
	for _, fact := range facts {
		if _, exists := byID[fact.ID]; exists {
			return nil, errors.New("duplicate fact identity")
		}
		byID[fact.ID] = fact
	}
	for _, block := range blocks {
		if !utf8.ValidString(block.Text) {
			return nil, errors.New("invalid answer text")
		}
		if block.Text == "" && len(block.FactRefs) == 0 {
			return nil, errors.New("empty answer block")
		}
		supported := false
		for _, ref := range block.FactRefs {
			fact, ok := byID[ref]
			if !ok || !fact.Complete || fact.Text == "" {
				if block.Text == "" {
					segments = append(segments, segment{text: plain("Requested current fact unavailable: " + ref), label: "Unverified — this wording is not established by current tool evidence."})
					supported = true
				}
				continue
			}
			if block.Text == "" || block.Text == fact.Text {
				label := "Source: " + plain(fact.Source) + "; target: " + plain(fact.Target)
				if fact.Caveats != "" {
					label += "; caveats: " + plain(fact.Caveats)
				}
				segments = append(segments, segment{text: plain(fact.Text), label: label})
				supported = true
				if block.Text != "" {
					break
				}
			}
		}
		if !supported {
			text := block.Text
			if text == "" {
				text = "Requested current fact unavailable: " + strings.Join(block.FactRefs, ", ")
			}
			segments = append(segments, segment{text: plain(text), label: "Unverified — this wording is not established by current tool evidence."})
		}
	}
	if protected != "" {
		segments = append(segments, segment{text: protected, protected: true})
	}
	// Reserve enough digits for any part count under the accepted text budget.
	reserve := fmt.Sprintf("Answer %s · part 999999 of 999999\n/details answer:%s\n\n", answerID, answerID)
	var parts []ConversationPart
	for _, seg := range segments {
		prefix := ""
		if seg.label != "" {
			prefix = seg.label + "\n"
		}
		budget := MessageLimit - messageSize(reserve) - messageSize(prefix)
		if budget < 2 {
			return nil, errors.New("answer metadata exceeds message budget")
		}
		if seg.protected && messageSize(seg.text) > budget {
			return nil, ErrProtectedProposalTooLarge
		}
		remaining := seg.text
		for remaining != "" {
			end := splitEnd(remaining, budget)
			if end == 0 {
				return nil, errors.New("answer cannot fit message budget")
			}
			parts = append(parts, ConversationPart{Content: prefix + remaining[:end], Protected: seg.protected})
			remaining = remaining[end:]
		}
	}
	if len(parts) > 1024 {
		return nil, errors.New("answer exceeds saved part budget")
	}
	for i := range parts {
		parts[i].Content = fmt.Sprintf("Answer %s · part %d of %d\n/details answer:%s\n\n", answerID, i+1, len(parts), answerID) + parts[i].Content
	}
	return parts, nil
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
