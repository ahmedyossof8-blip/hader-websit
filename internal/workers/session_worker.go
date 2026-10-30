package workers

import (
	"context"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"hader-backend/internal/modules/sessions"
)

// StartSessionWatcher monitors ongoing lecture sessions, automatically closes expired sessions,
// and atomically records absentee entries for enrolled students who failed to check in.
func StartSessionWatcher(ctx context.Context, pool *pgxpool.Pool, interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Second
	}

	repo := sessions.NewRepository(pool)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.Printf("[WORKER] SessionWatcher started. Monitoring interval: %v", interval)

	for {
		select {
		case <-ctx.Done():
			log.Println("[WORKER] SessionWatcher intercepted shutdown signal. Terminating gracefully...")
			return
		case <-ticker.C:
			ProcessExpiredSessions(ctx, repo)
		}
	}
}

// ProcessExpiredSessions queries and closes all active sessions whose ends_at time has elapsed.
func ProcessExpiredSessions(ctx context.Context, repo sessions.Repository) int {
	qCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	expiredSessions, err := repo.GetExpiredActiveSessions(qCtx)
	if err != nil {
		log.Printf("[WORKER-ERROR] Failed to query expired active sessions: %v", err)
		return 0
	}

	closedCount := 0
	for _, s := range expiredSessions {
		log.Printf("[WORKER] Auto-closing expired session %s ('%s'). EndsAt: %s",
			s.ID, s.SubjectName, s.EndsAt.Format(time.RFC3339))

		if err := repo.CloseSession(qCtx, s.ID); err != nil {
			log.Printf("[WORKER-ERROR] Failed to close session %s: %v", s.ID, err)
			continue
		}

		deptID := 0
		if s.DepartmentID != nil {
			deptID = *s.DepartmentID
		}

		absentees, err := repo.AutoMarkAbsenteesTx(qCtx, s.ID, deptID)
		if err != nil {
			log.Printf("[WORKER-ERROR] Failed to auto-mark absentees for session %s: %v", s.ID, err)
			continue
		}

		closedCount++
		log.Printf("[WORKER] Successfully closed session %s. Marked %d students absent.", s.ID, absentees)
	}

	return closedCount
}
