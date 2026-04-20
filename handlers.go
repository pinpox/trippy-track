package main

import (
	"bytes"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Server struct {
	db   *sql.DB
	tmpl *template.Template
	addr string
	sse  *SSEBroker
	auth *AuthService
}

func newServer(db *sql.DB, addr string, auth *AuthService) (*Server, error) {
	funcMap := template.FuncMap{
		"deref": func(f *float64) float64 {
			if f == nil {
				return 0
			}
			return *f
		},
		"formatTime": func(ts string) string {
			t, err := time.Parse(time.RFC3339, ts)
			if err != nil {
				return ts
			}
			return t.Format("Jan 2, 2006 · 15:04")
		},
		"formatDate": func(ts string) string {
			t, err := time.Parse(time.RFC3339, ts)
			if err != nil {
				return ts
			}
			return t.Format("2. January 2006")
		},
		"formatClock": func(ts string) string {
			t, err := time.Parse(time.RFC3339, ts)
			if err != nil {
				return ""
			}
			return t.Format("15:04")
		},
		"formatDist": func(km float64) string {
			if km < 1 {
				return fmt.Sprintf("%d m", int(km*1000))
			}
			return fmt.Sprintf("%.1f km", km)
		},
		"isVideo": func(path string) bool {
			ext := strings.ToLower(filepath.Ext(path))
			return ext == ".mp4" || ext == ".webm" || ext == ".mov" || ext == ".avi"
		},
		"weatherIcon": func(code *int) string {
			if code == nil {
				return ""
			}
			return WeatherIcon(*code)
		},
		"weatherLabel": func(code *int) string {
			if code == nil {
				return ""
			}
			return WeatherLabel(*code)
		},
		"countryFlag": func(code *string) string {
			if code == nil {
				return ""
			}
			return CountryFlag(*code)
		},
		"countryName": func(code *string) string {
			if code == nil {
				return ""
			}
			return CountryName(*code)
		},
		"toDatetimeLocal": func(ts string) string {
			t, err := time.Parse(time.RFC3339, ts)
			if err != nil {
				return ts
			}
			return t.Format("2006-01-02T15:04")
		},
		"sub": func(a, b int) int {
			return a - b
		},
		"index": func(s []float64, i int) float64 {
			if i < 0 || i >= len(s) {
				return 0
			}
			return s[i]
		},
	}

	tmpl := template.Must(template.New("").Funcs(funcMap).ParseGlob("templates/*.html"))
	template.Must(tmpl.ParseGlob("templates/partials/*.html"))

	return &Server{db: db, tmpl: tmpl, addr: addr, sse: newSSEBroker(), auth: auth}, nil
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	// Static files (no auth)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	mux.Handle("GET /uploads/", http.StripPrefix("/uploads/", http.FileServer(http.Dir("uploads"))))

	// Auth routes (no auth)
	mux.HandleFunc("GET /login", s.handleLoginPage)
	mux.HandleFunc("GET /login/start", s.handleLoginStart)
	mux.HandleFunc("GET /callback", s.handleCallback)
	mux.HandleFunc("GET /logout", s.handleLogout)

	// Public view (no auth)
	mux.HandleFunc("GET /t/{token}", s.handlePublicView)
	mux.HandleFunc("GET /t/{token}/track", s.handlePublicTrack)
	mux.HandleFunc("GET /t/{token}/entries", s.handlePublicEntries)
	mux.HandleFunc("GET /t/{token}/sse", s.handleSSE)

	// OwnTracks tracking (no auth, token in query param)
	mux.HandleFunc("POST /api/track", s.handleTrack)

	// Protected routes (require login)
	mux.HandleFunc("GET /", s.handleIndex)
	mux.HandleFunc("POST /trips", s.handleCreateTrip)
	mux.HandleFunc("GET /t/{token}/admin", s.handleAdmin)
	mux.HandleFunc("POST /t/{token}/admin/entries", s.handleCreateEntry)
	mux.HandleFunc("POST /t/{token}/admin/entries/{entryID}/photos", s.handleAddPhotos)
	mux.HandleFunc("POST /t/{token}/admin/entries/{entryID}/update", s.handleUpdateEntry)
	mux.HandleFunc("POST /t/{token}/admin/entries/{entryID}/delete", s.handleDeleteEntry)
	mux.HandleFunc("POST /t/{token}/admin/photos/{photoID}/delete", s.handleDeletePhoto)

	return s.auth.AuthMiddleware(mux)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	user := GetUserFromContext(r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	trips, err := listTripsByUser(s.db, user.ID)
	if err != nil {
		http.Error(w, "failed to list trips", http.StatusInternalServerError)
		log.Printf("list trips: %v", err)
		return
	}

	s.tmpl.ExecuteTemplate(w, "index.html", map[string]any{
		"Trips": trips,
		"User":  user,
	})
}

func (s *Server) handleCreateTrip(w http.ResponseWriter, r *http.Request) {
	user := GetUserFromContext(r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	name := r.FormValue("name")
	if name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	trip, err := createTrip(s.db, name, user.ID)
	if err != nil {
		http.Error(w, "failed to create trip", http.StatusInternalServerError)
		log.Printf("create trip: %v", err)
		return
	}

	http.Redirect(w, r, "/t/"+trip.ViewToken+"/admin", http.StatusSeeOther)
}

func (s *Server) verifyTripOwnership(w http.ResponseWriter, r *http.Request, trip *Trip) bool {
	user := GetUserFromContext(r)
	if user == nil || trip.UserID == nil || *trip.UserID != user.ID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return false
	}
	return true
}

func (s *Server) handleAdmin(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	trip, err := getTripByViewToken(s.db, token)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if !s.verifyTripOwnership(w, r, trip) {
		return
	}

	trackpoints, err := getTrackpoints(s.db, trip.ID)
	if err != nil {
		http.Error(w, "failed to get trackpoints", http.StatusInternalServerError)
		log.Printf("get trackpoints: %v", err)
		return
	}

	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if fwd := r.Header.Get("X-Forwarded-Proto"); fwd != "" {
		scheme = fwd
	}
	trackingURL := fmt.Sprintf(
		"%s://%s/api/track?token=%s",
		scheme, r.Host, trip.TrackingToken,
	)

	entries, err := getEntries(s.db, trip.ID, true)
	if err != nil {
		http.Error(w, "failed to get entries", http.StatusInternalServerError)
		log.Printf("get entries: %v", err)
		return
	}

	s.tmpl.ExecuteTemplate(w, "admin.html", map[string]any{
		"Trip":        trip,
		"Trackpoints": trackpoints,
		"Entries":     entries,
		"TrackingURL": trackingURL,
		"ShareURL":    fmt.Sprintf("%s://%s/t/%s", scheme, r.Host, trip.ViewToken),
	})
}

func (s *Server) handleTrack(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "token required", http.StatusBadRequest)
		return
	}

	trip, err := getTripByTrackingToken(s.db, token)
	if err != nil {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	// OwnTracks sends JSON POST
	var payload struct {
		Type string  `json:"_type"`
		Lat  float64 `json:"lat"`
		Lon  float64 `json:"lon"`
		Tst  int64   `json:"tst"` // Unix epoch seconds
		Alt  float64 `json:"alt"`
		Vel  int     `json:"vel"` // velocity km/h
		Batt int     `json:"batt"`
		Acc  int     `json:"acc"` // accuracy meters
		Cog  int     `json:"cog"` // course over ground (bearing)
	}

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	// Only process location messages
	if payload.Type != "location" {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("[]"))
		return
	}

	ts := time.Unix(payload.Tst, 0)

	tp := Trackpoint{
		TripID:    trip.ID,
		Lat:       payload.Lat,
		Lon:       payload.Lon,
		Timestamp: ts,
	}

	if payload.Alt != 0 {
		alt := payload.Alt
		tp.Altitude = &alt
	}
	if payload.Vel != 0 {
		speed := float64(payload.Vel)
		tp.Speed = &speed
	}
	if payload.Cog != 0 {
		bearing := float64(payload.Cog)
		tp.Bearing = &bearing
	}
	if payload.Acc != 0 {
		hdop := float64(payload.Acc)
		tp.HDOP = &hdop
	}

	if err := insertTrackpoint(s.db, tp); err != nil {
		http.Error(w, "failed to insert trackpoint", http.StatusInternalServerError)
		log.Printf("insert trackpoint: %v", err)
		return
	}

	log.Printf("trackpoint: trip=%s lat=%.6f lon=%.6f ts=%s", trip.ID, payload.Lat, payload.Lon, ts.Format(time.RFC3339))

	s.sse.Publish(trip.ID, SSEEvent{
		Type: EventTrackpoint,
		Data: fmt.Sprintf(`{"lat":%.6f,"lon":%.6f}`, payload.Lat, payload.Lon),
	})

	// OwnTracks expects a JSON array response
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte("[]"))
}

