package app

import (
	"fmt"
	"time"

	"patrol-platform/internal/domain"
)

// EquipmentStore is the persistence interface consumed by EquipmentService.
type EquipmentStore interface {
	SaveEquipment(domain.Equipment)
	GetEquipment(id string) (domain.Equipment, bool)
	ListEquipment() []domain.Equipment
	SaveClaim(domain.EquipmentClaim)
	GetClaim(id string) (domain.EquipmentClaim, bool)
	ListClaimsByShift(shiftID string) []domain.EquipmentClaim
	OutstandingBorrowedByOfficerAndType(officerID string, et domain.EquipmentType) []domain.EquipmentClaim
	ActiveClaimForEquipment(equipmentID string) (domain.EquipmentClaim, bool)
	QueuedClaimsForEquipment(equipmentID string) []domain.EquipmentClaim
	TryLockEquipment(claim domain.EquipmentClaim) (bool, error)
	PromoteNextQueued(equipmentID string, now time.Time) (domain.EquipmentClaim, bool)
	ReleaseEquipment(equipmentID string)
}

// EquipmentService handles equipment registration, claims, borrowing,
// returns, loss, and the cross-station approval rule.
type EquipmentService struct {
	store   EquipmentStore
	shifts  ShiftStore
	clock   domain.Clock
	weather domain.WeatherProvider
}

// NewEquipmentService constructs an EquipmentService.
func NewEquipmentService(s EquipmentStore, shifts ShiftStore, clock domain.Clock, weather domain.WeatherProvider) *EquipmentService {
	return &EquipmentService{store: s, shifts: shifts, clock: clock, weather: weather}
}

// RegisterEquipmentRequest is the input for registering a new asset.
type RegisterEquipmentRequest struct {
	Name          string
	Type          domain.EquipmentType
	HomeStationID string
}

// RegisterEquipment creates an available equipment record.
func (svc *EquipmentService) RegisterEquipment(req RegisterEquipmentRequest) (domain.Equipment, error) {
	if req.Name == "" || req.HomeStationID == "" {
		return domain.Equipment{}, fmt.Errorf("%w: name and home_station_id required", domain.ErrValidation)
	}
	switch req.Type {
	case domain.EquipmentTypeDrone, domain.EquipmentTypeInfraredCamera, domain.EquipmentTypeSatellitePhone:
	default:
		return domain.Equipment{}, fmt.Errorf("%w: invalid equipment type", domain.ErrValidation)
	}
	e := domain.Equipment{
		ID:            domain.NewID("eq"),
		Name:          req.Name,
		Type:          req.Type,
		HomeStationID: req.HomeStationID,
		Status:        domain.EquipmentStatusAvailable,
	}
	svc.store.SaveEquipment(e)
	return e, nil
}

// SubmitClaimRequest is the input for an officer claiming equipment.
type SubmitClaimRequest struct {
	ShiftID     string
	EquipmentID string
	OfficerID   string
}

// SubmitClaimResult reports the outcome of a claim attempt.
type SubmitClaimResult struct {
	Claim   domain.EquipmentClaim
	Won     bool // true when the claim now holds the lock
	Queued  bool // true when the claim entered the standby queue
	Message string
}

