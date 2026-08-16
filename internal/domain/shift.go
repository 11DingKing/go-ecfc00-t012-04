package domain

import (
	"fmt"
	"time"
)

// ShiftStatus represents the lifecycle stage of a patrol shift.
type ShiftStatus string

const (
	ShiftStatusDraft     ShiftStatus = "draft"
	ShiftStatusSubmitted ShiftStatus = "submitted"
	ShiftStatusApproved  ShiftStatus = "approved"
	ShiftStatusActive    ShiftStatus = "active"
	ShiftStatusCompleted ShiftStatus = "completed"
	ShiftStatusCancelled ShiftStatus = "cancelled"
)

// ShiftPriority determines equipment-allocation precedence.
// Lower numeric value means higher priority.
type ShiftPriority int

const (
	PriorityHigh   ShiftPriority = 1
	PriorityNormal ShiftPriority = 2
	PriorityLow    ShiftPriority = 3
)

func (p ShiftPriority) String() string {
	switch p {
	case PriorityHigh:
		return "high"
	case PriorityNormal:
		return "normal"
	case PriorityLow:
		return "low"
	default:
		return "unknown"
	}
}

// Shift is a scheduled patrol assignment for one officer on one route.
type Shift struct {
	ID          string        `json:"id"`
	StationID   string        `json:"station_id"`
	OfficerID   string        `json:"officer_id"`
	RouteID     string        `json:"route_id"`
	WeekStart   time.Time     `json:"week_start"`
	DepartureAt time.Time     `json:"departure_at"`
	Priority    ShiftPriority `json:"priority"`
	Status      ShiftStatus   `json:"status"`
	CreatedBy   string        `json:"created_by"`
	ApprovedBy  string        `json:"approved_by"`
	CreatedAt   time.Time     `json:"created_at"`
	UpdatedAt   time.Time     `json:"updated_at"`
}

// CanTransitionTo validates a shift state-machine edge.
func (s Shift) CanTransitionTo(target ShiftStatus) error {
	allowed := map[ShiftStatus][]ShiftStatus{
		ShiftStatusDraft:     {ShiftStatusSubmitted, ShiftStatusCancelled},
		ShiftStatusSubmitted: {ShiftStatusApproved, ShiftStatusCancelled},
		ShiftStatusApproved:  {ShiftStatusActive, ShiftStatusCancelled},
		ShiftStatusActive:    {ShiftStatusCompleted, ShiftStatusCancelled},
		ShiftStatusCompleted: {},
		ShiftStatusCancelled: {},
	}
	for _, ok := range allowed[s.Status] {
		if ok == target {
			return nil
		}
	}
	return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, s.Status, target)
}
