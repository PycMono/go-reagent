package main

import (
	logsdk "github.com/PycMono/go-logger-sdk"
	reagentinternal "github.com/PycMono/go-reagent/internal"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
)

func main() {
	logsdk.SetLogger(newApplicationLogger())

	fx.New(
		reagentinternal.Register,
		fx.WithLogger(func() fxevent.Logger { return fxevent.NopLogger }),
	).Run()
}

func newApplicationLogger() logsdk.Logger {
	return logsdk.NewLogrus(logsdk.Options{
		LogFormat: "json",
		Module:    "go-reagent",
	})
}
