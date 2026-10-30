package attendance

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"hader-backend/internal/utils"
)

// mockAttendanceRepository implements attendance Repository in-memory for testing.
type mockAttendanceRepository struct {
	session        *LectureSession
	enrolled       bool
	lastLocation   *LastStudentLocation
	checkedInUsers map[string]bool
	streak         int
	incidents      []string
}

func newMockAttendanceRepo(session *LectureSession) *mockAttendanceRepository {
	return &mockAttendanceRepository{
		session:        session,
		enrolled:       true,
		checkedInUsers: make(map[string]bool),
		streak:         3,
		incidents:      make([]string, 0),
	}
}

func (m *mockAttendanceRepository) GetActiveSessionByID(ctx context.Context, sessionID uuid.UUID) (*LectureSession, error) {
	if m.session != nil && m.session.ID == sessionID {
		return m.session, nil
	}
	return nil, nil
}

func (m *mockAttendanceRepository) GetSessionHistoricalDetails(ctx context.Context, sessionID uuid.UUID) (*LectureSession, error) {
	if m.session != nil && m.session.ID == sessionID {
		return m.session, nil
	}
	return nil, nil
}

func (m *mockAttendanceRepository) VerifyStudentDepartment(ctx context.Context, studentID uuid.UUID, departmentID int) (bool, error) {
	return m.enrolled, nil
}

func (m *mockAttendanceRepository) VerifyDeviceOwnership(ctx context.Context, studentID uuid.UUID, deviceID string) (bool, error) {
	if deviceID == "unauthorized-device" {
		return false, nil
	}
	return true, nil
}

func (m *mockAttendanceRepository) GetLastStudentLocation(ctx context.Context, studentID uuid.UUID) (*LastStudentLocation, error) {
	return m.lastLocation, nil
}

func (m *mockAttendanceRepository) AtomicInsertAttendance(ctx context.Context, record *AttendanceRecord) (int64, error) {
	key := record.SessionID.String() + ":" + record.StudentID.String()
	if m.checkedInUsers[key] {
		// Conflict simulated!
		return 0, nil
	}
	m.checkedInUsers[key] = true
	return 101, nil
}

func (m *mockAttendanceRepository) AtomicInsertOfflineAttendance(ctx context.Context, sessionID, studentID uuid.UUID, deviceID string, clientIP string, actualCheckInTime time.Time) (int64, error) {
	key := sessionID.String() + ":" + studentID.String()
	if m.checkedInUsers[key] {
		// Conflict simulated!
		return 0, nil
	}
	m.checkedInUsers[key] = true
	return 202, nil
}

func (m *mockAttendanceRepository) IncrementUserStreak(ctx context.Context, studentID uuid.UUID) (int, error) {
	m.streak++
	return m.streak, nil
}

func (m *mockAttendanceRepository) GetStudentAttendanceHistory(ctx context.Context, studentID uuid.UUID) ([]AttendanceHistoryItem, error) {
	return []AttendanceHistoryItem{}, nil
}

func (m *mockAttendanceRepository) UpdateDeviceLocation(ctx context.Context, userID uuid.UUID, deviceID string, lat, lng float64, ip string) error {
	return nil
}

func (m *mockAttendanceRepository) LogSecurityIncident(ctx context.Context, userID *uuid.UUID, eventType, severity string, details map[string]interface{}, ip *string) error {
	m.incidents = append(m.incidents, eventType)
	return nil
}

