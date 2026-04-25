package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// setupTestServer creates an in-memory DB with a trip, entries, trackpoints,
// and returns a ready-to-use Server plus the trip's view token.
func setupTestServer(t *testing.T) (*Server, string) {
	t.Helper()

	db, err := initDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	// Create a user
	var userID int
	err = db.QueryRow(
		"INSERT INTO users (oidc_subject, email, name) VALUES (?, ?, ?) RETURNING id",
		"test-sub", "test@example.com", "Test User",
	).Scan(&userID)
	if err != nil {
		t.Fatal(err)
	}

	trip, err := createTrip(db, "Test Trip", userID)
	if err != nil {
		t.Fatal(err)
	}

	// Insert trackpoints along a route
	trackpoints := []Trackpoint{
		{TripID: trip.ID, Lat: 50.9375, Lon: 6.9603, Timestamp: time.Date(2025, 4, 20, 10, 0, 0, 0, time.UTC)},
		{TripID: trip.ID, Lat: 51.0000, Lon: 6.9500, Timestamp: time.Date(2025, 4, 20, 11, 0, 0, 0, time.UTC)},
		{TripID: trip.ID, Lat: 51.0500, Lon: 6.9400, Timestamp: time.Date(2025, 4, 20, 12, 0, 0, 0, time.UTC)},
		{TripID: trip.ID, Lat: 51.2277, Lon: 6.7735, Timestamp: time.Date(2025, 4, 20, 14, 0, 0, 0, time.UTC)},
	}
	for _, tp := range trackpoints {
		if err := insertTrackpoint(db, tp); err != nil {
			t.Fatal(err)
		}
	}

	// Create entries with coordinates and photos
	lat1, lon1 := 50.9375, 6.9603
	lat2, lon2 := 51.2277, 6.7735
	body1 := "Starting the ride!"
	body2 := "Arrived in Düsseldorf"

	entryID1, err := createEntry(db, trip.ID, body1, &lat1, &lon1, nil, "2025-04-20T10:00:00Z", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := addPhoto(db, entryID1, "trip1/photo1.jpg", 0); err != nil {
		t.Fatal(err)
	}
	if err := addPhoto(db, entryID1, "trip1/video1.mp4", 1); err != nil {
		t.Fatal(err)
	}

	entryID2, err := createEntry(db, trip.ID, body2, &lat2, &lon2, nil, "2025-04-20T14:00:00Z", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := addPhoto(db, entryID2, "trip1/photo2.jpg", 0); err != nil {
		t.Fatal(err)
	}

	// Entry without coordinates (should be skipped in GeoJSON markers)
	_, err = createEntry(db, trip.ID, "no location", nil, nil, nil, "2025-04-20T12:00:00Z", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	srv, err := newServer(db, ":0", t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}

	return srv, trip.ViewToken
}

// GeoJSON types for test assertions
type geoJSON struct {
	Type     string       `json:"type"`
	Features []geoFeature `json:"features"`
}

type geoFeature struct {
	Type       string         `json:"type"`
	Geometry   geoGeometry    `json:"geometry"`
	Properties map[string]any `json:"properties"`
}

type geoGeometry struct {
	Type        string          `json:"type"`
	Coordinates json.RawMessage `json:"coordinates"`
}

func TestGeoJSONEndpoint(t *testing.T) {
	srv, viewToken := setupTestServer(t)
	mux := srv.routes()

	t.Run("valid token returns GeoJSON FeatureCollection", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/t/"+viewToken+"/track", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		if ct := w.Header().Get("Content-Type"); ct != "application/geo+json" {
			t.Fatalf("expected application/geo+json, got %s", ct)
		}

		var fc geoJSON
		if err := json.Unmarshal(w.Body.Bytes(), &fc); err != nil {
			t.Fatalf("invalid JSON: %v\nbody: %s", err, w.Body.String())
		}
		if fc.Type != "FeatureCollection" {
			t.Fatalf("expected FeatureCollection, got %s", fc.Type)
		}
	})

	t.Run("contains track line feature", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/t/"+viewToken+"/track", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		var fc geoJSON
		json.Unmarshal(w.Body.Bytes(), &fc)

		var trackFeature *geoFeature
		for _, f := range fc.Features {
			if f.Properties["type"] == "track" {
				trackFeature = &f
				break
			}
		}
		if trackFeature == nil {
			t.Fatal("no track line feature found")
		}
		if trackFeature.Geometry.Type != "LineString" {
			t.Fatalf("expected LineString geometry, got %s", trackFeature.Geometry.Type)
		}

		var coords [][]float64
		json.Unmarshal(trackFeature.Geometry.Coordinates, &coords)
		if len(coords) != 4 {
			t.Fatalf("expected 4 trackpoint coordinates, got %d", len(coords))
		}
		// GeoJSON is [lon, lat]
		if coords[0][0] != 6.9603 || coords[0][1] != 50.9375 {
			t.Fatalf("first coordinate wrong: got [%f, %f]", coords[0][0], coords[0][1])
		}
	})

	t.Run("contains entry marker features with required properties", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/t/"+viewToken+"/track", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		var fc geoJSON
		json.Unmarshal(w.Body.Bytes(), &fc)

		var entryFeatures []geoFeature
		for _, f := range fc.Features {
			if f.Properties["type"] == "entry" {
				entryFeatures = append(entryFeatures, f)
			}
		}

		// 2 entries with coords (the one without coords is skipped)
		if len(entryFeatures) != 2 {
			t.Fatalf("expected 2 entry features, got %d", len(entryFeatures))
		}

		for i, f := range entryFeatures {
			if f.Geometry.Type != "Point" {
				t.Errorf("entry %d: expected Point geometry, got %s", i, f.Geometry.Type)
			}
			// Must have id (used by JS for marker↔timeline sync)
			if _, ok := f.Properties["id"]; !ok {
				t.Errorf("entry %d: missing 'id' property", i)
			}
			// Must have timestamp
			if _, ok := f.Properties["timestamp"]; !ok {
				t.Errorf("entry %d: missing 'timestamp' property", i)
			}
		}
	})

	t.Run("entry coordinates are lon,lat order", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/t/"+viewToken+"/track", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		var fc geoJSON
		json.Unmarshal(w.Body.Bytes(), &fc)

		for _, f := range fc.Features {
			if f.Properties["type"] != "entry" {
				continue
			}
			var coords [2]float64
			json.Unmarshal(f.Geometry.Coordinates, &coords)
			lon, lat := coords[0], coords[1]
			// Sanity: lon should be ~6.x, lat should be ~50-51.x
			if lon < 6 || lon > 7 {
				t.Errorf("lon looks wrong (maybe swapped?): %f", lon)
			}
			if lat < 50 || lat > 52 {
				t.Errorf("lat looks wrong (maybe swapped?): %f", lat)
			}
		}
	})

	t.Run("entry photo property picks first non-video", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/t/"+viewToken+"/track", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		var fc geoJSON
		json.Unmarshal(w.Body.Bytes(), &fc)

		for _, f := range fc.Features {
			if f.Properties["type"] != "entry" {
				continue
			}
			photo, _ := f.Properties["photo"].(string)
			if photo != "" && (strings.HasSuffix(photo, ".mp4") ||
				strings.HasSuffix(photo, ".webm") ||
				strings.HasSuffix(photo, ".mov")) {
				t.Errorf("photo property should not be a video: %s", photo)
			}
		}
	})

	t.Run("invalid token returns 404", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/t/nonexistent/track", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", w.Code)
		}
	})
}