func (s *Server) handleCreateEntry(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	trip, err := getTripByViewToken(s.db, token)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if !s.verifyTripOwnership(w, r, trip) {
		return
	}

	// 32MB max upload
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "failed to parse form", http.StatusBadRequest)
		return
	}

	body := strings.TrimSpace(r.FormValue("body"))
	tsOverride := r.FormValue("timestamp")

	// Process uploaded photos
	var photoFiles []string
	var firstExif *ExifData

	files := r.MultipartForm.File["photos"]
	for i, fh := range files {
		f, err := fh.Open()
		if err != nil {
			log.Printf("open upload: %v", err)
			continue
		}

		// Read file into memory for EXIF extraction + saving
		data, err := io.ReadAll(f)
		f.Close()
		if err != nil {
			log.Printf("read upload: %v", err)
			continue
		}

		// Extract EXIF from first photo
		if i == 0 {
			firstExif, _ = extractExif(bytes.NewReader(data))
		}

		// Save file with random name
		ext := filepath.Ext(fh.Filename)
		if ext == "" {
			ext = ".jpg"
		}
		b := make([]byte, 16)
		rand.Read(b)
		filename := hex.EncodeToString(b) + ext

		if err := os.WriteFile(filepath.Join("uploads", filename), data, 0o644); err != nil {
			log.Printf("save upload: %v", err)
			continue
		}

		photoFiles = append(photoFiles, filename)
	}

	// Determine timestamp
	var entryTS string
	if tsOverride != "" {
		entryTS = tsOverride
	} else if firstExif != nil && firstExif.Timestamp != nil {
		entryTS = firstExif.Timestamp.UTC().Format(time.RFC3339)
	} else {
		entryTS = time.Now().UTC().Format(time.RFC3339)
	}

	// Determine location
	var lat, lon *float64
	if firstExif != nil && firstExif.Lat != nil && firstExif.Lon != nil {
		lat = firstExif.Lat
		lon = firstExif.Lon
	} else {
		// Try to interpolate from track
		ts, err := time.Parse(time.RFC3339, entryTS)
		if err == nil {
			if iLat, iLon, ok := InterpolatePosition(s.db, trip.ID, ts); ok {
				lat = &iLat
				lon = &iLon
			}
		}
	}

	// Allow manual override
	if v := r.FormValue("lat"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			lat = &f
		}
	}
	if v := r.FormValue("lon"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			lon = &f
		}
	}

	// Fetch weather and country data
	var weatherCode *int
	var temperature *float64
	var countryCode *string
	if lat != nil && lon != nil {
		ts, err := time.Parse(time.RFC3339, entryTS)
		if err == nil {
			w, err := fetchWeather(*lat, *lon, ts)
			if err != nil {
				log.Printf("fetch weather: %v", err)
			} else {
				weatherCode = &w.Code
				temperature = &w.Temperature
				log.Printf("weather: code=%d temp=%.1f", w.Code, w.Temperature)
			}
		}
		cc, err := fetchCountryCode(*lat, *lon)
		if err != nil {
			log.Printf("fetch country: %v", err)
		} else {
			countryCode = &cc
			log.Printf("country: %s", cc)
		}
	}

	entryID, err := createEntry(s.db, trip.ID, body, lat, lon, nil, entryTS, weatherCode, temperature, countryCode)
	if err != nil {
		http.Error(w, "failed to create entry", http.StatusInternalServerError)
		log.Printf("create entry: %v", err)
		return
	}

	for i, filename := range photoFiles {
		if err := addPhoto(s.db, entryID, filename, i); err != nil {
			log.Printf("add photo: %v", err)
		}
	}

	log.Printf("entry: trip=%s id=%d photos=%d", trip.ID, entryID, len(photoFiles))

	// Publish SSE event with entry data
	s.sse.Publish(trip.ID, SSEEvent{
		Type: EventEntry,
		Data: "reload",
	})

	http.Redirect(w, r, "/t/"+token+"/admin", http.StatusSeeOther)
}

