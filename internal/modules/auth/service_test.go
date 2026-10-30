package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"hader-backend/internal/config"
)

// mockAuthRepository implements Repository interface for isolated business rule testing.
type mockAuthRepository struct {
	profiles       map[string]*UserProfile
	devices        map[string]*UserDevice
	incidents      []*SecurityAuditLog
	deviceCount    map[uuid.UUID]int
	createTxCalled bool
}

func newMockRepository() *mockAuthRepository {
	return &mockAuthRepository{
		profiles:    make(map[string]*UserProfile),
		devices:     make(map[string]*UserDevice),
		incidents:   make([]*SecurityAuditLog, 0),
		deviceCount: make(map[uuid.UUID]int),
	}
}

func (m *mockAuthRepository) CreateUserWithProfileAndDeviceTx(ctx context.Context, profile *UserProfile, device *UserDevice) error {
	m.createTxCalled = true
	m.profiles[profile.AcademicID] = profile
	m.profiles[profile.Email] = profile
	m.devices[profile.ID.String()+":"+device.DeviceID] = device
	m.deviceCount[profile.ID] = 1
	return nil
}

func (m *mockAuthRepository) GetProfileByIdentifier(ctx context.Context, identifier string) (*UserProfile, error) {
	if p, ok := m.profiles[identifier]; ok {
		return p, nil
	}
	return nil, nil
}

func (m *mockAuthRepository) GetProfileByID(ctx context.Context, id uuid.UUID) (*UserProfile, error) {
	for _, p := range m.profiles {
		if p.ID == id {
			return p, nil
		}
	}
	return nil, nil
}

func (m *mockAuthRepository) GetDeviceByUserAndDeviceID(ctx context.Context, userID uuid.UUID, deviceID string) (*UserDevice, error) {
	key := userID.String() + ":" + deviceID
	if d, ok := m.devices[key]; ok {
		return d, nil
	}
	return nil, nil
}

func (m *mockAuthRepository) CountUserDevices(ctx context.Context, userID uuid.UUID) (int, error) {
	return m.deviceCount[userID], nil
}

func (m *mockAuthRepository) BindInitialDevice(ctx context.Context, device *UserDevice) error {
	key := device.UserID.String() + ":" + device.DeviceID
	m.devices[key] = device
	m.deviceCount[device.UserID]++
	return nil
}

func (m *mockAuthRepository) UpdateDeviceLoginMetadata(ctx context.Context, userID uuid.UUID, deviceID string, ip string, lat *float64, lng *float64) error {
	key := userID.String() + ":" + deviceID
	if d, ok := m.devices[key]; ok {
		d.LastIP = &ip
		d.LastLoginAt = time.Now()
	}
	return nil
}

func (m *mockAuthRepository) LogSecurityIncident(ctx context.Context, incident *SecurityAuditLog) error {
	m.incidents = append(m.incidents, incident)
	return nil
}

func TestRegisterValidation(t *testing.T) {
	// 1. Missing Terms agreement must fail
	req := RegisterRequest{
		FullName:     "أحمد محمد علي حسن",
		AcademicID:   "20230894",
		Email:        "ahmed.hassan@edu.eg",
		Password:     "SecurePass123",
		DepartmentID: 2,
		DeviceID:     "sha256-fingerprint-device-1",
		TermsAgreed:  false,
	}

	if err := req.Validate(); err == nil {
		t.Errorf("Expected validation failure when TermsAgreed is false")
	}

	// 2. Short name (< 8 characters) must fail
	req.TermsAgreed = true
	req.FullName = "أحمد"
	if err := req.Validate(); err == nil {
		t.Errorf("Expected validation failure for short quadruple name")
	}

	// 3. Valid payload must pass
	req.FullName = "أحمد محمد علي حسن"
	if err := req.Validate(); err != nil {
		t.Errorf("Expected valid payload to pass, got: %v", err)
	}
}

