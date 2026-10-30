package attendance

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"hader-backend/internal/utils"
)

var (
	ErrSessionNotActive          = errors.New("SESSION_NOT_ACTIVE: الجلسة غير نشطة حالياً أو تم إغلاقها")
	ErrSessionOutsideWindow      = errors.New("SESSION_OUTSIDE_WINDOW: انتهت المدة الزمنية المحددة لحضور هذه الجلسة")
	ErrNotEnrolledInDepartment   = errors.New("NOT_ENROLLED_IN_DEPARTMENT: الطالب غير مقيد بالقسم الأكاديمي التابع له هذه المحاضرة")
	ErrPoorGPSAccuracyOrMock     = errors.New("POOR_GPS_ACCURACY_OR_MOCK: دقة الموقع الجغرافي ضعيفة جداً أو تم رصد تزييف موقع (Mock GPS)")
	ErrOutsideLectureGeofence    = errors.New("OUTSIDE_LECTURE_GEOFENCE: أنت متواجد خارج النطاق الجغرافي المحدد لقاعة المحاضرة")
	ErrImpossibleTravelDetected  = errors.New("IMPOSSIBLE_TRAVEL_DETECTED: تم اكتشاف انتقال جغرافي غير واقعي (Impossible Travel)")
	ErrInvalidOrExpiredQR        = errors.New("INVALID_OR_EXPIRED_QR: رمز الاستجابة السريعة (QR) غير صحيح أو انتهت صلاحيته (5 ثوانٍ)")
	ErrAlreadyCheckedIn          = errors.New("ALREADY_CHECKED_IN: لقد تم تسجيل حضورك في هذه الجلسة بالفعل")
)

// Service defines the business contract for high-concurrency attendance check-in and dynamic QR generation.
type Service interface {
	ProcessCheckIn(ctx context.Context, studentID uuid.UUID, req *CheckInRequest, clientIP string) (*CheckInResponse, error)
	GetDynamicQRForLeader(ctx context.Context, leaderID, sessionID uuid.UUID) (*SessionQRResponse, error)
	GetStudentHistory(ctx context.Context, studentID uuid.UUID) ([]AttendanceHistoryItem, error)
	SyncOfflineProofs(ctx context.Context, studentID uuid.UUID, req *BatchOfflineSyncRequest, clientIP string) (*BatchOfflineSyncResponse, error)
}

type attendanceService struct {
	repo Repository
}

// NewService instantiates the attendance business service.
func NewService(repo Repository) Service {
	return &attendanceService{repo: repo}
}

// ProcessCheckIn validates all anti-fraud vectors and executes zero-race-condition attendance persistence.
func (s *attendanceService) ProcessCheckIn(ctx context.Context, studentID uuid.UUID, req *CheckInRequest, clientIP string) (*CheckInResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("validation error: %w", err)
	}

	now := time.Now()

	// 1. Session State Guard
	session, err := s.repo.GetActiveSessionByID(ctx, req.SessionID)
	if err != nil {
		return nil, fmt.Errorf("database session lookup failed: %w", err)
	}
	if session == nil {
		return nil, ErrSessionNotActive
	}

	// 2. Department Enrollment Guard
	if session.DepartmentID != nil {
		isEnrolled, err := s.repo.VerifyStudentDepartment(ctx, studentID, *session.DepartmentID)
		if err != nil {
			return nil, fmt.Errorf("department check failed: %w", err)
		}
		if !isEnrolled {
			return nil, ErrNotEnrolledInDepartment
		}
	}

	// 3. Fake GPS & Accuracy Guard
	if !utils.ValidateGPSAccuracy(req.AccuracyMeters) {
		_ = s.repo.LogSecurityIncident(ctx, &studentID, "MOCK_GPS_DETECTED", "HIGH", map[string]interface{}{
			"session_id":      req.SessionID.String(),
			"accuracy_meters": req.AccuracyMeters,
			"lat":             req.Latitude,
			"lng":             req.Longitude,
			"action":          "check_in_blocked",
		}, &clientIP)
		return nil, ErrPoorGPSAccuracyOrMock
	}

	// 4. Geofencing Guard (Spherical Haversine)
	dist := utils.CalculateHaversineDistance(req.Latitude, req.Longitude, session.GeofenceLat, session.GeofenceLng)
	if dist > float64(session.GeofenceRadius) {
		_ = s.repo.LogSecurityIncident(ctx, &studentID, "OUT_OF_BOUNDS_ATTEMPT", "MEDIUM", map[string]interface{}{
			"session_id":      req.SessionID.String(),
			"distance_meters": dist,
			"allowed_radius":  session.GeofenceRadius,
			"student_lat":     req.Latitude,
			"student_lng":     req.Longitude,
		}, &clientIP)
		return nil, ErrOutsideLectureGeofence
	}

	// 5. Impossible Travel Guard
	lastLoc, err := s.repo.GetLastStudentLocation(ctx, studentID)
	if err == nil && lastLoc != nil {
		isImpossible, speed := utils.CheckImpossibleTravel(lastLoc.Lat, lastLoc.Lng, lastLoc.RecordedAt, req.Latitude, req.Longitude, now)
		if isImpossible {
			_ = s.repo.LogSecurityIncident(ctx, &studentID, "IMPOSSIBLE_TRAVEL_DETECTED", "CRITICAL", map[string]interface{}{
				"session_id":     req.SessionID.String(),
				"speed_kmh":      speed,
				"prev_lat":       lastLoc.Lat,
				"prev_lng":       lastLoc.Lng,
				"prev_timestamp": lastLoc.RecordedAt.Format(time.RFC3339),
			}, &clientIP)
			return nil, ErrImpossibleTravelDetected
		}
	}

	// 6. Dynamic QR Code Guard (HMAC-SHA256 with 5-second step & t-1 grace window)
	if !utils.ValidateDynamicTOTP(session.QRSecret, req.QRCode, 5) {
		return nil, ErrInvalidOrExpiredQR
	}

	// 7. Atomic Concurrency Lock
	record := &AttendanceRecord{
		SessionID:        req.SessionID,
		StudentID:        studentID,
		DeviceID:         req.DeviceID,
		VerificationMode: ModeQRDynamicGPS,
		AccuracyMeters:   &req.AccuracyMeters,
		ClientIP:         &clientIP,
	}

	attendanceID, err := s.repo.AtomicInsertAttendance(ctx, record)
	if err != nil {
		return nil, fmt.Errorf("persistence failure: %w", err)
	}

	if attendanceID == 0 {
		return nil, ErrAlreadyCheckedIn
	}

	// 8. Streak Increment & Telemetry Update
	streak, _ := s.repo.IncrementUserStreak(ctx, studentID)
	_ = s.repo.UpdateDeviceLocation(ctx, studentID, req.DeviceID, req.Latitude, req.Longitude, clientIP)

	return &CheckInResponse{
		AttendanceID:  attendanceID,
		CheckInTime:   now,
		Status:        "present",
		SubjectName:   session.SubjectName,
		CurrentStreak: streak,
	}, nil
}

