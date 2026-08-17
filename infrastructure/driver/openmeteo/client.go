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

type transportError struct {
	operation string
	err       error
}

func (e *transportError) Error() string {
	return e.operation + " failed"
}

func (e *transportError) Unwrap() error {
	return e.err
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
		return nil, &transportError{operation: "request Open-Meteo geocoding", err: err}
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

func (c *Client) Forecast(ctx context.Context, request service.ForecastRequest) (service.Forecast, error) {
	if request.Days <= 0 || request.Days > 7 {
		return service.Forecast{}, errors.New("Open-Meteo forecast days must be between 1 and 7")
	}
	endpoint, err := url.Parse(c.forecastURL)
	if err != nil {
		return service.Forecast{}, errors.New("invalid Open-Meteo forecast endpoint")
	}
	endDate := request.StartDate.AddDate(0, 0, request.Days-1)
	values := endpoint.Query()
	values.Set("latitude", strconv.FormatFloat(request.Location.Latitude, 'f', -1, 64))
	values.Set("longitude", strconv.FormatFloat(request.Location.Longitude, 'f', -1, 64))
	values.Set("timezone", request.Location.Timezone)
	values.Set("start_date", request.StartDate.Format("2006-01-02"))
	values.Set("end_date", endDate.Format("2006-01-02"))
	values.Set("daily", "weather_code,temperature_2m_min,temperature_2m_max,precipitation_probability_max,wind_speed_10m_max")
	endpoint.RawQuery = values.Encode()

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return service.Forecast{}, fmt.Errorf("create Open-Meteo forecast request: %w", err)
	}
	httpRequest.Header.Set("User-Agent", userAgent)
	response, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return service.Forecast{}, &transportError{operation: "request Open-Meteo forecast", err: err}
	}
	defer response.Body.Close()

	var payload struct {
		Daily *struct {
			Time                     []*string  `json:"time"`
			WeatherCode              []*int     `json:"weather_code"`
			TemperatureMinC          []*float64 `json:"temperature_2m_min"`
			TemperatureMaxC          []*float64 `json:"temperature_2m_max"`
			PrecipitationProbability []*int     `json:"precipitation_probability_max"`
			WindSpeedMaxKPH          []*float64 `json:"wind_speed_10m_max"`
		} `json:"daily"`
	}
	if err := decodeResponse(response, &payload); err != nil {
		return service.Forecast{}, fmt.Errorf("decode Open-Meteo forecast response: %w", err)
	}
	if payload.Daily == nil {
		return service.Forecast{}, errors.New("Open-Meteo forecast response is missing daily data")
	}
	daily := payload.Daily
	if len(daily.Time) != request.Days || len(daily.WeatherCode) != request.Days ||
		len(daily.TemperatureMinC) != request.Days || len(daily.TemperatureMaxC) != request.Days ||
		len(daily.PrecipitationProbability) != request.Days || len(daily.WindSpeedMaxKPH) != request.Days {
		return service.Forecast{}, errors.New("Open-Meteo forecast response has inconsistent array lengths")
	}

	days := make([]service.DailyForecast, request.Days)
	for i := range request.Days {
		if daily.Time[i] == nil || daily.WeatherCode[i] == nil || daily.TemperatureMinC[i] == nil ||
			daily.TemperatureMaxC[i] == nil || daily.PrecipitationProbability[i] == nil ||
			daily.WindSpeedMaxKPH[i] == nil {
			return service.Forecast{}, errors.New("Open-Meteo forecast response contains null values")
		}
		expectedDate := request.StartDate.AddDate(0, 0, i).Format("2006-01-02")
		if *daily.Time[i] != expectedDate {
			return service.Forecast{}, errors.New("Open-Meteo forecast response has invalid date sequence")
		}
		days[i] = service.DailyForecast{
			Date: *daily.Time[i], WeatherCode: *daily.WeatherCode[i], Condition: conditionForCode(*daily.WeatherCode[i]),
			TemperatureMinC: *daily.TemperatureMinC[i], TemperatureMaxC: *daily.TemperatureMaxC[i],
			PrecipitationProbability: *daily.PrecipitationProbability[i], WindSpeedMaxKPH: *daily.WindSpeedMaxKPH[i],
		}
	}
	return service.Forecast{Location: request.Location, Days: days}, nil
}

func conditionForCode(code int) string {
	switch {
	case code == 0:
		return "clear"
	case code >= 1 && code <= 3:
		return "partly_cloudy"
	case code == 45 || code == 48:
		return "fog"
	case code >= 51 && code <= 57:
		return "drizzle"
	case (code >= 61 && code <= 67) || (code >= 80 && code <= 82):
		return "rain"
	case (code >= 71 && code <= 77) || code == 85 || code == 86:
		return "snow"
	case code == 95 || code == 96 || code == 99:
		return "thunderstorm"
	default:
		return "unknown"
	}
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
