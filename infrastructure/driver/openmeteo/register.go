package openmeteo

import (
	"github.com/PycMono/go-reagent/domain/service"
	"go.uber.org/fx"
)

var Register = fx.Options(
	fx.Provide(NewClient),
	fx.Provide(asLocationResolver),
	fx.Provide(asWeatherProvider),
)

// asLocationResolver and asWeatherProvider both build on the single *Client
// instance provided by NewClient, so the two interfaces share one client.
func asLocationResolver(client *Client) service.LocationResolver { return client }

func asWeatherProvider(client *Client) service.WeatherProvider { return client }
