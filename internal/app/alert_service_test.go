package app

import (
	"testing"
	"time"

	"patrol-platform/internal/domain"
)

func TestAlertSignAndDispatchFlow(t *testing.T) {
	h := newHarness()

	a, _, err := h.alert.ReportAlert(ReportAlertRequest{
		ShiftID: "shift-1", OfficerID: "officer-1",
		Type: domain.AlertTypeFire, Description: "smoke near ridge",
		IdempotencyKey: "alert-key-1",
	})
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if a.Status != domain.AlertStatusReported {
		t.Fatalf("expected reported, got %s", a.Status)
	}

	if err := h.alert.SignAlert(a.ID, "dispatcher-1"); err != nil {
		t.Fatalf("sign: %v", err)
	}
	if err := h.alert.DispatchAlert(a.ID, "responder-1"); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if err := h.alert.StartProgress(a.ID); err != nil {
		t.Fatalf("progress: %v", err)
	}
	if err := h.alert.ResolveAlert(a.ID); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if err := h.alert.CloseAlert(a.ID); err != nil {
		t.Fatalf("close: %v", err)
	}
	a, _ = h.alert.GetAlert(a.ID)
	if a.Status != domain.AlertStatusClosed {
		t.Fatalf("expected closed, got %s", a.Status)
	}
}

func TestAlertInvalidTransition(t *testing.T) {
	h := newHarness()
	a, _, _ := h.alert.ReportAlert(ReportAlertRequest{
		ShiftID: "shift-1", OfficerID: "officer-1",
		Type: domain.AlertTypePoaching, IdempotencyKey: "key-x",
	})
	// Cannot dispatch directly from reported (must sign first).
	if err := h.alert.DispatchAlert(a.ID, "r-1"); err == nil {
		t.Fatal("expected error dispatching unsigned alert")
	}
}

func TestAlertAutoEscalation(t *testing.T) {
	h := newHarness()
	reportedAt := h.clock.t
	a, _, _ := h.alert.ReportAlert(ReportAlertRequest{
		ShiftID: "shift-1", OfficerID: "officer-1",
		Type: domain.AlertTypeFire, ReportedAt: reportedAt,
		IdempotencyKey: "esc-key",
	})

	// Within 5 minutes: no escalation.
	n := h.alert.EscalateOverdue()
	if n != 0 {
		t.Fatalf("expected 0 escalations, got %d", n)
	}

	// Advance past the 5-minute deadline.
	h.clock.advance(6 * time.Minute)
	n = h.alert.EscalateOverdue()
	if n != 1 {
		t.Fatalf("expected 1 escalation, got %d", n)
	}

	a, _ = h.alert.GetAlert(a.ID)
	if a.Status != domain.AlertStatusEscalated {
		t.Fatalf("expected escalated, got %s", a.Status)
	}
	if a.EscalatedTo != "chief-001" {
		t.Fatalf("expected escalation to chief-001, got %s", a.EscalatedTo)
	}

	// Running again should not re-escalate (already escalated).
	n = h.alert.EscalateOverdue()
	if n != 0 {
		t.Fatalf("expected 0 re-escalations, got %d", n)
	}
}

func TestAlertOfflineBatchIdempotencyAndOrdering(t *testing.T) {
	h := newHarness()
	base := h.clock.t

	reqs := []ReportAlertRequest{
		{ShiftID: "s1", OfficerID: "o1", Type: domain.AlertTypeOther, ReportedAt: base.Add(3 * time.Minute), IdempotencyKey: "k3", Offline: true},
		{ShiftID: "s1", OfficerID: "o1", Type: domain.AlertTypeFire, ReportedAt: base.Add(1 * time.Minute), IdempotencyKey: "k1", Offline: true},
		{ShiftID: "s1", OfficerID: "o1", Type: domain.AlertTypePoaching, ReportedAt: base.Add(2 * time.Minute), IdempotencyKey: "k2", Offline: true},
	}

	results, err := h.alert.ReportAlertBatch(reqs)
	if err != nil {
		t.Fatalf("batch: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// Results should be ordered by original timestamp (k1, k2, k3).
	if results[0].IdempotencyKey != "k1" || results[1].IdempotencyKey != "k2" || results[2].IdempotencyKey != "k3" {
		t.Fatal("batch results should be ordered by original timestamp")
	}

	// Replay the same batch — all should be deduplicated, no new records.
	results2, err := h.alert.ReportAlertBatch(reqs)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if len(results2) != 3 {
		t.Fatalf("expected 3 results on replay, got %d", len(results2))
	}
	allAlerts := h.alert.ListAlerts()
	if len(allAlerts) != 3 {
		t.Fatalf("expected 3 unique alerts after replay, got %d", len(allAlerts))
	}
}

func TestAlertSingleReportIdempotency(t *testing.T) {
	h := newHarness()
	req := ReportAlertRequest{
		ShiftID: "s1", OfficerID: "o1", Type: domain.AlertTypeFire,
		IdempotencyKey: "single-key", Description: "first",
	}
	a1, dup1, err := h.alert.ReportAlert(req)
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if dup1 {
		t.Fatal("first report should not be a duplicate")
	}

	// Same key, different description — should return original, not create new.
	a2, dup2, err := h.alert.ReportAlert(ReportAlertRequest{
		ShiftID: "s1", OfficerID: "o1", Type: domain.AlertTypeFire,
		IdempotencyKey: "single-key", Description: "second",
	})
	if err != nil {
		t.Fatalf("re-report: %v", err)
	}
	if !dup2 {
		t.Fatal("second report with same key should be flagged duplicate")
	}
	if a2.Description != "first" {
		t.Fatal("duplicate should return the original record")
	}
	if a1.ID != a2.ID {
		t.Fatal("duplicate should return the same record ID")
	}
}
