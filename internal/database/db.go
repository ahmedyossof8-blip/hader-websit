package database

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ConnectDB initializes a high-concurrency connection pool tailored for Supabase
// transaction poolers (Supavisor / port 6543). It configures conservative pool bounds
// and verifies database responsiveness via connection retries with exponential backoff.
func ConnectDB(databaseURL string) (*pgxpool.Pool, error) {
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse database configuration string: %w", err)
	}

	// Supabase High-Concurrency Pool Tuning (Port 6543 / Transaction Pooler)
	poolConfig.MaxConns = 30
	poolConfig.MinConns = 5
	poolConfig.MaxConnLifetime = 1 * time.Hour
	poolConfig.MaxConnIdleTime = 30 * time.Minute
	poolConfig.HealthCheckPeriod = 1 * time.Minute
	poolConfig.ConnConfig.ConnectTimeout = 10 * time.Second

	// Enable simple protocol mode for Supabase transaction pooler compatibility (avoids prepared statement collisions)
	poolConfig.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol

	const maxRetries = 5
	initialBackoff := 2 * time.Second

	var pool *pgxpool.Pool
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		// Context for pool creation attempt
		initCtx, initCancel := context.WithTimeout(context.Background(), 10*time.Second)
		pool, err = pgxpool.NewWithConfig(initCtx, poolConfig)
		initCancel()

		if err != nil {
			lastErr = err
			log.Printf("[DATABASE] Attempt %d/%d: Failed to initialize pool config: %v. Retrying in %v...",
				attempt, maxRetries, err, initialBackoff)
			time.Sleep(initialBackoff)
			initialBackoff *= 2
			continue
		}

		// Ping check with explicit timeout to ensure Supabase pooler is actively accepting connections
		pingCtx, pingCancel := context.WithTimeout(context.Background(), 8*time.Second)
		pingErr := pool.Ping(pingCtx)
		pingCancel()

		if pingErr == nil {
			log.Printf("[DATABASE] Supabase pgxpool initialized successfully on attempt %d/%d (MinConns: %d, MaxConns: %d)",
				attempt, maxRetries, poolConfig.MinConns, poolConfig.MaxConns)
			return pool, nil
		}

		lastErr = pingErr
		pool.Close()
		log.Printf("[DATABASE] Attempt %d/%d: Supabase transaction pooler ping failed (%v). Retrying in %v...",
			attempt, maxRetries, pingErr, initialBackoff)

		if attempt < maxRetries {
			time.Sleep(initialBackoff)
			initialBackoff *= 2
		}
	}

	return nil, fmt.Errorf("failed to establish connection to Supabase pooler after %d attempts: %w", maxRetries, lastErr)
}

// CloseDB gracefully and safely drains and closes the pgxpool connection pool.
func CloseDB(pool *pgxpool.Pool) {
	if pool != nil {
		pool.Close()
		log.Println("[DATABASE] PostgreSQL connection pool safely closed.")
	}
}

