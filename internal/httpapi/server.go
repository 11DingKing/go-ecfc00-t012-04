package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"patrol-platform/internal/app"
	"patrol-platform/internal/background"
	"patrol-platform/internal/domain"
)

// Server wires together the HTTP handlers, services, and background scheduler.
type Server struct {
	shift   *app.ShiftService
	equip   *app.EquipmentService
	alert   *app.AlertService
	patrol  *app.PatrolService
	archive *app.ArchiveService
	clock   domain.Clock
	logger  *slog.Logger
	sched   *background.Scheduler
	httpSrv *http.Server
}

// NewServer constructs a Server with all services and starts the
// background scheduler.
func NewServer(
	shift *app.ShiftService,
	equip *app.EquipmentService,
	alert *app.AlertService,
	patrol *app.PatrolService,
	archive *app.ArchiveService,
	clock domain.Clock,
	logger *slog.Logger,
) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	s := &Server{
		shift:   shift,
		equip:   equip,
		alert:   alert,
		patrol:  patrol,
		archive: archive,
		clock:   clock,
		logger:  logger,
	}
	s.sched = background.NewScheduler(alert, shift, 30*time.Second, logger)
	return s
}

// Handler returns the configured HTTP handler with all routes registered.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", s.handleHealth)

	// Shifts
	mux.HandleFunc("POST /api/shifts/generate", s.requireRole(domain.RoleChief, s.handleGenerateShifts))
	mux.HandleFunc("POST /api/shifts/{id}/submit", s.requireRole(domain.RoleChief, s.handleSubmitShift))
	mux.HandleFunc("POST /api/shifts/{id}/approve", s.requireRole(domain.RoleChief, s.handleApproveShift))
	mux.HandleFunc("POST /api/shifts/{id}/complete", s.requireRole(domain.RoleChief, s.handleCompleteShift))
	mux.HandleFunc("POST /api/shifts/{id}/cancel", s.requireRole(domain.RoleChief, s.handleCancelShift))
	mux.HandleFunc("GET /api/shifts", s.handleListShifts)
	mux.HandleFunc("GET /api/shifts/{id}", s.handleGetShift)

	// Equipment
	mux.HandleFunc("POST /api/equipment", s.requireRole(domain.RoleKeeper, s.handleRegisterEquipment))
	mux.HandleFunc("GET /api/equipment", s.handleListEquipment)
	mux.HandleFunc("POST /api/equipment/{id}/claims", s.requireRole(domain.RoleOfficer, s.handleSubmitClaim))
	mux.HandleFunc("POST /api/claims/{id}/borrow", s.requireRole(domain.RoleKeeper, s.handleBorrowEquipment))
	mux.HandleFunc("POST /api/claims/{id}/return", s.requireRole(domain.RoleKeeper, s.handleReturnEquipment))
	mux.HandleFunc("POST /api/claims/{id}/cancel", s.handleCancelClaim)
	mux.HandleFunc("POST /api/claims/{id}/approve-cross-station", s.requireRole(domain.RoleDispatcher, s.handleApproveCrossStation))
	mux.HandleFunc("GET /api/claims/{id}", s.handleGetClaim)

	// Patrol
	mux.HandleFunc("POST /api/routes", s.requireRole(domain.RoleChief, s.handleCreateRoute))
	mux.HandleFunc("GET /api/routes", s.handleListRoutes)
	mux.HandleFunc("POST /api/checkins", s.requireRole(domain.RoleOfficer, s.handleCheckIn))
	mux.HandleFunc("POST /api/checkins/batch", s.requireRole(domain.RoleOfficer, s.handleCheckInBatch))

	// Alerts
	mux.HandleFunc("POST /api/alerts", s.requireRole(domain.RoleOfficer, s.handleReportAlert))
	mux.HandleFunc("POST /api/alerts/batch", s.requireRole(domain.RoleOfficer, s.handleReportAlertBatch))
	mux.HandleFunc("POST /api/alerts/{id}/sign", s.requireRole(domain.RoleDispatcher, s.handleSignAlert))
	mux.HandleFunc("POST /api/alerts/{id}/dispatch", s.requireRole(domain.RoleDispatcher, s.handleDispatchAlert))
	mux.HandleFunc("POST /api/alerts/{id}/progress", s.requireRole(domain.RoleDispatcher, s.handleStartProgress))
	mux.HandleFunc("POST /api/alerts/{id}/resolve", s.requireRole(domain.RoleDispatcher, s.handleResolveAlert))
	mux.HandleFunc("POST /api/alerts/{id}/close", s.requireRole(domain.RoleDispatcher, s.handleCloseAlert))
	mux.HandleFunc("POST /api/alerts/{id}/escalate", s.requireRole(domain.RoleDispatcher, s.handleEscalateAlert))
	mux.HandleFunc("GET /api/alerts", s.handleListAlerts)
	mux.HandleFunc("GET /api/alerts/{id}", s.handleGetAlert)

	// Archive
	mux.HandleFunc("POST /api/shifts/{id}/archive", s.handleArchive)
	mux.HandleFunc("GET /api/shifts/{id}/archive", s.handleGetArchive)

	return s.recoverMiddleware(s.loggingMiddleware(mux))
}

// Start begins listening on addr and starts the background scheduler.
func (s *Server) Start(addr string) error {
	s.sched.Start(context.Background())
	s.httpSrv = &http.Server{
		Addr:    addr,
		Handler: s.Handler(),
	}
	s.logger.Info("patrol platform listening", "addr", addr)
	return s.httpSrv.ListenAndServe()
}

// Shutdown gracefully stops the HTTP server and scheduler.
func (s *Server) Shutdown(ctx context.Context) error {
	s.sched.Stop()
	if s.httpSrv != nil {
		return s.httpSrv.Shutdown(ctx)
	}
	return nil
}
