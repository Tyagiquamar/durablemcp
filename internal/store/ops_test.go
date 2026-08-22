package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"slices"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tyagiquamar/durablemcp/internal/store"
	"github.com/tyagiquamar/durablemcp/internal/testdb"
)

// jsonEqual compares JSON payloads semantically: jsonb storage re-serializes
// whitespace, so byte equality would be flaky.
func jsonEqual(a, b json.RawMessage) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return string(a) == string(b)
	}
	return reflect.DeepEqual(x, y)
}

func TestMain(m *testing.M) {
	code := m.Run()
	testdb.Shutdown()
	os.Exit(code)
}

// harness wraps a store plus a raw pool for fixture manipulation.
type harness struct {
	t    *testing.T
	ctx  context.Context
	p    *store.Postgres
	pool *pgxpool.Pool
}

func newHarness(t *testing.T) *harness {
	t.Helper()
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

	h := &harness{t: t, ctx: ctx, p: p, pool: pool}
	h.reset()
	return h
}

func (h *harness) reset() {
	h.t.Helper()
	if _, err := h.pool.Exec(h.ctx, `TRUNCATE execution_events, execution_leases, retry_schedule, executions RESTART IDENTITY`); err != nil {
		h.t.Fatalf("truncate: %v", err)
	}
	if _, err := h.pool.Exec(h.ctx, `DELETE FROM tools`); err != nil {
		h.t.Fatalf("clear tools: %v", err)
	}
}

func (h *harness) seedTool(name string, maxAttempts int) {
	h.t.Helper()
	err := h.p.SeedTools(h.ctx, []store.Tool{{
		Name:         name,
		Description:  "test tool " + name,
		InputSchema:  json.RawMessage(`{"type":"object"}`),
		MaxAttempts:  maxAttempts,
		LeaseSeconds: 30,
	}})
	if err != nil {
		h.t.Fatalf("seed tool %s: %v", name, err)
	}
}

func (h *harness) mustSubmit(tool, key string) store.SubmitResult {
	h.t.Helper()
	res, err := h.p.Submit(h.ctx, "test-ns", tool, key, json.RawMessage(`{"k":"v"}`))
	if err != nil {
		h.t.Fatalf("submit %s/%s: %v", tool, key, err)
	}
	return res
}

func (h *harness) mustClaim(worker string) store.Claim {
	h.t.Helper()
	c, err := h.p.Claim(h.ctx, worker, 30)
	if err != nil {
		h.t.Fatalf("claim by %s: %v", worker, err)
	}
	return c
}

func (h *harness) mustFailNoClaim() {
	h.t.Helper()
	if _, err := h.p.Claim(h.ctx, "idle-worker", 30); !errors.Is(err, store.ErrNoWork) {
		h.t.Fatalf("expected ErrNoWork, got %v", err)
	}
}

func (h *harness) eventTypes(id string) []string {
	h.t.Helper()
	events, err := h.p.ListEvents(h.ctx, id)
	if err != nil {
		h.t.Fatalf("events for %s: %v", id, err)
	}
	types := make([]string, 0, len(events))
	for _, e := range events {
		types = append(types, e.EventType)
	}
	return types
}

func (h *harness) requireEvents(id string, want ...string) {
	h.t.Helper()
	got := h.eventTypes(id)
	for _, w := range want {
		if !slices.Contains(got, w) {
			h.t.Fatalf("event log for %s missing %q; got %v", id, w, got)
		}
	}
}

func (h *harness) status(id string) string {
	h.t.Helper()
	ex, err := h.p.GetExecution(h.ctx, id)
	if err != nil || ex == nil {
		h.t.Fatalf("get execution %s: %v (nil=%v)", id, err, ex == nil)
	}
	return ex.Status
}

func (h *harness) expireActiveLease(id string) {
	h.t.Helper()
	if _, err := h.pool.Exec(h.ctx,
		`UPDATE execution_leases SET lease_expires = now() - interval '1 minute' WHERE execution_id = $1`, id); err != nil {
		h.t.Fatalf("force-expire lease: %v", err)
	}
	if _, err := h.pool.Exec(h.ctx,
		`UPDATE retry_schedule SET retry_at = now() - interval '1 minute' WHERE execution_id = $1`, id); err != nil {
		h.t.Fatalf("force-due retry: %v", err)
	}
}

