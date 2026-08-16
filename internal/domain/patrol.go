package domain

import "time"

// Waypoint is an ordered checkpoint along a patrol route.
type Waypoint struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Order     int     `json:"order"`
}

// Route is a preset patrol path with ordered waypoints.
type Route struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	StationID string     `json:"station_id"`
	Waypoints []Waypoint `json:"waypoints"`
}

// CheckIn records a patrol officer's arrival at a waypoint.
type CheckIn struct {
	ID             string    `json:"id"`
	ShiftID        string    `json:"shift_id"`
	OfficerID      string    `json:"officer_id"`
	RouteID        string    `json:"route_id"`
	WaypointID     string    `json:"waypoint_id"`
	Latitude       float64   `json:"latitude"`
	Longitude      float64   `json:"longitude"`
	Timestamp      time.Time `json:"timestamp"`
	ReceivedAt     time.Time `json:"received_at"`
	Offline        bool      `json:"offline"`
	IdempotencyKey string    `json:"idempotency_key"`
}

// Archive stores infrared-camera data captured during a shift.
type Archive struct {
	ShiftID    string    `json:"shift_id"`
	CameraData []byte    `json:"camera_data"`
	ArchivedBy string    `json:"archived_by"`
	ArchivedAt time.Time `json:"archived_at"`
	Notes      string    `json:"notes"`
}
