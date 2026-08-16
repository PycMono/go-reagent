package openmeteo

import (
	"reflect"
	"testing"

	"github.com/PycMono/go-reagent/domain/service"
	"go.uber.org/fx"
)

func TestRegisterProvidesBothWeatherInterfacesFromOneClient(t *testing.T) {
	var resolver service.LocationResolver
	var provider service.WeatherProvider
	if err := fx.ValidateApp(Register, fx.Populate(&resolver, &provider)); err != nil {
		t.Fatalf("Open-Meteo graph is invalid: %v", err)
	}
	app := fx.New(Register, fx.Populate(&resolver, &provider), fx.NopLogger)
	if err := app.Err(); err != nil {
		t.Fatal(err)
	}
	if resolver == nil || provider == nil {
		t.Fatalf("resolver = %#v, provider = %#v", resolver, provider)
	}
	if reflect.ValueOf(resolver).Pointer() != reflect.ValueOf(provider).Pointer() {
		t.Fatalf("interfaces use different clients: resolver=%p provider=%p", resolver, provider)
	}
}
