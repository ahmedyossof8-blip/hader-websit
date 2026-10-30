package workers

import (
	"context"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"hader-backend/internal/modules/sessions"
)

// StartDailyStreakCalculator runs a background goroutine scheduled to execute every midnight (00:01 AM).
// It recalculates streak counters across all enrolled students and resets broken streaks.
func StartDailyStreakCalculator(ctx context.Context, pool *pgxpool.Pool) {
	repo := sessions.NewRepository(pool)
	log.Println("[WORKER] DailyStreakCalculator initialized. Scheduled to run daily at 00:01 AM.")

	for {
		delay := DurationUntilNextRun(0, 1)
		timer := time.NewTimer(delay)

		select {
		case <-ctx.Done():
			timer.Stop()
			log.Println("[WORKER] DailyStreakCalculator intercepted shutdown signal. Terminating gracefully...")
			return
		case <-timer.C:
			RunStreakRecalculation(ctx, repo)
		}
	}
}

// RunStreakRecalculation executes the SQL streak adjustment query and outputs execution logs.
func RunStreakRecalculation(ctx context.Context, repo sessions.Repository) (int64, error) {
	qCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	updated, err := repo.RecalculateStudentStreaks(qCtx)
	if err != nil {
		log.Printf("[WORKER-ERROR] Daily streak recalculation failed: %v", err)
		return 0, err
	}

	log.Printf("[WORKER] Daily streak recalculation complete. Reset broken streaks for %d students with absences.", updated)
	return updated, nil
}

// DurationUntilNextRun calculates time duration until the specified hour and minute.
func DurationUntilNextRun(targetHour, targetMinute int) time.Duration {
	now := time.Now()
	next := time.Date(now.Year(), now.Month(), now.Day(), targetHour, targetMinute, 0, 0, now.Location())
	if !now.Before(next) {
		next = next.Add(24 * time.Hour)
	}
	return next.Sub(now)
}