func TestAttendanceAntiFraudPipeline(t *testing.T) {
	sessionID := uuid.New()
	studentID := uuid.New()
	qrSecret := "test-lecture-totp-qr-secret-key-123"
	hallLat := 30.0276
	hallLng := 31.2101
	radius := 40 // 40 meters

	now := time.Now()
	session := &LectureSession{
		ID:             sessionID,
		SubjectName:    "هندسة البرمجيات الموزعة",
		GeofenceLat:    hallLat,
		GeofenceLng:    hallLng,
		GeofenceRadius: radius,
		QRSecret:       qrSecret,
		Status:         "active",
		StartsAt:       now.Add(-30 * time.Minute),
		EndsAt:         now.Add(90 * time.Minute),
	}

	repo := newMockAttendanceRepo(session)
	svc := NewService(repo)

	validQR, _, err := utils.GenerateDynamicTOTP(qrSecret, 5)
	if err != nil {
		t.Fatalf("Failed to generate TOTP: %v", err)
	}

	// Case 1: Mock GPS (accuracy == 0.0) must be rejected
	reqMockGPS := &CheckInRequest{
		SessionID:      sessionID,
		QRCode:         validQR,
		Latitude:       hallLat,
		Longitude:      hallLng,
		AccuracyMeters: 0.0,
		DeviceID:       "device-fingerprint-123",
	}
	_, err = svc.ProcessCheckIn(context.Background(), studentID, reqMockGPS, "127.0.0.1")
	if !errors.Is(err, ErrPoorGPSAccuracyOrMock) {
		t.Errorf("Expected ErrPoorGPSAccuracyOrMock for 0.0m accuracy, got %v", err)
	}

	// Case 2: Geofence violation (outside 40m radius) must be rejected
	reqOutOfBounds := &CheckInRequest{
		SessionID:      sessionID,
		QRCode:         validQR,
		Latitude:       30.0444, // ~3.1 km away
		Longitude:      31.2357,
		AccuracyMeters: 10.0,
		DeviceID:       "device-fingerprint-123",
	}
	_, err = svc.ProcessCheckIn(context.Background(), studentID, reqOutOfBounds, "127.0.0.1")
	if !errors.Is(err, ErrOutsideLectureGeofence) {
		t.Errorf("Expected ErrOutsideLectureGeofence, got %v", err)
	}

	// Case 3: Impossible Travel (10km in 2 minutes) must be blocked
	repo.lastLocation = &LastStudentLocation{
		Lat:        30.1200,
		Lng:        31.2101,
		RecordedAt: now.Add(-2 * time.Minute), // 10km away 2 mins ago!
	}
	reqInside := &CheckInRequest{
		SessionID:      sessionID,
		QRCode:         validQR,
		Latitude:       hallLat,
		Longitude:      hallLng,
		AccuracyMeters: 8.0,
		DeviceID:       "device-fingerprint-123",
	}
	_, err = svc.ProcessCheckIn(context.Background(), studentID, reqInside, "127.0.0.1")
	if !errors.Is(err, ErrImpossibleTravelDetected) {
		t.Errorf("Expected ErrImpossibleTravelDetected, got %v", err)
	}

	// Case 4: Expired or invalid QR code must be rejected
	repo.lastLocation = nil // Clear impossible travel
	reqBadQR := &CheckInRequest{
		SessionID:      sessionID,
		QRCode:         "999999", // Invalid OTP
		Latitude:       hallLat,
		Longitude:      hallLng,
		AccuracyMeters: 8.0,
		DeviceID:       "device-fingerprint-123",
	}
	_, err = svc.ProcessCheckIn(context.Background(), studentID, reqBadQR, "127.0.0.1")
	if !errors.Is(err, ErrInvalidOrExpiredQR) {
		t.Errorf("Expected ErrInvalidOrExpiredQR, got %v", err)
	}

	// Case 5: Legitimate Check-in must succeed
	reqValid := &CheckInRequest{
		SessionID:      sessionID,
		QRCode:         validQR,
		Latitude:       hallLat,
		Longitude:      hallLng,
		AccuracyMeters: 8.0,
		DeviceID:       "device-fingerprint-123",
	}
	resp, err := svc.ProcessCheckIn(context.Background(), studentID, reqValid, "127.0.0.1")
	if err != nil {
		t.Fatalf("Expected check-in to succeed, got %v", err)
	}
	if resp.AttendanceID != 101 || resp.Status != "present" || resp.CurrentStreak != 4 {
		t.Errorf("Unexpected check-in response: %+v", resp)
	}

	// Case 6: Duplicate Check-in in the same session must be rejected with ALREADY_CHECKED_IN
	_, err = svc.ProcessCheckIn(context.Background(), studentID, reqValid, "127.0.0.1")
	if !errors.Is(err, ErrAlreadyCheckedIn) {
		t.Errorf("Expected ErrAlreadyCheckedIn on repeated check-in, got %v", err)
	}
}