func (s *Server) handleUpdateEntry(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	trip, err := getTripByViewToken(s.db, token)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if !s.verifyTripOwnership(w, r, trip) {
		return
	}

	entryID, err := strconv.ParseInt(r.PathValue("entryID"), 10, 64)
	if err != nil {
		http.Error(w, "invalid entry ID", http.StatusBadRequest)
		return
	}

	tripID, err := getEntryTripID(s.db, entryID)
	if err != nil || tripID != trip.ID {
		http.Error(w, "entry not found", http.StatusNotFound)
		return
	}

	body := r.FormValue("body")
	timestamp := r.FormValue("timestamp")
	if timestamp != "" {
		// Convert datetime-local format to RFC3339
		if t, err := time.Parse("2006-01-02T15:04", timestamp); err == nil {
			timestamp = t.UTC().Format(time.RFC3339)
		}
	} else {
		timestamp = time.Now().UTC().Format(time.RFC3339)
	}
	if err := updateEntry(s.db, entryID, body, timestamp); err != nil {
		http.Error(w, "failed to update entry", http.StatusInternalServerError)
		log.Printf("update entry: %v", err)
		return
	}

	http.Redirect(w, r, "/t/"+token+"/admin", http.StatusSeeOther)
}

func (s *Server) handleDeleteEntry(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	trip, err := getTripByViewToken(s.db, token)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if !s.verifyTripOwnership(w, r, trip) {
		return
	}

	entryID, err := strconv.ParseInt(r.PathValue("entryID"), 10, 64)
	if err != nil {
		http.Error(w, "invalid entry ID", http.StatusBadRequest)
		return
	}

	tripID, err := getEntryTripID(s.db, entryID)
	if err != nil || tripID != trip.ID {
		http.Error(w, "entry not found", http.StatusNotFound)
		return
	}

	if err := deleteEntry(s.db, entryID); err != nil {
		http.Error(w, "failed to delete entry", http.StatusInternalServerError)
		log.Printf("delete entry: %v", err)
		return
	}

	http.Redirect(w, r, "/t/"+token+"/admin", http.StatusSeeOther)
}

