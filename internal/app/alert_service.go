package app

import (
	"fmt"
	"sort"
	"time"

	"patrol-platform/internal/domain"
)

// AlertStore is the persistence interface consumed by AlertService.
type AlertStore interface {
	SaveAlert(domain.Alert)
	GetAlert(id string) (domain.Alert, bool)
	AlertByidempotencyKey(key string) (domain.Alert, bool)
	ListAlerts() []domain.Alert
	ListAlertsByStatus(status domain.AlertStatus) []domain.Alert
	ListAlertsByShift(shiftID string) []domain.Alert
}

// AlertService handles alert reporting, signing, dispatch, escalation,
// and offline replay with idempotency.
type AlertService struct {
	store   AlertStore
	clock   domain.Clock
	chiefID string
}

// NewAlertService constructs an AlertService. chiefID is the station chief
// to whom unsigned alerts are escalated.
func NewAlertService(s AlertStore, clock domain.Clock, chiefID string) *AlertService {
	return &AlertService{store: s, clock: clock, chiefID: chiefID}
}

// ReportAlertRequest is the input for reporting an incident.
type ReportAlertRequest struct {
	ShiftID        string
	OfficerID      string
	Type           domain.AlertType
	Description    string
	Location       string
	ReportedAt     time.Time // original device timestamp (may be in the past)
	Offline        bool
	IdempotencyKey string
}

// ReportAlert records a single alert. If the idempotency key was already
// processed the existing record is returned without re-counting.
func (svc *AlertService) ReportAlert(req ReportAlertRequest) (domain.Alert, bool, error) {
	if req.ShiftID == "" || req.OfficerID == "" {
		return domain.Alert{}, false, fmt.Errorf("%w: shift_id and officer_id required", domain.ErrValidation)
	}
	if req.IdempotencyKey == "" {
		return domain.Alert{}, false, fmt.Errorf("%w: idempotency_key required", domain.ErrValidation)
	}
	switch req.Type {
	case domain.AlertTypeFire, domain.AlertTypePoaching, domain.AlertTypeOther:
	default:
		return domain.Alert{}, false, fmt.Errorf("%w: invalid alert type", domain.ErrValidation)
	}

	// Idempotency: if already seen, return the existing record unchanged.
	if existing, ok := svc.store.AlertByidempotencyKey(req.IdempotencyKey); ok {
		return existing, true, nil
	}

	now := svc.clock.Now()
	reportedAt := req.ReportedAt
	if reportedAt.IsZero() {
		reportedAt = now
	}
	alert := domain.Alert{
		ID:             domain.NewID("alert"),
		ShiftID:        req.ShiftID,
		OfficerID:      req.OfficerID,
		Type:           req.Type,
		Description:    req.Description,
		Location:       req.Location,
		Status:         domain.AlertStatusReported,
		ReportedAt:     reportedAt,
		ReceivedAt:     now,
		Offline:        req.Offline,
		IdempotencyKey: req.IdempotencyKey,
	}
	svc.store.SaveAlert(alert)
	return alert, false, nil
}

// ReportAlertBatch replays a batch of offline alerts sorted by original
// timestamp. Each record is idempotent: duplicates are skipped, no records
// are lost, and ordering follows the device clock.
func (svc *AlertService) ReportAlertBatch(reqs []ReportAlertRequest) ([]domain.Alert, error) {
	// Sort by original timestamp to preserve temporal ordering during replay.
	sorted := make([]ReportAlertRequest, len(reqs))
	copy(sorted, reqs)
	sort.SliceStable(sorted, func(i, j int) bool {
		ti := sorted[i].ReportedAt
		tj := sorted[j].ReportedAt
		if ti.IsZero() {
			ti = svc.clock.Now()
		}
		if tj.IsZero() {
			tj = svc.clock.Now()
		}
		return ti.Before(tj)
	})

	results := make([]domain.Alert, 0, len(sorted))
	for _, r := range sorted {
		a, _, err := svc.ReportAlert(r)
		if err != nil {
			return results, err
		}
		results = append(results, a)
	}
	return results, nil
}

