package main

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"
)

type GPX struct {
	Tracks []Track `xml:"trk"`
}

type Track struct {
	Segments []Segment `xml:"trkseg"`
}

type Segment struct {
	Points []Point `xml:"trkpt"`
}

type Point struct {
	Lat  string `xml:"lat,attr"`
	Lon  string `xml:"lon,attr"`
	Ele  string `xml:"ele"`
	Time string `xml:"time"`
}

func main() {
	if len(os.Args) < 4 {
		fmt.Fprintf(os.Stderr, "Usage: import-gpx <server-url> <admin-token> <gpx-file>\n")
		os.Exit(1)
	}

	serverURL := os.Args[1]
	token := os.Args[2]
	gpxFile := os.Args[3]

	data, err := os.ReadFile(gpxFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read file: %v\n", err)
		os.Exit(1)
	}

	var gpx GPX
	if err := xml.Unmarshal(data, &gpx); err != nil {
		fmt.Fprintf(os.Stderr, "parse gpx: %v\n", err)
		os.Exit(1)
	}

	count := 0
	for _, trk := range gpx.Tracks {
		for _, seg := range trk.Segments {
			for _, pt := range seg.Points {
				if pt.Time == "" {
					continue
				}

				t, err := time.Parse(time.RFC3339Nano, pt.Time)
				if err != nil {
					t, err = time.Parse("2006-01-02T15:04:05Z", pt.Time)
					if err != nil {
						continue
					}
				}

				params := url.Values{
					"token":     {token},
					"lat":       {pt.Lat},
					"lon":       {pt.Lon},
					"timestamp": {fmt.Sprintf("%d", t.UnixMilli())},
				}
				if pt.Ele != "" {
					params.Set("altitude", pt.Ele)
				}

				resp, err := http.Get(serverURL + "/api/track?" + params.Encode())
				if err != nil {
					fmt.Fprintf(os.Stderr, "request failed: %v\n", err)
					continue
				}
				resp.Body.Close()
				count++
			}
		}
	}

	fmt.Printf("Imported %d trackpoints\n", count)
}
