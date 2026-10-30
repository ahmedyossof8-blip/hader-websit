package attendance

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"hader-backend/internal/middleware"
)

// Handler handles HTTP requests for lecture attendance and dynamic QR token broadcasting.
type Handler struct {
	service   Service
	jwtSecret string
}

// NewHandler instantiates an attendance HTTP controller.
func NewHandler(service Service, jwtSecret string) *Handler {
	return &Handler{
		service:   service,
		jwtSecret: jwtSecret,
	}
}

// RegisterRoutes registers endpoints for student check-ins and leader QR feeds.
func (h *Handler) RegisterRoutes(router fiber.Router) {
	// Attendance group: /api/v1/attendance
	attGroup := router.Group("/attendance")
	attGroup.Post("/check-in",
		middleware.Authenticate(h.jwtSecret),
		middleware.AttendanceLimiter(),
		h.CheckIn,
	)
	attGroup.Post("/sync-offline",
		middleware.Authenticate(h.jwtSecret),
		middleware.AttendanceLimiter(),
		h.SyncOffline,
	)
	attGroup.Get("/history/me",
		middleware.Authenticate(h.jwtSecret),
		h.GetMyHistory,
	)

	// Sessions group: /api/v1/sessions
	sessionsGroup := router.Group("/sessions")
	sessionsGroup.Get("/:id/qr-stream",
		middleware.Authenticate(h.jwtSecret),
		middleware.RequireRole("class_leader", "admin"),
		h.GetDynamicQR,
	)
}

// CheckIn processes dynamic QR scans, GPS geofencing, and atomic check-in locks.
func (h *Handler) CheckIn(c *fiber.Ctx) error {
	studentIDStr := middleware.GetUserID(c)
	studentID, err := uuid.Parse(studentIDStr)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "INVALID_STUDENT_ID",
				"message": "Authenticated student identifier is invalid",
			},
		})
	}

	var req CheckInRequest
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

	resp, err := h.service.ProcessCheckIn(c.Context(), studentID, &req, c.IP())
	if err != nil {
		status := fiber.StatusBadRequest
		code := "CHECK_IN_FAILED"

		if errors.Is(err, ErrSessionNotActive) || errors.Is(err, ErrSessionOutsideWindow) {
			status = fiber.StatusBadRequest
			code = "SESSION_NOT_ACTIVE"
		} else if errors.Is(err, ErrNotEnrolledInDepartment) {
			status = fiber.StatusForbidden
			code = "NOT_ENROLLED_IN_DEPARTMENT"
		} else if errors.Is(err, ErrPoorGPSAccuracyOrMock) {
			status = fiber.StatusUnprocessableEntity
			code = "POOR_GPS_ACCURACY_OR_MOCK"
		} else if errors.Is(err, ErrOutsideLectureGeofence) {
			status = fiber.StatusForbidden
			code = "OUTSIDE_LECTURE_GEOFENCE"
		} else if errors.Is(err, ErrImpossibleTravelDetected) {
			status = fiber.StatusForbidden
			code = "IMPOSSIBLE_TRAVEL_DETECTED"
		} else if errors.Is(err, ErrInvalidOrExpiredQR) {
			status = fiber.StatusBadRequest
			code = "INVALID_OR_EXPIRED_QR"
		} else if errors.Is(err, ErrAlreadyCheckedIn) {
			status = fiber.StatusConflict
			code = "ALREADY_CHECKED_IN"
		}

		return c.Status(status).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    code,
				"message": err.Error(),
			},
		})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"success": true,
		"data":    resp,
	})
}

// GetDynamicQR supplies Class Leaders and Instructors with real-time rotating TOTP codes.
func (h *Handler) GetDynamicQR(c *fiber.Ctx) error {
	sessionIDParam := c.Params("id")
	sessionID, err := uuid.Parse(sessionIDParam)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "INVALID_SESSION_ID",
				"message": "Provided session identifier is not a valid UUID",
			},
		})
	}

	leaderIDStr := middleware.GetUserID(c)
	leaderID, _ := uuid.Parse(leaderIDStr)

	resp, err := h.service.GetDynamicQRForLeader(c.Context(), leaderID, sessionID)
	if err != nil {
		status := fiber.StatusBadRequest
		if errors.Is(err, ErrSessionNotActive) {
			status = fiber.StatusNotFound
		}
		return c.Status(status).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "QR_FETCH_FAILED",
				"message": err.Error(),
			},
		})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"success": true,
		"data":    resp,
	})
}

// GetMyHistory returns historical attendance logs for the authenticated student.
func (h *Handler) GetMyHistory(c *fiber.Ctx) error {
	studentIDStr := middleware.GetUserID(c)
	studentID, err := uuid.Parse(studentIDStr)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "INVALID_STUDENT_ID",
				"message": "Invalid token subject",
			},
		})
	}

	history, err := h.service.GetStudentHistory(c.Context(), studentID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "HISTORY_FETCH_FAILED",
				"message": "Failed to query attendance history",
			},
		})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"success": true,
		"data":    history,
	})
}

// SyncOffline validates and batch-syncs cryptographically signed offline attendance proofs.
func (h *Handler) SyncOffline(c *fiber.Ctx) error {
	studentIDStr := middleware.GetUserID(c)
	studentID, err := uuid.Parse(studentIDStr)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "INVALID_STUDENT_ID",
				"message": "Authenticated student identifier is invalid",
			},
		})
	}

	var req BatchOfflineSyncRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "MALFORMED_JSON",
				"message": "Invalid JSON payload in request body",
			},
		})
	}

	// Validate batch limits (1 to 5 items)
	if err := req.Validate(); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "VALIDATION_ERROR",
				"message": err.Error(),
			},
		})
	}

	resp, err := h.service.SyncOfflineProofs(c.Context(), studentID, &req, c.IP())
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "SYNC_FAILED",
				"message": err.Error(),
			},
		})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"success": true,
		"data":    resp,
	})
}