func TestDynamicQRForLeader(t *testing.T) {
	sessionID := uuid.New()
	leaderID := uuid.New()
	qrSecret := "test-leader-totp-qr-secret"

	now := time.Now()
	session := &LectureSession{
		ID:             sessionID,
		SubjectName:    "تراكيب البيانات والخوارزميات",
		GeofenceLat:    30.0,
		GeofenceLng:    31.0,
		GeofenceRadius: 40,
		QRSecret:       qrSecret,
		Status:         "active",
		StartsAt:       now.Add(-10 * time.Minute),
		EndsAt:         now.Add(50 * time.Minute),
	}

	repo := newMockAttendanceRepo(session)
	svc := NewService(repo)

	qrResp, err := svc.GetDynamicQRForLeader(context.Background(), leaderID, sessionID)
	if err != nil {
		t.Fatalf("Failed to retrieve dynamic QR for leader: %v", err)
	}

	if len(qrResp.CurrentToken) != 6 {
		t.Errorf("Expected 6-digit TOTP token, got: %s", qrResp.CurrentToken)
	}

	if qrResp.ExpiresInSeconds <= 0 || qrResp.ExpiresInSeconds > 5 {
		t.Errorf("Invalid expiration countdown: %d", qrResp.ExpiresInSeconds)
	}

	// Verify generated token matches ValidateDynamicTOTP
	if !utils.ValidateDynamicTOTP(qrSecret, qrResp.CurrentToken, 5) {
		t.Errorf("Generated token failed self-validation")
	}
}

