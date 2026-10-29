package database

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MaskDatabaseURL safely obscures sensitive credentials (passwords) from the database URL for secure logging.
func MaskDatabaseURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "[malformed database url]"
	}
	if u.User != nil {
		if _, hasPassword := u.User.Password(); hasPassword {
			u.User = url.UserPassword(u.User.Username(), "******")
		}
	}
	return u.String()
}

// ConnectDB initializes a high-concurrency connection pool tailored for Supabase
// transaction poolers (Supavisor / port 6543). It configures conservative pool bounds
// and verifies database responsiveness via connection retries with exponential backoff and diagnostics.
func ConnectDB(databaseURL string) (*pgxpool.Pool, error) {
	cleanURL := strings.TrimSpace(databaseURL)
	if cleanURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is empty; please ensure the environment variable or Fly.io secret is configured")
	}

	poolConfig, err := pgxpool.ParseConfig(cleanURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse DATABASE_URL (%s): %w\n[HINT] Ensure passwords containing special characters (e.g. '@', ':', '#', '$', '%%', '&') are URL-encoded in the connection string", MaskDatabaseURL(cleanURL), err)
	}

	connHost := poolConfig.ConnConfig.Host
	connPort := poolConfig.ConnConfig.Port
	connUser := poolConfig.ConnConfig.User
	connDB := poolConfig.ConnConfig.Database
	connTLS := poolConfig.ConnConfig.TLSConfig != nil

	log.Printf("[DATABASE] Configuring Supabase connection pool: Host=%s, Port=%d, DB=%s, User=%s, TLS=%v",
		connHost, connPort, connDB, connUser, connTLS)

	if connPort == 5432 && strings.Contains(connHost, "supabase.co") {
		log.Printf("[DATABASE NOTICE] Connecting to Supabase direct port 5432. Note: Supabase recommends using the transaction pooler (port 6543, pooler.supabase.com) for containerized deployments with IPv4.")
	}

	// Supabase High-Concurrency Pool Tuning (Port 6543 / Transaction Pooler)
	poolConfig.MaxConns = 25
	poolConfig.MinConns = 2
	poolConfig.MaxConnLifetime = 1 * time.Hour
	poolConfig.MaxConnIdleTime = 15 * time.Minute
	poolConfig.HealthCheckPeriod = 1 * time.Minute
	poolConfig.ConnConfig.ConnectTimeout = 8 * time.Second

	// Enable simple protocol mode for Supabase transaction pooler compatibility (avoids prepared statement collisions)
	poolConfig.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol

	const maxRetries = 5
	initialBackoff := 1500 * time.Millisecond

	var pool *pgxpool.Pool
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		initCtx, initCancel := context.WithTimeout(context.Background(), 8*time.Second)
		pool, err = pgxpool.NewWithConfig(initCtx, poolConfig)
		initCancel()

		if err != nil {
			lastErr = err
			log.Printf("[DATABASE] Attempt %d/%d: Failed to initialize pool: %v. Retrying in %v...",
				attempt, maxRetries, err, initialBackoff)
			time.Sleep(initialBackoff)
			initialBackoff *= 2
			continue
		}

		// Ping check with explicit timeout to ensure Supabase pooler is actively accepting connections
		pingCtx, pingCancel := context.WithTimeout(context.Background(), 5*time.Second)
		pingErr := pool.Ping(pingCtx)
		pingCancel()

		if pingErr == nil {
			log.Printf("[DATABASE] Supabase pgxpool connected and ping verified on attempt %d/%d (MinConns: %d, MaxConns: %d)",
				attempt, maxRetries, poolConfig.MinConns, poolConfig.MaxConns)
			return pool, nil
		}

		lastErr = pingErr
		pool.Close()

		// Diagnose the error for fast troubleshooting in Fly.io logs
		errStr := pingErr.Error()
		diagnosticHint := ""
		switch {
		case strings.Contains(errStr, "password authentication failed"):
			diagnosticHint = " -> Check database password and user in DATABASE_URL."
		case strings.Contains(errStr, "no such host"):
			diagnosticHint = " -> Hostname resolution failed. Check your Supabase host in DATABASE_URL."
		case strings.Contains(errStr, "connection refused"):
			diagnosticHint = " -> Connection refused. Verify host, port (e.g. 6543 vs 5432), and Supabase project status."
		case strings.Contains(errStr, "timeout") || strings.Contains(errStr, "deadline exceeded"):
			diagnosticHint = " -> Network timeout reaching database. Check firewall, network egress, or pooler response."
		case strings.Contains(errStr, "certificate") || strings.Contains(errStr, "tls"):
			diagnosticHint = " -> SSL/TLS negotiation failed. Verify sslmode parameter (e.g. sslmode=require)."
		}

		log.Printf("[DATABASE] Attempt %d/%d: Supabase ping failed (%v)%s. Retrying in %v...",
			attempt, maxRetries, pingErr, diagnosticHint, initialBackoff)

		if attempt < maxRetries {
			time.Sleep(initialBackoff)
			initialBackoff *= 2
		}
	}

	return nil, fmt.Errorf("failed to establish connection to Supabase pooler at %s:%d after %d attempts: %w\n[URL Preview: %s]",
		connHost, connPort, maxRetries, lastErr, MaskDatabaseURL(cleanURL))
}

// CloseDB gracefully and safely drains and closes the pgxpool connection pool.
func CloseDB(pool *pgxpool.Pool) {
	if pool != nil {
		pool.Close()
		log.Println("[DATABASE] PostgreSQL connection pool safely closed.")
	}
}
