package domain

import "time"

// WeatherSnapshot captures conditions at a station at a point in time.
type WeatherSnapshot struct {
	StationID        string    `json:"station_id"`
	HasPrecipitation bool      `json:"has_precipitation"`
	WindForce        int       `json:"wind_force"`
	ObservedAt       time.Time `json:"observed_at"`
}

// WeatherProvider abstracts current-weather lookups so the drone
// dispatch rule can be tested deterministically.
type WeatherProvider interface {
	Current(stationID string) (WeatherSnapshot, error)
}

// DroneFlightProhibited reports whether drones may not leave the depot
// under the given conditions: precipitation or wind force >= 6.
func DroneFlightProhibited(w WeatherSnapshot) bool {
	return w.HasPrecipitation || w.WindForce >= 6
}

// StaticWeatherProvider returns a fixed snapshot for every station.
// In production this would be replaced by a real meteorological feed.
type StaticWeatherProvider struct {
	Snapshot WeatherSnapshot
}

func (p StaticWeatherProvider) Current(_ string) (WeatherSnapshot, error) {
	return p.Snapshot, nil
}