// GetDynamicQRForLeader provides active TOTP tokens and countdown for Class Leader broadcast displays.
func (s *attendanceService) GetDynamicQRForLeader(ctx context.Context, leaderID, sessionID uuid.UUID) (*SessionQRResponse, error) {
	session, err := s.repo.GetActiveSessionByID(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("session lookup error: %w", err)
	}
	if session == nil {
		return nil, ErrSessionNotActive
	}

	token, remaining, err := utils.GenerateDynamicTOTP(session.QRSecret, 5)
	if err != nil {
		return nil, fmt.Errorf("totp generation failed: %w", err)
	}

	return &SessionQRResponse{
		SessionID:        session.ID,
		CurrentToken:     token,
		ExpiresInSeconds: remaining,
		Window:           time.Now().Unix() / 5,
	}, nil
}

// GetStudentHistory returns the authenticated student's attendance records.
func (s *attendanceService) GetStudentHistory(ctx context.Context, studentID uuid.UUID) ([]AttendanceHistoryItem, error) {
	return s.repo.GetStudentAttendanceHistory(ctx, studentID)
}

// SyncOfflineProofs verifies offline proof signatures, checks session time boundaries,
// authenticates historical TOTP codes, and atomically persists valid attendance.
func (s *attendanceService) SyncOfflineProofs(ctx context.Context, studentID uuid.UUID, req *BatchOfflineSyncRequest, clientIP string) (*BatchOfflineSyncResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("batch validation error: %w", err)
	}

	results := make([]OfflineSyncResultItem, 0, len(req.Proofs))
	totalAccepted := 0
	now := time.Now()

	for _, item := range req.Proofs {
		// 1. Device Authorization Check (Verify hardware binding to authenticated student)
		isOwner, err := s.repo.VerifyDeviceOwnership(ctx, studentID, item.DeviceID)
		if err != nil || !isOwner {
			_ = s.repo.LogSecurityIncident(ctx, &studentID, "UNAUTHORIZED_DEVICE_OFFLINE_SYNC", "HIGH", map[string]interface{}{
				"session_id": item.SessionID.String(),
				"device_id":  item.DeviceID,
				"action":     "offline_sync_rejected",
			}, &clientIP)

			results = append(results, OfflineSyncResultItem{
				SessionID: item.SessionID,
				Status:    "rejected",
				Reason:    "UNAUTHORIZED_DEVICE: الجهاز المستخدم غير مقترن بحساب الطالب المعتمد",
			})
			continue
		}

		// 2. Cryptographic Proof Signature Verification (HMAC-SHA256)
		validSig := utils.VerifyOfflineProofSignature(
			item.DeviceID,
			item.SessionID.String(),
			item.ScannedQRCode,
			item.ScanTimestamp,
			item.ProofSignature,
		)
		if !validSig {
			_ = s.repo.LogSecurityIncident(ctx, &studentID, "FORGED_OFFLINE_PROOF_SIGNATURE", "HIGH", map[string]interface{}{
				"session_id": item.SessionID.String(),
				"device_id":  item.DeviceID,
				"timestamp":  item.ScanTimestamp,
				"action":     "offline_sync_blocked",
			}, &clientIP)

			results = append(results, OfflineSyncResultItem{
				SessionID: item.SessionID,
				Status:    "rejected",
				Reason:    "FORGED_PROOF_SIGNATURE: فشل التحقق من صحة التوقيع الرقمي المشفر",
			})
			continue
		}

		// 3. Session & Time-Window Validation
		session, err := s.repo.GetSessionHistoricalDetails(ctx, item.SessionID)
		if err != nil || session == nil {
			results = append(results, OfflineSyncResultItem{
				SessionID: item.SessionID,
				Status:    "rejected",
				Reason:    "SESSION_NOT_FOUND: لم يتم العثور على الجلسة الأكاديمية المحددة",
			})
			continue
		}

		// Department Enrollment Guard
		if session.DepartmentID != nil {
			isEnrolled, err := s.repo.VerifyStudentDepartment(ctx, studentID, *session.DepartmentID)
			if err != nil || !isEnrolled {
				results = append(results, OfflineSyncResultItem{
					SessionID: item.SessionID,
					Status:    "rejected",
					Reason:    "NOT_ENROLLED_IN_DEPARTMENT: الطالب غير مقيد بالقسم الأكاديمي لهذه المحاضرة",
				})
				continue
			}
		}

		scanTime := time.Unix(item.ScanTimestamp, 0)

		// Future-Time Guard: clock drift tolerance up to 1 minute
		if scanTime.After(now.Add(1 * time.Minute)) {
			results = append(results, OfflineSyncResultItem{
				SessionID: item.SessionID,
				Status:    "rejected",
				Reason:    "INVALID_FUTURE_TIMESTAMP: توقيت المسح مسجل في المستقبل وغير مقبول",
			})
			continue
		}

		// Session Window Guard: scanTime must strictly be between starts_at and ends_at
		if scanTime.Before(session.StartsAt) || scanTime.After(session.EndsAt) {
			results = append(results, OfflineSyncResultItem{
				SessionID: item.SessionID,
				Status:    "rejected",
				Reason:    "SCAN_OUTSIDE_SESSION_HOURS: توقيت المسح غير المتصل يقع خارج المدى الزمني للمحاضرة",
			})
			continue
		}

		// Max Sync Delay Guard: sync must be submitted within 12 hours of session completion
		if now.Sub(session.EndsAt) > 12*time.Hour {
			results = append(results, OfflineSyncResultItem{
				SessionID: item.SessionID,
				Status:    "rejected",
				Reason:    "SYNC_WINDOW_EXPIRED: انتهت المهلة القصوى لمزامنة الحضور دون اتصال (12 ساعة)",
			})
			continue
		}

		// 4. Historical Dynamic TOTP Validation
		if !utils.ValidateHistoricalTOTP(session.QRSecret, item.ScannedQRCode, item.ScanTimestamp, 5) {
			results = append(results, OfflineSyncResultItem{
				SessionID: item.SessionID,
				Status:    "rejected",
				Reason:    "INVALID_OFFLINE_QR_CODE: رمز الاستجابة السريعة غير مطابق للنافذة الزمنية لحظة المسح",
			})
			continue
		}

		// 5. Atomic Persistence with verification_mode = 'offline_proof'
		attID, err := s.repo.AtomicInsertOfflineAttendance(ctx, item.SessionID, studentID, item.DeviceID, clientIP, scanTime)
		if err != nil {
			results = append(results, OfflineSyncResultItem{
				SessionID: item.SessionID,
				Status:    "rejected",
				Reason:    "PERSISTENCE_ERROR: " + err.Error(),
			})
			continue
		}

		if attID == 0 {
			// Already registered in this session
			results = append(results, OfflineSyncResultItem{
				SessionID: item.SessionID,
				Status:    "rejected",
				Reason:    "ALREADY_CHECKED_IN: لقد تم تسجيل حضورك في هذه الجلسة مسبقاً",
			})
			continue
		}

		// Success: Increment continuous streak
		_, _ = s.repo.IncrementUserStreak(ctx, studentID)
		totalAccepted++

		results = append(results, OfflineSyncResultItem{
			SessionID:    item.SessionID,
			Status:       "synced",
			AttendanceID: &attID,
		})
	}

	return &BatchOfflineSyncResponse{
		TotalReceived: len(req.Proofs),
		TotalAccepted: totalAccepted,
		Results:       results,
	}, nil
}
