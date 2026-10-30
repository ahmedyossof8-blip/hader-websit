package auth

import (
	"errors"
	"net/mail"
	"strings"
	"unicode/utf8"
)

// RegisterRequest contains registration fields with mandatory terms agreement.
type RegisterRequest struct {
	FullName     string `json:"full_name"`
	AcademicID   string `json:"academic_id"`
	Email        string `json:"email"`
	Password     string `json:"password"`
	DepartmentID int    `json:"department_id"`
	DeviceID     string `json:"device_id"`
	DeviceName   string `json:"device_name"`
	TermsAgreed  bool   `json:"terms_agreed"`
}

// Validate executes strict business input validation on the registration payload.
func (r *RegisterRequest) Validate() error {
	r.FullName = strings.TrimSpace(r.FullName)
	if utf8.RuneCountInString(r.FullName) < 8 {
		return errors.New("full_name must be at least 8 characters (quadruple academic name)")
	}

	r.AcademicID = strings.TrimSpace(r.AcademicID)
	if r.AcademicID == "" {
		return errors.New("academic_id is mandatory")
	}

	r.Email = strings.TrimSpace(strings.ToLower(r.Email))
	if _, err := mail.ParseAddress(r.Email); err != nil {
		return errors.New("invalid email address format")
	}

	if len(r.Password) < 8 {
		return errors.New("password must be at least 8 characters")
	}

	if r.DepartmentID <= 0 {
		return errors.New("department_id must be a valid positive identifier")
	}

	r.DeviceID = strings.TrimSpace(r.DeviceID)
	if r.DeviceID == "" {
		return errors.New("device_id hardware fingerprint is required")
	}

	if !r.TermsAgreed {
		return errors.New("you must explicitly agree to data verification and device pinning terms")
	}

	return nil
}

// LoginRequest handles dual-role credentials and hardware device verification.
type LoginRequest struct {
	Identifier string `json:"identifier"` // Academic ID or Email
	Password   string `json:"password"`
	LoginRole  string `json:"login_role"` // 'student' | 'class_leader'
	DeviceID   string `json:"device_id"`   // SHA-256 fingerprint
	DeviceName string `json:"device_name"`
	RememberMe bool   `json:"remember_me"`
}

// Validate inspects the login payload parameters.
func (r *LoginRequest) Validate() error {
	r.Identifier = strings.TrimSpace(r.Identifier)
	if r.Identifier == "" {
		return errors.New("identifier (academic ID or email) is required")
	}

	if r.Password == "" {
		return errors.New("password is required")
	}

	r.LoginRole = strings.TrimSpace(strings.ToLower(r.LoginRole))
	if r.LoginRole != string(RoleStudent) && r.LoginRole != string(RoleClassLeader) {
		return errors.New("login_role must be either 'student' or 'class_leader'")
	}

	r.DeviceID = strings.TrimSpace(r.DeviceID)
	if r.DeviceID == "" {
		return errors.New("device_id is required for hardware lock verification")
	}

	return nil
}

// PasswordRecoveryRequest initiates a password reset for an academic email.
type PasswordRecoveryRequest struct {
	Email string `json:"email"`
}

// Validate checks email format for password recovery.
func (r *PasswordRecoveryRequest) Validate() error {
	r.Email = strings.TrimSpace(strings.ToLower(r.Email))
	if _, err := mail.ParseAddress(r.Email); err != nil {
		return errors.New("invalid email address format")
	}
	return nil
}

// UserResponse provides a safe profile representation omitting sensitive internals.
type UserResponse struct {
	ID            string `json:"id"`
	AcademicID    string `json:"academic_id"`
	FullName      string `json:"full_name"`
	Email         string `json:"email"`
	Role          string `json:"role"`
	DepartmentID  *int   `json:"department_id"`
	CurrentStreak int    `json:"current_streak"`
	IsActive      bool   `json:"is_active"`
}

// AuthResponse delivers the authentication token and authenticated identity payload.
type AuthResponse struct {
	Token        string       `json:"token"`
	User         UserResponse `json:"user"`
	DeviceLocked bool         `json:"device_locked"`
}
