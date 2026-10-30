package schedule

import (
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"hader-backend/internal/middleware"
	"hader-backend/internal/modules/sessions"
)

// Handler handles academic timetable and schedule endpoints.
type Handler struct {
	sessionService sessions.Service
	jwtSecret      string
}

// NewHandler instantiates a schedule HTTP controller.
func NewHandler(sessionService sessions.Service, jwtSecret string) *Handler {
	return &Handler{
		sessionService: sessionService,
		jwtSecret:      jwtSecret,
	}
}

// RegisterRoutes registers schedule endpoints onto the schedule router group (/api/v1/schedule).
func (h *Handler) RegisterRoutes(router fiber.Router) {
	router.Get("/today",
		middleware.Authenticate(h.jwtSecret),
		h.GetTodaySchedule,
	)
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

	schedule, err := h.sessionService.GetStudentTodaySchedule(c.Context(), studentID, deptID)
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

