package service

import (
	"context"
	"time"
)

type LocationQuery struct {
	Name        string
	CountryCode string
	Admin1      string
	Limit       int
}

type Location struct {
	Name        string
	Country     string
	CountryCode string
	Admin1      string
	Latitude    float64
	Longitude   float64
	Timezone    string
}

type ForecastRequest struct {
	Location  Location
	StartDate time.Time
	Days      int
}

type DailyForecast struct {
	Date                     string
	Condition                string
	WeatherCode              int
	PrecipitationProbability int
	TemperatureMinC          float64
	TemperatureMaxC          float64
	WindSpeedMaxKPH          float64
}

type Forecast struct {
	Location Location
	Days     []DailyForecast
}

type LocationResolver interface {
	ResolveLocations(context.Context, LocationQuery) ([]Location, error)
}

type WeatherProvider interface {
	Forecast(context.Context, ForecastRequest) (Forecast, error)
}
