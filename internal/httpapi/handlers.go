package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"patrol-platform/internal/app"
	"patrol-platform/internal/domain"
)

// ---------------------------------------------------------------------------
// JSON helpers
// ---------------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func errorStatus(err error) (int, string) {
	switch {
	case errors.Is(err, domain.ErrShiftNotFound),
		errors.Is(err, domain.ErrEquipmentNotFound),
		errors.Is(err, domain.ErrClaimNotFound),
		errors.Is(err, domain.ErrAlertNotFound),
		errors.Is(err, domain.ErrCheckInNotFound),
		errors.Is(err, domain.ErrRouteNotFound),
		errors.Is(err, domain.ErrArchiveNotFound):
		return http.StatusNotFound, err.Error()
	case errors.Is(err, domain.ErrInvalidTransition),
		errors.Is(err, domain.ErrClaimAlreadyTerminal):
		return http.StatusConflict, err.Error()
	case errors.Is(err, domain.ErrLockWindowViolation),
		errors.Is(err, domain.ErrOutstandingEquipment),
		errors.Is(err, domain.ErrWeatherProhibited),
		errors.Is(err, domain.ErrCrossStationApproval),
		errors.Is(err, domain.ErrShiftNotApproved),
		errors.Is(err, domain.ErrArchiveAccessDenied):
		return http.StatusUnprocessableEntity, err.Error()
	case errors.Is(err, domain.ErrRoleForbidden):
		return http.StatusForbidden, err.Error()
	case errors.Is(err, domain.ErrValidation):
		return http.StatusBadRequest, err.Error()
	default:
		return http.StatusBadRequest, err.Error()
	}
}

// ---------------------------------------------------------------------------
// Health
// ---------------------------------------------------------------------------

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ---------------------------------------------------------------------------
// Shift handlers
// ---------------------------------------------------------------------------

type generateShiftsRequest struct {
	StationID   string           `json:"station_id"`
	ChiefID     string           `json:"chief_id"`
	WeekStart   time.Time        `json:"week_start"`
	Assignments []assignmentSpec `json:"assignments"`
}

type assignmentSpec struct {
	OfficerID   string    `json:"officer_id"`
	RouteID     string    `json:"route_id"`
	DepartureAt time.Time `json:"departure_at"`
	Priority    int       `json:"priority"`
}

func (s *Server) handleGenerateShifts(w http.ResponseWriter, r *http.Request) {
	var req generateShiftsRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	specs := make([]app.AssignmentSpec, len(req.Assignments))
	for i, a := range req.Assignments {
		specs[i] = app.AssignmentSpec{
			OfficerID:   a.OfficerID,
			RouteID:     a.RouteID,
			DepartureAt: a.DepartureAt,
			Priority:    domain.ShiftPriority(a.Priority),
		}
	}
	shifts, err := s.shift.GenerateWeeklyShifts(req.StationID, req.ChiefID, req.WeekStart, specs)
	if err != nil {
		code, msg := errorStatus(err)
		writeError(w, code, msg)
		return
	}
	writeJSON(w, http.StatusCreated, shifts)
}

func (s *Server) handleSubmitShift(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.shift.SubmitShift(id); err != nil {
		code, msg := errorStatus(err)
		writeError(w, code, msg)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "submitted"})
}

func (s *Server) handleApproveShift(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	chiefID := r.Header.Get("X-User-ID")
	if err := s.shift.ApproveShift(id, chiefID); err != nil {
		code, msg := errorStatus(err)
		writeError(w, code, msg)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "approved"})
}

func (s *Server) handleCompleteShift(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.shift.CompleteShift(id); err != nil {
		code, msg := errorStatus(err)
		writeError(w, code, msg)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "completed"})
}

func (s *Server) handleCancelShift(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.shift.CancelShift(id); err != nil {
		code, msg := errorStatus(err)
		writeError(w, code, msg)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

func (s *Server) handleListShifts(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.shift.ListAllShifts())
}

func (s *Server) handleGetShift(w http.ResponseWriter, r *http.Request) {
	sh, err := s.shift.GetShift(r.PathValue("id"))
	if err != nil {
		code, msg := errorStatus(err)
		writeError(w, code, msg)
		return
	}
	writeJSON(w, http.StatusOK, sh)
}

// ---------------------------------------------------------------------------
// Equipment handlers
// ---------------------------------------------------------------------------

type registerEquipmentRequest struct {
	Name          string `json:"name"`
	Type          string `json:"type"`
	HomeStationID string `json:"home_station_id"`
}

func (s *Server) handleRegisterEquipment(w http.ResponseWriter, r *http.Request) {
	var req registerEquipmentRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	e, err := s.equip.RegisterEquipment(app.RegisterEquipmentRequest{
		Name:          req.Name,
		Type:          domain.EquipmentType(req.Type),
		HomeStationID: req.HomeStationID,
	})
	if err != nil {
		code, msg := errorStatus(err)
		writeError(w, code, msg)
		return
	}
	writeJSON(w, http.StatusCreated, e)
}

func (s *Server) handleListEquipment(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.equip.ListEquipment())
}