func TestSubmitPersistsBeforeDispatch(t *testing.T) {
	h := newHarness(t)
	h.seedTool("slow_compute", 3)

	res := h.mustSubmit("slow_compute", "order-abc")
	if res.Duplicate {
		t.Fatal("first submit must not be a duplicate")
	}
	if got := h.status(res.ExecutionID); got != "ready" {
		t.Fatalf("status after submit = %q, want ready", got)
	}
	h.requireEvents(res.ExecutionID, "submitted", "ready")

	ex, err := h.p.GetExecution(h.ctx, res.ExecutionID)
	if err != nil || ex == nil {
		t.Fatalf("load execution: %v", err)
	}
	if ex.InputArgs == nil || !jsonEqual(ex.InputArgs, json.RawMessage(`{"k":"v"}`)) {
		t.Fatalf("input args not persisted verbatim: %s", ex.InputArgs)
	}
	if ex.IdempotencyKey != "order-abc" || ex.Namespace != "test-ns" {
		t.Fatalf("idempotency tuple wrong: ns=%q key=%q", ex.Namespace, ex.IdempotencyKey)
	}
}

func TestSubmitUnknownToolRejected(t *testing.T) {
	h := newHarness(t)
	if _, err := h.p.Submit(h.ctx, "ns", "nope", "key-1", json.RawMessage(`{}`)); !errors.Is(err, store.ErrUnknownTool) {
		t.Fatalf("want ErrUnknownTool, got %v", err)
	}
}

