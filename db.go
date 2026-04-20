package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"
)

const schema = `
CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    oidc_subject TEXT UNIQUE NOT NULL,
    email TEXT NOT NULL,
    name TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    last_login_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE TABLE IF NOT EXISTS trips (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    token TEXT NOT NULL UNIQUE,
    is_active   INTEGER NOT NULL DEFAULT 1,
    user_id     INTEGER REFERENCES users(id)
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
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    weather_code  INTEGER,
    temperature   REAL,
    country_code  TEXT
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
	Token string
	IsActive   bool
	UserID     *int
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
	WeatherCode  *int
	Temperature  *float64
	CountryCode  *string
	Photos       []Photo
}

type Photo struct {
	ID        int64
	EntryID   int64
	FilePath  string
	SortOrder int
}

func createTrip(db *sql.DB, name string, userID int) (*Trip, error) {
	id := randomToken(8)
	token := randomToken(16)

	_, err := db.Exec(
		"INSERT INTO trips (id, name, token, user_id) VALUES (?, ?, ?, ?)",
		id, name, token, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("insert trip: %w", err)
	}

	return &Trip{
		ID:         id,
		Name:       name,
		Token: token,
		IsActive:   true,
		UserID:     &userID,
	}, nil
}

func getTripByToken(db *sql.DB, token string) (*Trip, error) {
	t := &Trip{}
	err := db.QueryRow(
		"SELECT id, name, created_at, token, is_active, user_id FROM trips WHERE token = ?",
		token,
	).Scan(&t.ID, &t.Name, &t.CreatedAt, &t.Token, &t.IsActive, &t.UserID)
	if err != nil {
		return nil, err
	}
	return t, nil
}

func listTripsByUser(db *sql.DB, userID int) ([]Trip, error) {
	rows, err := db.Query(
		"SELECT id, name, created_at, token, is_active, user_id FROM trips WHERE user_id = ? ORDER BY created_at DESC",
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var trips []Trip
	for rows.Next() {
		var t Trip
		if err := rows.Scan(&t.ID, &t.Name, &t.CreatedAt, &t.Token, &t.IsActive, &t.UserID); err != nil {
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

func createEntry(db *sql.DB, tripID, body string, lat, lon *float64, locationName *string, timestamp string, weatherCode *int, temperature *float64, countryCode *string) (int64, error) {
	res, err := db.Exec(
		"INSERT INTO entries (trip_id, body, lat, lon, location_name, timestamp, weather_code, temperature, country_code) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		tripID, body, lat, lon, locationName, timestamp, weatherCode, temperature, countryCode,
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

func getEntries(db *sql.DB, tripID string, descending bool) ([]Entry, error) {
	order := "ASC"
	if descending {
		order = "DESC"
	}
	rows, err := db.Query(
		"SELECT id, trip_id, body, lat, lon, location_name, timestamp, created_at, weather_code, temperature, country_code FROM entries WHERE trip_id = ? ORDER BY timestamp "+order,
		tripID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []Entry
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.TripID, &e.Body, &e.Lat, &e.Lon, &e.LocationName, &e.Timestamp, &e.CreatedAt, &e.WeatherCode, &e.Temperature, &e.CountryCode); err != nil {
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

func getLatestTrackpoint(db *sql.DB, tripID string) (*Trackpoint, error) {
	var tp Trackpoint
	var ts string
	err := db.QueryRow(
		"SELECT trip_id, lat, lon, altitude, speed, bearing, hdop, timestamp FROM trackpoints WHERE trip_id = ? ORDER BY timestamp DESC LIMIT 1",
		tripID,
	).Scan(&tp.TripID, &tp.Lat, &tp.Lon, &tp.Altitude, &tp.Speed, &tp.Bearing, &tp.HDOP, &ts)
	if err != nil {
		return nil, err
	}
	tp.Timestamp, _ = time.Parse(time.RFC3339, ts)
	return &tp, nil
}

func photobelongsToTrip(db *sql.DB, photoID int64, tripID string) bool {
	var count int
	db.QueryRow(
		"SELECT COUNT(*) FROM photos p JOIN entries e ON p.entry_id = e.id WHERE p.id = ? AND e.trip_id = ?",
		photoID, tripID,
	).Scan(&count)
	return count > 0
}

func deletePhoto(db *sql.DB, photoID int64) (string, error) {
	var filePath string
	err := db.QueryRow("SELECT file_path FROM photos WHERE id = ?", photoID).Scan(&filePath)
	if err != nil {
		return "", err
	}
	_, err = db.Exec("DELETE FROM photos WHERE id = ?", photoID)
	return filePath, err
}

func getEntryTripID(db *sql.DB, entryID int64) (string, error) {
	var tripID string
	err := db.QueryRow("SELECT trip_id FROM entries WHERE id = ?", entryID).Scan(&tripID)
	return tripID, err
}

func maxPhotoOrder(db *sql.DB, entryID int64) int {
	var maxOrder int
	db.QueryRow("SELECT COALESCE(MAX(sort_order), -1) FROM photos WHERE entry_id = ?", entryID).Scan(&maxOrder)
	return maxOrder
}