type submitClaimRequest struct {
	ShiftID     string `json:"shift_id"`
	EquipmentID string `json:"equipment_id"`
}

func (s *Server) handleSubmitClaim(w http.ResponseWriter, r *http.Request) {
	var req submitClaimRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	officerID := r.Header.Get("X-User-ID")
	result, err := s.equip.SubmitClaim(app.SubmitClaimRequest{
		ShiftID:     req.ShiftID,
		EquipmentID: req.EquipmentID,
		OfficerID:   officerID,
	})
	if err != nil {
		code, msg := errorStatus(err)
		writeError(w, code, msg)
		return
	}
	status := http.StatusCreated
	if result.Queued {
		status = http.StatusAccepted
	}
	writeJSON(w, status, result)
}

func (s *Server) handleBorrowEquipment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	keeperID := r.Header.Get("X-User-ID")
	if err := s.equip.BorrowEquipment(id, keeperID); err != nil {
		code, msg := errorStatus(err)
		writeError(w, code, msg)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "borrowed"})
}

type returnRequest struct {
	LossNote string `json:"loss_note"`
}

func (s *Server) handleReturnEquipment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	keeperID := r.Header.Get("X-User-ID")
	var req returnRequest
	_ = decodeJSON(r, &req) // body is optional
	if err := s.equip.ReturnEquipment(id, keeperID, req.LossNote); err != nil {
		code, msg := errorStatus(err)
		writeError(w, code, msg)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "returned"})
}

func (s *Server) handleCancelClaim(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.equip.CancelClaim(id); err != nil {
		code, msg := errorStatus(err)
		writeError(w, code, msg)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

func (s *Server) handleApproveCrossStation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	dispatcherID := r.Header.Get("X-User-ID")
	result, err := s.equip.ApproveCrossStationClaim(id, dispatcherID)
	if err != nil {
		code, msg := errorStatus(err)
		writeError(w, code, msg)
		return
	}
	status := http.StatusOK
	if result.Queued {
		status = http.StatusAccepted
	}
	writeJSON(w, status, result)
}

func (s *Server) handleGetClaim(w http.ResponseWriter, r *http.Request) {
	c, err := s.equip.GetClaim(r.PathValue("id"))
	if err != nil {
		code, msg := errorStatus(err)
		writeError(w, code, msg)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// ---------------------------------------------------------------------------
// Patrol handlers
// ---------------------------------------------------------------------------

type createRouteRequest struct {
	Name      string            `json:"name"`
	StationID string            `json:"station_id"`
	Waypoints []domain.Waypoint `json:"waypoints"`
}

func (s *Server) handleCreateRoute(w http.ResponseWriter, r *http.Request) {
	var req createRouteRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	route, err := s.patrol.CreateRoute(app.CreateRouteRequest{
		Name:      req.Name,
		StationID: req.StationID,
		Waypoints: req.Waypoints,
	})
	if err != nil {
		code, msg := errorStatus(err)
		writeError(w, code, msg)
		return
	}
	writeJSON(w, http.StatusCreated, route)
}

func (s *Server) handleListRoutes(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.patrol.ListRoutes())
}

type checkInRequest struct {
	ShiftID        string    `json:"shift_id"`
	RouteID        string    `json:"route_id"`
	WaypointID     string    `json:"waypoint_id"`
	Latitude       float64   `json:"latitude"`
	Longitude      float64   `json:"longitude"`
	Timestamp      time.Time `json:"timestamp"`
	Offline        bool      `json:"offline"`
	IdempotencyKey string    `json:"idempotency_key"`
}

func (s *Server) handleCheckIn(w http.ResponseWriter, r *http.Request) {
	var req checkInRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	officerID := r.Header.Get("X-User-ID")
	ci, dup, err := s.patrol.CheckIn(app.CheckInRequest{
		ShiftID:        req.ShiftID,
		OfficerID:      officerID,
		RouteID:        req.RouteID,
		WaypointID:     req.WaypointID,
		Latitude:       req.Latitude,
		Longitude:      req.Longitude,
		Timestamp:      req.Timestamp,
		Offline:        req.Offline,
		IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		code, msg := errorStatus(err)
		writeError(w, code, msg)
		return
	}
	status := http.StatusCreated
	if dup {
		status = http.StatusOK
	}
	writeJSON(w, status, ci)
}

func (s *Server) handleCheckInBatch(w http.ResponseWriter, r *http.Request) {
	var reqs []checkInRequest
	if err := decodeJSON(r, &reqs); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	officerID := r.Header.Get("X-User-ID")
	specs := make([]app.CheckInRequest, len(reqs))
	for i, rq := range reqs {
		specs[i] = app.CheckInRequest{
			ShiftID:        rq.ShiftID,
			OfficerID:      officerID,
			RouteID:        rq.RouteID,
			WaypointID:     rq.WaypointID,
			Latitude:       rq.Latitude,
			Longitude:      rq.Longitude,
			Timestamp:      rq.Timestamp,
			Offline:        rq.Offline,
			IdempotencyKey: rq.IdempotencyKey,
		}
	}
	results, err := s.patrol.CheckInBatch(specs)
	if err != nil {
		code, msg := errorStatus(err)
		writeError(w, code, msg)
		return
	}
	writeJSON(w, http.StatusOK, results)
}

// ---------------------------------------------------------------------------
// Alert handlers
// ---------------------------------------------------------------------------

type reportAlertRequest struct {
	ShiftID        string    `json:"shift_id"`
	Type           string    `json:"type"`
	Description    string    `json:"description"`
	Location       string    `json:"location"`
	ReportedAt     time.Time `json:"reported_at"`
	Offline        bool      `json:"offline"`
	IdempotencyKey string    `json:"idempotency_key"`
}

func (s *Server) handleReportAlert(w http.ResponseWriter, r *http.Request) {
	var req reportAlertRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	officerID := r.Header.Get("X-User-ID")
	a, dup, err := s.alert.ReportAlert(app.ReportAlertRequest{
		ShiftID:        req.ShiftID,
		OfficerID:      officerID,
		Type:           domain.AlertType(req.Type),
		Description:    req.Description,
		Location:       req.Location,
		ReportedAt:     req.ReportedAt,
		Offline:        req.Offline,
		IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		code, msg := errorStatus(err)
		writeError(w, code, msg)
		return
	}
	status := http.StatusCreated
	if dup {
		status = http.StatusOK
	}
	writeJSON(w, status, a)
}

func (s *Server) handleReportAlertBatch(w http.ResponseWriter, r *http.Request) {
	var reqs []reportAlertRequest
	if err := decodeJSON(r, &reqs); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	officerID := r.Header.Get("X-User-ID")
	specs := make([]app.ReportAlertRequest, len(reqs))
	for i, rq := range reqs {
		specs[i] = app.ReportAlertRequest{
			ShiftID:        rq.ShiftID,
			OfficerID:      officerID,
			Type:           domain.AlertType(rq.Type),
			Description:    rq.Description,
			Location:       rq.Location,
			ReportedAt:     rq.ReportedAt,
			Offline:        rq.Offline,
			IdempotencyKey: rq.IdempotencyKey,
		}
	}
	results, err := s.alert.ReportAlertBatch(specs)
	if err != nil {
		code, msg := errorStatus(err)
		writeError(w, code, msg)
		return
	}
	writeJSON(w, http.StatusOK, results)
}

func (s *Server) handleSignAlert(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	dispatcherID := r.Header.Get("X-User-ID")
	if err := s.alert.SignAlert(id, dispatcherID); err != nil {
		code, msg := errorStatus(err)
		writeError(w, code, msg)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "signed"})
}

type dispatchRequest struct {
	AssigneeID string `json:"assignee_id"`
}

func (s *Server) handleDispatchAlert(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req dispatchRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.alert.DispatchAlert(id, req.AssigneeID); err != nil {
		code, msg := errorStatus(err)
		writeError(w, code, msg)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "dispatched"})
}

func (s *Server) handleStartProgress(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.alert.StartProgress(id); err != nil {
		code, msg := errorStatus(err)
		writeError(w, code, msg)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "in_progress"})
}

func (s *Server) handleResolveAlert(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.alert.ResolveAlert(id); err != nil {
		code, msg := errorStatus(err)
		writeError(w, code, msg)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "resolved"})
}

