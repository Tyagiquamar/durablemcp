package executor_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tyagiquamar/durablemcp/internal/store"
	"github.com/tyagiquamar/durablemcp/internal/testdb"
)

// TestWorkerKillFencingRecovery is the automated equivalent of
// scripts/fencing-demo.sh: a real executor subprocess claims long-running
// work, dies mid-execution, the lease expires and is reclaimed by another
// worker, and the dead worker's late completion is rejected by the fencing
// contract instead of silently overwriting the winner's result.
func TestWorkerKillFencingRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("process-kill failure scene requires PostgreSQL; run make test-pg")
	}

	url := testdb.Shared(t)
	ctx := context.Background()

	p, err := store.NewPostgres(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(p.Close)
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)

	reset(t, pool)
	if err := p.SeedTools(ctx, []store.Tool{{
		Name:        "slow_compute",
		Description: "kill-scene tool",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		MaxAttempts: 3,
	}}); err != nil {
		t.Fatal(err)
	}

	sub, err := p.Submit(ctx, "fencing-test", "slow_compute", "kill-case", json.RawMessage(`{"seconds": 25}`))
	if err != nil {
		t.Fatal(err)
	}
	id := sub.ExecutionID

	bin := buildExecutor(t)

	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(),
		"DATABASE_URL="+url,
		"WORKER_ID=worker-doomed",
		"LEASE_SECONDS=2",
		"POLL_INTERVAL_MS=100",
		"LOG_LEVEL=error",
	)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start executor subprocess: %v", err)
	}

	waitForStatus(t, pool, id, "running", 15*time.Second)

	if err := cmd.Process.Kill(); err != nil { // SIGKILL-equivalent: no cleanup, mid-heartbeat death
		t.Fatalf("kill executor: %v", err)
	}
	_, _ = cmd.Process.Wait()

	// The dead worker leaves a lease that will expire; force the expiry rather
	// than sleeping through it, then let the reaper return the work to ready.
	if _, err := pool.Exec(ctx,
		`UPDATE execution_leases SET lease_expires = now() - interval '1 second' WHERE execution_id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := p.RunSchedulerPass(ctx); err != nil {
		t.Fatalf("scheduler pass: %v", err)
	}
	if got := statusOf(t, pool, id); got != "ready" {
		t.Fatalf("status after reaper = %q, want ready", got)
	}

	b, err := p.Claim(ctx, "worker-heir", 30)
	if err != nil {
		t.Fatalf("reclaim: %v", err)
	}
	if b.FencingToken != 2 || b.Attempts != 2 {
		t.Fatalf("reclaim token/attempts = %d/%d, want 2/2", b.FencingToken, b.Attempts)
	}

	// The doomed worker comes back from the dead and reports completion with
	// its old token. Must be rejected; the heir's result must win.
	stale := json.RawMessage(`{"from": "worker-doomed"}`)
	if err := p.Complete(ctx, id, 1, "worker-doomed", stale); !errors.Is(err, store.ErrStale) {
		t.Fatalf("stale completion err = %v, want ErrStale", err)
	}

	winner := json.RawMessage(`{"from": "worker-heir"}`)
	if err := p.Complete(ctx, id, b.FencingToken, "worker-heir", winner); err != nil {
		t.Fatalf("heir completion rejected: %v", err)
	}

	events, err := p.ListEvents(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	types := make([]string, 0, len(events))
	for _, e := range events {
		types = append(types, e.EventType)
	}
	for _, want := range []string{"submitted", "ready", "claimed", "lease_expired", "stale_rejected", "completed"} {
		if !slices.Contains(types, want) {
			t.Fatalf("event arc missing %q; got %v", want, types)
		}
	}

	ex, err := p.GetExecution(ctx, id)
	if err != nil || ex == nil {
		t.Fatal(err)
	}
	var result struct {
		From string `json:"from"`
	}
	if err := json.Unmarshal(ex.Result, &result); err != nil {
		t.Fatal(err)
	}
	if result.From != "worker-heir" {
		t.Fatalf("final result authored by %q — fencing contract violated", result.From)
	}
}

func buildExecutor(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "durablemcp-executor.exe")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/executor")
	cmd.Dir = repoRoot(t)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build executor binary: %v\n%s", err, out)
	}
	return bin
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// internal/executor -> repo root
	return filepath.Join(wd, "..", "..")
}

func reset(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`TRUNCATE execution_events, execution_leases, retry_schedule, executions RESTART IDENTITY`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `DELETE FROM tools`); err != nil {
		t.Fatal(err)
	}
}

func statusOf(t *testing.T, pool *pgxpool.Pool, id string) string {
	t.Helper()
	var s string
	if err := pool.QueryRow(context.Background(),
		`SELECT status::text FROM executions WHERE id = $1`, id).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func waitForStatus(t *testing.T, pool *pgxpool.Pool, id, want string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if statusOf(t, pool, id) == want {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("execution %s never reached %q within %s (last=%q)", id, want, timeout, statusOf(t, pool, id))
}
