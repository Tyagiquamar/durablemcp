package mcp

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// hashKey derives a deterministic idempotency key from a tool name and args.
func hashKey(name string, args json.RawMessage) string {
	sum := sha256.Sum256(append([]byte(name+":"), args...))
	return "auto-" + hex.EncodeToString(sum[:8])
}

// ServeStdio runs the newline-delimited JSON-RPC loop over stdin/stdout for
// Claude Desktop / Cursor.
func (h *Handler) ServeStdio(ctx context.Context, in io.Reader, out io.Writer) error {
	reader := bufio.NewScanner(in)
	reader.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	writer := bufio.NewWriter(out)

	for reader.Scan() {
		line := reader.Bytes()
		if len(line) == 0 {
			continue
		}
		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			writeLine(writer, fail(nil, codeParseError, "parse error"))
			continue
		}
		resp := h.Dispatch(ctx, req)
		if resp != nil {
			writeLine(writer, resp)
		}
	}
	return reader.Err()
}

func writeLine(w *bufio.Writer, resp *Response) {
	b, _ := json.Marshal(resp)
	w.Write(b)
	w.WriteByte('\n')
	w.Flush()
}

// HTTPHandler handles POST /mcp client->server JSON-RPC messages.
func (h *Handler) HTTPHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			writeJSONRPC(w, fail(nil, codeParseError, "read error"))
			return
		}
		var req Request
		if err := json.Unmarshal(body, &req); err != nil {
			writeJSONRPC(w, fail(nil, codeParseError, "parse error"))
			return
		}
		if req.JSONRPC != "2.0" {
			writeJSONRPC(w, fail(req.ID, codeInvalidRequest, "jsonrpc must be 2.0"))
			return
		}
		resp := h.Dispatch(r.Context(), req)
		if resp == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		writeJSONRPC(w, resp)
	}
}

// SSEHandler streams server->client keepalive events over GET /mcp/sse.
func (h *Handler) SSEHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		fmt.Fprintf(w, "event: ready\ndata: {\"protocolVersion\":%q}\n\n", ProtocolVersion)
		flusher.Flush()

		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
				fmt.Fprint(w, ": keepalive\n\n")
				flusher.Flush()
			}
		}
	}
}

func writeJSONRPC(w http.ResponseWriter, resp *Response) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
