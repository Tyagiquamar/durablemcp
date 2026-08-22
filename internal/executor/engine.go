package executor

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/tyagiquamar/durablemcp/internal/store"
)

// Handler runs a tool. Returning an error marks the attempt failed; the engine
// schedules a retry until the tool's max_attempts is exhausted.
type Handler func(ctx context.Context, args json.RawMessage) (json.RawMessage, error)

// Runtime polls for ready executions, claims them with a fencing token,
// heartbeats while working, and reports the outcome under the fencing contract.
type Runtime struct {
	Store        *store.Postgres
	WorkerID     string
	Handlers     map[string]Handler
	PollInterval time.Duration
	LeaseSeconds int
	// HeartbeatTimeout bounds each individual heartbeat round-trip so a hung
	// database connection cannot wedge the heartbeat goroutine forever.
	HeartbeatTimeout time.Duration
	Logger           *slog.Logger
}

// Run drives the executor poll loop until the context is cancelled.
func (r *Runtime) Run(ctx context.Context) error {
	if r.PollInterval <= 0 {
		r.PollInterval = 500 * time.Millisecond
	}
	if r.LeaseSeconds <= 0 {
		r.LeaseSeconds = 30
	}
	if r.HeartbeatTimeout <= 0 {
		r.HeartbeatTimeout = 10 * time.Second
	}
	if r.Logger == nil {
		r.Logger = slog.Default()
	}
	r.Logger.Info("executor started", "worker", r.WorkerID, "poll", r.PollInterval, "lease_seconds", r.LeaseSeconds)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		claim, err := r.Store.Claim(ctx, r.WorkerID, r.LeaseSeconds)
		if errors.Is(err, store.ErrNoWork) {
			if !sleep(ctx, r.PollInterval) {
				return ctx.Err()
			}
			continue
		}
		if err != nil {
			r.Logger.Error("claim failed", "err", err)
			if !sleep(ctx, r.PollInterval) {
				return ctx.Err()
			}
			continue
		}
		r.execute(ctx, claim)
	}
}

// execute runs one claimed execution with a background heartbeat.
func (r *Runtime) execute(ctx context.Context, claim store.Claim) {
	handler, ok := r.Handlers[claim.ToolName]
	if !ok {
		_ = r.Store.Fail(ctx, claim.ExecutionID, claim.FencingToken, r.WorkerID, "no handler registered for tool "+claim.ToolName)
		return
	}

	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go r.heartbeat(workCtx, cancel, claim)

	result, err := handler(workCtx, claim.InputArgs)
	if err != nil {
		if ctxErr := workCtx.Err(); ctxErr != nil {
			r.Logger.Warn("execution aborted: lease lost mid-run", "execution_id", claim.ExecutionID, "handler_err", err)
			return
		}
		if failErr := r.Store.Fail(ctx, claim.ExecutionID, claim.FencingToken, r.WorkerID, err.Error()); failErr != nil {
			r.Logger.Error("fail report rejected", "execution_id", claim.ExecutionID, "err", failErr)
		}
		return
	}

	if compErr := r.Store.Complete(ctx, claim.ExecutionID, claim.FencingToken, r.WorkerID, result); compErr != nil {
		if errors.Is(compErr, store.ErrStale) {
			r.Logger.Warn("completion rejected: stale fencing token",
				"execution_id", claim.ExecutionID, "fencing_token", claim.FencingToken)
			return
		}
		r.Logger.Error("complete failed", "execution_id", claim.ExecutionID, "err", compErr)
	}
}

// heartbeat extends the lease periodically and cancels the work context if the
// fencing token is superseded by a newer claim.
func (r *Runtime) heartbeat(ctx context.Context, cancel context.CancelFunc, claim store.Claim) {
	interval := time.Duration(r.LeaseSeconds) * time.Second / 3
	if interval <= 0 {
		interval = 10 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			hbCtx, cancelHB := context.WithTimeout(context.Background(), r.HeartbeatTimeout)
			err := r.Store.Heartbeat(hbCtx, claim.ExecutionID, claim.FencingToken, r.LeaseSeconds)
			cancelHB()
			switch {
			case errors.Is(err, store.ErrStale):
				r.Logger.Warn("lease lost: token superseded, aborting work",
					"execution_id", claim.ExecutionID, "fencing_token", claim.FencingToken)
				cancel()
				return
			case err != nil:
				r.Logger.Error("heartbeat failed", "execution_id", claim.ExecutionID, "err", err)
			}
		}
	}
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
