package main

import (
	logsdk "github.com/PycMono/go-logger-sdk"
	"github.com/PycMono/go-reagent/application"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
)

func main() {
	logsdk.SetLogger(newApplicationLogger())

	fx.New(
		application.Module,
		fx.WithLogger(func() fxevent.Logger { return fxevent.NopLogger }),
	).Run()
}

func newApplicationLogger() logsdk.Logger {
	return logsdk.NewLogrus(logsdk.Options{
		LogFormat: "json",
		Module:    "go-reagent",
	})
}