func (s *Server) handleDeletePhoto(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	trip, err := getTripByViewToken(s.db, token)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if !s.verifyTripOwnership(w, r, trip) {
		return
	}

	photoID, err := strconv.ParseInt(r.PathValue("photoID"), 10, 64)
	if err != nil {
		http.Error(w, "invalid photo ID", http.StatusBadRequest)
		return
	}

	// Verify photo belongs to this trip
	if !photobelongsToTrip(s.db, photoID, trip.ID) {
		http.Error(w, "photo not found", http.StatusNotFound)
		return
	}

	filePath, err := deletePhoto(s.db, photoID)
	if err != nil {
		http.Error(w, "failed to delete photo", http.StatusInternalServerError)
		log.Printf("delete photo: %v", err)
		return
	}

	// Remove file from disk
	os.Remove(filepath.Join("uploads", filePath))

	http.Redirect(w, r, "/t/"+token+"/admin", http.StatusSeeOther)
}

func (s *Server) handleAddPhotos(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	trip, err := getTripByViewToken(s.db, token)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if !s.verifyTripOwnership(w, r, trip) {
		return
	}

	entryID, err := strconv.ParseInt(r.PathValue("entryID"), 10, 64)
	if err != nil {
		http.Error(w, "invalid entry ID", http.StatusBadRequest)
		return
	}

	// Verify entry belongs to this trip
	tripID, err := getEntryTripID(s.db, entryID)
	if err != nil || tripID != trip.ID {
		http.Error(w, "entry not found", http.StatusNotFound)
		return
	}

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "failed to parse form", http.StatusBadRequest)
		return
	}

	order := maxPhotoOrder(s.db, entryID) + 1

	files := r.MultipartForm.File["photos"]
	for _, fh := range files {
		f, err := fh.Open()
		if err != nil {
			log.Printf("open upload: %v", err)
			continue
		}
		data, err := io.ReadAll(f)
		f.Close()
		if err != nil {
			log.Printf("read upload: %v", err)
			continue
		}

		ext := filepath.Ext(fh.Filename)
		if ext == "" {
			ext = ".jpg"
		}
		b := make([]byte, 16)
		rand.Read(b)
		filename := hex.EncodeToString(b) + ext

		if err := os.WriteFile(filepath.Join("uploads", filename), data, 0o644); err != nil {
			log.Printf("save upload: %v", err)
			continue
		}

		if err := addPhoto(s.db, entryID, filename, order); err != nil {
			log.Printf("add photo: %v", err)
		}
		order++
	}

	http.Redirect(w, r, "/t/"+token+"/admin", http.StatusSeeOther)
}

