package domain

import (
	"testing"
	"time"
)

func TestClaimBeatsByPriority(t *testing.T) {
	earlier := time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)

	highEarly := EquipmentClaim{Priority: PriorityHigh, SubmittedAt: earlier}
	normalEarly := EquipmentClaim{Priority: PriorityNormal, SubmittedAt: earlier}

	if !highEarly.Beats(normalEarly) {
		t.Fatal("high priority should beat normal priority regardless of time")
	}
	if normalEarly.Beats(highEarly) {
		t.Fatal("normal priority should not beat high priority")
	}
}

func TestClaimBeatsBySubmissionTime(t *testing.T) {
	earlier := time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)
	later := earlier.Add(10 * time.Minute)

	first := EquipmentClaim{Priority: PriorityNormal, SubmittedAt: earlier}
	second := EquipmentClaim{Priority: PriorityNormal, SubmittedAt: later}

	if !first.Beats(second) {
		t.Fatal("earlier submission should win when priorities are equal")
	}
	if second.Beats(first) {
		t.Fatal("later submission should not win when priorities are equal")
	}
}

func TestAlertNeedsEscalation(t *testing.T) {
	reported := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	a := Alert{Status: AlertStatusReported, ReportedAt: reported}

	if a.NeedsEscalation(reported.Add(4 * time.Minute)) {
		t.Fatal("alert within 5-minute window should not need escalation")
	}
	if !a.NeedsEscalation(reported.Add(5 * time.Minute)) {
		t.Fatal("alert at 5-minute deadline should need escalation")
	}
	if !a.NeedsEscalation(reported.Add(10 * time.Minute)) {
		t.Fatal("alert past deadline should need escalation")
	}

	a.Status = AlertStatusSigned
	if a.NeedsEscalation(reported.Add(10 * time.Minute)) {
		t.Fatal("signed alert should never need escalation")
	}
}

func TestShiftTransitionValidation(t *testing.T) {
	sh := Shift{Status: ShiftStatusDraft}
	if err := sh.CanTransitionTo(ShiftStatusSubmitted); err != nil {
		t.Fatalf("draft -> submitted should be valid: %v", err)
	}
	if err := sh.CanTransitionTo(ShiftStatusApproved); err == nil {
		t.Fatal("draft -> approved should be invalid")
	}

	sh.Status = ShiftStatusCompleted
	if err := sh.CanTransitionTo(ShiftStatusActive); err == nil {
		t.Fatal("completed -> active should be invalid")
	}
}

func TestDroneFlightProhibitedRule(t *testing.T) {
	cases := []struct {
		name     string
		snap     WeatherSnapshot
		expected bool
	}{
		{"clear calm", WeatherSnapshot{HasPrecipitation: false, WindForce: 3}, false},
		{"rain", WeatherSnapshot{HasPrecipitation: true, WindForce: 2}, true},
		{"wind force 6", WeatherSnapshot{HasPrecipitation: false, WindForce: 6}, true},
		{"wind force 7", WeatherSnapshot{HasPrecipitation: false, WindForce: 7}, true},
		{"wind force 5", WeatherSnapshot{HasPrecipitation: false, WindForce: 5}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DroneFlightProhibited(tc.snap); got != tc.expected {
				t.Fatalf("expected %v, got %v", tc.expected, got)
			}
		})
	}
}

func TestClaimStatusTerminal(t *testing.T) {
	if !ClaimStatusReturned.IsTerminal() {
		t.Fatal("returned should be terminal")
	}
	if !ClaimStatusCancelled.IsTerminal() {
		t.Fatal("cancelled should be terminal")
	}
	if ClaimStatusLocked.IsTerminal() {
		t.Fatal("locked should not be terminal")
	}
	if ClaimStatusBorrowed.IsTerminal() {
		t.Fatal("borrowed should not be terminal")
	}
	if ClaimStatusQueued.IsTerminal() {
		t.Fatal("queued should not be terminal")
	}
}
