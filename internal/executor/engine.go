package executor

import (
	"context"
	"encoding/json"
	"errors"
	"log"
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
	Logger       *log.Logger
}

// Run drives the executor poll loop until the context is cancelled.
func (r *Runtime) Run(ctx context.Context) error {
	if r.PollInterval <= 0 {
		r.PollInterval = 500 * time.Millisecond
	}
	if r.LeaseSeconds <= 0 {
		r.LeaseSeconds = 30
	}
	if r.Logger == nil {
		r.Logger = log.Default()
	}
	r.Logger.Printf("executor %s started (poll=%s lease=%ds)", r.WorkerID, r.PollInterval, r.LeaseSeconds)

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
			r.Logger.Printf("claim error: %v", err)
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
			r.Logger.Printf("execution %s aborted (lease lost): %v", claim.ExecutionID, err)
			return
		}
		if failErr := r.Store.Fail(ctx, claim.ExecutionID, claim.FencingToken, r.WorkerID, err.Error()); failErr != nil {
			r.Logger.Printf("fail error for %s: %v", claim.ExecutionID, failErr)
		}
		return
	}

	if compErr := r.Store.Complete(ctx, claim.ExecutionID, claim.FencingToken, r.WorkerID, result); compErr != nil {
		if errors.Is(compErr, store.ErrStale) {
			r.Logger.Printf("execution %s completion rejected: stale fencing token %d", claim.ExecutionID, claim.FencingToken)
			return
		}
		r.Logger.Printf("complete error for %s: %v", claim.ExecutionID, compErr)
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
			err := r.Store.Heartbeat(context.Background(), claim.ExecutionID, claim.FencingToken, r.LeaseSeconds)
			if errors.Is(err, store.ErrStale) {
				r.Logger.Printf("execution %s lost lease (token %d superseded), aborting", claim.ExecutionID, claim.FencingToken)
				cancel()
				return
			}
			if err != nil {
				r.Logger.Printf("heartbeat error for %s: %v", claim.ExecutionID, err)
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