func TestPublicViewDataAttributes(t *testing.T) {
	srv, viewToken := setupTestServer(t)
	mux := srv.routes()

	req := httptest.NewRequest("GET", "/t/"+viewToken, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	body := w.Body.String()

	t.Run("timeline entries have data-entry-id", func(t *testing.T) {
		if !strings.Contains(body, "data-entry-id=") {
			t.Fatal("no data-entry-id attributes found in rendered HTML")
		}
	})

	t.Run("timeline entries have data-lat and data-lon", func(t *testing.T) {
		if !strings.Contains(body, "data-lat=") {
			t.Fatal("no data-lat attributes found")
		}
		if !strings.Contains(body, "data-lon=") {
			t.Fatal("no data-lon attributes found")
		}
	})

	t.Run("coordinates have 6 decimal places", func(t *testing.T) {
		// Check that lat/lon are formatted with precision
		if !strings.Contains(body, `data-lat="50.937500"`) {
			t.Errorf("expected data-lat with 6 decimal places, not found in body")
		}
		if !strings.Contains(body, `data-lon="6.960300"`) {
			t.Errorf("expected data-lon with 6 decimal places, not found in body")
		}
	})

	t.Run("mobile cards have data-dist", func(t *testing.T) {
		if !strings.Contains(body, "data-dist=") {
			t.Fatal("no data-dist attributes found for mobile cards")
		}
	})

	t.Run("share token appears in script", func(t *testing.T) {
		if !strings.Contains(body, viewToken) {
			t.Fatal("view token not found in rendered page (needed for JS fetch)")
		}
	})
}

func TestBuildGeoJSON(t *testing.T) {
	t.Run("empty inputs produce valid GeoJSON", func(t *testing.T) {
		result := buildGeoJSON(nil, nil)
		var fc geoJSON
		if err := json.Unmarshal([]byte(result), &fc); err != nil {
			t.Fatalf("invalid JSON from empty inputs: %v", err)
		}
		if fc.Type != "FeatureCollection" {
			t.Fatalf("expected FeatureCollection, got %s", fc.Type)
		}
		if len(fc.Features) != 0 {
			t.Fatalf("expected 0 features, got %d", len(fc.Features))
		}
	})

	t.Run("entry body with quotes is escaped", func(t *testing.T) {
		lat, lon := 50.0, 6.0
		body := `She said "hello"`
		entries := []Entry{{
			ID: 1, Body: &body, Lat: &lat, Lon: &lon,
			Timestamp: "2025-04-20T10:00:00Z",
		}}
		result := buildGeoJSON(nil, entries)
		var fc geoJSON
		if err := json.Unmarshal([]byte(result), &fc); err != nil {
			t.Fatalf("invalid JSON with quoted body: %v\nraw: %s", err, result)
		}
		got, _ := fc.Features[0].Properties["body"].(string)
		if !strings.Contains(got, `"hello"`) {
			t.Errorf("body quotes not preserved: %s", got)
		}
	})

	t.Run("entries without coordinates are skipped", func(t *testing.T) {
		body := "no location"
		entries := []Entry{
			{ID: 1, Body: &body, Lat: nil, Lon: nil, Timestamp: "2025-04-20T10:00:00Z"},
		}
		result := buildGeoJSON(nil, entries)
		var fc geoJSON
		json.Unmarshal([]byte(result), &fc)
		if len(fc.Features) != 0 {
			t.Fatalf("expected 0 features for entry without coords, got %d", len(fc.Features))
		}
	})
}

func TestTrackEndpointOrdering(t *testing.T) {
	srv, viewToken := setupTestServer(t)
	mux := srv.routes()

	req := httptest.NewRequest("GET", "/t/"+viewToken+"/track", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var fc geoJSON
	json.Unmarshal(w.Body.Bytes(), &fc)

	// Find track line and verify coordinates are in chronological order
	for _, f := range fc.Features {
		if f.Properties["type"] != "track" {
			continue
		}
		var coords [][]float64
		json.Unmarshal(f.Geometry.Coordinates, &coords)

		// First point should be the southern one (Cologne ~50.9)
		// Last point should be the northern one (Düsseldorf ~51.2)
		if coords[0][1] > coords[len(coords)-1][1] {
			t.Error("trackpoints not in chronological (south→north) order")
		}
	}

	// Verify entry markers are ordered chronologically
	var entryTimestamps []string
	for _, f := range fc.Features {
		if f.Properties["type"] == "entry" {
			ts, _ := f.Properties["timestamp"].(string)
			entryTimestamps = append(entryTimestamps, ts)
		}
	}
	for i := 1; i < len(entryTimestamps); i++ {
		if entryTimestamps[i] < entryTimestamps[i-1] {
			t.Errorf("entries not in chronological order: %s before %s",
				entryTimestamps[i-1], entryTimestamps[i])
		}
	}
}
