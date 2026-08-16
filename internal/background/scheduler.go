package background

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// EscalationRunner is the interface the scheduler calls each tick to
// escalate overdue alerts.
type EscalationRunner interface {
	EscalateOverdue() int
}

// ActivationRunner activates approved shifts whose departure time arrived.
type ActivationRunner interface {
	ActivateDueShifts() int
}

// Scheduler runs periodic background tasks: alert escalation and shift
// activation. It is safe to Start and Stop concurrently.
type Scheduler struct {
	escalation EscalationRunner
	activation ActivationRunner
	interval   time.Duration
	logger     *slog.Logger

	mu     sync.Mutex
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewScheduler constructs a Scheduler with the given tick interval.
func NewScheduler(esc EscalationRunner, act ActivationRunner, interval time.Duration, logger *slog.Logger) *Scheduler {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Scheduler{
		escalation: esc,
		activation: act,
		interval:   interval,
		logger:     logger,
	}
}

// Start launches the background loop. Calling Start twice without Stop
// is a no-op.
func (s *Scheduler) Start(ctx context.Context) {
	s.mu.Lock()
	if s.cancel != nil {
		s.mu.Unlock()
		return
	}
	ctx, s.cancel = context.WithCancel(ctx)
	s.mu.Unlock()

	s.wg.Add(1)
	go s.loop(ctx)
}

// Stop signals the loop to exit and waits for it.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	if s.cancel == nil {
		s.mu.Unlock()
		return
	}
	s.cancel()
	s.cancel = nil
	s.mu.Unlock()
	s.wg.Wait()
}

func (s *Scheduler) loop(ctx context.Context) {
	defer s.wg.Done()
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if n := s.escalation.EscalateOverdue(); n > 0 {
				s.logger.Info("escalated overdue alerts", "count", n)
			}
			if n := s.activation.ActivateDueShifts(); n > 0 {
				s.logger.Info("activated due shifts", "count", n)
			}
		}
	}
}

// TickOnce runs one iteration immediately. Primarily for testing.
func (s *Scheduler) TickOnce() {
	if n := s.escalation.EscalateOverdue(); n > 0 {
		s.logger.Info("escalated overdue alerts", "count", n)
	}
	if n := s.activation.ActivateDueShifts(); n > 0 {
		s.logger.Info("activated due shifts", "count", n)
	}
}
