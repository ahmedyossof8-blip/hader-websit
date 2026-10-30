package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"hader-backend/internal/config"
)

var (
	ErrInvalidCredentials         = errors.New("INVALID_CREDENTIALS: وبيانات الدخول غير صحيحة")
	ErrAccountDisabled            = errors.New("ACCOUNT_DISABLED: الحساب الأكاديمي معطل، يُرجى مراجعة شؤون الطلاب")
	ErrInsufficientRolePrivileges = errors.New("INSUFFICIENT_ROLE_PRIVILEGES: عفواً، هذا الحساب غير مصرح له بالدخول كـ ممثل دفعة")
	ErrDeviceLockedMismatch       = errors.New("DEVICE_LOCKED_MISMATCH: هذا الحساب مقترن بجهاز آخر. يُرجى مراجعة إدارة النظام لفك الارتباط")
	ErrDeviceUntrusted            = errors.New("DEVICE_UNTRUSTED: هذا الجهاز غير موثوق به للمصادقة الأكاديمية")
	ErrAcademicIDExists           = errors.New("ACADEMIC_ID_EXISTS: الرقم الأكاديمي مسجل بالفعل في النظام")
	ErrEmailExists                = errors.New("EMAIL_EXISTS: البريد الإلكتروني مسجل بالفعل")
)

// Service specifies the business contract for Hader-V2 authentication.
type Service interface {
	Register(ctx context.Context, req *RegisterRequest, clientIP string) (*AuthResponse, error)
	Login(ctx context.Context, req *LoginRequest, clientIP string) (*AuthResponse, string, error)
	RecoverPassword(ctx context.Context, req *PasswordRecoveryRequest) error
	GetCurrentUser(ctx context.Context, userID uuid.UUID, deviceID string) (*UserResponse, *UserDevice, error)
}

type authService struct {
	repo       Repository
	cfg        *config.Config
	httpClient *http.Client
}