// SubmitClaim validates business rules then atomically attempts to lock
// the equipment. When two shifts contend for the same drone the winner is
// decided by shift priority then submission time; the loser is queued.
func (svc *EquipmentService) SubmitClaim(req SubmitClaimRequest) (SubmitClaimResult, error) {
	if req.ShiftID == "" || req.EquipmentID == "" || req.OfficerID == "" {
		return SubmitClaimResult{}, fmt.Errorf("%w: shift_id, equipment_id and officer_id required", domain.ErrValidation)
	}

	sh, ok := svc.shifts.GetShift(req.ShiftID)
	if !ok {
		return SubmitClaimResult{}, domain.ErrShiftNotFound
	}
	if sh.Status != domain.ShiftStatusApproved && sh.Status != domain.ShiftStatusActive {
		return SubmitClaimResult{}, domain.ErrShiftNotApproved
	}
	if sh.OfficerID != req.OfficerID {
		return SubmitClaimResult{}, fmt.Errorf("%w: shift does not belong to this officer", domain.ErrValidation)
	}

	equip, ok := svc.store.GetEquipment(req.EquipmentID)
	if !ok {
		return SubmitClaimResult{}, domain.ErrEquipmentNotFound
	}

	now := svc.clock.Now()

	// Rule 1: equipment must be locked at least 2 hours before departure.
	if sh.DepartureAt.Sub(now) < domain.LockLeadTime {
		return SubmitClaimResult{}, domain.ErrLockWindowViolation
	}

	// Rule 2: officer with outstanding borrowed same-type equipment may not
	// claim again.
	outstanding := svc.store.OutstandingBorrowedByOfficerAndType(req.OfficerID, equip.Type)
	if len(outstanding) > 0 {
		return SubmitClaimResult{}, domain.ErrOutstandingEquipment
	}

	// Rule 3: drones may not leave the depot in rain/snow or wind >= 6.
	if equip.Type == domain.EquipmentTypeDrone {
		w, err := svc.weather.Current(sh.StationID)
		if err != nil {
			return SubmitClaimResult{}, fmt.Errorf("weather lookup failed: %w", err)
		}
		if domain.DroneFlightProhibited(w) {
			return SubmitClaimResult{}, domain.ErrWeatherProhibited
		}
	}

	// Rule 5: cross-station borrowing requires dispatcher approval.
	crossStation := equip.HomeStationID != sh.StationID

	claim := domain.EquipmentClaim{
		ID:            domain.NewID("claim"),
		ShiftID:       req.ShiftID,
		EquipmentID:   req.EquipmentID,
		EquipmentType: equip.Type,
		OfficerID:     req.OfficerID,
		StationID:     sh.StationID,
		Priority:      sh.Priority,
		SubmittedAt:   now,
		CrossStation:  crossStation,
	}

	if crossStation {
		// Cross-station claims are queued pending dispatcher approval; the
		// equipment is not locked until approved.
		claim.Status = domain.ClaimStatusQueued
		svc.store.SaveClaim(claim)
		return SubmitClaimResult{
			Claim:   claim,
			Queued:  true,
			Message: "cross-station claim queued pending dispatcher approval",
		}, nil
	}

	won, err := svc.store.TryLockEquipment(claim)
	if err != nil {
		return SubmitClaimResult{}, err
	}
	claim, _ = svc.store.GetClaim(claim.ID)
	if won {
		lockedAt := now
		claim.LockedAt = &lockedAt
		svc.store.SaveClaim(claim)
		return SubmitClaimResult{Claim: claim, Won: true, Message: "equipment locked"}, nil
	}
	return SubmitClaimResult{Claim: claim, Queued: true, Message: "entered standby queue; will auto-promote when equipment is freed"}, nil
}

// ApproveCrossStationClaim lets a dispatcher approve a queued cross-station
// request. After approval the claim attempts to lock the equipment.
func (svc *EquipmentService) ApproveCrossStationClaim(claimID, dispatcherID string) (SubmitClaimResult, error) {
	claim, ok := svc.store.GetClaim(claimID)
	if !ok {
		return SubmitClaimResult{}, domain.ErrClaimNotFound
	}
	if !claim.CrossStation {
		return SubmitClaimResult{}, fmt.Errorf("%w: claim is not cross-station", domain.ErrValidation)
	}
	if claim.DispatcherApproved {
		return SubmitClaimResult{}, fmt.Errorf("%w: claim already approved", domain.ErrValidation)
	}
	claim.DispatcherApproved = true

	// Re-validate rules at approval time.
	sh, ok := svc.shifts.GetShift(claim.ShiftID)
	if !ok {
		return SubmitClaimResult{}, domain.ErrShiftNotFound
	}
	now := svc.clock.Now()
	if sh.DepartureAt.Sub(now) < domain.LockLeadTime {
		return SubmitClaimResult{}, domain.ErrLockWindowViolation
	}
	if claim.EquipmentType == domain.EquipmentTypeDrone {
		w, err := svc.weather.Current(sh.StationID)
		if err != nil {
			return SubmitClaimResult{}, err
		}
		if domain.DroneFlightProhibited(w) {
			return SubmitClaimResult{}, domain.ErrWeatherProhibited
		}
	}

	won, err := svc.store.TryLockEquipment(claim)
	if err != nil {
		return SubmitClaimResult{}, err
	}
	claim, _ = svc.store.GetClaim(claim.ID)
	if won {
		la := now
		claim.LockedAt = &la
		svc.store.SaveClaim(claim)
		return SubmitClaimResult{Claim: claim, Won: true, Message: "cross-station claim approved and locked"}, nil
	}
	return SubmitClaimResult{Claim: claim, Queued: true, Message: "approved but equipment busy; entered queue"}, nil
}

