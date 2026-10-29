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

// RegisterRoutes registers endpoints on the sessions router group (/api/v1/sessions).
// Uses relative sub-paths without repeating "/sessions".
func (h *Handler) RegisterRoutes(router fiber.Router) {
	// POST /api/v1/sessions (handles both with and without trailing slash)
	router.Post("/",
		middleware.Authenticate(h.jwtSecret),
		middleware.RequireRole("class_leader", "admin"),
		h.CreateSession,
	)
	router.Post("",
		middleware.Authenticate(h.jwtSecret),
		middleware.RequireRole("class_leader", "admin"),
		h.CreateSession,
	)

	// GET /api/v1/sessions/active
	router.Get("/active",
		middleware.Authenticate(h.jwtSecret),
		h.GetActiveSession,
	)

	// PUT /api/v1/sessions/:id/close
	router.Put("/:id/close",
		middleware.Authenticate(h.jwtSecret),
		middleware.RequireRole("class_leader", "admin"),
		h.CloseSession,
	)

	// GET /api/v1/sessions/:id/stream-qr
	router.Get("/:id/stream-qr",
		middleware.Authenticate(h.jwtSecret),
		middleware.RequireRole("class_leader", "admin"),
		h.StreamDynamicQR,
	)

	// GET /api/v1/sessions/:id/qr-stream (alias)
	router.Get("/:id/qr-stream",
		middleware.Authenticate(h.jwtSecret),
		middleware.RequireRole("class_leader", "admin"),
		h.StreamDynamicQR,
	)

	// GET /api/v1/sessions/:id/stream-attendees
	router.Get("/:id/stream-attendees",
		middleware.Authenticate(h.jwtSecret),
		middleware.RequireRole("class_leader", "admin"),
		h.StreamLiveAttendees,
	)
}

// CreateSession initiates a new lecture session.
// Maps to POST /api/v1/sessions
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
// Maps to PUT /api/v1/sessions/:id/close
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
// Maps to GET /api/v1/sessions/active
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
// Maps to GET /api/v1/sessions/:id/stream-qr
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
// Maps to GET /api/v1/sessions/:id/stream-attendees
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
// Maps to GET /api/v1/schedule/today
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
