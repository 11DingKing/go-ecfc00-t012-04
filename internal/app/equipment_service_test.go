package app

import (
	"testing"
	"time"

	"patrol-platform/internal/domain"
	"patrol-platform/internal/store"
)

// testHarness wires a store and services with a controllable clock.
type testHarness struct {
	store   *store.MemoryStore
	clock   *mutableClock
	shift   *ShiftService
	equip   *EquipmentService
	alert   *AlertService
	patrol  *PatrolService
	archive *ArchiveService
	weather *domain.StaticWeatherProvider
}

type mutableClock struct {
	t time.Time
}

func (c *mutableClock) Now() time.Time          { return c.t }
func (c *mutableClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newHarness() *testHarness {
	c := &mutableClock{t: time.Date(2026, 3, 2, 6, 0, 0, 0, time.UTC)}
	st := store.New()
	wp := &domain.StaticWeatherProvider{Snapshot: domain.WeatherSnapshot{
		HasPrecipitation: false, WindForce: 3, ObservedAt: c.t,
	}}
	return &testHarness{
		store:   st,
		clock:   c,
		shift:   NewShiftService(st, c),
		equip:   NewEquipmentService(st, st, c, wp),
		alert:   NewAlertService(st, c, "chief-001"),
		patrol:  NewPatrolService(st, c),
		archive: NewArchiveService(st, c),
		weather: wp,
	}
}

// createApprovedShift builds an approved shift departing 4 hours from now.
func (h *testHarness) createApprovedShift(t *testing.T, officerID string) domain.Shift {
	t.Helper()
	shifts, err := h.shift.GenerateWeeklyShifts("hualong", "chief-001", h.clock.t,
		[]AssignmentSpec{{
			OfficerID:   officerID,
			RouteID:     "route-1",
			DepartureAt: h.clock.t.Add(4 * time.Hour),
			Priority:    domain.PriorityNormal,
		}})
	if err != nil {
		t.Fatalf("generate shifts: %v", err)
	}
	sh := shifts[0]
	if err := h.shift.SubmitShift(sh.ID); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if err := h.shift.ApproveShift(sh.ID, "chief-001"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	return sh
}

func TestClaimEquipmentSuccess(t *testing.T) {
	h := newHarness()
	sh := h.createApprovedShift(t, "officer-1")

	e, err := h.equip.RegisterEquipment(RegisterEquipmentRequest{
		Name: "Drone-A", Type: domain.EquipmentTypeDrone, HomeStationID: "hualong",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	result, err := h.equip.SubmitClaim(SubmitClaimRequest{
		ShiftID: sh.ID, EquipmentID: e.ID, OfficerID: "officer-1",
	})
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if !result.Won {
		t.Fatal("expected claim to win the lock")
	}
	if result.Claim.Status != domain.ClaimStatusLocked {
		t.Fatalf("expected locked, got %s", result.Claim.Status)
	}
}

func TestClaimEquipmentLockWindowViolation(t *testing.T) {
	h := newHarness()
	// Create a shift departing only 1 hour from now (less than 2h lead time).
	shifts, err := h.shift.GenerateWeeklyShifts("hualong", "chief-001", h.clock.t,
		[]AssignmentSpec{{
			OfficerID: "officer-1", RouteID: "route-1",
			DepartureAt: h.clock.t.Add(1 * time.Hour),
			Priority:    domain.PriorityNormal,
		}})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	sh := shifts[0]
	_ = h.shift.SubmitShift(sh.ID)
	_ = h.shift.ApproveShift(sh.ID, "chief-001")

	e, _ := h.equip.RegisterEquipment(RegisterEquipmentRequest{
		Name: "Phone-A", Type: domain.EquipmentTypeSatellitePhone, HomeStationID: "hualong",
	})

	_, err = h.equip.SubmitClaim(SubmitClaimRequest{
		ShiftID: sh.ID, EquipmentID: e.ID, OfficerID: "officer-1",
	})
	if err != domain.ErrLockWindowViolation {
		t.Fatalf("expected ErrLockWindowViolation, got %v", err)
	}
}

func TestClaimEquipmentOutstandingNotReturned(t *testing.T) {
	h := newHarness()
	sh := h.createApprovedShift(t, "officer-1")

	e, _ := h.equip.RegisterEquipment(RegisterEquipmentRequest{
		Name: "Phone-A", Type: domain.EquipmentTypeSatellitePhone, HomeStationID: "hualong",
	})
	e2, _ := h.equip.RegisterEquipment(RegisterEquipmentRequest{
		Name: "Phone-B", Type: domain.EquipmentTypeSatellitePhone, HomeStationID: "hualong",
	})

	// Claim and borrow the first phone.
	result, _ := h.equip.SubmitClaim(SubmitClaimRequest{
		ShiftID: sh.ID, EquipmentID: e.ID, OfficerID: "officer-1",
	})
	if err := h.equip.BorrowEquipment(result.Claim.ID, "keeper-1"); err != nil {
		t.Fatalf("borrow: %v", err)
	}

	// Attempting to claim a second phone of the same type should fail.
	_, err := h.equip.SubmitClaim(SubmitClaimRequest{
		ShiftID: sh.ID, EquipmentID: e2.ID, OfficerID: "officer-1",
	})
	if err != domain.ErrOutstandingEquipment {
		t.Fatalf("expected ErrOutstandingEquipment, got %v", err)
	}
}

func TestClaimDroneWeatherProhibited(t *testing.T) {
	h := newHarness()
	sh := h.createApprovedShift(t, "officer-1")

	e, _ := h.equip.RegisterEquipment(RegisterEquipmentRequest{
		Name: "Drone-A", Type: domain.EquipmentTypeDrone, HomeStationID: "hualong",
	})

	// Simulate storm conditions.
	h.weather.Snapshot = domain.WeatherSnapshot{
		HasPrecipitation: true, WindForce: 8, ObservedAt: h.clock.t,
	}

	_, err := h.equip.SubmitClaim(SubmitClaimRequest{
		ShiftID: sh.ID, EquipmentID: e.ID, OfficerID: "officer-1",
	})
	if err != domain.ErrWeatherProhibited {
		t.Fatalf("expected ErrWeatherProhibited, got %v", err)
	}
}

func TestConcurrentDroneClaimPriorityAndQueue(t *testing.T) {
	h := newHarness()

	// Two shifts: one high priority, one normal priority.
	shiftsHi, _ := h.shift.GenerateWeeklyShifts("hualong", "chief-001", h.clock.t,
		[]AssignmentSpec{{
			OfficerID: "officer-hi", RouteID: "route-1",
			DepartureAt: h.clock.t.Add(4 * time.Hour), Priority: domain.PriorityHigh,
		}})
	shHi := shiftsHi[0]
	_ = h.shift.SubmitShift(shHi.ID)
	_ = h.shift.ApproveShift(shHi.ID, "chief-001")

	shiftsNorm, _ := h.shift.GenerateWeeklyShifts("hualong", "chief-001", h.clock.t,
		[]AssignmentSpec{{
			OfficerID: "officer-norm", RouteID: "route-1",
			DepartureAt: h.clock.t.Add(4 * time.Hour), Priority: domain.PriorityNormal,
		}})
	shNorm := shiftsNorm[0]
	_ = h.shift.SubmitShift(shNorm.ID)
	_ = h.shift.ApproveShift(shNorm.ID, "chief-001")

	e, _ := h.equip.RegisterEquipment(RegisterEquipmentRequest{
		Name: "Drone-X", Type: domain.EquipmentTypeDrone, HomeStationID: "hualong",
	})

	// Normal priority claims first, then high priority.
	resNorm, err := h.equip.SubmitClaim(SubmitClaimRequest{
		ShiftID: shNorm.ID, EquipmentID: e.ID, OfficerID: "officer-norm",
	})
	if err != nil {
		t.Fatalf("normal claim: %v", err)
	}
	if !resNorm.Won {
		t.Fatal("normal claim should win when equipment is free")
	}

	resHi, err := h.equip.SubmitClaim(SubmitClaimRequest{
		ShiftID: shHi.ID, EquipmentID: e.ID, OfficerID: "officer-hi",
	})
	if err != nil {
		t.Fatalf("high claim: %v", err)
	}
	// High priority should preempt the normal claim.
	if !resHi.Won {
		t.Fatal("high priority claim should preempt the normal claim")
	}

	// The normal claim should now be queued.
	normClaim, _ := h.equip.GetClaim(resNorm.Claim.ID)
	if normClaim.Status != domain.ClaimStatusQueued {
		t.Fatalf("expected normal claim queued after preemption, got %s", normClaim.Status)
	}

	// Cancel the high-priority claim; the normal claim should auto-promote.
	if err := h.equip.CancelClaim(resHi.Claim.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	normClaim, _ = h.equip.GetClaim(resNorm.Claim.ID)
	if normClaim.Status != domain.ClaimStatusLocked {
		t.Fatalf("expected normal claim auto-promoted to locked, got %s", normClaim.Status)
	}
}

func TestCrossStationRequiresApproval(t *testing.T) {
	h := newHarness()
	sh := h.createApprovedShift(t, "officer-1")

	// Equipment from a different station.
	e, _ := h.equip.RegisterEquipment(RegisterEquipmentRequest{
		Name: "Phone-Other", Type: domain.EquipmentTypeSatellitePhone, HomeStationID: "other-station",
	})

	result, err := h.equip.SubmitClaim(SubmitClaimRequest{
		ShiftID: sh.ID, EquipmentID: e.ID, OfficerID: "officer-1",
	})
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if !result.Queued || !result.Claim.CrossStation {
		t.Fatal("cross-station claim should be queued pending dispatcher approval")
	}

	// Dispatcher approves — equipment should lock.
	approved, err := h.equip.ApproveCrossStationClaim(result.Claim.ID, "dispatcher-1")
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if !approved.Won {
		t.Fatal("approved cross-station claim should lock the equipment")
	}
}

func TestBorrowReturnLifecycle(t *testing.T) {
	h := newHarness()
	sh := h.createApprovedShift(t, "officer-1")
	e, _ := h.equip.RegisterEquipment(RegisterEquipmentRequest{
		Name: "Cam-A", Type: domain.EquipmentTypeInfraredCamera, HomeStationID: "hualong",
	})

	result, _ := h.equip.SubmitClaim(SubmitClaimRequest{
		ShiftID: sh.ID, EquipmentID: e.ID, OfficerID: "officer-1",
	})

	if err := h.equip.BorrowEquipment(result.Claim.ID, "keeper-1"); err != nil {
		t.Fatalf("borrow: %v", err)
	}
	claim, _ := h.equip.GetClaim(result.Claim.ID)
	if claim.Status != domain.ClaimStatusBorrowed {
		t.Fatalf("expected borrowed, got %s", claim.Status)
	}

	if err := h.equip.ReturnEquipment(result.Claim.ID, "keeper-1", ""); err != nil {
		t.Fatalf("return: %v", err)
	}
	claim, _ = h.equip.GetClaim(result.Claim.ID)
	if claim.Status != domain.ClaimStatusReturned {
		t.Fatalf("expected returned, got %s", claim.Status)
	}

	eq, _ := h.store.GetEquipment(e.ID)
	if eq.Status != domain.EquipmentStatusAvailable {
		t.Fatalf("expected equipment available after return, got %s", eq.Status)
	}
}

func TestReturnWithLossEntersMaintenance(t *testing.T) {
	h := newHarness()
	sh := h.createApprovedShift(t, "officer-1")
	e, _ := h.equip.RegisterEquipment(RegisterEquipmentRequest{
		Name: "Cam-A", Type: domain.EquipmentTypeInfraredCamera, HomeStationID: "hualong",
	})
	result, _ := h.equip.SubmitClaim(SubmitClaimRequest{
		ShiftID: sh.ID, EquipmentID: e.ID, OfficerID: "officer-1",
	})
	_ = h.equip.BorrowEquipment(result.Claim.ID, "keeper-1")

	if err := h.equip.ReturnEquipment(result.Claim.ID, "keeper-1", "lens cracked"); err != nil {
		t.Fatalf("return: %v", err)
	}
	eq, _ := h.store.GetEquipment(e.ID)
	if eq.Status != domain.EquipmentStatusMaintenance {
		t.Fatalf("expected maintenance after loss, got %s", eq.Status)
	}
	claim, _ := h.equip.GetClaim(result.Claim.ID)
	if !claim.LossReported || claim.LossNote != "lens cracked" {
		t.Fatal("loss should be recorded on the claim")
	}
}

func TestCancelTerminalClaimFails(t *testing.T) {
	h := newHarness()
	sh := h.createApprovedShift(t, "officer-1")
	e, _ := h.equip.RegisterEquipment(RegisterEquipmentRequest{
		Name: "Phone-A", Type: domain.EquipmentTypeSatellitePhone, HomeStationID: "hualong",
	})
	result, _ := h.equip.SubmitClaim(SubmitClaimRequest{
		ShiftID: sh.ID, EquipmentID: e.ID, OfficerID: "officer-1",
	})
	_ = h.equip.BorrowEquipment(result.Claim.ID, "keeper-1")
	_ = h.equip.ReturnEquipment(result.Claim.ID, "keeper-1", "")

	err := h.equip.CancelClaim(result.Claim.ID)
	if err != domain.ErrClaimAlreadyTerminal {
		t.Fatalf("expected ErrClaimAlreadyTerminal, got %v", err)
	}
}
