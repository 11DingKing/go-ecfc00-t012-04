package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"patrol-platform/internal/app"
	"patrol-platform/internal/domain"
	"patrol-platform/internal/store"
)

func newTestServer() (*Server, *store.MemoryStore) {
	clock := domain.FixedClock{T: time.Date(2026, 3, 2, 6, 0, 0, 0, time.UTC)}
	st := store.New()
	wp := domain.StaticWeatherProvider{Snapshot: domain.WeatherSnapshot{
		HasPrecipitation: false, WindForce: 3, ObservedAt: clock.T,
	}}
	shiftSvc := app.NewShiftService(st, clock)
	equipSvc := app.NewEquipmentService(st, st, clock, wp)
	alertSvc := app.NewAlertService(st, clock, "chief-001")
	patrolSvc := app.NewPatrolService(st, clock)
	archiveSvc := app.NewArchiveService(st, clock)
	srv := NewServer(shiftSvc, equipSvc, alertSvc, patrolSvc, archiveSvc, clock, nil)
	return srv, st
}

func doRequest(t *testing.T, srv *Server, method, path, role, userID string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	if userID != "" {
		req.Header.Set("X-User-ID", userID)
	}
	if role != "" {
		req.Header.Set("X-User-Role", role)
	}
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	return rr
}

