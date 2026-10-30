package sessions

import (
	"time"

	"github.com/google/uuid"
)

// SessionStatus defines the lifecycle states of an academic lecture session.
type SessionStatus string

const (
	StatusActive    SessionStatus = "active"
	StatusClosed    SessionStatus = "closed"
	StatusCancelled SessionStatus = "cancelled"
)

// LectureSession mirrors the database entity for academic lectures and geofenced sessions.
type LectureSession struct {
	ID             uuid.UUID     `json:"id"`
	SubjectName    string        `json:"subject_name"`
	InstructorName *string       `json:"instructor_name"`
	DepartmentID   *int          `json:"department_id"`
	GeofenceLat    float64       `json:"geofence_lat"`
	GeofenceLng    float64       `json:"geofence_lng"`
	GeofenceRadius int           `json:"geofence_radius"`
	QRSecret       string        `json:"qr_secret"`
	Status         SessionStatus `json:"status"`
	StartsAt       time.Time     `json:"starts_at"`
	EndsAt         time.Time     `json:"ends_at"`
	CreatedAt      time.Time     `json:"created_at"`
}

// AttendeeRecord models an individual student check-in event for real-time monitoring.
type AttendeeRecord struct {
	StudentID        uuid.UUID `json:"student_id"`
	AcademicID       string    `json:"academic_id"`
	FullName         string    `json:"full_name"`
	CheckInTime      time.Time `json:"check_in_time"`
	VerificationMode string    `json:"verification_mode"`
}
