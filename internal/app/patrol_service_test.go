package app

import (
	"testing"
	"time"

	"patrol-platform/internal/domain"
)

func TestCheckInOfflineReplayAndIdempotency(t *testing.T) {
	h := newHarness()
	base := h.clock.t

	// Simulate three offline check-ins captured at different waypoints.
	reqs := []CheckInRequest{
		{ShiftID: "s1", OfficerID: "o1", RouteID: "r1", WaypointID: "w3", Timestamp: base.Add(30 * time.Minute), IdempotencyKey: "ci3", Offline: true},
		{ShiftID: "s1", OfficerID: "o1", RouteID: "r1", WaypointID: "w1", Timestamp: base.Add(10 * time.Minute), IdempotencyKey: "ci1", Offline: true},
		{ShiftID: "s1", OfficerID: "o1", RouteID: "r1", WaypointID: "w2", Timestamp: base.Add(20 * time.Minute), IdempotencyKey: "ci2", Offline: true},
	}

	results, err := h.patrol.CheckInBatch(reqs)
	if err != nil {
		t.Fatalf("batch: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// Results should be ordered by original timestamp.
	if results[0].IdempotencyKey != "ci1" || results[1].IdempotencyKey != "ci2" || results[2].IdempotencyKey != "ci3" {
		t.Fatal("check-in batch should be ordered by original timestamp")
	}

	// Replay the batch — duplicates should be skipped, no new records.
	results2, err := h.patrol.CheckInBatch(reqs)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if len(results2) != 3 {
		t.Fatalf("expected 3 results on replay, got %d", len(results2))
	}

	all := h.patrol.ListCheckIns("s1")
	if len(all) != 3 {
		t.Fatalf("expected 3 unique check-ins after replay, got %d", len(all))
	}
}

func TestCheckInSingleIdempotency(t *testing.T) {
	h := newHarness()

	req := CheckInRequest{
		ShiftID: "s1", OfficerID: "o1", RouteID: "r1", WaypointID: "w1",
		Timestamp: h.clock.t, IdempotencyKey: "single-ci",
	}
	ci1, dup, err := h.patrol.CheckIn(req)
	if err != nil {
		t.Fatalf("check-in: %v", err)
	}
	if dup {
		t.Fatal("first check-in should not be duplicate")
	}

	ci2, dup, err := h.patrol.CheckIn(req)
	if err != nil {
		t.Fatalf("re-check-in: %v", err)
	}
	if !dup {
		t.Fatal("second check-in with same key should be duplicate")
	}
	if ci1.ID != ci2.ID {
		t.Fatal("duplicate should return same record ID")
	}
}

func TestCreateRouteValidatesWaypoints(t *testing.T) {
	h := newHarness()
	_, err := h.patrol.CreateRoute(CreateRouteRequest{
		Name: "Route-A", StationID: "hualong",
	})
	if err == nil {
		t.Fatal("expected error for route with no waypoints")
	}

	r, err := h.patrol.CreateRoute(CreateRouteRequest{
		Name: "Route-A", StationID: "hualong",
		Waypoints: []domain.Waypoint{
			{ID: "w2", Name: "North Ridge", Order: 2},
			{ID: "w1", Name: "Trail Start", Order: 1},
		},
	})
	if err != nil {
		t.Fatalf("create route: %v", err)
	}
	if r.Waypoints[0].ID != "w1" {
		t.Fatal("waypoints should be sorted by order")
	}
}

func TestArchiveAccessControl(t *testing.T) {
	h := newHarness()

	_, err := h.archive.Archive(ArchiveRequest{
		ShiftID: "shift-1", CameraData: []byte("camera-bytes"), ArchivedBy: "keeper-1",
	})
	if err != nil {
		t.Fatalf("archive: %v", err)
	}

	// Officer should be denied.
	_, err = h.archive.GetArchive("shift-1", domain.Actor{ID: "officer-1", Role: domain.RoleOfficer})
	if err != domain.ErrArchiveAccessDenied {
		t.Fatalf("expected ErrArchiveAccessDenied for officer, got %v", err)
	}

	// Keeper should be denied.
	_, err = h.archive.GetArchive("shift-1", domain.Actor{ID: "keeper-1", Role: domain.RoleKeeper})
	if err != domain.ErrArchiveAccessDenied {
		t.Fatalf("expected ErrArchiveAccessDenied for keeper, got %v", err)
	}

	// Chief should be allowed.
	a, err := h.archive.GetArchive("shift-1", domain.Actor{ID: "chief-1", Role: domain.RoleChief})
	if err != nil {
		t.Fatalf("chief should read archive: %v", err)
	}
	if string(a.CameraData) != "camera-bytes" {
		t.Fatal("archive data mismatch")
	}

	// Dispatcher should be allowed.
	_, err = h.archive.GetArchive("shift-1", domain.Actor{ID: "disp-1", Role: domain.RoleDispatcher})
	if err != nil {
		t.Fatalf("dispatcher should read archive: %v", err)
	}
}
