package auth

import (
	"errors"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"hader-backend/internal/middleware"
)

// Handler handles HTTP requests for user authentication and device binding.
type Handler struct {
	service   Service
	jwtSecret string
}

// NewHandler initializes a new auth HTTP handler.
func NewHandler(service Service, jwtSecret string) *Handler {
	return &Handler{
		service:   service,
		jwtSecret: jwtSecret,
	}
}

// RegisterRoutes hooks the authentication endpoints to Fiber router.
func (h *Handler) RegisterRoutes(router fiber.Router) {
	router.Post("/register", middleware.AuthLimiter(), h.Register)
	router.Post("/login", middleware.AuthLimiter(), h.Login)
	router.Post("/recover-password", middleware.AuthLimiter(), h.RecoverPassword)
	router.Post("/logout", h.Logout)
	router.Get("/me", middleware.Authenticate(h.jwtSecret), h.GetMe)
}

// Register handles student onboarding with device binding and terms verification.
func (h *Handler) Register(c *fiber.Ctx) error {
	var req RegisterRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "MALFORMED_JSON",
				"message": "Invalid JSON payload in request body",
			},
		})
	}

	// Fallback device ID from custom header if omitted from body
	if req.DeviceID == "" {
		req.DeviceID = c.Get("X-Device-Id")
	}

	resp, err := h.service.Register(c.Context(), &req, c.IP())
	if err != nil {
		status := fiber.StatusBadRequest
		code := "REGISTRATION_FAILED"

		if errors.Is(err, ErrAcademicIDExists) {
			status = fiber.StatusConflict
			code = "ACADEMIC_ID_EXISTS"
		} else if errors.Is(err, ErrEmailExists) {
			status = fiber.StatusConflict
			code = "EMAIL_EXISTS"
		}

		return c.Status(status).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    code,
				"message": err.Error(),
			},
		})
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"success": true,
		"data":    resp,
	})
}

// Login handles dual-mode student/class-leader authentication with hardware lock enforcement.
func (h *Handler) Login(c *fiber.Ctx) error {
	var req LoginRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "MALFORMED_JSON",
				"message": "Invalid JSON payload in request body",
			},
		})
	}

	if req.DeviceID == "" {
		req.DeviceID = c.Get("X-Device-Id")
	}

	resp, refreshToken, err := h.service.Login(c.Context(), &req, c.IP())
	if err != nil {
		status := fiber.StatusUnauthorized
		code := "LOGIN_FAILED"

		if errors.Is(err, ErrInvalidCredentials) {
			status = fiber.StatusUnauthorized
			code = "INVALID_CREDENTIALS"
		} else if errors.Is(err, ErrAccountDisabled) {
			status = fiber.StatusForbidden
			code = "ACCOUNT_DISABLED"
		} else if errors.Is(err, ErrInsufficientRolePrivileges) {
			status = fiber.StatusForbidden
			code = "INSUFFICIENT_ROLE_PRIVILEGES"
		} else if errors.Is(err, ErrDeviceLockedMismatch) {
			status = fiber.StatusForbidden
			code = "DEVICE_LOCKED_MISMATCH"
		} else if errors.Is(err, ErrDeviceUntrusted) {
			status = fiber.StatusForbidden
			code = "DEVICE_UNTRUSTED"
		}

		return c.Status(status).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    code,
				"message": err.Error(),
			},
		})
	}

	// Attach secure HttpOnly refresh token cookie if remember-me was activated
	if refreshToken != "" {
		c.Cookie(&fiber.Cookie{
			Name:     "hader_refresh_token",
			Value:    refreshToken,
			Expires:  time.Now().Add(30 * 24 * time.Hour),
			HTTPOnly: true,
			Secure:   true,
			SameSite: "Strict",
		})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"success": true,
		"data":    resp,
	})
}

// RecoverPassword dispatches password reset email.
func (h *Handler) RecoverPassword(c *fiber.Ctx) error {
	var req PasswordRecoveryRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "MALFORMED_JSON",
				"message": "Invalid JSON payload",
			},
		})
	}

	_ = h.service.RecoverPassword(c.Context(), &req)

	// Consistent message returned regardless of email existence to prevent user enumeration
	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"success": true,
		"message": "إذا كان البريد الإلكتروني مسجلاً بالنظام، فستصلك رسالة إعادة ضبط كلمة المرور",
	})
}

// Logout clears the refresh token cookie and terminates the session.
func (h *Handler) Logout(c *fiber.Ctx) error {
	c.ClearCookie("hader_refresh_token")
	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"success": true,
		"message": "تم تسجيل الخروج بنجاح",
	})
}

// GetMe retrieves identity details of the currently authenticated student or class leader.
func (h *Handler) GetMe(c *fiber.Ctx) error {
	uidStr := middleware.GetUserID(c)
	uid, err := uuid.Parse(uidStr)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "INVALID_USER_ID",
				"message": "User identifier in token is invalid",
			},
		})
	}

	deviceID := c.Get("X-Device-Id")
	user, device, err := h.service.GetCurrentUser(c.Context(), uid, deviceID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "USER_NOT_FOUND",
				"message": "User profile could not be located",
			},
		})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"user":   user,
			"device": device,
		},
	})
}
