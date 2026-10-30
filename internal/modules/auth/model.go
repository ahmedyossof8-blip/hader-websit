package auth

import (
	"time"

	"github.com/google/uuid"
)

// UserRole represents permitted authorization roles across the Hader-V2 platform.
type UserRole string

const (
	RoleStudent     UserRole = "student"
	RoleClassLeader UserRole = "class_leader"
	RoleAdmin       UserRole = "admin"
)

// UserProfile mirrors the database entity for academic users extending Supabase Auth.
type UserProfile struct {
	ID            uuid.UUID `json:"id"`
	AcademicID    string    `json:"academic_id"`
	FullName      string    `json:"full_name"`
	Email         string    `json:"email"`
	Role          string    `json:"role"`
	DepartmentID  *int      `json:"department_id"`
	CurrentStreak int       `json:"current_streak"`
	IsActive      bool      `json:"is_active"`
	CreatedAt     time.Time `json:"created_at"`
}

// UserDevice models pinned hardware fingerprints for anti-fraud device locking.
type UserDevice struct {
	ID          uuid.UUID  `json:"id"`
	UserID      uuid.UUID  `json:"user_id"`
	DeviceID    string     `json:"device_id"`
	DeviceName  *string    `json:"device_name"`
	IsTrusted   bool       `json:"is_trusted"`
	LastLoginAt time.Time  `json:"last_login_at"`
	LastIP      *string    `json:"last_ip"`
	LastLat     *float64   `json:"last_lat"`
	LastLng     *float64   `json:"last_lng"`
}

// SecurityAuditLog tracks critical access anomalies, fraud telemetry, and lock mismatches.
type SecurityAuditLog struct {
	ID        int64                  `json:"id"`
	UserID    *uuid.UUID             `json:"user_id"`
	EventType string                 `json:"event_type"`
	Severity  string                 `json:"severity"` // LOW, MEDIUM, HIGH, CRITICAL
	Details   map[string]interface{} `json:"details"`
	IPAddress *string                `json:"ip_address"`
	CreatedAt time.Time              `json:"created_at"`
}
