package workers

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"hader-backend/internal/modules/sessions"
)

// mockWorkerRepo provides a lightweight mock of sessions.Repository for worker tests.
type mockWorkerRepo struct {
	sessions.Repository
	expiredSessions []sessions.LectureSession
	closedSessions  []uuid.UUID
	absenteesMarked int64
	streaksReset    int64
}

func (m *mockWorkerRepo) GetExpiredActiveSessions(ctx context.Context) ([]sessions.LectureSession, error) {
	return m.expiredSessions, nil
}

func (m *mockWorkerRepo) CloseSession(ctx context.Context, sessionID uuid.UUID) error {
	m.closedSessions = append(m.closedSessions, sessionID)
	return nil
}

func (m *mockWorkerRepo) AutoMarkAbsenteesTx(ctx context.Context, sessionID uuid.UUID, departmentID int) (int64, error) {
	m.absenteesMarked += 10
	return 10, nil
}

func (m *mockWorkerRepo) RecalculateStudentStreaks(ctx context.Context) (int64, error) {
	m.streaksReset = 7
	return 7, nil
}

func TestProcessExpiredSessions(t *testing.T) {
	sessionID1 := uuid.New()
	sessionID2 := uuid.New()
	dept1 := 1
	dept2 := 2

	repo := &mockWorkerRepo{
		expiredSessions: []sessions.LectureSession{
			{
				ID:           sessionID1,
				SubjectName:  "مناهج وطرق تدريس",
				DepartmentID: &dept1,
				Status:       sessions.StatusActive,
				EndsAt:       time.Now().Add(-10 * time.Minute),
			},
			{
				ID:           sessionID2,
				SubjectName:  "تطوير البرمجيات التعليمية",
				DepartmentID: &dept2,
				Status:       sessions.StatusActive,
				EndsAt:       time.Now().Add(-5 * time.Minute),
			},
		},
	}

	closed := ProcessExpiredSessions(context.Background(), repo)
	if closed != 2 {
		t.Fatalf("expected 2 sessions to be closed, got %d", closed)
	}

	if len(repo.closedSessions) != 2 {
		t.Errorf("expected 2 closed sessions recorded, got %d", len(repo.closedSessions))
	}

	if repo.absenteesMarked != 20 {
		t.Errorf("expected 20 absentees marked, got %d", repo.absenteesMarked)
	}
}

func TestRunStreakRecalculation(t *testing.T) {
	repo := &mockWorkerRepo{}
	updated, err := RunStreakRecalculation(context.Background(), repo)
	if err != nil {
		t.Fatalf("unexpected error during streak recalculation: %v", err)
	}
	if updated != 7 {
		t.Errorf("expected 7 streaks reset, got %d", updated)
	}
}

func TestDurationUntilNextRun(t *testing.T) {
	duration := DurationUntilNextRun(0, 1)
	if duration <= 0 {
		t.Errorf("expected positive duration until next run, got %v", duration)
	}
	if duration > 24*time.Hour {
		t.Errorf("expected duration <= 24 hours, got %v", duration)
	}
}
