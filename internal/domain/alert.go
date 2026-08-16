package domain

import (
	"fmt"
	"time"
)

// AlertType classifies a reported incident.
type AlertType string

const (
	AlertTypeFire     AlertType = "fire"
	AlertTypePoaching AlertType = "poaching"
	AlertTypeOther    AlertType = "other"
)

// AlertStatus tracks the alert handling workflow.
type AlertStatus string

const (
	AlertStatusReported   AlertStatus = "reported"
	AlertStatusSigned     AlertStatus = "signed"
	AlertStatusDispatched AlertStatus = "dispatched"
	AlertStatusInProgress AlertStatus = "in_progress"
	AlertStatusResolved   AlertStatus = "resolved"
	AlertStatusClosed     AlertStatus = "closed"
	AlertStatusEscalated  AlertStatus = "escalated"
)

// Alert is an incident ticket reported by a patrol officer.
type Alert struct {
	ID             string      `json:"id"`
	ShiftID        string      `json:"shift_id"`
	OfficerID      string      `json:"officer_id"`
	Type           AlertType   `json:"type"`
	Description    string      `json:"description"`
	Location       string      `json:"location"`
	Status         AlertStatus `json:"status"`
	ReportedAt     time.Time   `json:"reported_at"`
	ReceivedAt     time.Time   `json:"received_at"`
	Offline        bool        `json:"offline"`
	SignedBy       string      `json:"signed_by"`
	SignedAt       *time.Time  `json:"signed_at,omitempty"`
	EscalatedTo    string      `json:"escalated_to"`
	EscalatedAt    *time.Time  `json:"escalated_at,omitempty"`
	AssignedTo     string      `json:"assigned_to"`
	DispatchedAt   *time.Time  `json:"dispatched_at,omitempty"`
	ResolvedAt     *time.Time  `json:"resolved_at,omitempty"`
	ClosedAt       *time.Time  `json:"closed_at,omitempty"`
	IdempotencyKey string      `json:"idempotency_key"`
}

// NeedsEscalation reports whether an alert has sat unsigned past the deadline.
func (a Alert) NeedsEscalation(now time.Time) bool {
	if a.Status != AlertStatusReported {
		return false
	}
	return now.Sub(a.ReportedAt) >= EscalationDeadline
}

// CanTransitionTo validates alert state-machine edges.
func (a Alert) CanTransitionTo(target AlertStatus) error {
	allowed := map[AlertStatus][]AlertStatus{
		AlertStatusReported:   {AlertStatusSigned, AlertStatusEscalated, AlertStatusClosed},
		AlertStatusEscalated:  {AlertStatusSigned, AlertStatusClosed},
		AlertStatusSigned:     {AlertStatusDispatched, AlertStatusClosed},
		AlertStatusDispatched: {AlertStatusInProgress, AlertStatusClosed},
		AlertStatusInProgress: {AlertStatusResolved, AlertStatusClosed},
		AlertStatusResolved:   {AlertStatusClosed},
		AlertStatusClosed:     {},
	}
	for _, ok := range allowed[a.Status] {
		if ok == target {
			return nil
		}
	}
	return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, a.Status, target)
}
