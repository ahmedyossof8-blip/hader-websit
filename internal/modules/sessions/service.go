package sessions

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"hader-backend/internal/utils"
)

var (
	ErrSessionNotFound      = errors.New("lecture session not found")
	ErrSessionNotActive     = errors.New("lecture session is not active")
	ErrSessionAlreadyClosed = errors.New("lecture session is already closed")
	ErrNoActiveSession      = errors.New("no active lecture session found for this department")
	ErrInvalidDepartment    = errors.New("department identifier must be specified or enrolled")
)

// Service specifies high-level business capabilities for lecture session lifecycles,
// real-time SSE streaming hubs, and academic timetable retrieval.
type Service interface {
	CreateSession(ctx context.Context, leaderID uuid.UUID, req *CreateSessionRequest) (*SessionSummaryResponse, error)
	GetSessionByID(ctx context.Context, sessionID uuid.UUID) (*SessionSummaryResponse, error)
	GetActiveSessionByDepartment(ctx context.Context, departmentID int) (*SessionSummaryResponse, error)
	CloseSession(ctx context.Context, sessionID uuid.UUID) error
	StreamDynamicQR(c *fiber.Ctx, sessionID uuid.UUID) error
	StreamLiveAttendees(c *fiber.Ctx, sessionID uuid.UUID) error
	GetStudentTodaySchedule(ctx context.Context, studentID uuid.UUID, departmentID int) ([]StudentScheduleItem, error)
}

type sessionService struct {
	repo Repository
}

// NewService instantiates a production lecture session management service.
func NewService(repo Repository) Service {
	return &sessionService{repo: repo}
}

// CreateSession initiates a new active lecture session with geofence boundaries and TOTP rotation.
func (s *sessionService) CreateSession(ctx context.Context, leaderID uuid.UUID, req *CreateSessionRequest) (*SessionSummaryResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("invalid session request: %w", err)
	}

	now := time.Now().UTC()
	endsAt := now.Add(time.Duration(req.DurationMinutes) * time.Minute)

	deptID := req.DepartmentID
	session := &LectureSession{
		ID:             uuid.New(),
		SubjectName:    req.SubjectName,
		InstructorName: &req.InstructorName,
		DepartmentID:   &deptID,
		GeofenceLat:    req.GeofenceLat,
		GeofenceLng:    req.GeofenceLng,
		GeofenceRadius: req.GeofenceRadius,
		Status:         StatusActive,
		StartsAt:       now,
		EndsAt:         endsAt,
	}

	if err := s.repo.CreateSession(ctx, session); err != nil {
		return nil, fmt.Errorf("failed creating session: %w", err)
	}

	return &SessionSummaryResponse{
		SessionID:      session.ID,
		SubjectName:    session.SubjectName,
		InstructorName: req.InstructorName,
		DepartmentID:   session.DepartmentID,
		Status:         string(session.Status),
		TotalAttendees: 0,
		StartsAt:       session.StartsAt,
		EndsAt:         session.EndsAt,
	}, nil
}

// GetSessionByID fetches session details along with the latest attendee tally.
func (s *sessionService) GetSessionByID(ctx context.Context, sessionID uuid.UUID) (*SessionSummaryResponse, error) {
	session, err := s.repo.GetSessionByID(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("repository error: %w", err)
	}
	if session == nil {
		return nil, ErrSessionNotFound
	}

	total, err := s.repo.GetSessionAttendeesCount(ctx, sessionID)
	if err != nil {
		total = 0
	}

	instructor := "هيئة التدريس"
	if session.InstructorName != nil && *session.InstructorName != "" {
		instructor = *session.InstructorName
	}

	return &SessionSummaryResponse{
		SessionID:      session.ID,
		SubjectName:    session.SubjectName,
		InstructorName: instructor,
		DepartmentID:   session.DepartmentID,
		Status:         string(session.Status),
		TotalAttendees: total,
		StartsAt:       session.StartsAt,
		EndsAt:         session.EndsAt,
	}, nil
}

// GetActiveSessionByDepartment retrieves the currently active lecture for a given department.
func (s *sessionService) GetActiveSessionByDepartment(ctx context.Context, departmentID int) (*SessionSummaryResponse, error) {
	session, err := s.repo.GetActiveSessionByDepartment(ctx, departmentID)
	if err != nil {
		return nil, fmt.Errorf("failed fetching active session: %w", err)
	}
	if session == nil {
		return nil, ErrNoActiveSession
	}

	total, err := s.repo.GetSessionAttendeesCount(ctx, session.ID)
	if err != nil {
		total = 0
	}

	instructor := "هيئة التدريس"
	if session.InstructorName != nil && *session.InstructorName != "" {
		instructor = *session.InstructorName
	}

	return &SessionSummaryResponse{
		SessionID:      session.ID,
		SubjectName:    session.SubjectName,
		InstructorName: instructor,
		DepartmentID:   session.DepartmentID,
		Status:         string(session.Status),
		TotalAttendees: total,
		StartsAt:       session.StartsAt,
		EndsAt:         session.EndsAt,
	}, nil
}

