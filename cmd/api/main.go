package main

import (
	"github.com/chronoflow/internal/app"
	"github.com/chronoflow/internal/launcher"
)

func main() {
	launcher.Main(app.NewAPI)
}
