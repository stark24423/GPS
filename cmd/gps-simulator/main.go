package main

import (
	"errors"
	"fmt"
	"os"

	"gpssim/internal/platform/elevation"
	"gpssim/internal/ui"
)

func main() {
	if err := elevation.EnsureAdministrator(); err != nil {
		if errors.Is(err, elevation.ErrRelaunched) {
			return
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ui.Run()
}
