package sessions

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// mockSessionRepository provides an in-memory test double for sessions.Repository.
type mockSessionRepository struct {
	sessions         map[uuid.UUID]*LectureSession
	attendees        map[uuid.UUID]int
	recentAttendees  map[uuid.UUID][]AttendeeRecord
	absenteesMarked  int64
	streaksReset     int64
	studentDept      map[uuid.UUID]int
	studentSchedules []StudentScheduleItem
}

func newMockSessionRepo() *mockSessionRepository {
	return &mockSessionRepository{
		sessions:        make(map[uuid.UUID]*LectureSession),
		attendees:       make(map[uuid.UUID]int),
		recentAttendees: make(map[uuid.UUID][]AttendeeRecord),
		studentDept:     make(map[uuid.UUID]int),
	}
}

func (m *mockSessionRepository) CreateSession(ctx context.Context, session *LectureSession) error {
	if session.QRSecret == "" {
		session.QRSecret = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	}
	m.sessions[session.ID] = session
	return nil
}

func (m *mockSessionRepository) GetSessionByID(ctx context.Context, id uuid.UUID) (*LectureSession, error) {
	if s, exists := m.sessions[id]; exists {
		return s, nil
	}
	return nil, nil
}

func (m *mockSessionRepository) GetActiveSessionByDepartment(ctx context.Context, departmentID int) (*LectureSession, error) {
	for _, s := range m.sessions {
		if s.DepartmentID != nil && *s.DepartmentID == departmentID && s.Status == StatusActive {
			return s, nil
		}
	}
	return nil, nil
}

func (m *mockSessionRepository) GetSessionAttendeesCount(ctx context.Context, sessionID uuid.UUID) (int, error) {
	return m.attendees[sessionID], nil
}

func (m *mockSessionRepository) GetRecentAttendees(ctx context.Context, sessionID uuid.UUID, limit int) ([]AttendeeRecord, error) {
	return m.recentAttendees[sessionID], nil
}

func (m *mockSessionRepository) CloseSession(ctx context.Context, sessionID uuid.UUID) error {
	if s, exists := m.sessions[sessionID]; exists {
		s.Status = StatusClosed
	}
	return nil
}

func (m *mockSessionRepository) GetExpiredActiveSessions(ctx context.Context) ([]LectureSession, error) {
	var expired []LectureSession
	now := time.Now()
	for _, s := range m.sessions {
		if s.Status == StatusActive && s.EndsAt.Before(now) {
			expired = append(expired, *s)
		}
	}
	return expired, nil
}

func (m *mockSessionRepository) AutoMarkAbsenteesTx(ctx context.Context, sessionID uuid.UUID, departmentID int) (int64, error) {
	m.absenteesMarked += 5
	return 5, nil
}

func (m *mockSessionRepository) RecalculateStudentStreaks(ctx context.Context) (int64, error) {
	m.streaksReset += 2
	return 2, nil
}

func (m *mockSessionRepository) GetStudentTodaySchedule(ctx context.Context, studentID uuid.UUID, departmentID int) ([]StudentScheduleItem, error) {
	return m.studentSchedules, nil
}

func (m *mockSessionRepository) GetStudentDepartment(ctx context.Context, studentID uuid.UUID) (*int, error) {
	if dept, exists := m.studentDept[studentID]; exists {
		return &dept, nil
	}
	return nil, nil
}

// Tests

func TestCreateSession_Success(t *testing.T) {
	repo := newMockSessionRepo()
	svc := NewService(repo)

	leaderID := uuid.New()
	req := &CreateSessionRequest{
		SubjectName:     "تكنولوجيا التعليم والوسائط المتعددة",
		InstructorName:  "د. أحمد السعيد",
		DepartmentID:    1,
		GeofenceLat:     30.0444,
		GeofenceLng:     31.2357,
		GeofenceRadius:  50,
		DurationMinutes: 90,
	}

	resp, err := svc.CreateSession(context.Background(), leaderID, req)
	if err != nil {
		t.Fatalf("unexpected error creating session: %v", err)
	}

	if resp.SessionID == uuid.Nil {
		t.Errorf("expected valid session UUID, got nil")
	}
	if resp.SubjectName != req.SubjectName {
		t.Errorf("expected subject %s, got %s", req.SubjectName, resp.SubjectName)
	}
	if resp.Status != string(StatusActive) {
		t.Errorf("expected status 'active', got %s", resp.Status)
	}
	if resp.EndsAt.Sub(resp.StartsAt).Minutes() < 89 {
		t.Errorf("expected duration around 90 minutes, got %v", resp.EndsAt.Sub(resp.StartsAt))
	}
}

