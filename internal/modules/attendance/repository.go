package attendance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository specifies atomic database operations for lecture sessions, geofenced check-ins, and streak management.
type Repository interface {
	GetActiveSessionByID(ctx context.Context, sessionID uuid.UUID) (*LectureSession, error)
	GetSessionHistoricalDetails(ctx context.Context, sessionID uuid.UUID) (*LectureSession, error)
	VerifyStudentDepartment(ctx context.Context, studentID uuid.UUID, departmentID int) (bool, error)
	VerifyDeviceOwnership(ctx context.Context, studentID uuid.UUID, deviceID string) (bool, error)
	GetLastStudentLocation(ctx context.Context, studentID uuid.UUID) (*LastStudentLocation, error)
	AtomicInsertAttendance(ctx context.Context, record *AttendanceRecord) (int64, error)
	AtomicInsertOfflineAttendance(ctx context.Context, sessionID, studentID uuid.UUID, deviceID string, clientIP string, actualCheckInTime time.Time) (int64, error)
	IncrementUserStreak(ctx context.Context, studentID uuid.UUID) (int, error)
	GetStudentAttendanceHistory(ctx context.Context, studentID uuid.UUID) ([]AttendanceHistoryItem, error)
	UpdateDeviceLocation(ctx context.Context, userID uuid.UUID, deviceID string, lat, lng float64, ip string) error
	LogSecurityIncident(ctx context.Context, userID *uuid.UUID, eventType, severity string, details map[string]interface{}, ip *string) error
}

type sqlRepository struct {
	pool *pgxpool.Pool
}

// NewRepository initializes an attendance repository backed by pgxpool.
func NewRepository(pool *pgxpool.Pool) Repository {
	return &sqlRepository{pool: pool}
}

// GetActiveSessionByID retrieves an ongoing active session with geofence and secret data.
func (r *sqlRepository) GetActiveSessionByID(ctx context.Context, sessionID uuid.UUID) (*LectureSession, error) {
	qCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	query := `
		SELECT id, subject_name, instructor_name, department_id,
		       geofence_lat, geofence_lng, geofence_radius, qr_secret,
		       status, starts_at, ends_at, created_at
		FROM lecture_sessions
		WHERE id = $1 AND status = 'active'
		LIMIT 1
	`

	var s LectureSession
	err := r.pool.QueryRow(qCtx, query, sessionID).Scan(
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
		return nil, fmt.Errorf("failed to fetch active session: %w", err)
	}

	return &s, nil
}

// VerifyStudentDepartment confirms student eligibility for the session's department.
func (r *sqlRepository) VerifyStudentDepartment(ctx context.Context, studentID uuid.UUID, departmentID int) (bool, error) {
	qCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	query := `
		SELECT EXISTS (
			SELECT 1 FROM user_profiles
			WHERE id = $1 AND (department_id = $2 OR role = 'admin')
		)
	`

	var exists bool
	if err := r.pool.QueryRow(qCtx, query, studentID, departmentID).Scan(&exists); err != nil {
		return false, fmt.Errorf("failed to verify department enrollment: %w", err)
	}

	return exists, nil
}

// GetLastStudentLocation fetches the student's most recent valid GPS coordinate.
func (r *sqlRepository) GetLastStudentLocation(ctx context.Context, studentID uuid.UUID) (*LastStudentLocation, error) {
	qCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	query := `
		SELECT last_lat, last_lng, last_login_at
		FROM user_devices
		WHERE user_id = $1 AND last_lat IS NOT NULL AND last_lng IS NOT NULL
		ORDER BY last_login_at DESC
		LIMIT 1
	`

	var loc LastStudentLocation
	err := r.pool.QueryRow(qCtx, query, studentID).Scan(&loc.Lat, &loc.Lng, &loc.RecordedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to query last location: %w", err)
	}

	return &loc, nil
}

// AtomicInsertAttendance executes an atomic insert with zero race condition vulnerability.
// Returns the newly inserted ID, or 0 if conflict occurred (already checked in).
func (r *sqlRepository) AtomicInsertAttendance(ctx context.Context, record *AttendanceRecord) (int64, error) {
	qCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	query := `
		INSERT INTO attendance_records (
			session_id, student_id, device_id, verification_mode, accuracy_meters, client_ip, check_in_time
		) VALUES ($1, $2, $3, $4, $5, $6, NOW())
		ON CONFLICT (session_id, student_id) DO NOTHING
		RETURNING id;
	`

	var id int64
	err := r.pool.QueryRow(qCtx, query,
		record.SessionID,
		record.StudentID,
		record.DeviceID,
		record.VerificationMode,
		record.AccuracyMeters,
		record.ClientIP,
	).Scan(&id)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Zero rows returned due to ON CONFLICT DO NOTHING -> Already checked in!
			return 0, nil
		}
		return 0, fmt.Errorf("failed to execute atomic attendance insert: %w", err)
	}

	return id, nil
}

