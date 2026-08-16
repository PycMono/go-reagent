package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/PycMono/go-reagent/domain/service"
	"github.com/PycMono/go-reagent/pi/ai"
)

type weatherTool struct {
	resolver service.LocationResolver
	provider service.WeatherProvider
	clock    Clock
}

type weatherArgs struct {
	Location    string `json:"location"`
	CountryCode string `json:"country_code,omitempty"`
	Admin1      string `json:"admin1,omitempty"`
	Date        string `json:"date,omitempty"`
	Days        *int   `json:"days,omitempty"`
}

type weatherDayView struct {
	Date                     string  `json:"date"`
	WeatherCode              int     `json:"weather_code"`
	Condition                string  `json:"condition"`
	TemperatureMinC          float64 `json:"temperature_min_c"`
	TemperatureMaxC          float64 `json:"temperature_max_c"`
	PrecipitationProbability int     `json:"precipitation_probability"`
	WindSpeedMaxKPH          float64 `json:"wind_speed_max_kph"`
}

type weatherResult struct {
	Status      string           `json:"status"`
	Location    locationView     `json:"location"`
	GeneratedAt string           `json:"generated_at"`
	Days        []weatherDayView `json:"days"`
}

func newWeatherTool(resolver service.LocationResolver, provider service.WeatherProvider, clock Clock) *weatherTool {
	return &weatherTool{resolver: resolver, provider: provider, clock: clock}
}

func (t *weatherTool) Definition() ai.ToolDefinition {
	return ai.ToolDefinition{
		Name:         "get_weather",
		Description:  "按地点获取今天、明天或未来七天内的真实每日天气预报；重名地点会返回候选供用户确认。",
		ParallelSafe: true,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"location":     map[string]any{"type": "string", "minLength": 1, "maxLength": 120},
				"country_code": map[string]any{"type": "string", "pattern": "^[A-Za-z]{2}$"},
				"admin1":       map[string]any{"type": "string", "minLength": 1, "maxLength": 120},
				"date":         map[string]any{"type": "string", "minLength": 1, "maxLength": 10},
				"days":         map[string]any{"type": "integer", "minimum": 1, "maximum": 7},
			},
			"required":             []string{"location"},
			"additionalProperties": false,
		},
	}
}

func (t *weatherTool) Execute(ctx context.Context, raw json.RawMessage, _ ai.UpdateEmitter) (ai.ToolOutput, error) {
	arguments, err := decodeArguments[weatherArgs](raw)
	if err != nil {
		return ai.ToolOutput{}, err
	}
	location, failure, err := resolveLocation(ctx, t.resolver, locationInput{
		Location: arguments.Location, CountryCode: arguments.CountryCode, Admin1: arguments.Admin1,
	})
	if err != nil {
		return ai.ToolOutput{}, fmt.Errorf("resolve weather location: %w", err)
	}
	if failure != nil {
		return jsonOutput(failure)
	}

	today, zone, err := localDate(t.clock, location.Timezone)
	if err != nil {
		return ai.ToolOutput{}, err
	}
	startDate, err := parseForecastDate(arguments.Date, today, zone)
	if err != nil {
		return ai.ToolOutput{}, err
	}
	days := 1
	if arguments.Days != nil {
		days = *arguments.Days
	}
	if days < 1 || days > 7 {
		return ai.ToolOutput{}, invalidArguments("invalid forecast days", errors.New("days must be between 1 and 7"))
	}
	lastDate := startDate.AddDate(0, 0, days-1)
	if startDate.Before(today) || lastDate.After(today.AddDate(0, 0, 6)) {
		return ai.ToolOutput{}, invalidArguments("forecast date is outside available range", errors.New("forecast range must be within the location's next seven calendar days"))
	}

	forecast, err := t.provider.Forecast(ctx, service.ForecastRequest{Location: location, StartDate: startDate, Days: days})
	if err != nil {
		return ai.ToolOutput{}, fmt.Errorf("get weather forecast: %w", err)
	}
	resultDays := make([]weatherDayView, len(forecast.Days))
	for i, day := range forecast.Days {
		resultDays[i] = weatherDayView{
			Date: day.Date, WeatherCode: day.WeatherCode, Condition: day.Condition,
			TemperatureMinC: day.TemperatureMinC, TemperatureMaxC: day.TemperatureMaxC,
			PrecipitationProbability: day.PrecipitationProbability, WindSpeedMaxKPH: day.WindSpeedMaxKPH,
		}
	}
	return jsonOutput(weatherResult{
		Status: "ok", Location: locationToView(location), GeneratedAt: t.clock().In(zone).Format(time.RFC3339), Days: resultDays,
	})
}

func localDate(clock Clock, timezone string) (time.Time, *time.Location, error) {
	zone, err := time.LoadLocation(timezone)
	if err != nil {
		return time.Time{}, nil, invalidArguments("invalid location timezone", err)
	}
	now := clock().In(zone)
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, zone), zone, nil
}

func parseForecastDate(value string, today time.Time, zone *time.Location) (time.Time, error) {
	switch value {
	case "", "today":
		return today, nil
	case "tomorrow":
		return today.AddDate(0, 0, 1), nil
	default:
		parsed, err := time.ParseInLocation("2006-01-02", value, zone)
		if err != nil || parsed.Format("2006-01-02") != value {
			if err == nil {
				err = errors.New("date must use YYYY-MM-DD")
			}
			return time.Time{}, invalidArguments("invalid forecast date", err)
		}
		return parsed, nil
	}
}
