package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"
)

type Weather struct {
	Code        int
	Temperature float64
}

func fetchWeather(lat, lon float64, ts time.Time) (*Weather, error) {
	date := ts.Format("2006-01-02")
	hour := ts.Hour()

	url := fmt.Sprintf(
		"https://archive-api.open-meteo.com/v1/archive?latitude=%.4f&longitude=%.4f&start_date=%s&end_date=%s&hourly=temperature_2m,weather_code",
		lat, lon, date, date,
	)

	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetch weather: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("weather API returned %d", resp.StatusCode)
	}

	var result struct {
		Hourly struct {
			Time          []string  `json:"time"`
			Temperature2m []float64 `json:"temperature_2m"`
			WeatherCode   []int     `json:"weather_code"`
		} `json:"hourly"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode weather: %w", err)
	}

	if hour >= len(result.Hourly.Temperature2m) || hour >= len(result.Hourly.WeatherCode) {
		return nil, fmt.Errorf("no weather data for hour %d", hour)
	}

	return &Weather{
		Code:        result.Hourly.WeatherCode[hour],
		Temperature: math.Round(result.Hourly.Temperature2m[hour]*10) / 10,
	}, nil
}

func fetchCountryCode(lat, lon float64) (string, error) {
	url := fmt.Sprintf(
		"https://nominatim.openstreetmap.org/reverse?format=json&lat=%.6f&lon=%.6f&zoom=3",
		lat, lon,
	)

	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "trippy-track/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result struct {
		Address struct {
			CountryCode string `json:"country_code"`
		} `json:"address"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.Address.CountryCode, nil
}

// CountryFlag converts a 2-letter country code to a flag emoji.
func CountryFlag(code string) string {
	if len(code) != 2 {
		return ""
	}
	r1 := rune(code[0]-'a') + 0x1F1E6
	r2 := rune(code[1]-'a') + 0x1F1E6
	return string([]rune{r1, r2})
}

// CountryName returns the English name for common country codes.
func CountryName(code string) string {
	names := map[string]string{
		"de": "Germany", "at": "Austria", "ch": "Switzerland", "nl": "Netherlands",
		"be": "Belgium", "fr": "France", "it": "Italy", "es": "Spain", "pt": "Portugal",
		"gb": "United Kingdom", "ie": "Ireland", "dk": "Denmark", "se": "Sweden",
		"no": "Norway", "fi": "Finland", "pl": "Poland", "cz": "Czech Republic",
		"sk": "Slovakia", "hu": "Hungary", "hr": "Croatia", "si": "Slovenia",
		"ba": "Bosnia", "rs": "Serbia", "me": "Montenegro", "al": "Albania",
		"gr": "Greece", "tr": "Turkey", "bg": "Bulgaria", "ro": "Romania",
		"us": "United States", "ca": "Canada", "mx": "Mexico",
		"jp": "Japan", "cn": "China", "kr": "South Korea", "th": "Thailand",
		"vn": "Vietnam", "id": "Indonesia", "in": "India", "au": "Australia",
		"nz": "New Zealand", "br": "Brazil", "ar": "Argentina", "cl": "Chile",
		"co": "Colombia", "pe": "Peru", "ma": "Morocco", "eg": "Egypt",
		"za": "South Africa", "ke": "Kenya", "tz": "Tanzania",
	}
	if name, ok := names[code]; ok {
		return name
	}
	return strings.ToUpper(code)
}

// WeatherLabel returns a short description for a WMO weather code.
func WeatherLabel(code int) string {
	switch {
	case code == 0:
		return "Clear"
	case code == 1:
		return "Mostly clear"
	case code == 2:
		return "Partly cloudy"
	case code == 3:
		return "Overcast"
	case code == 45 || code == 48:
		return "Foggy"
	case code >= 51 && code <= 55:
		return "Drizzle"
	case code == 56 || code == 57:
		return "Freezing drizzle"
	case code == 61:
		return "Light rain"
	case code == 63:
		return "Rain"
	case code == 65:
		return "Heavy rain"
	case code == 66 || code == 67:
		return "Freezing rain"
	case code == 71:
		return "Light snow"
	case code == 73:
		return "Snow"
	case code == 75:
		return "Heavy snow"
	case code == 77:
		return "Snow grains"
	case code >= 80 && code <= 82:
		return "Showers"
	case code == 85 || code == 86:
		return "Snow showers"
	case code == 95:
		return "Thunderstorm"
	case code == 96 || code == 99:
		return "Thunderstorm with hail"
	default:
		return "Cloudy"
	}
}

// WeatherIcon returns an emoji for a WMO weather code.
func WeatherIcon(code int) string {
	switch {
	case code == 0:
		return "\u2600\ufe0f"
	case code <= 3:
		return "\u26c5"
	case code == 45 || code == 48:
		return "\U0001F32B\ufe0f"
	case code >= 51 && code <= 57:
		return "\U0001F326\ufe0f"
	case code >= 61 && code <= 67:
		return "\U0001F327\ufe0f"
	case code >= 71 && code <= 77:
		return "\u2744\ufe0f"
	case code >= 80 && code <= 82:
		return "\U0001F326\ufe0f"
	case code >= 85 && code <= 86:
		return "\U0001F328\ufe0f"
	case code >= 95:
		return "\u26c8\ufe0f"
	default:
		return "\u2601\ufe0f"
	}
}
