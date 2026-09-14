package turn

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

// MCPResultService is the existing JSON service boundary, with factory scope
// bound to the lease. Implementations must honor the MCP context restriction.
type MCPResultService interface {
	Binding() store.ResultBinding
	MCPPermissionDigest() string
	Preflight(context.Context, []byte) (agentresult.UpdateDiagnostics, error)
	Submit(context.Context, string, []byte) (AgentResultSubmission, error)
	Receipt(context.Context, int64, string) (AgentResultSubmission, error)
}

// MCPPermissionDigest binds this catalog, mode and the existing service's exact
// immutable read/edit grants. The result binding separately binds frozen scope.
func (s *AgentResultService) MCPPermissionDigest() string {
	data, err := json.Marshal(struct {
		Catalog    string
		Binding    store.ResultBinding
		Read, Edit []agentresult.Target
	}{"communication-v1-no-creation", s.binding, s.readTargets, s.editTargets})
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// ToolDispatcher owns server-only authorization and calls each shared method
// once. Neither discovery nor recording a result changes host permissions.
type ToolDispatcher struct {
	guard   *AttemptGuard
	service MCPResultService
	profile submission.ToolProfile
}

// NewToolDispatcher requires a matching live binding and complete service permission digest.
func NewToolDispatcher(guard *AttemptGuard, service MCPResultService) (*ToolDispatcher, error) {
	if guard == nil || service == nil || guard.Binding() != service.Binding() || service.MCPPermissionDigest() == "" || guard.Lease().PermissionDigest != service.MCPPermissionDigest() {
		return nil, errors.New("MCP dispatcher requires matching immutable authority and service scope")
	}
	profile, err := submission.NewToolProfile(guard.Binding().Mode)
	if err != nil {
		return nil, err
	}
	return &ToolDispatcher{guard: guard, service: service, profile: profile}, nil
}

// Handler is installed only on an AllowTools broker. Original helper paths keep
// their original handler and publication behavior; they are not rerouted here.
func (d *ToolDispatcher) Handler(ctx context.Context, q submission.Request) (json.RawMessage, error) {
	live, release, err := d.guard.Context(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	live = context.WithValue(live, mcpCommunicationKey{}, true)
	if q.Method == "tools/list" {
		var empty struct{}
		if err := decodeMCPArguments(q.Payload, &empty); err != nil {
			return nil, err
		}
		return json.Marshal(struct {
			Tools []submission.ToolSpec `json:"tools"`
		}{d.profile.List()})
	}
	if q.Method != "tools/call" {
		return nil, errors.New("MCP dispatcher refuses non-tool operation")
	}
	call, err := submission.DecodeToolCall(q.Payload)
	if err != nil {
		return nil, err
	}
	if !d.profile.Allows(call.Name) {
		return nil, errors.New("MCP tool not permitted")
	}
	switch call.Name {
	case "communication_preflight":
		result, err := d.service.Preflight(live, call.Arguments)
		if err != nil {
			return nil, err
		}
		return json.Marshal(result)
	case "communication_submit":
		if q.RequestID == "" {
			return nil, errors.New("missing durable application correlation")
		}
		result, err := d.service.Submit(live, q.RequestID, call.Arguments)
		if err != nil {
			return nil, err
		}
		return json.Marshal(result)
	case "communication_receipt":
		var lookup struct {
			RequestID string `json:"request_id"`
		}
		if err := decodeMCPArguments(call.Arguments, &lookup); err != nil {
			return nil, err
		}
		if lookup.RequestID == "" || len(lookup.RequestID) > 128 || lookup.RequestID != q.RequestID {
			return nil, errors.New("invalid receipt correlation")
		}
		result, err := d.service.Receipt(live, d.guard.Binding().RunID, lookup.RequestID)
		if err != nil {
			return nil, err
		}
		return json.Marshal(result)
	}
	return nil, fmt.Errorf("MCP tool has no registered handler")
}

type mcpCommunicationKey struct{}

func decodeMCPArguments(data []byte, out interface{}) error {
	// Reuse the transport's strict duplicate-key/depth checks without a new
	// application validator. A temporary tool envelope validates raw parameters.
	envelope := append([]byte(`{"name":"arguments","arguments":`), data...)
	envelope = append(envelope, '}')
	if _, err := submission.DecodeToolCall(envelope); err != nil {
		return err
	}
	return strictMCPFields(data, out)
}

func strictMCPFields(data []byte, out interface{}) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return err
	}
	if err := d.Decode(&json.RawMessage{}); !errors.Is(err, io.EOF) {
		return errors.New("invalid trailing MCP parameters")
	}
	return nil
}
