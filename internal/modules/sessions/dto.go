package sessions

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// CreateSessionRequest captures parameters to initiate a lecture session.
type CreateSessionRequest struct {
	SubjectName     string  `json:"subject_name"`
	InstructorName  string  `json:"instructor_name"`
	DepartmentID    int     `json:"department_id"`
	GeofenceLat     float64 `json:"geofence_lat"`
	GeofenceLng     float64 `json:"geofence_lng"`
	GeofenceRadius  int     `json:"geofence_radius"`
	DurationMinutes int     `json:"duration_minutes"`
}

// Validate executes input constraints on session initiation.
func (r *CreateSessionRequest) Validate() error {
	r.SubjectName = strings.TrimSpace(r.SubjectName)
	if r.SubjectName == "" {
		return errors.New("subject_name is mandatory")
	}

	r.InstructorName = strings.TrimSpace(r.InstructorName)
	if r.InstructorName == "" {
		return errors.New("instructor_name is mandatory")
	}

	if r.DepartmentID <= 0 {
		return errors.New("department_id must be a positive integer")
	}

	if r.GeofenceLat < -90.0 || r.GeofenceLat > 90.0 {
		return errors.New("geofence_lat must be between -90.0 and 90.0")
	}

	if r.GeofenceLng < -180.0 || r.GeofenceLng > 180.0 {
		return errors.New("geofence_lng must be between -180.0 and 180.0")
	}

	if r.GeofenceRadius <= 0 {
		r.GeofenceRadius = 40 // default 40 meters
	}

	if r.DurationMinutes < 15 || r.DurationMinutes > 240 {
		return errors.New("duration_minutes must be between 15 and 240 minutes")
	}

	return nil
}

// SessionSummaryResponse delivers aggregate status and attendance statistics for a session.
type SessionSummaryResponse struct {
	SessionID      uuid.UUID `json:"session_id"`
	SubjectName    string    `json:"subject_name"`
	InstructorName string    `json:"instructor_name"`
	DepartmentID   *int      `json:"department_id"`
	Status         string    `json:"status"`
	TotalAttendees int       `json:"total_attendees"`
	StartsAt       time.Time `json:"starts_at"`
	EndsAt         time.Time `json:"ends_at"`
}

// LiveAttendeeEvent is streamed over SSE to the instructor's live monitor as students check in.
type LiveAttendeeEvent struct {
	StudentID        uuid.UUID `json:"student_id"`
	AcademicID       string    `json:"academic_id"`
	FullName         string    `json:"full_name"`
	CheckInTime      time.Time `json:"check_in_time"`
	VerificationMode string    `json:"verification_mode"`
	TotalCount       int       `json:"total_count"`
}

// StudentScheduleItem represents an academic timetable entry tailored for the student's department.
type StudentScheduleItem struct {
	SessionID      *string `json:"session_id,omitempty"`
	SubjectName    string  `json:"subject_name"`
	InstructorName string  `json:"instructor_name"`
	DayOfWeek      string  `json:"day_of_week"`
	StartTime      string  `json:"start_time"`
	EndTime        string  `json:"end_time"`
	IsActiveNow    bool    `json:"is_active_now"`
	HasCheckedIn   bool    `json:"has_checked_in"`
}