func TestCreateSession_ValidationFailures(t *testing.T) {
	repo := newMockSessionRepo()
	svc := NewService(repo)
	leaderID := uuid.New()

	tests := []struct {
		name string
		req  *CreateSessionRequest
	}{
		{
			name: "Missing subject",
			req: &CreateSessionRequest{
				SubjectName:     "",
				InstructorName:  "Dr. Smith",
				DepartmentID:    1,
				GeofenceLat:     30.0,
				GeofenceLng:     31.0,
				DurationMinutes: 60,
			},
		},
		{
			name: "Missing instructor",
			req: &CreateSessionRequest{
				SubjectName:     "Math",
				InstructorName:  "",
				DepartmentID:    1,
				GeofenceLat:     30.0,
				GeofenceLng:     31.0,
				DurationMinutes: 60,
			},
		},
		{
			name: "Invalid department ID",
			req: &CreateSessionRequest{
				SubjectName:     "Math",
				InstructorName:  "Dr. Smith",
				DepartmentID:    0,
				GeofenceLat:     30.0,
				GeofenceLng:     31.0,
				DurationMinutes: 60,
			},
		},
		{
			name: "Invalid latitude",
			req: &CreateSessionRequest{
				SubjectName:     "Math",
				InstructorName:  "Dr. Smith",
				DepartmentID:    1,
				GeofenceLat:     95.0,
				GeofenceLng:     31.0,
				DurationMinutes: 60,
			},
		},
		{
			name: "Duration out of bounds (too short)",
			req: &CreateSessionRequest{
				SubjectName:     "Math",
				InstructorName:  "Dr. Smith",
				DepartmentID:    1,
				GeofenceLat:     30.0,
				GeofenceLng:     31.0,
				DurationMinutes: 10,
			},
		},
		{
			name: "Duration out of bounds (too long)",
			req: &CreateSessionRequest{
				SubjectName:     "Math",
				InstructorName:  "Dr. Smith",
				DepartmentID:    1,
				GeofenceLat:     30.0,
				GeofenceLng:     31.0,
				DurationMinutes: 300,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.CreateSession(context.Background(), leaderID, tc.req)
			if err == nil {
				t.Fatalf("expected validation error for test case '%s', got nil", tc.name)
			}
		})
	}
}

func TestGetSessionByID_AndActiveByDepartment(t *testing.T) {
	repo := newMockSessionRepo()
	svc := NewService(repo)

	deptID := 2
	sessionID := uuid.New()
	instructor := "د. سارة عادل"
	repo.sessions[sessionID] = &LectureSession{
		ID:             sessionID,
		SubjectName:    "تصميم نظم التعليم الذكية",
		InstructorName: &instructor,
		DepartmentID:   &deptID,
		Status:         StatusActive,
		StartsAt:       time.Now().Add(-10 * time.Minute),
		EndsAt:         time.Now().Add(50 * time.Minute),
	}
	repo.attendees[sessionID] = 28

	// 1. Fetch by ID
	res, err := svc.GetSessionByID(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.TotalAttendees != 28 {
		t.Errorf("expected 28 attendees, got %d", res.TotalAttendees)
	}

	// 2. Fetch by unknown ID
	_, err = svc.GetSessionByID(context.Background(), uuid.New())
	if err != ErrSessionNotFound {
		t.Errorf("expected ErrSessionNotFound, got %v", err)
	}

	// 3. Fetch active by department
	activeRes, err := svc.GetActiveSessionByDepartment(context.Background(), deptID)
	if err != nil {
		t.Fatalf("unexpected error querying active session: %v", err)
	}
	if activeRes.SessionID != sessionID {
		t.Errorf("expected session %s, got %s", sessionID, activeRes.SessionID)
	}

	// 4. Fetch active for inactive department
	_, err = svc.GetActiveSessionByDepartment(context.Background(), 999)
	if err != ErrNoActiveSession {
		t.Errorf("expected ErrNoActiveSession, got %v", err)
	}
}

func TestCloseSession_AndAutoMarkAbsentees(t *testing.T) {
	repo := newMockSessionRepo()
	svc := NewService(repo)

	deptID := 1
	sessionID := uuid.New()
	repo.sessions[sessionID] = &LectureSession{
		ID:           sessionID,
		SubjectName:  "البرمجة التربوية",
		DepartmentID: &deptID,
		Status:       StatusActive,
		StartsAt:     time.Now().Add(-60 * time.Minute),
		EndsAt:       time.Now().Add(-5 * time.Minute),
	}

	if err := svc.CloseSession(context.Background(), sessionID); err != nil {
		t.Fatalf("unexpected error closing session: %v", err)
	}

	// Check status updated
	if repo.sessions[sessionID].Status != StatusClosed {
		t.Errorf("expected status closed, got %s", repo.sessions[sessionID].Status)
	}

	// Check absentees marked
	if repo.absenteesMarked != 5 {
		t.Errorf("expected 5 absentees marked, got %d", repo.absenteesMarked)
	}

	// Idempotent close
	if err := svc.CloseSession(context.Background(), sessionID); err != nil {
		t.Errorf("expected closing already closed session to succeed, got %v", err)
	}
}

func TestGetStudentTodaySchedule(t *testing.T) {
	repo := newMockSessionRepo()
	svc := NewService(repo)

	studentID := uuid.New()
	deptID := 3
	repo.studentDept[studentID] = deptID

	sessIDStr := uuid.New().String()
	repo.studentSchedules = []StudentScheduleItem{
		{
			SessionID:      &sessIDStr,
			SubjectName:    "تكنولوجيا التعليم والذكاء الاصطناعي",
			InstructorName: "د. هاني ممدوح",
			DayOfWeek:      "Saturday",
			StartTime:      "09:00",
			EndTime:        "11:00",
			IsActiveNow:    true,
			HasCheckedIn:   false,
		},
	}

	// Query with explicit department
	items, err := svc.GetStudentTodaySchedule(context.Background(), studentID, deptID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if !items[0].IsActiveNow {
		t.Errorf("expected IsActiveNow to be true")
	}

	// Query with departmentID = 0 (auto-resolved from student profile)
	autoItems, err := svc.GetStudentTodaySchedule(context.Background(), studentID, 0)
	if err != nil {
		t.Fatalf("unexpected error with auto-resolution: %v", err)
	}
	if len(autoItems) != 1 {
		t.Fatalf("expected 1 item, got %d", len(autoItems))
	}

	// Unknown student with no enrolled department
	unknownStudentID := uuid.New()
	_, err = svc.GetStudentTodaySchedule(context.Background(), unknownStudentID, 0)
	if err != ErrInvalidDepartment {
		t.Errorf("expected ErrInvalidDepartment, got %v", err)
	}
}
