package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// SeedTools registers the given tools if they are not already present.
func (p *Postgres) SeedTools(ctx context.Context, tools []Tool) error {
	for _, t := range tools {
		schema := t.InputSchema
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object"}`)
		}
		if _, err := p.pool.Exec(ctx, `
			INSERT INTO tools (name, description, input_schema, max_attempts, lease_seconds)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (name) DO NOTHING
		`, t.Name, t.Description, schema, t.MaxAttempts, t.LeaseSeconds); err != nil {
			return err
		}
	}
	return nil
}

// Stats returns aggregate counts for the dashboard overview.
func (p *Postgres) Stats(ctx context.Context) (Stats, error) {
	var s Stats
	err := p.pool.QueryRow(ctx, `
		SELECT
			count(*),
			count(*) FILTER (WHERE status = 'ready'),
			count(*) FILTER (WHERE status = 'running'),
			count(*) FILTER (WHERE status = 'completed'),
			count(*) FILTER (WHERE status = 'failed')
		FROM executions
	`).Scan(&s.Total, &s.Ready, &s.Running, &s.Completed, &s.Failed)
	if err != nil {
		return Stats{}, err
	}
	if err := p.pool.QueryRow(ctx, `SELECT count(*) FROM execution_leases WHERE lease_expires > now()`).Scan(&s.ActiveLeases); err != nil {
		return Stats{}, err
	}
	return s, nil
}

// ListTools returns every registered tool.
func (p *Postgres) ListTools(ctx context.Context) ([]Tool, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT id::text, name, description, input_schema, max_attempts, lease_seconds, created_at
		FROM tools ORDER BY name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tools := []Tool{}
	for rows.Next() {
		var t Tool
		if err := rows.Scan(&t.ID, &t.Name, &t.Description, &t.InputSchema, &t.MaxAttempts, &t.LeaseSeconds, &t.CreatedAt); err != nil {
			return nil, err
		}
		tools = append(tools, t)
	}
	return tools, rows.Err()
}

// ListExecutions returns a filtered, paginated execution list with lease data.
func (p *Postgres) ListExecutions(ctx context.Context, status, tool string, limit, offset int) ([]Execution, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := p.pool.Query(ctx, `
		SELECT e.id::text, e.namespace, e.tool_name, e.idempotency_key, e.input_args,
		       e.status::text, e.attempts, e.max_attempts, e.result, e.error_message,
		       e.created_at, e.updated_at,
		       l.worker_id, l.fencing_token, l.lease_expires, l.claimed_at
		FROM executions e
		LEFT JOIN execution_leases l ON l.execution_id = e.id
		WHERE ($1 = '' OR e.status::text = $1)
		  AND ($2 = '' OR e.tool_name = $2)
		ORDER BY e.created_at DESC
		LIMIT $3 OFFSET $4
	`, status, tool, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := []Execution{}
	for rows.Next() {
		ex, err := scanExecution(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, ex)
	}
	return list, rows.Err()
}

// GetExecution returns one execution with its active lease, if any.
func (p *Postgres) GetExecution(ctx context.Context, id string) (*Execution, error) {
	row := p.pool.QueryRow(ctx, `
		SELECT e.id::text, e.namespace, e.tool_name, e.idempotency_key, e.input_args,
		       e.status::text, e.attempts, e.max_attempts, e.result, e.error_message,
		       e.created_at, e.updated_at,
		       l.worker_id, l.fencing_token, l.lease_expires, l.claimed_at
		FROM executions e
		LEFT JOIN execution_leases l ON l.execution_id = e.id
		WHERE e.id = $1
	`, id)
	ex, err := scanExecution(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ex, nil
}

// ListEvents returns the ordered event history for one execution.
func (p *Postgres) ListEvents(ctx context.Context, executionID string) ([]Event, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT id, execution_id::text, event_type::text, worker_id, fencing_token, payload, occurred_at
		FROM execution_events
		WHERE execution_id = $1
		ORDER BY occurred_at, id
	`, executionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := []Event{}
	for rows.Next() {
		var e Event
		var worker *string
		if err := rows.Scan(&e.ID, &e.ExecutionID, &e.EventType, &worker, &e.FencingToken, &e.Payload, &e.OccurredAt); err != nil {
			return nil, err
		}
		if worker != nil {
			e.WorkerID = *worker
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// ListWorkers derives active workers from current leases.
func (p *Postgres) ListWorkers(ctx context.Context) ([]WorkerInfo, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT worker_id, count(*), EXTRACT(EPOCH FROM (now() - min(claimed_at)))
		FROM execution_leases
		WHERE lease_expires > now()
		GROUP BY worker_id
		ORDER BY worker_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	workers := []WorkerInfo{}
	for rows.Next() {
		var w WorkerInfo
		if err := rows.Scan(&w.WorkerID, &w.Claimed, &w.OldestClaimAge); err != nil {
			return nil, err
		}
		workers = append(workers, w)
	}
	return workers, rows.Err()
}

// scanExecution reads an execution row (with optional joined lease columns).
func scanExecution(row pgx.Row) (Execution, error) {
	var ex Execution
	var result []byte
	var errMsg *string
	var workerID *string
	var fencingToken *int64
	var leaseExpires, claimedAt *time.Time
	if err := row.Scan(
		&ex.ID, &ex.Namespace, &ex.ToolName, &ex.IdempotencyKey, &ex.InputArgs,
		&ex.Status, &ex.Attempts, &ex.MaxAttempts, &result, &errMsg,
		&ex.CreatedAt, &ex.UpdatedAt,
		&workerID, &fencingToken, &leaseExpires, &claimedAt,
	); err != nil {
		return Execution{}, err
	}
	if len(result) > 0 {
		ex.Result = result
	}
	if errMsg != nil {
		ex.ErrorMessage = *errMsg
	}
	if workerID != nil && fencingToken != nil {
		ex.Lease = &Lease{
			ExecutionID:  ex.ID,
			WorkerID:     *workerID,
			FencingToken: *fencingToken,
		}
		if leaseExpires != nil {
			ex.Lease.LeaseExpires = *leaseExpires
		}
		if claimedAt != nil {
			ex.Lease.ClaimedAt = *claimedAt
		}
	}
	return ex, nil
}
