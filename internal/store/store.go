package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNoWork is returned by Claim when there is no ready execution to run.
var ErrNoWork = errors.New("no ready execution")

// ErrStale is returned when a heartbeat, complete, or fail is attempted with a
// fencing token that has been superseded by a newer claim.
var ErrStale = errors.New("stale fencing token")

// ErrUnknownTool is returned when a submission references an unregistered tool.
var ErrUnknownTool = errors.New("unknown tool")

// Tool is a registered MCP tool.
type Tool struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	InputSchema  json.RawMessage `json:"input_schema"`
	MaxAttempts  int             `json:"max_attempts"`
	LeaseSeconds int             `json:"lease_seconds"`
	CreatedAt    time.Time       `json:"created_at"`
}

// Lease is the active claim on an execution.
type Lease struct {
	ExecutionID  string    `json:"execution_id"`
	WorkerID     string    `json:"worker_id"`
	FencingToken int64     `json:"fencing_token"`
	LeaseExpires time.Time `json:"lease_expires"`
	ClaimedAt    time.Time `json:"claimed_at"`
}

// Execution is one tool-call attempt.
type Execution struct {
	ID             string          `json:"id"`
	Namespace      string          `json:"namespace"`
	ToolName       string          `json:"tool_name"`
	IdempotencyKey string          `json:"idempotency_key"`
	InputArgs      json.RawMessage `json:"input_args"`
	Status         string          `json:"status"`
	Attempts       int             `json:"attempts"`
	MaxAttempts    int             `json:"max_attempts"`
	Result         json.RawMessage `json:"result,omitempty"`
	ErrorMessage   string          `json:"error_message,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
	Lease          *Lease          `json:"lease,omitempty"`
}

// Event is one immutable entry in the execution event log.
type Event struct {
	ID           int64           `json:"id"`
	ExecutionID  string          `json:"execution_id"`
	EventType    string          `json:"event_type"`
	WorkerID     string          `json:"worker_id,omitempty"`
	FencingToken *int64          `json:"fencing_token,omitempty"`
	Payload      json.RawMessage `json:"payload,omitempty"`
	OccurredAt   time.Time       `json:"occurred_at"`
}

// Stats is the aggregate dashboard view.
type Stats struct {
	Total        int `json:"total"`
	Ready        int `json:"ready"`
	Running      int `json:"running"`
	Completed    int `json:"completed"`
	Failed       int `json:"failed"`
	ActiveLeases int `json:"active_leases"`
}

// WorkerInfo is derived from active leases.
type WorkerInfo struct {
	WorkerID       string  `json:"worker_id"`
	Claimed        int     `json:"claimed"`
	OldestClaimAge float64 `json:"oldest_claim_age_seconds"`
}

// Claim is the result of an executor claiming a ready execution.
type Claim struct {
	ExecutionID  string          `json:"execution_id"`
	ToolName     string          `json:"tool_name"`
	InputArgs    json.RawMessage `json:"input_args"`
	Attempts     int             `json:"attempts"`
	MaxAttempts  int             `json:"max_attempts"`
	FencingToken int64           `json:"fencing_token"`
	LeaseExpires time.Time       `json:"lease_expires"`
}

// SubmitResult is the outcome of the tools/call critical path.
type SubmitResult struct {
	ExecutionID string          `json:"execution_id"`
	Status      string          `json:"status"`
	Result      json.RawMessage `json:"result,omitempty"`
	Duplicate   bool            `json:"duplicate"`
}

// Postgres is the single repository backing every DurableMCP process.
type Postgres struct {
	pool *pgxpool.Pool
}

// NewPostgres opens a pgx pool and verifies connectivity.
func NewPostgres(ctx context.Context, databaseURL string) (*Postgres, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Postgres{pool: pool}, nil
}

// Close releases the connection pool.
func (p *Postgres) Close() { p.pool.Close() }

// Ping verifies database connectivity for readiness checks.
func (p *Postgres) Ping(ctx context.Context) error { return p.pool.Ping(ctx) }

func rollback(ctx context.Context, tx pgx.Tx) { _ = tx.Rollback(ctx) }

// appendEvent inserts one immutable row into the execution event log.
func appendEvent(ctx context.Context, tx pgx.Tx, executionID, eventType, workerID string, token *int64, payload any) error {
	var raw []byte
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		raw = b
	}
	var workerArg any
	if workerID != "" {
		workerArg = workerID
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO execution_events (execution_id, event_type, worker_id, fencing_token, payload)
		VALUES ($1, $2, $3, $4, $5)
	`, executionID, eventType, workerArg, token, raw)
	return err
}