func TestDuplicateSubmitWhileRunningReturnsSameExecution(t *testing.T) {
	h := newHarness(t)
	h.seedTool("send_webhook", 3)

	first := h.mustSubmit("send_webhook", "order-dup")
	c := h.mustClaim("worker-a") // move it to running

	second := h.mustSubmit("send_webhook", "order-dup")
	if !second.Duplicate {
		t.Fatal("second submit while running must be flagged duplicate")
	}
	if second.ExecutionID != first.ExecutionID || second.ExecutionID != c.ExecutionID {
		t.Fatalf("duplicate returned different execution: first=%s second=%s claim=%s",
			first.ExecutionID, second.ExecutionID, c.ExecutionID)
	}
	if second.Result != nil {
		t.Fatal("running execution must not expose a result yet")
	}

	var n int
	if err := h.pool.QueryRow(h.ctx,
		`SELECT count(*) FROM executions WHERE namespace='test-ns' AND tool_name='send_webhook' AND idempotency_key='order-dup'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("duplicate submit created rows: count=%d want 1", n)
	}
	h.requireEvents(first.ExecutionID, "duplicate_detected")
}

func TestCompletedExecutionReplaysCachedResult(t *testing.T) {
	h := newHarness(t)
	h.seedTool("call_api", 3)

	first := h.mustSubmit("call_api", "order-cache")
	h.mustClaim("worker-a")
	result := json.RawMessage(`{"body":"cached-payload"}`)
	if err := h.p.Complete(h.ctx, first.ExecutionID, 1, "worker-a", result); err != nil {
		t.Fatalf("complete: %v", err)
	}

	replay := h.mustSubmit("call_api", "order-cache")
	if !replay.Duplicate || replay.Status != "completed" {
		t.Fatalf("replay status=%q duplicate=%v, want completed/true", replay.Status, replay.Duplicate)
	}
	if !jsonEqual(replay.Result, result) {
		t.Fatalf("cached result mismatch: got %s want %s", replay.Result, result)
	}
	h.mustFailNoClaim() // completed work never returns to the ready queue
}

func TestConcurrentClaimsNeverDoubleAssign(t *testing.T) {
	h := newHarness(t)
	h.seedTool("write_file", 3)

	const executions = 6
	const racers = 24
	ids := make([]string, 0, executions)
	for i := 0; i < executions; i++ {
		ids = append(ids, h.mustSubmit("write_file", "key-"+string(rune('a'+i))).ExecutionID)
	}

	var mu sync.Mutex
	var success int
	claimedBy := map[string]string{}
	var wg sync.WaitGroup
	for w := 0; w < racers; w++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			worker := "racer-" + string(rune('A'+n%26))
			c, err := h.p.Claim(h.ctx, worker, 30)
			if errors.Is(err, store.ErrNoWork) {
				return
			}
			if err != nil {
				t.Errorf("claim error: %v", err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			success++
			if prev, ok := claimedBy[c.ExecutionID]; ok {
				t.Errorf("execution %s claimed twice (%s and %s)", c.ExecutionID, prev, worker)
			}
			claimedBy[c.ExecutionID] = worker
			if c.FencingToken != 1 || c.Attempts != 1 {
				t.Errorf("fresh claim token/attempts = %d/%d, want 1/1", c.FencingToken, c.Attempts)
			}
		}(w)
	}
	wg.Wait()

	if success != executions {
		t.Fatalf("successful claims = %d, want %d", success, executions)
	}
}

func TestStaleHeartbeatAndCompleteAfterReclaim(t *testing.T) {
	h := newHarness(t)
	h.seedTool("slow_compute", 3)

	id := h.mustSubmit("slow_compute", "stale-case").ExecutionID

	a := h.mustClaim("worker-a") // token 1
	if a.FencingToken != 1 {
		t.Fatalf("first fencing token = %d, want 1", a.FencingToken)
	}

	h.expireActiveLease(id)
	if _, err := h.p.RunSchedulerPass(h.ctx); err != nil {
		t.Fatalf("scheduler pass: %v", err)
	}
	if got := h.status(id); got != "ready" {
		t.Fatalf("status after lease expiry = %q, want ready", got)
	}
	h.requireEvents(id, "lease_expired")

	b := h.mustClaim("worker-b") // token 2
	if b.FencingToken != 2 {
		t.Fatalf("second fencing token = %d, want 2 (monotonic)", b.FencingToken)
	}
	if b.Attempts != 2 {
		t.Fatalf("attempts after reclaim = %d, want 2", b.Attempts)
	}

	if err := h.p.Heartbeat(h.ctx, id, a.FencingToken, 30); !errors.Is(err, store.ErrStale) {
		t.Fatalf("stale heartbeat err = %v, want ErrStale", err)
	}
	staleResult := json.RawMessage(`{"from":"dead-worker-a"}`)
	if err := h.p.Complete(h.ctx, id, a.FencingToken, "worker-a", staleResult); !errors.Is(err, store.ErrStale) {
		t.Fatalf("stale complete err = %v, want ErrStale", err)
	}
	if err := h.p.Fail(h.ctx, id, a.FencingToken, "worker-a", "late failure"); !errors.Is(err, store.ErrStale) {
		t.Fatalf("stale fail err = %v, want ErrStale", err)
	}
	if got := h.status(id); got != "running" {
		t.Fatalf("state mutated by stale worker: status=%q, want running", got)
	}
	h.requireEvents(id, "stale_rejected")

	good := json.RawMessage(`{"from":"worker-b"}`)
	if err := h.p.Complete(h.ctx, id, b.FencingToken, "worker-b", good); err != nil {
		t.Fatalf("valid completion rejected: %v", err)
	}
	if got := h.status(id); got != "completed" {
		t.Fatalf("final status = %q, want completed", got)
	}
	ex, _ := h.p.GetExecution(h.ctx, id)
	if !jsonEqual(ex.Result, good) {
		t.Fatalf("stored result = %s, want winner's %s", ex.Result, good)
	}
}

func TestHeartbeatExtendsMatchingLeaseOnly(t *testing.T) {
	h := newHarness(t)
	h.seedTool("slow_compute", 3)

	id := h.mustSubmit("slow_compute", "hb-ok").ExecutionID
	c := h.mustClaim("worker-hb")

	before, err := h.p.GetExecution(h.ctx, id)
	if err != nil || before.Lease == nil {
		t.Fatalf("lease missing after claim: %v", err)
	}
	if err := h.p.Heartbeat(h.ctx, id, c.FencingToken, 30); err != nil {
		t.Fatalf("heartbeat with live token failed: %v", err)
	}
	after, _ := h.p.GetExecution(h.ctx, id)
	if !after.Lease.LeaseExpires.After(before.Lease.LeaseExpires) {
		t.Fatalf("heartbeat did not extend lease: before=%v after=%v", before.Lease.LeaseExpires, after.Lease.LeaseExpires)
	}
	h.requireEvents(id, "heartbeat")
}

func TestRetryExhaustionArc(t *testing.T) {
	h := newHarness(t)
	h.seedTool("flaky", 3)

	id := h.mustSubmit("flaky", "retry-case").ExecutionID
	wantBackoffs := []int{2, 4} // attempts=1 -> 2s, attempts=2 -> 4s

	for attempt := 1; attempt <= 2; attempt++ {
		c := h.mustClaim("worker-retry")
		if c.Attempts != attempt {
			t.Fatalf("claim attempts = %d, want %d", c.Attempts, attempt)
		}
		if err := h.p.Fail(h.ctx, id, c.FencingToken, "worker-retry", "boom"); err != nil {
			t.Fatalf("fail on attempt %d: %v", attempt, err)
		}
		if got := h.status(id); got != "pending" {
			t.Fatalf("status after retryable failure = %q, want pending", got)
		}

		var backoff int
		if err := h.pool.QueryRow(h.ctx,
			`SELECT EXTRACT(EPOCH FROM (retry_at - now()))::int FROM retry_schedule WHERE execution_id = $1`, id).Scan(&backoff); err != nil {
			t.Fatalf("retry row missing after failure %d: %v", attempt, err)
		}
		if backoff > wantBackoffs[attempt-1] {
			t.Fatalf("backoff %ds exceeds expected %ds", backoff, wantBackoffs[attempt-1])
		}

		h.expireActiveLease(id) // pull retry_at into the past
		if _, err := h.p.RunSchedulerPass(h.ctx); err != nil {
			t.Fatalf("promote retry: %v", err)
		}
		if got := h.status(id); got != "ready" {
			t.Fatalf("retry not promoted to ready: %q", got)
		}
	}

	final := h.mustClaim("worker-retry")
	if final.Attempts != 3 {
		t.Fatalf("final claim attempts = %d, want 3", final.Attempts)
	}
	if err := h.p.Fail(h.ctx, id, final.FencingToken, "worker-retry", "terminal boom"); err != nil {
		t.Fatalf("final fail: %v", err)
	}
	if got := h.status(id); got != "failed" {
		t.Fatalf("exhausted status = %q, want failed", got)
	}
	ex, _ := h.p.GetExecution(h.ctx, id)
	if ex.ErrorMessage != "terminal boom" {
		t.Fatalf("error_message = %q, want terminal boom", ex.ErrorMessage)
	}
	h.mustFailNoClaim()

	got := h.eventTypes(id)
	retries := 0
	for _, e := range got {
		if e == "retry_scheduled" {
			retries++
		}
	}
	if retries != 2 {
		t.Fatalf("retry_scheduled events = %d, want 2 (arc: %v)", retries, got)
	}
	h.requireEvents(id, "failed")
}

func TestSchedulerExpiresRunningLeaseWithoutHeartbeat(t *testing.T) {
	h := newHarness(t)
	h.seedTool("slow_compute", 5)

	id := h.mustSubmit("slow_compute", "expiry-case").ExecutionID
	h.mustClaim("worker-gone")
	h.expireActiveLease(id)

	n, err := h.p.RunSchedulerPass(h.ctx)
	if err != nil {
		t.Fatalf("scheduler pass: %v", err)
	}
	if n < 1 {
		t.Fatalf("scheduler reported %d changes, want >=1", n)
	}
	if got := h.status(id); got != "ready" {
		t.Fatalf("expired lease left status=%q, want ready", got)
	}

	next := h.mustClaim("worker-next")
	if next.FencingToken != 2 {
		t.Fatalf("reclaimed token = %d, want 2", next.FencingToken)
	}
}

func TestLeaseExpiryAtMaxAttemptsFailsTerminal(t *testing.T) {
	h := newHarness(t)
	h.seedTool("cursed", 1)

	id := h.mustSubmit("cursed", "max-expiry").ExecutionID
	h.mustClaim("worker-gone")
	h.expireActiveLease(id)

	if _, err := h.p.RunSchedulerPass(h.ctx); err != nil {
		t.Fatal(err)
	}
	if got := h.status(id); got != "failed" {
		t.Fatalf("expired at max_attempts status = %q, want failed", got)
	}
	h.mustFailNoClaim()
	h.requireEvents(id, "lease_expired", "failed")
}

func TestStatsReflectLifecycle(t *testing.T) {
	h := newHarness(t)
	h.seedTool("slow_compute", 3)

	done := h.mustSubmit("slow_compute", "stats-done").ExecutionID
	h.mustSubmit("slow_compute", "stats-run")
	h.mustSubmit("slow_compute", "stats-ready")

	h.mustClaim("w1") // done -> running
	h.mustClaim("w1") // run  -> running
	if err := h.p.Complete(h.ctx, done, 1, "w1", json.RawMessage(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}

	s, err := h.p.Stats(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if s.Total != 3 || s.Completed != 1 || s.Running != 1 || s.Ready != 1 {
		t.Fatalf("stats mismatch: total=%d completed=%d running=%d ready=%d, want 3/1/1/1",
			s.Total, s.Completed, s.Running, s.Ready)
	}
	if s.ActiveLeases != 1 {
		t.Fatalf("active leases = %d, want 1 (the still-running execution)", s.ActiveLeases)
	}
}
