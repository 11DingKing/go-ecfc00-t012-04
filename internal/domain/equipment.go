package domain

import (
	"fmt"
	"time"
)

// EquipmentType identifies a class of patrol equipment.
type EquipmentType string

const (
	EquipmentTypeDrone          EquipmentType = "drone"
	EquipmentTypeInfraredCamera EquipmentType = "infrared_camera"
	EquipmentTypeSatellitePhone EquipmentType = "satellite_phone"
)

// EquipmentStatus tracks physical availability of an item.
type EquipmentStatus string

const (
	EquipmentStatusAvailable   EquipmentStatus = "available"
	EquipmentStatusLocked      EquipmentStatus = "locked"
	EquipmentStatusBorrowed    EquipmentStatus = "borrowed"
	EquipmentStatusMaintenance EquipmentStatus = "maintenance"
)

// Equipment is a physical asset managed by a station keeper.
type Equipment struct {
	ID              string          `json:"id"`
	Name            string          `json:"name"`
	Type            EquipmentType   `json:"type"`
	HomeStationID   string          `json:"home_station_id"`
	Status          EquipmentStatus `json:"status"`
	LockedByShiftID string          `json:"locked_by_shift_id"`
	HeldBy          string          `json:"held_by"`
}

// ClaimStatus tracks the lifecycle of an equipment request.
type ClaimStatus string

const (
	ClaimStatusLocked    ClaimStatus = "locked"
	ClaimStatusBorrowed  ClaimStatus = "borrowed"
	ClaimStatusReturned  ClaimStatus = "returned"
	ClaimStatusCancelled ClaimStatus = "cancelled"
	ClaimStatusQueued    ClaimStatus = "queued"
)

// IsTerminal reports whether the claim has reached a final state.
func (c ClaimStatus) IsTerminal() bool {
	return c == ClaimStatusReturned || c == ClaimStatusCancelled
}

// EquipmentClaim represents an officer's request to use equipment for a shift.
type EquipmentClaim struct {
	ID                 string        `json:"id"`
	ShiftID            string        `json:"shift_id"`
	EquipmentID        string        `json:"equipment_id"`
	EquipmentType      EquipmentType `json:"equipment_type"`
	OfficerID          string        `json:"officer_id"`
	StationID          string        `json:"station_id"`
	Status             ClaimStatus   `json:"status"`
	Priority           ShiftPriority `json:"priority"`
	SubmittedAt        time.Time     `json:"submitted_at"`
	LockedAt           *time.Time    `json:"locked_at,omitempty"`
	BorrowedAt         *time.Time    `json:"borrowed_at,omitempty"`
	ReturnedAt         *time.Time    `json:"returned_at,omitempty"`
	LossReported       bool          `json:"loss_reported"`
	LossNote           string        `json:"loss_note"`
	CrossStation       bool          `json:"cross_station"`
	DispatcherApproved bool          `json:"dispatcher_approved"`
}

// Beats returns true if this claim should win over the other when both
// contend for the same equipment. Comparison is by priority (lower wins)
// then by submission time (earlier wins).
func (c EquipmentClaim) Beats(other EquipmentClaim) bool {
	if c.Priority != other.Priority {
		return c.Priority < other.Priority
	}
	return c.SubmittedAt.Before(other.SubmittedAt)
}

// CanBorrow validates the transition from locked to borrowed.
func (c EquipmentClaim) CanBorrow() error {
	if c.Status != ClaimStatusLocked {
		return fmt.Errorf("%w: cannot borrow claim in status %s", ErrInvalidTransition, c.Status)
	}
	return nil
}

// CanReturn validates the transition from borrowed to returned.
func (c EquipmentClaim) CanReturn() error {
	if c.Status != ClaimStatusBorrowed {
		return fmt.Errorf("%w: cannot return claim in status %s", ErrInvalidTransition, c.Status)
	}
	return nil
}