// NewService instantiates the authentication business service.
func NewService(repo Repository, cfg *config.Config) Service {
	return &authService{
		repo: repo,
		cfg:  cfg,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Register onboard a new academic student, provisions Supabase Auth identity, and binds the initial trusted hardware device.
func (s *authService) Register(ctx context.Context, req *RegisterRequest, clientIP string) (*AuthResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("validation error: %w", err)
	}

	// 1. Enforce uniqueness in local profiles database
	existingProfile, err := s.repo.GetProfileByIdentifier(ctx, req.AcademicID)
	if err != nil {
		return nil, fmt.Errorf("database check error: %w", err)
	}
	if existingProfile != nil {
		return nil, ErrAcademicIDExists
	}

	existingEmail, err := s.repo.GetProfileByIdentifier(ctx, req.Email)
	if err != nil {
		return nil, fmt.Errorf("database check error: %w", err)
	}
	if existingEmail != nil {
		return nil, ErrEmailExists
	}

	// 2. Provision Supabase Auth User Identity
	authUID, err := s.provisionSupabaseUser(ctx, req.Email, req.Password, req.FullName, req.AcademicID)
	if err != nil {
		return nil, fmt.Errorf("supabase identity creation failed: %w", err)
	}

	// 3. Persist Profile and Bind Trusted Initial Hardware Device
	profile := UserProfile{
		ID:            authUID,
		AcademicID:    req.AcademicID,
		FullName:      req.FullName,
		Email:         req.Email,
		Role:          string(RoleStudent),
		DepartmentID:  &req.DepartmentID,
		CurrentStreak: 0,
		IsActive:      true,
	}

	device := UserDevice{
		UserID:     authUID,
		DeviceID:   req.DeviceID,
		DeviceName: &req.DeviceName,
		IsTrusted:  true,
		LastIP:     &clientIP,
	}

	if err := s.repo.CreateUserWithProfileAndDeviceTx(ctx, &profile, &device); err != nil {
		return nil, fmt.Errorf("failed to commit user profile and device lock: %w", err)
	}

	// 4. Generate In-Memory JWT Access Token (15-min lifespan)
	tokenString, err := s.generateAccessToken(&profile, req.DeviceID, 15*time.Minute)
	if err != nil {
		return nil, fmt.Errorf("token minting failed: %w", err)
	}

	return &AuthResponse{
		Token: tokenString,
		User: UserResponse{
			ID:            profile.ID.String(),
			AcademicID:    profile.AcademicID,
			FullName:      profile.FullName,
			Email:         profile.Email,
			Role:          profile.Role,
			DepartmentID:  profile.DepartmentID,
			CurrentStreak: profile.CurrentStreak,
			IsActive:      profile.IsActive,
		},
		DeviceLocked: true,
	}, nil
}

// Login authenticates credentials, enforces role separation, and validates hardware device locks.
func (s *authService) Login(ctx context.Context, req *LoginRequest, clientIP string) (*AuthResponse, string, error) {
	if err := req.Validate(); err != nil {
		return nil, "", fmt.Errorf("validation error: %w", err)
	}

	// 1. Fetch user profile by academic_id or email
	profile, err := s.repo.GetProfileByIdentifier(ctx, req.Identifier)
	if err != nil {
		return nil, "", fmt.Errorf("database query failure: %w", err)
	}
	if profile == nil {
		return nil, "", ErrInvalidCredentials
	}

	if !profile.IsActive {
		return nil, "", ErrAccountDisabled
	}

	// 2. Verify Credentials against Supabase Auth endpoint
	if err := s.verifySupabaseCredentials(ctx, profile.Email, req.Password); err != nil {
		_ = s.repo.LogSecurityIncident(ctx, &SecurityAuditLog{
			UserID:    &profile.ID,
			EventType: "FAILED_LOGIN",
			Severity:  "LOW",
			Details: map[string]interface{}{
				"identifier": req.Identifier,
				"client_ip":   clientIP,
				"reason":     err.Error(),
			},
			IPAddress: &clientIP,
		})
		return nil, "", ErrInvalidCredentials
	}

	// 3. Strict Dual-Role Enforcement
	if req.LoginRole == string(RoleClassLeader) {
		if profile.Role != string(RoleClassLeader) && profile.Role != string(RoleAdmin) {
			_ = s.repo.LogSecurityIncident(ctx, &SecurityAuditLog{
				UserID:    &profile.ID,
				EventType: "UNAUTHORIZED_ROLE_ACCESS",
				Severity:  "HIGH",
				Details: map[string]interface{}{
					"attempted_role": req.LoginRole,
					"actual_role":    profile.Role,
					"academic_id":    profile.AcademicID,
				},
				IPAddress: &clientIP,
			})
			return nil, "", ErrInsufficientRolePrivileges
		}
	}

	// 4. Hardware Device Pinning Guard
	device, err := s.repo.GetDeviceByUserAndDeviceID(ctx, profile.ID, req.DeviceID)
	if err != nil {
		return nil, "", fmt.Errorf("device verification failure: %w", err)
	}

	if device == nil {
		// Device not found. Inspect if user has an existing trusted device registered
		deviceCount, err := s.repo.CountUserDevices(ctx, profile.ID)
		if err != nil {
			return nil, "", fmt.Errorf("device count inspection failed: %w", err)
		}

		if deviceCount == 0 {
			// First-time device enrollment: automatically bind as primary trusted hardware
			if err := s.repo.BindInitialDevice(ctx, &UserDevice{
				UserID:     profile.ID,
				DeviceID:   req.DeviceID,
				DeviceName: &req.DeviceName,
				IsTrusted:  true,
				LastIP:     &clientIP,
			}); err != nil {
				return nil, "", fmt.Errorf("failed to register primary device: %w", err)
			}
		} else {
			// Account is already pinned to a different hardware fingerprint -> BLOCK FRAUD ATTEMPT
			_ = s.repo.LogSecurityIncident(ctx, &SecurityAuditLog{
				UserID:    &profile.ID,
				EventType: "DEVICE_MISMATCH",
				Severity:  "HIGH",
				Details: map[string]interface{}{
					"attempted_device_id":   req.DeviceID,
					"attempted_device_name": req.DeviceName,
					"academic_id":           profile.AcademicID,
					"action":                "login_blocked",
				},
				IPAddress: &clientIP,
			})
			return nil, "", ErrDeviceLockedMismatch
		}
	} else {
		// Existing device found: confirm trust status
		if !device.IsTrusted {
			return nil, "", ErrDeviceUntrusted
		}
		// Refresh login metadata and IP
		_ = s.repo.UpdateDeviceLoginMetadata(ctx, profile.ID, req.DeviceID, clientIP, nil, nil)
	}

	// 5. Generate Access Token (15-minute standard lifetime)
	accessToken, err := s.generateAccessToken(profile, req.DeviceID, 15*time.Minute)
	if err != nil {
		return nil, "", fmt.Errorf("failed to generate access token: %w", err)
	}

	// 6. Generate Refresh Token if RememberMe is selected (30-day lifetime)
	var refreshToken string
	if req.RememberMe {
		refreshToken, err = s.generateRefreshToken(profile, req.DeviceID, 30*24*time.Hour)
		if err != nil {
			return nil, "", fmt.Errorf("failed to generate refresh token: %w", err)
		}
	}

	return &AuthResponse{
		Token: accessToken,
		User: UserResponse{
			ID:            profile.ID.String(),
			AcademicID:    profile.AcademicID,
			FullName:      profile.FullName,
			Email:         profile.Email,
			Role:          profile.Role,
			DepartmentID:  profile.DepartmentID,
			CurrentStreak: profile.CurrentStreak,
			IsActive:      profile.IsActive,
		},
		DeviceLocked: true,
	}, refreshToken, nil
}

// RecoverPassword triggers Supabase Auth password recovery workflow.
func (s *authService) RecoverPassword(ctx context.Context, req *PasswordRecoveryRequest) error {
	if err := req.Validate(); err != nil {
		return fmt.Errorf("validation error: %w", err)
	}

	// Non-blocking invocation to prevent timing-based email enumeration
	if s.cfg.SupabaseURL != "" && s.cfg.SupabaseAnonKey != "" {
		recoverURL := fmt.Sprintf("%s/auth/v1/recover", s.cfg.SupabaseURL)
		payload, _ := json.Marshal(map[string]string{"email": req.Email})

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, recoverURL, bytes.NewBuffer(payload))
		if err == nil {
			httpReq.Header.Set("Content-Type", "application/json")
			httpReq.Header.Set("apikey", s.cfg.SupabaseAnonKey)
			resp, err := s.httpClient.Do(httpReq)
			if err == nil {
				_ = resp.Body.Close()
			}
		}
	}

	return nil
}

