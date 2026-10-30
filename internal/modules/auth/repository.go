package auth

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

// Repository specifies atomic persistence contracts for authentication and device security.
type Repository interface {
	CreateUserWithProfileAndDeviceTx(ctx context.Context, profile *UserProfile, device *UserDevice) error
	GetProfileByIdentifier(ctx context.Context, identifier string) (*UserProfile, error)
	GetProfileByID(ctx context.Context, id uuid.UUID) (*UserProfile, error)
	GetDeviceByUserAndDeviceID(ctx context.Context, userID uuid.UUID, deviceID string) (*UserDevice, error)
	CountUserDevices(ctx context.Context, userID uuid.UUID) (int, error)
	BindInitialDevice(ctx context.Context, device *UserDevice) error
	UpdateDeviceLoginMetadata(ctx context.Context, userID uuid.UUID, deviceID string, ip string, lat *float64, lng *float64) error
	LogSecurityIncident(ctx context.Context, incident *SecurityAuditLog) error
}

type sqlRepository struct {
	pool *pgxpool.Pool
}

// NewRepository instantiates an auth repository backed by pgxpool.
func NewRepository(pool *pgxpool.Pool) Repository {
	return &sqlRepository{pool: pool}
}

// CreateUserWithProfileAndDeviceTx atomically creates the user profile and initial trusted device binding.
func (r *sqlRepository) CreateUserWithProfileAndDeviceTx(ctx context.Context, profile *UserProfile, device *UserDevice) error {
	qCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	tx, err := r.pool.Begin(qCtx)
	if err != nil {
		return fmt.Errorf("failed to initiate database transaction: %w", err)
	}
	defer tx.Rollback(qCtx)

	// Ensure auth stub row exists in case standalone postgres is utilized
	_, _ = tx.Exec(qCtx, `INSERT INTO auth.users (id) VALUES ($1) ON CONFLICT (id) DO NOTHING`, profile.ID)

	// 1. Insert into user_profiles
	insertProfileSQL := `
		INSERT INTO user_profiles (
			id, academic_id, full_name, email, role, department_id, current_streak, is_active, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
	`
	_, err = tx.Exec(qCtx, insertProfileSQL,
		profile.ID,
		profile.AcademicID,
		profile.FullName,
		profile.Email,
		profile.Role,
		profile.DepartmentID,
		profile.CurrentStreak,
		profile.IsActive,
	)
	if err != nil {
		return fmt.Errorf("failed to insert user profile: %w", err)
	}

	// 2. Insert initial trusted hardware device
	insertDeviceSQL := `
		INSERT INTO user_devices (
			id, user_id, device_id, device_name, is_trusted, last_login_at, last_ip
		) VALUES (gen_random_uuid(), $1, $2, $3, $4, NOW(), $5)
	`
	_, err = tx.Exec(qCtx, insertDeviceSQL,
		profile.ID,
		device.DeviceID,
		device.DeviceName,
		true,
		device.LastIP,
	)
	if err != nil {
		return fmt.Errorf("failed to bind initial hardware device: %w", err)
	}

	if err := tx.Commit(qCtx); err != nil {
		return fmt.Errorf("transaction commit failed: %w", err)
	}

	return nil
}

// GetProfileByIdentifier retrieves a user profile by academic ID or email.
func (r *sqlRepository) GetProfileByIdentifier(ctx context.Context, identifier string) (*UserProfile, error) {
	qCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	query := `
		SELECT id, academic_id, full_name, email, role, department_id, current_streak, is_active, created_at
		FROM user_profiles
		WHERE LOWER(academic_id) = LOWER($1) OR LOWER(email) = LOWER($1)
		LIMIT 1
	`

	var p UserProfile
	err := r.pool.QueryRow(qCtx, query, identifier).Scan(
		&p.ID,
		&p.AcademicID,
		&p.FullName,
		&p.Email,
		&p.Role,
		&p.DepartmentID,
		&p.CurrentStreak,
		&p.IsActive,
		&p.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("error querying user profile: %w", err)
	}

	return &p, nil
}

