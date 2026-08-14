package main

import (
	logsdk "github.com/PycMono/go-logger-sdk"
	webapplication "github.com/PycMono/go-reagent/application/web"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
)

func main() {
	logsdk.SetLogger(newApplicationLogger())
	fx.New(newAppOptions()...).Run()
}

func newAppOptions() []fx.Option {
	return []fx.Option{
		webapplication.Register,
		fx.WithLogger(func() fxevent.Logger { return fxevent.NopLogger }),
	}
}

func newApplicationLogger() logsdk.Logger {
	return logsdk.NewLogrus(logsdk.Options{LogFormat: "json", Module: "go-reagent-web"})
}
