package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// getTool loads a tool row by name.
func (p *Postgres) getTool(ctx context.Context, name string) (Tool, error) {
	var t Tool
	err := p.pool.QueryRow(ctx, `
		SELECT id::text, name, description, input_schema, max_attempts, lease_seconds, created_at
		FROM tools WHERE name = $1
	`, name).Scan(&t.ID, &t.Name, &t.Description, &t.InputSchema, &t.MaxAttempts, &t.LeaseSeconds, &t.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Tool{}, ErrUnknownTool
	}
	return t, err
}

// Submit implements the tools/call critical path: persist the call, dedupe on
// the idempotency key, and mark it ready for an executor to claim.
func (p *Postgres) Submit(ctx context.Context, namespace, toolName, idempotencyKey string, args json.RawMessage) (SubmitResult, error) {
	tool, err := p.getTool(ctx, toolName)
	if err != nil {
		return SubmitResult{}, err
	}
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return SubmitResult{}, err
	}
	defer rollback(ctx, tx)

	var id, status string
	var result []byte
	var inserted bool
	err = tx.QueryRow(ctx, `
		INSERT INTO executions (namespace, tool_name, idempotency_key, input_args, status, max_attempts)
		VALUES ($1, $2, $3, $4, 'pending', $5)
		ON CONFLICT (namespace, tool_name, idempotency_key)
		DO UPDATE SET updated_at = now()
		RETURNING id::text, status::text, result, (xmax = 0)
	`, namespace, toolName, idempotencyKey, args, tool.MaxAttempts).Scan(&id, &status, &result, &inserted)
	if err != nil {
		return SubmitResult{}, err
	}

	if !inserted {
		if err := appendEvent(ctx, tx, id, "duplicate_detected", "", nil, map[string]any{"idempotency_key": idempotencyKey}); err != nil {
			return SubmitResult{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return SubmitResult{}, err
		}
		res := SubmitResult{ExecutionID: id, Status: status, Duplicate: true}
		if status == "completed" {
			res.Result = result
		}
		return res, nil
	}

	if err := appendEvent(ctx, tx, id, "submitted", "", nil, map[string]any{"tool": toolName, "namespace": namespace}); err != nil {
		return SubmitResult{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE executions SET status = 'ready', updated_at = now() WHERE id = $1`, id); err != nil {
		return SubmitResult{}, err
	}
	if err := appendEvent(ctx, tx, id, "ready", "", nil, nil); err != nil {
		return SubmitResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return SubmitResult{}, err
	}
	return SubmitResult{ExecutionID: id, Status: "accepted"}, nil
}

// Claim atomically selects one ready execution and claims it with the next
// monotonic fencing token for that execution.
func (p *Postgres) Claim(ctx context.Context, workerID string, leaseSeconds int) (Claim, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return Claim{}, err
	}
	defer rollback(ctx, tx)

	var c Claim
	err = tx.QueryRow(ctx, `
		WITH ready AS (
			SELECT id FROM executions
			WHERE status = 'ready'
			ORDER BY created_at
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		UPDATE executions AS e
		SET status = 'running', attempts = attempts + 1, updated_at = now()
		FROM ready
		WHERE e.id = ready.id
		RETURNING e.id::text, e.tool_name, e.input_args, e.attempts, e.max_attempts
	`).Scan(&c.ExecutionID, &c.ToolName, &c.InputArgs, &c.Attempts, &c.MaxAttempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return Claim{}, ErrNoWork
	}
	if err != nil {
		return Claim{}, err
	}

	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(fencing_token), 0) + 1
		FROM execution_events WHERE execution_id = $1
	`, c.ExecutionID).Scan(&c.FencingToken); err != nil {
		return Claim{}, err
	}

	if err := tx.QueryRow(ctx, `
		INSERT INTO execution_leases (execution_id, worker_id, fencing_token, lease_expires)
		VALUES ($1, $2, $3, now() + make_interval(secs => $4))
		ON CONFLICT (execution_id) DO UPDATE
			SET worker_id = $2, fencing_token = $3,
			    lease_expires = now() + make_interval(secs => $4), claimed_at = now()
		RETURNING lease_expires
	`, c.ExecutionID, workerID, c.FencingToken, leaseSeconds).Scan(&c.LeaseExpires); err != nil {
		return Claim{}, err
	}

	if err := appendEvent(ctx, tx, c.ExecutionID, "claimed", workerID, &c.FencingToken, map[string]any{"attempt": c.Attempts}); err != nil {
		return Claim{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Claim{}, err
	}
	return c, nil
}

// Heartbeat extends the lease iff the fencing token still matches the active
// claim. Returns ErrStale when a newer worker has reclaimed the execution.
func (p *Postgres) Heartbeat(ctx context.Context, executionID string, token int64, leaseSeconds int) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(ctx, tx)

	var id string
	err = tx.QueryRow(ctx, `
		UPDATE execution_leases
		SET lease_expires = now() + make_interval(secs => $3)
		WHERE execution_id = $1 AND fencing_token = $2
		RETURNING execution_id::text
	`, executionID, token, leaseSeconds).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrStale
	}
	if err != nil {
		return err
	}
	if err := appendEvent(ctx, tx, executionID, "heartbeat", "", &token, nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Complete marks an execution completed iff the fencing token still owns the
// lease. A stale token records a stale_rejected event and returns ErrStale.
func (p *Postgres) Complete(ctx context.Context, executionID string, token int64, workerID string, result json.RawMessage) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(ctx, tx)

	var id string
	err = tx.QueryRow(ctx, `
		UPDATE executions
		SET status = 'completed', result = $3, error_message = NULL, updated_at = now()
		WHERE id = $1
		  AND EXISTS (SELECT 1 FROM execution_leases WHERE execution_id = $1 AND fencing_token = $2)
		RETURNING id::text
	`, executionID, token, result).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := appendEvent(ctx, tx, executionID, "stale_rejected", workerID, &token, map[string]any{"reason": "completion rejected: fencing token superseded"}); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		return ErrStale
	}
	if err != nil {
		return err
	}
	if err := appendEvent(ctx, tx, executionID, "completed", workerID, &token, nil); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM execution_leases WHERE execution_id = $1`, executionID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Fail records a failed attempt. When retries remain it schedules a retry with
// exponential backoff; otherwise it marks the execution permanently failed.
// The lease-token guard is part of the UPDATE itself, so a stale worker can
// never interleave between the validity check and the state mutation.
func (p *Postgres) Fail(ctx context.Context, executionID string, token int64, workerID, errMsg string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(ctx, tx)

	var attempts int
	var terminal bool
	err = tx.QueryRow(ctx, `
		UPDATE executions
		SET status = CASE WHEN attempts >= max_attempts THEN 'failed'::execution_status ELSE 'pending'::execution_status END,
		    error_message = $3,
		    updated_at = now()
		WHERE id = $1
		  AND EXISTS (
		    SELECT 1 FROM execution_leases l
		    WHERE l.execution_id = executions.id AND l.fencing_token = $2
		  )
		RETURNING executions.attempts, executions.status = 'failed'
	`, executionID, token, errMsg).Scan(&attempts, &terminal)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := appendEvent(ctx, tx, executionID, "stale_rejected", workerID, &token, map[string]any{"reason": "failure rejected: fencing token superseded"}); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		return ErrStale
	}
	if err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `DELETE FROM execution_leases WHERE execution_id = $1`, executionID); err != nil {
		return err
	}

	if terminal {
		if err := appendEvent(ctx, tx, executionID, "failed", workerID, &token, map[string]any{"error": errMsg, "attempt": attempts}); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}

	backoff := time.Duration(1<<uint(attempts)) * time.Second
	if _, err := tx.Exec(ctx, `
		INSERT INTO retry_schedule (execution_id, retry_at, attempt)
		VALUES ($1, now() + make_interval(secs => $2), $3)
		ON CONFLICT (execution_id) DO UPDATE SET retry_at = EXCLUDED.retry_at, attempt = EXCLUDED.attempt
	`, executionID, int(backoff.Seconds()), attempts); err != nil {
		return err
	}
	if err := appendEvent(ctx, tx, executionID, "retry_scheduled", workerID, &token, map[string]any{"error": errMsg, "attempt": attempts, "backoff_seconds": int(backoff.Seconds())}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// RunSchedulerPass expires stale leases and promotes due retries. It returns
// the number of executions whose state changed.
func (p *Postgres) RunSchedulerPass(ctx context.Context) (int, error) {
	changed := 0

	type expired struct {
		id          string
		attempts    int
		maxAttempts int
	}
	rows, err := p.pool.Query(ctx, `
		SELECT e.id::text, e.attempts, e.max_attempts
		FROM executions e
		JOIN execution_leases l ON l.execution_id = e.id
		WHERE e.status = 'running' AND l.lease_expires < now()
	`)
	if err != nil {
		return changed, err
	}
	var stale []expired
	for rows.Next() {
		var ex expired
		if err := rows.Scan(&ex.id, &ex.attempts, &ex.maxAttempts); err != nil {
			rows.Close()
			return changed, err
		}
		stale = append(stale, ex)
	}
	rows.Close()

	for _, ex := range stale {
		if err := p.expireLease(ctx, ex.id, ex.attempts, ex.maxAttempts); err != nil {
			return changed, err
		}
		changed++
	}

	dueRows, err := p.pool.Query(ctx, `SELECT execution_id::text FROM retry_schedule WHERE retry_at <= now()`)
	if err != nil {
		return changed, err
	}
	var due []string
	for dueRows.Next() {
		var id string
		if err := dueRows.Scan(&id); err != nil {
			dueRows.Close()
			return changed, err
		}
		due = append(due, id)
	}
	dueRows.Close()

	for _, id := range due {
		if err := p.promoteRetry(ctx, id); err != nil {
			return changed, err
		}
		changed++
	}

	return changed, nil
}

func (p *Postgres) expireLease(ctx context.Context, id string, attempts, maxAttempts int) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(ctx, tx)

	if err := appendEvent(ctx, tx, id, "lease_expired", "", nil, map[string]any{"attempt": attempts}); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM execution_leases WHERE execution_id = $1`, id); err != nil {
		return err
	}
	if attempts >= maxAttempts {
		if _, err := tx.Exec(ctx, `UPDATE executions SET status = 'failed', error_message = 'lease expired after max attempts', updated_at = now() WHERE id = $1`, id); err != nil {
			return err
		}
		if err := appendEvent(ctx, tx, id, "failed", "", nil, map[string]any{"reason": "lease expired after max attempts"}); err != nil {
			return err
		}
	} else if _, err := tx.Exec(ctx, `UPDATE executions SET status = 'ready', updated_at = now() WHERE id = $1`, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (p *Postgres) promoteRetry(ctx context.Context, id string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollback(ctx, tx)

	if _, err := tx.Exec(ctx, `UPDATE executions SET status = 'ready', updated_at = now() WHERE id = $1`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM retry_schedule WHERE execution_id = $1`, id); err != nil {
		return err
	}
	if err := appendEvent(ctx, tx, id, "ready", "", nil, map[string]any{"reason": "retry promoted"}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
