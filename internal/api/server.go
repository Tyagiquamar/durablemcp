package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/tyagiquamar/durablemcp/internal/store"
)

// Server exposes the read-only dashboard API plus a guarded testing endpoint.
type Server struct {
	store         *store.Postgres
	readerKey     string
	allowTestEnds bool
	mux           *http.ServeMux
}

// New builds the REST server. When mcpHandler is non-nil, MCP HTTP routes are
// mounted alongside the REST API on the same mux.
func New(st *store.Postgres, readerKey string, allowTestEnds bool, mcpPost, mcpSSE http.HandlerFunc) *Server {
	s := &Server{store: st, readerKey: readerKey, allowTestEnds: allowTestEnds, mux: http.NewServeMux()}

	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, http.StatusOK, map[string]string{"status": "ok"}) })
	s.mux.HandleFunc("GET /readyz", s.ready)

	s.mux.HandleFunc("GET /api/v1/stats", s.requireReader(s.stats))
	s.mux.HandleFunc("GET /api/v1/executions", s.requireReader(s.listExecutions))
	s.mux.HandleFunc("GET /api/v1/executions/{id}", s.requireReader(s.getExecution))
	s.mux.HandleFunc("GET /api/v1/executions/{id}/events", s.requireReader(s.listEvents))
	s.mux.HandleFunc("GET /api/v1/tools", s.requireReader(s.listTools))
	s.mux.HandleFunc("GET /api/v1/workers", s.requireReader(s.listWorkers))

	if mcpPost != nil {
		s.mux.HandleFunc("POST /mcp", mcpPost)
	}
	if mcpSSE != nil {
		s.mux.HandleFunc("GET /mcp/sse", mcpSSE)
	}
	if allowTestEnds {
		// Testing-only: force a completion with a caller-supplied fencing token
		// so the fencing demo can prove stale-worker rejection over HTTP.
		s.mux.HandleFunc("POST /internal/complete", s.forceComplete)
	}

	return s
}

// ServeHTTP satisfies http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Ping(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

// requireReader enforces the dashboard reader bearer token when configured.
func (s *Server) requireReader(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.readerKey != "" {
			auth := r.Header.Get("Authorization")
			if auth != "Bearer reader:"+s.readerKey && auth != "Bearer "+s.readerKey {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid reader key"})
				return
			}
		}
		next(w, r)
	}
}

func (s *Server) stats(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.Stats(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) listExecutions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	list, err := s.store.ListExecutions(r.Context(), q.Get("status"), q.Get("tool"), limit, offset)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"executions": list})
}

func (s *Server) getExecution(w http.ResponseWriter, r *http.Request) {
	ex, err := s.store.GetExecution(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	if ex == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	writeJSON(w, http.StatusOK, ex)
}

func (s *Server) listEvents(w http.ResponseWriter, r *http.Request) {
	events, err := s.store.ListEvents(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

func (s *Server) listTools(w http.ResponseWriter, r *http.Request) {
	tools, err := s.store.ListTools(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tools": tools})
}

func (s *Server) listWorkers(w http.ResponseWriter, r *http.Request) {
	workers, err := s.store.ListWorkers(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"workers": workers})
}

func (s *Server) forceComplete(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ExecutionID  string          `json:"execution_id"`
		FencingToken int64           `json:"fencing_token"`
		WorkerID     string          `json:"worker_id"`
		Result       json.RawMessage `json:"result"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if body.Result == nil {
		body.Result = json.RawMessage(`{"forced":true}`)
	}
	err := s.store.Complete(context.Background(), body.ExecutionID, body.FencingToken, body.WorkerID, body.Result)
	if err == store.ErrStale {
		writeJSON(w, http.StatusConflict, map[string]any{"rejected": true, "reason": "stale fencing token"})
		return
	}
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"completed": true})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if status != http.StatusNoContent {
		_ = json.NewEncoder(w).Encode(value)
	}
}

func writeError(w http.ResponseWriter, err error) {
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
}
