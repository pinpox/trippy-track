package main

import (
	"database/sql"
	"fmt"
	"math"
	"time"
)

// InterpolatePosition finds the GPS position at a given timestamp by
// interpolating between the two nearest trackpoints in the database.
func InterpolatePosition(db *sql.DB, tripID string, ts time.Time) (lat, lon float64, ok bool) {
	tsStr := ts.UTC().Format(time.RFC3339)

	// Find the trackpoint just before
	var beforeLat, beforeLon float64
	var beforeTS string
	errBefore := db.QueryRow(
		"SELECT lat, lon, timestamp FROM trackpoints WHERE trip_id = ? AND timestamp <= ? ORDER BY timestamp DESC LIMIT 1",
		tripID, tsStr,
	).Scan(&beforeLat, &beforeLon, &beforeTS)

	// Find the trackpoint just after
	var afterLat, afterLon float64
	var afterTS string
	errAfter := db.QueryRow(
		"SELECT lat, lon, timestamp FROM trackpoints WHERE trip_id = ? AND timestamp >= ? ORDER BY timestamp ASC LIMIT 1",
		tripID, tsStr,
	).Scan(&afterLat, &afterLon, &afterTS)

	if errBefore != nil && errAfter != nil {
		return 0, 0, false
	}

	// Only one side available — use it directly
	if errBefore != nil {
		return afterLat, afterLon, true
	}
	if errAfter != nil {
		return beforeLat, beforeLon, true
	}

	// Same point
	if beforeTS == afterTS {
		return beforeLat, beforeLon, true
	}

	// Interpolate
	tBefore, _ := time.Parse(time.RFC3339, beforeTS)
	tAfter, _ := time.Parse(time.RFC3339, afterTS)

	totalDuration := tAfter.Sub(tBefore).Seconds()
	if totalDuration == 0 {
		return beforeLat, beforeLon, true
	}

	elapsed := ts.Sub(tBefore).Seconds()
	ratio := elapsed / totalDuration

	lat = beforeLat + (afterLat-beforeLat)*ratio
	lon = beforeLon + (afterLon-beforeLon)*ratio

	return lat, lon, true
}

// Haversine returns the distance in km between two lat/lon points.
func Haversine(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371.0 // Earth radius in km
	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*math.Pi/180)*math.Cos(lat2*math.Pi/180)*
			math.Sin(dLon/2)*math.Sin(dLon/2)
	return R * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

// DistanceBetweenEntries computes the distance in km between consecutive entries.
// Returns a slice of length len(entries), where index i holds the distance
// from entry i-1 to entry i. Index 0 is always 0.
func DistanceBetweenEntries(entries []Entry) []float64 {
	dists := make([]float64, len(entries))
	for i := 1; i < len(entries); i++ {
		prev := entries[i-1]
		cur := entries[i]
		if prev.Lat != nil && prev.Lon != nil && cur.Lat != nil && cur.Lon != nil {
			dists[i] = Haversine(*prev.Lat, *prev.Lon, *cur.Lat, *cur.Lon)
		}
	}
	return dists
}

// TotalTrackDistance returns the total distance in km along the trackpoints.
func TotalTrackDistance(points []Trackpoint) float64 {
	var total float64
	for i := 1; i < len(points); i++ {
		total += Haversine(points[i-1].Lat, points[i-1].Lon, points[i].Lat, points[i].Lon)
	}
	return total
}

type TripStats struct {
	TotalDistanceKm float64
	DurationDays    int
	DurationHours   int
	EntryCount      int
	IsActive        bool
	LastSpeed       *float64 // km/h
}

func ComputeStats(points []Trackpoint, entryCount int, isActive bool) TripStats {
	stats := TripStats{
		EntryCount: entryCount,
		IsActive:   isActive,
	}

	if len(points) == 0 {
		return stats
	}

	stats.TotalDistanceKm = TotalTrackDistance(points)

	first := points[0].Timestamp
	last := points[len(points)-1].Timestamp
	dur := last.Sub(first)
	stats.DurationDays = int(dur.Hours()) / 24
	stats.DurationHours = int(dur.Hours()) % 24

	// Last known speed
	lastPoint := points[len(points)-1]
	if lastPoint.Speed != nil {
		stats.LastSpeed = lastPoint.Speed
	}

	return stats
}

// FormatCoord returns a human-readable coordinate string.
func FormatCoord(lat, lon float64) string {
	ns := "N"
	if lat < 0 {
		ns = "S"
		lat = -lat
	}
	ew := "E"
	if lon < 0 {
		ew = "W"
		lon = -lon
	}
	return fmt.Sprintf("%.4f%s, %.4f%s", lat, ns, lon, ew)
}
