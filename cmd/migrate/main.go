package main

import (
	"fmt"
	"os"

	"github.com/chronoflow/internal/migration"
)

func main() {
	if err := migration.RunCLI(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "ChronoFlow migration failed: %v\n", err)
		os.Exit(1)
	}
}
