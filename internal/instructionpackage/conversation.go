package instructionpackage

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"slices"
)

// MaxConversationCaptureBytes bounds the encoded role captures together.
const MaxConversationCaptureBytes = 16 << 20

// ConversationReasonerRole is the optional fifth conversation role. Captures
// made before it existed, the legacy baseline and packages whose catalogue
// does not list it hold only the four base roles; their Reasoner is empty.
const ConversationReasonerRole = "conversation-reasoner"

// ConversationInboxOrganizerRole is an optional snapshot-only presentation role.
const ConversationInboxOrganizerRole = "conversation-inbox-organizer"

// InboxOrganizerSchemaPath is the captured snapshot organizer result contract.
const InboxOrganizerSchemaPath = "resources/inbox-organizer/schema.md"

//go:embed legacy-conversation-v1/*
var legacyConversationFiles embed.FS

// ConversationRoles is the complete instruction inventory for a conversation,
// in capture order. The trailing reasoner and organizer roles are optional;
// older retained captures continue to restore without them.
func ConversationRoles() []string {
	return []string{"conversation-native", "conversation-factory", "conversation-correction", "conversation-action-outcome", ConversationReasonerRole, ConversationInboxOrganizerRole}
}

// baseConversationRoles is the number of roles every capture holds.
const baseConversationRoles = 4

// ConversationCapture retains all roles together, independent of live files.
type ConversationCapture struct {
	data  conversationData
	roles []*RoleCapture
}
type conversationData struct {
	Schema int               `json:"schema"`
	Legacy bool              `json:"legacy_baseline"`
	Roles  []json.RawMessage `json:"roles"`
	Digest string            `json:"digest"`
}

// CaptureConversation captures a complete new conversation before binding creation.
func (p *Package) CaptureConversation(ctx context.Context) (*ConversationCapture, []Diagnostic, error) {
	return p.captureConversation(ctx, false)
}

func (p *Package) captureConversation(ctx context.Context, legacy bool) (*ConversationCapture, []Diagnostic, error) {
	data := conversationData{Schema: 1, Legacy: legacy}
	var diagnostics []Diagnostic
	for i, role := range ConversationRoles() {
		if i >= baseConversationRoles && !slices.Contains(p.roles, role) {
			break
		}
		captured, notes, err := p.Capture(ctx, role)
		if err != nil {
			return nil, nil, err
		}
		raw, err := captured.Encode()
		if err != nil {
			return nil, nil, err
		}
		data.Roles = append(data.Roles, raw)
		diagnostics = append(diagnostics, notes...)
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	data.Digest = conversationDigest(data)
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, nil, err
	}
	captured, err := RestoreConversation(raw)
	return captured, diagnostics, err
}

// LegacyConversation uses an explicitly pinned baseline without owner overrides.
// It exists for migration only; new bindings must use CaptureConversation.
func LegacyConversation(ctx context.Context) (*ConversationCapture, error) {
	baseline, err := fs.Sub(legacyConversationFiles, "legacy-conversation-v1")
	if err != nil {
		return nil, err
	}
	pkg, err := Load(ctx, baseline, "")
	if err != nil {
		return nil, err
	}
	captured, _, err := pkg.captureConversation(ctx, true)
	return captured, err
}

// RestoreConversation validates retained roles and aggregate integrity without source reads.
func RestoreConversation(raw []byte) (*ConversationCapture, error) {
	if len(raw) > MaxConversationCaptureBytes {
		return nil, fmt.Errorf("retained conversation instructions exceed encoding bound")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var data conversationData
	if err := dec.Decode(&data); err != nil {
		return nil, fmt.Errorf("decode conversation instructions: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("conversation instructions have trailing data")
	}
	if data.Schema != 1 || len(data.Roles) < baseConversationRoles || len(data.Roles) > len(ConversationRoles()) {
		return nil, fmt.Errorf("invalid conversation instruction schema or role count")
	}
	if !validDigest(data.Digest) || data.Digest != conversationDigest(data) {
		return nil, fmt.Errorf("conversation instruction aggregate digest mismatch")
	}
	result := &ConversationCapture{data: data}
	for i, rawRole := range data.Roles {
		role, err := Restore(rawRole)
		if err != nil {
			return nil, fmt.Errorf("restore conversation role %s: %w", ConversationRoles()[i], err)
		}
		if role.data.Role != ConversationRoles()[i] {
			return nil, fmt.Errorf("conversation instruction role mismatch")
		}
		result.roles = append(result.roles, role)
	}
	return result, nil
}

func conversationDigest(data conversationData) string {
	data.Digest = ""
	// RawMessage whitespace is normalized so marshal/unmarshal is stable.
	raw, _ := json.Marshal(data)
	return hashBytes(raw)
}

// Encode returns a fresh serialization of this immutable complete capture.
func (c *ConversationCapture) Encode() ([]byte, error) {
	if c == nil {
		return nil, fmt.Errorf("missing conversation instruction capture")
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

// Digest identifies the complete retained set, including legacy provenance.
func (c *ConversationCapture) Digest() string { return c.data.Digest }

// Legacy reports explicitly pinned upgrade baseline provenance.
func (c *ConversationCapture) Legacy() bool { return c.data.Legacy }

// Native supplies both provider-level instructions and plain prompt instructions.
func (c *ConversationCapture) Native() string { return string(c.roles[0].Instructions()) }

// Factory supplies the factory envelope instructions.
func (c *ConversationCapture) Factory() string { return string(c.roles[1].Instructions()) }

// Correction supplies the strict-validation correction fragment.
func (c *ConversationCapture) Correction() string { return string(c.roles[2].Instructions()) }

// ActionOutcome supplies narration instructions, joined after Factory with one space.
func (c *ConversationCapture) ActionOutcome() string { return string(c.roles[3].Instructions()) }

// Reasoner supplies the reasoner instructions, or "" when this capture has no
// reasoner role.
func (c *ConversationCapture) Reasoner() string {
	if len(c.roles) <= baseConversationRoles {
		return ""
	}
	return string(c.roles[baseConversationRoles].Instructions())
}

// ShippedConversationPackage loads validated shipped instructions without an owner overlay.
func ShippedConversationPackage(ctx context.Context) (*Package, error) {
	baseline, err := fs.Sub(embedded, "embedded")
	if err != nil {
		return nil, err
	}
	return Load(ctx, baseline, "")
}

// InboxOrganizer supplies the dedicated organizer instructions. Older captures
// deliberately return empty so callers retain the factual inbox fallback.
func (c *ConversationCapture) InboxOrganizer() string {
	if c == nil || len(c.roles) < 6 {
		return ""
	}
	return string(c.roles[5].Instructions()) + "\n\n" + string(c.roles[5].Resource(InboxOrganizerSchemaPath))
}
