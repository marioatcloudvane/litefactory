package connector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

const protocolLatest = "2025-11-25"

func supportedVersion(v string) bool { return v == protocolLatest || v == "2025-06-18" }

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

func (s *Server) mcp(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	event, tool := "connection", "unavailable"
	refusal := func(status int, reason string) {
		s.audit(r, event, "refusal", reason, tool, start)
		http.Error(w, http.StatusText(status), status)
	}
	auth := r.Header.Values("Authorization")
	var credential string
	if len(auth) == 1 {
		scheme, token, ok := strings.Cut(auth[0], " ")
		if ok && strings.EqualFold(scheme, "Bearer") {
			credential = token
		}
	}
	if credential == "" || strings.ContainsAny(credential, " \t\r\n,") || len(credential) > 256 {
		s.challenge(w)
		refusal(401, "credentials")
		return
	}
	grant, err := s.db.CheckConnectorAccess(r.Context(), digest(credential), s.cfg.OwnerID, s.cfg.ResourceURL, "", s.now())
	if err != nil {
		switch {
		case errors.Is(err, store.ErrConnectorScope):
			w.Header().Set("WWW-Authenticate", `Bearer error="insufficient_scope", scope="factory:overview"`)
			refusal(403, "scope")
		case errors.Is(err, store.ErrConnectorInvalid), errors.Is(err, store.ErrConnectorOwner):
			s.challenge(w)
			refusal(401, "credentials")
		default:
			refusal(503, "storage")
		}
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		refusal(405, "method")
		return
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		refusal(415, "content_type")
		return
	}
	if !strings.Contains(r.Header.Get("Accept"), "application/json") {
		refusal(406, "accept")
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var sizeErr *http.MaxBytesError
		if errors.As(err, &sizeErr) {
			refusal(413, "request_size")
		} else {
			refusal(400, "body")
		}
		return
	}
	var req rpcRequest
	if json.Unmarshal(body, &req) != nil || req.JSONRPC != "2.0" || req.Method == "" || !validID(req.ID) {
		s.rpcError(w, nil, -32600, "Invalid Request")
		s.audit(r, "connection", "refusal", "parse", "unavailable", start)
		return
	}
	tool = "unavailable"
	event = "connection"
	if req.Method == "tools/call" {
		event = "tool_call"
		tool = "unknown"
		var params struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(req.Params, &params) == nil && (params.Name == "factory_overview" || params.Name == "task_evidence_read" || params.Name == "usage_read") {
			tool = params.Name
		}
	}
	refuseRPC := func(code int, msg, reason string) {
		if tool == "usage_read" {
			s.writeDiscussionValue(w, map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{"code": code, "message": msg}})
		} else {
			s.rpcError(w, req.ID, code, msg)
		}
		s.audit(r, event, "refusal", reason, tool, start)
	}
	version := r.Header.Get("MCP-Protocol-Version")
	if len(r.Header.Values("MCP-Protocol-Version")) > 1 || req.Method != "initialize" && !supportedVersion(version) || req.Method == "initialize" && version != "" && !supportedVersion(version) {
		refusal(400, "protocol")
		return
	}
	if req.Method != "initialize" && r.Header.Get("Mcp-Session-Id") != "" {
		refusal(400, "session")
		return
	}
	if len(req.ID) == 0 {
		w.WriteHeader(http.StatusAccepted)
		s.audit(r, event, "success", "notification", tool, start)
		return
	}
	switch req.Method {
	case "initialize":
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if json.Unmarshal(req.Params, &params) != nil || params.ProtocolVersion == "" {
			refuseRPC(-32602, "Invalid params", "params")
			return
		}
		if supportedVersion(params.ProtocolVersion) {
			version = params.ProtocolVersion
		} else {
			version = protocolLatest
		}
		instructions, guidanceErr := loadGuidance(s.guidance, s.availableTools(grant))
		if guidanceErr != nil {
			refuseRPC(-32603, "Tool guidance unavailable", "guidance")
			return
		}
		s.rpcResult(w, req.ID, map[string]any{"protocolVersion": version, "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]string{"name": "litefactory", "version": "1"}, "instructions": instructions})
		s.audit(r, event, "success", "initialized", tool, start)
	case "ping":
		s.rpcResult(w, req.ID, map[string]any{})
		s.audit(r, event, "success", "ping", tool, start)
	case "tools/list":
		tools := []any{}
		if slices.Contains(s.availableTools(grant), "factory_overview") {
			tools = append(tools, map[string]any{"name": "factory_overview", "description": overviewToolDescription, "inputSchema": map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}, "outputSchema": overviewOutputSchema(), "annotations": map[string]bool{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false}})
		}
		if slices.Contains(s.availableTools(grant), "task_evidence_read") {
			tools = append(tools, map[string]any{"name": "task_evidence_read", "description": "Read six recorded evidence sections for the exact consented task, with source revisions, coverage gaps and bounded continuation pages.", "inputSchema": evidenceInputSchema(), "outputSchema": evidenceOutputSchema(), "annotations": map[string]bool{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false}})
		}
		if slices.Contains(s.availableTools(grant), "usage_read") {
			tools = append(tools, map[string]any{"name": "usage_read", "description": "Read recorded usage, supported historical cost estimates and their recorded price basis for a consented scope and server-resolved period, with bounded evidence pages.", "inputSchema": usageInputSchema(), "outputSchema": usageOutputSchema(), "annotations": map[string]bool{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false}})
		}

		s.rpcResult(w, req.ID, map[string]any{"tools": tools})
		s.audit(r, event, "success", "listed", tool, start)
	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if json.Unmarshal(req.Params, &params) != nil {
			refuseRPC(-32602, "Invalid params", "params")
			return
		}
		requiredScope := ""
		if params.Name == "factory_overview" && !store.ConnectorScopeHas(grant.Scope, Scope) {
			requiredScope = Scope
		}
		if params.Name == "task_evidence_read" && (!store.ConnectorScopeHas(grant.Scope, DiscussionScope) || grant.Target == nil) {
			requiredScope = DiscussionScope
		}
		if requiredScope != "" {
			w.Header().Set("WWW-Authenticate", `Bearer error="insufficient_scope", scope="`+requiredScope+`"`)
			refusal(http.StatusForbidden, "scope")
			return
		}
		if !slices.Contains(s.availableTools(grant), params.Name) {
			refuseRPC(-32602, "Unknown tool", "tool")
			return
		}
		if params.Name == "factory_overview" && !emptyArguments(params.Arguments) {
			refuseRPC(-32602, "Invalid params", "params")
			return
		}
		var evidenceArgs evidenceArguments
		if params.Name == "task_evidence_read" {
			evidenceArgs, err = parseEvidenceArguments(params.Arguments)
			if err != nil {
				refuseRPC(-32602, "Invalid params", "params")
				return
			}
		}
		var usageArgs usageArguments
		if params.Name == "usage_read" {
			usageArgs, err = parseUsageArguments(params.Arguments)
			if err != nil {
				refuseRPC(-32602, "Invalid params", "params")
				return
			}
			if _, err = s.usageAccess(r.Context(), credential, usageArgs); err != nil {
				refuseRPC(-32603, "Access unavailable", "scope")
				return
			}
		}

		select {
		case s.transport.tools <- struct{}{}:
			defer func() { <-s.transport.tools }()
		default:
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			s.audit(r, event, "refusal", "capacity", tool, start)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), ToolTimeout)
		defer cancel()
		var result ToolResult
		var callErr error
		switch params.Name {
		case "usage_read":
			result, callErr = s.readUsage(ctx, credential, usageArgs)
		case "task_evidence_read":
			result, callErr = s.readEvidence(ctx, credential, evidenceArgs)
		default:
			result, callErr = s.overview.Overview(ctx, params.Arguments)
		}
		reason, outcome := "completed", "success"
		if callErr != nil || ctx.Err() != nil {
			result = ToolResult{Content: []TextContent{{Type: "text", Text: "Overview unavailable"}}, IsError: true}
			if params.Name == "task_evidence_read" {
				gap := "source_unavailable"
				summary := "Task evidence is unavailable."
				if errors.Is(callErr, errEvidenceChanged) {
					gap = "revision_changed_restart_required"
					summary = "Task evidence changed. Restart this section without a cursor."
				}
				result, _ = DiscussionResult(summary, map[string]any{"version": 1, "task_ref": "", "section": evidenceArgs.Section, "generated_at": s.now().UTC(), "revision": "", "items": []any{}, "sources": []any{}, "gaps": []string{gap}, "has_more": false, "next_cursor": ""})
				result.IsError = true
			}
			if params.Name == "usage_read" {
				result = usageFailureResult()
			}
			reason = "provider"
			outcome = "error"
		} else if result.IsError {
			outcome = "error"
			reason = "tool"
		}
		// Only a successful task read can release evidence. Failed or canceled
		// providers return a fixed refusal with no source facts.
		if params.Name == "task_evidence_read" && callErr == nil && ctx.Err() == nil {
			_, accessErr := s.db.CheckConnectorDiscussionAccess(ctx, digest(credential), s.cfg.OwnerID, s.cfg.ResourceURL, evidenceArgs.TaskRef, s.now())
			if accessErr != nil || s.localControl.check(ctx, s.db, s.cfg.OwnerID) != nil {
				refuseRPC(-32603, "Access unavailable", "credentials")
				return
			}
		}

		if params.Name == "usage_read" && callErr == nil && ctx.Err() == nil {
			if _, accessErr := s.usageAccess(ctx, credential, usageArgs); accessErr != nil || s.localControl.check(ctx, s.db, s.cfg.OwnerID) != nil {
				refuseRPC(-32603, "Access unavailable", "credentials")
				return
			}
		}

		var written bool
		if params.Name == "task_evidence_read" || params.Name == "usage_read" {
			written = s.writeDiscussionRPC(w, req.ID, result)
		} else {
			written = s.rpcResult(w, req.ID, result)
		}
		if !written {
			outcome = "error"
			reason = "response_size"
		}
		s.audit(r, event, outcome, reason, tool, start)
	default:
		refuseRPC(-32601, "Method not found", "method")
	}
}

