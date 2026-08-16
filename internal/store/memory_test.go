package store

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"patrol-platform/internal/domain"
)

func TestStoreConcurrentDroneAllocation(t *testing.T) {
	s := New()
	equip := domain.Equipment{
		ID:            "drone-1",
		Name:          "Mavic 3",
		Type:          domain.EquipmentTypeDrone,
		HomeStationID: "hualong",
		Status:        domain.EquipmentStatusAvailable,
	}
	s.SaveEquipment(equip)

	now := time.Date(2026, 3, 1, 8, 0, 0, 0, time.UTC)
	const goroutines = 50
	var wg sync.WaitGroup
	claims := make([]domain.EquipmentClaim, goroutines)

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		i := i
		go func() {
			defer wg.Done()
			claim := domain.EquipmentClaim{
				ID:          fmt.Sprintf("claim-%d", i),
				ShiftID:     fmt.Sprintf("shift-%d", i),
				EquipmentID: "drone-1",
				Priority:    domain.PriorityNormal,
				SubmittedAt: now.Add(time.Duration(i) * time.Millisecond),
			}
			claims[i] = claim
			_, err := s.TryLockEquipment(claim)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()

	// Verify final state: exactly one claim holds the lock, rest are queued.
	lockedClaims := 0
	queuedClaims := 0
	winnerID := ""
	for _, c := range claims {
		stored, ok := s.GetClaim(c.ID)
		if !ok {
			continue
		}
		switch stored.Status {
		case domain.ClaimStatusLocked:
			lockedClaims++
			winnerID = stored.ID
		case domain.ClaimStatusQueued:
			queuedClaims++
		}
	}
	if lockedClaims != 1 {
		t.Fatalf("expected exactly 1 locked claim, got %d", lockedClaims)
	}
	if queuedClaims != goroutines-1 {
		t.Fatalf("expected %d queued claims, got %d", goroutines-1, queuedClaims)
	}

	// The winner should be the one with the earliest submission time (claim-0).
	if winnerID != "claim-0" {
		t.Fatalf("expected claim-0 (earliest) to hold lock, got %s", winnerID)
	}

	// The equipment should be in locked status.
	eq, _ := s.GetEquipment("drone-1")
	if eq.Status != domain.EquipmentStatusLocked {
		t.Fatalf("expected equipment locked, got %s", eq.Status)
	}
}

func TestStorePromoteNextQueued(t *testing.T) {
	s := New()
	s.SaveEquipment(domain.Equipment{
		ID:            "cam-1",
		Type:          domain.EquipmentTypeInfraredCamera,
		HomeStationID: "hualong",
		Status:        domain.EquipmentStatusAvailable,
	})

	now := time.Now()
	// claimA is high priority and submitted first — it should lock.
	claimA := domain.EquipmentClaim{
		ID: "claim-a", ShiftID: "s-a", EquipmentID: "cam-1",
		Priority: domain.PriorityHigh, SubmittedAt: now.Add(-2 * time.Minute),
	}
	// claimB is lower priority — it should be queued.
	claimB := domain.EquipmentClaim{
		ID: "claim-b", ShiftID: "s-b", EquipmentID: "cam-1",
		Priority: domain.PriorityLow, SubmittedAt: now.Add(-1 * time.Minute),
	}

	wonA, _ := s.TryLockEquipment(claimA)
	if !wonA {
		t.Fatal("claimA should win initially (equipment available)")
	}
	wonB, _ := s.TryLockEquipment(claimB)
	if wonB {
		t.Fatal("claimB should be queued, not win, because it has lower priority")
	}
	queuedClaims := s.QueuedClaimsForEquipment("cam-1")
	if len(queuedClaims) != 1 {
		t.Fatalf("expected 1 queued claim, got %d", len(queuedClaims))
	}
	if queuedClaims[0].ID != "claim-b" {
		t.Fatalf("expected claim-b in queue, got %s", queuedClaims[0].ID)
	}

	// Release equipment then promote.
	s.ReleaseEquipment("cam-1")
	eq, _ := s.GetEquipment("cam-1")
	if eq.Status != domain.EquipmentStatusAvailable {
		t.Fatalf("expected equipment available after release, got %s", eq.Status)
	}

	promoted, ok := s.PromoteNextQueued("cam-1", now)
	if !ok {
		t.Fatal("expected a promotion")
	}
	if promoted.ID != "claim-b" {
		t.Fatalf("expected claim-b to be promoted, got %s", promoted.ID)
	}
	eq, _ = s.GetEquipment("cam-1")
	if eq.Status != domain.EquipmentStatusLocked {
		t.Fatalf("expected equipment locked after promotion, got %s", eq.Status)
	}
}

func TestStoreIdempotencyKeys(t *testing.T) {
	s := New()
	ci := domain.CheckIn{
		ID:             "ci-1",
		ShiftID:        "shift-1",
		IdempotencyKey: "key-abc",
		Timestamp:      time.Now(),
	}
	s.SaveCheckIn(ci)

	if _, ok := s.CheckInByIdempotencyKey("key-abc"); !ok {
		t.Fatal("should find check-in by idempotency key")
	}
	if _, ok := s.CheckInByIdempotencyKey("nonexistent"); ok {
		t.Fatal("should not find nonexistent key")
	}

	a := domain.Alert{
		ID:             "alert-1",
		ShiftID:        "shift-1",
		IdempotencyKey: "alert-key",
		ReportedAt:     time.Now(),
	}
	s.SaveAlert(a)
	if _, ok := s.AlertByidempotencyKey("alert-key"); !ok {
		t.Fatal("should find alert by idempotency key")
	}
}
