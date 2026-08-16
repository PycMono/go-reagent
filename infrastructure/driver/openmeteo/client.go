package openmeteo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/PycMono/go-reagent/domain/service"
)

const (
	defaultGeocodingURL = "https://geocoding-api.open-meteo.com/v1/search"
	defaultForecastURL  = "https://api.open-meteo.com/v1/forecast"
	maxResponseBytes    = 1 << 20
	userAgent           = "go-reagent/1.0"
)

type Client struct {
	geocodingURL string
	forecastURL  string
	httpClient   *http.Client
}

func NewClient() *Client {
	return newClient(defaultGeocodingURL, defaultForecastURL, &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	})
}

func newClient(geocodingURL, forecastURL string, httpClient *http.Client) *Client {
	return &Client{geocodingURL: geocodingURL, forecastURL: forecastURL, httpClient: httpClient}
}

func (c *Client) ResolveLocations(ctx context.Context, query service.LocationQuery) ([]service.Location, error) {
	endpoint, err := url.Parse(c.geocodingURL)
	if err != nil {
		return nil, errors.New("invalid Open-Meteo geocoding endpoint")
	}
	limit := query.Limit
	if limit <= 0 || limit > 5 {
		limit = 5
	}
	values := endpoint.Query()
	values.Set("name", query.Name)
	values.Set("count", strconv.Itoa(limit))
	values.Set("language", "en")
	values.Set("format", "json")
	endpoint.RawQuery = values.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create Open-Meteo geocoding request: %w", err)
	}
	request.Header.Set("User-Agent", userAgent)
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request Open-Meteo geocoding: %w", err)
	}
	defer response.Body.Close()

	var payload struct {
		Results []struct {
			Name        string   `json:"name"`
			Country     string   `json:"country"`
			CountryCode string   `json:"country_code"`
			Admin1      string   `json:"admin1"`
			Latitude    *float64 `json:"latitude"`
			Longitude   *float64 `json:"longitude"`
			Timezone    string   `json:"timezone"`
		} `json:"results"`
	}
	if err := decodeResponse(response, &payload); err != nil {
		return nil, fmt.Errorf("decode Open-Meteo geocoding response: %w", err)
	}

	locations := make([]service.Location, 0, len(payload.Results))
	for _, result := range payload.Results {
		if result.Name == "" || result.Country == "" || result.CountryCode == "" ||
			result.Latitude == nil || result.Longitude == nil || result.Timezone == "" {
			return nil, errors.New("Open-Meteo geocoding response is missing required fields")
		}
		locations = append(locations, service.Location{
			Name: result.Name, Country: result.Country, CountryCode: result.CountryCode,
			Admin1: result.Admin1, Latitude: *result.Latitude, Longitude: *result.Longitude,
			Timezone: result.Timezone,
		})
	}
	return locations, nil
}

func decodeResponse(response *http.Response, target any) error {
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("Open-Meteo returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read Open-Meteo response: %w", err)
	}
	if len(data) > maxResponseBytes {
		return errors.New("Open-Meteo response exceeds 1 MiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(target); err != nil {
		return errors.New("Open-Meteo returned invalid JSON")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("Open-Meteo returned trailing JSON")
	}
	return nil
}
