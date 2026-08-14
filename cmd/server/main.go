package main

import (
	logsdk "github.com/PycMono/go-logger-sdk"
	"github.com/PycMono/go-reagent/application"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
)

func main() {
	logsdk.SetLogger(newApplicationLogger())
	fx.New(newAppOptions()...).Run()
}

func newAppOptions() []fx.Option {
	return []fx.Option{
		application.WebRegister,
		fx.WithLogger(func() fxevent.Logger { return fxevent.NopLogger }),
	}
}

func newApplicationLogger() logsdk.Logger {
	return logsdk.NewLogrus(logsdk.Options{LogFormat: "json", Module: "go-reagent-web"})
}
