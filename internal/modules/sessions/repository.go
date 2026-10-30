package sessions

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository specifies data operations for lecture sessions, real-time attendee queries, and streak workers.
type Repository interface {
	CreateSession(ctx context.Context, session *LectureSession) error
	GetSessionByID(ctx context.Context, id uuid.UUID) (*LectureSession, error)
	GetActiveSessionByDepartment(ctx context.Context, departmentID int) (*LectureSession, error)
	GetSessionAttendeesCount(ctx context.Context, sessionID uuid.UUID) (int, error)
	GetRecentAttendees(ctx context.Context, sessionID uuid.UUID, limit int) ([]AttendeeRecord, error)
	CloseSession(ctx context.Context, sessionID uuid.UUID) error
	GetExpiredActiveSessions(ctx context.Context) ([]LectureSession, error)
	AutoMarkAbsenteesTx(ctx context.Context, sessionID uuid.UUID, departmentID int) (int64, error)
	RecalculateStudentStreaks(ctx context.Context) (int64, error)
	GetStudentTodaySchedule(ctx context.Context, studentID uuid.UUID, departmentID int) ([]StudentScheduleItem, error)
	GetStudentDepartment(ctx context.Context, studentID uuid.UUID) (*int, error)
}

type sqlRepository struct {
	pool *pgxpool.Pool
}

// NewRepository initializes the session repository backed by pgxpool.
func NewRepository(pool *pgxpool.Pool) Repository {
	return &sqlRepository{pool: pool}
}

// CreateSession persists a new session and generates a cryptographically secure 32-byte hex QR secret if missing.
func (r *sqlRepository) CreateSession(ctx context.Context, session *LectureSession) error {
	qCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	if session.QRSecret == "" {
		randomBytes := make([]byte, 32)
		if _, err := rand.Read(randomBytes); err != nil {
			return fmt.Errorf("failed generating random secret: %w", err)
		}
		session.QRSecret = hex.EncodeToString(randomBytes)
	}

	if session.ID == uuid.Nil {
		session.ID = uuid.New()
	}

	query := `
		INSERT INTO lecture_sessions (
			id, subject_name, instructor_name, department_id,
			geofence_lat, geofence_lng, geofence_radius, qr_secret,
			status, starts_at, ends_at, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NOW())
	`

	_, err := r.pool.Exec(qCtx, query,
		session.ID,
		session.SubjectName,
		session.InstructorName,
		session.DepartmentID,
		session.GeofenceLat,
		session.GeofenceLng,
		session.GeofenceRadius,
		session.QRSecret,
		session.Status,
		session.StartsAt,
		session.EndsAt,
	)
	if err != nil {
		return fmt.Errorf("failed inserting lecture session: %w", err)
	}

	return nil
}

// GetSessionByID fetches a session by its unique UUID identifier.
func (r *sqlRepository) GetSessionByID(ctx context.Context, id uuid.UUID) (*LectureSession, error) {
	qCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	query := `
		SELECT id, subject_name, instructor_name, department_id,
		       geofence_lat, geofence_lng, geofence_radius, qr_secret,
		       status, starts_at, ends_at, created_at
		FROM lecture_sessions
		WHERE id = $1
		LIMIT 1
	`

	var s LectureSession
	err := r.pool.QueryRow(qCtx, query, id).Scan(
		&s.ID,
		&s.SubjectName,
		&s.InstructorName,
		&s.DepartmentID,
		&s.GeofenceLat,
		&s.GeofenceLng,
		&s.GeofenceRadius,
		&s.QRSecret,
		&s.Status,
		&s.StartsAt,
		&s.EndsAt,
		&s.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("error querying session: %w", err)
	}

	return &s, nil
}

// GetActiveSessionByDepartment returns the currently ongoing lecture for a given department.
func (r *sqlRepository) GetActiveSessionByDepartment(ctx context.Context, departmentID int) (*LectureSession, error) {
	qCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	query := `
		SELECT id, subject_name, instructor_name, department_id,
		       geofence_lat, geofence_lng, geofence_radius, qr_secret,
		       status, starts_at, ends_at, created_at
		FROM lecture_sessions
		WHERE department_id = $1 AND status = 'active'
		ORDER BY starts_at DESC
		LIMIT 1
	`

	var s LectureSession
	err := r.pool.QueryRow(qCtx, query, departmentID).Scan(
		&s.ID,
		&s.SubjectName,
		&s.InstructorName,
		&s.DepartmentID,
		&s.GeofenceLat,
		&s.GeofenceLng,
		&s.GeofenceRadius,
		&s.QRSecret,
		&s.Status,
		&s.StartsAt,
		&s.EndsAt,
		&s.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("error finding active session: %w", err)
	}

	return &s, nil
}