// CloseSession terminates an ongoing session and triggers atomic absentee marking.
func (s *sessionService) CloseSession(ctx context.Context, sessionID uuid.UUID) error {
	session, err := s.repo.GetSessionByID(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("error verifying session: %w", err)
	}
	if session == nil {
		return ErrSessionNotFound
	}
	if session.Status == StatusClosed {
		return nil // Idempotent close
	}

	if err := s.repo.CloseSession(ctx, sessionID); err != nil {
		return fmt.Errorf("failed to close session: %w", err)
	}

	deptID := 0
	if session.DepartmentID != nil {
		deptID = *session.DepartmentID
	}

	// Auto-mark absent records for non-attending students in the department
	_, _ = s.repo.AutoMarkAbsenteesTx(ctx, sessionID, deptID)
	return nil
}

// StreamDynamicQR pushes real-time dynamic 5-second rotating TOTP QR codes via Server-Sent Events (SSE).
func (s *sessionService) StreamDynamicQR(c *fiber.Ctx, sessionID uuid.UUID) error {
	session, err := s.repo.GetSessionByID(c.Context(), sessionID)
	if err != nil {
		return fmt.Errorf("failed loading session: %w", err)
	}
	if session == nil {
		return ErrSessionNotFound
	}
	if session.Status != StatusActive {
		return ErrSessionNotActive
	}

	// Configure HTTP headers for standards-compliant Server-Sent Events (SSE)
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Set("Transfer-Encoding", "chunked")
	c.Set("X-Accel-Buffering", "no")

	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		emitToken := func() error {
			token, remaining, err := utils.GenerateDynamicTOTP(session.QRSecret, 5)
			if err != nil {
				return err
			}

			payload := fiber.Map{
				"token":      token,
				"expires_in": remaining,
				"session_id": session.ID.String(),
				"timestamp":  time.Now().UTC().Format(time.RFC3339),
			}

			data, err := json.Marshal(payload)
			if err != nil {
				return err
			}

			if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
				return err
			}
			return w.Flush()
		}

		// Push first event immediately upon client connection
		if err := emitToken(); err != nil {
			return
		}

		for {
			select {
			case <-c.Context().Done():
				// Client disconnected; terminate goroutine immediately to avoid memory leaks
				return
			case <-ticker.C:
				if time.Now().After(session.EndsAt) {
					fmt.Fprintf(w, "event: session_expired\ndata: {\"message\": \"Lecture session elapsed\"}\n\n")
					_ = w.Flush()
					return
				}
				if err := emitToken(); err != nil {
					return
				}
			}
		}
	})

	return nil
}

// StreamLiveAttendees broadcasts real-time check-in updates and total attendee counts via SSE.
func (s *sessionService) StreamLiveAttendees(c *fiber.Ctx, sessionID uuid.UUID) error {
	session, err := s.repo.GetSessionByID(c.Context(), sessionID)
	if err != nil {
		return fmt.Errorf("failed loading session: %w", err)
	}
	if session == nil {
		return ErrSessionNotFound
	}
	if session.Status != StatusActive {
		return ErrSessionNotActive
	}

	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Set("Transfer-Encoding", "chunked")
	c.Set("X-Accel-Buffering", "no")

	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		emitAttendees := func() error {
			qCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			count, err := s.repo.GetSessionAttendeesCount(qCtx, sessionID)
			if err != nil {
				return err
			}

			recent, err := s.repo.GetRecentAttendees(qCtx, sessionID, 10)
			if err != nil {
				return err
			}

			payload := fiber.Map{
				"session_id":       sessionID.String(),
				"total_count":      count,
				"recent_attendees": recent,
				"timestamp":        time.Now().UTC().Format(time.RFC3339),
			}

			data, err := json.Marshal(payload)
			if err != nil {
				return err
			}

			if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
				return err
			}
			return w.Flush()
		}

		// Initial push
		if err := emitAttendees(); err != nil {
			return
		}

		for {
			select {
			case <-c.Context().Done():
				return
			case <-ticker.C:
				if time.Now().After(session.EndsAt) {
					fmt.Fprintf(w, "event: session_expired\ndata: {\"message\": \"Lecture session elapsed\"}\n\n")
					_ = w.Flush()
					return
				}
				if err := emitAttendees(); err != nil {
					return
				}
			}
		}
	})

	return nil
}

// GetStudentTodaySchedule returns today's academic timetable for the student's department,
// indicating active lecture sessions and student check-in status.
func (s *sessionService) GetStudentTodaySchedule(ctx context.Context, studentID uuid.UUID, departmentID int) ([]StudentScheduleItem, error) {
	if departmentID <= 0 {
		dept, err := s.repo.GetStudentDepartment(ctx, studentID)
		if err != nil {
			return nil, fmt.Errorf("failed resolving department: %w", err)
		}
		if dept == nil || *dept <= 0 {
			return nil, ErrInvalidDepartment
		}
		departmentID = *dept
	}

	items, err := s.repo.GetStudentTodaySchedule(ctx, studentID, departmentID)
	if err != nil {
		return nil, fmt.Errorf("failed fetching timetable: %w", err)
	}

	return items, nil
}
