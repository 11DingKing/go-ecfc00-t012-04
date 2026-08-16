package app

import (
	"testing"
	"time"

	"patrol-platform/internal/domain"
)

func TestShiftCreateSubmitApproveActivate(t *testing.T) {
	h := newHarness()

	shifts, err := h.shift.GenerateWeeklyShifts("hualong", "chief-001", h.clock.t,
		[]AssignmentSpec{{
			OfficerID: "officer-1", RouteID: "route-1",
			DepartureAt: h.clock.t.Add(4 * time.Hour),
			Priority:    domain.PriorityNormal,
		}})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	sh := shifts[0]
	if sh.Status != domain.ShiftStatusDraft {
		t.Fatalf("expected draft, got %s", sh.Status)
	}

	if err := h.shift.SubmitShift(sh.ID); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if err := h.shift.ApproveShift(sh.ID, "chief-001"); err != nil {
		t.Fatalf("approve: %v", err)
	}

	sh, _ = h.shift.GetShift(sh.ID)
	if sh.Status != domain.ShiftStatusApproved {
		t.Fatalf("expected approved, got %s", sh.Status)
	}

	// Advance past departure time and activate via scheduler.
	h.clock.advance(5 * time.Hour)
	if err := h.shift.ActivateShift(sh.ID); err != nil {
		t.Fatalf("activate: %v", err)
	}
	sh, _ = h.shift.GetShift(sh.ID)
	if sh.Status != domain.ShiftStatusActive {
		t.Fatalf("expected active, got %s", sh.Status)
	}

	if err := h.shift.CompleteShift(sh.ID); err != nil {
		t.Fatalf("complete: %v", err)
	}
	sh, _ = h.shift.GetShift(sh.ID)
	if sh.Status != domain.ShiftStatusCompleted {
		t.Fatalf("expected completed, got %s", sh.Status)
	}
}

func TestShiftGenerateValidatesInput(t *testing.T) {
	h := newHarness()

	// Empty assignments.
	_, err := h.shift.GenerateWeeklyShifts("hualong", "chief-001", h.clock.t, nil)
	if err == nil {
		t.Fatal("expected error for empty assignments")
	}

	// Past departure time.
	_, err = h.shift.GenerateWeeklyShifts("hualong", "chief-001", h.clock.t,
		[]AssignmentSpec{{
			OfficerID: "o1", RouteID: "r1",
			DepartureAt: h.clock.t.Add(-1 * time.Hour),
		}})
	if err == nil {
		t.Fatal("expected error for past departure")
	}

	// Missing station.
	_, err = h.shift.GenerateWeeklyShifts("", "chief-001", h.clock.t,
		[]AssignmentSpec{{
			OfficerID: "o1", RouteID: "r1",
			DepartureAt: h.clock.t.Add(4 * time.Hour),
		}})
	if err == nil {
		t.Fatal("expected error for missing station")
	}
}

func TestShiftActivateDueShifts(t *testing.T) {
	h := newHarness()
	shifts, _ := h.shift.GenerateWeeklyShifts("hualong", "chief-001", h.clock.t,
		[]AssignmentSpec{{
			OfficerID: "o1", RouteID: "r1",
			DepartureAt: h.clock.t.Add(2 * time.Hour),
		}})
	sh := shifts[0]
	_ = h.shift.SubmitShift(sh.ID)
	_ = h.shift.ApproveShift(sh.ID, "chief-001")

	// Not yet due.
	if n := h.shift.ActivateDueShifts(); n != 0 {
		t.Fatalf("expected 0 activations before departure, got %d", n)
	}

	// Advance past departure.
	h.clock.advance(3 * time.Hour)
	if n := h.shift.ActivateDueShifts(); n != 1 {
		t.Fatalf("expected 1 activation, got %d", n)
	}
	sh, _ = h.shift.GetShift(sh.ID)
	if sh.Status != domain.ShiftStatusActive {
		t.Fatalf("expected active, got %s", sh.Status)
	}
}
