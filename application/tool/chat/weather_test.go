package chat

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/PycMono/go-reagent/domain/service"
	"github.com/PycMono/go-reagent/pi/ai"
	pierrors "github.com/PycMono/go-reagent/pi/harness/errors"
)

func TestWeatherToolDefinitionIsStrictAndParallelSafe(t *testing.T) {
	definition := newWeatherTool(&fakeResolver{}, &fakeWeatherProvider{}, fixedClock("2026-08-16T01:30:00Z")).Definition()
	if definition.Name != "get_weather" || definition.Description == "" || !definition.ParallelSafe {
		t.Fatalf("definition = %#v", definition)
	}
	schema, ok := definition.InputSchema.(map[string]any)
	if !ok || schema["type"] != "object" || schema["additionalProperties"] != false {
		t.Fatalf("schema = %#v", definition.InputSchema)
	}
	properties := schema["properties"].(map[string]any)
	if len(properties) != 5 {
		t.Fatalf("properties = %#v", properties)
	}
}

func TestWeatherToolReturnsAmbiguousCandidatesWithoutForecast(t *testing.T) {
	resolver := &fakeResolver{locations: []service.Location{
		{Name: "Chaoyang", Country: "China", CountryCode: "CN", Admin1: "Beijing", Timezone: "Asia/Shanghai"},
		{Name: "Chaoyang", Country: "China", CountryCode: "CN", Admin1: "Liaoning", Timezone: "Asia/Shanghai"},
	}}
	provider := &fakeWeatherProvider{}
	tool := newWeatherTool(resolver, provider, fixedClock("2026-08-16T01:30:00Z"))
	output, err := tool.Execute(context.Background(), json.RawMessage(`{"location":"朝阳"}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	got := decodeToolJSON[locationFailure](t, output)
	if got.Status != "ambiguous" || got.Query != "朝阳" || len(got.Candidates) != 2 || provider.calls != 0 {
		t.Fatalf("result = %#v, forecast calls = %d", got, provider.calls)
	}
	text, _ := ai.TextContent(output.Content)
	if strings.Contains(text, "latitude") || strings.Contains(text, "longitude") {
		t.Fatalf("ambiguous result exposes coordinates: %s", text)
	}
}

func TestWeatherToolReturnsNotFound(t *testing.T) {
	provider := &fakeWeatherProvider{}
	tool := newWeatherTool(&fakeResolver{}, provider, fixedClock("2026-08-16T01:30:00Z"))
	output, err := tool.Execute(context.Background(), json.RawMessage(`{"location":"不存在"}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	got := decodeToolJSON[locationFailure](t, output)
	if got.Status != "not_found" || got.Query != "不存在" || len(got.Candidates) != 0 || provider.calls != 0 {
		t.Fatalf("result = %#v, forecast calls = %d", got, provider.calls)
	}
}

func TestWeatherToolFiltersCountryAndAdminBeforeForecast(t *testing.T) {
	resolver := &fakeResolver{locations: []service.Location{
		{Name: "Springfield", Country: "United States", CountryCode: "US", Admin1: "Illinois", Latitude: 39.8, Longitude: -89.6, Timezone: "America/Chicago"},
		{Name: "Springfield", Country: "United States", CountryCode: "US", Admin1: "Missouri", Latitude: 37.2, Longitude: -93.3, Timezone: "America/Chicago"},
	}}
	provider := &fakeWeatherProvider{forecast: forecastWithDays("2026-08-15", 1)}
	tool := newWeatherTool(resolver, provider, fixedClock("2026-08-16T01:30:00Z"))
	output, err := tool.Execute(context.Background(), json.RawMessage(`{"location":" Springfield ","country_code":" us ","admin1":" missouri "}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	got := decodeToolJSON[weatherResult](t, output)
	if got.Status != "ok" || got.Location.Admin1 != "Missouri" || provider.calls != 1 {
		t.Fatalf("result = %#v, calls = %d", got, provider.calls)
	}
	if resolver.query.Name != "Springfield" || resolver.query.CountryCode != "US" || resolver.query.Admin1 != "missouri" || resolver.query.Limit != 5 {
		t.Fatalf("location query = %#v", resolver.query)
	}
}

func TestWeatherToolMapsForecastAndUsesLocationTimezone(t *testing.T) {
	location := service.Location{Name: "Beijing", Country: "China", CountryCode: "CN", Admin1: "Beijing", Latitude: 39.9, Longitude: 116.4, Timezone: "Asia/Shanghai"}
	provider := &fakeWeatherProvider{forecast: service.Forecast{Location: location, Days: []service.DailyForecast{
		{Date: "2026-08-16", WeatherCode: 61, Condition: "rain", TemperatureMinC: 23.1, TemperatureMaxC: 31.2, PrecipitationProbability: 70, WindSpeedMaxKPH: 18.4},
	}}}
	tool := newWeatherTool(&fakeResolver{locations: []service.Location{location}}, provider, fixedClock("2026-08-16T01:30:00Z"))
	output, err := tool.Execute(context.Background(), json.RawMessage(`{"location":"北京"}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	got := decodeToolJSON[weatherResult](t, output)
	if got.Status != "ok" || got.GeneratedAt != "2026-08-16T09:30:00+08:00" || len(got.Days) != 1 {
		t.Fatalf("result = %#v", got)
	}
	if got.Days[0].Date != "2026-08-16" || got.Days[0].Condition != "rain" || got.Days[0].PrecipitationProbability != 70 {
		t.Fatalf("day = %#v", got.Days[0])
	}
	if provider.request.StartDate.Format("2006-01-02") != "2026-08-16" || provider.request.Days != 1 || provider.request.Location != location {
		t.Fatalf("forecast request = %#v", provider.request)
	}
}

func TestWeatherToolCalculatesDateInResolvedTimezone(t *testing.T) {
	location := service.Location{Name: "Beijing", Country: "China", CountryCode: "CN", Timezone: "Asia/Shanghai"}
	provider := &fakeWeatherProvider{forecast: forecastWithDays("2026-08-17", 1)}
	tool := newWeatherTool(&fakeResolver{locations: []service.Location{location}}, provider, fixedClock("2026-08-16T16:30:00Z"))
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"location":"北京","date":"today"}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if provider.request.StartDate.Format("2006-01-02") != "2026-08-17" {
		t.Fatalf("start date = %s", provider.request.StartDate)
	}
}

func TestWeatherToolAcceptsSupportedDateWindows(t *testing.T) {
	tests := []struct {
		name      string
		arguments string
		wantStart string
		wantDays  int
	}{
		{name: "today seven days", arguments: `{"location":"Beijing","date":"today","days":7}`, wantStart: "2026-08-16", wantDays: 7},
		{name: "tomorrow six days", arguments: `{"location":"Beijing","date":"tomorrow","days":6}`, wantStart: "2026-08-17", wantDays: 6},
		{name: "explicit date", arguments: `{"location":"Beijing","date":"2026-08-18","days":2}`, wantStart: "2026-08-18", wantDays: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			location := service.Location{Name: "Beijing", Country: "China", CountryCode: "CN", Timezone: "Asia/Shanghai"}
			provider := &fakeWeatherProvider{dynamic: true}
			tool := newWeatherTool(&fakeResolver{locations: []service.Location{location}}, provider, fixedClock("2026-08-16T01:30:00Z"))
			_, err := tool.Execute(context.Background(), json.RawMessage(test.arguments), nil)
			if err != nil {
				t.Fatal(err)
			}
			if provider.request.StartDate.Format("2006-01-02") != test.wantStart || provider.request.Days != test.wantDays {
				t.Fatalf("request = %#v", provider.request)
			}
		})
	}
}

func TestWeatherToolRejectsInvalidArguments(t *testing.T) {
	tests := []string{
		`{"location":""}`,
		`{"location":"   "}`,
		`{"location":"Beijing","country_code":"C"}`,
		`{"location":"Beijing","country_code":"中国"}`,
		`{"location":"Beijing","date":"yesterday"}`,
		`{"location":"Beijing","date":"2026-8-16"}`,
		`{"location":"Beijing","days":0}`,
		`{"location":"Beijing","days":8}`,
		`{"location":"Beijing","date":"tomorrow","days":7}`,
		`{"location":"Beijing","date":"2026-08-15"}`,
		`{"location":"Beijing","date":"2026-08-23"}`,
		`{"location":"Beijing","unexpected":true}`,
		`{"location":"Beijing"} {}`,
	}
	location := service.Location{Name: "Beijing", Country: "China", CountryCode: "CN", Timezone: "Asia/Shanghai"}
	for _, arguments := range tests {
		t.Run(arguments, func(t *testing.T) {
			tool := newWeatherTool(&fakeResolver{locations: []service.Location{location}}, &fakeWeatherProvider{dynamic: true}, fixedClock("2026-08-16T01:30:00Z"))
			_, err := tool.Execute(context.Background(), json.RawMessage(arguments), nil)
			if pierrors.ErrorCodeOf(err) != pierrors.ErrorCodeToolInvalidArguments {
				t.Fatalf("error = %v, code = %s", err, pierrors.ErrorCodeOf(err))
			}
		})
	}
}

func TestWeatherToolRejectsInvalidResolvedTimezone(t *testing.T) {
	location := service.Location{Name: "Nowhere", Country: "Nowhere", CountryCode: "NW", Timezone: "invalid/timezone"}
	tool := newWeatherTool(&fakeResolver{locations: []service.Location{location}}, &fakeWeatherProvider{}, fixedClock("2026-08-16T01:30:00Z"))
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"location":"Nowhere"}`), nil)
	if pierrors.ErrorCodeOf(err) != pierrors.ErrorCodeToolInvalidArguments {
		t.Fatalf("error = %v", err)
	}
}

func TestWeatherToolReturnsResolverAndProviderErrors(t *testing.T) {
	resolverErr := errors.New("resolver unavailable")
	tool := newWeatherTool(&fakeResolver{err: resolverErr}, &fakeWeatherProvider{}, fixedClock("2026-08-16T01:30:00Z"))
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"location":"Beijing"}`), nil); !errors.Is(err, resolverErr) {
		t.Fatalf("resolver error = %v", err)
	}

	providerErr := errors.New("forecast unavailable")
	location := service.Location{Name: "Beijing", Country: "China", CountryCode: "CN", Timezone: "Asia/Shanghai"}
	tool = newWeatherTool(&fakeResolver{locations: []service.Location{location}}, &fakeWeatherProvider{err: providerErr}, fixedClock("2026-08-16T01:30:00Z"))
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"location":"Beijing"}`), nil); !errors.Is(err, providerErr) {
		t.Fatalf("provider error = %v", err)
	}
}

func TestWeatherToolHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tool := newWeatherTool(&fakeResolver{}, &fakeWeatherProvider{}, fixedClock("2026-08-16T01:30:00Z"))
	_, err := tool.Execute(ctx, json.RawMessage(`{"location":"Beijing"}`), nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}

type fakeResolver struct {
	locations []service.Location
	err       error
	query     service.LocationQuery
}

func (f *fakeResolver) ResolveLocations(ctx context.Context, query service.LocationQuery) ([]service.Location, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.query = query
	return append([]service.Location(nil), f.locations...), f.err
}

type fakeWeatherProvider struct {
	forecast service.Forecast
	err      error
	calls    int
	request  service.ForecastRequest
	dynamic  bool
}

func (f *fakeWeatherProvider) Forecast(ctx context.Context, request service.ForecastRequest) (service.Forecast, error) {
	if err := ctx.Err(); err != nil {
		return service.Forecast{}, err
	}
	f.calls++
	f.request = request
	if f.dynamic {
		return forecastWithDays(request.StartDate.Format("2006-01-02"), request.Days), f.err
	}
	return f.forecast, f.err
}

func fixedClock(value string) Clock {
	instant, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return func() time.Time { return instant }
}

func forecastWithDays(start string, count int) service.Forecast {
	date, err := time.Parse("2006-01-02", start)
	if err != nil {
		panic(err)
	}
	days := make([]service.DailyForecast, count)
	for i := range days {
		days[i] = service.DailyForecast{Date: date.AddDate(0, 0, i).Format("2006-01-02")}
	}
	return service.Forecast{Days: days}
}

func decodeToolJSON[T any](t *testing.T, output ai.ToolOutput) T {
	t.Helper()
	text, err := ai.TextContent(output.Content)
	if err != nil {
		t.Fatal(err)
	}
	var value T
	if err := json.Unmarshal([]byte(text), &value); err != nil {
		t.Fatal(err)
	}
	return value
}
