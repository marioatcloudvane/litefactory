package factorylib

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// MaxConversationCaptureBytes bounds the encoded role captures together.
const MaxConversationCaptureBytes = 16 << 20

// ConversationRoles is the role inventory of a conversation, in capture order.
// Retained captures made before the reasoner or the inbox organizer existed
// hold only the leading roles; the missing ones read as empty.
func ConversationRoles() []Role {
	return []Role{ConversationNative, ConversationFactory, ConversationCorrection, ConversationActionOutcome, ConversationReasoner, ConversationInboxOrganizer}
}

const baseConversationRoles = 4

// ConversationCapture retains all conversation roles together.
type ConversationCapture struct {
	data  conversationData
	roles []*Capture
}

type conversationData struct {
	Schema int               `json:"schema"`
	Legacy bool              `json:"legacy_baseline"`
	Roles  []json.RawMessage `json:"roles"`
	Digest string            `json:"digest"`
}

// CaptureConversation captures every conversation role from the library.
func (l *Library) CaptureConversation(ctx context.Context) (*ConversationCapture, error) {
	data := conversationData{Schema: 1}
	for _, role := range ConversationRoles() {
		captured, err := l.Capture(ctx, role)
		if err != nil {
			return nil, err
		}
		raw, err := captured.Encode()
		if err != nil {
			return nil, err
		}
		data.Roles = append(data.Roles, raw)
	}
	data.Digest = conversationDigest(data)
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	return RestoreConversation(raw)
}

// RestoreConversation validates retained roles and their aggregate digest
// without reading the library.
func RestoreConversation(raw []byte) (*ConversationCapture, error) {
	if len(raw) > MaxConversationCaptureBytes {
		return nil, errors.New("retained conversation instructions exceed their encoding bound")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var data conversationData
	if err := dec.Decode(&data); err != nil {
		return nil, fmt.Errorf("decode conversation instructions: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("conversation instructions have trailing data")
	}
	if data.Schema != 1 || len(data.Roles) < baseConversationRoles || len(data.Roles) > len(ConversationRoles()) {
		return nil, errors.New("invalid conversation instruction schema or role count")
	}
	if data.Digest != conversationDigest(data) {
		return nil, errors.New("conversation instruction aggregate digest mismatch")
	}
	result := &ConversationCapture{data: data}
	for i, rawRole := range data.Roles {
		role, err := Restore(rawRole)
		if err != nil {
			return nil, fmt.Errorf("restore conversation role %s: %w", ConversationRoles()[i], err)
		}
		if role.Role() != ConversationRoles()[i] {
			return nil, errors.New("conversation instruction role mismatch")
		}
		result.roles = append(result.roles, role)
	}
	return result, nil
}

func conversationDigest(data conversationData) string {
	data.Digest = ""
	raw, _ := json.Marshal(data)
	return hashBytes(raw)
}

// Encode returns a fresh serialization of this capture.
func (c *ConversationCapture) Encode() ([]byte, error) {
	if c == nil {
		return nil, errors.New("missing conversation instruction capture")
	}
	raw, err := json.Marshal(c.data)
	if err != nil {
		return nil, err
	}
	if _, err := RestoreConversation(raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// Digest identifies the complete retained set.
func (c *ConversationCapture) Digest() string { return c.data.Digest }

// Legacy reports a capture backfilled from the pinned pre-library baseline.
func (c *ConversationCapture) Legacy() bool { return c.data.Legacy }

func (c *ConversationCapture) text(i int) string {
	if c == nil || len(c.roles) <= i {
		return ""
	}
	return string(c.roles[i].Instructions())
}

// Native supplies provider-level and plain prompt instructions.
func (c *ConversationCapture) Native() string { return c.text(0) }

// Factory supplies the factory envelope instructions followed by the
// standards-authoring skill; a capture retained before the skill has none.
func (c *ConversationCapture) Factory() string {
	text := c.text(1)
	if skill := c.roles[1].SkillInstructions(StandardsAuthoringSkill); len(skill) > 0 {
		text += "\n\n" + string(skill)
	}
	return text
}

// Correction supplies the strict-validation correction fragment.
func (c *ConversationCapture) Correction() string { return c.text(2) }

// ActionOutcome supplies narration instructions, joined after Factory with one space.
func (c *ConversationCapture) ActionOutcome() string { return c.text(3) }

// Reasoner supplies the reasoner instructions, or "" for an older capture.
func (c *ConversationCapture) Reasoner() string { return c.text(4) }

// InboxOrganizer supplies the organizer instructions with its schema, or ""
// for an older capture so callers keep the factual inbox fallback.
func (c *ConversationCapture) InboxOrganizer() string {
	if c == nil || len(c.roles) < 6 {
		return ""
	}
	return string(c.roles[5].Instructions()) + "\n\n" + string(c.roles[5].Resource(InboxOrganizerSchema))
}
