package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"
)

const schema = `
CREATE TABLE IF NOT EXISTS trips (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    share_token TEXT NOT NULL UNIQUE,
    admin_token TEXT NOT NULL UNIQUE,
    is_active   INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE IF NOT EXISTS trackpoints (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    trip_id   TEXT NOT NULL REFERENCES trips(id),
    lat       REAL NOT NULL,
    lon       REAL NOT NULL,
    altitude  REAL,
    speed     REAL,
    bearing   REAL,
    hdop      REAL,
    timestamp TEXT NOT NULL,
    UNIQUE(trip_id, timestamp)
);

CREATE INDEX IF NOT EXISTS idx_trackpoints_trip_ts ON trackpoints(trip_id, timestamp);

CREATE TABLE IF NOT EXISTS entries (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    trip_id       TEXT NOT NULL REFERENCES trips(id),
    body          TEXT,
    lat           REAL,
    lon           REAL,
    location_name TEXT,
    timestamp     TEXT NOT NULL,
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_entries_trip_ts ON entries(trip_id, timestamp);

CREATE TABLE IF NOT EXISTS photos (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    entry_id   INTEGER NOT NULL REFERENCES entries(id),
    file_path  TEXT NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0
);
`

func initDB(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	if _, err := db.Exec("PRAGMA mmap_size=0"); err != nil {
		return nil, fmt.Errorf("disable mmap: %w", err)
	}
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return nil, fmt.Errorf("set WAL mode: %w", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("run schema: %w", err)
	}

	return db, nil
}

func randomToken(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

type Trip struct {
	ID         string
	Name       string
	CreatedAt  string
	ShareToken string
	AdminToken string
	IsActive   bool
}

type Trackpoint struct {
	TripID    string
	Lat       float64
	Lon       float64
	Altitude  *float64
	Speed     *float64
	Bearing   *float64
	HDOP      *float64
	Timestamp time.Time
}

type Entry struct {
	ID           int64
	TripID       string
	Body         *string
	Lat          *float64
	Lon          *float64
	LocationName *string
	Timestamp    string
	CreatedAt    string
	Photos       []Photo
}

type Photo struct {
	ID        int64
	EntryID   int64
	FilePath  string
	SortOrder int
}

func createTrip(db *sql.DB, name string) (*Trip, error) {
	id := randomToken(8)
	shareToken := randomToken(16)
	adminToken := randomToken(16)

	_, err := db.Exec(
		"INSERT INTO trips (id, name, share_token, admin_token) VALUES (?, ?, ?, ?)",
		id, name, shareToken, adminToken,
	)
	if err != nil {
		return nil, fmt.Errorf("insert trip: %w", err)
	}

	return &Trip{
		ID:         id,
		Name:       name,
		ShareToken: shareToken,
		AdminToken: adminToken,
		IsActive:   true,
	}, nil
}

func getTripByAdminToken(db *sql.DB, token string) (*Trip, error) {
	t := &Trip{}
	err := db.QueryRow(
		"SELECT id, name, created_at, share_token, admin_token, is_active FROM trips WHERE admin_token = ?",
		token,
	).Scan(&t.ID, &t.Name, &t.CreatedAt, &t.ShareToken, &t.AdminToken, &t.IsActive)
	if err != nil {
		return nil, err
	}
	return t, nil
}

func getTripByShareToken(db *sql.DB, token string) (*Trip, error) {
	t := &Trip{}
	err := db.QueryRow(
		"SELECT id, name, created_at, share_token, admin_token, is_active FROM trips WHERE share_token = ?",
		token,
	).Scan(&t.ID, &t.Name, &t.CreatedAt, &t.ShareToken, &t.AdminToken, &t.IsActive)
	if err != nil {
		return nil, err
	}
	return t, nil
}

func listTrips(db *sql.DB) ([]Trip, error) {
	rows, err := db.Query("SELECT id, name, created_at, share_token, admin_token, is_active FROM trips ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var trips []Trip
	for rows.Next() {
		var t Trip
		if err := rows.Scan(&t.ID, &t.Name, &t.CreatedAt, &t.ShareToken, &t.AdminToken, &t.IsActive); err != nil {
			return nil, err
		}
		trips = append(trips, t)
	}
	return trips, rows.Err()
}

func insertTrackpoint(db *sql.DB, tp Trackpoint) error {
	_, err := db.Exec(
		`INSERT OR IGNORE INTO trackpoints (trip_id, lat, lon, altitude, speed, bearing, hdop, timestamp)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		tp.TripID, tp.Lat, tp.Lon, tp.Altitude, tp.Speed, tp.Bearing, tp.HDOP,
		tp.Timestamp.UTC().Format(time.RFC3339),
	)
	return err
}

func getTrackpoints(db *sql.DB, tripID string) ([]Trackpoint, error) {
	rows, err := db.Query(
		"SELECT trip_id, lat, lon, altitude, speed, bearing, hdop, timestamp FROM trackpoints WHERE trip_id = ? ORDER BY timestamp ASC",
		tripID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var points []Trackpoint
	for rows.Next() {
		var tp Trackpoint
		var ts string
		if err := rows.Scan(&tp.TripID, &tp.Lat, &tp.Lon, &tp.Altitude, &tp.Speed, &tp.Bearing, &tp.HDOP, &ts); err != nil {
			return nil, err
		}
		tp.Timestamp, _ = time.Parse(time.RFC3339, ts)
		points = append(points, tp)
	}
	return points, rows.Err()
}

func createEntry(db *sql.DB, tripID, body string, lat, lon *float64, locationName *string, timestamp string) (int64, error) {
	res, err := db.Exec(
		"INSERT INTO entries (trip_id, body, lat, lon, location_name, timestamp) VALUES (?, ?, ?, ?, ?, ?)",
		tripID, body, lat, lon, locationName, timestamp,
	)
	if err != nil {
		return 0, fmt.Errorf("insert entry: %w", err)
	}
	return res.LastInsertId()
}

func addPhoto(db *sql.DB, entryID int64, filePath string, sortOrder int) error {
	_, err := db.Exec(
		"INSERT INTO photos (entry_id, file_path, sort_order) VALUES (?, ?, ?)",
		entryID, filePath, sortOrder,
	)
	return err
}

func getEntries(db *sql.DB, tripID string) ([]Entry, error) {
	rows, err := db.Query(
		"SELECT id, trip_id, body, lat, lon, location_name, timestamp, created_at FROM entries WHERE trip_id = ? ORDER BY timestamp ASC",
		tripID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []Entry
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.TripID, &e.Body, &e.Lat, &e.Lon, &e.LocationName, &e.Timestamp, &e.CreatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Load photos for each entry
	for i := range entries {
		photos, err := getPhotos(db, entries[i].ID)
		if err != nil {
			return nil, err
		}
		entries[i].Photos = photos
	}

	return entries, nil
}

func getPhotos(db *sql.DB, entryID int64) ([]Photo, error) {
	rows, err := db.Query(
		"SELECT id, entry_id, file_path, sort_order FROM photos WHERE entry_id = ? ORDER BY sort_order ASC",
		entryID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var photos []Photo
	for rows.Next() {
		var p Photo
		if err := rows.Scan(&p.ID, &p.EntryID, &p.FilePath, &p.SortOrder); err != nil {
			return nil, err
		}
		photos = append(photos, p)
	}
	return photos, rows.Err()
}
