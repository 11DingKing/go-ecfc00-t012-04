package app

import (
	"fmt"
	"sort"
	"time"

	"patrol-platform/internal/domain"
)

// PatrolStore is the persistence interface consumed by PatrolService.
type PatrolStore interface {
	SaveCheckIn(domain.CheckIn)
	GetCheckIn(id string) (domain.CheckIn, bool)
	CheckInByIdempotencyKey(key string) (domain.CheckIn, bool)
	ListCheckInsByShift(shiftID string) []domain.CheckIn
	SaveRoute(domain.Route)
	GetRoute(id string) (domain.Route, bool)
	ListRoutes() []domain.Route
}

// PatrolService handles route management, online check-ins, and offline
// batch replay with idempotency and original-timestamp ordering.
type PatrolService struct {
	store PatrolStore
	clock domain.Clock
}

// NewPatrolService constructs a PatrolService.
func NewPatrolService(s PatrolStore, clock domain.Clock) *PatrolService {
	return &PatrolService{store: s, clock: clock}
}

// CreateRouteRequest creates a preset patrol route with waypoints.
type CreateRouteRequest struct {
	Name      string
	StationID string
	Waypoints []domain.Waypoint
}

// CreateRoute registers a new route.
func (svc *PatrolService) CreateRoute(req CreateRouteRequest) (domain.Route, error) {
	if req.Name == "" || req.StationID == "" {
		return domain.Route{}, fmt.Errorf("%w: name and station_id required", domain.ErrValidation)
	}
	if len(req.Waypoints) == 0 {
		return domain.Route{}, fmt.Errorf("%w: at least one waypoint required", domain.ErrValidation)
	}
	sort.Slice(req.Waypoints, func(i, j int) bool {
		return req.Waypoints[i].Order < req.Waypoints[j].Order
	})
	r := domain.Route{
		ID:        domain.NewID("route"),
		Name:      req.Name,
		StationID: req.StationID,
		Waypoints: req.Waypoints,
	}
	svc.store.SaveRoute(r)
	return r, nil
}

// CheckInRequest is the input for recording a waypoint arrival.
type CheckInRequest struct {
	ShiftID        string
	OfficerID      string
	RouteID        string
	WaypointID     string
	Latitude       float64
	Longitude      float64
	Timestamp      time.Time // original device timestamp
	Offline        bool
	IdempotencyKey string
}

// CheckIn records a single check-in. If the idempotency key was already
// processed the existing record is returned without re-counting.
func (svc *PatrolService) CheckIn(req CheckInRequest) (domain.CheckIn, bool, error) {
	if req.ShiftID == "" || req.OfficerID == "" {
		return domain.CheckIn{}, false, fmt.Errorf("%w: shift_id and officer_id required", domain.ErrValidation)
	}
	if req.IdempotencyKey == "" {
		return domain.CheckIn{}, false, fmt.Errorf("%w: idempotency_key required", domain.ErrValidation)
	}

	// Idempotency: return existing record if the key was seen.
	if existing, ok := svc.store.CheckInByIdempotencyKey(req.IdempotencyKey); ok {
		return existing, true, nil
	}

	now := svc.clock.Now()
	ts := req.Timestamp
	if ts.IsZero() {
		ts = now
	}
	ci := domain.CheckIn{
		ID:             domain.NewID("checkin"),
		ShiftID:        req.ShiftID,
		OfficerID:      req.OfficerID,
		RouteID:        req.RouteID,
		WaypointID:     req.WaypointID,
		Latitude:       req.Latitude,
		Longitude:      req.Longitude,
		Timestamp:      ts,
		ReceivedAt:     now,
		Offline:        req.Offline,
		IdempotencyKey: req.IdempotencyKey,
	}
	svc.store.SaveCheckIn(ci)
	return ci, false, nil
}

// CheckInBatch replays offline check-ins sorted by original timestamp.
// Duplicates are skipped via idempotency keys; no records are lost.
func (svc *PatrolService) CheckInBatch(reqs []CheckInRequest) ([]domain.CheckIn, error) {
	sorted := make([]CheckInRequest, len(reqs))
	copy(sorted, reqs)
	sort.SliceStable(sorted, func(i, j int) bool {
		ti := sorted[i].Timestamp
		tj := sorted[j].Timestamp
		if ti.IsZero() {
			ti = svc.clock.Now()
		}
		if tj.IsZero() {
			tj = svc.clock.Now()
		}
		return ti.Before(tj)
	})

	results := make([]domain.CheckIn, 0, len(sorted))
	for _, r := range sorted {
		ci, _, err := svc.CheckIn(r)
		if err != nil {
			return results, err
		}
		results = append(results, ci)
	}
	return results, nil
}

// ListCheckIns returns all check-ins for a shift ordered by timestamp.
func (svc *PatrolService) ListCheckIns(shiftID string) []domain.CheckIn {
	return svc.store.ListCheckInsByShift(shiftID)
}

// ListRoutes returns all routes.
func (svc *PatrolService) ListRoutes() []domain.Route {
	return svc.store.ListRoutes()
}

// GetRoute returns a route by ID.
func (svc *PatrolService) GetRoute(id string) (domain.Route, error) {
	r, ok := svc.store.GetRoute(id)
	if !ok {
		return domain.Route{}, domain.ErrRouteNotFound
	}
	return r, nil
}
