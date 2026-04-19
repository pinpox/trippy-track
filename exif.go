package main

import (
	"io"
	"math"
	"time"

	"github.com/rwcarlsen/goexif/exif"
)

type ExifData struct {
	Lat       *float64
	Lon       *float64
	Timestamp *time.Time
}

func extractExif(r io.Reader) (*ExifData, error) {
	x, err := exif.Decode(r)
	if err != nil {
		return &ExifData{}, nil // Not a JPEG or no EXIF — not an error
	}

	data := &ExifData{}

	// GPS coordinates
	lat, lon, err := x.LatLong()
	if err == nil && !math.IsNaN(lat) && !math.IsNaN(lon) {
		data.Lat = &lat
		data.Lon = &lon
	}

	// Timestamp
	t, err := x.DateTime()
	if err == nil {
		data.Timestamp = &t
	}

	return data, nil
}