// GetCurrentUser returns the profile and device telemetry of the authenticated user.
func (s *authService) GetCurrentUser(ctx context.Context, userID uuid.UUID, deviceID string) (*UserResponse, *UserDevice, error) {
	profile, err := s.repo.GetProfileByID(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	if profile == nil {
		return nil, nil, errors.New("user profile not found")
	}

	device, _ := s.repo.GetDeviceByUserAndDeviceID(ctx, userID, deviceID)

	userResp := &UserResponse{
		ID:            profile.ID.String(),
		AcademicID:    profile.AcademicID,
		FullName:      profile.FullName,
		Email:         profile.Email,
		Role:          profile.Role,
		DepartmentID:  profile.DepartmentID,
		CurrentStreak: profile.CurrentStreak,
		IsActive:      profile.IsActive,
	}

	return userResp, device, nil
}

// provisionSupabaseUser registers a user in Supabase Auth via REST endpoint or generates a UUID stub in local environments.
func (s *authService) provisionSupabaseUser(ctx context.Context, email, password, fullName, academicID string) (uuid.UUID, error) {
	if s.cfg.SupabaseURL == "" || (s.cfg.SupabaseServiceRoleKey == "" && s.cfg.SupabaseAnonKey == "") {
		// Standalone or local testing mode without live cloud connection
		return uuid.New(), nil
	}

	var endpoint string
	var apiKey string
	var bodyPayload map[string]interface{}

	if s.cfg.SupabaseServiceRoleKey != "" {
		// Admin user creation endpoint (auto-confirms email)
		endpoint = fmt.Sprintf("%s/auth/v1/admin/users", s.cfg.SupabaseURL)
		apiKey = s.cfg.SupabaseServiceRoleKey
		bodyPayload = map[string]interface{}{
			"email":         email,
			"password":      password,
			"email_confirm": true,
			"user_metadata": map[string]interface{}{
				"full_name":   fullName,
				"academic_id": academicID,
				"role":        string(RoleStudent),
			},
		}
	} else {
		// Public sign-up endpoint
		endpoint = fmt.Sprintf("%s/auth/v1/signup", s.cfg.SupabaseURL)
		apiKey = s.cfg.SupabaseAnonKey
		bodyPayload = map[string]interface{}{
			"email":    email,
			"password": password,
			"data": map[string]interface{}{
				"full_name":   fullName,
				"academic_id": academicID,
				"role":        string(RoleStudent),
			},
		}
	}

	payloadBytes, err := json.Marshal(bodyPayload)
	if err != nil {
		return uuid.Nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return uuid.Nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("apikey", apiKey)
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return uuid.Nil, fmt.Errorf("http request to supabase failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		return uuid.Nil, fmt.Errorf("supabase returned error status %d: %s", resp.StatusCode, string(respBody))
	}

	var parsed struct {
		ID   string `json:"id"`
		User struct {
			ID string `json:"id"`
		} `json:"user"`
	}

	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return uuid.Nil, fmt.Errorf("failed to parse supabase response: %w", err)
	}

	uidStr := parsed.ID
	if uidStr == "" {
		uidStr = parsed.User.ID
	}

	uid, err := uuid.Parse(uidStr)
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid uuid returned by supabase auth: %w", err)
	}

	return uid, nil
}