func TestSyncOfflineProofs(t *testing.T) {
	sessionID := uuid.New()
	studentID := uuid.New()
	deviceID := "trusted-hardware-device-555"
	qrSecret := "offline-test-qr-secret-888"

	now := time.Now()
	session := &LectureSession{
		ID:             sessionID,
		SubjectName:    "الأمن السيبراني والتحليل الجنائي",
		GeofenceLat:    30.0,
		GeofenceLng:    31.0,
		GeofenceRadius: 40,
		QRSecret:       qrSecret,
		Status:         "closed", // Offline sync typically occurs after session ends
		StartsAt:       now.Add(-60 * time.Minute),
		EndsAt:         now.Add(-10 * time.Minute),
	}

	repo := newMockAttendanceRepo(session)
	svc := NewService(repo)

	// Valid scan timestamp (20 minutes ago, strictly inside session window)
	validScanTimestamp := now.Add(-20 * time.Minute).Unix()
	validWindow := validScanTimestamp / 5
	validTOTP, err := utils.ComputeTOTPForWindow(qrSecret, validWindow)
	if err != nil {
		t.Fatalf("Failed computing TOTP code: %v", err)
	}

	validSig := utils.GenerateOfflineProofSignature(deviceID, sessionID.String(), validTOTP, validScanTimestamp)

	// Case 1: Valid offline proof submission
	validBatch := &BatchOfflineSyncRequest{
		Proofs: []OfflineSyncItem{
			{
				SessionID:      sessionID,
				ScannedQRCode:  validTOTP,
				ScanTimestamp:  validScanTimestamp,
				ProofSignature: validSig,
				DeviceID:       deviceID,
			},
		},
	}

	resp, err := svc.SyncOfflineProofs(context.Background(), studentID, validBatch, "192.168.1.50")
	if err != nil {
		t.Fatalf("SyncOfflineProofs error: %v", err)
	}

	if resp.TotalAccepted != 1 || len(resp.Results) != 1 || resp.Results[0].Status != "synced" {
		t.Fatalf("Expected 1 accepted sync item, got: %+v", resp)
	}

	// Case 2: Duplicate/Replay attempt of already processed proof
	respDup, _ := svc.SyncOfflineProofs(context.Background(), studentID, validBatch, "192.168.1.50")
	if respDup.TotalAccepted != 0 || respDup.Results[0].Status != "rejected" {
		t.Errorf("Expected duplicate replay proof to be rejected")
	}

	// Reset checked-in users for subsequent negative vector tests
	repo.checkedInUsers = make(map[string]bool)

	// Case 3: Forged cryptographic signature
	forgedBatch := &BatchOfflineSyncRequest{
		Proofs: []OfflineSyncItem{
			{
				SessionID:      sessionID,
				ScannedQRCode:  validTOTP,
				ScanTimestamp:  validScanTimestamp,
				ProofSignature: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
				DeviceID:       deviceID,
			},
		},
	}
	respForged, _ := svc.SyncOfflineProofs(context.Background(), studentID, forgedBatch, "192.168.1.50")
	if respForged.TotalAccepted != 0 || respForged.Results[0].Status != "rejected" {
		t.Errorf("Expected forged signature proof to be rejected")
	}

	// Case 4: Unauthorized device
	unauthorizedBatch := &BatchOfflineSyncRequest{
		Proofs: []OfflineSyncItem{
			{
				SessionID:      sessionID,
				ScannedQRCode:  validTOTP,
				ScanTimestamp:  validScanTimestamp,
				ProofSignature: validSig,
				DeviceID:       "unauthorized-device",
			},
		},
	}
	respUnauth, _ := svc.SyncOfflineProofs(context.Background(), studentID, unauthorizedBatch, "192.168.1.50")
	if respUnauth.TotalAccepted != 0 || respUnauth.Results[0].Status != "rejected" {
		t.Errorf("Expected unauthorized device to be rejected")
	}

	// Case 5: Scan outside session hours (scanned 2 hours ago, before lecture started)
	earlyTimestamp := now.Add(-120 * time.Minute).Unix()
	earlyTOTP, _ := utils.ComputeTOTPForWindow(qrSecret, earlyTimestamp/5)
	earlySig := utils.GenerateOfflineProofSignature(deviceID, sessionID.String(), earlyTOTP, earlyTimestamp)

	outsideHoursBatch := &BatchOfflineSyncRequest{
		Proofs: []OfflineSyncItem{
			{
				SessionID:      sessionID,
				ScannedQRCode:  earlyTOTP,
				ScanTimestamp:  earlyTimestamp,
				ProofSignature: earlySig,
				DeviceID:       deviceID,
			},
		},
	}
	respOutside, _ := svc.SyncOfflineProofs(context.Background(), studentID, outsideHoursBatch, "192.168.1.50")
	if respOutside.TotalAccepted != 0 || respOutside.Results[0].Status != "rejected" {
		t.Errorf("Expected proof outside session hours to be rejected")
	}

	// Case 6: Future scan timestamp (+10 minutes in future)
	futureTimestamp := now.Add(10 * time.Minute).Unix()
	futureTOTP, _ := utils.ComputeTOTPForWindow(qrSecret, futureTimestamp/5)
	futureSig := utils.GenerateOfflineProofSignature(deviceID, sessionID.String(), futureTOTP, futureTimestamp)

	futureBatch := &BatchOfflineSyncRequest{
		Proofs: []OfflineSyncItem{
			{
				SessionID:      sessionID,
				ScannedQRCode:  futureTOTP,
				ScanTimestamp:  futureTimestamp,
				ProofSignature: futureSig,
				DeviceID:       deviceID,
			},
		},
	}
	respFuture, _ := svc.SyncOfflineProofs(context.Background(), studentID, futureBatch, "192.168.1.50")
	if respFuture.TotalAccepted != 0 || respFuture.Results[0].Status != "rejected" {
		t.Errorf("Expected future timestamp to be rejected")
	}
}
