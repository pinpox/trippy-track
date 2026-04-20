package main

import (
	"bytes"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
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
}

func newServer(db *sql.DB, addr string) (*Server, error) {
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

	return &Server{db: db, tmpl: tmpl, addr: addr, sse: newSSEBroker()}, nil
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	// Static files
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	mux.Handle("GET /uploads/", http.StripPrefix("/uploads/", http.FileServer(http.Dir("uploads"))))

	// Landing page
	mux.HandleFunc("GET /", s.handleIndex)
	mux.HandleFunc("POST /trips", s.handleCreateTrip)

	// Admin (secret admin token)
	mux.HandleFunc("GET /admin/{token}", s.handleAdmin)
	mux.HandleFunc("POST /admin/{token}/entries", s.handleCreateEntry)

	// OsmAnd tracking
	mux.HandleFunc("GET /api/track", s.handleTrack)

	// Public view
	mux.HandleFunc("GET /t/{shareToken}", s.handlePublicView)
	mux.HandleFunc("GET /t/{shareToken}/track", s.handlePublicTrack)
	mux.HandleFunc("GET /t/{shareToken}/entries", s.handlePublicEntries)
	mux.HandleFunc("GET /t/{shareToken}/sse", s.handleSSE)

	return mux
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	trips, err := listTrips(s.db)
	if err != nil {
		http.Error(w, "failed to list trips", http.StatusInternalServerError)
		log.Printf("list trips: %v", err)
		return
	}

	s.tmpl.ExecuteTemplate(w, "index.html", map[string]any{
		"Trips": trips,
	})
}

func (s *Server) handleCreateTrip(w http.ResponseWriter, r *http.Request) {
	name := r.FormValue("name")
	if name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	trip, err := createTrip(s.db, name)
	if err != nil {
		http.Error(w, "failed to create trip", http.StatusInternalServerError)
		log.Printf("create trip: %v", err)
		return
	}

	http.Redirect(w, r, "/admin/"+trip.AdminToken, http.StatusSeeOther)
}

func (s *Server) handleAdmin(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	trip, err := getTripByAdminToken(s.db, token)
	if err != nil {
		http.NotFound(w, r)
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
	osmandURL := fmt.Sprintf(
		"%s://%s/api/track?token=%s&lat={0}&lon={1}&timestamp={2}&speed={3}&bearing={4}&altitude={5}&hdop={6}",
		scheme, r.Host, trip.AdminToken,
	)

	entries, err := getEntries(s.db, trip.ID)
	if err != nil {
		http.Error(w, "failed to get entries", http.StatusInternalServerError)
		log.Printf("get entries: %v", err)
		return
	}

	s.tmpl.ExecuteTemplate(w, "admin.html", map[string]any{
		"Trip":        trip,
		"Trackpoints": trackpoints,
		"Entries":     entries,
		"OsmandURL":   osmandURL,
		"ShareURL":    fmt.Sprintf("%s://%s/t/%s", scheme, r.Host, trip.ShareToken),
	})
}

func (s *Server) handleTrack(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "token required", http.StatusBadRequest)
		return
	}

	trip, err := getTripByAdminToken(s.db, token)
	if err != nil {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	latStr := r.URL.Query().Get("lat")
	lonStr := r.URL.Query().Get("lon")
	tsStr := r.URL.Query().Get("timestamp")

	if latStr == "" || lonStr == "" || tsStr == "" {
		http.Error(w, "lat, lon, timestamp required", http.StatusBadRequest)
		return
	}

	lat, err := strconv.ParseFloat(latStr, 64)
	if err != nil {
		http.Error(w, "invalid lat", http.StatusBadRequest)
		return
	}
	lon, err := strconv.ParseFloat(lonStr, 64)
	if err != nil {
		http.Error(w, "invalid lon", http.StatusBadRequest)
		return
	}

	// OsmAnd sends timestamp as Unix epoch in milliseconds
	tsMs, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid timestamp", http.StatusBadRequest)
		return
	}
	ts := time.UnixMilli(tsMs)

	tp := Trackpoint{
		TripID:    trip.ID,
		Lat:       lat,
		Lon:       lon,
		Timestamp: ts,
	}

	// Optional fields
	if v := r.URL.Query().Get("altitude"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			tp.Altitude = &f
		}
	}
	if v := r.URL.Query().Get("speed"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			tp.Speed = &f
		}
	}
	if v := r.URL.Query().Get("bearing"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			tp.Bearing = &f
		}
	}
	if v := r.URL.Query().Get("hdop"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			tp.HDOP = &f
		}
	}

	if err := insertTrackpoint(s.db, tp); err != nil {
		http.Error(w, "failed to insert trackpoint", http.StatusInternalServerError)
		log.Printf("insert trackpoint: %v", err)
		return
	}

	log.Printf("trackpoint: trip=%s lat=%.6f lon=%.6f ts=%s", trip.ID, lat, lon, ts.Format(time.RFC3339))

	s.sse.Publish(trip.ID, SSEEvent{
		Type: EventTrackpoint,
		Data: fmt.Sprintf(`{"lat":%.6f,"lon":%.6f}`, lat, lon),
	})

	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleCreateEntry(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	trip, err := getTripByAdminToken(s.db, token)
	if err != nil {
		http.NotFound(w, r)
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

	entryID, err := createEntry(s.db, trip.ID, body, lat, lon, nil, entryTS)
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

	http.Redirect(w, r, "/admin/"+token, http.StatusSeeOther)
}

func (s *Server) handlePublicView(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("shareToken")
	trip, err := getTripByShareToken(s.db, token)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	entries, err := getEntries(s.db, trip.ID)
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

	s.tmpl.ExecuteTemplate(w, "public.html", map[string]any{
		"Trip":     trip,
		"Timeline": timeline,
	})
}

func (s *Server) handlePublicTrack(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("shareToken")
	trip, err := getTripByShareToken(s.db, token)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	trackpoints, err := getTrackpoints(s.db, trip.ID)
	if err != nil {
		http.Error(w, "failed to get trackpoints", http.StatusInternalServerError)
		return
	}

	entries, err := getEntries(s.db, trip.ID)
	if err != nil {
		http.Error(w, "failed to get entries", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/geo+json")
	fmt.Fprint(w, buildGeoJSON(trackpoints, entries))
}

func (s *Server) handlePublicEntries(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("shareToken")
	trip, err := getTripByShareToken(s.db, token)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	entries, err := getEntries(s.db, trip.ID)
	if err != nil {
		http.Error(w, "failed to get entries", http.StatusInternalServerError)
		return
	}

	for _, e := range entries {
		s.tmpl.ExecuteTemplate(w, "entry", e)
	}
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("shareToken")
	trip, err := getTripByShareToken(s.db, token)
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
		features = append(features, fmt.Sprintf(
			`{"type":"Feature","geometry":{"type":"Point","coordinates":[%.6f,%.6f]},"properties":{"type":"entry","id":%d,"body":"%s","timestamp":"%s"}}`,
			*e.Lon, *e.Lat, e.ID, body, e.Timestamp,
		))
	}

	return fmt.Sprintf(`{"type":"FeatureCollection","features":[%s]}`, strings.Join(features, ","))
}