// verifySupabaseCredentials checks user credentials against Supabase Auth password token endpoint.
func (s *authService) verifySupabaseCredentials(ctx context.Context, email, password string) error {
	if s.cfg.SupabaseURL == "" || s.cfg.SupabaseAnonKey == "" {
		// In standalone testing mode, skip cloud password verification
		return nil
	}

	tokenURL := fmt.Sprintf("%s/auth/v1/token?grant_type=password", s.cfg.SupabaseURL)
	payloadBytes, _ := json.Marshal(map[string]string{
		"email":    email,
		"password": password,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("apikey", s.cfg.SupabaseAnonKey)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("supabase connection error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("authentication rejected: %s", string(respBody))
	}

	return nil
}

// generateAccessToken creates an in-memory HMAC-SHA256 JWT access token.
func (s *authService) generateAccessToken(p *UserProfile, deviceID string, duration time.Duration) (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"sub":         p.ID.String(),
		"academic_id": p.AcademicID,
		"email":       p.Email,
		"role":        p.Role,
		"device_id":   deviceID,
		"iat":         now.Unix(),
		"exp":         now.Add(duration).Unix(),
		"app_metadata": map[string]interface{}{
			"role": p.Role,
		},
		"user_metadata": map[string]interface{}{
			"full_name":   p.FullName,
			"academic_id": p.AcademicID,
		},
	}

	if p.DepartmentID != nil {
		claims["department_id"] = *p.DepartmentID
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.cfg.SupabaseJWTSecret))
}

// generateRefreshToken generates a cryptographically signed refresh token.
func (s *authService) generateRefreshToken(p *UserProfile, deviceID string, duration time.Duration) (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"sub":       p.ID.String(),
		"device_id": deviceID,
		"token_type": "refresh",
		"iat":       now.Unix(),
		"exp":       now.Add(duration).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.cfg.SupabaseJWTSecret))
}
