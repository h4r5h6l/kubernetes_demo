// Package database manages the PostgreSQL connection pool and migrations.
package database

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Connect creates a pgx pool and verifies that PostgreSQL is reachable.
func Connect(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}

	poolCfg.MaxConns = 10
	poolCfg.MinConns = 1
	poolCfg.MaxConnLifetime = time.Hour
	poolCfg.MaxConnIdleTime = 30 * time.Minute
	poolCfg.HealthCheckPeriod = time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("create connection pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return pool, nil
}

// ConnectWithRetry waits for PostgreSQL to become available. This is useful in
// orchestrators where the app container can start before the database.
func ConnectWithRetry(ctx context.Context, dsn string, attempts int, delay time.Duration) (*pgxpool.Pool, error) {
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		pool, err := Connect(ctx, dsn)
		if err == nil {
			return pool, nil
		}
		lastErr = err

		if attempt == attempts {
			break
		}
		log.Printf("database not ready (attempt %d/%d): %v", attempt, attempts, err)

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}
	return nil, fmt.Errorf("database unavailable after %d attempts: %w", attempts, lastErr)
}
