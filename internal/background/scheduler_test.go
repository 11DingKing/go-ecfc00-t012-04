package background

import (
	"context"
	"testing"
	"time"

	"patrol-platform/internal/app"
	"patrol-platform/internal/domain"
	"patrol-platform/internal/store"
)

// fakeEscalation is a minimal EscalationRunner that counts calls.
type fakeEscalation struct {
	calls int
}

func (f *fakeEscalation) EscalateOverdue() int {
	f.calls++
	return 0
}

type fakeActivation struct {
	calls int
}

func (f *fakeActivation) ActivateDueShifts() int {
	f.calls++
	return 0
}

func TestSchedulerStartStop(t *testing.T) {
	esc := &fakeEscalation{}
	act := &fakeActivation{}
	s := NewScheduler(esc, act, 10*time.Millisecond, nil)

	s.Start(context.Background())
	time.Sleep(55 * time.Millisecond)
	s.Stop()

	if esc.calls == 0 {
		t.Fatal("escalation runner should have been called at least once")
	}
	if act.calls == 0 {
		t.Fatal("activation runner should have been called at least once")
	}
}

func TestSchedulerTickOnce(t *testing.T) {
	esc := &fakeEscalation{}
	act := &fakeActivation{}
	s := NewScheduler(esc, act, time.Hour, nil)

	s.TickOnce()
	if esc.calls != 1 || act.calls != 1 {
		t.Fatal("TickOnce should call each runner exactly once")
	}
}

func TestSchedulerDoubleStartIsNoop(t *testing.T) {
	esc := &fakeEscalation{}
	act := &fakeActivation{}
	s := NewScheduler(esc, act, time.Hour, nil)

	s.Start(context.Background())
	s.Start(context.Background()) // should be a no-op
	s.Stop()
}

// Integration: scheduler escalates a real overdue alert via the app service.
func TestSchedulerEscalatesRealAlert(t *testing.T) {
	st := store.New()
	clock := domain.FixedClock{T: time.Date(2026, 3, 2, 8, 0, 0, 0, time.UTC)}
	alertSvc := app.NewAlertService(st, clock, "chief-001")

	_, _, err := alertSvc.ReportAlert(app.ReportAlertRequest{
		ShiftID:        "s-1",
		OfficerID:      "o-1",
		Type:           domain.AlertTypeFire,
		ReportedAt:     clock.T,
		IdempotencyKey: "key-1",
	})
	if err != nil {
		t.Fatalf("report: %v", err)
	}

	// Advance the clock past the 5-minute deadline and rebuild the service.
	advanced := domain.FixedClock{T: clock.T.Add(6 * time.Minute)}
	alertSvc2 := app.NewAlertService(st, advanced, "chief-001")

	s := NewScheduler(alertSvc2, &fakeActivation{}, time.Hour, nil)
	s.TickOnce()

	a, _ := st.AlertByidempotencyKey("key-1")
	if a.Status != domain.AlertStatusEscalated {
		t.Fatalf("expected escalated after tick, got %s", a.Status)
	}
	if a.EscalatedTo != "chief-001" {
		t.Fatalf("expected escalation to chief-001, got %s", a.EscalatedTo)
	}
}
