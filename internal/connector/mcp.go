package connector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/cloudvane/litefactory/internal/connectorstate"
	"io"
	"maps"
	"mime"
	"net/http"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
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
	grant, err := s.storage.CheckConnectorAccess(r.Context(), digest(credential), s.cfg.OwnerID, s.cfg.ResourceURL, "", s.now())
	if err != nil {
		switch {
		case errors.Is(err, connectorstate.ErrConnectorScope):
			w.Header().Set("WWW-Authenticate", `Bearer error="insufficient_scope", scope="factory:overview"`)
			refusal(403, "scope")
		case errors.Is(err, connectorstate.ErrConnectorInvalid), errors.Is(err, connectorstate.ErrConnectorOwner):
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
		if json.Unmarshal(req.Params, &params) == nil && isKnownTool(params.Name) {
			tool = params.Name
		}
	}
	writeRefusal := func(rpcErr map[string]any) {
		if tool == "usage_read" || isCodeTool(tool) || isActionTool(tool) || isWorkflowTool(tool) || isAdvisoryTool(tool) || isBrainstormTool(tool) {
			s.writeDiscussionValue(w, map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": rpcErr})
		} else {
			id := req.ID
			if len(id) == 0 {
				id = json.RawMessage("null")
			}
			s.writeRPC(w, map[string]any{"jsonrpc": "2.0", "id": id, "error": rpcErr})
		}
	}
	refuseRPC := func(code int, msg, reason string) {
		writeRefusal(map[string]any{"code": code, "message": msg})
		s.audit(r, event, "refusal", reason, tool, start)
	}
	// refuseParams names the known tool, the argument and the problem; it
	// never repeats a supplied value.
	refuseParams := func(err error) {
		problem, detail := "the arguments are not valid for this tool", "invalid_value"
		data := map[string]any{}
		var argErr *argumentError
		if errors.As(err, &argErr) {
			problem, detail = argErr.Problem, argErr.Code
			if argErr.Argument != "" {
				data["argument"] = argErr.Argument
			}
		}
		data["problem"] = problem
		message := "Invalid params: " + problem
		if isKnownTool(tool) {
			message = "Invalid params: " + tool + ": " + problem
			data["tool"] = tool
		}
		writeRefusal(map[string]any{"code": -32602, "message": message, "data": data})
		s.auditDetail(r, event, "refusal", "params", detail, tool, start)
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
		instructions, guidanceErr := composeInstructions(s.guidance, s.availableTools(grant), s.missingScopes(grant))
		if guidanceErr != nil {
			refuseRPC(-32603, "Tool guidance unavailable", "guidance")
			return
		}
		s.rpcResult(w, req.ID, map[string]any{"protocolVersion": version, "capabilities": map[string]any{"tools": map[string]any{}, "prompts": map[string]any{}}, "serverInfo": map[string]string{"name": "litefactory", "version": "1"}, "instructions": instructions})
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
			tools = append(tools, map[string]any{"name": "task_evidence_read", "description": "Read six recorded evidence sections for one task, named by task_ref, in a repository this grant covers, with source revisions, coverage gaps and bounded continuation pages.", "inputSchema": evidenceInputSchema(), "outputSchema": evidenceOutputSchema(), "annotations": map[string]bool{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false}})
		}
		if slices.Contains(s.availableTools(grant), "usage_read") {
			tools = append(tools, map[string]any{"name": "usage_read", "description": "Read recorded usage, supported historical cost estimates and their recorded price basis for a consented scope and server-resolved period, with bounded evidence pages.", "inputSchema": usageInputSchema(), "outputSchema": usageOutputSchema(), "annotations": map[string]bool{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false}})
		}

		for _, name := range codeToolNames {
			if slices.Contains(s.availableTools(grant), name) {
				tools = append(tools, map[string]any{"name": name, "description": codeToolDescription(name), "inputSchema": codeInputSchema(name), "outputSchema": codeOutputSchema(), "annotations": map[string]bool{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false}})
			}
		}

		for _, name := range actionToolNames {
			if slices.Contains(s.availableTools(grant), name) {
				tools = append(tools, map[string]any{"name": name, "description": actionDescription(name), "inputSchema": s.actionSchema(name, slices.Contains(s.availableTools(grant), "brainstorm_drafts_get")), "annotations": map[string]bool{"readOnlyHint": name == "action_inspect" || name == "action_history", "destructiveHint": name == "action_confirm", "idempotentHint": true, "openWorldHint": true}})
			}
		}

		for _, name := range workflowToolNames {
			if slices.Contains(s.availableTools(grant), name) {
				tools = append(tools, map[string]any{"name": name, "description": workflowDescription(name), "inputSchema": workflowInputSchema(name), "annotations": map[string]bool{"readOnlyHint": name == "workflows_list" || name == "workflow_describe" || name == "workflow_job_get", "idempotentHint": true, "openWorldHint": true}})
			}
		}

		for _, name := range brainstormToolNames {
			if slices.Contains(s.availableTools(grant), name) {
				tools = append(tools, map[string]any{"name": name, "description": brainstormDescription(name), "inputSchema": brainstormInputSchema(name), "annotations": map[string]bool{"readOnlyHint": brainstormReadOnly(name), "destructiveHint": false, "idempotentHint": name != "brainstorm_scan_cancel", "openWorldHint": name == "brainstorm_ask" || name == "issue_search"}})
			}
		}
		for _, name := range advisoryToolNames {
			if slices.Contains(s.availableTools(grant), name) {
				tools = append(tools, map[string]any{"name": name, "description": advisoryDescription(name), "inputSchema": advisoryInputSchema(name), "outputSchema": advisoryOutputSchema(name), "annotations": map[string]bool{"readOnlyHint": name == "advisory_job_get", "destructiveHint": false, "idempotentHint": true, "openWorldHint": true}})
			}
		}
		for _, name := range decisionToolNames {
			if slices.Contains(s.availableTools(grant), name) {
				tools = append(tools, map[string]any{"name": name, "description": decisionDescription(name), "inputSchema": decisionInputSchema(name), "annotations": map[string]bool{"readOnlyHint": name == "decision_get", "destructiveHint": false, "idempotentHint": true, "openWorldHint": true}})
			}
		}
		s.rpcResult(w, req.ID, map[string]any{"tools": tools})
		s.audit(r, event, "success", "listed", tool, start)
	case "prompts/list":
		if len(req.Params) > 0 && string(req.Params) != "null" {
			var lp struct {
				Cursor *string `json:"cursor"`
			}
			if !strictParams(req.Params, &lp) || lp.Cursor != nil {
				refuseRPC(-32602, "Invalid params", "params")
				return
			}
		}
		prompts, perr := promptList(s.guidance, s.availableTools(grant))
		if perr != nil {
			refuseRPC(-32603, "Prompt guidance unavailable", "guidance")
			return
		}
		s.rpcResult(w, req.ID, map[string]any{"prompts": prompts})
		s.audit(r, event, "success", "listed", tool, start)
	case "prompts/get":
		var gp struct {
			Name      string            `json:"name"`
			Arguments map[string]string `json:"arguments"`
		}
		if !strictParams(req.Params, &gp) {
			refuseRPC(-32602, "Invalid params", "params")
			return
		}
		var spec *promptSpec
		for i := range promptSpecs {
			if promptSpecs[i].Name == gp.Name && promptSpecs[i].listed(s.availableTools(grant)) {
				spec = &promptSpecs[i]
			}
		}
		arg := ""
		argOK := spec != nil
		if spec != nil {
			if spec.Argument == "" {
				argOK = len(gp.Arguments) == 0
			} else {
				v, has := gp.Arguments[spec.Argument]
				arg = v
				argOK = has && len(gp.Arguments) == 1 && v != "" && len(v) <= 512 && !strings.ContainsAny(v, "\r\n")
			}
		}
		if !argOK {
			refuseRPC(-32602, "Invalid params", "params")
			return
		}
		desc, text, perr := promptText(s.guidance, *spec, arg)
		if perr != nil {
			refuseRPC(-32603, "Prompt guidance unavailable", "guidance")
			return
		}
		s.rpcResult(w, req.ID, map[string]any{"description": desc, "messages": []any{map[string]any{"role": "user", "content": map[string]any{"type": "text", "text": text}}}})
		s.audit(r, event, "success", "prompt", tool, start)
	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if json.Unmarshal(req.Params, &params) != nil {
			refuseParams(errToolCallEnvelope)
			return
		}
		if isActionTool(params.Name) || isWorkflowTool(params.Name) || isAdvisoryTool(params.Name) || isDecisionTool(params.Name) || isBrainstormTool(params.Name) {
			d := json.NewDecoder(bytes.NewReader(body))
			if err := uniqueJSONObject(d); err != nil {
				refuseParams(uniqueArgumentsError(err))
				return
			}
			var envelopeFields, callFields map[string]json.RawMessage
			if json.Unmarshal(body, &envelopeFields) != nil || json.Unmarshal(req.Params, &callFields) != nil {
				refuseParams(errToolCallEnvelope)
				return
			}
			for key := range envelopeFields {
				if !slices.Contains([]string{"jsonrpc", "id", "method", "params"}, key) {
					refuseParams(errToolCallEnvelope)
					return
				}
			}
			for key := range callFields {
				if key != "name" && key != "arguments" {
					refuseParams(errToolCallEnvelope)
					return
				}
			}
			d = json.NewDecoder(bytes.NewReader(req.Params))
			d.DisallowUnknownFields()
			if err := d.Decode(&params); err != nil {
				refuseParams(errToolCallEnvelope)
				return
			}
		}
		requiredScope := ""
		if (isActionTool(params.Name) || isWorkflowTool(params.Name)) && !actionGrant(grant) {
			requiredScope = RepositoryActionsScope + " " + StandardsManagementScope
		}
		if isDecisionTool(params.Name) && !decisionGrant(grant) {
			requiredScope = RepositoryActionsScope
		}
		if isAdvisoryTool(params.Name) && !advisoryToolAllowed(grant, params.Name) {
			requiredScope = AdvisoryScope
			if params.Name == "discussion_summary_publish" || params.Name == "discussion_summary_get" {
				requiredScope = DiscussionPublishScope
			}
			if params.Name == "discussion_open" {
				requiredScope = AdvisoryScope + " " + DiscussionPublishScope
			}
		}
		if isBrainstormTool(params.Name) && !brainstormToolAllowed(grant, params.Name) {
			requiredScope = BrainstormScope
			if params.Name == "brainstorm_tasks_get" {
				requiredScope = BrainstormScope + " " + RepositoryActionsScope
			}
		}
		if params.Name == "factory_overview" && !connectorstate.ConnectorScopeHas(grant.Scope, Scope) {
			requiredScope = Scope
		}
		if params.Name == "task_evidence_read" && !connectorstate.ConnectorScopeHas(grant.Scope, DiscussionScope) {
			requiredScope = DiscussionScope
		}
		if requiredScope != "" {
			w.Header().Set("WWW-Authenticate", `Bearer error="insufficient_scope", scope="`+requiredScope+`"`)
			refusal(http.StatusForbidden, "scope")
			return
		}
		if !slices.Contains(s.availableTools(grant), params.Name) {
			refuseRPC(-32602, "Unknown tool; call tools/list for the tools this grant can use", "tool")
			return
		}
		if params.Name == "factory_overview" && !emptyArguments(params.Arguments) {
			refuseParams(errNoArguments)
			return
		}
		var evidenceArgs evidenceArguments
		if params.Name == "task_evidence_read" {
			evidenceArgs, err = parseEvidenceArguments(params.Arguments)
			if err != nil {
				refuseParams(err)
				return
			}
		}
		var usageArgs usageArguments
		if params.Name == "usage_read" {
			usageArgs, err = parseUsageArguments(params.Arguments)
			if err != nil {
				refuseParams(err)
				return
			}
			if _, _, err = s.usageAccess(r.Context(), credential, usageArgs); err != nil {
				message := "Access unavailable"
				if summary := refusalSummary(err); summary != "" {
					message = strings.TrimSuffix(summary, ".")
				}
				refuseRPC(-32603, message, "scope")
				return
			}
		}

		var codeArgs codeArguments
		if isCodeTool(params.Name) {
			codeArgs, err = parseCodeArguments(params.Name, params.Arguments)
			if err != nil {
				refuseParams(err)
				return
			}
		}

		var workflowArgs workflowArguments
		if isWorkflowTool(params.Name) {
			workflowArgs, err = parseWorkflowArguments(params.Name, params.Arguments)
			if err != nil {
				refuseParams(err)
				return
			}
		}

		var advisoryArgs advisoryArguments
		if isAdvisoryTool(params.Name) {
			advisoryArgs, err = parseAdvisoryArguments(params.Name, params.Arguments)
			if err != nil {
				refuseParams(err)
				return
			}
		}
		var brainstormArgs brainstormArguments
		if isBrainstormTool(params.Name) {
			brainstormArgs, err = parseBrainstormArguments(params.Name, params.Arguments)
			if err != nil {
				refuseParams(err)
				return
			}
		}
		var decisionArgs decisionArguments
		if isDecisionTool(params.Name) {
			decisionArgs, err = parseDecisionArguments(params.Name, params.Arguments)
			if err != nil {
				refuseParams(err)
				return
			}
		}
		var actionArgs actionArguments
		if isActionTool(params.Name) {
			actionArgs, err = parseActionArguments(params.Name, params.Arguments)
			if err != nil {
				refuseParams(err)
				return
			}
		}

		select {
		case s.transport.tools <- struct{}{}:
			defer func() { <-s.transport.tools }()
		default:
			refuseRetryLater(w, "concurrency")
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
			switch {
			case isDecisionTool(params.Name):
				result, callErr = s.callDecision(ctx, credential, params.Name, decisionArgs)
			case isAdvisoryTool(params.Name):
				result, callErr = s.callAdvisory(ctx, credential, params.Name, advisoryArgs)
			case isBrainstormTool(params.Name):
				result, callErr = s.callBrainstorm(ctx, credential, params.Name, brainstormArgs)
			case isWorkflowTool(params.Name):
				result, callErr = s.callWorkflow(ctx, credential, params.Name, workflowArgs)
			case isActionTool(params.Name):
				result, callErr = s.callAction(ctx, credential, params.Name, actionArgs)
			case isCodeTool(params.Name):
				result, callErr = s.readCode(ctx, credential, params.Name, codeArgs)
			default:
				result, callErr = s.overview.Overview(ctx, params.Arguments)
			}
		}
		reason, outcome := "completed", "success"
		if callErr != nil || ctx.Err() != nil {
			result = ToolResult{Content: []TextContent{{Type: "text", Text: "Overview unavailable"}}, IsError: true}
			refused := refusalSummary(callErr)
			if params.Name == "task_evidence_read" {
				gap := "source_unavailable"
				summary := "Task evidence is unavailable."
				if errors.Is(callErr, errEvidenceChanged) {
					gap = "revision_changed_restart_required"
					summary = "Task evidence changed. Restart this section without a cursor."
				}
				if errors.Is(callErr, errEvidenceCursor) {
					gap = "invalid_cursor_restart_required"
					summary = "Task evidence is unavailable: the cursor is not valid for this task section. Restart this section without a cursor."
				}
				if refused != "" {
					gap, summary = "access_refused", refused
				}
				result, _ = DiscussionResult(summary, map[string]any{"version": 1, "task_ref": "", "section": evidenceArgs.Section, "generated_at": s.now().UTC(), "revision": "", "items": []any{}, "sources": []any{}, "gaps": []string{gap}, "has_more": false, "next_cursor": ""})
				result.IsError = true
			}
			if params.Name == "usage_read" {
				result = usageFailureResult(refused)
			}
			if isCodeTool(params.Name) {
				result = codeFailureResult(refused)
			}
			if isActionTool(params.Name) || isWorkflowTool(params.Name) {
				result = actionFailure(callErr)
			}
			if isDecisionTool(params.Name) {
				result = decisionFailure(callErr)
			}
			if isAdvisoryTool(params.Name) {
				result = advisoryFailure(callErr)
			}
			if isBrainstormTool(params.Name) {
				result = brainstormFailure(callErr)
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
			_, _, accessErr := s.access.CheckConnectorDiscussionAccess(ctx, digest(credential), s.cfg.OwnerID, s.cfg.ResourceURL, evidenceArgs.TaskRef, s.now())
			if accessErr != nil || s.localControl.check(ctx, s.storage, s.cfg.OwnerID) != nil {
				refuseRPC(-32603, "Access unavailable", "credentials")
				return
			}
		}

		if params.Name == "usage_read" && callErr == nil && ctx.Err() == nil {
			if _, _, accessErr := s.usageAccess(ctx, credential, usageArgs); accessErr != nil || s.localControl.check(ctx, s.storage, s.cfg.OwnerID) != nil {
				refuseRPC(-32603, "Access unavailable", "credentials")
				return
			}
		}

		var written bool
		if params.Name == "task_evidence_read" || params.Name == "usage_read" || isCodeTool(params.Name) || isActionTool(params.Name) || isWorkflowTool(params.Name) || isAdvisoryTool(params.Name) || isDecisionTool(params.Name) || isBrainstormTool(params.Name) {
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
	// The scope attribute names every known scope (MCP authorization, scope
	// discovery), so a client re-authorizing asks for all of them.
	w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+strings.TrimSuffix(s.cfg.IssuerURL, "/litefactory")+`/.well-known/oauth-protected-resource/litefactory/mcp", scope="`+strings.Join(scopeNames(), " ")+`"`)
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

// isKnownTool reports whether name is a tool this connector dispatches; the
// outcome log and argument refusals name only such tools.
func isKnownTool(name string) bool {
	return name == "factory_overview" || name == "task_evidence_read" || name == "usage_read" || isCodeTool(name) || isActionTool(name) || isWorkflowTool(name) || isAdvisoryTool(name) || isDecisionTool(name) || isBrainstormTool(name)
}

// argumentError is a refused tool argument in plain words. Argument is a
// known field or a short safe key, else empty; Problem never carries a value;
// Code is a fixed short code for the outcome log.
type argumentError struct {
	Argument, Problem, Code string
}

func (e *argumentError) Error() string { return e.Problem }

// safeArgumentName reports whether a client-supplied key may be repeated in a
// refusal: at most 64 characters from [a-z0-9_].
func safeArgumentName(key string) bool {
	if key == "" || len(key) > 64 {
		return false
	}
	for _, c := range key {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' {
			return false
		}
	}
	return true
}

func namedArgument(name, problem, code string) error {
	if !safeArgumentName(name) {
		return &argumentError{Problem: "an argument " + problem, Code: code}
	}
	return &argumentError{Argument: name, Problem: `"` + name + `" ` + problem, Code: code}
}

func missingArgument(name string) error { return namedArgument(name, "is required", "missing") }

func invalidArgument(name, problem string) error {
	return namedArgument(name, problem, "invalid_value")
}

func unknownArgument(key string) error {
	if !safeArgumentName(key) {
		return &argumentError{Problem: "an unknown argument was supplied; it is not an argument of this tool", Code: "unknown_argument"}
	}
	return &argumentError{Argument: key, Problem: `"` + key + `" is not an argument of this tool`, Code: "unknown_argument"}
}

func nullArgument(name string) error { return invalidArgument(name, "must not be null") }

func duplicateArgument() error {
	return &argumentError{Problem: "an argument is supplied more than once", Code: "invalid_value"}
}

func argumentsShapeError() error {
	return &argumentError{Problem: "arguments must be a single JSON object", Code: "wrong_type"}
}

// decodeArgumentError names the argument a strict json decode refused,
// without the refused value; other failures become argumentsShapeError.
func decodeArgumentError(err error) error {
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) && typeErr.Field != "" {
		name, _, _ := strings.Cut(typeErr.Field, ".")
		want := "a different type"
		switch typeErr.Type.Kind() {
		case reflect.String:
			want = "a string"
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			want = "an integer"
		case reflect.Bool:
			want = "a boolean"
		case reflect.Map, reflect.Struct:
			want = "an object"
		case reflect.Slice, reflect.Array:
			want = "an array"
		}
		if name != typeErr.Field {
			return namedArgument(name, "has a member that must be "+want, "wrong_type")
		}
		return namedArgument(name, "must be "+want, "wrong_type")
	}
	if key, ok := strings.CutPrefix(fmt.Sprint(err), "json: unknown field "); ok {
		if unquoted, uerr := strconv.Unquote(key); uerr == nil {
			return unknownArgument(unquoted)
		}
		return unknownArgument("")
	}
	return argumentsShapeError()
}

// uniqueArgumentsError maps a uniqueJSONObject failure to a plain refusal.
func uniqueArgumentsError(err error) error {
	var argErr *argumentError
	if errors.As(err, &argErr) {
		return err
	}
	return argumentsShapeError()
}

// checkArgumentFields refuses, in key order, a key outside allowed or a null
// value, then a missing required key.
func checkArgumentFields(fields map[string]json.RawMessage, allowed, required []string) error {
	for _, key := range slices.Sorted(maps.Keys(fields)) {
		if !slices.Contains(allowed, key) {
			return unknownArgument(key)
		}
		if bytes.Equal(bytes.TrimSpace(fields[key]), []byte("null")) {
			return nullArgument(key)
		}
	}
	for _, key := range required {
		if _, ok := fields[key]; !ok {
			return missingArgument(key)
		}
	}
	return nil
}

// errToolCallEnvelope refuses a tools/call whose request or params carry
// anything beyond the JSON-RPC fields, "name" and "arguments".
var errToolCallEnvelope error = &argumentError{Problem: `params must be one JSON object with only "name" and "arguments"`, Code: "invalid_value"}

var errNoArguments error = &argumentError{Problem: "this tool takes no arguments; send an empty object", Code: "unknown_argument"}
