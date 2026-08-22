package testdb

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

const (
	image         = "postgres:17-alpine"
	migrationFile = "001_init.sql"
)

// Start boots a throwaway PostgreSQL with the DurableMCP schema applied, or
// reuses DURABLEMCP_TEST_DATABASE_URL when set. The returned stop func releases
// the container (a no-op for the external URL).
func Start(ctx context.Context) (string, func(), error) {
	if url := os.Getenv("DURABLEMCP_TEST_DATABASE_URL"); url != "" {
		if err := migrate(ctx, url); err != nil {
			return "", nil, fmt.Errorf("apply migrations to DURABLEMCP_TEST_DATABASE_URL: %w", err)
		}
		return url, func() {}, nil
	}
	container, err := postgres.Run(ctx, image,
		postgres.WithDatabase("durablemcp"),
		postgres.WithUsername("durablemcp"),
		postgres.WithPassword("durablemcp"),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		return "", nil, fmt.Errorf("start %s container (is Docker running?): %w", image, err)
	}
	url, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = container.Terminate(ctx)
		return "", nil, err
	}
	if err := migrate(ctx, url); err != nil {
		_ = container.Terminate(ctx)
		return "", nil, err
	}
	return url, func() { _ = container.Terminate(context.Background()) }, nil
}

func migrate(ctx context.Context, url string) error {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return err
	}
	defer pool.Close()
	var existing bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('public.executions') IS NOT NULL`).Scan(&existing); err != nil {
		return err
	}
	if existing {
		return nil
	}
	sql, err := os.ReadFile(migrationsPath())
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, string(sql))
	return err
}

func migrationsPath() string {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return filepath.Join("migrations", migrationFile)
	}
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "migrations", migrationFile)
}

var sharedHarness struct {
	mu   sync.Mutex
	url  string
	stop func()
}

// Shared returns the connection string for the process-wide test database,
// booting the container on first use.
func Shared(t testing.TB) string {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in -short mode; run make test-pg")
	}
	sharedHarness.mu.Lock()
	defer sharedHarness.mu.Unlock()
	if sharedHarness.url == "" {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		url, stop, err := Start(ctx)
		if err != nil {
			t.Fatalf("PostgreSQL test database unavailable: %v (set DURABLEMCP_TEST_DATABASE_URL or ensure Docker is running)", err)
		}
		sharedHarness.url, sharedHarness.stop = url, stop
	}
	return sharedHarness.url
}

// Shutdown terminates the shared container; called once from TestMain.
func Shutdown() {
	sharedHarness.mu.Lock()
	defer sharedHarness.mu.Unlock()
	if sharedHarness.stop != nil {
		sharedHarness.stop()
		sharedHarness.stop = nil
	}
}