func emptyArguments(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var arguments map[string]json.RawMessage
	return json.Unmarshal(raw, &arguments) == nil && arguments != nil && len(arguments) == 0
}

func validID(id json.RawMessage) bool {
	if len(id) == 0 {
		return true
	}
	if bytes.Equal(id, []byte("null")) {
		return false
	}
	var value any
	if json.Unmarshal(id, &value) != nil {
		return false
	}
	switch value.(type) {
	case string, float64:
		return true
	default:
		return false
	}
}
func (s *Server) challenge(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+strings.TrimSuffix(s.cfg.IssuerURL, "/litefactory")+`/.well-known/oauth-protected-resource/litefactory/mcp"`)
}
func (s *Server) rpcError(w http.ResponseWriter, id json.RawMessage, code int, message string) {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	s.writeRPC(w, map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message}})
}
func (s *Server) rpcResult(w http.ResponseWriter, id json.RawMessage, result any) bool {
	return s.writeRPC(w, map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}
func (s *Server) writeRPC(w http.ResponseWriter, value map[string]any) bool {
	body, err := json.Marshal(value)
	if err != nil || len(body) > MaxResponseBytes {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		fallback, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": value["id"], "error": map[string]any{"code": -32603, "message": "Response unavailable"}})
		if len(fallback) > MaxResponseBytes {
			fallback = []byte(`{"jsonrpc":"2.0","id":null,"error":{"code":-32603,"message":"Response unavailable"}}`)
		}
		_, _ = w.Write(fallback)
		return false
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
	return true
}

// overviewToolDescription retains the existing overview contract. Initialization
// guidance is loaded separately for authorized installed capabilities.
const overviewToolDescription = "Read the owner's software factory: health, work in progress, questions and reviews waiting for the owner (oldest first, with links), and reports confirmed in the last seven days, across all registered repositories. One call answers most questions about the factory. Lead with what needs the owner, then what is running, then health only if something is wrong."

func (s *Server) writeDiscussionRPC(w http.ResponseWriter, id json.RawMessage, result ToolResult) bool {
	return s.writeDiscussionValue(w, map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func (s *Server) writeDiscussionValue(w http.ResponseWriter, value map[string]any) bool {
	id := value["id"]
	body, err := json.Marshal(value)
	if err != nil || len(body) > DiscussionMaxResponseBytes {
		body, _ = json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32603, "message": "Response unavailable"}})
		if len(body) > DiscussionMaxResponseBytes {
			body = []byte(`{"jsonrpc":"2.0","id":null,"error":{"code":-32603,"message":"Response unavailable"}}`)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
		return false
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
	return true
}