// SignAlert records dispatcher acknowledgement of an alert.
func (svc *AlertService) SignAlert(id, dispatcherID string) error {
	a, ok := svc.store.GetAlert(id)
	if !ok {
		return domain.ErrAlertNotFound
	}
	if a.Status == domain.AlertStatusEscalated {
		// Escalated alerts can still be signed by the dispatcher or chief.
	} else if err := a.CanTransitionTo(domain.AlertStatusSigned); err != nil {
		return err
	}
	now := svc.clock.Now()
	a.Status = domain.AlertStatusSigned
	a.SignedBy = dispatcherID
	a.SignedAt = &now
	svc.store.SaveAlert(a)
	return nil
}

// DispatchAlert assigns the alert to a responder and advances the workflow.
func (svc *AlertService) DispatchAlert(id, assigneeID string) error {
	a, ok := svc.store.GetAlert(id)
	if !ok {
		return domain.ErrAlertNotFound
	}
	if err := a.CanTransitionTo(domain.AlertStatusDispatched); err != nil {
		return err
	}
	now := svc.clock.Now()
	a.Status = domain.AlertStatusDispatched
	a.AssignedTo = assigneeID
	a.DispatchedAt = &now
	svc.store.SaveAlert(a)
	return nil
}

// StartProgress moves a dispatched alert to in-progress.
func (svc *AlertService) StartProgress(id string) error {
	a, ok := svc.store.GetAlert(id)
	if !ok {
		return domain.ErrAlertNotFound
	}
	if err := a.CanTransitionTo(domain.AlertStatusInProgress); err != nil {
		return err
	}
	a.Status = domain.AlertStatusInProgress
	svc.store.SaveAlert(a)
	return nil
}

// ResolveAlert marks an in-progress alert resolved.
func (svc *AlertService) ResolveAlert(id string) error {
	a, ok := svc.store.GetAlert(id)
	if !ok {
		return domain.ErrAlertNotFound
	}
	if err := a.CanTransitionTo(domain.AlertStatusResolved); err != nil {
		return err
	}
	now := svc.clock.Now()
	a.Status = domain.AlertStatusResolved
	a.ResolvedAt = &now
	svc.store.SaveAlert(a)
	return nil
}

// CloseAlert closes a resolved or otherwise terminal alert.
func (svc *AlertService) CloseAlert(id string) error {
	a, ok := svc.store.GetAlert(id)
	if !ok {
		return domain.ErrAlertNotFound
	}
	if err := a.CanTransitionTo(domain.AlertStatusClosed); err != nil {
		return err
	}
	now := svc.clock.Now()
	a.Status = domain.AlertStatusClosed
	a.ClosedAt = &now
	svc.store.SaveAlert(a)
	return nil
}

// EscalateAlert moves an unsigned alert to the station chief. This is
// called automatically by the scheduler after the 5-minute deadline or
// manually by a dispatcher.
func (svc *AlertService) EscalateAlert(id string) error {
	a, ok := svc.store.GetAlert(id)
	if !ok {
		return domain.ErrAlertNotFound
	}
	if a.Status != domain.AlertStatusReported {
		return nil
	}
	if err := a.CanTransitionTo(domain.AlertStatusEscalated); err != nil {
		return err
	}
	now := svc.clock.Now()
	a.Status = domain.AlertStatusEscalated
	a.EscalatedTo = svc.chiefID
	a.EscalatedAt = &now
	svc.store.SaveAlert(a)
	return nil
}

// EscalateOverdue scans for reported alerts past the deadline and escalates
// them. Returns the count of alerts escalated.
func (svc *AlertService) EscalateOverdue() int {
	now := svc.clock.Now()
	escalated := 0
	for _, a := range svc.store.ListAlertsByStatus(domain.AlertStatusReported) {
		if a.NeedsEscalation(now) {
			if err := svc.EscalateAlert(a.ID); err == nil {
				escalated++
			}
		}
	}
	return escalated
}

// GetAlert returns an alert by ID.
func (svc *AlertService) GetAlert(id string) (domain.Alert, error) {
	a, ok := svc.store.GetAlert(id)
	if !ok {
		return domain.Alert{}, domain.ErrAlertNotFound
	}
	return a, nil
}

// ListAlerts returns all alerts ordered by reported time.
func (svc *AlertService) ListAlerts() []domain.Alert {
	return svc.store.ListAlerts()
}

// ListAlertsByShift returns alerts for a shift.
func (svc *AlertService) ListAlertsByShift(shiftID string) []domain.Alert {
	return svc.store.ListAlertsByShift(shiftID)
}
