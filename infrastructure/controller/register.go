package controller

import (
	httpcontroller "github.com/PycMono/go-reagent/infrastructure/controller/http"
	"go.uber.org/fx"
)

var Register = fx.Options(httpcontroller.Register)
