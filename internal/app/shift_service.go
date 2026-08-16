package app

import (
	"fmt"
	"time"

	"patrol-platform/internal/domain"
)

// ShiftService orchestrates the weekly shift generation and approval workflow.
type ShiftService struct {
	store ShiftStore
	clock domain.Clock
}

// ShiftStore is the persistence interface consumed by ShiftService.
type ShiftStore interface {
	SaveShift(domain.Shift)
	GetShift(id string) (domain.Shift, bool)
	ListShifts() []domain.Shift
	ListShiftsByWeek(weekStart time.Time) []domain.Shift
}

// NewShiftService constructs a ShiftService.
func NewShiftService(s ShiftStore, clock domain.Clock) *ShiftService {
	return &ShiftService{store: s, clock: clock}
}

// AssignmentSpec describes one officer's weekly assignment.
type AssignmentSpec struct {
	OfficerID   string
	RouteID     string
	DepartureAt time.Time
	Priority    domain.ShiftPriority
}

// GenerateWeeklyShifts creates draft shifts for every assignment in a week.
// The station chief calls this once per week; shifts start in draft and
// must be submitted then approved before they become active.
func (svc *ShiftService) GenerateWeeklyShifts(stationID, chiefID string, weekStart time.Time, assignments []AssignmentSpec) ([]domain.Shift, error) {
	if stationID == "" || chiefID == "" {
		return nil, fmt.Errorf("%w: station_id and chief_id required", domain.ErrValidation)
	}
	if len(assignments) == 0 {
		return nil, fmt.Errorf("%w: at least one assignment required", domain.ErrValidation)
	}
	if weekStart.IsZero() {
		return nil, fmt.Errorf("%w: week_start required", domain.ErrValidation)
	}

	now := svc.clock.Now()
	created := make([]domain.Shift, 0, len(assignments))
	for _, a := range assignments {
		if a.OfficerID == "" || a.RouteID == "" {
			return nil, fmt.Errorf("%w: officer_id and route_id required", domain.ErrValidation)
		}
		if a.DepartureAt.Before(now) {
			return nil, fmt.Errorf("%w: departure must be in the future", domain.ErrValidation)
		}
		if a.Priority == 0 {
			a.Priority = domain.PriorityNormal
		}
		sh := domain.Shift{
			ID:          domain.NewID("shift"),
			StationID:   stationID,
			OfficerID:   a.OfficerID,
			RouteID:     a.RouteID,
			WeekStart:   weekStart,
			DepartureAt: a.DepartureAt,
			Priority:    a.Priority,
			Status:      domain.ShiftStatusDraft,
			CreatedBy:   chiefID,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		svc.store.SaveShift(sh)
		created = append(created, sh)
	}
	return created, nil
}

// SubmitShift moves a draft shift to submitted for chief approval.
func (svc *ShiftService) SubmitShift(id string) error {
	sh, ok := svc.store.GetShift(id)
	if !ok {
		return domain.ErrShiftNotFound
	}
	if err := sh.CanTransitionTo(domain.ShiftStatusSubmitted); err != nil {
		return err
	}
	sh.Status = domain.ShiftStatusSubmitted
	sh.UpdatedAt = svc.clock.Now()
	svc.store.SaveShift(sh)
	return nil
}

// ApproveShift approves a submitted shift. Only the chief may approve.
func (svc *ShiftService) ApproveShift(id, chiefID string) error {
	sh, ok := svc.store.GetShift(id)
	if !ok {
		return domain.ErrShiftNotFound
	}
	if err := sh.CanTransitionTo(domain.ShiftStatusApproved); err != nil {
		return err
	}
	sh.Status = domain.ShiftStatusApproved
	sh.ApprovedBy = chiefID
	sh.UpdatedAt = svc.clock.Now()
	svc.store.SaveShift(sh)
	return nil
}

// ActivateShift marks an approved shift active when departure time arrives.
func (svc *ShiftService) ActivateShift(id string) error {
	sh, ok := svc.store.GetShift(id)
	if !ok {
		return domain.ErrShiftNotFound
	}
	if err := sh.CanTransitionTo(domain.ShiftStatusActive); err != nil {
		return err
	}
	sh.Status = domain.ShiftStatusActive
	sh.UpdatedAt = svc.clock.Now()
	svc.store.SaveShift(sh)
	return nil
}

// CompleteShift marks an active shift completed.
func (svc *ShiftService) CompleteShift(id string) error {
	sh, ok := svc.store.GetShift(id)
	if !ok {
		return domain.ErrShiftNotFound
	}
	if err := sh.CanTransitionTo(domain.ShiftStatusCompleted); err != nil {
		return err
	}
	sh.Status = domain.ShiftStatusCompleted
	sh.UpdatedAt = svc.clock.Now()
	svc.store.SaveShift(sh)
	return nil
}

// CancelShift cancels a shift that has not yet completed.
func (svc *ShiftService) CancelShift(id string) error {
	sh, ok := svc.store.GetShift(id)
	if !ok {
		return domain.ErrShiftNotFound
	}
	if err := sh.CanTransitionTo(domain.ShiftStatusCancelled); err != nil {
		return err
	}
	sh.Status = domain.ShiftStatusCancelled
	sh.UpdatedAt = svc.clock.Now()
	svc.store.SaveShift(sh)
	return nil
}

// ActivateDueShifts promotes all approved shifts whose departure time has
// passed to active. Called by the background scheduler.
func (svc *ShiftService) ActivateDueShifts() int {
	now := svc.clock.Now()
	activated := 0
	for _, sh := range svc.store.ListShifts() {
		if sh.Status == domain.ShiftStatusApproved && !sh.DepartureAt.After(now) {
			_ = svc.ActivateShift(sh.ID)
			activated++
		}
	}
	return activated
}

// GetShift returns a shift by ID.
func (svc *ShiftService) GetShift(id string) (domain.Shift, error) {
	sh, ok := svc.store.GetShift(id)
	if !ok {
		return domain.Shift{}, domain.ErrShiftNotFound
	}
	return sh, nil
}

// ListShiftsByWeek returns shifts for a given week.
func (svc *ShiftService) ListShiftsByWeek(weekStart time.Time) []domain.Shift {
	return svc.store.ListShiftsByWeek(weekStart)
}

// ListAllShifts returns every shift.
func (svc *ShiftService) ListAllShifts() []domain.Shift {
	return svc.store.ListShifts()
}