// IncrementUserStreak increments the student's continuous attendance streak counter.
func (r *sqlRepository) IncrementUserStreak(ctx context.Context, studentID uuid.UUID) (int, error) {
	qCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	query := `
		UPDATE user_profiles
		SET current_streak = current_streak + 1
		WHERE id = $1
		RETURNING current_streak;
	`

	var streak int
	if err := r.pool.QueryRow(qCtx, query, studentID).Scan(&streak); err != nil {
		return 0, fmt.Errorf("failed to increment user streak: %w", err)
	}

	return streak, nil
}

// GetStudentAttendanceHistory retrieves a chronological history of verified student attendance.
func (r *sqlRepository) GetStudentAttendanceHistory(ctx context.Context, studentID uuid.UUID) ([]AttendanceHistoryItem, error) {
	qCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	query := `
		SELECT a.id, a.session_id::text, s.subject_name, s.instructor_name,
		       a.check_in_time, a.verification_mode, a.accuracy_meters
		FROM attendance_records a
		JOIN lecture_sessions s ON a.session_id = s.id
		WHERE a.student_id = $1
		ORDER BY a.check_in_time DESC
		LIMIT 50;
	`

	rows, err := r.pool.Query(qCtx, query, studentID)
	if err != nil {
		return nil, fmt.Errorf("failed to query attendance history: %w", err)
	}
	defer rows.Close()

	items := make([]AttendanceHistoryItem, 0, 16)
	for rows.Next() {
		var item AttendanceHistoryItem
		if err := rows.Scan(
			&item.ID,
			&item.SessionID,
			&item.SubjectName,
			&item.InstructorName,
			&item.CheckInTime,
			&item.VerificationMode,
			&item.AccuracyMeters,
		); err != nil {
			return nil, fmt.Errorf("failed scanning attendance history row: %w", err)
		}
		items = append(items, item)
	}

	return items, nil
}

// UpdateDeviceLocation updates telemetric coordinates on the student's verified device.
func (r *sqlRepository) UpdateDeviceLocation(ctx context.Context, userID uuid.UUID, deviceID string, lat, lng float64, ip string) error {
	qCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	query := `
		UPDATE user_devices
		SET last_lat = $3,
		    last_lng = $4,
		    last_ip = $5,
		    last_login_at = NOW()
		WHERE user_id = $1 AND device_id = $2
	`

	_, err := r.pool.Exec(qCtx, query, userID, deviceID, lat, lng, ip)
	return err
}

// LogSecurityIncident writes an incident audit log entry for attendance anomalies.
func (r *sqlRepository) LogSecurityIncident(ctx context.Context, userID *uuid.UUID, eventType, severity string, details map[string]interface{}, ip *string) error {
	qCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	detailsBytes, err := json.Marshal(details)
	if err != nil {
		detailsBytes = []byte("{}")
	}

	query := `
		INSERT INTO security_audit_logs (user_id, event_type, severity, details, ip_address, created_at)
		VALUES ($1, $2, $3, $4, $5, NOW())
	`

	_, err = r.pool.Exec(qCtx, query, userID, eventType, severity, detailsBytes, ip)
	return err
}

// GetSessionHistoricalDetails retrieves session information regardless of its active/closed state.
func (r *sqlRepository) GetSessionHistoricalDetails(ctx context.Context, sessionID uuid.UUID) (*LectureSession, error) {
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
	err := r.pool.QueryRow(qCtx, query, sessionID).Scan(
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
		return nil, fmt.Errorf("failed fetching session historical details: %w", err)
	}

	return &s, nil
}

// VerifyDeviceOwnership confirms whether the device is bound and trusted for the given student.
func (r *sqlRepository) VerifyDeviceOwnership(ctx context.Context, studentID uuid.UUID, deviceID string) (bool, error) {
	qCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	query := `
		SELECT EXISTS (
			SELECT 1 FROM user_devices
			WHERE user_id = $1 AND device_id = $2 AND is_trusted = TRUE
		)
	`

	var exists bool
	if err := r.pool.QueryRow(qCtx, query, studentID, deviceID).Scan(&exists); err != nil {
		return false, fmt.Errorf("device ownership verification error: %w", err)
	}

	return exists, nil
}

// AtomicInsertOfflineAttendance atomically registers an offline proof with zero race vulnerability.
func (r *sqlRepository) AtomicInsertOfflineAttendance(ctx context.Context, sessionID, studentID uuid.UUID, deviceID string, clientIP string, actualCheckInTime time.Time) (int64, error) {
	qCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	query := `
		INSERT INTO attendance_records (
			session_id, student_id, device_id, verification_mode, accuracy_meters, client_ip, check_in_time
		) VALUES ($1, $2, $3, 'offline_proof', 0.0, $4, $5)
		ON CONFLICT (session_id, student_id) DO NOTHING
		RETURNING id;
	`

	var id int64
	err := r.pool.QueryRow(qCtx, query, sessionID, studentID, deviceID, clientIP, actualCheckInTime).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil
		}
		return 0, fmt.Errorf("atomic offline attendance insertion failed: %w", err)
	}

	return id, nil
}
