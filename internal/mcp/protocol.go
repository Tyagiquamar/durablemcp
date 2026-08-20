package mcp

import (
	"context"
	"encoding/json"

	"github.com/tyagiquamar/durablemcp/internal/store"
)

// ProtocolVersion is the MCP spec revision this server implements.
const ProtocolVersion = "2025-03-26"

// Request is a JSON-RPC 2.0 request or notification.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response is a JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError is a JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternalError  = -32603
)

// Handler dispatches MCP JSON-RPC methods against the store.
type Handler struct {
	Store      *store.Postgres
	ServerName string
	Version    string
}

// Dispatch routes a single request to the matching MCP method. It returns nil
// for notifications (requests without an id), which take no response.
func (h *Handler) Dispatch(ctx context.Context, req Request) *Response {
	if req.ID == nil {
		return nil // notification -- no response
	}
	switch req.Method {
	case "initialize":
		return h.initialize(req)
	case "tools/list":
		return h.toolsList(ctx, req)
	case "tools/call":
		return h.toolsCall(ctx, req)
	case "ping":
		return ok(req.ID, map[string]any{})
	default:
		return fail(req.ID, codeMethodNotFound, "method not found: "+req.Method)
	}
}

func (h *Handler) initialize(req Request) *Response {
	return ok(req.ID, map[string]any{
		"protocolVersion": ProtocolVersion,
		"serverInfo":      map[string]any{"name": h.ServerName, "version": h.Version},
		"capabilities":    map[string]any{"tools": map[string]any{}},
	})
}

func (h *Handler) toolsList(ctx context.Context, req Request) *Response {
	tools, err := h.Store.ListTools(ctx)
	if err != nil {
		return fail(req.ID, codeInternalError, err.Error())
	}
	out := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		out = append(out, map[string]any{
			"name":        t.Name,
			"description": t.Description,
			"inputSchema": t.InputSchema,
		})
	}
	return ok(req.ID, map[string]any{"tools": out})
}

type callParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
	Meta      struct {
		IdempotencyKey string `json:"idempotency_key"`
		Namespace      string `json:"namespace"`
	} `json:"_meta"`
}

func (h *Handler) toolsCall(ctx context.Context, req Request) *Response {
	var p callParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return fail(req.ID, codeInvalidParams, "invalid params: "+err.Error())
	}
	if p.Name == "" {
		return fail(req.ID, codeInvalidParams, "tool name is required")
	}
	namespace := p.Meta.Namespace
	if namespace == "" {
		namespace = "default"
	}
	idempotencyKey := p.Meta.IdempotencyKey
	if idempotencyKey == "" {
		// Without a client-supplied key, hash the arguments so identical calls
		// collapse; distinct calls do not.
		idempotencyKey = hashKey(p.Name, p.Arguments)
	}

	res, err := h.Store.Submit(ctx, namespace, p.Name, idempotencyKey, p.Arguments)
	if err == store.ErrUnknownTool {
		return fail(req.ID, codeInvalidParams, "unknown tool: "+p.Name)
	}
	if err != nil {
		return fail(req.ID, codeInternalError, err.Error())
	}

	meta := map[string]any{"execution_id": res.ExecutionID, "status": res.Status, "duplicate": res.Duplicate}
	if res.Status == "completed" && len(res.Result) > 0 {
		return ok(req.ID, map[string]any{
			"content": []map[string]any{{"type": "text", "text": string(res.Result)}},
			"isError": false,
			"_meta":   meta,
		})
	}
	return ok(req.ID, map[string]any{
		"content": []map[string]any{{"type": "text", "text": "accepted execution " + res.ExecutionID + " (status " + res.Status + ")"}},
		"isError": false,
		"_meta":   meta,
	})
}

func ok(id json.RawMessage, result any) *Response {
	return &Response{JSONRPC: "2.0", ID: id, Result: result}
}

func fail(id json.RawMessage, code int, msg string) *Response {
	return &Response{JSONRPC: "2.0", ID: id, Error: &RPCError{Code: code, Message: msg}}
}
