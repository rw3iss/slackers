package weather

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// FetchWeather queries wttr.in for a compact weather forecast.
// Returns a formatted multi-line string suitable for the Output view.
func FetchWeather(city string) (string, error) {
	// wttr.in supports a compact format via query params.
	// ?format=... gives us a custom one-liner per line.
	// We'll use the "v2" format for a nice compact view.
	encoded := url.PathEscape(city)
	apiURL := fmt.Sprintf("https://wttr.in/%s?format=%%l:+%%c+%%t+%%w+%%h+%%P\\n%%C+%%S+%%s\\n3+day:+%%f", encoded)

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "slackers-weather-plugin/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("wttr.in returned %d", resp.StatusCode)
	}

	result := strings.TrimSpace(string(body))
	if result == "" {
		return "", fmt.Errorf("empty response from wttr.in")
	}

	// Also fetch a more detailed 3-day forecast.
	detailURL := fmt.Sprintf("https://wttr.in/%s?T&n&q", encoded)
	req2, _ := http.NewRequest("GET", detailURL, nil)
	req2.Header.Set("User-Agent", "curl") // wttr.in returns ASCII art for curl
	resp2, err := client.Do(req2)
	if err == nil {
		defer resp2.Body.Close()
		detail, _ := io.ReadAll(resp2.Body)
		if len(detail) > 0 {
			return string(detail), nil
		}
	}

	return result, nil
}
