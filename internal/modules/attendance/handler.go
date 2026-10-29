package attendance

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"hader-backend/internal/middleware"
)

// Handler handles HTTP requests for lecture attendance and check-in verification.
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

// RegisterRoutes registers endpoints on the attendance router group (/api/v1/attendance).
// Uses relative sub-paths without repeating "/attendance".
func (h *Handler) RegisterRoutes(router fiber.Router) {
	router.Post("/check-in",
		middleware.Authenticate(h.jwtSecret),
		middleware.AttendanceLimiter(),
		h.CheckIn,
	)
	router.Get("/history/me",
		middleware.Authenticate(h.jwtSecret),
		h.History,
	)
	router.Post("/sync-offline",
		middleware.Authenticate(h.jwtSecret),
		middleware.AttendanceLimiter(),
		h.SyncOffline,
	)
}

// CheckIn processes dynamic QR scans, GPS geofencing, and atomic check-in locks.
// Maps to POST /api/v1/attendance/check-in
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

// History returns historical attendance logs for the authenticated student.
// Maps to GET /api/v1/attendance/history/me
func (h *Handler) History(c *fiber.Ctx) error {
	return h.GetMyHistory(c)
}

// GetMyHistory returns historical attendance logs (alias of History for backward compatibility).
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
// Maps to POST /api/v1/attendance/sync-offline
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