func (s *Server) handleCloseAlert(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.alert.CloseAlert(id); err != nil {
		code, msg := errorStatus(err)
		writeError(w, code, msg)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "closed"})
}

func (s *Server) handleEscalateAlert(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.alert.EscalateAlert(id); err != nil {
		code, msg := errorStatus(err)
		writeError(w, code, msg)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "escalated"})
}

func (s *Server) handleListAlerts(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.alert.ListAlerts())
}

func (s *Server) handleGetAlert(w http.ResponseWriter, r *http.Request) {
	a, err := s.alert.GetAlert(r.PathValue("id"))
	if err != nil {
		code, msg := errorStatus(err)
		writeError(w, code, msg)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

// ---------------------------------------------------------------------------
// Archive handlers
// ---------------------------------------------------------------------------

type archiveRequest struct {
	CameraData []byte `json:"camera_data"`
	Notes      string `json:"notes"`
}

func (s *Server) handleArchive(w http.ResponseWriter, r *http.Request) {
	shiftID := r.PathValue("id")
	var req archiveRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	actor := actorFromRequest(r)
	a, err := s.archive.Archive(app.ArchiveRequest{
		ShiftID:    shiftID,
		CameraData: req.CameraData,
		ArchivedBy: actor.ID,
		Notes:      req.Notes,
	})
	if err != nil {
		code, msg := errorStatus(err)
		writeError(w, code, msg)
		return
	}
	writeJSON(w, http.StatusCreated, a)
}

func (s *Server) handleGetArchive(w http.ResponseWriter, r *http.Request) {
	shiftID := r.PathValue("id")
	actor := actorFromRequest(r)
	a, err := s.archive.GetArchive(shiftID, actor)
	if err != nil {
		code, msg := errorStatus(err)
		writeError(w, code, msg)
		return
	}
	writeJSON(w, http.StatusOK, a)
}