func (s *Server) handlePublicView(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	trip, err := getTripByViewToken(s.db, token)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	entries, err := getEntries(s.db, trip.ID, false)
	if err != nil {
		http.Error(w, "failed to get entries", http.StatusInternalServerError)
		return
	}

	distances := DistanceBetweenEntries(entries)

	type TimelineEntry struct {
		Entry      Entry
		DistFromPrev float64
	}
	var timeline []TimelineEntry
	for i, e := range entries {
		timeline = append(timeline, TimelineEntry{
			Entry:        e,
			DistFromPrev: distances[i],
		})
	}

	lastPoint, _ := getLatestTrackpoint(s.db, trip.ID)

	trackpoints, err := getTrackpoints(s.db, trip.ID)
	if err != nil {
		trackpoints = nil
	}
	stats := ComputeStats(trackpoints, len(entries), trip.IsActive)

	s.tmpl.ExecuteTemplate(w, "public.html", map[string]any{
		"Trip":      trip,
		"Timeline":  timeline,
		"LastPoint": lastPoint,
		"Stats":     stats,
	})
}

func (s *Server) handlePublicTrack(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	trip, err := getTripByViewToken(s.db, token)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	trackpoints, err := getTrackpoints(s.db, trip.ID)
	if err != nil {
		http.Error(w, "failed to get trackpoints", http.StatusInternalServerError)
		return
	}

	entries, err := getEntries(s.db, trip.ID, false)
	if err != nil {
		http.Error(w, "failed to get entries", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/geo+json")
	fmt.Fprint(w, buildGeoJSON(trackpoints, entries))
}

func (s *Server) handlePublicEntries(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	trip, err := getTripByViewToken(s.db, token)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	entries, err := getEntries(s.db, trip.ID, false)
	if err != nil {
		http.Error(w, "failed to get entries", http.StatusInternalServerError)
		return
	}

	for _, e := range entries {
		s.tmpl.ExecuteTemplate(w, "entry", e)
	}
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	trip, err := getTripByViewToken(s.db, token)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	ch := s.sse.Subscribe(trip.ID)
	defer s.sse.Unsubscribe(trip.ID, ch)

	// Send initial keepalive
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-ch:
			fmt.Fprint(w, formatSSE(event.Type, event.Data))
			flusher.Flush()
		}
	}
}

func buildGeoJSON(points []Trackpoint, entries []Entry) string {
	var features []string

	// Track line
	if len(points) > 0 {
		coords := ""
		for i, p := range points {
			if i > 0 {
				coords += ","
			}
			coords += fmt.Sprintf("[%.6f,%.6f]", p.Lon, p.Lat)
		}
		features = append(features, fmt.Sprintf(
			`{"type":"Feature","geometry":{"type":"LineString","coordinates":[%s]},"properties":{"type":"track"}}`,
			coords,
		))
	}

	// Entry markers
	for _, e := range entries {
		if e.Lat == nil || e.Lon == nil {
			continue
		}
		body := ""
		if e.Body != nil {
			body = strings.ReplaceAll(*e.Body, `"`, `\"`)
		}
		photo := ""
		for _, p := range e.Photos {
			if !strings.HasSuffix(strings.ToLower(p.FilePath), ".mp4") &&
				!strings.HasSuffix(strings.ToLower(p.FilePath), ".webm") &&
				!strings.HasSuffix(strings.ToLower(p.FilePath), ".mov") {
				photo = p.FilePath
				break
			}
		}
		features = append(features, fmt.Sprintf(
			`{"type":"Feature","geometry":{"type":"Point","coordinates":[%.6f,%.6f]},"properties":{"type":"entry","id":%d,"body":"%s","timestamp":"%s","photo":"%s"}}`,
			*e.Lon, *e.Lat, e.ID, body, e.Timestamp, photo,
		))
	}

	return fmt.Sprintf(`{"type":"FeatureCollection","features":[%s]}`, strings.Join(features, ","))
}