func TestDualRoleEnforcement(t *testing.T) {
	repo := newMockRepository()
	cfg := &config.Config{
		SupabaseJWTSecret: "test-secret-1234567890-test-secret",
	}
	svc := NewService(repo, cfg)

	studentUID := uuid.New()
	deptID := 1
	repo.profiles["20230001"] = &UserProfile{
		ID:           studentUID,
		AcademicID:   "20230001",
		FullName:     "علي مصطفى إبراهيم",
		Email:        "ali@edu.eg",
		Role:         string(RoleStudent),
		DepartmentID: &deptID,
		IsActive:     true,
	}
	repo.devices[studentUID.String()+":device-123"] = &UserDevice{
		UserID:    studentUID,
		DeviceID:  "device-123",
		IsTrusted: true,
	}
	repo.deviceCount[studentUID] = 1

	// Student tries logging in as class_leader -> Must be rejected with 403 / ErrInsufficientRolePrivileges
	loginReq := &LoginRequest{
		Identifier: "20230001",
		Password:   "Password123",
		LoginRole:  string(RoleClassLeader),
		DeviceID:   "device-123",
	}

	_, _, err := svc.Login(context.Background(), loginReq, "127.0.0.1")
	if !errors.Is(err, ErrInsufficientRolePrivileges) {
		t.Fatalf("Expected ErrInsufficientRolePrivileges, got: %v", err)
	}

	// Security incident must be recorded
	if len(repo.incidents) == 0 || repo.incidents[0].EventType != "UNAUTHORIZED_ROLE_ACCESS" {
		t.Errorf("Expected UNAUTHORIZED_ROLE_ACCESS incident to be logged")
	}
}

func TestHardwareDevicePinning(t *testing.T) {
	repo := newMockRepository()
	cfg := &config.Config{
		SupabaseJWTSecret: "test-secret-1234567890-test-secret",
	}
	svc := NewService(repo, cfg)

	studentUID := uuid.New()
	deptID := 2
	repo.profiles["20230099"] = &UserProfile{
		ID:           studentUID,
		AcademicID:   "20230099",
		FullName:     "محمود حسن رضوان",
		Email:        "mahmoud@edu.eg",
		Role:         string(RoleStudent),
		DepartmentID: &deptID,
		IsActive:     true,
	}

	// Bind Device A as trusted
	repo.devices[studentUID.String()+":hardware-device-A"] = &UserDevice{
		UserID:    studentUID,
		DeviceID:  "hardware-device-A",
		IsTrusted: true,
	}
	repo.deviceCount[studentUID] = 1

	// Scenario 1: Same user attempts to login from untrusted Device B -> Block with DEVICE_LOCKED_MISMATCH
	loginDeviceB := &LoginRequest{
		Identifier: "20230099",
		Password:   "Password123",
		LoginRole:  string(RoleStudent),
		DeviceID:   "hardware-device-B-fraudulent",
	}

	_, _, err := svc.Login(context.Background(), loginDeviceB, "192.168.1.100")
	if !errors.Is(err, ErrDeviceLockedMismatch) {
		t.Fatalf("Expected ErrDeviceLockedMismatch, got: %v", err)
	}

	// Incident audit log must be persisted with HIGH severity
	if len(repo.incidents) == 0 {
		t.Fatalf("Expected DEVICE_MISMATCH incident to be logged")
	}
	incident := repo.incidents[len(repo.incidents)-1]
	if incident.EventType != "DEVICE_MISMATCH" || incident.Severity != "HIGH" {
		t.Errorf("Unexpected incident metadata: %v", incident)
	}

	// Scenario 2: Login from legitimate Device A -> Allowed
	loginDeviceA := &LoginRequest{
		Identifier: "20230099",
		Password:   "Password123",
		LoginRole:  string(RoleStudent),
		DeviceID:   "hardware-device-A",
	}

	resp, _, err := svc.Login(context.Background(), loginDeviceA, "192.168.1.100")
	if err != nil {
		t.Fatalf("Expected successful login from legitimate Device A, got: %v", err)
	}
	if !resp.DeviceLocked || resp.User.AcademicID != "20230099" {
		t.Errorf("Unexpected login response payload: %v", resp)
	}
}