func TestHTTPShiftGenerateAndApprove(t *testing.T) {
	srv, _ := newTestServer()
	base := time.Date(2026, 3, 2, 6, 0, 0, 0, time.UTC)

	rr := doRequest(t, srv, "POST", "/api/shifts/generate", "chief", "chief-001", generateShiftsRequest{
		StationID: "hualong",
		ChiefID:   "chief-001",
		WeekStart: base,
		Assignments: []assignmentSpec{{
			OfficerID: "officer-1", RouteID: "route-1",
			DepartureAt: base.Add(4 * time.Hour), Priority: 2,
		}},
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	var shifts []domain.Shift
	json.Unmarshal(rr.Body.Bytes(), &shifts)
	if len(shifts) != 1 {
		t.Fatalf("expected 1 shift, got %d", len(shifts))
	}
	shiftID := shifts[0].ID

	// Submit and approve.
	rr = doRequest(t, srv, "POST", "/api/shifts/"+shiftID+"/submit", "chief", "chief-001", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("submit: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	rr = doRequest(t, srv, "POST", "/api/shifts/"+shiftID+"/approve", "chief", "chief-001", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("approve: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Non-chief should be forbidden.
	rr = doRequest(t, srv, "POST", "/api/shifts/"+shiftID+"/approve", "officer", "officer-1", nil)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-chief, got %d", rr.Code)
	}
}

func TestHTTPEquipmentClaimFlow(t *testing.T) {
	srv, _ := newTestServer()
	base := time.Date(2026, 3, 2, 6, 0, 0, 0, time.UTC)

	// Register equipment as keeper.
	rr := doRequest(t, srv, "POST", "/api/equipment", "keeper", "keeper-1", registerEquipmentRequest{
		Name: "Phone-1", Type: "satellite_phone", HomeStationID: "hualong",
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("register equipment: expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	var eq domain.Equipment
	json.Unmarshal(rr.Body.Bytes(), &eq)

	// Generate and approve a shift.
	rr = doRequest(t, srv, "POST", "/api/shifts/generate", "chief", "chief-001", generateShiftsRequest{
		StationID: "hualong", ChiefID: "chief-001", WeekStart: base,
		Assignments: []assignmentSpec{{
			OfficerID: "officer-1", RouteID: "route-1",
			DepartureAt: base.Add(4 * time.Hour), Priority: 2,
		}},
	})
	var shifts []domain.Shift
	json.Unmarshal(rr.Body.Bytes(), &shifts)
	shiftID := shifts[0].ID
	doRequest(t, srv, "POST", "/api/shifts/"+shiftID+"/submit", "chief", "chief-001", nil)
	doRequest(t, srv, "POST", "/api/shifts/"+shiftID+"/approve", "chief", "chief-001", nil)

	// Officer claims equipment.
	rr = doRequest(t, srv, "POST", "/api/equipment/"+eq.ID+"/claims", "officer", "officer-1", submitClaimRequest{
		ShiftID: shiftID, EquipmentID: eq.ID,
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("claim: expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	var result app.SubmitClaimResult
	json.Unmarshal(rr.Body.Bytes(), &result)
	if !result.Won {
		t.Fatal("expected claim to win")
	}
	claimID := result.Claim.ID

	// Keeper borrows.
	rr = doRequest(t, srv, "POST", "/api/claims/"+claimID+"/borrow", "keeper", "keeper-1", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("borrow: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Keeper returns.
	rr = doRequest(t, srv, "POST", "/api/claims/"+claimID+"/return", "keeper", "keeper-1", returnRequest{})
	if rr.Code != http.StatusOK {
		t.Fatalf("return: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHTTPAlertReportAndSign(t *testing.T) {
	srv, _ := newTestServer()

	rr := doRequest(t, srv, "POST", "/api/alerts", "officer", "officer-1", reportAlertRequest{
		ShiftID: "shift-1", Type: "fire", Description: "smoke",
		Location: "north ridge", IdempotencyKey: "http-alert-1",
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("report alert: expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	var a domain.Alert
	json.Unmarshal(rr.Body.Bytes(), &a)

	// Duplicate idempotency key returns 200 with original record.
	rr = doRequest(t, srv, "POST", "/api/alerts", "officer", "officer-1", reportAlertRequest{
		ShiftID: "shift-1", Type: "fire", Description: "different",
		IdempotencyKey: "http-alert-1",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("duplicate alert: expected 200, got %d", rr.Code)
	}
	var a2 domain.Alert
	json.Unmarshal(rr.Body.Bytes(), &a2)
	if a2.Description != "smoke" {
		t.Fatal("duplicate should return original record")
	}

	// Dispatcher signs.
	rr = doRequest(t, srv, "POST", "/api/alerts/"+a.ID+"/sign", "dispatcher", "disp-1", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("sign: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Officer cannot sign (wrong role).
	rr = doRequest(t, srv, "POST", "/api/alerts/"+a.ID+"/dispatch", "officer", "officer-1", dispatchRequest{AssigneeID: "r-1"})
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for officer dispatch, got %d", rr.Code)
	}
}

func TestHTTPCheckInBatch(t *testing.T) {
	srv, _ := newTestServer()
	base := time.Date(2026, 3, 2, 8, 0, 0, 0, time.UTC)

	reqs := []checkInRequest{
		{ShiftID: "s1", RouteID: "r1", WaypointID: "w2", Timestamp: base.Add(20 * time.Minute), IdempotencyKey: "h-ci2", Offline: true},
		{ShiftID: "s1", RouteID: "r1", WaypointID: "w1", Timestamp: base.Add(10 * time.Minute), IdempotencyKey: "h-ci1", Offline: true},
	}
	rr := doRequest(t, srv, "POST", "/api/checkins/batch", "officer", "officer-1", reqs)
	if rr.Code != http.StatusOK {
		t.Fatalf("batch check-in: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var results []domain.CheckIn
	json.Unmarshal(rr.Body.Bytes(), &results)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].IdempotencyKey != "h-ci1" {
		t.Fatal("batch should be ordered by timestamp")
	}
}

func TestHTTPArchiveAccessDenied(t *testing.T) {
	srv, _ := newTestServer()

	// Archive data.
	rr := doRequest(t, srv, "POST", "/api/shifts/shift-1/archive", "keeper", "keeper-1", archiveRequest{
		CameraData: []byte("data"),
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("archive: expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	// Officer denied.
	rr = doRequest(t, srv, "GET", "/api/shifts/shift-1/archive", "officer", "officer-1", nil)
	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for officer archive access, got %d", rr.Code)
	}

	// Chief allowed.
	rr = doRequest(t, srv, "GET", "/api/shifts/shift-1/archive", "chief", "chief-001", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("chief archive: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHTTPHealth(t *testing.T) {
	srv, _ := newTestServer()
	rr := doRequest(t, srv, "GET", "/api/health", "", "", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("health: expected 200, got %d", rr.Code)
	}
}
