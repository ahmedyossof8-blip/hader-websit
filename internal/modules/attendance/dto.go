package attendance

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// CheckInRequest captures the student's dynamic QR scan, GPS coordinates, and hardware lock.
type CheckInRequest struct {
	SessionID      uuid.UUID `json:"session_id"`
	QRCode         string    `json:"qr_code"`
	Latitude       float64   `json:"latitude"`
	Longitude      float64   `json:"longitude"`
	AccuracyMeters float64   `json:"accuracy_meters"`
	DeviceID       string    `json:"device_id"`
}

// Validate ensures structural validity of the check-in parameters before cryptographic checks.
func (r *CheckInRequest) Validate() error {
	if r.SessionID == uuid.Nil {
		return errors.New("session_id is mandatory")
	}

	r.QRCode = strings.TrimSpace(r.QRCode)
	if r.QRCode == "" {
		return errors.New("qr_code token is required")
	}

	if r.Latitude < -90.0 || r.Latitude > 90.0 {
		return errors.New("latitude must be between -90.0 and 90.0")
	}

	if r.Longitude < -180.0 || r.Longitude > 180.0 {
		return errors.New("longitude must be between -180.0 and 180.0")
	}

	if r.AccuracyMeters < 0 {
		return errors.New("accuracy_meters cannot be negative")
	}

	r.DeviceID = strings.TrimSpace(r.DeviceID)
	if r.DeviceID == "" {
		return errors.New("device_id is required for attendance validation")
	}

	return nil
}

// CheckInResponse confirms verified attendance registration.
type CheckInResponse struct {
	AttendanceID  int64     `json:"attendance_id"`
	CheckInTime   time.Time `json:"check_in_time"`
	Status        string    `json:"status"` // "present"
	SubjectName   string    `json:"subject_name"`
	CurrentStreak int       `json:"current_streak"`
}

// SessionQRResponse provides the dynamic rotating token and countdown for Class Leader broadcast screens.
type SessionQRResponse struct {
	SessionID        uuid.UUID `json:"session_id"`
	CurrentToken     string    `json:"current_token"`
	ExpiresInSeconds int64     `json:"expires_in_seconds"`
	Window           int64     `json:"window"`
}

// AttendanceHistoryItem provides a concise entry in the student's personal attendance ledger.
type AttendanceHistoryItem struct {
	ID               int64     `json:"id"`
	SessionID        string    `json:"session_id"`
	SubjectName      string    `json:"subject_name"`
	InstructorName   *string   `json:"instructor_name"`
	CheckInTime      time.Time `json:"check_in_time"`
	VerificationMode string    `json:"verification_mode"`
	AccuracyMeters   *float64  `json:"accuracy_meters"`
}

// OfflineSyncItem represents a cryptographically signed offline attendance proof captured by PWA.
type OfflineSyncItem struct {
	SessionID      uuid.UUID `json:"session_id"`
	ScannedQRCode  string    `json:"scanned_qr_code"`
	ScanTimestamp  int64     `json:"scan_timestamp"`
	ProofSignature string    `json:"proof_signature"`
	DeviceID       string    `json:"device_id"`
	ClientIP       string    `json:"-"`
}

// BatchOfflineSyncRequest accepts multiple offline proofs (bounded between 1 and 5 items).
type BatchOfflineSyncRequest struct {
	Proofs []OfflineSyncItem `json:"proofs"`
}

// Validate ensures non-empty and bounded batch payload.
func (r *BatchOfflineSyncRequest) Validate() error {
	if len(r.Proofs) == 0 {
		return errors.New("proofs array cannot be empty")
	}
	if len(r.Proofs) > 5 {
		return errors.New("maximum 5 offline proofs permitted per sync batch")
	}

	for i, item := range r.Proofs {
		if item.SessionID == uuid.Nil {
			return errors.New("session_id is mandatory on item " + string(rune('1'+i)))
		}
		if strings.TrimSpace(item.ScannedQRCode) == "" {
			return errors.New("scanned_qr_code is mandatory on all items")
		}
		if item.ScanTimestamp <= 0 {
			return errors.New("scan_timestamp must be a valid positive epoch second timestamp")
		}
		if strings.TrimSpace(item.ProofSignature) == "" {
			return errors.New("proof_signature is required for offline validation")
		}
		if strings.TrimSpace(item.DeviceID) == "" {
			return errors.New("device_id is required on all items")
		}
	}

	return nil
}

// OfflineSyncResultItem reports the verification verdict for an individual offline item.
type OfflineSyncResultItem struct {
	SessionID    uuid.UUID `json:"session_id"`
	Status       string    `json:"status"` // "synced" | "rejected"
	AttendanceID *int64    `json:"attendance_id,omitempty"`
	Reason       string    `json:"reason,omitempty"`
}

// BatchOfflineSyncResponse aggregates results across the submitted offline proofs.
type BatchOfflineSyncResponse struct {
	TotalReceived int                     `json:"total_received"`
	TotalAccepted int                     `json:"total_accepted"`
	Results       []OfflineSyncResultItem `json:"results"`
}