// GetSessionAttendeesCount returns the total number of present attendees.
func (r *sqlRepository) GetSessionAttendeesCount(ctx context.Context, sessionID uuid.UUID) (int, error) {
	qCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	query := `
		SELECT COUNT(*)
		FROM attendance_records
		WHERE session_id = $1 AND verification_mode != 'absent'
	`

	var count int
	if err := r.pool.QueryRow(qCtx, query, sessionID).Scan(&count); err != nil {
		return 0, fmt.Errorf("failed counting session attendees: %w", err)
	}

	return count, nil
}

// GetRecentAttendees retrieves the latest verified attendees for real-time instructor dashboards.
func (r *sqlRepository) GetRecentAttendees(ctx context.Context, sessionID uuid.UUID, limit int) ([]AttendeeRecord, error) {
	qCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	if limit <= 0 {
		limit = 10
	}

	query := `
		SELECT a.student_id, u.academic_id, u.full_name, a.check_in_time, a.verification_mode
		FROM attendance_records a
		JOIN user_profiles u ON a.student_id = u.id
		WHERE a.session_id = $1 AND a.verification_mode != 'absent'
		ORDER BY a.check_in_time DESC
		LIMIT $2
	`

	rows, err := r.pool.Query(qCtx, query, sessionID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed fetching recent attendees: %w", err)
	}
	defer rows.Close()

	records := make([]AttendeeRecord, 0, limit)
	for rows.Next() {
		var rec AttendeeRecord
		if err := rows.Scan(
			&rec.StudentID,
			&rec.AcademicID,
			&rec.FullName,
			&rec.CheckInTime,
			&rec.VerificationMode,
		); err != nil {
			return nil, fmt.Errorf("error scanning attendee: %w", err)
		}
		records = append(records, rec)
	}

	return records, nil
}

// CloseSession terminates an ongoing active lecture session.
func (r *sqlRepository) CloseSession(ctx context.Context, sessionID uuid.UUID) error {
	qCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	query := `UPDATE lecture_sessions SET status = 'closed' WHERE id = $1`
	_, err := r.pool.Exec(qCtx, query, sessionID)
	return err
}

// GetExpiredActiveSessions returns active sessions whose ends_at timestamp has elapsed.
func (r *sqlRepository) GetExpiredActiveSessions(ctx context.Context) ([]LectureSession, error) {
	qCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	query := `
		SELECT id, subject_name, instructor_name, department_id,
		       geofence_lat, geofence_lng, geofence_radius, qr_secret,
		       status, starts_at, ends_at, created_at
		FROM lecture_sessions
		WHERE status = 'active' AND ends_at <= NOW()
	`

	rows, err := r.pool.Query(qCtx, query)
	if err != nil {
		return nil, fmt.Errorf("failed querying expired sessions: %w", err)
	}
	defer rows.Close()

	sessions := make([]LectureSession, 0, 8)
	for rows.Next() {
		var s LectureSession
		if err := rows.Scan(
			&s.ID,
			&s.SubjectName,
			&s.InstructorName,
			&s.DepartmentID,
			&s.GeofenceLat,
			&s.GeofenceLng,
			&s.GeofenceRadius,
			&s.QRSecret,
			&s.Status,
			&s.StartsAt,
			&s.EndsAt,
			&s.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("error scanning session: %w", err)
		}
		sessions = append(sessions, s)
	}

	return sessions, nil
}