// BorrowEquipment records the keeper handing over equipment to the officer.
func (svc *EquipmentService) BorrowEquipment(claimID, keeperID string) error {
	claim, ok := svc.store.GetClaim(claimID)
	if !ok {
		return domain.ErrClaimNotFound
	}
	if err := claim.CanBorrow(); err != nil {
		return err
	}
	now := svc.clock.Now()
	claim.Status = domain.ClaimStatusBorrowed
	claim.BorrowedAt = &now
	svc.store.SaveClaim(claim)

	equip, ok := svc.store.GetEquipment(claim.EquipmentID)
	if ok {
		equip.Status = domain.EquipmentStatusBorrowed
		equip.HeldBy = claim.OfficerID
		svc.store.SaveEquipment(equip)
	}
	return nil
}

// ReturnEquipment records the keeper receiving equipment back. If loss is
// reported the equipment enters maintenance. The standby queue is then
// auto-promoted.
func (svc *EquipmentService) ReturnEquipment(claimID, keeperID string, lossNote string) error {
	claim, ok := svc.store.GetClaim(claimID)
	if !ok {
		return domain.ErrClaimNotFound
	}
	if err := claim.CanReturn(); err != nil {
		return err
	}
	now := svc.clock.Now()
	claim.Status = domain.ClaimStatusReturned
	claim.ReturnedAt = &now
	if lossNote != "" {
		claim.LossReported = true
		claim.LossNote = lossNote
	}
	svc.store.SaveClaim(claim)

	equip, ok := svc.store.GetEquipment(claim.EquipmentID)
	if !ok {
		return nil
	}
	if lossNote != "" {
		equip.Status = domain.EquipmentStatusMaintenance
	} else {
		equip.Status = domain.EquipmentStatusAvailable
	}
	equip.LockedByShiftID = ""
	equip.HeldBy = ""
	svc.store.SaveEquipment(equip)

	// Auto-promote the next queued claim for this equipment.
	if _, promoted := svc.store.PromoteNextQueued(equip.ID, now); promoted {
		// equipment re-locked by promotion
	}
	return nil
}

// CancelClaim cancels a claim and, if it held the lock, frees the equipment
// so the standby queue can advance.
func (svc *EquipmentService) CancelClaim(claimID string) error {
	claim, ok := svc.store.GetClaim(claimID)
	if !ok {
		return domain.ErrClaimNotFound
	}
	if claim.Status.IsTerminal() {
		return domain.ErrClaimAlreadyTerminal
	}
	now := svc.clock.Now()
	heldLock := claim.Status == domain.ClaimStatusLocked
	claim.Status = domain.ClaimStatusCancelled
	svc.store.SaveClaim(claim)

	if heldLock {
		svc.store.ReleaseEquipment(claim.EquipmentID)
		svc.store.PromoteNextQueued(claim.EquipmentID, now)
	}
	return nil
}

// GetClaim returns a claim by ID.
func (svc *EquipmentService) GetClaim(id string) (domain.EquipmentClaim, error) {
	c, ok := svc.store.GetClaim(id)
	if !ok {
		return domain.EquipmentClaim{}, domain.ErrClaimNotFound
	}
	return c, nil
}

// ListClaimsByShift returns all claims for a shift.
func (svc *EquipmentService) ListClaimsByShift(shiftID string) []domain.EquipmentClaim {
	return svc.store.ListClaimsByShift(shiftID)
}

// ListEquipment returns all equipment.
func (svc *EquipmentService) ListEquipment() []domain.Equipment {
	return svc.store.ListEquipment()
}

// QueuedClaimsForEquipment exposes the standby queue for inspection.
func (svc *EquipmentService) QueuedClaimsForEquipment(equipmentID string) []domain.EquipmentClaim {
	return svc.store.QueuedClaimsForEquipment(equipmentID)
}
