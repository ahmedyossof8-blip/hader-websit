package attendance

import (
	"time"

	"github.com/google/uuid"
)

// VerificationMode represents the verified authentication mode for attendance entry.
type VerificationMode string

const (
	ModeQRDynamicGPS   VerificationMode = "qr_dynamic_gps"
	ModeOfflineProof   VerificationMode = "offline_proof"
	ModeManualOverride VerificationMode = "manual_override"
	ModeAbsent         VerificationMode = "absent"
)

// AttendanceRecord mirrors the database entity for verified student check-ins.
type AttendanceRecord struct {
	ID               int64            `json:"id"`
	SessionID        uuid.UUID        `json:"session_id"`
	StudentID        uuid.UUID        `json:"student_id"`
	DeviceID         string           `json:"device_id"`
	CheckInTime      time.Time        `json:"check_in_time"`
	VerificationMode VerificationMode `json:"verification_mode"`
	AccuracyMeters   *float64         `json:"accuracy_meters"`
	ClientIP         *string          `json:"client_ip"`
}

// LectureSession represents an academic course session with geofence boundaries and dynamic TOTP secret.
type LectureSession struct {
	ID             uuid.UUID `json:"id"`
	SubjectName    string    `json:"subject_name"`
	InstructorName *string   `json:"instructor_name"`
	DepartmentID   *int      `json:"department_id"`
	GeofenceLat    float64   `json:"geofence_lat"`
	GeofenceLng    float64   `json:"geofence_lng"`
	GeofenceRadius int       `json:"geofence_radius"` // default: 40 meters
	QRSecret       string    `json:"qr_secret"`
	Status         string    `json:"status"` // 'active', 'closed', 'cancelled'
	StartsAt       time.Time `json:"starts_at"`
	EndsAt         time.Time `json:"ends_at"`
	CreatedAt      time.Time `json:"created_at"`
}

// LastStudentLocation holds the most recent geographical fix for impossible travel validation.
type LastStudentLocation struct {
	Lat        float64   `json:"lat"`
	Lng        float64   `json:"lng"`
	RecordedAt time.Time `json:"recorded_at"`
}