// AutoMarkAbsenteesTx atomically populates 'absent' records for enrolled department students
// who have not checked in to the ended session.
func (r *sqlRepository) AutoMarkAbsenteesTx(ctx context.Context, sessionID uuid.UUID, departmentID int) (int64, error) {
	qCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	query := `
		INSERT INTO attendance_records (
			session_id, student_id, device_id, verification_mode, accuracy_meters, client_ip, check_in_time
		)
		SELECT $1, u.id, 'SYSTEM_AUTO_ABSENT', 'absent', 0.0, '127.0.0.1'::inet, NOW()
		FROM user_profiles u
		WHERE ($2 = 0 OR u.department_id = $2)
		  AND u.role = 'student'
		  AND u.is_active = TRUE
		  AND NOT EXISTS (
		      SELECT 1 FROM attendance_records ar
		      WHERE ar.session_id = $1 AND ar.student_id = u.id
		  )
		ON CONFLICT (session_id, student_id) DO NOTHING
	`

	tag, err := r.pool.Exec(qCtx, query, sessionID, departmentID)
	if err != nil {
		return 0, fmt.Errorf("failed auto-marking absentees: %w", err)
	}

	return tag.RowsAffected(), nil
}

// RecalculateStudentStreaks resets streak counters for students with unexcused absences.
func (r *sqlRepository) RecalculateStudentStreaks(ctx context.Context) (int64, error) {
	qCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	query := `
		UPDATE user_profiles u
		SET current_streak = 0
		WHERE u.role = 'student'
		  AND u.is_active = TRUE
		  AND u.current_streak > 0
		  AND EXISTS (
		      SELECT 1 FROM attendance_records ar
		      JOIN lecture_sessions ls ON ar.session_id = ls.id
		      WHERE ar.student_id = u.id
		        AND ar.verification_mode = 'absent'
		        AND ls.ends_at >= CURRENT_DATE - INTERVAL '1 day'
		  )
	`

	tag, err := r.pool.Exec(qCtx, query)
	if err != nil {
		return 0, fmt.Errorf("failed recalculating streaks: %w", err)
	}

	return tag.RowsAffected(), nil
}

// GetStudentTodaySchedule queries today's lecture sessions for the student's department,
// indicating active status and whether the student has checked in.
func (r *sqlRepository) GetStudentTodaySchedule(ctx context.Context, studentID uuid.UUID, departmentID int) ([]StudentScheduleItem, error) {
	qCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	query := `
		SELECT s.id::text, s.subject_name, COALESCE(s.instructor_name, 'هيئة التدريس'),
		       TO_CHAR(s.starts_at, 'FMDay'),
		       TO_CHAR(s.starts_at, 'HH24:MI'),
		       TO_CHAR(s.ends_at, 'HH24:MI'),
		       (s.status = 'active' AND NOW() BETWEEN s.starts_at AND s.ends_at) AS is_active_now,
		       EXISTS (
		           SELECT 1 FROM attendance_records ar
		           WHERE ar.session_id = s.id AND ar.student_id = $2 AND ar.verification_mode != 'absent'
		       ) AS has_checked_in
		FROM lecture_sessions s
		WHERE s.department_id = $1
		  AND s.starts_at >= CURRENT_DATE - INTERVAL '1 day'
		ORDER BY s.starts_at ASC
		LIMIT 10
	`

	rows, err := r.pool.Query(qCtx, query, departmentID, studentID)
	if err != nil {
		return nil, fmt.Errorf("failed querying student timetable: %w", err)
	}
	defer rows.Close()

	items := make([]StudentScheduleItem, 0, 8)
	for rows.Next() {
		var item StudentScheduleItem
		var sessionID string
		if err := rows.Scan(
			&sessionID,
			&item.SubjectName,
			&item.InstructorName,
			&item.DayOfWeek,
			&item.StartTime,
			&item.EndTime,
			&item.IsActiveNow,
			&item.HasCheckedIn,
		); err != nil {
			return nil, fmt.Errorf("failed scanning timetable item: %w", err)
		}
		item.SessionID = &sessionID
		items = append(items, item)
	}

	return items, nil
}

// GetStudentDepartment retrieves the department_id assigned to a student.
func (r *sqlRepository) GetStudentDepartment(ctx context.Context, studentID uuid.UUID) (*int, error) {
	qCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	var deptID *int
	query := `SELECT department_id FROM user_profiles WHERE id = $1 LIMIT 1`
	err := r.pool.QueryRow(qCtx, query, studentID).Scan(&deptID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed fetching student department: %w", err)
	}
	return deptID, nil
}
