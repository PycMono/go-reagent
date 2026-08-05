package main

import (
	logsdk "github.com/PycMono/go-logger-sdk"
	"github.com/PycMono/go-reagent/internal/bootstrap"
	"github.com/PycMono/go-reagent/internal/cli"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
)

func main() {
	logsdk.SetLogger(newApplicationLogger())

	fx.New(
		bootstrap.Module,
		cli.Module,
		fx.WithLogger(func() fxevent.Logger { return fxevent.NopLogger }),
	).Run()
}

func newApplicationLogger() logsdk.Logger {
	return logsdk.NewLogrus(logsdk.Options{
		LogFormat: "json",
		Module:    "go-reagent",
	})
}