// GetProfileByID retrieves a user profile by internal UUID.
func (r *sqlRepository) GetProfileByID(ctx context.Context, id uuid.UUID) (*UserProfile, error) {
	qCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	query := `
		SELECT id, academic_id, full_name, email, role, department_id, current_streak, is_active, created_at
		FROM user_profiles
		WHERE id = $1
		LIMIT 1
	`

	var p UserProfile
	err := r.pool.QueryRow(qCtx, query, id).Scan(
		&p.ID,
		&p.AcademicID,
		&p.FullName,
		&p.Email,
		&p.Role,
		&p.DepartmentID,
		&p.CurrentStreak,
		&p.IsActive,
		&p.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("error querying user profile by ID: %w", err)
	}

	return &p, nil
}

// GetDeviceByUserAndDeviceID fetches a device record matching (user_id, device_id).
func (r *sqlRepository) GetDeviceByUserAndDeviceID(ctx context.Context, userID uuid.UUID, deviceID string) (*UserDevice, error) {
	qCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	query := `
		SELECT id, user_id, device_id, device_name, is_trusted, last_login_at, last_ip, last_lat, last_lng
		FROM user_devices
		WHERE user_id = $1 AND device_id = $2
		LIMIT 1
	`

	var d UserDevice
	err := r.pool.QueryRow(qCtx, query, userID, deviceID).Scan(
		&d.ID,
		&d.UserID,
		&d.DeviceID,
		&d.DeviceName,
		&d.IsTrusted,
		&d.LastLoginAt,
		&d.LastIP,
		&d.LastLat,
		&d.LastLng,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("error checking device match: %w", err)
	}

	return &d, nil
}

// CountUserDevices counts the number of trusted hardware devices bound to a user.
func (r *sqlRepository) CountUserDevices(ctx context.Context, userID uuid.UUID) (int, error) {
	qCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	query := `SELECT COUNT(*) FROM user_devices WHERE user_id = $1 AND is_trusted = TRUE`
	var count int
	if err := r.pool.QueryRow(qCtx, query, userID).Scan(&count); err != nil {
		return 0, fmt.Errorf("error counting user devices: %w", err)
	}

	return count, nil
}

// BindInitialDevice registers a hardware device as the trusted primary device.
func (r *sqlRepository) BindInitialDevice(ctx context.Context, device *UserDevice) error {
	qCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	query := `
		INSERT INTO user_devices (
			id, user_id, device_id, device_name, is_trusted, last_login_at, last_ip
		) VALUES (gen_random_uuid(), $1, $2, $3, TRUE, NOW(), $4)
		ON CONFLICT (user_id, device_id)
		DO UPDATE SET is_trusted = TRUE, last_login_at = NOW(), last_ip = EXCLUDED.last_ip
	`

	_, err := r.pool.Exec(qCtx, query,
		device.UserID,
		device.DeviceID,
		device.DeviceName,
		device.LastIP,
	)
	if err != nil {
		return fmt.Errorf("failed to bind initial device: %w", err)
	}

	return nil
}

// UpdateDeviceLoginMetadata updates telemetric data on successful authenticated login.
func (r *sqlRepository) UpdateDeviceLoginMetadata(ctx context.Context, userID uuid.UUID, deviceID string, ip string, lat *float64, lng *float64) error {
	qCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	query := `
		UPDATE user_devices
		SET last_login_at = NOW(),
		    last_ip = $3,
		    last_lat = COALESCE($4, last_lat),
		    last_lng = COALESCE($5, last_lng)
		WHERE user_id = $1 AND device_id = $2
	`

	_, err := r.pool.Exec(qCtx, query, userID, deviceID, ip, lat, lng)
	if err != nil {
		return fmt.Errorf("failed to update device login telemetry: %w", err)
	}

	return nil
}

// LogSecurityIncident writes an incident audit log entry for security anomalies.
func (r *sqlRepository) LogSecurityIncident(ctx context.Context, incident *SecurityAuditLog) error {
	qCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	detailsJSON, err := json.Marshal(incident.Details)
	if err != nil {
		detailsJSON = []byte("{}")
	}

	query := `
		INSERT INTO security_audit_logs (user_id, event_type, severity, details, ip_address, created_at)
		VALUES ($1, $2, $3, $4, $5, NOW())
	`

	_, err = r.pool.Exec(qCtx, query,
		incident.UserID,
		incident.EventType,
		incident.Severity,
		detailsJSON,
		incident.IPAddress,
	)
	if err != nil {
		return fmt.Errorf("failed to write security audit log: %w", err)
	}

	return nil
}
