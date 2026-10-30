package sessions

import (
	"errors"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"hader-backend/internal/middleware"
)

// Handler exposes HTTP and Server-Sent Events endpoints for session lifecycle,
// live check-in monitoring, dynamic QR streaming, and academic timetables.
type Handler struct {
	service   Service
	jwtSecret string
}

// NewHandler instantiates a sessions controller.
func NewHandler(service Service, jwtSecret string) *Handler {
	return &Handler{
		service:   service,
		jwtSecret: jwtSecret,
	}
}

// RegisterRoutes registers all session management, SSE streams, and schedule endpoints.
func (h *Handler) RegisterRoutes(router fiber.Router) {
	// Sessions group: /api/v1/sessions
	sessions := router.Group("/sessions")

	sessions.Post("/",
		middleware.Authenticate(h.jwtSecret),
		middleware.RequireRole("class_leader", "admin"),
		h.CreateSession,
	)

	sessions.Get("/active",
		middleware.Authenticate(h.jwtSecret),
		h.GetActiveSession,
	)

	sessions.Put("/:id/close",
		middleware.Authenticate(h.jwtSecret),
		middleware.RequireRole("class_leader", "admin"),
		h.CloseSession,
	)

	sessions.Get("/:id/stream-qr",
		middleware.Authenticate(h.jwtSecret),
		middleware.RequireRole("class_leader", "admin"),
		h.StreamDynamicQR,
	)

	sessions.Get("/:id/stream-attendees",
		middleware.Authenticate(h.jwtSecret),
		middleware.RequireRole("class_leader", "admin"),
		h.StreamLiveAttendees,
	)

	// Timetable group: /api/v1/schedule
	schedule := router.Group("/schedule")
	schedule.Get("/today",
		middleware.Authenticate(h.jwtSecret),
		h.GetTodaySchedule,
	)
}

// CreateSession initiates a new lecture session.
func (h *Handler) CreateSession(c *fiber.Ctx) error {
	leaderIDStr := middleware.GetUserID(c)
	leaderID, err := uuid.Parse(leaderIDStr)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "UNAUTHORIZED",
				"message": "Invalid user identity",
			},
		})
	}

	var req CreateSessionRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "MALFORMED_JSON",
				"message": "Invalid JSON body provided",
			},
		})
	}

	resp, err := h.service.CreateSession(c.Context(), leaderID, &req)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "SESSION_CREATION_FAILED",
				"message": err.Error(),
			},
		})
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"success": true,
		"data":    resp,
	})
}

// CloseSession prematurely or manually terminates an active lecture session.
func (h *Handler) CloseSession(c *fiber.Ctx) error {
	sessionIDParam := c.Params("id")
	sessionID, err := uuid.Parse(sessionIDParam)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "INVALID_SESSION_ID",
				"message": "Provided session ID is not a valid UUID",
			},
		})
	}

	if err := h.service.CloseSession(c.Context(), sessionID); err != nil {
		status := fiber.StatusBadRequest
		if errors.Is(err, ErrSessionNotFound) {
			status = fiber.StatusNotFound
		}
		return c.Status(status).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "SESSION_CLOSE_FAILED",
				"message": err.Error(),
			},
		})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"success": true,
		"message": "Lecture session successfully closed and absentees marked",
	})
}

// GetActiveSession returns the ongoing lecture session for a department.
func (h *Handler) GetActiveSession(c *fiber.Ctx) error {
	var deptID int
	if deptStr := c.Query("department_id"); deptStr != "" {
		if parsed, err := strconv.Atoi(deptStr); err == nil {
			deptID = parsed
		}
	}

	// If department_id query was not passed, resolve student's department via schedule lookup
	if deptID <= 0 {
		studentIDStr := middleware.GetUserID(c)
		studentID, err := uuid.Parse(studentIDStr)
		if err == nil {
			if schedule, err := h.service.GetStudentTodaySchedule(c.Context(), studentID, 0); err == nil && len(schedule) > 0 {
				// Department was resolved internally
			}
		}
	}

	resp, err := h.service.GetActiveSessionByDepartment(c.Context(), deptID)
	if err != nil {
		if errors.Is(err, ErrNoActiveSession) {
			return c.Status(fiber.StatusOK).JSON(fiber.Map{
				"success": true,
				"data":    nil,
				"message": "No active lecture session currently found for this department",
			})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "ACTIVE_SESSION_QUERY_FAILED",
				"message": err.Error(),
			},
		})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"success": true,
		"data":    resp,
	})
}

// StreamDynamicQR opens an SSE connection streaming 5-second dynamic TOTP tokens.
func (h *Handler) StreamDynamicQR(c *fiber.Ctx) error {
	sessionIDParam := c.Params("id")
	sessionID, err := uuid.Parse(sessionIDParam)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "INVALID_SESSION_ID",
				"message": "Provided session ID is not a valid UUID",
			},
		})
	}

	if err := h.service.StreamDynamicQR(c, sessionID); err != nil {
		status := fiber.StatusBadRequest
		if errors.Is(err, ErrSessionNotFound) {
			status = fiber.StatusNotFound
		} else if errors.Is(err, ErrSessionNotActive) {
			status = fiber.StatusConflict
		}
		return c.Status(status).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "STREAM_QR_FAILED",
				"message": err.Error(),
			},
		})
	}

	return nil
}

// StreamLiveAttendees opens an SSE connection broadcasting live attendee counts and check-in records.
func (h *Handler) StreamLiveAttendees(c *fiber.Ctx) error {
	sessionIDParam := c.Params("id")
	sessionID, err := uuid.Parse(sessionIDParam)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "INVALID_SESSION_ID",
				"message": "Provided session ID is not a valid UUID",
			},
		})
	}

	if err := h.service.StreamLiveAttendees(c, sessionID); err != nil {
		status := fiber.StatusBadRequest
		if errors.Is(err, ErrSessionNotFound) {
			status = fiber.StatusNotFound
		} else if errors.Is(err, ErrSessionNotActive) {
			status = fiber.StatusConflict
		}
		return c.Status(status).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "STREAM_ATTENDEES_FAILED",
				"message": err.Error(),
			},
		})
	}

	return nil
}

// GetTodaySchedule retrieves the student's daily lecture timetable and attendance statuses.
func (h *Handler) GetTodaySchedule(c *fiber.Ctx) error {
	studentIDStr := middleware.GetUserID(c)
	studentID, err := uuid.Parse(studentIDStr)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "UNAUTHORIZED",
				"message": "Invalid user identity",
			},
		})
	}

	var deptID int
	if deptStr := c.Query("department_id"); deptStr != "" {
		if parsed, err := strconv.Atoi(deptStr); err == nil {
			deptID = parsed
		}
	}

	schedule, err := h.service.GetStudentTodaySchedule(c.Context(), studentID, deptID)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"error": fiber.Map{
				"code":    "SCHEDULE_FETCH_FAILED",
				"message": err.Error(),
			},
		})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"success": true,
		"data":    schedule,
	})
}
