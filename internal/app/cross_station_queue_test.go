package app

import (
	"testing"
	"time"

	"patrol-platform/internal/domain"
)

func (h *testHarness) approvedShiftForStation(t *testing.T, stationID, officerID string, priority domain.ShiftPriority) domain.Shift {
	t.Helper()
	shifts, err := h.shift.GenerateWeeklyShifts(stationID, "chief-001", h.clock.t, []AssignmentSpec{{
		OfficerID:   officerID,
		RouteID:     "route-1",
		DepartureAt: h.clock.t.Add(4 * time.Hour),
		Priority:    priority,
	}})
	if err != nil {
		t.Fatalf("generate shift for %s: %v", officerID, err)
	}
	sh := shifts[0]
	if err := h.shift.SubmitShift(sh.ID); err != nil {
		t.Fatalf("submit %s: %v", sh.ID, err)
	}
	if err := h.shift.ApproveShift(sh.ID, "chief-001"); err != nil {
		t.Fatalf("approve %s: %v", sh.ID, err)
	}
	return sh
}

// TestCrossStationClaimWaitsForApprovalWhenEquipmentIsFreed covers the standby
// queue when a cross-station request is waiting in it: a cross-station request
// only becomes entitled to the equipment once a dispatcher has approved it, so
// freeing the equipment must leave an unapproved request waiting.
func TestCrossStationClaimWaitsForApprovalWhenEquipmentIsFreed(t *testing.T) {
	h := newHarness()

	// A drone that belongs to a neighbouring station.
	drone, err := h.equip.RegisterEquipment(RegisterEquipmentRequest{
		Name: "Drone-Neighbour", Type: domain.EquipmentTypeDrone, HomeStationID: "other-station",
	})
	if err != nil {
		t.Fatalf("register equipment: %v", err)
	}

	// A shift at the drone's own station takes it normally.
	local := h.approvedShiftForStation(t, "other-station", "officer-local", domain.PriorityNormal)
	resLocal, err := h.equip.SubmitClaim(SubmitClaimRequest{
		ShiftID: local.ID, EquipmentID: drone.ID, OfficerID: "officer-local",
	})
	if err != nil {
		t.Fatalf("local claim: %v", err)
	}
	if !resLocal.Won {
		t.Fatalf("local claim should hold the lock on a free drone")
	}

	// A shift at our station asks for the same drone: this is a cross-station
	// request and has to wait for a dispatcher.
	remote := h.approvedShiftForStation(t, "hualong", "officer-remote", domain.PriorityHigh)
	resRemote, err := h.equip.SubmitClaim(SubmitClaimRequest{
		ShiftID: remote.ID, EquipmentID: drone.ID, OfficerID: "officer-remote",
	})
	if err != nil {
		t.Fatalf("cross-station claim: %v", err)
	}
	if !resRemote.Claim.CrossStation || !resRemote.Queued {
		t.Fatalf("expected a queued cross-station claim, got cross_station=%v queued=%v",
			resRemote.Claim.CrossStation, resRemote.Queued)
	}
	if resRemote.Claim.DispatcherApproved {
		t.Fatalf("a freshly submitted cross-station claim must not be approved yet")
	}

	// The local shift releases the drone.
	if err := h.equip.CancelClaim(resLocal.Claim.ID); err != nil {
		t.Fatalf("cancel local claim: %v", err)
	}

	remoteClaim, err := h.equip.GetClaim(resRemote.Claim.ID)
	if err != nil {
		t.Fatalf("get cross-station claim: %v", err)
	}
	if remoteClaim.DispatcherApproved {
		t.Error("the cross-station claim became approved without a dispatcher")
	}
	if remoteClaim.Status != domain.ClaimStatusQueued {
		t.Errorf("cross-station claim status = %q, want %q", remoteClaim.Status, domain.ClaimStatusQueued)
	}
	eq, ok := h.store.GetEquipment(drone.ID)
	if !ok {
		t.Fatalf("equipment %s not found", drone.ID)
	}
	if eq.Status != domain.EquipmentStatusAvailable {
		t.Errorf("drone status = %q, want %q", eq.Status, domain.EquipmentStatusAvailable)
	}
	if eq.LockedByShiftID != "" {
		t.Errorf("drone locked by shift %q, want it not locked", eq.LockedByShiftID)
	}
	if err := h.equip.BorrowEquipment(resRemote.Claim.ID, "keeper-1"); err == nil {
		t.Error("the drone must not be handed over on a cross-station claim that no dispatcher approved")
	}

	// Once a dispatcher approves, the request takes the drone normally.
	approved, err := h.equip.ApproveCrossStationClaim(resRemote.Claim.ID, "dispatcher-1")
	if err != nil {
		t.Fatalf("dispatcher approval: %v", err)
	}
	if !approved.Won {
		t.Error("an approved cross-station claim should lock the free drone")
	}
}
